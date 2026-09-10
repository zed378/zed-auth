package management

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

type fakeRecorder struct {
	written []audit.Event
	err     error
}

func (f *fakeRecorder) Write(_ context.Context, _ *postgres.Tx, e audit.Event) error {
	if f.err != nil {
		return f.err
	}
	f.written = append(f.written, e)
	return nil
}

type countingAudit struct{ missed []string }

func (c *countingAudit) MutationNotAudited(route string) { c.missed = append(c.missed, route) }

// guarded serves r through the guard, with a handler that optionally audits.
func guarded(t *testing.T, h http.Handler, r *http.Request) (*httptest.ResponseRecorder, *countingAudit, string) {
	t.Helper()

	var logged bytes.Buffer
	observer := &countingAudit{}
	g := &AuditGuard{
		Log:      slog.New(slog.NewTextHandler(&logged, nil)),
		Observer: observer,
	}

	w := httptest.NewRecorder()
	g.Wrap(h).ServeHTTP(w, r)
	return w, observer, logged.String()
}

// --- the guard ---------------------------------------------------------------------------

// The finding this whole mechanism exists for: a handler that changed something,
// answered 201, and recorded nothing. Nothing errors and nothing is slow, so
// without the guard the gap is found during an incident — when the record is
// needed and absent.
func TestASuccessfulMutationThatAuditsNothingIsReported(t *testing.T) {
	handler := &ran{status: http.StatusCreated, response: `{"id":"x"}`}

	w, observer, logged := guarded(t, handler.handler(), post("", `{}`))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	if len(observer.missed) != 1 {
		t.Fatalf("the metric was incremented %d times, want 1", len(observer.missed))
	}
	if !strings.Contains(logged, "without writing an audit event") {
		t.Errorf("nothing was logged:\n%s", logged)
	}
}

// A handler that DID audit is not reported. Otherwise the metric is noise and
// nobody alerts on it, which is the same as not having it.
func TestAnAuditedMutationIsNotReported(t *testing.T) {
	recorder := &fakeRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := Audit(r.Context(), recorder, nil, audit.Event{Type: audit.EventUserCreated}); err != nil {
			t.Errorf("Audit: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})

	_, observer, logged := guarded(t, handler, post("", `{}`))

	if len(observer.missed) != 0 {
		t.Errorf("an audited mutation was reported: %v", observer.missed)
	}
	if strings.Contains(logged, "without writing an audit event") {
		t.Errorf("an audited mutation was logged:\n%s", logged)
	}
	if len(recorder.written) != 1 {
		t.Errorf("%d events written, want 1", len(recorder.written))
	}
}

// A FAILED write must not satisfy the guard. Marking the trail before the write
// would let a broken audit path report itself as healthy, which is precisely
// the reassurance the guard exists to withhold.
func TestAFailedAuditWriteDoesNotSatisfyTheGuard(t *testing.T) {
	recorder := &fakeRecorder{err: errors.New("the events table is unreachable")}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A handler that ignores the error, which is the mistake being caught.
		_ = Audit(r.Context(), recorder, nil, audit.Event{Type: audit.EventUserCreated})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})

	_, observer, _ := guarded(t, handler, post("", `{}`))

	if len(observer.missed) != 1 {
		t.Errorf("a failed audit write was accepted as an audit (%d reports)", len(observer.missed))
	}
}

// A refusal changed nothing and owes no record. Demanding one for every 4xx
// would fill the log with attempts — and hand a caller a way to write to an
// append-only table as fast as they can send requests.
func TestARefusedMutationIsNotReported(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusTooManyRequests, http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler := &ran{status: status, response: `{"error":{}}`}

			_, observer, _ := guarded(t, handler.handler(), post("", `{}`))

			if len(observer.missed) != 0 {
				t.Errorf("a %d was reported as an unaudited mutation", status)
			}
		})
	}
}

// A read changes nothing and has nothing to record. Auditing reads here would
// make the log mostly reads, which is how the entries that matter become
// unfindable.
func TestAReadIsNotReported(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			handler := &ran{status: http.StatusOK, response: `{}`}
			r := withCaller(httptest.NewRequest(method, "/v1/users", nil),
				Caller{UserID: "u", ClientID: testClient, OrgID: testOrg})

			_, observer, _ := guarded(t, handler.handler(), r)

			if len(observer.missed) != 0 {
				t.Errorf("a %s was reported as an unaudited mutation", method)
			}
		})
	}
}

// Every mutating method is guarded, not only POST. A DELETE that records
// nothing is the one an incident most wants to find.
func TestEveryMutatingMethodIsGuarded(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			handler := &ran{status: http.StatusOK, response: `{}`}
			r := withCaller(httptest.NewRequest(method, "/v1/users/x", strings.NewReader(`{}`)),
				Caller{UserID: "u", ClientID: testClient, OrgID: testOrg})

			_, observer, _ := guarded(t, handler.handler(), r)

			if len(observer.missed) != 1 {
				t.Errorf("a %s was not guarded", method)
			}
		})
	}
}

// A handler that writes a body without a status is a 200, and must not escape
// the guard by leaving the recorded status at zero.
func TestAnImplicitSuccessIsStillGuarded(t *testing.T) {
	handler := &ran{response: `{"id":"x"}`} // no WriteHeader

	_, observer, _ := guarded(t, handler.handler(), post("", `{}`))

	if len(observer.missed) != 1 {
		t.Errorf("an implicit 200 escaped the guard (%d reports)", len(observer.missed))
	}
}

// --- what the metric is labelled with -------------------------------------------------------

