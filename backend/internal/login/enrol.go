package login

import (
	"context"
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

// Forced enrolment (P3-07).
//
// Specification: MEMORY/specs/P3-07-mfa-enforcement.md.
//
// `P2-10` stored `mfa_required` and did not enforce it, because MFA did not
// exist. This is the enforcement, and the shape of it is decided by what the
// alternatives do to people:
//
//   - **Deny outright** and everybody without a factor is locked out the moment
//     the switch flips. That produces a support queue rather than security, and
//     the fastest way out of a support queue is to turn the setting off — which
//     leaves the organization less safe than before anybody tried.
//   - **Warn and let through** and the setting does nothing, which is what it
//     does today.
//
// So: route them into enrolment AT LOGIN, where the only way forward is
// through. No session exists until they finish, which is also what makes the
// state unbypassable — every other page needs one.

// EnrolPath is where the forced-enrolment step lives.
const EnrolPath = "/login/mfa/enrol"

// EnrolCookieName carries the enrolment handle.
//
// A different cookie from the challenge's, because the two states mean opposite
// things: a challenge says "this user has a factor and must use it", an
// enrolment says "this user has none and must get one". Sharing a cookie would
// let a stale one of either kind be read as the other.
const EnrolCookieName = "__Host-zedauth_enrol"

// EnrolmentTTL is how long a forced enrolment stays resumable.
//
// **Fifteen minutes**, three times the challenge's, because the work is
// different in kind. Answering a challenge is reading six digits off a phone
// that is already set up. Enrolling means finding the authenticator app,
// possibly installing it, scanning or typing a secret, and only then reading a
// code — with somebody who has just been told they cannot sign in without it.
const EnrolmentTTL = 15 * 60

// Enroller creates and confirms a factor during forced enrolment.
//
// An interface rather than *mfa.TOTP, and **nil means this build cannot enrol
// anybody**, which is load-bearing rather than defensive: forcing enrolment on
// a deployment with no factor implementation would be a lockout with no way
// out. `RequireMFA` decides the policy; this decides whether the policy can be
// acted on at all.
type Enroller interface {
	// Begin creates a pending factor and returns what the user needs to set up
	// their authenticator.
	Begin(ctx context.Context, userID, orgID, label string) (mfa.Enrolment, error)

	// Confirm proves the user can produce codes, which is what turns a pending
	// factor into a real one.
	Confirm(ctx context.Context, factorID, code string) error
}

// EnrolState is the server-side half of a forced enrolment.
//
// It carries the same things a challenge does and for the same reasons: the
// user id lives here rather than in anything the client holds, so a client
// cannot enrol a factor onto a different account, and the pending request ties
// it to one login.
type EnrolState struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	PendingID string `json:"pending_id"`

	// FactorID is the pending factor this enrolment is completing.
	FactorID string `json:"factor_id"`

	// Attempts counts wrong codes, bounded like a challenge's.
	Attempts int `json:"attempts"`
}

// EnrolStore holds forced-enrolment state.
//
// Deliberately the same shape as mfa.ChallengeStore. The handle is hashed
// before it becomes a key, the TTL is the store's rather than a timestamp
// somebody compares, and a failed attempt does not extend it.
type EnrolStore interface {
	Put(ctx context.Context, state EnrolState, ttlSeconds int) (handle string, err error)
	Get(ctx context.Context, handle string) (EnrolState, error)
	Replace(ctx context.Context, handle string, state EnrolState) error
	Delete(ctx context.Context, handle string) error
}

// ErrNoEnrolment is a handle that names nothing: expired, spent, or never was.
var ErrNoEnrolment = errors.New("login: no such enrolment")

// enrolCookie builds the cookie carrying a handle.
func enrolCookie(handle string) *http.Cookie {
	return &http.Cookie{
		Name:     EnrolCookieName,
		Value:    handle,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   EnrolmentTTL,
	}
}

