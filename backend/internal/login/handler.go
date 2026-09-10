// Package login serves the hosted login page — the one place in the estate
// where a password is ever typed.
//
// Two routes, both HTML:
//
//	GET  /login?request=<id>   render the form
//	POST /login                verify, create a session, resume the flow
//
// Server-rendered by the auth service itself rather than by the console
// (FR-1), so that signing in does not depend on the console being deployed,
// reachable, or working.
//
// The property that shapes everything here is that a wrong password and an
// address with no account must be indistinguishable — in the body, the status,
// every header, and the time taken. It is easy to satisfy by accident and just
// as easy to lose by accident, so the uniform answer is a named constant, the
// equal-cost path lives inside authn.UserStore.Authenticate, and the
// integration test compares whole responses byte for byte rather than
// checking that both say something unhelpful.
//
// Specification: MEMORY/specs/P1-12-login-page.md.
package login

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// Path and ForgotPath are where this handler is mounted.
const (
	Path       = "/login"
	ForgotPath = "/login/forgot"
)

// maxFormBytes bounds the submitted form.
//
// The endpoint is unauthenticated and anybody may POST to it. 8 KiB is far
// more than an address and a password and two opaque tokens, and small enough
// that the endpoint is not a place to push bytes.
const maxFormBytes = 8 << 10

// maxPasswordBytes bounds what is hashed.
//
// Argon2 cost is independent of input length, so this is not a denial-of-
// service bound so much as a sanity one — and it matches the ceiling
// authn.Hash already enforces, so a password that would be refused at
// registration is refused here rather than hashed and compared pointlessly.
const maxPasswordBytes = 1024

// Authorization is the seam back into the OAuth flow.
//
// An interface rather than *authorize.Handler so this package can be tested
// without Redis and without a client store — and so the direction of the
// dependency is stated: login knows how to finish an authorization, authorize
// knows nothing about login beyond a path to redirect to.
type Authorization interface {
	Peek(ctx context.Context, id string) (authorize.Pending, error)
	Resume(w http.ResponseWriter, r *http.Request, id string, current session.Session)
}

// Sessions is what this handler does with sessions.
type Sessions interface {
	Create(ctx context.Context, tx *postgres.Tx, in session.New, policy session.Policy, now time.Time) (session.Session, session.Token, error)
	Lookup(ctx context.Context, presented string, policy session.Policy, now time.Time) (session.Session, error)
	Revoke(ctx context.Context, tx *postgres.Tx, sessionID string, reason session.Reason, actorUserID string, now time.Time) (func(context.Context) error, error)
}

// Users authenticates a person.
type Users interface {
	Authenticate(ctx context.Context, tx *postgres.Tx, email, password string) (authn.User, bool, error)
	RecordRehash(ctx context.Context, tx *postgres.Tx, userID, password string) error
}

// Policies reads the organization's password policy, for expiry.
type Policies interface {
	Policy(ctx context.Context, tx *postgres.Tx, orgID string) (authn.Policy, error)
}

// Auditor records what happened.
//
// An interface for the same reason as Tenant: audit.Writer needs a real
// transaction, and the handler's response behaviour should be answerable
// without one.
type Auditor interface {
	Write(ctx context.Context, tx *postgres.Tx, e audit.Event) error
}

// Tenant runs work inside a tenant-scoped transaction.
//
// An interface over *postgres.DB rather than the type itself, so the handler's
// routing, headers, CSRF and uniformity can be tested without a container.
// Those are the properties most worth asking about often, and a test that
// needs Postgres to answer "did this response set frame-ancestors" is a test
// that gets run less.
//
// The properties that genuinely need a database — the byte-identical failure
// responses, the equal-cost not-found path — are tested against a real one.
type Tenant interface {
	WithTenant(ctx context.Context, orgID string, fn func(*postgres.Tx) error) error
}

// Branding reads the organization's logo and accent.
type Brandings interface {
	Branding(ctx context.Context, tx *postgres.Tx, orgID string) (Branding, []Rejection, error)
}

