package authorize

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/zed378/zed-auth/backend/internal/oauth/client"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Clients resolves a registered application by its client_id.
//
// An interface rather than the concrete store, so the handler can be tested
// without a database — the parameter and ordering logic is what matters here
// and it should not need Postgres to exercise.
type Clients interface {
	ByClientID(ctx context.Context, clientID string) (client.Application, error)
}

// Sessions resolves a cookie to a live session.
type Sessions interface {
	Lookup(ctx context.Context, token string, policy session.Policy, now time.Time) (session.Session, error)
}

// CodeStore issues codes and holds interrupted requests.
//
// An interface so the handler can be tested without Redis. What these tests
// are about is ordering and parameter handling — whether an error redirects,
// and to where — and needing a container to answer that would mean the
// question got asked less often.
//
// The atomicity that actually matters is a property of the Redis
// implementation, and it is tested against real Redis where it lives.
type CodeStore interface {
	IssueCode(ctx context.Context, c Code, ttl time.Duration) (string, error)
	SavePending(ctx context.Context, r Request, ttl time.Duration) (string, error)

	// PeekPending reads without consuming; LoadPending consumes. The login
	// page (P1-12) renders from the first and finishes with the second, so
	// only a successful authentication spends the request.
	PeekPending(ctx context.Context, id string) (Request, error)
	LoadPending(ctx context.Context, id string) (Request, error)
}

// Observer records which path a request took.
//
// Separate counters for silent and interactive because docs/PLAN/12 sets a latency
// target for the silent path specifically, and an average across both would
// hide it behind the login page's rendering time.
type Observer interface {
	Authorized(path string, d time.Duration)
	Denied(errorCode string)
}

// Handler serves GET /oauth/authorize.
type Handler struct {
	Clients  Clients
	Sessions Sessions
	Store    CodeStore
	DB       *postgres.DB
	Observer Observer
	Log      *slog.Logger

	// LoginPath is where a user without a session is sent. P1-12 serves it.
	LoginPath string

	// Policy is the session policy. P2-10 makes it per organization; until
	// then it is the instance default.
	Policy session.Policy

	// Now is overridable for tests.
	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// ServeHTTP implements the authorization endpoint.
//
// The structure IS the security property, so it is worth reading in order:
//
//  1. Phase 1 — resolve the client and match the redirect URI. Any failure
//     here renders an error page and sends NO redirect, because there is no
//     validated place to send one.
//  2. Phase 2 — everything else. Failures redirect to the now-validated URI
//     with an OAuth error and the original state.
//  3. Session — silent SSO, or the login page, or login_required.
//
// Nothing in step 2 or 3 can emit a redirect that step 1 did not authorise.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := h.now()
	ctx := r.Context()
	q := r.URL.Query()

	// --- Phase 1 -------------------------------------------------------------

	p1, err := ParsePhase1(q)
	if err != nil {
		h.renderError(w, "The request is missing information this service needs to continue.", err.Error())
		return
	}

	app, err := h.Clients.ByClientID(ctx, p1.ClientID)
	if err != nil {
		// Deliberately the same message as a redirect mismatch. Distinguishing
		// "no such client" from "wrong redirect" tells a prober which client
		// ids exist (docs/SECURITY/02 §12).
		h.renderError(w, "This application is not registered, or the address it asked us to return to is not one it registered.",
			"client_id or redirect_uri is not valid")
		return
	}

	if !app.MatchesRedirectURI(p1.RedirectURI) {
		// The single most important branch in this file. The presented
		// redirect_uri is NOT registered, so it must not be redirected to —
		// not even to report this error. Doing so is the open-redirect
		// vulnerability, delivered by the code meant to prevent it.
		h.renderError(w, "This application is not registered, or the address it asked us to return to is not one it registered.",
			"client_id or redirect_uri is not valid")
		return
	}

	// From here on the redirect target is known good, and errors may travel on
	// it.

	// --- Phase 2 -------------------------------------------------------------

	allowsRefresh := slices.Contains(app.GrantTypes, client.GrantRefreshToken)

	req, err := ParsePhase2(q, p1, allowsRefresh)
	if err != nil {
		h.redirectError(w, r, p1.RedirectURI, q.Get("state"), err)
		return
	}

	if !slices.Contains(app.GrantTypes, client.GrantAuthorizationCode) {
		h.redirectError(w, r, req.RedirectURI, req.State, oauthErr(ErrUnauthorizedClient,
			"this client is not permitted to use the authorization code grant"))
		return
	}

	// --- The session ----------------------------------------------------------

	current, hasSession := h.session(ctx, r, req, app)

	switch {
	case hasSession:
		h.issue(w, r, req, app, current, start, "silent")

	case req.HasPrompt(PromptNone):
		// An SPA renewing silently in a hidden iframe needs a machine-readable
		// answer, not a login form it cannot show. This is the whole purpose of
		// prompt=none and getting it wrong strands the renewal.
		h.redirectError(w, r, req.RedirectURI, req.State, oauthErr(ErrLoginRequired,
			"no active session and prompt=none was requested"))

	default:
		h.toLogin(w, r, req)
	}
}

