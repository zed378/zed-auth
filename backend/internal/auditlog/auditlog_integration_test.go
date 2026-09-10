//go:build integration

// The audit log read, end to end (P1-20).
//
// Two claims are worth a container. The first is that no filter combination
// can reach across tenants — asserted by trying every combination against a
// neighbour's data rather than by reading the query. The second is that this
// resource has exactly one operation, which is checked against what the ROUTER
// registers rather than against what this package happens to implement.
package auditlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/config"
	"github.com/zed378/zed-auth/backend/internal/httpserver"
	"github.com/zed378/zed-auth/backend/internal/management"
	"github.com/zed378/zed-auth/backend/internal/oauth/token"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

const issuer = "https://auth.example.test"

func TestMain(m *testing.M) { os.Exit(testsupport.StartForPackage(m)) }

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	db      *postgres.DB
	factory *testsupport.Factory
	handler http.Handler
	signer  *signing.Signer

	orgA, orgB string
	userID     string
	otherUser  string
	clientID   string
}

func setup(t *testing.T) *fixture {
	t.Helper()

	stack := testsupport.Start(t)
	testsupport.Truncate(t, stack)

	factory := testsupport.NewFactory(t, stack)
	instance := factory.Instance()
	orgA := factory.Organization(instance)
	orgB := factory.Organization(instance)
	userID := factory.User(orgA)
	otherUser := factory.User(orgA)

	var project, clientID string
	factory.QueryRow(&project,
		`INSERT INTO projects (org_id, name) VALUES ($1, 'console') RETURNING id`, orgA)
	factory.QueryRow(&clientID,
		`INSERT INTO applications (project_id, org_id, name, type)
		 VALUES ($1, $2, 'console', 'web') RETURNING id`, project, orgA)

	db, err := postgres.Open(context.Background(), config.PostgresConfig{
		DSN: stack.AppDSN, MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: time.Minute,
	}, discard())
	if err != nil {
		t.Fatalf("opening the app connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keys := signingKeys(t, stack)
	rdb := redis.NewClient(&redis.Options{Addr: stack.RedisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flushing redis: %v", err)
	}

	chain := &management.Chain{
		Auth: &management.Middleware{
			Issuer: issuer, Verifier: signing.NewVerifier(keys),
			Grants: management.NewRoleStore(), DB: db, Log: discard(),
		},
		RateLimit: &management.RateLimit{
			Counter: ratelimit.NewQuotas(rdb, nil, discard()).
				WithQuota(ratelimit.Quota{Limit: 500, Window: time.Minute}),
		},
		Idempotency: &management.Idempotency{Claims: management.NewDBClaims(db), Log: discard()},
		Audit:       &management.AuditGuard{Log: discard()},
		BufferBody:  true,
	}

	srv := httpserver.New(config.HTTPConfig{
		Addr: "127.0.0.1:0", ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second,
	}, httpserver.Deps{
		Logger:         discard(),
		Health:         &httpserver.Health{},
		V1:             chain,
		Organizations:  stubManager{},
		ProjectAPI:     stubProjects{},
		ApplicationAPI: stubApplications{},
		UserAPI:        stubUsers{},
		AuditAPI:       &Handler{DB: db, Log: discard()},
	})

	return &fixture{
		db: db, factory: factory, handler: srv.Handler(),
		signer: signing.NewSigner(keys),
		orgA:   orgA, orgB: orgB, userID: userID, otherUser: otherUser, clientID: clientID,
	}
}

func signingKeys(t *testing.T, stack *testsupport.Stack) *signing.Cache {
	t.Helper()

	pair, err := signing.Generate(signing.RS256)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	path := filepath.Join(t.TempDir(), pair.KID+".pem")
	if err := os.WriteFile(path, []byte(pair.PrivatePEM), 0o400); err != nil {
		t.Fatalf("writing the key: %v", err)
	}

	owner, err := sql.Open("pgx", stack.OwnerDSN)
	if err != nil {
		t.Fatalf("owner connection: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	store := signing.NewStore(owner, config.NewSecretResolver(false), signing.PurposeOIDC)
	if err := store.Insert(context.Background(), pair, "file:"+path); err != nil {
		t.Fatalf("inserting the key: %v", err)
	}
	if _, err := store.Rotate(context.Background()); err != nil {
		t.Fatalf("rotating: %v", err)
	}
	return signing.NewCache(func() (*signing.KeySet, error) {
		return store.Load(context.Background())
	}, time.Minute)
}

func (f *fixture) token(t *testing.T) string {
	t.Helper()

	claims, err := token.AccessTokenClaims(token.Subject{
		Issuer: issuer, Audience: issuer, ClientID: f.clientID,
		OrgID: f.orgA, UserID: f.userID, Scope: []string{"openid"},
	}, time.Now())
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	payload, err := claims.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	signed, err := f.signer.SignWithType(payload, signing.TypeAccessToken)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func (f *fixture) grant(role management.Role, scopeID string) {
	f.factory.Exec(`INSERT INTO manager_roles (user_id, role, scope_id) VALUES ($1, $2, $3)`,
		f.userID, string(role), scopeID)
}

func (f *fixture) call(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+f.token(t))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *fixture) events(orgID string) string {
	return "/v1/organizations/" + orgID + "/events"
}

// write puts an event in the log, as the writer would.
func (f *fixture) write(t *testing.T, orgID, kind, actor string, at time.Time) {
	t.Helper()

	var actorValue any
	if actor != "" {
		actorValue = actor
	}
	f.factory.Exec(`
		INSERT INTO events (org_id, actor_user_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5)`,
		orgID, actorValue, kind, fmt.Sprintf(`{"note":%q}`, kind), at)
}

func mustStatus(t *testing.T, w *httptest.ResponseRecorder, want int) *httptest.ResponseRecorder {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
	}
	return w
}

func decodeList(t *testing.T, w *httptest.ResponseRecorder) api.EventList {
	t.Helper()
	var list api.EventList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("not an EventList: %s", w.Body.String())
	}
	return list
}

// --- no mutating operation exists (the card's third DoD item) --------------------------------

// The claim that the ROUTER serves exactly one operation under this resource
// lives in internal/httpserver, where the mux is reachable — see
// TestTheAuditLogServesExactlyOneOperation. It belongs there rather than here:
// it is a statement about what a caller meets, not about what this package
// implements, and "we did not write a delete handler" is a statement about
// intent.

// And the same over the wire: every other method is refused.
func TestEveryWriteMethodIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)
	f.write(t, f.orgA, "user.login.success", f.userID, time.Now())

	// The control first: the read works, so the refusals below are about the
	// method rather than about the route being absent.
	mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA), ""), http.StatusOK)

	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	} {
		w := f.call(t, method, f.events(f.orgA), `{"event_type":"forged.event"}`)
		if w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 405 or 404: %s", method, w.Code, w.Body.String())
		}
	}

	// Nothing was written by any of them.
	var count int
	f.factory.QueryRow(&count,
		`SELECT count(*) FROM events WHERE event_type = 'forged.event'`)
	if count != 0 {
		t.Errorf("%d forged events exist", count)
	}
}

