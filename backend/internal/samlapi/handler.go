// Package samlapi serves the SAML identity provider's browser-facing endpoints
// (P4-08).
//
// The split mirrors `internal/mfa` / `internal/mfaapi` and `internal/session` /
// `internal/sessionapi`: `internal/saml` holds the protocol logic and knows
// nothing about HTTP, and this package is the HTTP.
//
// Three endpoints, and a fourth that exists to refuse:
//
//	GET  /saml/metadata   what a service provider configures itself from
//	GET  /saml/sso        HTTP-Redirect binding
//	POST /saml/sso        HTTP-POST binding
//	POST /saml/slo        Single Logout — deliberately unsupported
//
// # Why these are not generated handlers
//
// Every other endpoint in this service answers with `docs/PLAN/05`'s error
// envelope, and the generated strict wrapper turns that into a type. SAML does
// not: a failure is a `Response` carrying a `StatusCode`, delivered to the
// service provider as a self-submitting form through the user's browser. A
// service provider parses that, never a JSON `Error`.
//
// So these are excluded from code generation for the same reason the OAuth
// endpoints are, and `httpserver` registers them explicitly. They stay in
// `openapi/openapi.yaml`, so they are documented, the public API reference
// generates from them, and `openapi-shipped-paths.py` still checks the spec
// claims nothing beyond what ships.
package samlapi

import (
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/zed378/zed-auth/backend/internal/saml"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Keys supplies the SAML signing key.
//
// An interface rather than the signing store, so this package does not depend
// on key management and can be tested with a key built in memory.
type Keys interface {
	// SAML returns the current signing key, or an error if none is configured.
	SAML(ctx context.Context) (*saml.SigningKey, error)

	// Published returns every key a service provider should trust: the one
	// signing now, then the one that will (P4-09). Separate from SAML because
	// the two answer different questions — what to sign with, and what to tell
	// the other side to accept — and conflating them is what made a rotation
	// break every integration at once.
	Published(ctx context.Context) ([]*x509.Certificate, error)
}

// Providers resolves a registered service provider by its entity ID.
//
// It takes a plain querier rather than a tenant transaction, because the
// lookup runs BEFORE the tenant is known — the entity ID is what determines it.
type Providers interface {
	ByEntityID(ctx context.Context, db saml.Querier, entityID string) (saml.Registration, error)

	// ByID resolves a registration a pending request was recorded against.
	// The pending row stores the registration's own id rather than the entity
	// id, so a service provider that re-registers under the same entity id
	// cannot inherit a login somebody else started.
	ByID(ctx context.Context, db saml.Querier, id string) (saml.Registration, error)
}

// Sessions resolves the session behind the browser.
//
// The same shape `internal/oauth/authorize` uses, so single sign-on across the
// two protocols is the same session rather than two notions of one: a user who
// signed in through OIDC has a session a SAML request finds, which is F-8.
type Sessions interface {
	Lookup(ctx context.Context, presented string, policy session.Policy, now time.Time) (session.Session, error)
}

// Subjects builds what an assertion says about a user.
//
// The NameID it returns is persistent and per-service-provider, never an email
// address (C-5): a service provider keyed on an address inherits it when the
// address is reissued, and one shared across providers lets them correlate a
// user who never agreed to be correlated.
type Subjects interface {
	For(ctx context.Context, tx *postgres.Tx, userID string, reg saml.Registration) (saml.Subject, error)
}

// Audit records a SAML authentication in the same taxonomy as an OIDC one
// (F-8). A login that is invisible in the audit log because of which protocol
// it used is a gap an investigator finds at the worst possible moment.
type Audit interface {
	SAMLAuthenticated(ctx context.Context, tx *postgres.Tx, orgID, userID, entityID string) error
}

// Handler serves the SAML endpoints.
type Handler struct {
	// Issuer is this identity provider's entity ID. The same value the
	// metadata advertises and every assertion carries.
	Issuer string

	// Endpoints are the URLs the metadata advertises, absolute.
	Endpoints saml.Endpoints

	// LoginPath is where a browser with no usable session is sent.
	LoginPath string

	// Policy decides how long a session is good for. The same value the
	// authorization endpoint uses, so the two protocols agree about whether a
	// session is live.
	Policy session.Policy

	DB        *postgres.DB
	Keys      Keys
	Providers Providers
	Sessions  Sessions
	Subjects  Subjects
	Requests  *saml.Requests
	Audit     Audit
	Log       *slog.Logger

	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// ErrNotConfigured is an instance with no SAML signing key.
var ErrNotConfigured = errors.New("samlapi: no SAML signing key is configured")

// Metadata serves the identity provider's metadata.
//
// 503 rather than an empty document when no key is configured. SAML is
// available on an instance that has a key; serving metadata with no
// certificate would be serving something nothing can verify, and a service
// provider would only discover that at the first login.
func (h *Handler) Metadata(w http.ResponseWriter, r *http.Request) {
	// Every key a service provider should trust, not only the one signing
	// now — see CachedKeys.Published. A document naming one certificate makes
	// a rotation an outage that waits on other people.
	keys, err := h.Keys.Published(r.Context())
	if err != nil {
		h.unavailable(w, "SAML is not configured on this instance.")
		return
	}

	entity, err := saml.Metadata(h.Issuer, keys, h.Endpoints)
	if err != nil {
		h.logError("building SAML metadata", err)
		h.unavailable(w, "SAML metadata could not be produced.")
		return
	}

	raw, err := saml.Serialise(entity)
	if err != nil {
		h.logError("serialising SAML metadata", err)
		h.unavailable(w, "SAML metadata could not be produced.")
		return
	}

	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	// Metadata is a public document and changes only when the signing key
	// does. A short cache is the difference between a service provider
	// refreshing it hourly and refreshing it per login.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// SingleLogout refuses, in the protocol's own vocabulary (P4-08 §7).
//
// Not a 404. A service provider that gets a 404 assumes a misconfiguration and
// retries; one that gets `RequestDenied` with a reason has been told a
// decision. The metadata advertises no SLO endpoint either, so a well-behaved
// service provider never arrives here — this is for the ones that try anyway.
func (h *Handler) SingleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(
		"SAML Single Logout is not supported by this identity provider.\n\n" +
			"Status: " + saml.StatusRequestDenied + "\n\n" +
			"Front-channel Single Logout iterates every service provider in a session\n" +
			"through the user's browser, and any one of them being slow, unreachable or\n" +
			"wrong leaves a partial logout — a user told they are signed out of\n" +
			"applications they are not. That failure is silent and users act on it, so\n" +
			"this service does not offer the feature rather than offering it unreliably.\n\n" +
			"Ending the session at this identity provider still ends it. A service\n" +
			"provider's own session is its own to end.\n"))
}

func (h *Handler) unavailable(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(message + "\n"))
}

// current returns the live session behind this request, or false.
//
// Any failure is "no session", including an infrastructure one. Failing closed
// here is a login prompt, which is recoverable; failing open would be an
// assertion about a user nobody authenticated.
func (h *Handler) current(r *http.Request) (session.Session, bool) {
	token, ok := session.FromRequest(r)
	if !ok {
		return session.Session{}, false
	}
	found, err := h.Sessions.Lookup(r.Context(), token, h.Policy, h.now())
	if err != nil {
		if !errors.Is(err, session.ErrNotFound) && h.Log != nil {
			h.Log.Warn("session lookup failed during a SAML request", "error", err.Error())
		}
		return session.Session{}, false
	}
	return found, true
}

func (h *Handler) logError(what string, err error) {
	if h.Log != nil {
		h.Log.Error(what, "error", err.Error())
	}
}
