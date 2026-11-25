package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/ratelimit"
	"github.com/zed378/zed-auth/backend/internal/social"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

type socialProviderFake struct {
	provider social.Provider
	calls    int
	err      error
}

func (p *socialProviderFake) AuthorizeURL(state, nonce, challenge string) (string, error) {
	return "https://provider.example/authorize?state=" + state, nil
}
func (p *socialProviderFake) Exchange(context.Context, string, string, string) (social.ProviderIdentity, error) {
	p.calls++
	return social.ProviderIdentity{Provider: p.provider, Subject: "stable-subject"}, p.err
}

type socialStateFake struct {
	binding social.StateBinding
	spent   bool
}

func (s *socialStateFake) Issue(_ context.Context, b social.StateBinding) (social.StateTransaction, error) {
	s.binding = b
	return social.StateTransaction{State: "opaque", Nonce: "nonce", PKCEChallenge: "challenge"}, nil
}
func (s *socialStateFake) Consume(_ context.Context, state string, b social.StateBinding) (social.StateTransaction, error) {
	spent := s.spent
	s.spent = true
	if spent || state != "opaque" || b != s.binding {
		return social.StateTransaction{}, social.ErrInvalidState
	}
	return social.StateTransaction{PKCEVerifier: "verifier", Nonce: "nonce"}, nil
}

type socialIdentityFake struct {
	user  string
	err   error
	calls int
}

func (s *socialIdentityFake) BySubject(_ context.Context, _ *postgres.Tx, provider social.Provider, subject string) (social.Identity, error) {
	s.calls++
	if subject != "stable-subject" || !provider.Valid() {
		return social.Identity{}, social.ErrNotFound
	}
	return social.Identity{UserID: s.user}, s.err
}

func socialFixture(t *testing.T, provider social.Provider) (*fixture, *socialProviderFake, *socialStateFake, *socialIdentityFake) {
	t.Helper()
	f := newFixture(t)
	f.users.user = authn.User{ID: testUserID, OrgID: testOrgID, Status: "active"}
	policy := authn.DefaultLoginPolicy
	policy.AllowedMethods = []string{"password", string(provider)}
	f.handler.Policies = fakePolicies{login: &policy}
	p := &socialProviderFake{provider: provider}
	s := &socialStateFake{}
	i := &socialIdentityFake{user: testUserID}
	f.handler.Social = &SocialFlow{Issuer: "https://auth.example", Providers: map[social.Provider]SocialProvider{provider: p}, States: s, Identities: i}
	return f, p, s, i
}

func startSocial(t *testing.T, f *fixture, provider social.Provider) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	f.handler.SocialLogin(w, httptest.NewRequest("GET", SocialPath+string(provider)+"?request="+testPendingID, nil))
	if w.Code != http.StatusFound {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == socialCookieName(provider) {
			if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || c.Path != "/" {
				t.Fatal("unsafe browser cookie")
			}
			return c
		}
	}
	t.Fatal("no browser binding cookie")
	return nil
}

func callbackSocial(f *fixture, provider social.Provider, cookie *http.Cookie, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", SocialPath+string(provider)+"/callback?"+query, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.SocialLogin(w, r)
	return w
}

func TestSocialProvidersCompleteOnlyLinkedAccounts(t *testing.T) {
	for _, provider := range []social.Provider{social.Google, social.Microsoft, social.GitHub} {
		t.Run(string(provider), func(t *testing.T) {
			f, p, _, _ := socialFixture(t, provider)
			cookie := startSocial(t, f, provider)
			w := callbackSocial(f, provider, cookie, "state=opaque&code=private-code")
			if w.Code != 302 || len(f.sessions.created) != 1 || p.calls != 1 {
				t.Fatalf("callback: %d %s", w.Code, w.Body.String())
			}
			if !slices.Equal(f.sessions.created[0].AuthMethods, []string{string(provider)}) {
				t.Fatalf("amr: %v", f.sessions.created[0].AuthMethods)
			}
			if len(f.auth.resumed) != 1 || len(f.users.rehashed) != 0 {
				t.Fatal("unexpected password work or no authorization")
			}
			w = callbackSocial(f, provider, cookie, "state=opaque&code=private-code")
			if len(f.sessions.created) != 1 || p.calls != 1 || !strings.Contains(w.Body.String(), MsgSocial) {
				t.Fatal("replayed callback accepted")
			}
		})
	}
}