// The application role cannot rewrite history even directly. That is P0-07's
// guarantee rather than this task's, and it is what makes the absence of a
// write endpoint meaningful rather than decorative.
func TestTheApplicationRoleCannotAlterTheLog(t *testing.T) {
	f := setup(t)
	f.write(t, f.orgA, "user.login.success", f.userID, time.Now())

	err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE events SET event_type = 'rewritten' WHERE org_id = $1`, f.orgA)
		return err
	})
	if err == nil {
		t.Error("the application role updated the audit log")
	}

	err = f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(), `DELETE FROM events WHERE org_id = $1`, f.orgA)
		return err
	})
	if err == nil {
		t.Error("the application role deleted from the audit log")
	}
}

// --- the tenant boundary, across every filter combination ------------------------------------

// **The card's third step**, and the reason it is worth a loop rather than one
// case: "no filter combination can reach across tenants" is a claim about the
// combinations nobody thought of, so the test enumerates them.
func TestNoFilterCombinationReachesAnotherTenant(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	now := time.Now()
	// A neighbour's log, deliberately sharing every attribute we might filter
	// on: the same event type, the same actor id, the same instant.
	f.write(t, f.orgB, "user.login.success", f.otherUser, now)
	f.write(t, f.orgB, "organization.updated", "", now)

	// One of ours, so an empty page cannot pass for a working filter.
	f.write(t, f.orgA, "user.login.success", f.userID, now)

	from := now.Add(-time.Hour).UTC().Format(time.RFC3339)
	to := now.Add(time.Hour).UTC().Format(time.RFC3339)

	for _, query := range []string{
		"",
		"?event_type=user.login.success",
		"?event_type=organization.updated",
		"?event_type=user.login.success&event_type=organization.updated",
		"?actor_id=" + f.otherUser,
		"?actor_id=" + f.userID,
		"?from=" + url.QueryEscape(from),
		"?to=" + url.QueryEscape(to),
		"?from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to),
		"?event_type=user.login.success&actor_id=" + f.otherUser +
			"&from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to),
		"?page_size=100",
	} {
		w := mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA)+query, ""), http.StatusOK)
		list := decodeList(t, w)

		for _, e := range list.Events {
			// Every event in our organization was written by us in this test,
			// and the only actor we ever used is f.userID. An entry naming the
			// neighbour's actor could only have come from their tenant.
			if actor, err := e.ActorUserId.Get(); err == nil && actor.String() == f.otherUser {
				t.Errorf("%s returned an event from another tenant", query)
			}
		}
	}

	// The control: the unfiltered read DOES return our own event, so the
	// absences above are not an endpoint that returns nothing.
	w := mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA), ""), http.StatusOK)
	if len(decodeList(t, w).Events) == 0 {
		t.Fatal("the unfiltered read returned nothing, so this test proves nothing")
	}
}

// --- filtering ---------------------------------------------------------------------------------

func TestFilteringByEventType(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	now := time.Now()
	f.write(t, f.orgA, "user.login.success", f.userID, now)
	f.write(t, f.orgA, "user.login.failed", "", now.Add(-time.Minute))
	f.write(t, f.orgA, "organization.updated", f.userID, now.Add(-2*time.Minute))

	one := decodeList(t, mustStatus(t, f.call(t, http.MethodGet,
		f.events(f.orgA)+"?event_type=user.login.failed", ""), http.StatusOK))
	if len(one.Events) != 1 || one.Events[0].EventType != "user.login.failed" {
		t.Errorf("single-type filter returned %d events: %+v", len(one.Events), one.Events)
	}

	two := decodeList(t, mustStatus(t, f.call(t, http.MethodGet,
		f.events(f.orgA)+"?event_type=user.login.failed&event_type=organization.updated", ""),
		http.StatusOK))
	if len(two.Events) != 2 {
		t.Errorf("two-type filter returned %d events, want 2", len(two.Events))
	}
	for _, e := range two.Events {
		if e.EventType == "user.login.success" {
			t.Error("the filter admitted a type it did not name")
		}
	}
}

func TestFilteringByActor(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	now := time.Now()
	f.write(t, f.orgA, "user.login.success", f.userID, now)
	f.write(t, f.orgA, "user.login.success", f.otherUser, now.Add(-time.Minute))
	// No actor: a failed login against an address that does not exist.
	f.write(t, f.orgA, "user.login.failed", "", now.Add(-2*time.Minute))

	list := decodeList(t, mustStatus(t, f.call(t, http.MethodGet,
		f.events(f.orgA)+"?actor_id="+f.userID, ""), http.StatusOK))

	if len(list.Events) != 1 {
		t.Fatalf("%d events for one actor, want 1", len(list.Events))
	}
	actor, err := list.Events[0].ActorUserId.Get()
	if err != nil || actor.String() != f.userID {
		t.Errorf("actor = %v (%v)", actor, err)
	}

	// An actorless event is reachable without the filter, and matches no
	// value of it — which is correct, because it has none.
	all := decodeList(t, mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA), ""),
		http.StatusOK))
	found := false
	for _, e := range all.Events {
		if e.EventType == "user.login.failed" {
			found = true
			if _, err := e.ActorUserId.Get(); err == nil {
				t.Error("an actorless event reports an actor")
			}
		}
	}
	if !found {
		t.Error("the actorless event is not readable at all")
	}
}

func TestFilteringByTimeRange(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	base := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	f.write(t, f.orgA, "a.old", f.userID, base)
	f.write(t, f.orgA, "b.middle", f.userID, base.Add(time.Hour))
	f.write(t, f.orgA, "c.new", f.userID, base.Add(2*time.Hour))

	// `from` is inclusive and `to` is exclusive, so a window on the middle
	// event's own instant returns exactly it.
	from := base.Add(time.Hour).UTC().Format(time.RFC3339Nano)
	to := base.Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)

	list := decodeList(t, mustStatus(t, f.call(t, http.MethodGet,
		f.events(f.orgA)+"?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to), ""),
		http.StatusOK))

	if len(list.Events) != 1 || list.Events[0].EventType != "b.middle" {
		t.Errorf("the window returned %d events: %+v", len(list.Events), list.Events)
	}
}

func TestAnEmptyTimeRangeIsRefused(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	now := time.Now().UTC().Format(time.RFC3339)
	w := f.call(t, http.MethodGet,
		f.events(f.orgA)+"?from="+url.QueryEscape(now)+"&to="+url.QueryEscape(now), "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — an empty range answered as an empty page reads as 'nothing happened': %s",
			w.Code, w.Body.String())
	}
}

// --- ordering and pagination ---------------------------------------------------------------------

// Newest first, unlike every other list in this API.
func TestTheLogIsOrderedNewestFirst(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := range 5 {
		f.write(t, f.orgA, fmt.Sprintf("event.%02d", i), f.userID, base.Add(time.Duration(i)*time.Minute))
	}

	list := decodeList(t, mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA), ""),
		http.StatusOK))
	if len(list.Events) != 5 {
		t.Fatalf("%d events, want 5", len(list.Events))
	}

	for i := 1; i < len(list.Events); i++ {
		if list.Events[i].OccurredAt.After(list.Events[i-1].OccurredAt) {
			t.Errorf("event %d is newer than the one before it — the order is not descending", i)
		}
	}
	if list.Events[0].EventType != "event.04" {
		t.Errorf("the first event is %q, want the newest", list.Events[0].EventType)
	}
}

func TestWalkingTheLogSeesEachEntryOnce(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := range 11 {
		f.write(t, f.orgA, fmt.Sprintf("walk.%02d", i), f.userID, base.Add(time.Duration(i)*time.Second))
	}

	seen := map[string]int{}
	target := f.events(f.orgA) + "?page_size=4"

	for range 10 {
		list := decodeList(t, mustStatus(t, f.call(t, http.MethodGet, target, ""), http.StatusOK))
		for _, e := range list.Events {
			seen[e.Id]++
		}
		if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
			break
		}
		target = f.events(f.orgA) + "?page_size=4&page_token=" +
			url.QueryEscape(*list.PageInfo.NextPageToken)
	}

	if len(seen) != 11 {
		t.Errorf("saw %d distinct events, want 11", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times", id, n)
		}
	}
}

// Two events at the SAME instant still page correctly, because the cursor
// carries the id as well as the timestamp.
func TestEventsAtTheSameInstantPageWithoutRepeating(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	same := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := range 6 {
		f.write(t, f.orgA, fmt.Sprintf("tie.%02d", i), f.userID, same)
	}

	seen := map[string]int{}
	target := f.events(f.orgA) + "?page_size=2"

	for range 6 {
		list := decodeList(t, mustStatus(t, f.call(t, http.MethodGet, target, ""), http.StatusOK))
		for _, e := range list.Events {
			seen[e.Id]++
		}
		if list.PageInfo == nil || list.PageInfo.NextPageToken == nil {
			break
		}
		target = f.events(f.orgA) + "?page_size=2&page_token=" +
			url.QueryEscape(*list.PageInfo.NextPageToken)
	}

	if len(seen) != 6 {
		t.Errorf("saw %d of 6 events with identical timestamps", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times across pages", id, n)
		}
	}
}

// --- permissions ---------------------------------------------------------------------------------

// **The card's second DoD item.** The audit log records who did what, and is
// not general-readable.
func TestANonAdminIsRefused(t *testing.T) {
	f := setup(t)
	f.write(t, f.orgA, "user.login.success", f.userID, time.Now())

	// No role at all.
	w := f.call(t, http.MethodGet, f.events(f.orgA), "")
	if w.Code == http.StatusOK {
		t.Fatalf("a caller with no role read the audit log: %s", w.Body.String())
	}

	// PROJECT_OWNER is reserved and grants nothing (P1-17).
	f.grant(management.ProjectOwner, f.orgA)
	if w := f.call(t, http.MethodGet, f.events(f.orgA), ""); w.Code == http.StatusOK {
		t.Error("PROJECT_OWNER read the audit log")
	}

	// The control.
	f.grant(management.OrgAdmin, f.orgA)
	mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA), ""), http.StatusOK)
}

// --- the payload -----------------------------------------------------------------------------------

func TestThePayloadIsReturnedAsData(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)
	f.write(t, f.orgA, "organization.updated", f.userID, time.Now())

	list := decodeList(t, mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA), ""),
		http.StatusOK))
	if len(list.Events) != 1 {
		t.Fatalf("%d events", len(list.Events))
	}
	if list.Events[0].Payload == nil {
		t.Fatal("no payload")
	}
	if (*list.Events[0].Payload)["note"] != "organization.updated" {
		t.Errorf("payload = %+v", *list.Events[0].Payload)
	}
}

// An unreadable payload cannot exist, and the code tolerates one anyway.
//
// The first version of this test tried to store `"not an object"` and the
// database refused it: `events_payload_object` is a CHECK, so a payload that
// is valid JSON but not an object never reaches a reader. That is a better
// answer than the one the test was written to expect.
//
// `decodePayload` still returns nil rather than failing, and that stays —
// defence in depth for a state the schema prevents costs nothing, and an
// investigator reading a page should not lose it to one row's detail. What
// this test now asserts is the guarantee that makes the fallback unreachable.
func TestAPayloadThatIsNotAnObjectIsRefusedByTheDatabase(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	err := f.db.WithTenant(context.Background(), f.orgA, func(tx *postgres.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO events (org_id, event_type, payload)
			VALUES ($1, 'odd.event', '"not an object"'::jsonb)`, f.orgA)
		return err
	})
	if err == nil {
		t.Fatal("a non-object payload was stored")
	}
	if !strings.Contains(err.Error(), "events_payload_object") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	// And the fallback itself, as a unit: nil rather than a panic.
	for _, raw := range []string{"", "null", `"a string"`, "not json at all", "[1,2]"} {
		if got := decodePayload(raw); got != nil {
			t.Errorf("decodePayload(%q) = %v, want nil", raw, got)
		}
	}
	if got := decodePayload(`{"a":1}`); got == nil {
		t.Error("decodePayload rejected a real object")
	}
}