// session returns the usable session for this request, if any.
//
// Returns false when there is none, when prompt=login asked for a fresh
// authentication, when max_age has passed, or when the session belongs to a
// different organization than the client.
func (h *Handler) session(
	ctx context.Context, r *http.Request, req Request, app client.Application,
) (session.Session, bool) {
	// prompt=login means re-authenticate even with a live session. The session
	// is NOT revoked: the user asked to prove themselves again, not to be
	// logged out of everything else they have open.
	if req.HasPrompt(PromptLogin) {
		return session.Session{}, false
	}

	token, ok := session.FromRequest(r)
	if !ok {
		return session.Session{}, false
	}

	now := h.now()
	current, err := h.Sessions.Lookup(ctx, token, h.Policy, now)
	if err != nil {
		// Any failure is "no session", including an infrastructure one.
		// Failing closed here is a login prompt, which is recoverable; failing
		// open would be an unauthenticated code.
		if !errors.Is(err, session.ErrNotFound) && h.Log != nil {
			h.Log.Warn("session lookup failed during authorize", "error", err.Error())
		}
		return session.Session{}, false
	}

	// A client belongs to a project in one organization. A session for another
	// organization is not a session for this client, however live it is.
	if current.OrgID != app.OrgID {
		return session.Session{}, false
	}

	// max_age: the client is asking how recently the user actually
	// authenticated, which is not the same as how recently they were active.
	if req.MaxAge != nil && now.Sub(current.CreatedAt) > *req.MaxAge {
		return session.Session{}, false
	}

	return current, true
}

// issue mints a code and redirects to the client.
//
// `path` is which route got here — "silent" for an existing session, "login"
// for one just established by P1-12. Recorded separately because docs/PLAN/12 sets
// a latency target for the silent path specifically, and averaging it with a
// path that includes a human typing a password would hide it entirely.
func (h *Handler) issue(
	w http.ResponseWriter, r *http.Request,
	req Request, app client.Application, current session.Session, start time.Time, path string,
) {
	now := h.now()

	code, err := h.Store.IssueCode(r.Context(), Code{
		ClientID:      app.ID,
		RedirectURI:   req.RedirectURI,
		UserID:        current.UserID,
		OrgID:         current.OrgID,
		SessionID:     current.ID,
		Scope:         req.Scope,
		Nonce:         req.Nonce,
		CodeChallenge: req.CodeChallenge,
		AuthMethods:   current.AuthMethods,
		AuthTime:      current.CreatedAt,
		IssuedAt:      now,
	}, CodeTTL)
	if err != nil {
		// A code that could not be stored must not be issued: it could never
		// be redeemed, and the client would see a code that silently fails.
		if h.Log != nil {
			h.Log.Error("issuing authorization code failed", "error", err.Error())
		}
		h.redirectError(w, r, req.RedirectURI, req.State,
			oauthErr(ErrServerError, "the authorization code could not be issued"))
		return
	}

	target, err := url.Parse(req.RedirectURI)
	if err != nil {
		// Unreachable: the URI was validated at registration and matched
		// exactly here. Handled anyway rather than ignored.
		h.renderError(w, "Something went wrong completing your sign-in.", "redirect_uri could not be parsed")
		return
	}

	query := target.Query()
	query.Set("code", code)
	query.Set("state", req.State)
	target.RawQuery = query.Encode()

	if h.Observer != nil {
		h.Observer.Authorized(path, h.now().Sub(start))
	}

	http.Redirect(w, r, target.String(), http.StatusFound)
}