// Limiter bounds how often a credential may be guessed.
//
// Optional: nil means no limiting, which is what every test that is not about
// limiting wants. In production it is always set, and the startup wiring is
// what makes that true rather than a comment here.
type Limiter interface {
	// Check runs BEFORE the password is verified, which is the point: a
	// refused attempt must not cost an Argon2 computation.
	Check(ctx context.Context, address, ip string, now time.Time) (ratelimit.Decision, string)

	// Fail records a failure and reports whether it began a cooldown, so the
	// caller audits once per cooldown rather than once per attempt.
	Fail(ctx context.Context, address, ip string, now time.Time) (bool, string)

	// Succeed clears the address counter (FR-9).
	Succeed(ctx context.Context, address string)
}

// ClientIP resolves the address a request came from.
//
// An interface rather than a bare function so this handler cannot quietly go
// back to reading RemoteAddr: what counts as a client IP is a deployment
// decision (httpserver.ClientIP) and this package should not re-decide it.
type ClientIP interface {
	Of(r *http.Request) string
}

// Observer counts attempts.
//
// The outcome vocabulary is deliberately coarser than the internal one:
// "failed" covers a wrong password, an unknown address and a locked account
// alike, because a metric that separated them would put the enumeration
// disclosure in Prometheus instead of in the response body — and /metrics is
// scraped, stored and often more widely readable than the audit log.
type Observer interface {
	LoginAttempt(outcome string)
}

const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
	OutcomeError   = "error"

	// OutcomeRateLimited is counted separately from OutcomeFailed because the
	// two mean opposite things operationally: a rise in "failed" is people
	// mistyping or an attack getting through, a rise in "rate_limited" is the
	// control working. Averaging them together would hide both.
	OutcomeRateLimited = "rate_limited"
)

// Handler serves the login page.
type Handler struct {
	Authorization Authorization
	Sessions      Sessions
	Users         Users
	Policies      Policies
	Brandings     Brandings
	DB            Tenant
	Audit         Auditor
	Observer      Observer
	Log           *slog.Logger

	// Limiter and IP are P1-13. Both optional; nil means no rate limiting.
	Limiter Limiter
	IP      ClientIP

	// Policy is the session policy. P2-10 makes it per organization.
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

// ServeHTTP routes the two methods.
//
// Registered by hand on the router rather than through the generated one, for
// the reason the other hand-registered endpoints give: this serves HTML, not
// the JSON envelope the generated interface produces.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.showForm(w, r)
	case http.MethodPost:
		h.submit(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		h.notice(w, r, http.StatusMethodNotAllowed, Notice{
			Title: "Method not allowed",
			Body:  "This page accepts GET and POST.",
		})
	}
}

// Forgot serves the forgotten-password entry point.
//
// FR-9 requires an entry point; P1-19.4 owns the flow that would sit behind
// it. Until then this says so plainly rather than linking to a route that
// 404s or to a form that silently does nothing — a reset page that appears to
// work and does not is worse than an honest dead end, because the person
// waiting for an email never asks anybody for help.
func (h *Handler) Forgot(w http.ResponseWriter, r *http.Request) {
	back := ""
	if id := r.URL.Query().Get("request"); validPendingID(id) {
		back = Path + "?request=" + url.QueryEscape(id)
	}

	h.notice(w, r, http.StatusOK, Notice{
		Title: "Resetting your password",
		Body: "Self-service password reset is not available yet. " +
			"Please ask your organization's administrator to set a new password for you.",
		BackPath: back,
	})
}

// --- GET ----------------------------------------------------------------------

func (h *Handler) showForm(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("request")
	if !validPendingID(id) {
		// Reached directly, or with a mangled id. There is nothing to sign in
		// TO, and no way to find out where the person meant to go — the whole
		// point of the opaque reference is that the destination is not in the
		// URL. So this explains rather than redirecting anywhere.
		h.noRequest(w, r)
		return
	}

	pending, err := h.Authorization.Peek(r.Context(), id)
	if err != nil {
		h.expired(w, r, err)
		return
	}

	page, err := h.page(r.Context(), pending, id)
	if err != nil {
		h.serverError(w, r, "preparing the login page", err)
		return
	}

	// Reuse an existing token rather than minting one per render. Minting
	// would invalidate every other tab the user has open on this page, and a
	// user with two tabs is not doing anything wrong.
	token, ok := csrfFromRequest(r)
	if !ok {
		token, err = newCSRFToken()
		if err != nil {
			h.serverError(w, r, "issuing a CSRF token", err)
			return
		}
		http.SetCookie(w, csrfCookie(token))
	}
	page.CSRFToken = token

	h.renderPage(w, r, http.StatusOK, page)
}