// --- the latency budget ------------------------------------------------------------------------------

// **The card's fourth DoD item**, measured rather than asserted.
//
// docs/PLAN/12 gives the Management API p50 < 100ms. This writes five thousand
// events and times a filtered read — a real number on real hardware, and one
// that fails loudly if the query stops using events_org_created_at_idx.
func TestAFilteredReadOverALargeLogStaysWithinTheBudget(t *testing.T) {
	f := setup(t)
	f.grant(management.OrgAdmin, f.orgA)

	// Inside the current month. `events` is partitioned by `created_at` and
	// P0-12's maintenance creates partitions three months ahead, not behind —
	// a backdated bulk insert has nowhere to land, which the first version of
	// this test discovered.
	base := time.Now().Add(-6 * time.Hour)
	f.factory.Exec(`
		INSERT INTO events (org_id, actor_user_id, event_type, payload, created_at)
		SELECT $1, $2, 'bulk.event', '{"n":1}'::jsonb, $3::timestamptz + (n || ' milliseconds')::interval
		  FROM generate_series(1, 5000) AS n`, f.orgA, f.userID, base)

	// Warm the plan and the buffers first; the first query of a connection
	// pays for parsing and a cold cache, and neither is what the budget is
	// about.
	mustStatus(t, f.call(t, http.MethodGet, f.events(f.orgA)+"?page_size=50", ""), http.StatusOK)

	const rounds = 5
	start := time.Now()
	for range rounds {
		mustStatus(t, f.call(t, http.MethodGet,
			f.events(f.orgA)+"?event_type=bulk.event&page_size=50", ""), http.StatusOK)
	}
	each := time.Since(start) / rounds

	// The budget is 100ms at p50. The bound here is deliberately looser,
	// because this runs inside a container on a developer machine alongside
	// whatever else is running — what it catches is a sequential scan, which
	// on five thousand rows is orders of magnitude away rather than a few
	// milliseconds.
	if each > 300*time.Millisecond {
		t.Errorf("a filtered read over 5000 events took %v, which is not index-backed", each)
	}
	t.Logf("filtered read over 5000 events: %v each", each)
}

