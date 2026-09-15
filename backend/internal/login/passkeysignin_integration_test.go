//go:build integration

// Signing in with a passkey, through the real login handler and relying party
// (P3-14 step 2).
//
// P3-05 proved the verifier against genuinely signed assertions, and P3-10's
// E2E test signs in with a browser's virtual authenticator. Between them was
// nothing: every login-handler test of the passkey step used a fake ceremony
// that accepts anything. So "the handler hands a real assertion to the real
// relying party, and the session it opens says `hwk`" was a property two
// layers each assumed of the other.
package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/authn"
	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport/webauthntest"
)

var challengeOptionsPattern = regexp.MustCompile(`(?s)<script type="application/json" id="passkey-options">(.*?)</script>`)

// registeredPasskey registers a passkey for the fixture user on the hosted page
// and points the login framework at the same relying party.
func (s *stack) registeredPasskey(t *testing.T) *webauthntest.Authenticator {
	t.Helper()
	s.withPasskeys(t)

	cookie := s.signIn(t)
	page := s.openPasskeys(t, "", cookie)
	key := webauthntest.New(t)
	w := s.submitPasskey(t, page, key.Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, page.options)), cookie, true)
	if !strings.Contains(w.Body.String(), "Passkey added") {
		t.Fatalf("setup: the passkey was not registered:\n%s", w.Body.String())
	}

	s.login.MFA = &mfa.Framework{
		Registry:   mfa.NewRegistry(),
		Store:      &mfa.PostgresFactors{Store: mfa.NewStore(), DB: s.db},
		Challenges: mfa.NewRedisChallenges(s.rdb),
		Attempts:   &mfa.RedisAttempts{Client: s.rdb},
		WebAuthn:   s.login.PasskeyRegistration.Ceremony.(*mfa.WebAuthnVerifier),
		Log:        discard(),
	}
	return key
}

// passkeyChallenge signs in with the password and returns the challenge the
// page issued, the handle, and the request id.
func (s *stack) passkeyChallenge(t *testing.T) (string, *http.Cookie, string) {
	t.Helper()
	id := s.begin(t)
	w := s.submit(t, id, s.form(t, id), testEmail, testPassword)
	if sessionCookieSet(w) {
		t.Fatal("a session was issued before the passkey step")
	}
	m := challengeOptionsPattern.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("the challenge page offers no passkey:\n%s", w.Body.String())
	}
	return webauthntest.ChallengeFrom(t, []byte(m[1])), handleFrom(t, w), id
}

func (s *stack) assertion(t *testing.T, id string, assertion []byte, handle *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	token := s.form(t, id)
	form := url.Values{"request": {id}, csrfField: {token}, "assertion": {string(assertion)}}
	r := httptest.NewRequest(http.MethodPost, WebAuthnPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	r.AddCookie(handle)
	w := httptest.NewRecorder()
	s.login.WebAuthnStep(w, r)
	return w
}

func TestSigningInWithARealPasskeyOpensAnHwkSession(t *testing.T) {
	s := setup(t)
	key := s.registeredPasskey(t)

	challenge, handle, id := s.passkeyChallenge(t)
	w := s.assertion(t, id, key.Assert(t, rpID, rpOrigin, challenge), handle)
	if !sessionCookieSet(w) {
		t.Fatalf("a genuinely signed assertion did not sign in (status %d):\n%s", w.Code, w.Body.String())
	}

	var methods []string
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT auth_methods FROM sessions WHERE user_id = $1 AND revoked_at IS NULL
			  ORDER BY created_at DESC LIMIT 1`, s.userID).Scan(pq.Array(&methods))
	}); err != nil {
		t.Fatalf("reading the session: %v", err)
	}
	if want := mfa.AuthMethods(true, mfa.TypeWebAuthn); strings.Join(methods, ",") != strings.Join(want, ",") {
		t.Errorf("auth_methods = %v, want %v — a consumer demanding hwk depends on this", methods, want)
	}
}

// A credential the user never registered — a real signature from the wrong key
// — does not complete the login. The positive control above is what makes this
// refusal mean something.
func TestAnAssertionFromAnUnregisteredKeyIsRefused(t *testing.T) {
	s := setup(t)
	_ = s.registeredPasskey(t)

	challenge, handle, id := s.passkeyChallenge(t)
	w := s.assertion(t, id, webauthntest.New(t).Assert(t, rpID, rpOrigin, challenge), handle)
	if sessionCookieSet(w) {
		t.Error("an assertion from a key that was never registered signed the user in")
	}
}

// Abuse case (P3-05 card): registering a credential to another user's account.
// A colleague with their own session opens the hosted page and submits a
// credential; it lands on THEIR account, never on the victim's, whatever the
// ceremony was begun with.
func TestAPasskeyRegistersOnlyToTheSignedInUser(t *testing.T) {
	s := setup(t)
	s.withPasskeys(t)

	const colleagueEmail, colleaguePassword = "bob@example.test", "another correct horse battery staple"
	colleague := s.factory.User(s.orgID, colleagueEmail)
	hash, err := authn.Hash(colleaguePassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	s.factory.Exec(`UPDATE users SET password_hash = $2 WHERE id = $1`, colleague, hash)

	victimCookie := s.signIn(t)
	id := s.begin(t)
	w := s.submit(t, id, s.form(t, id), colleagueEmail, colleaguePassword)
	var colleagueCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName && c.Value != "" {
			colleagueCookie = &http.Cookie{Name: c.Name, Value: c.Value}
		}
	}
	if colleagueCookie == nil {
		t.Fatalf("setup: the colleague could not sign in: %d", w.Code)
	}

	// The victim's ceremony, finished by the colleague's session.
	victimPage := s.openPasskeys(t, "", victimCookie)
	credential := webauthntest.New(t).Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, victimPage.options))
	s.submitPasskey(t, victimPage, credential, colleagueCookie, true)

	// The colleague's own ceremony, registered normally: it must be theirs.
	own := s.openPasskeys(t, "", colleagueCookie)
	ownCredential := webauthntest.New(t).Register(t, rpID, rpOrigin, webauthntest.ChallengeFrom(t, own.options))
	if w := s.submitPasskey(t, own, ownCredential, colleagueCookie, true); !strings.Contains(w.Body.String(), "Passkey added") {
		t.Fatalf("positive control: the colleague could not register their own passkey:\n%s", w.Body.String())
	}

	var victims, colleagues int
	s.factory.QueryRow(&victims, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1 AND type = 'webauthn'`, s.userID)
	s.factory.QueryRow(&colleagues, `SELECT count(*) FROM user_mfa_factors WHERE user_id = $1 AND type = 'webauthn' AND status = 'active'`, colleague)
	if victims != 0 {
		t.Errorf("the victim's account holds %d passkeys registered by somebody else's session", victims)
	}
	if colleagues != 1 {
		t.Errorf("the colleague holds %d passkeys, want exactly their own 1", colleagues)
	}
}
