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

	// RefreshReuse counts a rotated refresh token presented again (P3-06).
	//
	// Its own metric rather than a `Denied` label, because the two are read
	// differently: `docs/PLAN/13` § Alerting expects token errors to have a
	// non-zero baseline — expired tokens, clients that never clean up — and
	// reuse has none. An operator pages on ANY of this, which is only possible
	// if it is not buried in a counter that is always moving.
	RefreshReuse()
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

	// Roles supplies the role claims (P2-04). An interface, so this package
	// does not need to know how grants are stored — the same arrangement
	// Clients, Codes and Sessions use.
	//
	// A nil Roles issues tokens with an EMPTY role claim rather than failing.
	// That is the safe direction: a consumer reading no roles denies access it
	// might have granted, which is recoverable, where failing token issuance
	// would take down every login in the deployment.
	Roles Roles

	Now func() time.Time
}

// Roles reads what a token should say about a user (P2-04).
type Roles interface {
	ForToken(ctx context.Context, orgID, userID, projectID string) (RoleClaims, error)
}

// RoleClaims are the two role sets a token carries.
type RoleClaims struct {
	Keys    []string
	Manager []string
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

	out, err := h.issue(ctx, app, subject, code.Scope, true, true, now)
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

	// Reuse detection comes FIRST, before the liveness lookup (P3-06).
	//
	// It has to: rotation leaves the predecessor un-revoked but linked, and a
	// family kill revokes everything — so by the time `Lookup` filters a token
	// out as dead, the evidence of what happened to it is gone. Asking the
	// lineage first is what makes "this was rotated away" distinguishable from
	// "this was never real".
	reuse, supersede, err := h.detectReuse(ctx, presented, app, now)
	if err != nil {
		return response{}, "", err
	}
	if reuse {
		// Answered exactly as every other refusal is. A thief who could tell
		// "reuse detected" from "unknown token" would learn that their copy was
		// genuine and that they have been caught.
		return response{}, "", badRequest(ErrInvalidGrant, "the refresh token is not valid")
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

	out, err := h.issue(ctx, app, subject, scope, false, false, now)
	if err != nil {
		return response{}, stored.UserID, err
	}

	// The successor, in the SAME family and inheriting its absolute expiry
	// (P3-06 F-1, F-5).
	//
	// Replacing what `issue` would otherwise have minted: that path starts a
	// fresh family with a fresh absolute lifetime, which is how a session could
	// be extended forever by refreshing — and, because it left `replaced_by`
	// unwritten, why reuse detection was impossible rather than merely absent.
	rotated, err := h.rotate(ctx, stored, scope, supersede, now)
	if err != nil {
		return response{}, stored.UserID, err
	}
	out.RefreshToken = rotated

	return out, stored.UserID, nil
}

// detectReuse reports whether a presented token has already been rotated away,
// and kills its family when it has.
//
// A token this service never issued answers "not reuse", and that is clarity
// rather than a control: a family kill needs a real, rotated lineage, so an
// invented token could not reach one however this branch answered. An earlier
// comment here claimed it stopped somebody killing a family by guessing — a
// mutation run showed the branch makes no behavioural difference at all, which
// is how the overstatement was found. The guard stays because a function that
// reported an unknown token as reuse would be lying to its caller, and the next
// caller might act on it.
//
// It also reports the successor a legitimate retry must supersede, empty on
// every other path.
func (h *Handler) detectReuse(
	ctx context.Context, presented string, app client.Application, now time.Time,
) (reuse bool, supersede string, err error) {
	if h.Refresh == nil {
		return false, "", nil
	}

	lineage, err := h.Refresh.LookupLineage(ctx, h.DB, presented)
	switch {
	case errors.Is(err, ErrRefreshNotFound):
		return false, "", nil
	case err != nil:
		return false, "", h.wrap("reading a refresh token's lineage", err)
	}

	if !lineage.Rotated() {
		return false, "", nil
	}

	// The legitimate race: a client retrying after a response it never
	// received. Admitted only while the successor is untouched — see
	// Lineage.LegitimateRetry, where the reasoning lives.
	if lineage.LegitimateRetry(now, RotationGrace) {
		if h.Log != nil {
			h.Log.Info("a refresh token was presented again within the rotation grace window",
				"client_id", app.ID, "family_id", lineage.FamilyID)
		}
		return false, lineage.ReplacedBy, nil
	}

	if lineage.Revoked {
		// The family is already dead — this is a second presentation after the
		// kill. Refused, but not re-killed and not re-alerted: an attacker
		// retrying a dead token should not be able to generate an alert per
		// attempt.
		return true, "", nil
	}

	h.killFamily(ctx, lineage, app, now)
	return true, "", nil
}

// killFamily revokes an entire lineage and raises the alarm (F-2, F-3).
//
// Every token descended from one original issuance, including the successor the
// legitimate client is holding right now. That logs out the victim along with
// the thief, and it is the right trade: the alternative is deciding which of
// two identical presentations is genuine, which cannot be done. The alert is
// what makes it actionable rather than merely disruptive.
func (h *Handler) killFamily(
	ctx context.Context, lineage Lineage, app client.Application, now time.Time,
) {
	var revoked int64

	err := h.DB.WithTenant(ctx, lineage.OrgID, func(tx *postgres.Tx) error {
		count, err := h.Refresh.RevokeFamily(ctx, tx, lineage.FamilyID)
		if err != nil {
			return err
		}
		revoked = count

		if h.Audit == nil {
			return nil
		}
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID: lineage.OrgID,
			Type:  audit.EventRefreshReuseDetected,
			Payload: map[string]any{
				"family_id":      lineage.FamilyID,
				"client_id":      app.ID,
				"tokens_revoked": revoked,
				// How long after rotation the stale token appeared. An
				// operator triaging this wants to know whether it was seconds
				// (a broken client, probably) or hours (a copy).
				"seconds_after_rotation": int(now.Sub(lineage.ReplacedAt).Seconds()),
			},
		})
	})
	if err != nil && h.Log != nil {
		h.Log.Error("failed to revoke a refresh family after reuse was detected",
			"family_id", lineage.FamilyID, "error", err.Error())
	}

	if h.Log != nil {
		// ERROR, not warn. `docs/PLAN/13` § Alerting: ordinary refresh failures
		// are routine — expired tokens, clients that never clean up — and reuse
		// never is. It means a credential was copied or a client is broken, and
		// both want a human.
		h.Log.Error("a rotated refresh token was presented again; the family has been revoked",
			"family_id", lineage.FamilyID, "client_id", app.ID, "tokens_revoked", revoked)
	}
	if h.Observer != nil {
		h.Observer.RefreshReuse()
	}
}

