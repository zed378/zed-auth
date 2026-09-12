package login

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// The second step of the hosted login flow (P3-03).
//
// Specification: MEMORY/specs/P3-03-totp-verification-at-login.md.
//
// It is the login page's sibling and not a new surface: the same stylesheet,
// the same CSP, the same CSRF, the same "no script, ever". The difference is
// what it asks for and what it has already established — by the time this page
// renders, a password has been proven, which changes what may safely be said on
// it (see MsgWrongCode).
//
// **What this page must never do is exist without a challenge behind it.** A
// code form that renders for anybody who navigates to it would be a place to
// guess codes against a challenge that was never issued — and, worse, a place
// where the presence of the form means nothing, so a user could not tell the
// real step from a forged one.

// MFAPath is where the challenge step is mounted.
const MFAPath = "/login/mfa"

// ChallengeCookieName carries the handle.
//
// `__Host-` for the reason the CSRF cookie has it, and with more at stake: a
// subdomain that could set this cookie could substitute its own challenge for
// the user's, which is a way to make somebody complete an attacker's login.
const ChallengeCookieName = "__Host-zedauth_mfa"

// challengeCookieLifetime matches mfa.ChallengeTTL.
//
// The cookie is not the bound — the store's TTL is, and it is the only one that
// can be enforced. This exists so a browser does not keep sending a handle that
// stopped meaning anything, which would turn every stale tab into a lookup.
const challengeCookieLifetime = mfa.ChallengeTTL

// handleLength is the encoded length of a challenge handle — 32 bytes,
// base64url without padding, the shape mfa.NewHandle produces.
var handleLength = base64.RawURLEncoding.EncodedLen(32)

// challengeCookie builds the cookie carrying a handle.
func challengeCookie(handle string) *http.Cookie {
	return &http.Cookie{
		Name:     ChallengeCookieName,
		Value:    handle,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(challengeCookieLifetime.Seconds()),
	}
}