func TestSocialCallbackRejectsAbuseBeforeIssuingSession(t *testing.T) {
	for _, scenario := range []string{"unlinked", "locked", "wrong-tenant", "policy-changed", "missing-cookie", "wrong-browser", "wrong-state", "duplicate-state", "provider-error", "provider-mismatch", "mfa-mandate"} {
		t.Run(scenario, func(t *testing.T) {
			f, p, _, identities := socialFixture(t, social.Google)
			cookie := startSocial(t, f, social.Google)
			query := "state=opaque&code=private-code"
			switch scenario {
			case "unlinked":
				identities.err = social.ErrNotFound
			case "locked":
				f.users.user.Status = "locked"
			case "wrong-tenant":
				f.users.user.OrgID = "different-tenant"
			case "policy-changed":
				f.handler.Policies = fakePolicies{}
			case "missing-cookie":
				cookie = nil
			case "wrong-browser":
				cookie.Value = testPendingID + "~" + strings.Repeat("A", 43)
			case "wrong-state":
				query = "state=wrong&code=private-code"
			case "duplicate-state":
				query += "&state=opaque"
			case "provider-error":
				p.err = errors.New("private-provider-token")
			case "provider-mismatch":
				p.provider = social.GitHub
			case "mfa-mandate":
				policy := authn.DefaultLoginPolicy
				policy.AllowedMethods = []string{"google"}
				policy.MFARequired = true
				policy.MFARequiredSince = time.Now().Add(-30 * 24 * time.Hour)
				f.handler.Policies = fakePolicies{login: &policy}
			}
			w := callbackSocial(f, social.Google, cookie, query)
			if len(f.sessions.created) != 0 || len(f.auth.resumed) != 0 {
				t.Fatal("refused attempt created a session")
			}
			if !strings.Contains(w.Body.String(), MsgSocial) || strings.Contains(w.Body.String(), "private-") || w.Header().Get("Location") != "" {
				t.Fatal("non-generic failure")
			}
			if scenario == "policy-changed" || scenario == "missing-cookie" || scenario == "wrong-browser" || scenario == "wrong-state" || scenario == "duplicate-state" {
				if p.calls != 0 || identities.calls != 0 {
					t.Fatal("untrusted attempt reached provider or identity lookup")
				}
			}
		})
	}
}

func TestHostedSocialButtonsRespectPolicyAndConfiguration(t *testing.T) {
	f, _, _, _ := socialFixture(t, social.Google)
	w, _ := f.get(t)
	if !strings.Contains(w.Body.String(), "Continue with Google") || strings.Contains(w.Body.String(), "Continue with GitHub") {
		t.Fatal("provider buttons do not match configuration")
	}
	f.handler.Policies = fakePolicies{}
	w, _ = f.get(t)
	if strings.Contains(w.Body.String(), "Continue with Google") {
		t.Fatal("policy-disabled button shown")
	}
}

func TestSocialIssueRechecksPolicyAndAccount(t *testing.T) {
	for _, scenario := range []string{"policy", "status", "tenant", "method", "mandate"} {
		t.Run(scenario, func(t *testing.T) {
			f, _, _, _ := socialFixture(t, social.Google)
			user := f.users.user
			method := "google"
			switch scenario {
			case "policy":
				f.handler.Policies = fakePolicies{}
			case "status":
				f.users.user.Status = "deactivated"
			case "tenant":
				f.users.user.OrgID = "other"
			case "method":
				method = "unrecognized"
			case "mandate":
				policy := authn.DefaultLoginPolicy
				policy.AllowedMethods = []string{"google"}
				policy.MFARequired = true
				policy.MFARequiredSince = time.Now().Add(-30 * 24 * time.Hour)
				f.handler.Policies = fakePolicies{login: &policy}
			}
			out, _, _ := f.handler.issueWithMethod(httptest.NewRequest("GET", "/", nil), f.auth.pending, user, authn.DefaultLoginPolicy, nil, false, method)
			if out.result != resultError || len(f.sessions.created) != 0 {
				t.Fatal("issuance ignored changed security state")
			}
		})
	}
}

