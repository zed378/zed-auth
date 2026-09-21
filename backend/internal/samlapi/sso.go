package samlapi

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/beevik/etree"

	"github.com/zed378/zed-auth/backend/internal/session"

	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Single sign-on, both bindings (P4-08 F-1 … F-5).
//
// The shape of this handler is the shape of the constraints. Read the request
// under the document bounds, look the service provider up BY ITS ISSUER, and
// from that point on every destination, audience and attribute comes from the
// registration — never from the request. The request supplies exactly three
// things this service acts on: an id to echo, an issuer to look up, and
// optionally a context to satisfy or refuse.

// SSORedirect serves the HTTP-Redirect binding.
func (h *Handler) SSORedirect(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("SAMLRequest")
	if encoded == "" {
		h.badRequest(w, "The request carries no SAMLRequest.")
		return
	}
	raw, err := saml.DecodeRedirect(encoded)
	if err != nil {
		h.refuseDecode(w, err)
		return
	}
	h.sso(w, r, raw, r.URL.Query().Get("RelayState"), bindingRedirect)
}

// SSOPost serves the HTTP-POST binding.
func (h *Handler) SSOPost(w http.ResponseWriter, r *http.Request) {
	// The body is bounded before parsing. ParseForm reads it all, and a form
	// parser is not a place to discover that a request was 400 MB.
	r.Body = http.MaxBytesReader(w, r.Body, saml.MaxEncodedRequestBytes*2)
	if err := r.ParseForm(); err != nil {
		h.badRequest(w, "The form could not be read.")
		return
	}

	encoded := r.PostForm.Get("SAMLRequest")
	if encoded == "" {
		h.badRequest(w, "The request carries no SAMLRequest.")
		return
	}
	raw, err := saml.DecodePost(encoded)
	if err != nil {
		h.refuseDecode(w, err)
		return
	}
	h.sso(w, r, raw, r.PostForm.Get("RelayState"), bindingPost)
}

// sso is the flow both bindings share once the document is in hand.
// binding says how a request arrived, which decides how a signature on it can
// be checked.
type binding int

const (
	bindingRedirect binding = iota
	bindingPost
)

func (h *Handler) sso(w http.ResponseWriter, r *http.Request, raw []byte, relayState string, via binding) {
	if len(relayState) > saml.MaxRelayStateBytes {
		// Refused rather than truncated, for the reason PostForm gives: a
		// truncated RelayState is one the service provider cannot match
		// against anything it stored.
		h.badRequest(w, "RelayState is too large.")
		return
	}

	key, err := h.Keys.SAML(r.Context())
	if err != nil {
		h.unavailable(w, "SAML is not configured on this instance.")
		return
	}

	request, err := saml.ParseAuthnRequest(raw)
	if err != nil {
		h.badRequest(w, "The AuthnRequest could not be read.")
		return
	}

	// The service provider, by ITS ISSUER (C-1). Everything downstream — where
	// the answer goes, what audience it names, which attributes it carries —
	// comes from this row.
	//
	// Outside any tenant scope, because the entity ID is what DETERMINES the
	// tenant. The function it calls is bounded to one exact entity ID and to
	// live registrations, which is ADR-023's shape for OIDC applied here.
	reg, err := h.Providers.ByEntityID(r.Context(), h.DB.SQL(), request.Issuer)
	if err != nil {
		// An unregistered issuer is refused here, with nowhere to send a SAML
		// error: the only address this service would have is one the request
		// supplied, and POSTing a failure to an unverified URL is the open
		// redirect this whole lookup exists to prevent.
		h.badRequest(w, "That service provider is not registered with this identity provider.")
		return
	}

	// A service provider that asked for its requests to be verified is
	// verified — or refused (C-1's neighbour, and the reason `want_signed_requests`
	// is stored rather than decorative).
	//
	// Storing the flag and not enforcing it would be worse than not offering
	// it: an administrator who ticked the box believes requests are checked,
	// and nothing tells them otherwise.
	if reg.WantSignedRequests {
		if err := h.verifyRequest(raw, reg, via); err != nil {
			if h.Log != nil {
				h.Log.Warn("a SAML request failed signature verification",
					"entity_id", reg.EntityID, "reason", err.Error())
			}
			h.deliverFailure(w, reg, request.ID, saml.StatusRequester, relayState)
			return
		}
	}

	// A live session, or the hosted login.
	current, ok := h.current(r)
	if !ok || current.OrgID != reg.OrgID || request.ForceAuthn {
		h.toLogin(w, r, reg, request, relayState)
		return
	}

	// What the session actually earned, against what was asked for (C-6).
	class, err := saml.CheckAuthnContext(request.RequestedAuthnContext, current.AuthMethods)
	if err != nil {
		// Not silence, and not a re-prompt loop: the service provider is told
		// the session cannot meet its requirement, in its own vocabulary, at
		// its own registered address.
		h.deliverFailure(w, reg, request.ID, saml.StatusNoAuthnContext, relayState)
		return
	}

	h.issue(w, r, key, reg, request.ID, class, current, relayState)
}

