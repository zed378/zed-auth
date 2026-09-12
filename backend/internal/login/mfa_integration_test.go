//go:build integration

// The challenge step end to end (P3-03).
//
// Real Postgres, real Redis, a real TOTP secret sealed in the real column, and
// codes computed the way an authenticator app computes them. Nothing is faked
// below the handler, which is the point: the unit tests prove the handler's
// branch table, and this proves the branches are wired to the things they name.
//
// `docs/PLAN/11` § E2E names one case literally — correct password, wrong code,
// rejected. It is the first test here and it asserts on what the browser is
// given rather than on which branch ran.
package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/session"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// enrolled gives the fixture's user a confirmed TOTP factor and wires the
// handler to a real framework.
//
// It returns the secret and the moment the LOGIN happens, which is one step
// after enrolment — and that gap is not tidiness. Confirming an enrolment
// spends its counter, so the code that proved the factor works cannot then be
// used to sign in with. The first draft of these tests reused it and read the
// refusal as a broken verifier; it was `P3-02`'s replay bound doing exactly
// what it is for, reached end to end for the first time.
func (s *stack) enrolled(t *testing.T, at time.Time) ([]byte, time.Time) {
	t.Helper()

	// One clock both the verifier and the framework read, so the test can move
	// past the enrolment step without rebuilding either.
	clock := at

	sealer, err := mfa.NewSealer([]byte("an-integration-test-key-long-enough"))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	factorStore := mfa.NewStore()

	totp := &mfa.TOTP{
		Store:  factorStore,
		DB:     s.db,
		Sealer: sealer,
		Log:    discard(),
		Issuer: "auth.example.test",
		OrgOf: func(ctx context.Context, factorID string) (string, error) {
			// Through the SECURITY DEFINER function the migration adds, so
			// this test exercises the same door production uses rather than
			// short-circuiting to the org it already knows.
			var orgID string
			err := s.db.SQL().QueryRowContext(ctx, `SELECT mfa_factor_org($1)`, factorID).Scan(&orgID)
			return orgID, err
		},
		Now: func() time.Time { return clock },
	}

	enrolment, err := totp.Begin(context.Background(), s.userID, s.orgID, "phone")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	secret, err := mfa.DecodeTOTPSecret(enrolment.Secret)
	if err != nil {
		t.Fatalf("DecodeTOTPSecret: %v", err)
	}
	if err := totp.Confirm(context.Background(), enrolment.FactorID,
		mfa.TOTPCode(secret, mfa.TOTPCounter(at))); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// The next step. Within the skew window, so the code is valid, and
	// strictly past the counter the confirmation spent.
	clock = at.Add(mfa.TOTPPeriod)

	s.login.MFA = &mfa.Framework{
		Registry:   mfa.NewRegistry(totp),
		Store:      &mfa.PostgresFactors{Store: factorStore, DB: s.db},
		Challenges: mfa.NewRedisChallenges(s.rdb),
		Attempts:   &mfa.RedisAttempts{Client: s.rdb},
		Log:        discard(),
		Now:        func() time.Time { return clock },
	}
	return secret, clock
}

// code submits one code to the challenge step.
func (s *stack) code(t *testing.T, id, value string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	token := s.form(t, id)

	form := url.Values{
		"request": {id},
		csrfField: {token},
		"factor":  {string(mfa.TypeTOTP)},
		"code":    {value},
	}

	r := httptest.NewRequest(http.MethodPost, MFAPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	for _, c := range cookies {
		r.AddCookie(c)
	}

	w := httptest.NewRecorder()
	s.login.MFAStep(w, r)
	return w
}

// handleFrom extracts the challenge cookie a response set.
func handleFrom(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == ChallengeCookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no challenge cookie was set (status %d):\n%s", w.Code, w.Body.String())
	return nil
}

func sessionCookieSet(w *httptest.ResponseRecorder) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == session.CookieName && c.Value != "" {
			return true
		}
	}
	return false
}

// --- the case docs/PLAN/11 names ----------------------------------------------------

// DoD item 1: a correct password with a wrong code is rejected.
//
// Against the real chain. The password IS correct — asserted by the challenge
// being issued at all — and the code is wrong, and the browser ends with no
// session cookie and no authorization code.
func TestCorrectPasswordAndWrongCodeIsRejectedEndToEnd(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	_, at = s.enrolled(t, at)

	id := s.begin(t)
	token := s.form(t, id)

	first := s.submit(t, id, token, testEmail, testPassword)
	if sessionCookieSet(first) {
		t.Fatal("a user with a confirmed factor was given a session on the password alone")
	}
	if first.Code == http.StatusFound {
		t.Fatalf("the password step redirected, so the authorization flow completed without a factor")
	}
	handle := handleFrom(t, first)

	// A code that is not the one the app would show.
	wrong := s.code(t, id, "000000", handle)

	if sessionCookieSet(wrong) {
		t.Error("a wrong code produced a session")
	}
	if wrong.Code == http.StatusFound {
		t.Error("a wrong code resumed the authorization flow")
	}
	if !strings.Contains(wrong.Body.String(), MsgWrongCode) {
		t.Errorf("the response does not report a wrong code:\n%s", wrong.Body.String())
	}
}

