package token

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Token introspection (RFC 7662) and revocation (RFC 7009).
//
// Both live in this package rather than a new one because both need
// ParseCredentials, AuthenticateClient, the Error type and the RefreshStore —
// a separate package would mean exporting more of this one's internals to no
// benefit. They are token-lifecycle endpoints and this is the token-lifecycle
// package.
//
// The rule that does the most work in both is in resolve(): a client may only
// see or destroy tokens issued to ITSELF. A token belonging to another client
// is answered exactly as an unknown one, because a "forbidden" would confirm
// it exists and belongs to somebody.
//
// Specification: MEMORY/specs/P1-09-introspect-revoke.md.

// maxLifecycleBody bounds the form.
//
// Generous against a JWT with a large claim set, small enough that neither
// endpoint is a place to push bytes.
const maxLifecycleBody = 16 << 10

// Token type hints, RFC 7662 §2.1. A hint and nothing more: a wrong one costs
// an extra lookup, it never causes a real token to be reported unknown.
const (
	HintAccessToken  = "access_token"
	HintRefreshToken = "refresh_token"
)

// Kind is which sort of token a presented value turned out to be.
type Kind string

const (
	KindNone    Kind = ""
	KindAccess  Kind = "access_token"
	KindRefresh Kind = "refresh_token"
)

// resolved is a token that was recognised AND belongs to the caller.
type resolved struct {
	Kind Kind

	// Access is populated for KindAccess.
	Access map[string]any

	// Refresh is populated for KindRefresh.
	Refresh Refresh
}

// Sessions is the interface handler.go already declares, reused here rather
// than redeclared. It is what makes an access token's `active` answer honest:
// the token is a signed JWT that nothing looks up, so without a liveness check
// an introspection would keep reporting `active: true` for a user who logged
// out ten minutes ago.

// LifecycleObserver counts outcomes.
//
// Labelled by endpoint and a coarse outcome. Never by WHY a token was
// inactive: that would put the disclosure the response body refuses to make
// into /metrics, which is scraped and retained.
type LifecycleObserver interface {
	Lifecycle(endpoint, outcome string)
}

const (
	OutcomeActive   = "active"
	OutcomeInactive = "inactive"
	OutcomeRevoked  = "revoked"
	OutcomeNothing  = "nothing"
	OutcomeRefused  = "refused"
	OutcomeError    = "error"
)

// LifecycleHandler serves /oauth/introspect and /oauth/revoke.
//
// One type with two ServeHTTP-shaped methods rather than two types, because
// everything up to and including resolve() is shared and duplicating it would
// be duplicating the ownership rule.
type LifecycleHandler struct {
	Issuer   string
	Clients  Clients
	Verifier Verifier
	Refresh  RefreshTokens
	Sessions Sessions
	Tenant   Tenant
	Audit    Auditor
	Observer LifecycleObserver
	Log      *slog.Logger

	Now func() time.Time
}

// Verifier checks a compact JWS of an expected type.
type Verifier interface {
	Verify(compact, wantType string) ([]byte, error)
}

// RefreshTokens resolves and destroys opaque refresh tokens.
//
// An interface, like Clients and Verifier, for the same reason: the response
// shapes these endpoints produce — above all that every negative answer is the
// identical bytes — should be answerable without a database, because that is
// the question worth asking often.
//
// Lookup drops the *postgres.DB the store's own method takes: binding the
// database is the adapter's job (cmd/authservice wires it, the way it wires
// clientLookup), not something every caller repeats.
type RefreshTokens interface {
	Lookup(ctx context.Context, presented string, now time.Time) (Refresh, error)
	RevokeFamily(ctx context.Context, tx *postgres.Tx, familyID string) (int64, error)
	RevokeForSessionAndClient(ctx context.Context, tx *postgres.Tx, sessionID, clientID string) (int64, error)
}

// Tenant runs work inside a tenant-scoped transaction, and Auditor records it.
//
// Interfaces for the reason login.Handler uses the same pair: RLS is not
// something a fake can pretend to have, so the reads that depend on it are
// tested against a real database — while the response shapes, which are most
// of what can go wrong here, stay answerable without one.
type Tenant interface {
	WithTenant(ctx context.Context, orgID string, fn func(*postgres.Tx) error) error
}

type Auditor interface {
	Write(ctx context.Context, tx *postgres.Tx, e audit.Event) error
}