// page assembles everything the form needs from the database.
func (h *Handler) page(ctx context.Context, pending authorize.Pending, id string) (Page, error) {
	branding := DefaultBranding

	err := h.DB.WithTenant(ctx, pending.App.OrgID, func(tx *postgres.Tx) error {
		found, rejected, err := h.Brandings.Branding(ctx, tx, pending.App.OrgID)
		if err != nil {
			return err
		}
		h.warnBranding(pending.App.OrgID, rejected)
		branding = found
		return nil
	})
	if err != nil {
		return Page{}, err
	}

	style, hash := renderStyle(branding.AccentColor)

	return Page{
		AppName:    pending.App.Name,
		Branding:   branding,
		Style:      style,
		StyleHash:  hash,
		RequestID:  id,
		ForgotPath: ForgotPath + "?request=" + url.QueryEscape(id),
	}, nil
}

// warnBranding reports every value that was refused.
//
// At WARN and per rejection, for the reason authn.PolicyStore gives about
// clamped policy: an administrator who set a logo that never appears has a
// setting they believe is in force and is not, and the difference should not
// be discoverable only by squinting at the page.
func (h *Handler) warnBranding(orgID string, rejected []Rejection) {
	if h.Log == nil {
		return
	}
	for _, rejection := range rejected {
		h.Log.Warn("organization branding value was refused",
			"org_id", orgID,
			"field", rejection.Field,
			"value", rejection.Value,
			"reason", rejection.Reason)
	}
}

