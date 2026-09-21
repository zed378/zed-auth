package samlapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Coming back from the hosted login (P4-08 F-3).
//
// The login page finishes a sign-in and hands the request id back to whoever
// started it. Until now that was always the OAuth flow, so `internal/login`
// held one seam pointing at `authorize.Handler`.
//
// SAML needs the same seam, and the answer is NOT a second one. A login page
// that knew which protocols exist would have to be edited for the third, and
// the most security-critical path in the service is the worst place to
// accumulate branches. Instead the request id carries a namespace and a
// dispatcher in front of the seam routes on it — login keeps asking one thing
// one way.

// RequestPrefix namespaces a SAML request id in the login URL.
//
// A prefix rather than a separate parameter: the login page passes `request`
// through unchanged across the password step, the factor step and a refresh,
// and a second parameter would have to be threaded through every one of them.
//
// The colon cannot collide with an OAuth pending id, which is opaque base64url.
const RequestPrefix = "saml:"

// IsSAMLRequest reports whether a login request id belongs to SAML.
func IsSAMLRequest(id string) bool { return strings.HasPrefix(id, RequestPrefix) }

// Peek renders what the login page needs to show, for a SAML request.
//
// It returns an `authorize.Pending` because that is what the login page
// consumes, and the three things it reads are protocol-neutral: which
// application this is, which organization it belongs to, and where the sign-in
// ends. The last is the ACS URL here and the redirect URI for OAuth — both
// "where the browser goes next", which is what the login page's
// Content-Security-Policy needs it for.
func (h *Handler) Peek(ctx context.Context, id string) (authorize.Pending, error) {
	requestID := strings.TrimPrefix(id, RequestPrefix)

	var pending saml.Pending
	if err := h.DB.WithInstanceScope(ctx, "saml: resolving a pending AuthnRequest for the login page", func(tx *postgres.Tx) error {
		var err error
		pending, err = h.Requests.Peek(ctx, tx, requestID)
		return err
	}); err != nil {
		return authorize.Pending{}, err
	}

	reg, err := h.Providers.ByID(ctx, h.DB.SQL(), pending.SPID)
	if err != nil {
		return authorize.Pending{}, err
	}

	return authorize.Pending{
		ID: id,
		Request: authorize.Request{
			// Where this sign-in ends. Registered, never request-supplied, so
			// the login page's form-action names an address this service
			// approved.
			RedirectURI: reg.ACSURL,
		},
		App: client.Application{
			ID:    reg.ApplicationID,
			OrgID: reg.OrgID,
			Name:  reg.EntityID,
		},
	}, nil
}

// Resume completes a SAML sign-in with a session that has just been created.
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request, id string, current session.Session) {
	requestID := strings.TrimPrefix(id, RequestPrefix)

	key, err := h.Keys.SAML(r.Context())
	if err != nil {
		h.unavailable(w, "SAML is not configured on this instance.")
		return
	}

	// Consumed here, once, and only now that there is a session to answer with
	// (C-3). Consuming it earlier — when the browser left for the login page —
	// would burn the request on an attempt that might never finish.
	var pending saml.Pending
	if err := h.DB.WithTenant(r.Context(), current.OrgID, func(tx *postgres.Tx) error {
		var err error
		pending, err = h.Requests.Consume(r.Context(), tx, requestID, h.now())
		return err
	}); err != nil {
		h.logError("consuming the SAML AuthnRequest", err)
		h.badRequest(w, "That sign-in request is no longer available. Start again from the application.")
		return
	}

	reg, err := h.Providers.ByID(r.Context(), h.DB.SQL(), pending.SPID)
	if err != nil {
		h.badRequest(w, "That service provider is no longer registered.")
		return
	}
	if reg.OrgID != current.OrgID {
		// The session belongs to a different organization than the service
		// provider. Refused rather than answered: an assertion is a statement
		// about a user of THIS organization, and issuing one across the
		// boundary is the cross-tenant hole the whole tenancy model exists to
		// close.
		h.badRequest(w, "That sign-in cannot be completed for this account.")
		return
	}

	// The context check runs here too, not only on the fast path. A user who
	// signed in with a password when the service provider asked for
	// multi-factor must still be refused, and a check that only guards the
	// live-session path is a check an attacker routes around by not having a
	// session.
	class, err := saml.CheckAuthnContext(pending.RequestedAuthnContext, current.AuthMethods)
	if err != nil {
		failureID := pending.ID
		if pending.IdPInitiated {
			failureID = ""
		}
		h.deliverFailure(w, reg, failureID, saml.StatusNoAuthnContext, pending.RelayState)
		return
	}

	// An IdP-initiated sign-on answers no request, so nothing is echoed. The
	// stored id was this service's own bookkeeping; returning it would tell
	// the service provider the assertion answers a request it never made.
	inResponseTo := pending.ID
	if pending.IdPInitiated {
		inResponseTo = ""
	}

	h.issue(w, r, key, reg, inResponseTo, class, current, pending.RelayState)
}
