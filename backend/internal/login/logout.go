package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/signing"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// RP-initiated logout (OpenID Connect RP-Initiated Logout 1.0).
//
// It lives in this package rather than its own because the confirmation
// interstitial needs everything the login page already has — the CSRF
// machinery, the hashed-CSP shell, the header discipline, the branding read —
// and a separate package would have to export all of it across a boundary to
// render one more page. The package is "the browser pages this service
// serves"; the doc comment on handler.go says so.
//
// The security core is one rule, in shouldActWithoutAsking(): a GET logs
// somebody out only when the request proves it was initiated by a party that
// already holds a token for that very session. Everything else is a click.
//
// Specification: MEMORY/specs/P1-10-logout.md.

// LogoutPath is where this handler is mounted.
const LogoutPath = "/oidc/logout"

// Scope is how much a logout ended.
type Scope string

const (
	// ScopeSession ends the browser's current session and nothing else.
	ScopeSession Scope = "session"

	// ScopeAll ends every session the user has and revokes their refresh
	// tokens. docs/PLAN/05 names this an MVP requirement.
	ScopeAll Scope = "all"
)

// LogoutSessions is what this handler does with sessions.
type LogoutSessions interface {
	Lookup(ctx context.Context, presented string, policy session.Policy, now time.Time) (session.Session, error)
	Revoke(ctx context.Context, tx *postgres.Tx, sessionID string, reason session.Reason, actorUserID string, now time.Time) (func(context.Context) error, error)
	RevokeAllForUser(ctx context.Context, tx *postgres.Tx, userID, orgID, actorUserID string, now time.Time) (func(context.Context) error, error)
}

// RefreshRevoker ends a user's refresh tokens.
//
// Step 4 of the card: "log out of all sessions" terminates every session AND
// revokes their refresh tokens. Without the second half a user who logs out
// everywhere still has live refresh tokens, and an application holding one
// mints a new access token minutes later — which is not what anybody means by
// the button.
type RefreshRevoker interface {
	RevokeAllForUser(ctx context.Context, tx *postgres.Tx, userID string) (int64, error)
}

// Verifier checks the id_token_hint.
type Verifier interface {
	Verify(compact, wantType string) ([]byte, error)
}

// LogoutObserver counts outcomes.
type LogoutObserver interface {
	Logout(outcome string)
}

const (
	LogoutCompleted = "completed"
	LogoutAsked     = "asked"
	LogoutRefused   = "refused"
	LogoutError     = "error"
)

// LogoutHandler serves GET and POST /oidc/logout.
type LogoutHandler struct {
	Issuer    string
	Clients   Clients
	Sessions  LogoutSessions
	Refresh   RefreshRevoker
	Verifier  Verifier
	Brandings Brandings
	DB        Tenant
	Audit     Auditor
	Observer  LogoutObserver
	Log       *slog.Logger

	Policy session.Policy

	Now func() time.Time
}

// Clients resolves a registered application.
type Clients interface {
	ByClientID(ctx context.Context, clientID string) (client.Application, error)
}

func (h *LogoutHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// request is a parsed and validated logout request.
type request struct {
	// App is the client this logout belongs to, when one resolved. Zero when
	// no hint and no client_id were supplied, which is legitimate: a user can
	// reach this page from a bookmark.
	App client.Application

	// RedirectURI is set only when it EXACTLY matched App's registered list.
	// An unvalidated value never reaches this struct, so no code path can
	// redirect to one.
	RedirectURI string

	State string

	// Hint is the verified id_token_hint's claims, or nil.
	Hint map[string]any
}

// ServeHTTP routes the two methods.
func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.begin(w, r)
	case http.MethodPost:
		h.confirm(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		h.notice(w, r, http.StatusMethodNotAllowed, Notice{
			Title: "Method not allowed",
			Body:  "This page accepts GET and POST.",
		})
	}
}

// --- GET ----------------------------------------------------------------------

func (h *LogoutHandler) begin(w http.ResponseWriter, r *http.Request) {
	req, err := h.parse(r, r.URL.Query())
	if err != nil {
		h.refuse(w, r, err)
		return
	}

	current, hasSession := h.currentSession(r)

	if h.shouldActWithoutAsking(req, current, hasSession) {
		h.finish(w, r, req, current, ScopeSession)
		return
	}

	h.ask(w, r, req)
}

