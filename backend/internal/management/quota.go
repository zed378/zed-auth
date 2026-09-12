package management

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/zed378/zed-auth/backend/internal/ratelimit"
)

// Per-client rate limiting for /v1 (P1-15 FR-8, closing PG-19).
//
// docs/PLAN/05 Part B: "Rate limits applied per client_id/API key (not just IP),
// with standard X-RateLimit-* headers." Both halves matter — the bound, and
// telling the client about it. A limit a well-behaved client cannot see is a
// limit it can only discover by breaking.
//
// 429 here and deliberately NOT on the login page: this caller is a machine
// that acts on a status code and reads Retry-After, which is exactly what
// P1-13 argued a browser is not.

// Counter is the quota seam.
//
// An interface so the middleware's own behaviour — the headers, the refusal,
// the ordering against the handler — is testable without Redis. What the
// counter itself does under concurrency is proved against a real Redis in
// internal/ratelimit.
type Counter interface {
	Consume(ctx context.Context, clientID string, now time.Time) ratelimit.Verdict
}

// RateLimit refuses a client that is over its bound.
type RateLimit struct {
	Counter Counter

	// HotCounter bounds the routes in HotRoutes, which are called at a rate
	// the Management API's own bound was never sized for (P2-17).
	//
	// Optional: nil means every route uses Counter, which is what every
	// deployment did before Phase 2 had an endpoint on a consumer's hot path.
	HotCounter Counter

	Now func() time.Time
}

// HotRoutes are the /v1 routes a consumer calls per-request rather than
// per-administrator-action.
//
// A table rather than a flag on each route, for `Policy`'s reason: the whole
// set is readable in one screen, and "which endpoints get the larger
// allowance" is a question somebody will ask during a review.
//
// One entry, and it should stay short. A route in here is a route whose bound
// is loose enough to be worth justifying individually — see
// `ratelimit.PerClientAuthz` for this one's justification.
var HotRoutes = map[string]bool{
	"POST /v1/authz/check": true,
}

func (rl *RateLimit) now() time.Time {
	if rl.Now != nil {
		return rl.Now()
	}
	return time.Now()
}

// Wrap counts the request and either refuses it or lets it through.
//
// Registered INSIDE Require and OUTSIDE Idempotency, and the order is the
// design:
//
//   - After authentication, because the bound is per client and there is no
//     client before the token is read.
//   - Before idempotency, because a replay is still a request. A client
//     hammering one key would otherwise be served from the record for free,
//     and "free" is the property a limit exists to remove.
func (rl *RateLimit) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := CallerFrom(r.Context())
		if !ok {
			// Registered outside Require. Refused rather than run unbounded:
			// a mutating handler reached without authentication is the worse
			// of the two failures.
			WriteError(w, Fault{Class: Internal, Message: "An unexpected error occurred."})
			return
		}

		counter := rl.Counter
		if rl.HotCounter != nil {
			if rc := chi.RouteContext(r.Context()); rc != nil &&
				HotRoutes[PolicyKey(r.Method, rc.RoutePattern())] {
				counter = rl.HotCounter
			}
		}

		verdict := counter.Consume(r.Context(), caller.ClientID, rl.now())

		// On EVERY response, not only on a refusal. A client that learns its
		// remaining allowance only once it has run out cannot pace itself.
		writeQuotaHeaders(w.Header(), verdict)

		if !verdict.Allowed {
			WriteError(w, Fault{
				Class:      RateLimited,
				Message:    "Too many requests. Please retry later.",
				RetryAfter: verdict.RetryAfter,
				Reason:     "over the per-client request quota",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeQuotaHeaders renders a verdict.
//
// Set before the handler runs, because a handler that has already called
// WriteHeader has sent the headers and anything added afterwards is silently
// dropped — which would leave the limit invisible on exactly the successful
// responses a client paces itself against.
func writeQuotaHeaders(h http.Header, v ratelimit.Verdict) {
	h.Set("X-RateLimit-Limit", strconv.Itoa(v.Limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(v.Remaining))
	// Seconds since the epoch, which is the form every widely-used API uses
	// for this header and the one client libraries parse.
	h.Set("X-RateLimit-Reset", strconv.FormatInt(v.Reset.Unix(), 10))
}
