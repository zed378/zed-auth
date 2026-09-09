package token

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Clients resolves and authenticates the calling client.
type Clients interface {
	ByClientID(ctx context.Context, clientID string) (client.Application, error)
	CredentialsFor(ctx context.Context, app client.Application) (client.Credentials, error)
}

// Codes redeems authorization codes.
type Codes interface {
	RedeemCode(ctx context.Context, code string) (authorize.Code, error)
}

// Sessions checks that the session behind a token is still live.
type Sessions interface {
	IsLive(ctx context.Context, sessionID string, now time.Time) bool
}

// Observer records outcomes and latency.
type Observer interface {
	Issued(grant string, d time.Duration)
	Denied(grant, errorCode string)
}

// Handler serves POST /oauth/token.
type Handler struct {
	Issuer   string
	Clients  Clients
	Codes    Codes
	Sessions Sessions
	Refresh  *RefreshStore
	Signer   *signing.Signer
	DB       *postgres.DB
	Audit    *audit.Writer
	Observer Observer
	Log      *slog.Logger

	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// response is the OAuth token response (RFC 6749 § 5.1).
//
// gosec's G117 flags this struct because a marshalled field named
// `access_token` matches its secret pattern. The rule is right in general and
// wrong here: delivering these values to the client that earned them is the
// endpoint's entire purpose, and there is no version of it that does not
// serialise a token.
//
// What makes the suppression safe is narrow and worth stating, because it is
// what a future reader has to re-check: this struct is written to exactly one
// place — respond(), which sets Cache-Control: no-store — and it is never
// logged, never audited, and never stored. The audit event a few lines below
// carries the client, the grant and the scope, and no token at all. If this
// struct ever reaches a log line or a database column, the suppression stops
// being true and G117 was right after all.
//
//nolint:gosec // G117: see above — this is the response body, by design.
type response struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := h.now()

	if r.Method != http.MethodPost {
		h.fail(w, "", Error{Code: ErrInvalidRequest, Status: http.StatusMethodNotAllowed,
			Description: "the token endpoint accepts POST"})
		return
	}

	if err := r.ParseForm(); err != nil {
		h.fail(w, "", badRequest(ErrInvalidRequest, "the request body could not be parsed"))
		return
	}
	form := r.PostForm

	grant := form.Get("grant_type")
	if err := ValidateGrant(grant); err != nil {
		h.fail(w, grant, err)
		return
	}

	creds, err := ParseCredentials(r, form)
	if err != nil {
		h.fail(w, grant, err)
		return
	}

	app, err := h.Clients.ByClientID(r.Context(), creds.ClientID)
	if err != nil {
		// The same answer as a wrong secret. Distinguishing them would confirm
		// which client ids exist to anyone who can send a request.
		h.fail(w, grant, Error{Code: ErrInvalidClient, Status: http.StatusUnauthorized,
			Basic: creds.UsedBasic, Description: "client authentication failed"})
		return
	}

	stored, err := h.Clients.CredentialsFor(r.Context(), app)
	if err != nil {
		h.internal(w, grant, "reading client credentials", err)
		return
	}

	now := h.now()
	if err := AuthenticateClient(app, creds, stored, now); err != nil {
		h.fail(w, grant, err)
		return
	}

	// The client must hold the grant it is using. P1-05 already refuses to
	// register a nonsensical combination; this refuses to honour one that was
	// registered before a rule tightened.
	if !slices.Contains(app.GrantTypes, grant) {
		h.fail(w, grant, badRequest(ErrUnauthorizedClient,
			"this client is not permitted to use the %s grant", grant))
		return
	}

	var (
		out    response
		userID string
	)
	switch grant {
	case GrantAuthorizationCode:
		out, userID, err = h.authorizationCode(r.Context(), app, form, now)
	case GrantRefreshToken:
		out, userID, err = h.refreshToken(r.Context(), app, form, now)
	case GrantClientCredentials:
		out, err = h.clientCredentials(r.Context(), app, form, now)
	}
	if err != nil {
		h.fail(w, grant, err)
		return
	}

	h.audit(r.Context(), app, userID, grant, out.Scope)

	if h.Observer != nil {
		h.Observer.Issued(grant, h.now().Sub(start))
	}
	h.respond(w, out)
}

// --- authorization_code ------------------------------------------------------------