// rotate issues the successor to a presented token.
func (h *Handler) rotate(
	ctx context.Context, stored Refresh, scope []string, supersede string, now time.Time,
) (string, error) {
	var plaintext string

	err := h.DB.WithTenant(ctx, stored.OrgID, func(tx *postgres.Tx) error {
		successor, err := h.Refresh.Rotate(ctx, tx, Refresh{
			ID:        stored.ID,
			UserID:    stored.UserID,
			ClientID:  stored.ClientID,
			OrgID:     stored.OrgID,
			SessionID: stored.SessionID,
			FamilyID:  stored.FamilyID,
			Scope:     scope,
		}, supersede, now)
		if err != nil {
			return err
		}
		plaintext = successor.Reveal()
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRefreshReuse) || errors.Is(err, ErrRefreshNotFound) {
			return "", badRequest(ErrInvalidGrant, "the refresh token is not valid")
		}
		return "", h.wrap("rotating a refresh token", err)
	}
	return plaintext, nil
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

// rolesFor reads the role claims, or reports none.
//
// A failure here does NOT fail the token. `docs/PLAN/08` is explicit that
// claims are a point-in-time snapshot and that a consumer needing certainty
// asks `/v1/authz/check` — so a token with no roles is a token that grants
// less, which every consumer already has to handle, whereas refusing to issue
// one takes down every login in the deployment because one query failed.
//
// It is logged at ERROR, because a service quietly issuing role-less tokens is
// an outage that looks like a permissions bug to everybody downstream.
func (h *Handler) rolesFor(ctx context.Context, subject Subject, app client.Application) ([]string, []string) {
	if h.Roles == nil || subject.UserID == "" {
		return nil, nil
	}

	claims, err := h.Roles.ForToken(ctx, subject.OrgID, subject.UserID, app.ProjectID)
	if err != nil {
		if h.Log != nil {
			h.Log.Error("a token was issued without role claims",
				"error", err.Error(), "client_id", app.ID, "project_id", app.ProjectID)
		}
		return nil, nil
	}
	return claims.Keys, claims.Manager
}

// --- issuance -------------------------------------------------------------------------

func (h *Handler) issue(
	ctx context.Context, app client.Application, subject Subject,
	scope []string, withIDToken bool, mintRefresh bool, now time.Time,
) (response, error) {
	// Read here rather than at each call site, so both grants that produce a
	// user token — the authorization code and the refresh — carry the same
	// claims by construction. A refresh token minted before a role was granted
	// therefore produces a token that HAS it: the claims are a snapshot of
	// now, not of when the session began.
	subject.RoleKeys, subject.ManagerRoles = h.rolesFor(ctx, subject, app)

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

	// `mintRefresh` is false on the REFRESH path, where a successor is rotated
	// from the presented token instead (P3-06).
	//
	// Without the flag this branch also fired, so every refresh produced TWO
	// tokens: a successor in the right family, and an orphan starting a brand
	// new one with a brand new absolute lifetime. The orphan is what made
	// `family_expires_at` look extendable and what left `replaced_by`
	// pointing at a lineage nobody was using.
	if mintRefresh && slices.Contains(scope, "offline_access") {
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