// shouldActWithoutAsking is the whole security decision of this endpoint.
//
// A GET is triggerable by any page on the internet — an <img>, a prefetch, a
// link in an email — so acting on one is a cross-site request forgery whose
// payload is "log this person out" (docs/SECURITY/02 §5). What makes a
// particular GET safe to act on is proof that it came from a party which
// already holds a token for THIS session, and the id_token_hint is that proof:
// only somebody who completed the flow has one.
//
// So: a hint that verifies, for the session the cookie resolves to. A hint for
// a different session logs nobody out — not its own subject, and not the
// cookie's owner — it simply falls through to the interstitial, where the user
// decides.
func (h *LogoutHandler) shouldActWithoutAsking(
	req request, current session.Session, hasSession bool,
) bool {
	if !hasSession || req.Hint == nil {
		return false
	}

	sid, _ := req.Hint["sid"].(string)
	if sid == "" || sid != current.ID {
		return false
	}

	// The subject too. A hint whose `sid` matched but whose `sub` did not
	// would mean the token and the session disagree about whose they are,
	// which is not a request to act on.
	sub, _ := req.Hint["sub"].(string)
	return sub != "" && sub == current.UserID
}

// --- POST ---------------------------------------------------------------------

func (h *LogoutHandler) confirm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.badRequest(w, r, "the form could not be read")
		return
	}

	// Re-validated, not trusted. The parameters travel through the
	// interstitial in hidden fields rather than by an opaque server-side
	// reference the way P1-12 carries a pending authorization request, and the
	// difference is deliberate: a pending authorization has seven parameters
	// whose validation ORDER is itself the security property, so re-deriving
	// it is a second place to get the ordering wrong. Logout has one parameter
	// that matters and one function that validates it, called unconditionally
	// on both paths — so a hidden field the user can edit is a hidden field
	// this same check rejects.
	req, err := h.parse(r, r.PostForm)
	if err != nil {
		h.refuse(w, r, err)
		return
	}

	if !checkCSRF(r, r.PostForm.Get(csrfField)) {
		// A stale tab far more often than an attack. The form comes back with
		// a fresh token.
		h.observe(LogoutRefused)
		h.render(w, r, req, MsgSessionProblem)
		return
	}

	current, hasSession := h.currentSession(r)
	if !hasSession {
		// Already signed out. Not an error — a logout link clicked twice, or
		// clicked after the session expired, should still take the user where
		// the application said to send them.
		h.done(w, r, req)
		return
	}

	scope := ScopeSession
	if r.PostForm.Get("everywhere") != "" {
		scope = ScopeAll
	}

	h.finish(w, r, req, current, scope)
}

// --- doing it ---------------------------------------------------------------------

// finish revokes, clears the cookie, audits, and sends the user on.
func (h *LogoutHandler) finish(
	w http.ResponseWriter, r *http.Request, req request, current session.Session, scope Scope,
) {
	invalidate, count, err := h.revoke(r.Context(), current, scope)
	if err != nil {
		if h.Log != nil {
			h.Log.Error("logging out failed",
				"session_id", current.ID, "scope", string(scope), "error", err.Error())
		}
		h.observe(LogoutError)
		// The user must not be told they are signed out when they are not.
		h.notice(w, r, http.StatusInternalServerError, Notice{
			Title: "We could not sign you out",
			Body:  "Please try again in a moment. Your session is still active.",
		})
		return
	}

	// After the commit, never before: a cache invalidated ahead of the commit
	// can be repopulated from the pre-commit state by a concurrent reader.
	// This is the ordering P1-11 was built around, and the reason Revoke hands
	// the invalidation back as a function rather than doing it inline.
	if invalidate != nil {
		if err := invalidate(r.Context()); err != nil && h.Log != nil {
			h.Log.Warn("invalidating a session after logout failed",
				"session_id", current.ID, "error", err.Error())
		}
	}

	// Cleared as WELL as revoked, never instead. A cleared cookie the user
	// already copied still works if the row survives, which is the whole
	// reason step 3 of the card says so explicitly.
	http.SetCookie(w, session.ClearCookie())

	if h.Log != nil {
		h.Log.Info("a user signed out",
			"session_id", current.ID, "scope", string(scope), "sessions_ended", count)
	}
	h.observe(LogoutCompleted)
	h.done(w, r, req)
}