func (h *Handler) authorizationCode(
	ctx context.Context, app client.Application, form formValues, now time.Time,
) (response, string, error) {
	// Redeemed FIRST, before the client, redirect URI or verifier is checked.
	//
	// A wrong guess therefore burns the code, and that is deliberate: leaving
	// the code alive through a failed check would turn a single-use credential
	// into an oracle a verifier could be brute-forced against, one request at
	// a time, with the code staying valid throughout.
	code, err := h.Codes.RedeemCode(ctx, form.Get("code"))
	if err != nil {
		// Unknown, expired and already-redeemed collapse into one answer. Any
		// distinction tells a holder of a dead code that it was once real.
		return response{}, "", badRequest(ErrInvalidGrant, "the authorization code is not valid")
	}

	if code.ClientID != app.ID {
		// Cross-client redemption. The code bound its client at issuance
		// precisely so this comparison exists.
		return response{}, "", badRequest(ErrInvalidGrant, "the authorization code is not valid")
	}

	if code.RedirectURI != form.Get("redirect_uri") {
		return response{}, "", badRequest(ErrInvalidGrant, "the authorization code is not valid")
	}

	if err := VerifyPKCE(code.CodeChallenge, form.Get("code_verifier")); err != nil {
		return response{}, "", err
	}

	subject := Subject{
		Issuer:      h.Issuer,
		Audience:    h.Issuer,
		ClientID:    app.ID,
		ProjectID:   app.ProjectID,
		OrgID:       code.OrgID,
		UserID:      code.UserID,
		SessionID:   code.SessionID,
		AuthMethods: code.AuthMethods,
		AuthTime:    code.AuthTime,
		Nonce:       code.Nonce,
		Scope:       code.Scope,
	}

	out, err := h.issue(ctx, app, subject, code.Scope, true, now)
	return out, code.UserID, err
}

// --- refresh_token ------------------------------------------------------------------

func (h *Handler) refreshToken(
	ctx context.Context, app client.Application, form formValues, now time.Time,
) (response, string, error) {
	presented := form.Get("refresh_token")
	if presented == "" {
		return response{}, "", badRequest(ErrInvalidRequest, "refresh_token is required")
	}

	stored, err := h.Refresh.Lookup(ctx, h.DB, presented, now)
	if err != nil {
		return response{}, "", badRequest(ErrInvalidGrant, "the refresh token is not valid")
	}

	if stored.ClientID != app.ID {
		return response{}, "", badRequest(ErrInvalidGrant, "the refresh token is not valid")
	}

	// A refresh token outlives the browser session it came from, but not a
	// revoked one: P3-09 makes this systematic and Phase 1 does the check.
	if stored.SessionID != "" && h.Sessions != nil && !h.Sessions.IsLive(ctx, stored.SessionID, now) {
		return response{}, "", badRequest(ErrInvalidGrant, "the refresh token is not valid")
	}

	scope, err := NarrowScope(stored.Scope, ParseScope(form.Get("scope")))
	if err != nil {
		return response{}, "", err
	}

	subject := Subject{
		Issuer:    h.Issuer,
		Audience:  h.Issuer,
		ClientID:  app.ID,
		ProjectID: app.ProjectID,
		OrgID:     stored.OrgID,
		UserID:    stored.UserID,
		SessionID: stored.SessionID,
		Scope:     scope,

		// No ID token on a refresh: nothing was authenticated just now, and an
		// id_token asserting an authentication that did not happen would be a
		// false statement with a fresh timestamp on it.
	}

	out, err := h.issue(ctx, app, subject, scope, false, now)
	return out, stored.UserID, err
}

// --- client_credentials --------------------------------------------------------------

func (h *Handler) clientCredentials(
	ctx context.Context, app client.Application, form formValues, now time.Time,
) (response, error) {
	scope := ParseScope(form.Get("scope"))

	// There is no user, so there is nobody to identify.
	if slices.Contains(scope, "openid") {
		return response{}, badRequest(ErrInvalidScope,
			"the openid scope requires a user; client_credentials has none")
	}

	claims, err := ClientCredentialsClaims(Subject{
		Issuer:    h.Issuer,
		Audience:  h.Issuer,
		ClientID:  app.ID,
		ProjectID: app.ProjectID,
		OrgID:     app.OrgID,
		Scope:     scope,
	}, now)
	if err != nil {
		return response{}, err
	}

	access, err := h.sign(claims, signing.TypeAccessToken)
	if err != nil {
		return response{}, err
	}

	// No refresh token: the client can authenticate again whenever it likes,
	// so a refresh token would be a second, longer-lived credential bought for
	// nothing.
	return response{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(AccessTokenLifetime.Seconds()),
		Scope:       joinScope(scope),
	}, nil
}