// toLogin stores the request and sends the browser to the hosted login.
//
// The id is opaque and carries a namespace, so the login page's resume seam can
// route it back here rather than to the OAuth flow.
func (h *Handler) toLogin(
	w http.ResponseWriter, r *http.Request, reg saml.Registration,
	request saml.AuthnRequest, relayState string,
) {
	now := h.now()
	pending := saml.Pending{
		ID:                    request.ID,
		SPID:                  reg.ID,
		RelayState:            relayState,
		RequestedAuthnContext: request.RequestedAuthnContext,
		CreatedAt:             now,
		ExpiresAt:             now.Add(saml.PendingLifetime),
	}

	if err := h.DB.WithTenant(r.Context(), reg.OrgID, func(tx *postgres.Tx) error {
		return h.Requests.Record(r.Context(), tx, reg.OrgID, pending)
	}); err != nil {
		if errors.Is(err, saml.ErrAlreadyAnswered) {
			// A repeated AuthnRequest id. Broken or replaying; either way the
			// answer is a refusal to the registered address rather than a new
			// login.
			h.deliverFailure(w, reg, request.ID, saml.StatusRequester, relayState)
			return
		}
		h.logError("recording the SAML AuthnRequest", err)
		h.deliverFailure(w, reg, request.ID, saml.StatusResponder, relayState)
		return
	}

	target, err := url.Parse(h.LoginPath)
	if err != nil {
		h.logError("parsing the login path", err)
		h.unavailable(w, "Sign-in is unavailable.")
		return
	}
	query := target.Query()
	// The namespaced id and nothing else — the same discipline the OAuth flow
	// uses. Carrying the original parameters through the login page would put
	// an attacker-supplied document in a URL the user can edit.
	query.Set("request", RequestPrefix+request.ID)
	target.RawQuery = query.Encode()

	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

// issue signs an assertion for a session and delivers it.
func (h *Handler) issue(
	w http.ResponseWriter, r *http.Request, key *saml.SigningKey, reg saml.Registration,
	inResponseTo, class string, current session.Session, relayState string,
) {
	now := h.now()

	var subject saml.Subject
	if err := h.DB.WithTenant(r.Context(), reg.OrgID, func(tx *postgres.Tx) error {
		var err error
		subject, err = h.Subjects.For(r.Context(), tx, current.UserID, reg)
		if err != nil {
			return err
		}
		// Audited inside the same transaction as the read that produced the
		// claims, so an assertion that was issued and an event that says so
		// cannot come apart (F-8).
		if h.Audit != nil {
			return h.Audit.SAMLAuthenticated(r.Context(), tx, reg.OrgID, current.UserID, reg.EntityID)
		}
		return nil
	}); err != nil {
		h.logError("building the SAML subject", err)
		h.deliverFailure(w, reg, inResponseTo, saml.StatusResponder, relayState)
		return
	}

	subject.AuthnContextClassRef = class
	subject.SessionIndex = current.ID
	if subject.AuthnInstant.IsZero() {
		subject.AuthnInstant = current.CreatedAt
	}

	assertion, err := saml.Issue(key, h.Issuer, reg.ServiceProvider, subject, inResponseTo, now)
	if err != nil {
		h.logError("issuing the SAML assertion", err)
		h.deliverFailure(w, reg, inResponseTo, saml.StatusResponder, relayState)
		return
	}

	response, err := saml.Response(h.Issuer, inResponseTo, reg.ACSURL, saml.StatusSuccess, assertion, now)
	if err != nil {
		h.logError("building the SAML response", err)
		h.deliverFailure(w, reg, inResponseTo, saml.StatusResponder, relayState)
		return
	}
	h.deliver(w, reg, response, relayState)
}

// deliverFailure tells the service provider, in its own vocabulary, at its own
// registered address.
//
// A failure still goes to the REGISTERED ACS URL. Answering a failure to an
// address the request supplied would be the open redirect the registration
// lookup exists to prevent, and a failure is exactly where somebody would
// forget.
func (h *Handler) deliverFailure(
	w http.ResponseWriter, reg saml.Registration, inResponseTo, status, relayState string,
) {
	response, err := saml.Response(h.Issuer, inResponseTo, reg.ACSURL, status, nil, h.now())
	if err != nil {
		h.logError("building a SAML failure response", err)
		h.unavailable(w, "Sign-in could not be completed.")
		return
	}
	h.deliver(w, reg, response, relayState)
}

// deliver writes the self-submitting form.
func (h *Handler) deliver(w http.ResponseWriter, reg saml.Registration, response *etree.Element, relayState string) {
	form, err := saml.PostForm(reg.ACSURL, response, relayState)
	if err != nil {
		h.logError("rendering the SAML post form", err)
		h.unavailable(w, "Sign-in could not be completed.")
		return
	}

	// The page posts to a partner's ACS URL, so `form-action` must allow that
	// origin and nothing wider (threat review T4-7). Set here rather than
	// inherited, because the default policy for this service's own pages has no
	// reason to allow a third-party form target.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; form-action "+originOf(reg.ACSURL)+"; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// #nosec G705 -- the taint is real and already neutralised. `form` is the
	// output of saml.PostForm, which is html/template with contextual
	// escaping; the only attacker-influenced values in it are RelayState and
	// the destination, and both are interpolated as template data rather than
	// concatenated. response_test.go asserts an injection payload comes back
	// escaped and still carried, and a mutation that rebuilds the form by
	// concatenation turns those tests red.
	//
	// gosec cannot see through the call, so it flags the write rather than the
	// construction. Silenced here, narrowly, rather than by excluding the rule.
	_, _ = w.Write(form)
}

// originOf reduces a URL to scheme://host, for a Content-Security-Policy.
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// Nothing rather than a guess. A malformed registered URL is a
		// configuration error, and a policy of "'none'" fails closed.
		return "'none'"
	}
	return u.Scheme + "://" + u.Host
}