// --- stubs ---------------------------------------------------------------------------------------------

// The other four halves of the Management API. This package exercises none of
// them, and httpserver.New refuses a /v1 chain with any of them missing.
var errNotWired = fmt.Errorf("this harness does not wire that half of the Management API")

type stubManager struct{}

func (stubManager) ListOrganizations(context.Context, api.ListOrganizationsRequestObject) (api.ListOrganizationsResponseObject, error) {
	return nil, errNotWired
}
func (stubManager) CreateOrganization(context.Context, api.CreateOrganizationRequestObject) (api.CreateOrganizationResponseObject, error) {
	return nil, errNotWired
}
func (stubManager) GetOrganization(context.Context, api.GetOrganizationRequestObject) (api.GetOrganizationResponseObject, error) {
	return nil, errNotWired
}
func (stubManager) UpdateOrganization(context.Context, api.UpdateOrganizationRequestObject) (api.UpdateOrganizationResponseObject, error) {
	return nil, errNotWired
}
func (stubManager) DeleteOrganization(context.Context, api.DeleteOrganizationRequestObject) (api.DeleteOrganizationResponseObject, error) {
	return nil, errNotWired
}

type stubProjects struct{}

func (stubProjects) ListProjects(context.Context, api.ListProjectsRequestObject) (api.ListProjectsResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) CreateProject(context.Context, api.CreateProjectRequestObject) (api.CreateProjectResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) GetProject(context.Context, api.GetProjectRequestObject) (api.GetProjectResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) UpdateProject(context.Context, api.UpdateProjectRequestObject) (api.UpdateProjectResponseObject, error) {
	return nil, errNotWired
}
func (stubProjects) DeleteProject(context.Context, api.DeleteProjectRequestObject) (api.DeleteProjectResponseObject, error) {
	return nil, errNotWired
}

