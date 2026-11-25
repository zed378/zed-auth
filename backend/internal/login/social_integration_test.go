//go:build integration

package login

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/social"
)

func (s *stack) socialStart(t *testing.T, provider social.Provider, linked bool) (string, *http.Cookie, string) {
	t.Helper()
	s.factory.Exec(`UPDATE organizations SET settings = jsonb_set(settings, '{allowed_login_methods}', $2::jsonb) WHERE id=$1`, s.orgID, `["password","`+string(provider)+`"]`)
	if linked {
		s.factory.Exec(`INSERT INTO user_identities(user_id,provider,provider_subject) VALUES($1,$2,'stable-subject')`, s.userID, string(provider))
	}
	s.login.Social = &SocialFlow{Issuer: "https://auth.example.test", Providers: map[social.Provider]SocialProvider{provider: &socialProviderFake{provider: provider}},
		States: social.NewStateStore(s.rdb), Identities: social.NewStore()}
	id := s.begin(t)
	w := httptest.NewRecorder()
	s.login.SocialLogin(w, httptest.NewRequest("GET", SocialPath+string(provider)+"?request="+id, nil))
	if w.Code != 302 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	target, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == socialCookieName(provider) {
			return id, cookie, target.Query().Get("state")
		}
	}
	t.Fatal("no binding cookie")
	return "", nil, ""
}

func (s *stack) socialCallback(provider social.Provider, cookie *http.Cookie, state string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", SocialPath+string(provider)+"/callback?"+url.Values{"state": {state}, "code": {"private-code"}}.Encode(), nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.login.SocialLogin(w, r)
	return w
}

func (s *stack) socialSession(t *testing.T, w *httptest.ResponseRecorder) session.Session {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName {
			current, err := s.sessions.Lookup(context.Background(), c.Value, session.DefaultPolicy, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			return current
		}
	}
	t.Fatalf("no session: %d %s", w.Code, w.Body.String())
	return session.Session{}
}

func TestSocialHostedFlowWithPostgresAndRedis(t *testing.T) {
	for _, provider := range []social.Provider{social.Google, social.Microsoft, social.GitHub} {
		t.Run(string(provider), func(t *testing.T) {
			s := setup(t)
			_, cookie, state := s.socialStart(t, provider, true)
			w := s.socialCallback(provider, cookie, state)
			if w.Code != 302 {
				t.Fatalf("callback: %d %s", w.Code, w.Body.String())
			}
			current := s.socialSession(t, w)
			if current.UserID != s.userID || current.OrgID != s.orgID || !slices.Equal(current.AuthMethods, []string{string(provider)}) {
				t.Fatalf("wrong session: %+v", current)
			}
			w = s.socialCallback(provider, cookie, state)
			if w.Code == 302 || !strings.Contains(w.Body.String(), MsgSocial) {
				t.Fatal("state replay accepted")
			}
		})
	}
}

func TestSocialHostedRefusalsDoNotCreateAccountsOrSessions(t *testing.T) {
	for _, scenario := range []string{"unlinked", "changed-policy", "deactivated", "cross-tenant"} {
		t.Run(scenario, func(t *testing.T) {
			s := setup(t)
			_, cookie, state := s.socialStart(t, social.Google, scenario != "unlinked" && scenario != "cross-tenant")
			switch scenario {
			case "changed-policy":
				s.factory.Exec(`UPDATE organizations SET settings='{}'::jsonb WHERE id=$1`, s.orgID)
			case "deactivated":
				s.factory.Exec(`UPDATE users SET status='deactivated' WHERE id=$1`, s.userID)
			case "cross-tenant":
				otherOrg := s.factory.Organization(s.factory.Instance())
				otherUser := s.factory.User(otherOrg)
				s.factory.Exec(`INSERT INTO user_identities(user_id,provider,provider_subject) VALUES($1,'google','stable-subject')`, otherUser)
			}
			var usersBefore int
			s.factory.QueryRow(&usersBefore, `SELECT count(*) FROM users`)
			w := s.socialCallback(social.Google, cookie, state)
			if w.Code == 302 || !strings.Contains(w.Body.String(), MsgSocial) {
				t.Fatalf("refused callback: %d %s", w.Code, w.Body.String())
			}
			var usersAfter, sessions int
			s.factory.QueryRow(&usersAfter, `SELECT count(*) FROM users`)
			s.factory.QueryRow(&sessions, `SELECT count(*) FROM sessions`)
			if usersAfter != usersBefore || sessions != 0 {
				t.Fatalf("users %d -> %d, sessions %d", usersBefore, usersAfter, sessions)
			}
		})
	}
}

func TestSocialMFAKeepsProviderThroughChallenge(t *testing.T) {
	s := setup(t)
	secret, clock := s.enrolled(t, time.Now().Truncate(mfa.TOTPPeriod))
	id, cookie, state := s.socialStart(t, social.Google, true)
	w := s.socialCallback(social.Google, cookie, state)
	if w.Code != 200 {
		t.Fatalf("challenge: %d %s", w.Code, w.Body.String())
	}
	var challenge *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == ChallengeCookieName {
			challenge = c
		}
		if c.Name == session.CookieName {
			t.Fatal("session before MFA")
		}
	}
	if challenge == nil {
		t.Fatal("no MFA challenge")
	}
	w = s.code(t, id, mfa.TOTPCode(secret, mfa.TOTPCounter(clock)), challenge)
	if w.Code != 302 {
		t.Fatalf("MFA: %d %s", w.Code, w.Body.String())
	}
	current := s.socialSession(t, w)
	if !slices.Equal(current.AuthMethods, []string{"google", "mfa", "otp"}) {
		t.Fatalf("amr: %v", current.AuthMethods)
	}
	var methods pq.StringArray
	s.factory.QueryRow(&methods, `SELECT auth_methods FROM sessions WHERE id=$1`, current.ID)
	if slices.Contains(methods, "pwd") {
		t.Fatal("social session claims password")
	}
}

func TestSocialMFARechecksPolicyAfterChallenge(t *testing.T) {
	s := setup(t)
	secret, clock := s.enrolled(t, time.Now().Truncate(mfa.TOTPPeriod))
	id, cookie, state := s.socialStart(t, social.Google, true)
	w := s.socialCallback(social.Google, cookie, state)
	var challenge *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == ChallengeCookieName {
			challenge = c
		}
	}
	if challenge == nil {
		t.Fatal("no MFA challenge")
	}
	s.factory.Exec(`UPDATE organizations SET settings='{}'::jsonb WHERE id=$1`, s.orgID)
	w = s.code(t, id, mfa.TOTPCode(secret, mfa.TOTPCounter(clock)), challenge)
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName {
			t.Fatal("policy bypass after MFA")
		}
	}
	var count int
	s.factory.QueryRow(&count, `SELECT count(*) FROM sessions`)
	if count != 0 {
		t.Fatal("session issued despite revoked policy")
	}
}

func TestSocialAuditContainsNoProviderCredentials(t *testing.T) {
	s := setup(t)
	_, cookie, state := s.socialStart(t, social.GitHub, true)
	w := s.socialCallback(social.GitHub, cookie, state)
	current := s.socialSession(t, w)
	var payload []byte
	s.factory.QueryRow(&payload, `SELECT payload FROM events WHERE event_type='user.login.success' AND actor_user_id=$1 ORDER BY created_at DESC LIMIT 1`, current.UserID)
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "private-code") || strings.Contains(string(payload), state) || strings.Contains(string(payload), "stable-subject") {
		t.Fatal("provider credentials in audit")
	}
}