func clearEnrolCookie() *http.Cookie {
	return &http.Cookie{
		Name:     EnrolCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

// enrolFromRequest reads the handle, from the cookie only.
func enrolFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(EnrolCookieName)
	if err != nil || len(cookie.Value) != handleLength {
		return "", false
	}
	return cookie.Value, true
}

// --- raising the forced state ------------------------------------------------------

// beginForcedEnrolment creates the pending factor and the state behind the page.
func (h *Handler) beginForcedEnrolment(
	ctx context.Context, user authn.User, pending authorize.Pending,
) (handle string, enrolment mfa.Enrolment, err error) {
	if h.Enrol == nil || h.Enrolments == nil {
		return "", mfa.Enrolment{}, errors.New("login: forced enrolment is not configured")
	}

	// "Set up at sign-in" rather than a device name the user has not been
	// asked for yet. They can rename it from the account screen; asking here
	// would put a text field between somebody and the account they are locked
	// out of.
	enrolment, err = h.Enrol.Begin(ctx, user.ID, user.OrgID, "Set up at sign-in")
	if err != nil {
		return "", mfa.Enrolment{}, err
	}

	handle, err = h.Enrolments.Put(ctx, EnrolState{
		UserID:    user.ID,
		OrgID:     user.OrgID,
		PendingID: pending.ID,
		FactorID:  enrolment.FactorID,
	}, EnrolmentTTL)
	if err != nil {
		return "", mfa.Enrolment{}, err
	}

	h.auditEnrolmentForced(ctx, user, pending)
	return handle, enrolment, nil
}

// --- the step ------------------------------------------------------------------------

// EnrolStep serves the forced-enrolment page and its submission.
func (h *Handler) EnrolStep(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.showEnrolAgain(w, r)
	case http.MethodPost:
		h.submitEnrolment(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		h.notice(w, r, http.StatusMethodNotAllowed, Notice{
			Title: "Method not allowed",
			Body:  "This page accepts GET and POST.",
		})
	}
}

// showEnrolAgain re-renders for a refresh.
//
// It does NOT begin a new enrolment. Doing so would mint a second secret every
// time somebody reloaded, and the one they had already scanned would stop
// working — which reads as "this service is broken" at the worst possible
// moment.
func (h *Handler) showEnrolAgain(w http.ResponseWriter, r *http.Request) {
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

	handle, ok := enrolFromRequest(r)
	if !ok || h.Enrolments == nil {
		h.enrolmentGone(w, r, page)
		return
	}

	state, err := h.Enrolments.Get(r.Context(), handle)
	if err != nil || state.PendingID != id {
		h.enrolmentGone(w, r, page)
		return
	}

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

	// The secret is NOT re-displayed on a refresh.
	//
	// It was shown once, when the enrolment began. Showing it again on every
	// reload would turn a one-time display into a value sitting in a browser
	// cache — and the user who needs it has already scanned it. Somebody who
	// lost it restarts, which costs one sign-in.
	h.showEnrolment(w, r, http.StatusOK, page, mfa.Enrolment{}, "")
}

// submitEnrolment confirms the factor and completes the login.
func (h *Handler) submitEnrolment(w http.ResponseWriter, r *http.Request) {
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
		h.showEnrolment(w, r, http.StatusOK, page, mfa.Enrolment{}, MsgSessionProblem)
		return
	}

	handle, ok := enrolFromRequest(r)
	if !ok || h.Enrolments == nil || h.Enrol == nil {
		h.enrolmentGone(w, r, page)
		return
	}

	state, err := h.Enrolments.Get(r.Context(), handle)
	if err != nil {
		h.enrolmentGone(w, r, page)
		return
	}

	// The same binding the challenge step enforces: an enrolment begun for one
	// authorization request must not complete another.
	if state.PendingID != id {
		h.enrolmentGone(w, r, page)
		return
	}

	// The defensive half of the attempt bound.
	//
	// The block after `Confirm` below is what increments, spends and DESTROYS a
	// used-up enrolment, and a mutation run showed the two are redundant for
	// the message a caller sees — either alone produces it. This one stays for
	// the state that somehow survived deletion: a store write that failed, a
	// Redis that dropped the DEL. Without it such a state would be answerable
	// again for its full fifteen minutes.
	if state.Attempts >= mfa.MaxAttempts {
		h.enrolmentGone(w, r, page)
		return
	}

	code := boundedCode(r.PostForm.Get("code"))

	if err := h.Enrol.Confirm(r.Context(), state.FactorID, code); err != nil {
		if !errors.Is(err, mfa.ErrWrongCode) {
			h.serverError(w, r, "confirming an enrolment", err)
			return
		}

		state.Attempts++
		if repErr := h.Enrolments.Replace(r.Context(), handle, state); repErr != nil && h.Log != nil {
			h.Log.Warn("recording a failed enrolment attempt failed", "error", repErr.Error())
		}
		if state.Attempts >= mfa.MaxAttempts {
			_ = h.Enrolments.Delete(r.Context(), handle)
			h.enrolmentGone(w, r, page)
			return
		}

		h.count(OutcomeFailed)
		h.showEnrolment(w, r, http.StatusOK, page, mfa.Enrolment{}, MsgWrongCode)
		return
	}

	h.completeEnrolment(w, r, pending, id, state, handle)
}

