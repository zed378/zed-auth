//go:build integration

// The lost-device walkthrough, executed (P3-04).
//
// `docs/PLAN/17`'s Phase 3 criterion is that recovery from a lost device has
// been **walked through**, not that a document describing it exists. This is
// that walkthrough as a test, so it is walked again on every run rather than
// once by somebody who then wrote down what they remembered.
//
// It follows `deploy/RUNBOOK-mfa-recovery.md` Path B and Path C in order,
// against real Postgres and real Redis.
package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/zed378/zed-auth/backend/internal/mfa"
	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// withRecoveryCodes issues a batch to the fixture's user and returns the
// plaintexts, wiring the framework's recovery store at the same time.
func (s *stack) withRecoveryCodes(t *testing.T) []string {
	t.Helper()

	store := mfa.NewRecoveryStore()

	var codes []string
	err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		var err error
		codes, _, err = store.Issue(
			context.Background(), tx, s.userID, s.orgID, mfa.RecoveryCodeCount, time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("issuing recovery codes: %v", err)
	}

	framework, ok := s.login.MFA.(*mfa.Framework)
	if !ok {
		t.Fatal("the handler's MFA is not a *mfa.Framework; enrolled() must run first")
	}
	framework.Recovery = &mfa.PostgresRecovery{Store: store, DB: s.db}

	return codes
}

// recoveryCode submits one recovery code to the challenge step.
func (s *stack) recoveryCode(t *testing.T, id, value string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	token := s.form(t, id)

	form := url.Values{
		"request": {id},
		csrfField: {token},
		"factor":  {RecoveryFactor},
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

// --- Path B: the user has a recovery code ---------------------------------------------

// The walkthrough. A user with a confirmed factor loses the device and signs in
// with a recovery code.
func TestALostDeviceIsRecoveredWithACode(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	// Step 1: the password. The device is gone, so no TOTP code is available.
	id := s.begin(t)
	w := s.submit(t, id, s.form(t, id), testEmail, testPassword)

	handle := handleFrom(t, w)
	if sessionCookieSet(w) {
		t.Fatal("a session was issued before the factor step")
	}

	// The page offers the recovery option, because the user holds codes. A
	// dead option here is somebody who cannot sign in reading that they can.
	if !strings.Contains(w.Body.String(), `value="`+RecoveryFactor+`"`) {
		t.Fatalf("the challenge page does not offer a recovery code:\n%s", w.Body.String())
	}

	// Step 2: a recovery code, typed the way somebody reads one off paper —
	// lower case, with the dashes it was printed with.
	typed := strings.ToLower(codes[0])
	got := s.recoveryCode(t, id, typed, handle)

	if !sessionCookieSet(got) {
		t.Fatalf("a correct recovery code did not sign the user in (status %d):\n%s",
			got.Code, got.Body.String())
	}
}

// The code is spent. Path B says "each code works once", and this is what makes
// that sentence true rather than aspirational.
func TestARecoveryCodeCannotSignInTwice(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	first := s.begin(t)
	handle := handleFrom(t, s.submit(t, first, s.form(t, first), testEmail, testPassword))
	if !sessionCookieSet(s.recoveryCode(t, first, codes[0], handle)) {
		t.Fatal("the first use of a recovery code failed")
	}

	// A second login, the same code.
	second := s.begin(t)
	handle2 := handleFrom(t, s.submit(t, second, s.form(t, second), testEmail, testPassword))
	w := s.recoveryCode(t, second, codes[0], handle2)

	if sessionCookieSet(w) {
		t.Fatal("a spent recovery code signed the user in a second time")
	}

	// The positive control: a DIFFERENT code still works, so the refusal above
	// is the code being spent rather than recovery being broken.
	third := s.begin(t)
	handle3 := handleFrom(t, s.submit(t, third, s.form(t, third), testEmail, testPassword))
	if !sessionCookieSet(s.recoveryCode(t, third, codes[1], handle3)) {
		t.Fatal("an unspent code does not work either; the test above proves nothing")
	}
}

// A recovery login's session says what actually happened.
//
// `amr` claims `mfa` — two factors were used — and NOT `otp`, because no
// authenticator device was presented. A consumer demanding `otp` for a
// sensitive action will correctly refuse this session and force a re-enrolment,
// which is the right outcome for somebody who no longer has the device.
func TestARecoverySessionClaimsMultiFactorButNotOTP(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	id := s.begin(t)
	handle := handleFrom(t, s.submit(t, id, s.form(t, id), testEmail, testPassword))
	if !sessionCookieSet(s.recoveryCode(t, id, codes[0], handle)) {
		t.Fatal("the recovery login failed")
	}

	var methods []string
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT auth_methods FROM sessions
			  WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`,
			s.userID).Scan(pq.Array(&methods))
	}); err != nil {
		t.Fatalf("reading the session: %v", err)
	}

	var hasMFA, hasOTP bool
	for _, m := range methods {
		switch m {
		case mfa.MethodMultiFactor:
			hasMFA = true
		case mfa.TypeTOTP.AMR():
			hasOTP = true
		}
	}
	if !hasMFA {
		t.Errorf("auth_methods = %v, missing %q", methods, mfa.MethodMultiFactor)
	}
	if hasOTP {
		t.Errorf("auth_methods = %v claims %q, but no device was presented", methods, mfa.TypeTOTP.AMR())
	}
}

// The recovery use is audited, at elevated visibility.
func TestARecoveryLoginIsAudited(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	id := s.begin(t)
	handle := handleFrom(t, s.submit(t, id, s.form(t, id), testEmail, testPassword))
	if !sessionCookieSet(s.recoveryCode(t, id, codes[0], handle)) {
		t.Fatal("the recovery login failed")
	}

	var count int
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM events
			  WHERE actor_user_id = $1 AND event_type = 'user.mfa.recovery_used'`,
			s.userID).Scan(&count)
	}); err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	if count != 1 {
		t.Errorf("found %d user.mfa.recovery_used events, want 1 — an incident review would not see this", count)
	}
}

