package management

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/observability"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Auditing a mutating request (P1-15 FR-9).
//
// Two halves, and the second is the one that matters.
//
// The first is Audit: a helper that writes an event inside the request's OWN
// transaction, so the change and its record commit together. An audit entry for
// a change that rolled back is a lie; a change with no entry is worse.
//
// The second is Guard: a check that a successful mutating request actually
// wrote one. docs/PLAN/09 § Audit requires every security-sensitive action to
// leave a record, and the way that requirement fails in practice is not a
// broken writer — it is one handler out of thirty that nobody remembered. A
// missing entry is invisible by nature: nothing errors, nothing is slow, and
// the gap is found during an incident, when the record is needed and absent.
// Guard turns that into a loud line and a metric on the very first request.

// Recorder writes an audit event.
type Recorder interface {
	Write(ctx context.Context, tx *postgres.Tx, e audit.Event) error
}

// AuditObserver counts what the guard finds.
type AuditObserver interface {
	// MutationNotAudited counts a successful mutating request that wrote no
	// audit event, by route. This is the metric to alert on: it should be
	// permanently zero, so any value at all is a defect.
	MutationNotAudited(route string)
}

// trail is the per-request note that an event was written.
type trail struct{ written bool }

type trailKey struct{}

// Audit writes an event for the request in ctx, inside tx.
//
// The actor, organization, IP and correlation id are filled in from the request
// rather than passed by the handler. Not for brevity: an event attributed to
// nobody is an event that cannot answer "who did this", and leaving that to
// thirty call sites guarantees some of them differ.
//
// A handler may pass its own OrgID — an INSTANCE_OWNER acting on another
// tenant's data must have the event land in THAT tenant's log, not their own.
func Audit(ctx context.Context, recorder Recorder, tx *postgres.Tx, e audit.Event) error {
	if caller, ok := CallerFrom(ctx); ok {
		if e.ActorUserID == "" {
			e.ActorUserID = caller.UserID
		}
		if e.OrgID == "" {
			// The organization the request ADDRESSES, which for an
			// INSTANCE_OWNER is not their own.
			e.OrgID, _ = ScopeFrom(ctx)
		}
	}
	if e.RequestID == "" {
		e.RequestID = observability.RequestIDFromContext(ctx)
	}
	if e.IP == "" {
		e.IP = ipFrom(ctx)
	}

	if err := recorder.Write(ctx, tx, e); err != nil {
		return err
	}

	// Marked only AFTER a successful write. Marking first would let a failed
	// write satisfy the guard, which is precisely the reassurance the guard
	// exists to withhold.
	if t, ok := ctx.Value(trailKey{}).(*trail); ok {
		t.written = true
	}
	return nil
}

// ipKey carries the client address for the audit record.
type ipKey struct{}

// WithClientIP records the address a request came from, for events written
// during it. Set by the guard from what the trusted-proxy logic already
// resolved, so no handler re-derives it — and re-deriving it is how a
// spoofable header ends up in an audit log (P1-13, BL-05).
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

func ipFrom(ctx context.Context) string {
	ip, _ := ctx.Value(ipKey{}).(string)
	return ip
}

// AuditGuard reports a mutating request that changed something and recorded
// nothing.
type AuditGuard struct {
	Log      *slog.Logger
	Observer AuditObserver

	// ClientIP resolves the request's address. Optional; when nil, events
	// carry no IP rather than a spoofable one.
	ClientIP func(*http.Request) string
}

// Wrap installs the trail and checks it afterwards.
//
// Registered inside Require, so a refused request — which changed nothing — is
// never reported as an unaudited mutation.
func (g *AuditGuard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mutating(r.Method) {
			// A GET changes nothing and has nothing to record. Auditing reads
			// is a separate, deliberate capability — and doing it here would
			// make the audit log mostly reads, which is how the entries that
			// matter become unfindable.
			next.ServeHTTP(w, r)
			return
		}

		t := &trail{}
		ctx := context.WithValue(r.Context(), trailKey{}, t)
		if g.ClientIP != nil {
			ctx = WithClientIP(ctx, g.ClientIP(r))
		}

		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(ctx))

		// Only a SUCCESS. A 400 or a 403 changed nothing, and demanding an
		// event for every refusal would fill the log with attempts and hand a
		// caller a way to write to it as fast as they can send requests —
		// which is the reason P1-13 audits a cooldown rather than each attempt.
		if !success(recorder.status) || t.written {
			return
		}

		route := r.Method + " " + routePattern(r)
		if g.Observer != nil {
			g.Observer.MutationNotAudited(route)
		}
		if g.Log != nil {
			g.Log.Error("a mutating request succeeded without writing an audit event",
				"route", route, "status", recorder.status,
				"request_id", observability.RequestIDFromContext(r.Context()))
		}
	})
}

// routePattern names the ROUTE, not the URL.
//
// "/v1/organizations/{org_id}/users", never "/v1/organizations/8f3.../users".
// The value becomes a metric label, and a label carrying an organization id is
// an unbounded cardinality explosion and a tenant identifier in the monitoring
// system at the same time.
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unknown"
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func success(status int) bool { return status >= 200 && status <= 299 }

// statusWriter remembers what status went out.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	if sw.status == 0 {
		sw.status = status
	}
	sw.ResponseWriter.WriteHeader(status)
}

func (sw *statusWriter) Write(p []byte) (int, error) {
	if sw.status == 0 {
		// net/http's implicit 200, recorded here too — a handler that writes a
		// body without a status is a success and must not escape the guard by
		// leaving it at zero.
		sw.status = http.StatusOK
	}
	return sw.ResponseWriter.Write(p)
}