// clearChallengeCookie expires the handle cookie.
//
// Sent on every terminal outcome — completed, spent, expired — so a browser
// stops presenting a handle that names nothing. Not a security control: the
// handle is already dead server-side by then, and a cookie the client chooses
// to keep changes nothing.
func clearChallengeCookie() *http.Cookie {
	return &http.Cookie{
		Name:     ChallengeCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// challengeFromRequest reads the handle.
//
// **From the cookie only.** Never from the form and never from the query
// string: a handle in a URL reaches the Referer header, the browser history,
// and every access log between here and the client — and it is the one value
// that stands between a proven password and a session.
//
// Shape-checked before use so a mangled cookie is a missing challenge rather
// than a store lookup on arbitrary bytes.
func challengeFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(ChallengeCookieName)
	if err != nil || len(cookie.Value) != handleLength {
		return "", false
	}
	if _, err := base64.RawURLEncoding.DecodeString(cookie.Value); err != nil {
		return "", false
	}
	return cookie.Value, true
}

// MFAStep serves the challenge page.
//
// Registered by hand alongside /login, for the same reason: it serves HTML
// rather than the generated JSON envelope.
func (h *Handler) MFAStep(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.showChallengeAgain(w, r)
	case http.MethodPost:
		h.submitChallenge(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		h.notice(w, r, http.StatusMethodNotAllowed, Notice{
			Title: "Method not allowed",
			Body:  "This page accepts GET and POST.",
		})
	}
}

// --- GET ---------------------------------------------------------------------

// showChallengeAgain re-renders the page for a refresh or a back button.
//
// It consumes nothing and counts nothing. A user who reloads the page has not
// guessed at anything, and charging them an attempt for it would make the
// bound measure browser behaviour instead of guessing.
func (h *Handler) showChallengeAgain(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("request")
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

	handle, ok := challengeFromRequest(r)
	if !ok || h.MFA == nil {
		h.challengeGone(w, r, page)
		return
	}

	offered, err := h.MFA.Peek(r.Context(), handle)
	if err != nil {
		// Expired, spent, or never was. All the same page, because the
		// difference is a fact about a login this caller may not own.
		h.challengeGone(w, r, page)
		return
	}

	// A fresh CSRF token when there is none, so a reload after the cookie
	// lapsed produces a submittable form rather than a dead one.
	token, hasToken := csrfFromRequest(r)
	if !hasToken {
		fresh, tokenErr := newCSRFToken()
		if tokenErr != nil {
			h.serverError(w, r, "issuing a CSRF token", tokenErr)
			return
		}
		http.SetCookie(w, csrfCookie(fresh))
		token = fresh
	}
	page.CSRFToken = token

	h.showChallenge(w, r, http.StatusOK, page, offered, "")
}

// --- POST --------------------------------------------------------------------

// submitChallenge verifies one code.
func (h *Handler) submitChallenge(w http.ResponseWriter, r *http.Request) {
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

	token, hasToken := csrfFromRequest(r)
	page.CSRFToken = token

	if !hasToken || !checkCSRF(r, r.PostForm.Get(csrfField)) {
		fresh, tokenErr := newCSRFToken()
		if tokenErr != nil {
			h.serverError(w, r, "issuing a CSRF token", tokenErr)
			return
		}
		http.SetCookie(w, csrfCookie(fresh))
		page.CSRFToken = fresh

		offered, _ := h.offeredFor(r)
		h.showChallenge(w, r, http.StatusOK, page, offered, MsgSessionProblem)
		return
	}

	handle, ok := challengeFromRequest(r)
	if !ok || h.MFA == nil {
		h.challengeGone(w, r, page)
		return
	}

	// The factor TYPE, from the form. Not a factor id — see
	// Framework.AnswerType. An unrecognised type resolves to no factor and is
	// refused exactly as a wrong code is.
	factorType := mfa.Type(r.PostForm.Get("factor"))
	code := boundedCode(r.PostForm.Get("code"))

	// `id` binds the answer to the authorization request this form belongs to.
	// A challenge issued for one request must not complete another.
	outcome, err := h.MFA.AnswerType(r.Context(), handle, id, factorType, code)
	switch {
	case err == nil && outcome.Complete:
		h.completeChallenge(w, r, pending, id, outcome)

	case errors.Is(err, mfa.ErrTooManyAttempts):
		// Truthful, for MsgRateLimited's reason: the counter is keyed on a user
		// whose password this caller has already proven, so it describes only
		// their own behaviour. Being vague would send somebody who must wait
		// into retrying, which is worse for them and for the service.
		h.count(OutcomeRateLimited)
		offered, _ := h.offeredFor(r)
		h.showChallenge(w, r, http.StatusOK, page, offered, MsgCodeRateLimited)

	case errors.Is(err, mfa.ErrChallengeSpent), errors.Is(err, mfa.ErrNoChallenge):
		// Out of attempts, or expired. The login restarts at the password step.
		h.count(OutcomeFailed)
		h.challengeGone(w, r, page)

	case errors.Is(err, mfa.ErrWrongCode), errors.Is(err, mfa.ErrNoSuchFactor):
		// **One answer for both.** A wrong code and a factor type this
		// challenge has no factor for are different facts, and the second is a
		// fact about what the user has enrolled — which somebody probing the
		// form must not be able to read off the response.
		h.auditMFA(r, audit.EventMFAFailed, factorType, outcome)
		h.count(OutcomeFailed)
		h.showChallenge(w, r, http.StatusOK, page, outcome.Offered, MsgWrongCode)

	default:
		// A failure to DECIDE: the factor store unreachable, a seal key that
		// will not open a secret. Not a wrong code — reporting an operator's
		// misconfiguration as one sends a user to recovery codes for a problem
		// they cannot fix.
		h.serverError(w, r, "verifying a factor", err)
	}
}

// completeChallenge creates the session the challenge was standing in front of.
func (h *Handler) completeChallenge(
	w http.ResponseWriter, r *http.Request, pending authorize.Pending, id string, outcome mfa.Outcome,
) {
	ctx := r.Context()

	// The challenge is the authority on WHO, so the user is loaded by the id it
	// carried rather than by anything the browser holds. This is abuse case A-2
	// closed at the last possible moment as well as the first.
	user, loginPolicy, err := h.userForChallenge(ctx, outcome)
	if err != nil {
		h.serverError(w, r, "loading the user for a challenge", err)
		return
	}

	out, current, invalidate := h.issue(r, pending, user, loginPolicy, outcome.Methods)
	if out.result != resultAuthenticated {
		h.serverError(w, r, "creating a session", out.err)
		return
	}

	if invalidate != nil {
		if err := invalidate(ctx); err != nil && h.Log != nil {
			h.Log.Warn("invalidating the replaced session failed", "error", err.Error())
		}
	}

	http.SetCookie(w, clearChallengeCookie())
	http.SetCookie(w, session.Cookie(out.token))

	h.auditMFA(r, audit.EventMFASucceeded, firstType(outcome.Methods), outcome)
	h.count(OutcomeSuccess)

	h.Authorization.Resume(w, r, id, current)
}

// userForChallenge loads the user and the organization's login policy.
//
// The login policy is re-read rather than carried through the challenge: an
// organization that disabled password sign-in while somebody was mid-challenge
// should not have that login complete, and a value captured five minutes ago
// would let it.
func (h *Handler) userForChallenge(
	ctx context.Context, outcome mfa.Outcome,
) (authn.User, authn.LoginPolicy, error) {
	var (
		user   authn.User
		policy authn.LoginPolicy
	)

	err := h.DB.WithTenant(ctx, outcome.OrgID, func(tx *postgres.Tx) error {
		var err error
		policy, err = h.Policies.LoginPolicy(ctx, tx, outcome.OrgID)
		if err != nil {
			return err
		}
		user, err = h.Users.ByID(ctx, tx, outcome.UserID)
		if err != nil {
			return err
		}
		if !user.CanSignIn() {
			// Deactivated or locked between the password step and the code.
			// Refused, because the state that matters is the one now.
			return errors.New("login: the user may no longer sign in")
		}
		return nil
	})

	return user, policy, err
}

// --- rendering ----------------------------------------------------------------

// showChallenge renders the code form.
func (h *Handler) showChallenge(
	w http.ResponseWriter, r *http.Request, status int, page Page, offered []mfa.Type, problem string,
) {
	challenge := ChallengePage{
		Page:    page,
		Offered: offeredLabels(offered),
		Problem: problem,
	}

	// A challenge with nothing to offer is a dead end. It can happen: every
	// factor removed between the password step and this render. Say so rather
	// than showing a form whose every answer is wrong.
	if len(challenge.Offered) == 0 {
		h.challengeGone(w, r, page)
		return
	}

	body, err := render(challengeTemplate, challenge)
	if err != nil {
		h.serverError(w, r, "rendering the challenge page", err)
		return
	}
	h.write(w, status, page.ContentSecurityPolicy(), body)
}

// challengeGone is the end of a challenge that cannot continue.
//
// One page for expired, spent, missing and never-was, and the same page
// whether the handle named somebody else's login or nothing at all. The way
// back is the password step, which is the only way back that exists.
func (h *Handler) challengeGone(w http.ResponseWriter, r *http.Request, page Page) {
	http.SetCookie(w, clearChallengeCookie())

	back := Path
	if page.RequestID != "" {
		back = Path + "?request=" + url.QueryEscape(page.RequestID)
	}

	h.notice(w, r, http.StatusOK, Notice{
		Title:    "Start again",
		Body:     MsgChallengeGone,
		BackPath: back,
	})
}

// offeredFor re-reads what a live challenge may be answered with.
//
// Used on the paths that must re-render without having just called
// AnswerType. An error yields nothing, and showChallenge turns that into the
// "start again" page rather than an empty form.
func (h *Handler) offeredFor(r *http.Request) ([]mfa.Type, error) {
	handle, ok := challengeFromRequest(r)
	if !ok || h.MFA == nil {
		return nil, mfa.ErrNoChallenge
	}
	return h.MFA.Peek(r.Context(), handle)
}

// --- audit ---------------------------------------------------------------------

// auditChallenged records that a password was proven and a factor demanded.
//
// Beyond the card's step 7, and deliberately. Without it, an attacker who holds
// a working password and is stopped by the factor step leaves NO trace at all
// unless they also guess wrong at least once — and "somebody with a valid
// password reached the factor step" is exactly the line an operator needs to
// find a compromised credential before it is used somewhere without MFA.
//
// It writes in its own transaction because it is not paired with anything. No
// session is created, so ADR-012's "a session exists exactly when its audit
// record does" has nothing to hold together here.
func (h *Handler) auditChallenged(ctx context.Context, user authn.User, pending authorize.Pending) {
	if h.Audit == nil {
		return
	}
	err := h.DB.WithTenant(ctx, user.OrgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       user.OrgID,
			ActorUserID: user.ID,
			Type:        audit.EventMFAChallenged,
			Payload: map[string]any{
				"client_id": pending.App.ID,
			},
		})
	})
	if err != nil && h.Log != nil {
		// Logged, not fatal. Refusing a login because an audit row could not be
		// written would convert an audit outage into an outage, and the user
		// has not been given anything yet.
		h.Log.Warn("recording an MFA challenge failed", "error", err.Error())
	}
}