// --- issuance -------------------------------------------------------------------------

func (h *Handler) issue(
	ctx context.Context, app client.Application, subject Subject,
	scope []string, withIDToken bool, now time.Time,
) (response, error) {
	accessClaims, err := AccessTokenClaims(subject, now)
	if err != nil {
		return response{}, err
	}
	access, err := h.sign(accessClaims, signing.TypeAccessToken)
	if err != nil {
		return response{}, err
	}

	out := response{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(AccessTokenLifetime.Seconds()),
		Scope:       joinScope(scope),
	}

	if withIDToken && slices.Contains(scope, "openid") {
		idClaims, err := IDTokenClaims(subject, now)
		if err != nil {
			return response{}, err
		}
		if out.IDToken, err = h.sign(idClaims, signing.TypeJWT); err != nil {
			return response{}, err
		}
	}

	if slices.Contains(scope, "offline_access") {
		err := h.DB.WithTenant(ctx, subject.OrgID, func(tx *postgres.Tx) error {
			refresh, _, err := h.Refresh.Issue(ctx, tx, Refresh{
				UserID:    subject.UserID,
				ClientID:  app.ID,
				OrgID:     subject.OrgID,
				SessionID: subject.SessionID,
				Scope:     scope,
			}, "", now)
			if err != nil {
				return err
			}
			out.RefreshToken = refresh.Reveal()
			return nil
		})
		if err != nil {
			return response{}, h.wrap("issuing a refresh token", err)
		}
	}

	return out, nil
}

func (h *Handler) sign(claims Claims, typ string) (string, error) {
	payload, err := claims.Encode()
	if err != nil {
		return "", h.wrap("encoding claims", err)
	}
	signed, err := h.Signer.SignWithType(payload, typ)
	if err != nil {
		return "", h.wrap("signing", err)
	}
	return signed, nil
}

// wrap turns an internal failure into server_error.
//
// The one place internal failure is admitted to the caller, and deliberately:
// a client needs to distinguish "retry, we broke" from "your request is
// wrong". The detail stays in the log.
func (h *Handler) wrap(what string, err error) error {
	if h.Log != nil {
		h.Log.Error("token endpoint failure", "operation", what, "error", err.Error())
	}
	return Error{Code: ErrServerError, Status: http.StatusInternalServerError,
		Description: "the request could not be completed"}
}

// --- responses ---------------------------------------------------------------------------

func (h *Handler) respond(w http.ResponseWriter, out response) {
	w.Header().Set("Content-Type", "application/json")
	// RFC 6749 § 5.1. A cached token response is a token handed to whoever
	// asks the cache next.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)

	// #nosec G117 -- see the comment on `response`: serialising the tokens to
	// the client that earned them is this endpoint's purpose, and this is the
	// one place it happens.
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handler) fail(w http.ResponseWriter, grant string, err error) {
	var oe Error
	if !errors.As(err, &oe) {
		oe = Error{Code: ErrServerError, Status: http.StatusInternalServerError,
			Description: "the request could not be completed"}
	}
	if oe.Status == 0 {
		oe.Status = http.StatusBadRequest
	}

	if h.Observer != nil {
		h.Observer.Denied(grant, oe.Code)
	}

	w.Header().Set("Content-Type", "application/json")
	// On the error path too. An error response carries no token, but it does
	// carry whether a client id is valid, and that is not for a shared cache.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	if oe.Status == http.StatusUnauthorized && oe.Basic {
		// RFC 6749 § 5.2: a 401 answering a Basic attempt must say so.
		w.Header().Set("WWW-Authenticate", `Basic realm="token"`)
	}

	w.WriteHeader(oe.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             oe.Code,
		"error_description": oe.Description,
	})
}

func (h *Handler) internal(w http.ResponseWriter, grant, what string, err error) {
	h.fail(w, grant, h.wrap(what, err))
}

func (h *Handler) audit(ctx context.Context, app client.Application, userID, grant, scope string) {
	if h.Audit == nil {
		return
	}
	_ = h.DB.WithTenant(ctx, app.OrgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       app.OrgID,
			ActorUserID: userID,
			Type:        audit.EventTokenIssued,
			Payload: map[string]any{
				"client_id": app.ID,
				"grant":     grant,
				"scope":     scope,
			},
		})
	})
}

// formValues is the subset of url.Values the grant handlers read.
type formValues interface{ Get(string) string }