// The ROUTE, never the URL. A label carrying an organization id is an unbounded
// cardinality explosion and a tenant identifier in the monitoring system at the
// same time.
func TestTheMetricLabelCarriesNoIdentifiers(t *testing.T) {
	handler := &ran{status: http.StatusOK, response: `{}`}

	// Served through a chi router, so a route pattern actually exists.
	router := chi.NewRouter()
	g := &AuditGuard{}
	observer := &countingAudit{}
	g.Observer = observer
	router.Method(http.MethodDelete, "/v1/organizations/{org_id}/users/{user_id}",
		g.Wrap(handler.handler()))

	r := withCaller(
		httptest.NewRequest(http.MethodDelete, "/v1/organizations/"+testOrg+"/users/"+testClient, nil),
		Caller{UserID: "u", ClientID: testClient, OrgID: testOrg})
	router.ServeHTTP(httptest.NewRecorder(), r)

	if len(observer.missed) != 1 {
		t.Fatalf("%d reports, want 1", len(observer.missed))
	}
	label := observer.missed[0]
	if strings.Contains(label, testOrg) || strings.Contains(label, testClient) {
		t.Errorf("the metric label carries an identifier: %q", label)
	}
	if !strings.Contains(label, "{org_id}") {
		t.Errorf("label = %q, want the route pattern", label)
	}
}

// --- what an event carries -------------------------------------------------------------------

// The actor, the tenant, the correlation id and the address are filled in from
// the request. An event attributed to nobody cannot answer "who did this", and
// leaving that to thirty call sites guarantees some of them differ.
func TestAnEventIsAttributedToTheCaller(t *testing.T) {
	recorder := &fakeRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Audit(r.Context(), recorder, nil, audit.Event{Type: audit.EventUserCreated})
		w.WriteHeader(http.StatusCreated)
	})

	g := &AuditGuard{ClientIP: func(*http.Request) string { return "203.0.113.7" }}
	r := post("", `{}`)
	r = r.WithContext(observability.WithRequestID(r.Context(), "req-abc"))
	// The scope the request runs in, as Require would have set it.
	r = r.WithContext(context.WithValue(r.Context(), scopeKey{}, scope{orgID: testOrg}))

	g.Wrap(handler).ServeHTTP(httptest.NewRecorder(), r)

	if len(recorder.written) != 1 {
		t.Fatalf("%d events written", len(recorder.written))
	}
	e := recorder.written[0]
	if e.ActorUserID != "u" {
		t.Errorf("ActorUserID = %q", e.ActorUserID)
	}
	if e.OrgID != testOrg {
		t.Errorf("OrgID = %q, want the scoped organization", e.OrgID)
	}
	if e.RequestID != "req-abc" {
		t.Errorf("RequestID = %q", e.RequestID)
	}
	if e.IP != "203.0.113.7" {
		t.Errorf("IP = %q", e.IP)
	}
}

// An INSTANCE_OWNER acting on another tenant writes the event into THAT
// tenant's log. Attributing it to the actor's own organization would hide a
// cross-tenant action from the tenant it was performed on — which is the one
// place it most needs to appear.
func TestACrossTenantActionIsLoggedInTheTargetTenant(t *testing.T) {
	const target = "99999999-9999-9999-9999-999999999999"

	recorder := &fakeRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Audit(r.Context(), recorder, nil, audit.Event{Type: audit.EventUserCreated})
		w.WriteHeader(http.StatusCreated)
	})

	r := post("", `{}`) // caller.OrgID is testOrg
	r = r.WithContext(context.WithValue(r.Context(), scopeKey{},
		scope{orgID: target, instanceScoped: true}))

	(&AuditGuard{}).Wrap(handler).ServeHTTP(httptest.NewRecorder(), r)

	if len(recorder.written) != 1 {
		t.Fatalf("%d events written", len(recorder.written))
	}
	if got := recorder.written[0].OrgID; got != target {
		t.Errorf("OrgID = %q, want the target organization %q", got, target)
	}
}

// A handler that names its own organization keeps it. The defaults fill gaps;
// they do not overrule a deliberate choice.
func TestAHandlersOwnFieldsAreNotOverwritten(t *testing.T) {
	recorder := &fakeRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Audit(r.Context(), recorder, nil, audit.Event{
			Type:        audit.EventUserCreated,
			OrgID:       "chosen-org",
			ActorUserID: "chosen-actor",
		})
		w.WriteHeader(http.StatusOK)
	})

	(&AuditGuard{}).Wrap(handler).ServeHTTP(httptest.NewRecorder(), post("", `{}`))

	e := recorder.written[0]
	if e.OrgID != "chosen-org" || e.ActorUserID != "chosen-actor" {
		t.Errorf("the handler's own fields were overwritten: %+v", e)
	}
}

// Nothing the caller controls reaches an event by default. The guard fills in
// four fields and no more, so a request body cannot smuggle its own attribution
// into the audit log.
func TestNoRequestContentReachesAnEventByDefault(t *testing.T) {
	recorder := &fakeRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Audit(r.Context(), recorder, nil, audit.Event{Type: audit.EventUserCreated})
		w.WriteHeader(http.StatusOK)
	})

	const secret = "correct-horse-battery-staple"
	r := post("smuggled-key", `{"password":"`+secret+`"}`)
	r.Header.Set("X-Forwarded-For", "10.0.0.1")

	(&AuditGuard{}).Wrap(handler).ServeHTTP(httptest.NewRecorder(), r)

	dump, err := json.Marshal(recorder.written[0])
	if err != nil {
		t.Fatalf("marshalling the event: %v", err)
	}
	for _, leak := range []string{secret, "smuggled-key", "10.0.0.1"} {
		if strings.Contains(string(dump), leak) {
			t.Errorf("%q reached the audit event: %s", leak, dump)
		}
	}
}