// completeEnrolment issues the session the enrolment was standing in front of.
func (h *Handler) completeEnrolment(
	w http.ResponseWriter, r *http.Request, pending authorize.Pending,
	id string, state EnrolState, handle string,
) {
	ctx := r.Context()

	user, loginPolicy, err := h.userForChallenge(ctx, mfa.Outcome{
		UserID: state.UserID, OrgID: state.OrgID,
	})
	if err != nil {
		h.serverError(w, r, "loading the user for an enrolment", err)
		return
	}

	// The factor was just proven, so this session used it. `amr` says so.
	out, current, invalidate := h.issue(r, pending, user, loginPolicy, []mfa.Type{mfa.TypeTOTP}, false)
	if out.result != resultAuthenticated {
		h.serverError(w, r, "creating a session", out.err)
		return
	}

	if invalidate != nil {
		if err := invalidate(ctx); err != nil && h.Log != nil {
			h.Log.Warn("invalidating the replaced session failed", "error", err.Error())
		}
	}

	_ = h.Enrolments.Delete(ctx, handle)
	http.SetCookie(w, clearEnrolCookie())
	http.SetCookie(w, session.Cookie(out.token))

	h.auditEnrolled(ctx, state)
	h.count(OutcomeSuccess)

	h.Authorization.Resume(w, r, id, current)
}

// --- rendering -------------------------------------------------------------------------

func (h *Handler) showEnrolment(
	w http.ResponseWriter, r *http.Request, status int, page Page,
	enrolment mfa.Enrolment, problem string,
) {
	enrol := EnrolPage{
		Page:    page,
		Secret:  enrolment.Secret,
		URI:     provisioningURI(enrolment),
		Problem: problem,
	}

	body, err := render(enrolTemplate, enrol)
	if err != nil {
		h.serverError(w, r, "rendering the enrolment page", err)
		return
	}
	h.write(w, status, enrol.ContentSecurityPolicy(), body)
}

// enrolmentGone ends an enrolment that cannot continue.
func (h *Handler) enrolmentGone(w http.ResponseWriter, r *http.Request, page Page) {
	http.SetCookie(w, clearEnrolCookie())

	back := Path
	if page.RequestID != "" {
		back = Path + "?request=" + url.QueryEscape(page.RequestID)
	}

	h.notice(w, r, http.StatusOK, Notice{
		Title:    "Start again",
		Body:     MsgEnrolmentGone,
		BackPath: back,
	})
}

// --- audit ---------------------------------------------------------------------------------

func (h *Handler) auditEnrolmentForced(ctx context.Context, user authn.User, pending authorize.Pending) {
	if h.Audit == nil {
		return
	}
	err := h.DB.WithTenant(ctx, user.OrgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       user.OrgID,
			ActorUserID: user.ID,
			Type:        audit.EventMFAEnrolmentForced,
			Payload:     map[string]any{"client_id": pending.App.ID},
		})
	})
	if err != nil && h.Log != nil {
		h.Log.Warn("recording a forced enrolment failed", "error", err.Error())
	}
}

func (h *Handler) auditEnrolled(ctx context.Context, state EnrolState) {
	if h.Audit == nil {
		return
	}
	err := h.DB.WithTenant(ctx, state.OrgID, func(tx *postgres.Tx) error {
		return h.Audit.Write(ctx, tx, audit.Event{
			OrgID:       state.OrgID,
			ActorUserID: state.UserID,
			Type:        audit.EventMFAEnrolled,
			// The factor TYPE, never its secret and never the code that proved
			// it.
			Payload: map[string]any{"factor_type": string(mfa.TypeTOTP), "forced": true},
		})
	})
	if err != nil && h.Log != nil {
		h.Log.Warn("recording an enrolment failed", "error", err.Error())
	}
}

// provisioningURI pulls the `otpauth://` string out of a factor type's
// per-type material.
//
// Typed out of a `map[string]any` rather than given a field on `Enrolment`,
// because it is TOTP's alone — a WebAuthn enrolment's `Challenge` carries
// creation options and would have no use for the field. A missing or
// wrong-typed value yields the empty string and the page simply shows the key,
// which every authenticator app accepts.
func provisioningURI(enrolment mfa.Enrolment) string {
	uri, _ := enrolment.Challenge["provisioning_uri"].(string)
	return strings.TrimSpace(uri)
}
