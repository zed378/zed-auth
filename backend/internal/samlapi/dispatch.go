package samlapi

import (
	"context"
	"net/http"

	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/session"
)

// One seam, two protocols (P4-08 F-3).
//
// `internal/login` finishes a sign-in and hands the request id back to whoever
// started it, through a single interface. Until SAML there was one
// implementation and the interface could name it.
//
// The temptation is a second seam and an `if` in the login handler. It is the
// wrong answer twice over: the login page would have to learn which protocols
// exist, and every future protocol would edit the most security-critical path
// in the service. A branch added there is a branch that has to be right for
// password, for the factor step, for a refresh, for the forced-enrolment
// detour, and for whatever Phase 5 adds.
//
// So the routing lives here instead. The request id carries a namespace, this
// dispatcher reads it, and `internal/login` keeps asking one thing one way — it
// is not edited at all.

// Dispatcher routes a login resume to the protocol that started it.
//
// It implements `login.Authorization`, which is why its methods have exactly
// that shape. The interface is not imported: doing so would make
// `internal/login` and this package mutually dependent, and the compiler checks
// the shape at the wiring site anyway.
type Dispatcher struct {
	// OAuth handles every id that is not namespaced, which is every id the
	// authorization endpoint has ever issued.
	OAuth *authorize.Handler

	// SAML handles the namespaced ones.
	SAML *Handler
}

// NewDispatcher returns a Dispatcher.
func NewDispatcher(oauth *authorize.Handler, saml *Handler) *Dispatcher {
	return &Dispatcher{OAuth: oauth, SAML: saml}
}

// Peek reads a pending request for rendering, from whichever protocol owns it.
func (d *Dispatcher) Peek(ctx context.Context, id string) (authorize.Pending, error) {
	if d.SAML != nil && IsSAMLRequest(id) {
		return d.SAML.Peek(ctx, id)
	}
	return d.OAuth.Peek(ctx, id)
}

// Resume completes a sign-in with a session that has just been created.
//
// The routing decision is made from the id alone, exactly as it is in Peek. A
// dispatcher that decided differently in the two would render one protocol's
// login page and complete another's flow, which is a confusion worth being
// unable to express.
func (d *Dispatcher) Resume(w http.ResponseWriter, r *http.Request, id string, current session.Session) {
	if d.SAML != nil && IsSAMLRequest(id) {
		d.SAML.Resume(w, r, id, current)
		return
	}
	d.OAuth.Resume(w, r, id, current)
}