// auditMFA records one verification outcome.
//
// The factor TYPE and nothing else. Never the code — it is a live credential
// for the rest of its step — and never the factor id, which would put a
// per-user identifier in a table with 24-month retention for no investigative
// gain over the type.
//
// The account comes from the Outcome, which got it from the challenge. Not
// from a second lookup and not from the form: an audit row attributing a
// failure to whichever user the request named would be worse than no row,
// because it would be believed.
func (h *Handler) auditMFA(
	r *http.Request, event audit.EventType, factorType mfa.Type, outcome mfa.Outcome,
) {
	if h.Audit == nil || outcome.OrgID == "" || outcome.UserID == "" {
		return
	}

	ctx := r.Context()
	ip := h.clientIP(r)

	err := h.DB.WithTenant(ctx, outcome.OrgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       outcome.OrgID,
			ActorUserID: outcome.UserID,
			Type:        event,
			Payload: map[string]any{
				"factor_type": string(factorType),
			},
			IP: ip,
		})
	})
	if err != nil && h.Log != nil {
		// Logged, not fatal. Refusing a login because an audit row could not be
		// written converts an audit outage into an outage — and on the failure
		// path it would hand an attacker a way to suppress their own trail by
		// making one table unwritable.
		h.Log.Warn("recording an MFA verification failed", "error", err.Error())
	}
}

// --- small helpers ---------------------------------------------------------------

// maxCodeBytes bounds a submitted code.
//
// Generous next to six digits and small enough that the field is not a place to
// push bytes. The verifier length-checks properly; this stops the value from
// being large before it gets there.
const maxCodeBytes = 64

func boundedCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxCodeBytes {
		return value[:maxCodeBytes]
	}
	return value
}

// firstType is the factor a completion's methods name, for the audit payload.
func firstType(methods []mfa.Type) mfa.Type {
	if len(methods) == 0 {
		return ""
	}
	return methods[0]
}
