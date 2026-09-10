package management

import (
	"net/http"
)

// The /v1 middleware chain, composed once.
//
// The ORDER below is a set of security decisions, not a style choice, so it
// lives here rather than at each mount site. Thirty endpoints assembling their
// own chain would be thirty chances to put the audit guard outside the
// idempotency middleware, and the symptom of that is a false alarm rather than
// a hole — which is worse, because a metric that cries wolf is a metric that
// gets muted.

// Chain carries the four /v1 middlewares.
//
// Each is optional. A nil member is skipped rather than panicking, so a test —
// or a deployment without Redis — can assemble a partial chain deliberately.
// Requirements are the exception: Handle always applies one, and the zero value
// is unsatisfiable, so a route registered without a declared permission is
// unreachable rather than open.
type Chain struct {
	Auth        *Middleware
	RateLimit   *RateLimit
	Idempotency *Idempotency
	Audit       *AuditGuard

	// BufferBody makes the raw request body available to handlers that need
	// the keys a caller actually sent rather than the ones the decoder kept.
	// Off by default so a test can leave it out deliberately; main.go turns it
	// on for every /v1 route.
	BufferBody bool
}

// Handle wraps h with everything a /v1 route needs.
//
// Outermost to innermost:
//
//	Require -> RateLimit -> Idempotency -> AuditGuard -> handler
//
// Require is FIRST, because everything after it is keyed on the caller: the
// quota is per client, the idempotency record is per (org, client), and an
// audit event names an actor. None of those exist before the token is read,
// and doing any of them for an unauthenticated request would mean writing rows
// on behalf of nobody.
//
// RateLimit comes before Idempotency, because a replay is still a request. A
// client hammering one key would otherwise be served from the record for free,
// and "free" is exactly the property a bound exists to remove.
//
// AuditGuard sits INSIDE Idempotency, and this is the one that is easy to get
// backwards. A replay answers 201 without running the handler and therefore
// without writing an event — entirely correct, since the original request
// already wrote one. With the guard outside, every replay would be reported as
// an unaudited mutation, and an alarm that fires in normal operation is an
// alarm somebody turns off within a week.
func (c *Chain) Handle(req Requirement, h http.Handler) http.Handler {
	if c.Audit != nil {
		h = c.Audit.Wrap(h)
	}
	if c.Idempotency != nil {
		h = c.Idempotency.Wrap(h)
	}
	if c.BufferBody {
		// Inside RateLimit and outside Idempotency: a refused request must not
		// cost a body read, and the idempotency hash must cover the same bytes
		// the handler will inspect.
		h = BufferBody(h)
	}
	if c.RateLimit != nil {
		h = c.RateLimit.Wrap(h)
	}
	if c.Auth != nil {
		h = c.Auth.Require(req, h)
	}
	return h
}

// HandleFunc is Handle for a bare function.
func (c *Chain) HandleFunc(req Requirement, h http.HandlerFunc) http.Handler {
	return c.Handle(req, h)
}

// Guarded wraps h with the chain, taking each route's Requirement from Policy.
//
// The counterpart to Handle for routes registered by the generated router,
// where every operation shares one middleware list and the route pattern is the
// only thing that distinguishes them.
func (c *Chain) Guarded(h http.Handler) http.Handler {
	if c.Audit != nil {
		h = c.Audit.Wrap(h)
	}
	if c.Idempotency != nil {
		h = c.Idempotency.Wrap(h)
	}
	if c.BufferBody {
		h = BufferBody(h)
	}
	if c.RateLimit != nil {
		h = c.RateLimit.Wrap(h)
	}
	if c.Auth != nil {
		h = c.Auth.Guard(h)
	}
	return h
}