func (h *Handler) badRequest(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(message + "\n"))
}

// refuseDecode answers a document that could not be read.
//
// The reason is not told to the caller. "Your decompression bomb was too large"
// is a useful message to an attacker tuning one and useless to a service
// provider, whose request is either well-formed or broken in a way its own
// library will explain.
func (h *Handler) refuseDecode(w http.ResponseWriter, err error) {
	if h.Log != nil {
		h.Log.Warn("a SAML request was refused", "reason", err.Error())
	}
	h.badRequest(w, "The SAMLRequest could not be read.")
}

// verifyRequest checks a service provider's signature on its own AuthnRequest.
//
// # What is implemented, and what is refused
//
// The HTTP-POST binding signs the XML, which is the signature this service
// already knows how to check: the same `saml.Verify` every assertion goes
// through, so the signed element must be the element consumed.
//
// The HTTP-Redirect binding does NOT sign the XML. It signs a query string —
// `SAMLRequest=…&RelayState=…&SigAlg=…` as an octet sequence, with the
// signature in a separate parameter — which is a different construction with
// its own canonicalisation rules and its own history of implementations that
// verify the wrong bytes.
//
// That is not implemented, and a request arriving on Redirect from a service
// provider that requires signing is REFUSED rather than accepted unverified.
// Refusing is a visible failure an administrator can act on; accepting would
// mean a registration that says "verify my requests" silently verifying
// nothing, which is the failure the flag exists to prevent.
func (h *Handler) verifyRequest(raw []byte, reg saml.Registration, via binding) error {
	if via != bindingPost {
		return fmt.Errorf("saml: this service verifies signed AuthnRequests on the HTTP-POST binding only")
	}
	if reg.Certificate == "" {
		// Migration 040's CHECK makes this unreachable — a registration that
		// wants signed requests must carry a certificate — so reaching it
		// means the row was written around the constraint.
		return fmt.Errorf("saml: the registration requires signed requests and carries no certificate")
	}

	cert, err := saml.ParseCertificate(reg.Certificate)
	if err != nil {
		return err
	}
	if _, err := saml.Verify(raw, "AuthnRequest", []*x509.Certificate{cert}); err != nil {
		return err
	}
	return nil
}