// The positive control. Without it the test above passes against a service
// that refuses every code, which would be a service nobody can sign in to.
func TestTheCodeAnAuthenticatorWouldShowCompletesTheLogin(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	secret, at := s.enrolled(t, at)

	id := s.begin(t)
	token := s.form(t, id)

	first := s.submit(t, id, token, testEmail, testPassword)
	handle := handleFrom(t, first)

	w := s.code(t, id, mfa.TOTPCode(secret, mfa.TOTPCounter(at)), handle)

	if w.Code != http.StatusFound {
		t.Fatalf("a correct code answered %d, want a redirect back to the application:\n%s",
			w.Code, w.Body.String())
	}
	if !sessionCookieSet(w) {
		t.Error("a completed challenge issued no session cookie")
	}
}

// --- what the session records ---------------------------------------------------------

// DoD item 4: auth_methods and the amr claim behind it reflect what was used.
//
// Read from the DATABASE rather than from the handler's own return, because
// the claim is built from the stored row and a value that never reached the
// column would still satisfy an in-memory assertion.
func TestASecondFactoredLoginIsRecordedAsOne(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	secret, at := s.enrolled(t, at)

	id := s.begin(t)
	token := s.form(t, id)
	handle := handleFrom(t, s.submit(t, id, token, testEmail, testPassword))

	if w := s.code(t, id, mfa.TOTPCode(secret, mfa.TOTPCounter(at)), handle); w.Code != http.StatusFound {
		t.Fatalf("the challenge did not complete: %d\n%s", w.Code, w.Body.String())
	}

	var methods []string
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		// pq.Array for the text[]: database/sql hands an array column back as
		// the driver's own string otherwise, and the scan fails (the same trap
		// P2-13 fell into).
		return tx.QueryRow(context.Background(),
			`SELECT auth_methods FROM sessions
			  WHERE user_id = $1 AND revoked_at IS NULL
			  ORDER BY created_at DESC LIMIT 1`, s.userID).Scan(pq.Array(&methods))
	}); err != nil {
		t.Fatalf("reading the session: %v", err)
	}

	want := map[string]bool{"pwd": true, "otp": true, "mfa": true}
	if len(methods) != len(want) {
		t.Fatalf("auth_methods = %v, want pwd, otp and mfa", methods)
	}
	for _, m := range methods {
		if !want[m] {
			t.Errorf("auth_methods contains %q", m)
		}
	}
}

// --- abuse cases ------------------------------------------------------------------------

// A-1: the six-digit keyspace is bounded across challenges, not just within
// one. An attacker who holds the password restarts the login for free, so a
// per-challenge cap alone caps nothing.
func TestGuessesAreBoundedAcrossChallengesNotJustWithinOne(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	secret, at := s.enrolled(t, at)

	guesses := 0
	for restart := 0; restart < 4; restart++ {
		id := s.begin(t)
		token := s.form(t, id)
		first := s.submit(t, id, token, testEmail, testPassword)

		// Once the bound is spent the password step still succeeds — the
		// password is right — so a challenge is still issued. It is the
		// GUESSING that is refused.
		handle := handleFrom(t, first)

		for i := 0; i < mfa.MaxAttempts; i++ {
			w := s.code(t, id, "000000", handle)
			guesses++
			if strings.Contains(w.Body.String(), MsgCodeRateLimited) {
				if guesses <= mfa.MaxAttemptsPerWindow {
					t.Fatalf("refused after %d guesses; the bound is %d",
						guesses, mfa.MaxAttemptsPerWindow)
				}
				// Bounded, as intended. And the RIGHT code is refused too —
				// otherwise the bound is only a filter on wrong answers.
				right := s.code(t, id, mfa.TOTPCode(secret, mfa.TOTPCounter(at)), handle)
				if sessionCookieSet(right) {
					t.Error("an exhausted user completed the challenge with a correct code")
				}
				return
			}
			if strings.Contains(w.Body.String(), MsgChallengeGone) {
				break // this challenge is spent; the next restart continues
			}
		}
	}
	t.Fatalf("%d guesses were accepted without the per-user bound refusing any", guesses)
}

// A-2: a challenge belongs to the user it was issued for. Somebody else's
// handle must not complete this browser's login.
//
// The handle carries no identity — the user id is server-side — so the attack
// this closes is "steal a handle, answer it with your own code". What the
// handle DOES complete is the login of the user it names, which is why the
// session that results must belong to that user and not to whoever presented
// the cookie.
func TestAChallengeCompletesOnlyTheLoginItWasIssuedFor(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	secret, at := s.enrolled(t, at)

	// Two separate authorization requests. The challenge is bound to the first.
	first := s.begin(t)
	second := s.begin(t)

	token := s.form(t, first)
	handle := handleFrom(t, s.submit(t, first, token, testEmail, testPassword))

	// Answer it while claiming to be finishing the OTHER request.
	w := s.code(t, second, mfa.TOTPCode(secret, mfa.TOTPCounter(at)), handle)

	if w.Code == http.StatusFound {
		// The redirect would carry a code for the second request, minted by a
		// challenge issued for the first.
		t.Error("a challenge issued for one authorization request completed a different one")
	}
}