// toLogin stores the request and sends the browser to the login page.
func (h *Handler) toLogin(w http.ResponseWriter, r *http.Request, req Request) {
	id, err := h.Store.SavePending(r.Context(), req, PendingTTL)
	if err != nil {
		if h.Log != nil {
			h.Log.Error("storing the pending authorization request failed", "error", err.Error())
		}
		h.redirectError(w, r, req.RedirectURI, req.State,
			oauthErr(ErrServerError, "the request could not be continued"))
		return
	}

	target, err := url.Parse(h.LoginPath)
	if err != nil {
		h.renderError(w, "Something went wrong starting your sign-in.", "login path is misconfigured")
		return
	}
	query := target.Query()
	// The opaque id and nothing else. Carrying the original parameters through
	// the login page would put a redirect_uri in a URL the user can edit, and
	// re-validating it on the way back would be a second place for the exact
	// match rule to be got wrong.
	query.Set("request", id)
	target.RawQuery = query.Encode()

	if h.Observer != nil {
		h.Observer.Authorized("login_required", 0)
	}

	http.Redirect(w, r, target.String(), http.StatusFound)
}

// redirectError sends an OAuth error to the validated redirect URI.
//
// Only ever called after phase 1 succeeded. `state` is echoed unchanged,
// including when it was the thing that was invalid — the client needs it to
// correlate the failure with the request it made.
func (h *Handler) redirectError(w http.ResponseWriter, r *http.Request, redirectURI, state string, err error) {
	var oe Error
	if !errors.As(err, &oe) {
		oe = oauthErr(ErrServerError, "the request could not be completed")
	}

	if h.Observer != nil {
		h.Observer.Denied(oe.Code)
	}

	target, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		h.renderError(w, "Something went wrong.", "redirect_uri could not be parsed")
		return
	}

	query := target.Query()
	query.Set("error", oe.Code)
	if oe.Description != "" {
		query.Set("error_description", oe.Description)
	}
	if state != "" {
		query.Set("state", state)
	}
	target.RawQuery = query.Encode()

	http.Redirect(w, r, target.String(), http.StatusFound)
}