// No audit row ever carries the code.
func TestNoAuditRowCarriesARecoveryCode(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	id := s.begin(t)
	handle := handleFrom(t, s.submit(t, id, s.form(t, id), testEmail, testPassword))
	if !sessionCookieSet(s.recoveryCode(t, id, codes[0], handle)) {
		t.Fatal("the recovery login failed")
	}

	plain := mfa.NormaliseRecoveryCode(codes[0])

	var hits int
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM events WHERE payload::text ILIKE '%' || $1 || '%'`,
			plain).Scan(&hits)
	}); err != nil {
		t.Fatalf("searching the audit log: %v", err)
	}
	if hits != 0 {
		t.Errorf("%d audit rows contain the recovery code in plaintext", hits)
	}
}

// --- what the page does not say ----------------------------------------------------------

// The challenge page does not disclose HOW MANY codes the user has.
//
// The count is a fact about the account, and this page is reachable with only a
// password. "You have 2 recovery codes left" would answer a question about the
// account to somebody who may not own it.
func TestTheChallengePageDoesNotDiscloseTheCodeCount(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	// Spend most of them, so a leak would be a distinctive number.
	store := mfa.NewRecoveryStore()
	for _, code := range codes[:8] {
		if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
			_, err := store.Consume(context.Background(), tx, s.userID, code, time.Now())
			return err
		}); err != nil {
			t.Fatalf("spending: %v", err)
		}
	}

	id := s.begin(t)
	body := s.submit(t, id, s.form(t, id), testEmail, testPassword).Body.String()

	for _, leak := range []string{"2 recovery", "2 codes", "you have 2", "remaining: 2"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("the page discloses the remaining count (%q):\n%s", leak, body)
		}
	}
}

// A user with no recovery codes is not offered the option.
func TestNoRecoveryOptionWithoutCodes(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	// Deliberately no withRecoveryCodes.

	id := s.begin(t)
	body := s.submit(t, id, s.form(t, id), testEmail, testPassword).Body.String()

	if strings.Contains(body, `value="`+RecoveryFactor+`"`) {
		t.Errorf("recovery is offered to a user holding no codes:\n%s", body)
	}
}

// --- Path C: the administrator-assisted reset --------------------------------------------

// After a reset the user signs in with a password alone, and the codes are gone.
//
// The runbook's Path C, end to end. What it asserts is the property that makes
// the path safe to document: the reset DESTROYS and mints nothing, so an
// administrator ends up with no credential of their own.
func TestAnAdministratorResetReturnsTheUserToPasswordOnly(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	// Before: the password alone does not sign them in.
	before := s.begin(t)
	if sessionCookieSet(s.submit(t, before, s.form(t, before), testEmail, testPassword)) {
		t.Fatal("the password alone signed the user in before the reset")
	}

	// The reset, as the endpoint performs it.
	reset := &mfa.AdminReset{Factors: mfa.NewStore(), Recovery: mfa.NewRecoveryStore()}
	var factors, removed int
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		var err error
		if factors, err = reset.ClearFactors(context.Background(), tx, s.userID); err != nil {
			return err
		}
		removed, err = reset.ClearRecoveryCodes(context.Background(), tx, s.userID)
		return err
	}); err != nil {
		t.Fatalf("the reset failed: %v", err)
	}

	if factors != 1 {
		t.Errorf("the reset removed %d factors, want 1", factors)
	}
	if removed != mfa.RecoveryCodeCount {
		t.Errorf("the reset removed %d codes, want %d", removed, mfa.RecoveryCodeCount)
	}

	// After: the password alone is enough, because there is nothing left to
	// challenge with.
	after := s.begin(t)
	if !sessionCookieSet(s.submit(t, after, s.form(t, after), testEmail, testPassword)) {
		t.Fatal("the user cannot sign in after the reset; the account is still locked")
	}

	// And the old recovery codes are dead, so an administrator who noted one
	// down before resetting holds nothing.
	var left int
	if err := s.db.WithTenant(context.Background(), s.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, s.userID).Scan(&left)
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if left != 0 {
		t.Errorf("%d recovery codes survived the reset", left)
	}
	_ = codes
}

// Abuse case (P3-04 card): brute-forcing recovery codes, through the real login
// step and the real per-user bound in Redis (P3-14).
//
// The unit tests prove the recovery path counts a failure against a counting
// fake. This proves the count lands in the same bound TOTP guesses do, so an
// attacker holding the password cannot switch to the recovery form to get a
// fresh allowance — and that once the bound is spent, a CORRECT code is refused
// too, or the bound would only filter wrong answers.
func TestRecoveryCodeGuessesShareThePerUserBound(t *testing.T) {
	s := setup(t)
	_, _ = s.enrolled(t, time.Now())
	codes := s.withRecoveryCodes(t)

	// Well-formed, so each guess reaches verification rather than being
	// refused for its shape. Base32 letters only; never a generated code.
	const guess = "ABCD-EFGH-JKLM-NPQR"
	for _, real := range codes {
		if mfa.NormaliseRecoveryCode(real) == mfa.NormaliseRecoveryCode(guess) {
			t.Skip("astronomically unlikely: the guess is a real code")
		}
	}

	guesses := 0
	for restart := 0; restart < 4; restart++ {
		id := s.begin(t)
		handle := handleFrom(t, s.submit(t, id, s.form(t, id), testEmail, testPassword))

		for i := 0; i < mfa.MaxAttempts; i++ {
			w := s.recoveryCode(t, id, guess, handle)
			guesses++
			if sessionCookieSet(w) {
				t.Fatal("a wrong recovery code signed the user in")
			}
			if strings.Contains(w.Body.String(), MsgCodeRateLimited) {
				if guesses <= mfa.MaxAttemptsPerWindow {
					t.Fatalf("refused after %d guesses; the bound is %d", guesses, mfa.MaxAttemptsPerWindow)
				}
				right := s.recoveryCode(t, id, codes[0], handle)
				if sessionCookieSet(right) {
					t.Error("an exhausted user completed the challenge with a correct recovery code")
				}
				return
			}
			if strings.Contains(w.Body.String(), MsgChallengeGone) {
				break
			}
		}
	}
	t.Fatalf("%d recovery code guesses were accepted without the per-user bound refusing any", guesses)
}

// --- timing (P3-14 step 5) --------------------------------------------------------------

// A factor answer that WAS valid must cost what one that never was costs.
//
// The two pairs that matter are within a factor type, because that is what an
// attacker holding the password can learn from: whether a TOTP code they
// captured is being refused as a replay (so it was right) or as wrong, and
// whether a recovery code they found is spent (so it was real) or invented.
// The content is already identical (P3-03, P3-04); this is the clock.
//
// Medians of several samples and a generous ratio, as the unknown-address test
// does: it exists to catch an early return that skips the expensive half, not
// a few percent of noise. The per-user attempt bound is cleared between
// samples, or the test would measure the cooldown page instead.
func TestAnAnswerThatWasValidCostsWhatAWrongOneCosts(t *testing.T) {
	s := setup(t)
	at := time.Now().UTC()
	secret, at := s.enrolled(t, at)
	codes := s.withRecoveryCodes(t)

	clearAttempts := func() {
		ctx := context.Background()
		keys, err := s.rdb.Keys(ctx, "mfa:attempts:"+s.userID+":*").Result()
		if err != nil {
			t.Fatalf("listing attempt keys: %v", err)
		}
		if len(keys) > 0 {
			s.rdb.Del(ctx, keys...)
		}
	}

	signInWith := func(answer func(id string, handle *http.Cookie) *httptest.ResponseRecorder) (*httptest.ResponseRecorder, time.Duration) {
		clearAttempts()
		id := s.begin(t)
		handle := handleFrom(t, s.submit(t, id, s.form(t, id), testEmail, testPassword))
		start := time.Now()
		w := answer(id, handle)
		return w, time.Since(start)
	}

	// Spend one of each, so there is a replayed TOTP code and a spent recovery code.
	used := mfa.TOTPCode(secret, mfa.TOTPCounter(at))
	if w, _ := signInWith(func(id string, h *http.Cookie) *httptest.ResponseRecorder { return s.code(t, id, used, h) }); !sessionCookieSet(w) {
		t.Fatal("setup: the TOTP code did not sign in")
	}
	if w, _ := signInWith(func(id string, h *http.Cookie) *httptest.ResponseRecorder { return s.recoveryCode(t, id, codes[0], h) }); !sessionCookieSet(w) {
		t.Fatal("setup: the recovery code did not sign in")
	}

	wrong := "000000"
	if wrong == used {
		wrong = "111111"
	}

	median := func(name string, answer func(id string, h *http.Cookie) *httptest.ResponseRecorder) time.Duration {
		const samples = 7
		durations := make([]time.Duration, 0, samples)
		for range samples {
			w, took := signInWith(answer)
			if sessionCookieSet(w) {
				t.Fatalf("%s signed the user in; the comparison is meaningless", name)
			}
			durations = append(durations, took)
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		return durations[len(durations)/2]
	}

	compare := func(pair string, never, was time.Duration) {
		ratio := float64(was) / float64(never)
		if ratio < 0.6 || ratio > 1.6 {
			t.Errorf("%s: a formerly valid answer takes %s and an invalid one %s (ratio %.2f) — "+
				"the response time says which was real", pair, was, never, ratio)
		}
	}

	compare("TOTP",
		median("a wrong code", func(id string, h *http.Cookie) *httptest.ResponseRecorder { return s.code(t, id, wrong, h) }),
		median("a replayed code", func(id string, h *http.Cookie) *httptest.ResponseRecorder { return s.code(t, id, used, h) }))
	compare("recovery code",
		median("an invented code", func(id string, h *http.Cookie) *httptest.ResponseRecorder {
			return s.recoveryCode(t, id, "ABCD-EFGH-JKLM-NPQR", h)
		}),
		median("a spent code", func(id string, h *http.Cookie) *httptest.ResponseRecorder { return s.recoveryCode(t, id, codes[0], h) }))
}