// --- POST ---------------------------------------------------------------------

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.badRequest(w, r, "the form could not be read")
		return
	}

	id := r.PostForm.Get("request")
	if !validPendingID(id) {
		h.noRequest(w, r)
		return
	}

	pending, err := h.Authorization.Peek(r.Context(), id)
	if err != nil {
		h.expired(w, r, err)
		return
	}

	page, err := h.page(r.Context(), pending, id)
	if err != nil {
		h.serverError(w, r, "preparing the login page", err)
		return
	}

	// The CSRF token that will be rendered back into the form. Read from the
	// cookie rather than reissued, so that re-rendering after a failure emits
	// no Set-Cookie header — which is what lets two failures be byte-identical
	// responses rather than merely similar ones.
	token, hasToken := csrfFromRequest(r)
	page.CSRFToken = token

	if !hasToken || !checkCSRF(r, r.PostForm.Get(csrfField)) {
		// A stale tab far more often than an attack. Either way the submission
		// is refused and the form comes back, with a fresh token so the retry
		// works.
		fresh, tokenErr := newCSRFToken()
		if tokenErr != nil {
			h.serverError(w, r, "issuing a CSRF token", tokenErr)
			return
		}
		http.SetCookie(w, csrfCookie(fresh))
		page.CSRFToken = fresh
		page.Error = MsgSessionProblem
		page.Email = boundedEmail(r.PostForm.Get("email"))
		h.count(OutcomeFailed)
		h.renderPage(w, r, http.StatusOK, page)
		return
	}

	email := boundedEmail(r.PostForm.Get("email"))
	password := r.PostForm.Get("password")

	// Both fields empty, or either. Refused before any lookup, so an empty
	// submission is not a free way to find out whether an address exists —
	// and refused at field level, because nothing about the account has been
	// consulted yet and there is therefore nothing to conceal.
	if email == "" || password == "" || len(password) > maxPasswordBytes {
		page.Email = email
		if email == "" {
			page.EmailError = MsgEmailRequired
		}
		if password == "" || len(password) > maxPasswordBytes {
			page.PasswordError = MsgPasswordRequired
		}
		page.Error = "Please check the fields below."
		h.count(OutcomeFailed)
		h.renderPage(w, r, http.StatusOK, page)
		return
	}

	// Before the password is verified, which is the point: a refused attempt
	// must cost no Argon2 computation. It also means a refusal is measurably
	// faster than a real attempt - a timing signal about the limiter's own
	// state, which discloses nothing, because the attacker produced that state.
	if h.Limiter != nil {
		if decision, bound := h.Limiter.Check(r.Context(), email, h.clientIP(r), h.now()); !decision.Allowed {
			if h.Log != nil {
				h.Log.Info("a login attempt was rate limited",
					"bound", bound, "retry_after", decision.RetryAfter.String())
			}
			page.Email = email
			page.Error = MsgRateLimited
			h.count(OutcomeRateLimited)
			h.renderPage(w, r, http.StatusOK, page)
			return
		}
	}

	outcome, current, invalidate := h.authenticate(r, pending, email, password)

	switch outcome.result {
	case resultAuthenticated:
		// After the commit, never before: a cache invalidated ahead of the
		// commit can be repopulated from the pre-commit state by a concurrent
		// reader (the ordering session.Manager.Revoke is built around).
		if invalidate != nil {
			if err := invalidate(r.Context()); err != nil && h.Log != nil {
				h.Log.Warn("invalidating the replaced session failed", "error", err.Error())
			}
		}

		http.SetCookie(w, session.Cookie(outcome.token))

		// FR-9. The ADDRESS counter is cleared because the person proved they
		// are who they said. The IP counter is NOT: one success from an office
		// does not vouch for the other four hundred attempts coming from it,
		// and clearing it would hand an attacker a reset button - one valid
		// credential of their own would clear the bound for everybody sharing
		// that address.
		if h.Limiter != nil {
			h.Limiter.Succeed(r.Context(), email)
		}
		h.count(OutcomeSuccess)

		// Hands off to P1-06, which consumes the pending request and issues
		// the code. This is the only place the flow continues, and it does not
		// re-validate the redirect URI — that was settled before the request
		// was stored.
		h.Authorization.Resume(w, r, id, current)

	case resultExpired:
		page.Email = email
		page.Error = MsgPasswordExpired
		h.count(OutcomeFailed)
		h.renderPage(w, r, http.StatusOK, page)

	case resultError:
		h.serverError(w, r, "authenticating", outcome.err)

	default:
		// resultRejected. One message, one status, one set of headers, for a
		// wrong password and an unknown address and a locked account alike.
		h.recordFailure(r, pending.App.OrgID, email)
		page.Email = email
		page.Error = MsgCredentials
		h.count(OutcomeFailed)
		h.renderPage(w, r, http.StatusOK, page)
	}
}

type result int

const (
	resultRejected result = iota
	resultAuthenticated
	resultExpired
	resultError
)

type attempt struct {
	result result
	token  session.Token
	err    error
}

