package login

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/audit"
	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/oauth/authorize"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/social"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

const SocialPath = "/login/social/"
const MsgSocial = "Social sign-in could not be completed. Try again, or sign in to your existing account to link this provider."

type SocialProvider interface {
	AuthorizeURL(state, nonce, challenge string) (string, error)
	Exchange(ctx context.Context, code, verifier, nonce string) (social.ProviderIdentity, error)
}

type SocialStates interface {
	Issue(context.Context, social.StateBinding) (social.StateTransaction, error)
	Consume(context.Context, string, social.StateBinding) (social.StateTransaction, error)
}

type SocialIdentities interface {
	BySubject(context.Context, *postgres.Tx, social.Provider, string) (social.Identity, error)
}

type SocialQuota interface {
	Consume(context.Context, string, time.Time) ratelimit.Verdict
}

type SocialFlow struct {
	Issuer     string
	Providers  map[social.Provider]SocialProvider
	States     SocialStates
	Identities SocialIdentities
	Quota      SocialQuota
}

// SocialLogin binds callback context to a host-only browser cookie and the
// server's single-use state. Callback query parameters never select a tenant.
func (h *Handler) SocialLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, SocialPath)
	name, callback := strings.CutSuffix(path, "/callback")
	provider := social.Provider(name)
	flow := h.Social
	if !strings.HasPrefix(r.URL.Path, SocialPath) || !provider.Valid() || flow == nil || flow.Providers[provider] == nil || flow.States == nil || flow.Identities == nil {
		h.socialError(w, r, authorize.Pending{}, "", provider, "unavailable")
		return
	}
	if len(r.URL.RawQuery) > 8192 {
		h.socialError(w, r, authorize.Pending{}, "", provider, "invalid_request")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		h.socialError(w, r, authorize.Pending{}, "", provider, "invalid_request")
		return
	}
	for _, values := range query {
		if len(values) != 1 {
			h.socialError(w, r, authorize.Pending{}, "", provider, "invalid_request")
			return
		}
	}
	if flow.Quota != nil && !flow.Quota.Consume(r.Context(), string(provider)+":"+h.clientIP(r), h.now()).Allowed {
		h.socialError(w, r, authorize.Pending{}, "", provider, "rate_limited")
		return
	}
	id := query.Get("request")
	browser := ""
	if callback {
		cookie, cookieErr := r.Cookie(socialCookieName(provider))
		if cookieErr == nil {
			id, browser, _ = strings.Cut(cookie.Value, "~")
		}
		http.SetCookie(w, socialCookie(provider, "", -1))
	}
	if !validPendingID(id) || (callback && !validCSRFToken(browser)) {
		h.socialError(w, r, authorize.Pending{}, "", provider, "invalid_state")
		return
	}
	pending, err := h.Authorization.Peek(r.Context(), id)
	if err != nil || pending.ID != id {
		h.socialError(w, r, authorize.Pending{}, "", provider, "expired_request")
		return
	}
	binding := social.StateBinding{OrgID: pending.App.OrgID, PendingID: id,
		Provider: provider, Intent: social.SignInIntent,
		RedirectURI: flow.Issuer + SocialPath + string(provider) + "/callback", BrowserToken: browser}
	if !callback {
		if !h.socialAllowed(r.Context(), pending, provider) {
			h.socialError(w, r, pending, id, provider, "policy")
			return
		}
		binding.BrowserToken, err = newCSRFToken()
		if err != nil {
			h.socialError(w, r, pending, id, provider, "state")
			return
		}
		transaction, err := flow.States.Issue(r.Context(), binding)
		if err != nil {
			h.socialError(w, r, pending, id, provider, "state")
			return
		}
		target, err := flow.Providers[provider].AuthorizeURL(transaction.State, transaction.Nonce, transaction.PKCEChallenge)
		if err != nil {
			h.socialError(w, r, pending, id, provider, "provider")
			return
		}
		http.SetCookie(w, socialCookie(provider, id+"~"+binding.BrowserToken, int(social.StateLifetime.Seconds())))
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	transaction, err := flow.States.Consume(r.Context(), query.Get("state"), binding)
	if err != nil {
		h.socialError(w, r, pending, id, provider, "invalid_state")
		return
	}
	if query.Get("error") != "" || query.Get("code") == "" || len(query.Get("code")) > 4096 || !h.socialAllowed(r.Context(), pending, provider) {
		h.socialError(w, r, pending, id, provider, "refused")
		return
	}
	identity, err := flow.Providers[provider].Exchange(r.Context(), query.Get("code"), transaction.PKCEVerifier, transaction.Nonce)
	if err != nil || identity.Provider != provider {
		h.socialError(w, r, pending, id, provider, "provider")
		return
	}
	user, policy, err := h.socialUser(r.Context(), pending, identity)
	if err != nil {
		h.socialError(w, r, pending, id, provider, "account", user)
		return
	}
	h.completeSocial(w, r, pending, user, policy, provider)
}

func socialCookieName(provider social.Provider) string {
	return "__Host-zedauth_social_" + string(provider)
}

func socialCookie(provider social.Provider, value string, age int) *http.Cookie {
	return &http.Cookie{Name: socialCookieName(provider), Value: value, Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age}
}

func (h *Handler) socialAllowed(ctx context.Context, pending authorize.Pending, provider social.Provider) bool {
	allowed := false
	err := h.DB.WithTenant(ctx, pending.App.OrgID, func(tx *postgres.Tx) error {
		policy, err := h.Policies.LoginPolicy(ctx, tx, pending.App.OrgID)
		allowed = err == nil && policy.Allows(string(provider))
		return err
	})
	return err == nil && allowed
}