func (h *LifecycleHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// --- POST /oauth/introspect -------------------------------------------------------

// Introspect answers whether a token is currently good.
func (h *LifecycleHandler) Introspect(w http.ResponseWriter, r *http.Request) {
	app, form, err := h.authenticate(r, introspectionRequiresConfidential)
	if err != nil {
		h.count("introspect", OutcomeRefused)
		h.writeError(w, err)
		return
	}

	presented := form.Get("token")
	if presented == "" {
		h.count("introspect", OutcomeRefused)
		h.writeError(w, badRequest(ErrInvalidRequest, "token is required"))
		return
	}

	found, reason := h.resolve(r.Context(), app, presented, form.Get("token_type_hint"))
	if found.Kind == KindNone {
		// Unknown, expired, revoked, malformed, or somebody else's. One
		// answer for all of them — see the spec's §12. The reason goes to the
		// log, never to the caller, and never with the token beside it.
		if h.Log != nil && reason != "" {
			h.Log.Debug("introspection reported a token inactive",
				"client_id", app.ID, "reason", reason)
		}
		h.count("introspect", OutcomeInactive)
		h.writeJSON(w, http.StatusOK, inactive)
		return
	}

	h.count("introspect", OutcomeActive)
	h.writeJSON(w, http.StatusOK, h.describe(found))
}

// inactive is the whole negative response.
//
// A package-level value rather than a literal per branch, so that every
// negative path emits the identical bytes by construction rather than by three
// call sites agreeing. Adding a `reason` field here would be one edit that
// breaks the uniformity everywhere at once — which is the intent: it should be
// hard to do by accident and obvious in review.
var inactive = map[string]any{"active": false}

// describe builds the positive response.
//
// `username` is deliberately absent, though RFC 7662 lists it: it is an email
// address, the caller already has `sub`, and a resource server that wants the
// address can ask /oauth/userinfo with the user's own token. Putting it here
// would write a personal identifier into a response resource servers routinely
// log.
func (h *LifecycleHandler) describe(found resolved) map[string]any {
	out := map[string]any{
		"active":     true,
		"token_type": "Bearer",
		"iss":        h.Issuer,
	}

	switch found.Kind {
	case KindAccess:
		for _, claim := range []string{"scope", "client_id", "exp", "iat", "sub", "aud"} {
			if value, ok := found.Access[claim]; ok {
				out[claim] = value
			}
		}
	case KindRefresh:
		out["client_id"] = found.Refresh.ClientID
		out["sub"] = found.Refresh.UserID
		out["exp"] = found.Refresh.ExpiresAt.Unix()
		out["aud"] = h.Issuer
		if len(found.Refresh.Scope) > 0 {
			out["scope"] = joinScope(found.Refresh.Scope)
		}
	}

	return out
}

// --- POST /oauth/revoke ------------------------------------------------------------

// Revoke destroys a token the caller no longer wants.
//
// Always answers 200, whatever happened — RFC 7009 §2.2, and for the same
// reason introspection has one negative answer: a status that distinguished
// "revoked something" from "there was nothing to revoke" is an oracle on
// whether a guessed token exists.
func (h *LifecycleHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	app, form, err := h.authenticate(r, revocationAdmitsPublic)
	if err != nil {
		h.count("revoke", OutcomeRefused)
		h.writeError(w, err)
		return
	}

	presented := form.Get("token")
	if presented == "" {
		h.count("revoke", OutcomeRefused)
		h.writeError(w, badRequest(ErrInvalidRequest, "token is required"))
		return
	}

	found, reason := h.resolve(r.Context(), app, presented, form.Get("token_type_hint"))
	if found.Kind == KindNone {
		if h.Log != nil && reason != "" {
			h.Log.Debug("revocation found nothing to revoke",
				"client_id", app.ID, "reason", reason)
		}
		h.count("revoke", OutcomeNothing)
		h.ok(w)
		return
	}

	revoked, err := h.destroy(r.Context(), app, found)
	if err != nil {
		if h.Log != nil {
			h.Log.Error("revoking a token failed", "client_id", app.ID, "error", err.Error())
		}
		h.count("revoke", OutcomeError)
		h.writeError(w, Error{
			Code: ErrServerError, Status: http.StatusInternalServerError,
			Description: "the token could not be revoked",
		})
		return
	}

	if revoked == 0 {
		// Already revoked. Idempotent by construction, and indistinguishable
		// from having revoked something — a client retrying a request it is
		// not sure landed must not be told which time worked.
		h.count("revoke", OutcomeNothing)
	} else {
		h.count("revoke", OutcomeRevoked)
	}
	h.ok(w)
}

// destroy revokes what the presented token stands for, and audits it.
//
// The interesting case is an ACCESS token. It is a signed JWT that nothing
// looks up (docs/PLAN/04), so it cannot be revoked — but RFC 7009 §2.1 says
// that presenting one SHOULD revoke the refresh token behind it, and that is
// implementable: the access token names its session and its client, which is
// exactly the pair a refresh token is issued against.
//
// The presented access token itself keeps working until it expires. That is a
// real limitation and the OpenAPI description says so plainly, rather than
// letting an integrator assume `200` means the token is dead.
func (h *LifecycleHandler) destroy(
	ctx context.Context, app client.Application, found resolved,
) (int64, error) {
	var (
		revoked int64
		kind    string
		userID  string
	)

	err := h.Tenant.WithTenant(ctx, app.OrgID, func(tx *postgres.Tx) error {
		switch found.Kind {
		case KindRefresh:
			kind = string(KindRefresh)
			userID = found.Refresh.UserID

			// The whole family, not the one row. Revoking a single token and
			// leaving its lineage alive would let a rotated descendant carry
			// on — which is what RFC 7009 §2.1 means by invalidating tokens
			// based on the same authorization grant, and what P3-06's reuse
			// detection will rely on.
			count, err := h.Refresh.RevokeFamily(ctx, tx, found.Refresh.FamilyID)
			revoked = count
			return err

		case KindAccess:
			kind = string(KindAccess)
			userID, _ = found.Access["sub"].(string)
			sessionID, _ := found.Access["sid"].(string)
			if sessionID == "" {
				// A client_credentials token: no session, no refresh token
				// behind it, nothing to revoke.
				return nil
			}

			count, err := h.Refresh.RevokeForSessionAndClient(ctx, tx, sessionID, app.ID)
			revoked = count
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	if revoked == 0 {
		// Nothing changed, so there is nothing to record. An audit entry for
		// every no-op retry would be volume without signal.
		return 0, nil
	}

	auditErr := h.Tenant.WithTenant(ctx, app.OrgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       app.OrgID,
			ActorUserID: userID,
			Type:        audit.EventTokenRevoked,
			Payload: map[string]any{
				// The client, the user, the count and the kind. NEVER the
				// token or its hash: the audit log has a 24-month retention,
				// which makes it the worst place in the system for either.
				"client_id":  app.ID,
				"token_kind": kind,
				"revoked":    revoked,
			},
		})
	})
	if auditErr != nil {
		// The revocation committed and the record did not. Reported rather
		// than swallowed, and NOT rolled back: a token that is actually gone
		// is the outcome the caller asked for, and undoing it to keep the log
		// tidy would trade a security action for bookkeeping.
		if h.Log != nil {
			h.Log.Error("a revocation was not audited",
				"client_id", app.ID, "revoked", revoked, "error", auditErr.Error())
		}
	}

	return revoked, nil
}

// --- shared ----------------------------------------------------------------------

// clientPolicy is which client types an endpoint admits.
type clientPolicy int

const (
	// introspectionRequiresConfidential refuses public clients outright.
	//
	// Stricter than RFC 7662, which leaves the authentication method open. A
	// public client has no secret, so admitting one would make introspection
	// reachable by anyone who can read a client_id out of a browser URL — a
	// token oracle, which is the one thing this endpoint must not be.
	introspectionRequiresConfidential clientPolicy = iota

	// revocationAdmitsPublic, as RFC 7009 §2.1 contemplates.
	//
	// The operation is different, so the reasoning is. A caller can only
	// destroy a token that belongs to its own client, so the worst a forged
	// caller achieves is throwing away a credential it was already holding.
	revocationAdmitsPublic
)

// authenticate parses the form and establishes which client is calling.
func (h *LifecycleHandler) authenticate(
	r *http.Request, policy clientPolicy,
) (client.Application, url.Values, error) {
	if r.Method != http.MethodPost {
		return client.Application{}, nil, Error{
			Code: ErrInvalidRequest, Status: http.StatusMethodNotAllowed,
			Description: "this endpoint accepts POST",
		}
	}

	r.Body = http.MaxBytesReader(nil, r.Body, maxLifecycleBody)
	if err := r.ParseForm(); err != nil {
		return client.Application{}, nil, badRequest(ErrInvalidRequest, "the form could not be read")
	}

	creds, err := ParseCredentials(r, r.PostForm)
	if err != nil {
		return client.Application{}, nil, err
	}
	if creds.ClientID == "" {
		return client.Application{}, nil, Error{
			Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
			Description: "client authentication is required",
		}
	}

	app, err := h.Clients.ByClientID(r.Context(), creds.ClientID)
	if err != nil {
		// The same answer as a wrong secret. Distinguishing them would confirm
		// which client ids exist to anyone who can send a request.
		return client.Application{}, nil, Error{
			Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
			Description: "client authentication failed",
		}
	}

	if policy == introspectionRequiresConfidential && !app.Type.IsConfidential() {
		return client.Application{}, nil, Error{
			Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
			Description: "introspection requires a confidential client",
		}
	}

	stored, err := h.Clients.CredentialsFor(r.Context(), app)
	if err != nil {
		return client.Application{}, nil, Error{
			Code: ErrServerError, Status: http.StatusInternalServerError,
			Description: "client authentication could not be completed",
		}
	}

	if err := AuthenticateClient(app, creds, stored, h.now()); err != nil {
		return client.Application{}, nil, err
	}

	return app, r.PostForm, nil
}

// resolve identifies a presented token AND checks it belongs to the caller.
//
// The two are done together on purpose. Separating "what is this token" from
// "may this client see it" invites a call site that does the first and forgets
// the second, and the forgotten one is the security check.
//
// Returns KindNone with a reason for the log when the token is unknown,
// expired, revoked, malformed, or somebody else's — the caller answers all of
// those identically.
func (h *LifecycleHandler) resolve(
	ctx context.Context, app client.Application, presented, hint string,
) (resolved, string) {
	// The hint decides the ORDER, never the outcome: RFC 7662 §2.1 requires
	// the other type to be tried when the hinted one does not resolve, because
	// a caller that mislabels a token still holds a real token.
	first, second := h.asAccessToken, h.asRefreshToken
	if hint == HintRefreshToken {
		first, second = h.asRefreshToken, h.asAccessToken
	}

	if found, reason := first(ctx, app, presented); found.Kind != KindNone {
		return found, reason
	}
	return second(ctx, app, presented)
}

func (h *LifecycleHandler) asAccessToken(
	ctx context.Context, app client.Application, presented string,
) (resolved, string) {
	payload, err := h.Verifier.Verify(presented, signing.TypeAccessToken)
	if err != nil {
		return resolved{}, "not a valid access token"
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return resolved{}, "the access token payload is not JSON"
	}

	if issuer, _ := claims["iss"].(string); issuer != h.Issuer {
		return resolved{}, "the access token names another issuer"
	}

	exp, ok := numericClaim(claims["exp"])
	if !ok || h.now().Unix() >= exp {
		return resolved{}, "the access token has expired"
	}

	// The ownership rule. A token issued to another client is answered as
	// unknown, not forbidden — a "forbidden" confirms it exists.
	if clientID, _ := claims["client_id"].(string); clientID != app.ID {
		return resolved{}, "the access token belongs to another client"
	}

	// A live session is what makes `active: true` honest. An access token is
	// not looked up, so without this an introspection would keep reporting a
	// token good for a user who logged out ten minutes ago.
	//
	// A client_credentials token has no session and needs none.
	if sessionID, _ := claims["sid"].(string); sessionID != "" {
		if h.Sessions == nil || !h.Sessions.IsLive(ctx, sessionID, h.now()) {
			return resolved{}, "the session behind the access token has ended"
		}
	}

	return resolved{Kind: KindAccess, Access: claims}, ""
}

func (h *LifecycleHandler) asRefreshToken(
	ctx context.Context, app client.Application, presented string,
) (resolved, string) {
	// The store's lookup already filters revoked and expired rows, so an
	// unknown, a revoked and an expired token arrive here identically — which
	// is what the caller needs to answer them identically.
	stored, err := h.Refresh.Lookup(ctx, presented, h.now())
	if err != nil {
		if !errors.Is(err, ErrRefreshNotFound) && h.Log != nil {
			h.Log.Error("looking up a refresh token failed", "error", err.Error())
		}
		return resolved{}, "no live refresh token"
	}

	if stored.ClientID != app.ID {
		return resolved{}, "the refresh token belongs to another client"
	}

	return resolved{Kind: KindRefresh, Refresh: stored}, ""
}

// numericClaim reads a JSON number, which decodes as float64.
func numericClaim(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

// --- responses --------------------------------------------------------------------

func (h *LifecycleHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	h.headers(w)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *LifecycleHandler) ok(w http.ResponseWriter) {
	h.headers(w)
	w.WriteHeader(http.StatusOK)
}

func (h *LifecycleHandler) writeError(w http.ResponseWriter, err error) {
	var oe Error
	if !errors.As(err, &oe) {
		oe = Error{
			Code: ErrServerError, Status: http.StatusInternalServerError,
			Description: "the request could not be completed",
		}
	}

	h.headers(w)
	if oe.Basic {
		w.Header().Set("WWW-Authenticate", `Basic realm="`+h.Issuer+`"`)
	}
	w.WriteHeader(oe.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             oe.Code,
		"error_description": oe.Description,
	})
}

func (h *LifecycleHandler) headers(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func (h *LifecycleHandler) count(endpoint, outcome string) {
	if h.Observer != nil {
		h.Observer.Lifecycle(endpoint, outcome)
	}
}