// authenticate does the whole database side of a submission in one transaction.
//
// One transaction for the verification, the policy read, the session and the
// audit event, so that a session exists exactly when its audit record does
// (ADR-012). The returned function invalidates the cache for a session this
// login replaced, and must be called after the commit.
func (h *Handler) authenticate(
	r *http.Request, pending authorize.Pending, email, password string,
) (attempt, session.Session, func(context.Context) error) {
	var (
		out        attempt
		created    session.Session
		invalidate func(context.Context) error
	)

	ctx := r.Context()
	now := h.now()
	ip := h.clientIP(r)
	agent := r.UserAgent()

	err := h.DB.WithTenant(ctx, pending.App.OrgID, func(tx *postgres.Tx) error {
		user, verified, err := h.Users.Authenticate(ctx, tx, email, password)
		if err != nil {
			// A broken stored hash, or a failed query. The browser still gets
			// the uniform answer; this is for the operator.
			if h.Log != nil {
				h.Log.Error("verifying a password failed", "error", err.Error())
			}
			verified = false
		}

		if !verified || !user.CanSignIn() {
			out.result = resultRejected
			return h.auditFailure(ctx, tx, pending.App.OrgID, user, verified, ip, agent)
		}

		// Expiry after verification, deliberately. Checking it first would
		// answer "this address has an expired password" to somebody who never
		// proved they own it.
		policy, err := h.Policies.Policy(ctx, tx, pending.App.OrgID)
		if err != nil {
			return err
		}
		if authn.Expired(user.PasswordChangedAt, policy, now) {
			out.result = resultExpired
			return h.auditFailure(ctx, tx, pending.App.OrgID, user, true, ip, agent)
		}

		// Rehash-on-login. The only moment the plaintext and the stored hash
		// are both available, which is why P1-01's NeedsRehash has had no
		// caller until now.
		if user.NeedsRehash {
			if err := h.Users.RecordRehash(ctx, tx, user.ID, password); err != nil {
				// Not fatal. The password is correct and the user should be
				// let in; the hash stays at its old cost until next time.
				if h.Log != nil {
					h.Log.Warn("rehashing a password on login failed",
						"user_id", user.ID, "error", err.Error())
				}
			}
		}

		// Replace the session this browser already had, if any.
		//
		// The cookie is about to be overwritten, so whatever it pointed at
		// becomes unreachable — and an unreachable live session is one that
		// still appears on the sessions screen and still authorises a refresh
		// token. session.ReasonReauth exists for exactly this.
		if previous, ok := h.currentSession(ctx, r, now); ok && previous.UserID == user.ID {
			invalidate, err = h.Sessions.Revoke(ctx, tx, previous.ID, session.ReasonReauth, user.ID, now)
			if err != nil {
				return err
			}
		}

		newSession, token, err := h.Sessions.Create(ctx, tx, session.New{
			UserID: user.ID,
			OrgID:  user.OrgID,
			// The factor actually used. P1-11 requires this to be true from
			// the first release because P1-07's `amr` claim is built from it
			// and Phase 3's step-up authentication reads that claim.
			AuthMethods: []string{"pwd"},
			IP:          ip,
			UserAgent:   r.UserAgent(),
		}, h.Policy, now)
		if err != nil {
			return err
		}

		if err := h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       user.OrgID,
			ActorUserID: user.ID,
			Type:        audit.EventLoginSucceeded,
			Payload: map[string]any{
				"session_id":   newSession.ID,
				"client_id":    pending.App.ID,
				"auth_methods": newSession.AuthMethods,
				// P1-14 step 1. The user agent is what distinguishes "the user
				// signed in from a new laptop" from "somebody signed in as
				// them", and an incident timeline without it cannot tell those
				// apart. Bounded by session.New's own limit before storage.
				"user_agent": boundedUserAgent(r.UserAgent()),
			},
			IP: ip,
		}); err != nil {
			return err
		}

		created = newSession
		out.result = resultAuthenticated
		out.token = token
		return nil
	})

	if err != nil {
		return attempt{result: resultError, err: err}, session.Session{}, nil
	}
	return out, created, invalidate
}

// auditFailure records a refused login.
//
// The submitted address is NOT in the payload, and that is a considered trade
// rather than an omission. Including it would make credential-stuffing
// forensics much easier. It would also write every mistyped and every probed
// address into an append-only table with 24-month retention, which turns the
// audit log into a list of addresses somebody tried — the artefact an attacker
// who reaches the log most wants, and one the people in it never consented to.
// P1-13's rate limiting keys on the address in Redis with a short TTL, which
// is where that data belongs.
//
// `reason` is a class, not a message: it distinguishes a locked account from a
// wrong password FOR THE OPERATOR, which is legitimate, because the audit log
// is not the response body. The uniformity requirement is about what the
// browser can observe.
func (h *Handler) auditFailure(
	ctx context.Context, tx *postgres.Tx, orgID string, user authn.User,
	verified bool, ip, userAgent string,
) error {
	reason := "credentials"
	actor := ""
	switch {
	case verified && !user.CanSignIn():
		reason = "account_" + user.Status
		actor = user.ID
	case verified:
		reason = "password_expired"
		actor = user.ID
	case user.ID != "":
		reason = "wrong_password"
		actor = user.ID
	}

	return h.Audit.Write(ctx, tx, audit.Event{
		OrgID:       orgID,
		ActorUserID: actor,
		Type:        audit.EventLoginFailed,
		Payload: map[string]any{
			"reason": reason,
			// The user agent, and still not the address. A run of failures
			// from one agent is the shape of an attack; the addresses tried
			// are the artefact an attacker who reaches the log most wants.
			"user_agent": boundedUserAgent(userAgent),
		},
		IP: ip,
	})
}