func (h *Handler) socialUser(ctx context.Context, pending authorize.Pending, identity social.ProviderIdentity) (authn.User, authn.LoginPolicy, error) {
	var user authn.User
	var policy authn.LoginPolicy
	err := h.DB.WithTenant(ctx, pending.App.OrgID, func(tx *postgres.Tx) error {
		var err error
		policy, err = h.Policies.LoginPolicy(ctx, tx, pending.App.OrgID)
		if err != nil {
			return err
		}
		if !policy.Allows(string(identity.Provider)) {
			return errors.New("login: social policy refused")
		}
		linked, err := h.Social.Identities.BySubject(ctx, tx, identity.Provider, identity.Subject)
		if err != nil {
			return err
		}
		user, err = h.Users.ByID(ctx, tx, linked.UserID)
		if err != nil {
			return err
		}
		if user.ID != linked.UserID || user.OrgID != pending.App.OrgID || !user.CanSignIn() {
			return errors.New("login: social account refused")
		}
		return nil
	})
	return user, policy, err
}

// Re-read account and policy after network/MFA work, immediately before issuance.
func (h *Handler) socialUserForIssue(ctx context.Context, tx *postgres.Tx, pending authorize.Pending, user authn.User, method string, factored bool) (authn.User, authn.LoginPolicy, error) {
	var policy authn.LoginPolicy
	if !social.Provider(method).Valid() {
		return user, policy, errors.New("login: invalid primary method")
	}
	policy, err := h.Policies.LoginPolicy(ctx, tx, pending.App.OrgID)
	if err != nil {
		return user, policy, err
	}
	current, err := h.Users.ByID(ctx, tx, user.ID)
	if err != nil {
		return user, policy, err
	}
	if !policy.Allows(method) || current.ID != user.ID || current.OrgID != pending.App.OrgID || !current.CanSignIn() || (!factored && authn.RequireMFA(policy, false, h.now()) == authn.MFAEnrolmentRequired) {
		return user, policy, errors.New("login: social account or policy changed")
	}
	return current, policy, nil
}

func (h *Handler) completeSocial(w http.ResponseWriter, r *http.Request, pending authorize.Pending, user authn.User, policy authn.LoginPolicy, provider social.Provider) {
	method := string(provider)
	var decision mfa.Decision
	var err error
	if h.MFA != nil {
		framework, ok := h.MFA.(interface {
			RequiredWithMethod(context.Context, string, string, string, string) (mfa.Decision, error)
		})
		if !ok {
			h.socialError(w, r, pending, pending.ID, provider, "mfa", user)
			return
		}
		decision, err = framework.RequiredWithMethod(r.Context(), user.ID, user.OrgID, pending.ID, method)
		if err != nil {
			h.socialError(w, r, pending, pending.ID, provider, "mfa", user)
			return
		}
	}
	if decision.Challenge || authn.RequireMFA(policy, false, h.now()) == authn.MFAEnrolmentRequired {
		page, err := h.page(r.Context(), pending, pending.ID)
		if err != nil {
			h.socialError(w, r, pending, pending.ID, provider, "page", user)
			return
		}
		page.CSRFToken, err = h.csrfFor(w, r)
		if err != nil {
			h.socialError(w, r, pending, pending.ID, provider, "csrf", user)
			return
		}
		if decision.Challenge {
			h.auditChallenged(r.Context(), user, pending)
			http.SetCookie(w, challengeCookie(decision.Handle))
			h.count(OutcomeChallenged)
			h.showChallenge(w, r, http.StatusOK, page, mfa.Offer{Types: decision.Offered, Recovery: decision.Recovery, WebAuthnOptions: decision.WebAuthnOptions}, "")
			return
		}
		handle, enrolment, err := h.beginForcedEnrolmentWithMethod(r.Context(), user, pending, method)
		if err != nil {
			h.socialError(w, r, pending, pending.ID, provider, "mfa", user)
			return
		}
		http.SetCookie(w, enrolCookie(handle))
		h.count(OutcomeChallenged)
		h.showEnrolment(w, r, http.StatusOK, page, enrolment, "")
		return
	}
	out, current, invalidate := h.issueWithMethod(r, pending, user, policy, nil, false, method)
	if out.result != resultAuthenticated {
		h.socialError(w, r, pending, pending.ID, provider, "session", user)
		return
	}
	if invalidate != nil {
		_ = invalidate(r.Context())
	}
	http.SetCookie(w, session.Cookie(out.token))
	h.count(OutcomeSuccess)
	h.Authorization.Resume(w, r, pending.ID, current)
}

func (h *Handler) socialError(w http.ResponseWriter, r *http.Request, pending authorize.Pending, id string, provider social.Provider, reason string, known ...authn.User) {
	h.count(OutcomeFailed)
	if pending.App.OrgID != "" {
		actor := ""
		if len(known) == 1 && known[0].OrgID == pending.App.OrgID {
			actor = known[0].ID
		}
		err := h.DB.WithTenant(r.Context(), pending.App.OrgID, func(tx *postgres.Tx) error {
			return h.Audit.Write(r.Context(), tx, audit.Event{OrgID: pending.App.OrgID, ActorUserID: actor, Type: audit.EventLoginFailed,
				Payload: map[string]any{"provider": string(provider), "reason": reason}, IP: h.clientIP(r)})
		})
		if err != nil && h.Log != nil {
			h.Log.Warn("writing social login failure audit failed")
		}
	}
	if id != "" {
		page, err := h.page(r.Context(), pending, id)
		if err == nil {
			page.CSRFToken, err = h.csrfFor(w, r)
			if err == nil {
				page.Error = MsgSocial
				h.renderPage(w, r, http.StatusOK, page)
				return
			}
		}
	}
	h.notice(w, r, http.StatusBadRequest, Notice{Title: "Sign-in could not be completed", Body: MsgSocial})
}