// A-3: a code cannot be replayed inside its own 30-second window. P3-02's
// counter bound, reached through the login flow rather than through the store.
func TestACodeCannotBeUsedTwiceInItsWindow(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	secret, at := s.enrolled(t, at)
	value := mfa.TOTPCode(secret, mfa.TOTPCounter(at))

	id := s.begin(t)
	token := s.form(t, id)
	handle := handleFrom(t, s.submit(t, id, token, testEmail, testPassword))

	if w := s.code(t, id, value, handle); w.Code != http.StatusFound {
		t.Fatalf("the first use of a valid code failed: %d\n%s", w.Code, w.Body.String())
	}

	// A second login, the same code, the same step.
	again := s.begin(t)
	token2 := s.form(t, again)
	handle2 := handleFrom(t, s.submit(t, again, token2, testEmail, testPassword))

	w := s.code(t, again, value, handle2)
	if sessionCookieSet(w) {
		t.Error("a code was accepted twice inside its own window")
	}
}

// A-5: a wrong answer must not extend the challenge's life. An attacker who
// could refresh the clock by guessing would have removed the time bound by
// using the thing it bounds.
func TestAWrongAnswerDoesNotExtendTheChallenge(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	_, at = s.enrolled(t, at)

	id := s.begin(t)
	token := s.form(t, id)
	handle := handleFrom(t, s.submit(t, id, token, testEmail, testPassword))

	before, err := s.rdb.PTTL(context.Background(), mfa.HandleKey(handle.Value)).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if before <= 0 {
		t.Fatalf("the challenge has no TTL (%v)", before)
	}

	s.code(t, id, "000000", handle)

	after, err := s.rdb.PTTL(context.Background(), mfa.HandleKey(handle.Value)).Result()
	if err != nil {
		t.Fatalf("PTTL after: %v", err)
	}
	if after > before {
		t.Errorf("the TTL grew from %v to %v after a wrong answer", before, after)
	}
}

// A user with NO factor sees no change at all. The most important property of
// this task for everybody who is not enrolled.
func TestAUserWithoutAFactorSignsInExactlyAsBefore(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()

	// A framework is wired, but this user has enrolled nothing.
	sealer, _ := mfa.NewSealer([]byte("an-integration-test-key-long-enough"))
	factorStore := mfa.NewStore()
	s.login.MFA = &mfa.Framework{
		Registry: mfa.NewRegistry(&mfa.TOTP{
			Store: factorStore, DB: s.db, Sealer: sealer, Log: discard(),
			Issuer: "auth.example.test",
			OrgOf:  func(context.Context, string) (string, error) { return s.orgID, nil },
			Now:    func() time.Time { return at },
		}),
		Store:      &mfa.PostgresFactors{Store: factorStore, DB: s.db},
		Challenges: mfa.NewRedisChallenges(s.rdb),
		Attempts:   &mfa.RedisAttempts{Client: s.rdb},
		Log:        discard(),
		Now:        func() time.Time { return at },
	}

	id := s.begin(t)
	token := s.form(t, id)
	w := s.submit(t, id, token, testEmail, testPassword)

	if w.Code != http.StatusFound {
		t.Fatalf("a user with no factor was not signed in: %d\n%s", w.Code, w.Body.String())
	}
	if !sessionCookieSet(w) {
		t.Error("a user with no factor got no session")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == ChallengeCookieName && c.Value != "" {
			t.Error("a user with no factor was issued a challenge handle")
		}
	}
}

// --- audit ------------------------------------------------------------------------------

// DoD: MFA verification is audited separately from password verification, and
// a demanded factor is itself recorded — the line that says a password is
// already lost.
func TestTheChallengeIsAuditedUnderItsOwnEventTypes(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	_, at = s.enrolled(t, at)

	id := s.begin(t)
	token := s.form(t, id)
	handle := handleFrom(t, s.submit(t, id, token, testEmail, testPassword))
	s.code(t, id, "000000", handle)

	if len(s.events(t, "user.mfa.challenged")) == 0 {
		t.Error("a demanded factor was not recorded")
	}

	failures := s.events(t, "user.mfa.failed")
	if len(failures) == 0 {
		t.Fatal("a failed code was not recorded")
	}
	if len(s.events(t, "user.login.failed")) != 0 {
		t.Error("a failed CODE was recorded as a failed LOGIN, conflating two different signals")
	}

	// The payload names the factor type and nothing more. `events` returns the
	// whole row, so the payload is one field of it.
	payload, ok := failures[0]["payload"].(map[string]any)
	if !ok {
		t.Fatalf("the event has no payload object: %+v", failures[0])
	}
	if payload["factor_type"] != string(mfa.TypeTOTP) {
		t.Errorf("factor_type = %v, want totp", payload["factor_type"])
	}
	for key := range payload {
		if key != "factor_type" {
			t.Errorf("the MFA audit payload carries %q, which was not agreed", key)
		}
	}
}