// revoke ends what the scope says, inside one transaction.
func (h *LogoutHandler) revoke(
	ctx context.Context, current session.Session, scope Scope,
) (func(context.Context) error, int64, error) {
	var (
		invalidate func(context.Context) error
		ended      int64
	)
	now := h.now()

	err := h.DB.WithTenant(ctx, current.OrgID, func(tx *postgres.Tx) error {
		var err error

		if scope == ScopeAll {
			invalidate, err = h.Sessions.RevokeAllForUser(ctx, tx, current.UserID, current.OrgID, current.UserID, now)
			if err != nil {
				return err
			}

			// The other half of "log out everywhere". Without it a user who
			// pressed the button still has live refresh tokens, and an
			// application holding one mints a fresh access token minutes
			// later — which is not what anybody means by the button.
			if h.Refresh != nil {
				if _, err := h.Refresh.RevokeAllForUser(ctx, tx, current.UserID); err != nil {
					return err
				}
			}
		} else {
			invalidate, err = h.Sessions.Revoke(ctx, tx, current.ID, session.ReasonLogout, current.UserID, now)
			if err != nil {
				return err
			}
		}

		ended = 1
		payload := map[string]any{
			// The session id, not the cookie. PG-14's separation of credential
			// from identifier is what makes this line safe to write.
			"session_id": current.ID,
			"scope":      string(scope),
		}
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       current.OrgID,
			ActorUserID: current.UserID,
			Type:        audit.EventLogout,
			Payload:     payload,
		})
	})

	return invalidate, ended, err
}

// --- parsing and validation ---------------------------------------------------------

// errNoRedirect means the presented post_logout_redirect_uri is not one this
// client registered, or the client is unknown.
//
// One error for both, deliberately: distinguishing them would say which client
// ids exist to anyone who can send a request (docs/SECURITY/02 §12).
var errNoRedirect = errors.New("login: the post-logout address is not registered for this client")

// parse reads and validates the request, from either the query or the form.
//
// One function for both methods, so there is no path that validates the
// redirect target differently — or not at all.
func (h *LogoutHandler) parse(r *http.Request, values url.Values) (request, error) {
	hint, err := h.verifyHint(single(values, "id_token_hint"))
	if err != nil && h.Log != nil {
		// An unverifiable hint is treated as absent rather than refused: the
		// user still wants to sign out, and the worst case is that they are
		// asked to confirm.
		h.Log.Info("an id_token_hint could not be verified", "reason", err.Error())
	}

	out := request{
		Hint:  hint,
		State: single(values, "state"),
	}

	clientID := single(values, "client_id")
	if hint != nil {
		// The hint decides, because it is the half of the request that is
		// signed. A client_id parameter beside a hint for another client is a
		// caller confusing itself, not an instruction.
		if aud, ok := hint["aud"].(string); ok && aud != "" {
			clientID = aud
		}
	}

	presented := single(values, "post_logout_redirect_uri")

	if clientID != "" {
		app, err := h.Clients.ByClientID(r.Context(), clientID)
		if err == nil {
			out.App = app
		}
	}

	if presented == "" {
		// Nothing to validate and nowhere to send them afterwards. The
		// "you are signed out" page is the answer.
		return out, nil
	}

	// From here a redirect target was asked for, so it must be registered.
	// An unvalidated URI never reaches the request struct, which is what stops
	// any later code path from redirecting to one.
	if out.App.ID == "" || !out.App.MatchesPostLogoutRedirectURI(presented) {
		return request{}, errNoRedirect
	}

	out.RedirectURI = presented
	return out, nil
}

// verifyHint validates an id_token_hint and returns its claims.
func (h *LogoutHandler) verifyHint(compact string) (map[string]any, error) {
	if compact == "" {
		return nil, nil
	}
	if h.Verifier == nil {
		return nil, errors.New("no verifier is configured")
	}

	// TypeJWT: an id_token, not an access token. Presenting an access token
	// here would be the same substitution abuse case A-5 covers, arriving at a
	// third endpoint.
	payload, err := h.Verifier.Verify(compact, signing.TypeJWT)
	if err != nil {
		return nil, fmt.Errorf("signature or type: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("the hint payload is not JSON")
	}

	if issuer, _ := claims["iss"].(string); issuer != h.Issuer {
		return nil, errors.New("the hint names another issuer")
	}

	// Deliberately NOT checked: expiry. An id_token is short-lived by design
	// (five minutes) and a user signing out an hour later is the ordinary
	// case. The hint is evidence of who initiated the request, not a
	// credential being honoured — and it still has to match the live session,
	// which is the check that actually bounds it.
	return claims, nil
}

// currentSession resolves the cookie.
func (h *LogoutHandler) currentSession(r *http.Request) (session.Session, bool) {
	presented, ok := session.FromRequest(r)
	if !ok {
		return session.Session{}, false
	}
	current, err := h.Sessions.Lookup(r.Context(), presented, h.Policy, h.now())
	if err != nil {
		return session.Session{}, false
	}
	return current, true
}

// single returns a parameter, refusing duplicates by taking neither.
//
// The same rule P1-06 applies: taking the first or the last value is how
// parameter-pollution bypasses get in, because a validator reads one and a
// consumer reads the other.
func single(values url.Values, name string) string {
	if len(values[name]) != 1 {
		return ""
	}
	return values[name][0]
}