// errorPage is the response when no redirect is permitted.
//
// Plain and deliberately unhelpful about specifics: the person reading it is
// an end user who cannot fix it, and the person who can — the integrator —
// reads `detail`, which names the parameter without naming the expected value.
var errorPage = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Sign-in error</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
body{font:16px/1.6 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;
     color:#15181d;background:#f6f7f9;margin:0;display:grid;place-items:center;min-height:100vh;padding:24px}
main{background:#fff;border:1px solid #828d9c;border-radius:12px;padding:32px;max-width:34rem}
h1{font-size:1.25rem;margin:0 0 12px}
p{margin:0 0 12px}
code{font:14px ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:#5b6472}
</style></head>
<body><main>
<h1>We can't complete this sign-in</h1>
<p>{{.Message}}</p>
<p>Nothing is wrong with your account. Please contact the application that sent you here.</p>
<p><code>{{.Detail}}</code></p>
</main></body></html>
`))

// renderError responds WITHOUT a redirect.
//
// The absence of a Location header is the security property, which is why this
// is a separate method rather than a flag on redirectError: the two must not
// be one function with a boolean, because the boolean is the thing that gets
// passed wrongly.
func (h *Handler) renderError(w http.ResponseWriter, message, detail string) {
	if h.Observer != nil {
		h.Observer.Denied("invalid_client_or_redirect")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)

	_ = errorPage.Execute(w, struct{ Message, Detail string }{Message: message, Detail: detail})
}

// --- the seam the login page finishes through -----------------------------------

// Pending is an interrupted authorization request, as the login page needs it.
//
// The Request and the Application together, because both were resolved once
// already and re-deriving either on the way back would be a second place for
// the exact-match rule to be got wrong.
type Pending struct {
	ID      string
	Request Request
	App     client.Application
}

// Peek reads a pending request without consuming it.
//
// For rendering: the login page needs the organization (for branding and for
// which users it may authenticate) and the application's name, and it must be
// able to render more than once — a refresh, a back button, a mistyped
// password. Nothing here is echoed to the browser except the application's
// registered name.
func (h *Handler) Peek(ctx context.Context, id string) (Pending, error) {
	req, err := h.Store.PeekPending(ctx, id)
	if err != nil {
		return Pending{}, err
	}

	app, err := h.Clients.ByClientID(ctx, req.ClientID)
	if err != nil {
		// The request was stored after this client resolved, so the client has
		// been deleted since, or the database is unreachable. Either way the
		// flow cannot continue and there is nowhere to send an OAuth error:
		// req.RedirectURI was validated against a registration that no longer
		// exists, so redirecting to it now would be redirecting to an address
		// nothing currently registers.
		return Pending{}, fmt.Errorf("authorize: resolving the client of a pending request: %w", err)
	}

	return Pending{ID: id, Request: req, App: app}, nil
}

// Resume completes a pending authorization with a session that has just been
// established.
//
// This is the seam P1-12 needs, and what it deliberately does NOT do is as
// important as what it does. It does not re-parse parameters, re-check the
// client's grant types, or re-match the redirect URI: all of that happened in
// ServeHTTP before the request was stored, on the values that were stored.
// Doing it twice would put the exact-match rule in two places, and two places
// is how one of them ends up subtly different.
//
// What it does do is consume the request — this is the single-use point — and
// verify that the session belongs to the client's organization.
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request, id string, current session.Session) {
	start := h.now()

	// GETDEL. From here the id is spent, so a second tab arriving with the
	// same id gets the expired page rather than a second code.
	req, err := h.Store.LoadPending(r.Context(), id)
	if err != nil {
		if !errors.Is(err, ErrPendingNotFound) && h.Log != nil {
			h.Log.Error("loading a pending authorization request failed", "error", err.Error())
		}
		h.renderError(w,
			"This sign-in took too long, or has already been completed.",
			"the pending authorization request is no longer available")
		return
	}

	app, err := h.Clients.ByClientID(r.Context(), req.ClientID)
	if err != nil {
		h.renderError(w, "Something went wrong completing your sign-in.",
			"the application could not be resolved")
		return
	}

	// The login page authenticated against the organization it read from this
	// same client, so a mismatch is a bug or a swapped request id rather than
	// an ordinary condition. Checked anyway: the cost is one comparison and
	// the failure it prevents is a code issued across tenants.
	if current.OrgID != app.OrgID {
		if h.Log != nil {
			h.Log.Error("a session was presented to resume a request for another organization",
				"session_org_id", current.OrgID, "client_org_id", app.OrgID)
		}
		h.renderError(w, "Something went wrong completing your sign-in.",
			"the session does not belong to this application's organization")
		return
	}

	h.issue(w, r, req, app, current, start, "login")
}