type stubApplications struct{}

func (stubApplications) ListApplications(context.Context, api.ListApplicationsRequestObject) (api.ListApplicationsResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) CreateApplication(context.Context, api.CreateApplicationRequestObject) (api.CreateApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) GetApplication(context.Context, api.GetApplicationRequestObject) (api.GetApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) UpdateApplication(context.Context, api.UpdateApplicationRequestObject) (api.UpdateApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) DeleteApplication(context.Context, api.DeleteApplicationRequestObject) (api.DeleteApplicationResponseObject, error) {
	return nil, errNotWired
}
func (stubApplications) RotateApplicationSecret(context.Context, api.RotateApplicationSecretRequestObject) (api.RotateApplicationSecretResponseObject, error) {
	return nil, errNotWired
}

type stubUsers struct{}

func (stubUsers) ListUsers(context.Context, api.ListUsersRequestObject) (api.ListUsersResponseObject, error) {
	return nil, errNotWired
}
func (stubUsers) CreateUser(context.Context, api.CreateUserRequestObject) (api.CreateUserResponseObject, error) {
	return nil, errNotWired
}
func (stubUsers) GetUser(context.Context, api.GetUserRequestObject) (api.GetUserResponseObject, error) {
	return nil, errNotWired
}
func (stubUsers) UpdateUser(context.Context, api.UpdateUserRequestObject) (api.UpdateUserResponseObject, error) {
	return nil, errNotWired
}
func (stubUsers) DeactivateUser(context.Context, api.DeactivateUserRequestObject) (api.DeactivateUserResponseObject, error) {
	return nil, errNotWired
}
func (stubUsers) ReactivateUser(context.Context, api.ReactivateUserRequestObject) (api.ReactivateUserResponseObject, error) {
	return nil, errNotWired
}
func (stubUsers) ResetUserPassword(context.Context, api.ResetUserPasswordRequestObject) (api.ResetUserPasswordResponseObject, error) {
	return nil, errNotWired
}
