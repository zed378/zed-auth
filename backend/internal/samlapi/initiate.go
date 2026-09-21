package samlapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// IdP-initiated sign-on (P4-08 F-6).
//
// The user starts here — from a portal, a bookmark, a link in an email — and
// this service delivers an assertion to a service provider that never asked for
// one.
//
// # The risk, stated rather than implied
//
// An SP-initiated login is correlated: the service provider generated an
// `AuthnRequest`, remembers its id, and checks `InResponseTo` on the way back.
// That correlation is what tells it the assertion answers a login IT started.
//
// An IdP-initiated assertion answers nothing, so there is nothing to correlate
// against. Anyone who can cause a browser to visit this endpoint — a link, an
// image tag, a redirect from a page the user already trusted — can have an
// assertion delivered to a service provider. That is the same shape as a CSRF,
// and the SAML specification acknowledges it: the binding exists because
// enterprises want portal links, not because it is as safe as the other one.
//
// Some service providers defend themselves. Many log the user in.
//
// # What this service does about it
//
// **Opt in, per registration, off by default.** A service provider that has not
// said it accepts unsolicited assertions does not receive one, and the refusal
// is visible rather than a silent capability nobody chose. That is the card's
// own instruction — "consider making it opt-in per application" — taken as the
// answer rather than as a suggestion.
//
// **A live session is required.** This endpoint never starts an authentication
// of its own: with no session the browser goes to the hosted login and comes
// back, exactly as the SP-initiated flow does. An endpoint that both accepted
// unsolicited requests AND drove a login would be a way to make somebody
// authenticate at a service provider of an attacker's choosing.
//
// **No `InResponseTo`.** The assertion says plainly that it answers no request,
// rather than inventing an id that would make it look correlated.

// Initiate starts an IdP-initiated sign-on.
//
//	GET /saml/init?entity_id=<sp>&RelayState=<opaque>
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	entityID := strings.TrimSpace(r.URL.Query().Get("entity_id"))
	if entityID == "" {
		h.badRequest(w, "No service provider was named.")
		return
	}

	relayState := r.URL.Query().Get("RelayState")
	if len(relayState) > saml.MaxRelayStateBytes {
		h.badRequest(w, "RelayState is too large.")
		return
	}

	key, err := h.Keys.SAML(r.Context())
	if err != nil {
		h.unavailable(w, "SAML is not configured on this instance.")
		return
	}

	reg, err := h.Providers.ByEntityID(r.Context(), h.DB.SQL(), entityID)
	if err != nil {
		h.badRequest(w, "That service provider is not registered with this identity provider.")
		return
	}

	// The opt-in. Refused here rather than delivered, because an unsolicited
	// assertion to a service provider that did not ask for the capability is
	// precisely the thing the flag exists to prevent — and delivering a SAML
	// failure to it would still be delivering something it never asked for.
	if !reg.AllowIdPInitiated {
		h.badRequest(w, "That service provider does not accept identity-provider-initiated sign-on.")
		return
	}

	current, ok := h.current(r)
	if !ok || current.OrgID != reg.OrgID {
		// To the hosted login and back. The return path is this endpoint
		// again, with the same parameters, rather than the SP-initiated resume
		// seam: there is no AuthnRequest to record and nothing to consume.
		h.toLoginForInitiate(w, r, reg, relayState)
		return
	}

	// No requested context to satisfy — there is no request — so the assertion
	// reports what the session earned and the service provider decides whether
	// that is enough.
	class, err := saml.CheckAuthnContext("", current.AuthMethods)
	if err != nil {
		h.deliverFailure(w, reg, "", saml.StatusNoAuthnContext, relayState)
		return
	}

	h.issue(w, r, key, reg, "", class, current, relayState)
}

// toLoginForInitiate records the sign-on and sends the browser to the login.
//
// It reuses the pending-request table and the resume seam rather than teaching
// the login page a `next` parameter. Two reasons, and the second is the one
// that decided it:
//
// The login page has no `next` today, and adding one means a URL the page
// redirects to after authentication — which is an open redirect unless every
// path through the page validates it. That is a new hole in the most
// security-critical surface in the service, opened for one flow.
//
// The row is bookkeeping, not a request. Its id exists so this browser can be
// found again; `idp_initiated` is what stops that id being echoed back as
// `InResponseTo` and inventing a correlation this flow does not have.
func (h *Handler) toLoginForInitiate(
	w http.ResponseWriter, r *http.Request, reg saml.Registration, relayState string,
) {
	id, err := saml.NewRequestID()
	if err != nil {
		h.logError("generating an IdP-initiated request id", err)
		h.unavailable(w, "Sign-in is unavailable.")
		return
	}

	now := h.now()
	pending := saml.Pending{
		ID:           id,
		SPID:         reg.ID,
		RelayState:   relayState,
		IdPInitiated: true,
		CreatedAt:    now,
		ExpiresAt:    now.Add(saml.PendingLifetime),
	}

	if err := h.DB.WithTenant(r.Context(), reg.OrgID, func(tx *postgres.Tx) error {
		return h.Requests.Record(r.Context(), tx, reg.OrgID, pending)
	}); err != nil {
		h.logError("recording an IdP-initiated sign-on", err)
		h.unavailable(w, "Sign-in is unavailable.")
		return
	}

	target, err := url.Parse(h.LoginPath)
	if err != nil {
		h.logError("parsing the login path", err)
		h.unavailable(w, "Sign-in is unavailable.")
		return
	}
	query := target.Query()
	query.Set("request", RequestPrefix+id)
	target.RawQuery = query.Encode()

	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}