func TestSocialAuthMethodsDoNotInventPasswordOrFactor(t *testing.T) {
	for _, provider := range []string{"google", "microsoft", "github"} {
		if got := mfa.AuthMethodsWithPrimary(provider, false); !slices.Equal(got, []string{provider}) {
			t.Fatalf("primary: %v", got)
		}
		got := mfa.AuthMethodsWithPrimary(provider, false, mfa.TypeTOTP)
		if slices.Contains(got, "pwd") || !slices.Contains(got, "otp") || !slices.Contains(got, "mfa") {
			t.Fatalf("factored: %v", got)
		}
	}
}

func TestSocialForcedEnrollmentPreservesProvider(t *testing.T) {
	f, _, _, _ := socialFixture(t, social.Google)
	policy := authn.DefaultLoginPolicy
	policy.AllowedMethods = []string{"google"}
	policy.MFARequired = true
	policy.MFARequiredSince = time.Now().Add(-30 * 24 * time.Hour)
	f.handler.Policies = fakePolicies{login: &policy}
	enroller := &fakeEnroller{secret: "JBSWY3DPEHPK3PXP", code: "123456"}
	store := newMemoryEnrolments()
	f.handler.Enrol, f.handler.Enrolments = enroller, store
	cookie := startSocial(t, f, social.Google)
	w := callbackSocial(f, social.Google, cookie, "state=opaque&code=private-code")
	handle := enrolHandle(t, w)
	state, err := store.Get(context.Background(), handle)
	if err != nil || state.PrimaryMethod != "google" || len(f.sessions.created) != 0 {
		t.Fatalf("forced enrollment lost social identity: %+v %v", state, err)
	}
	_, csrf := f.get(t)
	w = f.postEnrolment(t, enroller.code, handle, csrf)
	if !signedIn(f) || len(f.sessions.created) != 1 || !slices.Equal(f.sessions.created[0].AuthMethods, []string{"google", "mfa", "otp"}) {
		t.Fatalf("social enrollment: %d %s", w.Code, w.Body.String())
	}
}

type socialQuotaFake struct{ keys []string }

func (q *socialQuotaFake) Consume(_ context.Context, key string, _ time.Time) ratelimit.Verdict {
	q.keys = append(q.keys, key)
	return ratelimit.Verdict{Allowed: false}
}

func TestSocialRateLimitPrecedesProviderAndIdentityWork(t *testing.T) {
	f, provider, state, identities := socialFixture(t, social.Google)
	quota := &socialQuotaFake{}
	f.handler.Social.Quota = quota
	w := httptest.NewRecorder()
	f.handler.SocialLogin(w, httptest.NewRequest("GET", SocialPath+"google?request="+testPendingID, nil))
	if len(quota.keys) != 1 || !strings.HasPrefix(quota.keys[0], "google:") || provider.calls != 0 || identities.calls != 0 || state.binding.PendingID != "" || !strings.Contains(w.Body.String(), MsgSocial) {
		t.Fatal("rate limited request reached social authentication work")
	}
}

func TestSocialFailureAuditsKnownUserWithoutProviderSecrets(t *testing.T) {
	f, _, _, _ := socialFixture(t, social.Google)
	cookie := startSocial(t, f, social.Google)
	f.users.user.Status = "locked"
	callbackSocial(f, social.Google, cookie, "state=opaque&code=private-code")
	if len(f.auditor.events) != 1 {
		t.Fatalf("audit events: %d", len(f.auditor.events))
	}
	event := f.auditor.events[0]
	if event.ActorUserID != testUserID || event.OrgID != testOrgID || event.Payload["provider"] != "google" || event.Payload["reason"] != "account" {
		t.Fatalf("missing social audit context: %+v", event)
	}
}