// currentSession resolves the cookie this request already carried.
func (h *Handler) currentSession(ctx context.Context, r *http.Request, now time.Time) (session.Session, bool) {
	presented, ok := session.FromRequest(r)
	if !ok {
		return session.Session{}, false
	}
	current, err := h.Sessions.Lookup(ctx, presented, h.Policy, now)
	if err != nil {
		return session.Session{}, false
	}
	return current, true
}

// --- responses ------------------------------------------------------------------

// renderPage writes the form.
//
// Every header this page needs beyond the global ones is set here, in one
// place, so that a response cannot be written from a path that forgot them.
func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, status int, page Page) {
	body, err := render(pageTemplate, page)
	if err != nil {
		h.serverError(w, r, "rendering the login page", err)
		return
	}
	h.write(w, status, page.ContentSecurityPolicy(), body)
}

func (h *Handler) notice(w http.ResponseWriter, r *http.Request, status int, notice Notice) {
	notice.Style, notice.StyleHash = renderStyle(DefaultAccent)

	body, err := render(noticeTemplate, notice)
	if err != nil {
		// Nothing left to render with. A bare 500 rather than a recursive
		// attempt at another page.
		if h.Log != nil {
			h.Log.Error("rendering a login notice failed", "error", err.Error())
		}
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	h.write(w, status, notice.ContentSecurityPolicy(), body)
}

// write emits the response with the page's own headers.
func (h *Handler) write(w http.ResponseWriter, status int, csp string, body []byte) {
	writePage(w, status, csp, body)
}

// writePage is the one place any browser page in this package gets its
// headers.
//
// A package-level function rather than a method, because P1-10's logout pages
// need exactly the same set and a second copy is a second thing to forget. On
// this surface the absence of any of these is exploitable, which is why they
// are set here rather than relied upon from the middleware chain.
func writePage(w http.ResponseWriter, status int, csp string, body []byte) {
	head := w.Header()
	head.Set("Content-Type", "text/html; charset=utf-8")
	head.Set("Content-Security-Policy", csp)
	// Set again rather than relied upon. SecurityHeaders already sets these
	// for every route, and this page is the one where their absence would be
	// exploitable — so it does not depend on a middleware chain staying in the
	// order somebody else maintains.
	head.Set("X-Frame-Options", "DENY")
	head.Set("Referrer-Policy", "no-referrer")
	head.Set("Cache-Control", "no-store")
	head.Set("X-Content-Type-Options", "nosniff")

	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (h *Handler) noRequest(w http.ResponseWriter, r *http.Request) {
	h.notice(w, r, http.StatusBadRequest, Notice{
		Title: "Nothing to sign in to",
		Body: "This page is reached from an application that needs you to sign in. " +
			"Open the application you want to use and it will bring you back here.",
	})
}

func (h *Handler) expired(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, authorize.ErrPendingNotFound) {
		if h.Log != nil {
			h.Log.Error("reading a pending authorization request failed", "error", err.Error())
		}
		h.count(OutcomeError)
	}

	h.notice(w, r, http.StatusBadRequest, Notice{
		Title: "This sign-in has expired",
		Body: "It took too long, or it has already been completed. " +
			"Please return to the application and try again.",
	})
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, detail string) {
	if h.Log != nil {
		h.Log.Info("a login submission was rejected", "detail", detail)
	}
	h.notice(w, r, http.StatusBadRequest, Notice{
		Title: "We couldn't read that",
		Body:  "Please return to the application and try again.",
	})
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, doing string, err error) {
	if h.Log != nil && err != nil {
		h.Log.Error("login failed while "+doing, "error", err.Error())
	}
	h.count(OutcomeError)
	h.notice(w, r, http.StatusInternalServerError, Notice{
		Title: "Something went wrong",
		Body:  "Please try again in a moment.",
	})
}

func (h *Handler) count(outcome string) {
	if h.Observer != nil {
		h.Observer.LoginAttempt(outcome)
	}
}

// --- input plumbing --------------------------------------------------------------

// validPendingID checks the shape of a pending request id before it is used.
//
// The ids authorize.SavePending mints are 32 random bytes in unpadded
// base64url. Checking the shape here means a crafted value never reaches Redis
// as a key, and — more importantly — never reaches the template, because a
// value that fails this test is answered by a page that renders nothing from
// the request at all.
func validPendingID(id string) bool {
	if len(id) != 43 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// boundedEmail trims and bounds the submitted address.
//
// Not normalised to lowercase here: the value is echoed back into the form, so
// somebody who types a capital letter should see the address they typed. The
// comparison is normalised inside authn, where it belongs.
func boundedEmail(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > 320 {
		return trimmed[:320]
	}
	return trimmed
}

// boundedUserAgent trims what goes into an audit payload.
//
// A user agent is attacker-controlled and unbounded, and the audit table is
// append-only with a 24-month retention — so an unbounded one is a place to
// park data that nothing can delete. 512 bytes is far more than any real
// browser sends, and matches the bound session.New already applies.
func boundedUserAgent(raw string) string {
	const max = 512
	if len(raw) > max {
		return raw[:max]
	}
	return raw
}

// recordFailure tells the limiter, and audits the first refusal of a cooldown.
//
// Once per cooldown, not once per attempt: auditing every refused attempt
// during one would let an attacker write to an append-only table as fast as
// they can send requests, which is a different attack handed to them by the
// control meant to stop the first.
func (h *Handler) recordFailure(r *http.Request, orgID, email string) {
	if h.Limiter == nil {
		return
	}

	started, bound := h.Limiter.Fail(r.Context(), email, h.clientIP(r), h.now())
	if !started {
		return
	}

	if h.Log != nil {
		h.Log.Warn("a sign-in cooldown started", "bound", bound)
	}

	if h.Audit == nil || h.DB == nil || orgID == "" {
		return
	}

	// Recorded against the CLIENT'S organization, not instance-wide.
	//
	// The address may belong to no organization at all — that is the whole
	// reason the counter is keyed on it — but the ATTEMPT belongs to one: it
	// happened at that organization's login page, which is the same reasoning
	// the failed-login event already uses.
	//
	// An earlier version passed "" here and called WithTenant, which returns
	// ErrEmptyOrgID without doing anything. The error was discarded, so the
	// lockout event was silently never written — and the unit test did not
	// notice, because its fake tenant ignores the organization entirely. The
	// integration test below it now asserts the row exists.
	if err := h.DB.WithTenant(r.Context(), orgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(r.Context(), tx, audit.Event{
			Type: audit.EventUserLockedOut,
			// The bound and nothing else. NOT the address: P1-12 explains why
			// the audit log must not become a list of addresses somebody
			// tried, and a lockout entry is where that list would come from
			// fastest.
			Payload: map[string]any{
				"bound":      bound,
				"user_agent": boundedUserAgent(r.UserAgent()),
			},
			IP: h.clientIP(r),
		})
	}); err != nil && h.Log != nil {
		// Reported rather than discarded. A lockout that is not recorded is a
		// security event that did not happen as far as any investigation is
		// concerned.
		h.Log.Error("a sign-in cooldown was not audited", "error", err.Error())
	}
}

// clientIP is the address the request came from.
//
// Delegated to the configured resolver (httpserver.ClientIP) rather than read
// here, because what counts as a client IP is a deployment decision: on this
// service's own staging topology RemoteAddr is the Docker gateway and is the
// same for every user in the world. With no resolver configured it falls back
// to RemoteAddr, which is unforgeable and, behind a proxy, shared.
func (h *Handler) clientIP(r *http.Request) string {
	if h.IP != nil {
		return h.IP.Of(r)
	}
	return remoteAddr(r)
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
