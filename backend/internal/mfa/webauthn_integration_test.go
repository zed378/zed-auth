//go:build integration

// The WebAuthn ceremonies against real Postgres and a real signature (P3-05).
//
// The abuse cases the card names — phishing via a lookalike origin, replay
// across origins, registering to another user's account — are all claims about
// what happens when a genuinely-signed assertion arrives with one thing wrong.
// A fake verifier cannot test any of them, because the fake would be the thing
// deciding. So every assertion below is signed by the software authenticator
// and verified by the library.
package mfa

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
	"github.com/zed378/zed-auth/backend/internal/testsupport"
)

const (
	testRPID   = "auth.example.test"
	testOrigin = "https://auth.example.test"

	// lookalikeOrigin is the phishing page. Note how ordinary it looks — that
	// is the point, and it is why a human cannot be the control here.
	lookalikeOrigin = "https://auth.example.test.evil.test"
)

type passkeyFixture struct {
	db       *postgres.DB
	factory  *testsupport.Factory
	verifier *WebAuthnVerifier

	orgID  string
	userID string
}

func passkeySetup(t *testing.T) *passkeyFixture {
	t.Helper()

	f := recoverySetup(t)

	rp, err := NewWebAuthn(testOrigin, "Auth Service")
	if err != nil {
		t.Fatalf("NewWebAuthn: %v", err)
	}

	return &passkeyFixture{
		db:      f.db,
		factory: f.factory,
		orgID:   f.orgID,
		userID:  f.userID,
		verifier: &WebAuthnVerifier{
			Store: NewWebAuthnStore(),
			DB:    f.db,
			RP:    rp,
			Now:   func() time.Time { return testNow() },
		},
	}
}

// enrol runs a full registration ceremony and returns the stored factor id.
func (f *passkeyFixture) enrol(t *testing.T, a *softAuthenticator, label string) string {
	t.Helper()

	options, session, err := f.verifier.BeginRegistration(
		context.Background(), f.userID, f.orgID, label, "alice@example.test")
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}

	response := a.register(t, testRPID, testOrigin, challengeFrom(t, options))

	factorID, err := f.verifier.FinishRegistration(
		context.Background(), f.userID, f.orgID, label, "alice@example.test", *session, response)
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	return factorID
}

// beginLogin starts an authentication ceremony.
func (f *passkeyFixture) beginLogin(t *testing.T) (options []byte, session webauthn.SessionData) {
	t.Helper()

	raw, s, err := f.verifier.BeginLogin(context.Background(), f.userID, f.orgID)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if raw == nil {
		t.Fatal("BeginLogin offered no ceremony for a user holding a credential")
	}
	return raw, *s
}

// --- the happy path ----------------------------------------------------------------

func TestAPasskeyRegistersAndAuthenticates(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)

	factorID := f.enrol(t, a, "yubikey")
	if factorID == "" {
		t.Fatal("registration returned no factor id")
	}

	options, session := f.beginLogin(t)
	a.signCount++
	assertion := a.assert(t, testRPID, testOrigin, challengeFrom(t, options))

	got, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session, assertion)
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if got != factorID {
		t.Errorf("authenticated as factor %q, want %q", got, factorID)
	}
}

// There is no secret stored for this factor type, which is the genuine
// advantage over TOTP worth asserting rather than only claiming.
func TestAPasskeyStoresNoSecret(t *testing.T) {
	f := passkeySetup(t)
	f.enrol(t, newSoftAuthenticator(t), "yubikey")

	var secret []byte
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT secret_encrypted FROM user_mfa_factors
			  WHERE user_id = $1 AND type = 'webauthn'`, f.userID).Scan(&secret)
	}); err != nil {
		t.Fatalf("reading the factor: %v", err)
	}
	if len(secret) != 0 {
		t.Errorf("a WebAuthn factor stored %d bytes of secret; there is nothing to store", len(secret))
	}
}

// --- the phishing defence, which is the whole point --------------------------------

// Abuse case A-1. A genuinely-signed assertion from a lookalike origin fails.
//
// Everything about this assertion is real: the authenticator holds the right
// key, signs the right challenge, and produces a signature that verifies
// cryptographically. The ONLY thing wrong is the origin the browser reported —
// which is exactly what a relayed phishing attempt looks like.
func TestAnAssertionFromALookalikeOriginIsRefused(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	options, session := f.beginLogin(t)
	a.signCount++
	relayed := a.assert(t, testRPID, lookalikeOrigin, challengeFrom(t, options))

	_, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session, relayed)
	if err == nil {
		t.Fatal("an assertion signed for a lookalike origin was accepted — the phishing resistance is gone")
	}
	if !errors.Is(err, ErrOriginMismatch) {
		t.Errorf("FinishLogin gave %v, want ErrOriginMismatch so an operator can tell this from a typo", err)
	}

	// The positive control: the SAME authenticator and the SAME challenge, with
	// the right origin, is accepted. Without this the test above would pass
	// against a verifier that refused everything.
	honest := a.assert(t, testRPID, testOrigin, challengeFrom(t, options))
	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session, honest); err != nil {
		t.Errorf("the honest assertion was refused too, so the test above proves nothing: %v", err)
	}
}

// The RP ID is signed over as well, so a lookalike that somehow got the origin
// past a proxy still fails on what the authenticator hashed.
func TestAnAssertionForAnotherRelyingPartyIsRefused(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	options, session := f.beginLogin(t)
	a.signCount++
	wrongRP := a.assert(t, "evil.test", testOrigin, challengeFrom(t, options))

	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session, wrongRP); err == nil {
		t.Fatal("an assertion signed for another relying party was accepted")
	}
}

// Abuse case A-3. An assertion is bound to the challenge that asked for it.
func TestAnAssertionForAnotherChallengeIsRefused(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	// Two ceremonies. The assertion answers the first; it is offered to the
	// second.
	firstOptions, _ := f.beginLogin(t)
	_, secondSession := f.beginLogin(t)

	a.signCount++
	forFirst := a.assert(t, testRPID, testOrigin, challengeFrom(t, firstOptions))

	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, secondSession, forFirst); err == nil {
		t.Fatal("an assertion for one challenge completed another")
	}
}

// A challenge the service never issued cannot be answered, even with a real
// signature over it.
func TestAnAssertionOverAnInventedChallengeIsRefused(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	_, session := f.beginLogin(t)
	a.signCount++
	invented := a.assert(t, testRPID, testOrigin, "aW52ZW50ZWQtY2hhbGxlbmdl")

	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session, invented); err == nil {
		t.Fatal("an assertion over a challenge this service never issued was accepted")
	}
}

// --- the signature counter -----------------------------------------------------------

// Abuse case A-5. A counter that goes backwards means two authenticators hold
// one key.
func TestARegressedSignatureCounterIsRefused(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	// A legitimate login, moving the counter to 5.
	a.signCount = 5
	options, session := f.beginLogin(t)
	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
		a.assert(t, testRPID, testOrigin, challengeFrom(t, options))); err != nil {
		t.Fatalf("the first login failed: %v", err)
	}

	// A clone: the same key, a counter that has not caught up.
	a.signCount = 3
	options, session = f.beginLogin(t)
	_, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
		a.assert(t, testRPID, testOrigin, challengeFrom(t, options)))

	if !errors.Is(err, ErrClonedAuthenticator) {
		t.Errorf("FinishLogin gave %v, want ErrClonedAuthenticator", err)
	}
}

// An authenticator that always reports zero is accepted. Most platform
// authenticators — phones, Touch ID — do not count, and refusing them would
// exclude most users.
func TestAnAuthenticatorThatDoesNotCountIsAccepted(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	a.signCount = 0
	f.enrol(t, a, "phone")

	for i := 0; i < 3; i++ {
		options, session := f.beginLogin(t)
		if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
			a.assert(t, testRPID, testOrigin, challengeFrom(t, options))); err != nil {
			t.Fatalf("login %d with a non-counting authenticator failed: %v", i, err)
		}
	}
}

// The stored counter moves with the authenticator, so the check has something
// to compare against next time.
func TestTheStoredCounterFollowsTheAuthenticator(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	factorID := f.enrol(t, a, "yubikey")

	a.signCount = 42
	options, session := f.beginLogin(t)
	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
		a.assert(t, testRPID, testOrigin, challengeFrom(t, options))); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}

	var stored int64
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT sign_count FROM user_mfa_factors WHERE id = $1`, factorID).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading the counter: %v", err)
	}
	if stored != 42 {
		t.Errorf("stored counter = %d, want 42 — a clone would go undetected", stored)
	}
}

// --- several credentials --------------------------------------------------------------

// Card step 4 / F-3. Losing one device is not an account loss.
func TestSeveralCredentialsPerUserAreIndividuallyRemovable(t *testing.T) {
	f := passkeySetup(t)

	laptop := newSoftAuthenticator(t)
	phone := newSoftAuthenticator(t)

	laptopID := f.enrol(t, laptop, "laptop")
	f.enrol(t, phone, "phone")

	// Either one authenticates.
	for name, a := range map[string]*softAuthenticator{"laptop": laptop, "phone": phone} {
		a.signCount += 10
		options, session := f.beginLogin(t)
		if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
			a.assert(t, testRPID, testOrigin, challengeFrom(t, options))); err != nil {
			t.Fatalf("the %s could not authenticate: %v", name, err)
		}
	}

	// Remove the laptop only.
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		return NewWebAuthnStore().Delete(context.Background(), tx, laptopID)
	}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The phone still works.
	phone.signCount += 10
	options, session := f.beginLogin(t)
	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
		phone.assert(t, testRPID, testOrigin, challengeFrom(t, options))); err != nil {
		t.Errorf("removing one credential broke the other: %v", err)
	}

	// The laptop does not.
	laptop.signCount += 10
	options, session = f.beginLogin(t)
	if _, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session,
		laptop.assert(t, testRPID, testOrigin, challengeFrom(t, options))); err == nil {
		t.Error("a removed credential still authenticates")
	}
}

// --- whose credential it is -------------------------------------------------------------

// Abuse case A-4. A credential registered to one account does not authenticate
// another, even with a perfect signature.
func TestACredentialDoesNotAuthenticateAnotherUser(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	stranger := f.factory.User(f.orgID, "bob@example.test")

	// A ceremony for the stranger cannot even begin — they hold nothing.
	options, session, err := f.verifier.BeginLogin(context.Background(), stranger, f.orgID)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if options != nil {
		t.Fatal("a user with no credential was offered a ceremony")
	}
	_ = session

	// And an assertion from the real owner, offered against the stranger's
	// identity with a ceremony that IS valid, resolves to nothing.
	ownerOptions, ownerSession := f.beginLogin(t)
	a.signCount++
	assertion := a.assert(t, testRPID, testOrigin, challengeFrom(t, ownerOptions))

	if _, err := f.verifier.FinishLogin(
		context.Background(), stranger, f.orgID, ownerSession, assertion); err == nil {
		t.Error("one user's credential authenticated another")
	}
}

// The database refuses one credential id on two accounts, so the guarantee does
// not rest only on the ceremony staying correct.
func TestOneCredentialCannotBeFiledUnderTwoAccounts(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	stranger := f.factory.User(f.orgID, "bob@example.test")

	err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		_, err := NewWebAuthnStore().InsertCredential(context.Background(), tx, NewCredential{
			UserID:       stranger,
			OrgID:        f.orgID,
			CredentialID: a.credentialID,
			PublicKey:    []byte("whatever"),
		}, testNow())
		return err
	})
	if err == nil {
		t.Error("the same credential id was filed under two accounts")
	}
}

// --- what the framework's seam does ----------------------------------------------------

// Options returns nothing for a user with no credential, so a caller can tell
// "no passkey" from "a passkey and something went wrong".
func TestOptionsAreEmptyForAUserWithNoCredential(t *testing.T) {
	f := passkeySetup(t)

	options, session, err := f.verifier.Options(context.Background(), f.orgID, f.userID)
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if options != nil || session != nil {
		t.Error("a ceremony was offered to a user holding no credential")
	}
}

// The session round-trips through JSON, which is how the framework stores it in
// the challenge.
func TestTheCeremonySessionSurvivesTheChallengeEncoding(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)
	f.enrol(t, a, "yubikey")

	options, session, err := f.verifier.Options(context.Background(), f.orgID, f.userID)
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	// Through the challenge's own encoding and back, which is what Redis holds.
	challenge := Challenge{
		UserID: f.userID, OrgID: f.orgID, PendingID: "pending-1",
		WebAuthnOptions: options, WebAuthnSession: session,
	}
	encoded, err := challenge.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	restored, err := DecodeChallenge(encoded)
	if err != nil {
		t.Fatalf("DecodeChallenge: %v", err)
	}

	a.signCount++
	assertion := a.assert(t, testRPID, testOrigin, challengeFrom(t, restored.WebAuthnOptions))

	if _, err := f.verifier.Verify(
		context.Background(), f.orgID, f.userID, restored.WebAuthnSession, assertion); err != nil {
		t.Errorf("the ceremony did not survive the challenge encoding: %v", err)
	}
}

// A session this service did not write is an error, never a wrong answer — it
// is a bug or a corrupted store, and reporting it as a failed login would send
// somebody to their recovery codes for an operator's problem.
func TestACorruptSessionIsNotAWrongAnswer(t *testing.T) {
	f := passkeySetup(t)
	f.enrol(t, newSoftAuthenticator(t), "yubikey")

	_, err := f.verifier.Verify(
		context.Background(), f.orgID, f.userID, []byte("not json"), []byte("{}"))

	if err == nil {
		t.Fatal("a corrupt session was accepted")
	}
	if errors.Is(err, ErrWrongCode) {
		t.Error("a corrupt session was reported as a wrong answer")
	}
}

// The user verification flag is recorded, so a true second factor is
// distinguishable from mere presence (card step 6).
func TestUserVerificationIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verified bool
	}{
		{"verified", true},
		{"presence only", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := passkeySetup(t)
			a := newSoftAuthenticator(t)
			a.userVerified = tc.verified
			f.enrol(t, a, "key")

			var stored []StoredCredential
			if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
				var err error
				stored, err = NewWebAuthnStore().Credentials(context.Background(), tx, f.userID)
				return err
			}); err != nil {
				t.Fatalf("Credentials: %v", err)
			}
			if len(stored) != 1 {
				t.Fatalf("found %d credentials, want 1", len(stored))
			}
			if stored[0].UserVerified != tc.verified {
				t.Errorf("UserVerified = %v, want %v", stored[0].UserVerified, tc.verified)
			}
		})
	}
}

// An oversized assertion is refused rather than handed to a CBOR parser.
func TestAnOversizedAssertionIsRefused(t *testing.T) {
	f := passkeySetup(t)
	f.enrol(t, newSoftAuthenticator(t), "yubikey")

	_, session := f.beginLogin(t)

	huge := make([]byte, maxAssertionBytes*2)
	for i := range huge {
		huge[i] = 'A'
	}

	if _, err := f.verifier.FinishLogin(
		context.Background(), f.userID, f.orgID, session, huge); err == nil {
		t.Error("an oversized assertion was accepted")
	}
}

// The RP ID is derived from the deployment's own issuer, never from a request.
func TestTheRelyingPartyComesFromConfiguration(t *testing.T) {
	for _, tc := range []struct{ issuer, wantRPID string }{
		{"https://auth.example.test", "auth.example.test"},
		{"https://auth.example.test/", "auth.example.test"},
		{"https://auth.example.test:8443", "auth.example.test"},
		{"https://auth.example.test/oauth", "auth.example.test"},
	} {
		rp, err := NewWebAuthn(tc.issuer, "Auth")
		if err != nil {
			t.Fatalf("NewWebAuthn(%q): %v", tc.issuer, err)
		}
		if rp.Config.RPID != tc.wantRPID {
			t.Errorf("NewWebAuthn(%q) gave RPID %q, want %q", tc.issuer, rp.Config.RPID, tc.wantRPID)
		}
	}

	if _, err := NewWebAuthn("", "Auth"); err == nil {
		t.Error("a relying party was built with no origin at all")
	}
}

// A registration that was stored but never activated cannot authenticate.
//
// Today `FinishRegistration` activates in the same transaction that inserts, so
// no pending WebAuthn row exists on any path — which is exactly why this needs
// a test rather than being assumed. A mutation run found the status filter
// unreachable, meaning nothing would have noticed if it were removed and
// `P3-12`'s self-service registration later left a row half-finished.
//
// The row is inserted directly, because no ceremony produces one.
func TestAPendingCredentialCannotAuthenticate(t *testing.T) {
	f := passkeySetup(t)
	a := newSoftAuthenticator(t)

	// A credential stored the way InsertCredential stores one — `pending` —
	// and deliberately not activated.
	if err := f.db.WithTenant(context.Background(), f.orgID, func(tx *postgres.Tx) error {
		_, err := NewWebAuthnStore().InsertCredential(context.Background(), tx, NewCredential{
			UserID:       f.userID,
			OrgID:        f.orgID,
			Label:        "half-finished",
			CredentialID: a.credentialID,
			PublicKey:    []byte("a key nobody proved"),
		}, testNow())
		return err
	}); err != nil {
		t.Fatalf("inserting a pending credential: %v", err)
	}

	// It is not offered: a ceremony for this user finds nothing answerable.
	options, _, err := f.verifier.BeginLogin(context.Background(), f.userID, f.orgID)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	if options != nil {
		t.Error("a pending credential was offered as a way to sign in")
	}

	// And it cannot answer one either. A ceremony is built by hand, since
	// BeginLogin correctly refuses to make one.
	second := newSoftAuthenticator(t)
	activeID := f.enrol(t, second, "real")
	liveOptions, session := f.beginLogin(t)

	a.signCount++
	pendingAssertion := a.assert(t, testRPID, testOrigin, challengeFrom(t, liveOptions))

	if _, err := f.verifier.FinishLogin(
		context.Background(), f.userID, f.orgID, session, pendingAssertion); err == nil {
		t.Error("a pending credential authenticated")
	}

	// The positive control: the ACTIVE credential answers the same ceremony, so
	// the refusal above is the status filter rather than a broken ceremony.
	second.signCount++
	honest := second.assert(t, testRPID, testOrigin, challengeFrom(t, liveOptions))
	got, err := f.verifier.FinishLogin(context.Background(), f.userID, f.orgID, session, honest)
	if err != nil {
		t.Fatalf("the active credential was refused too, so the test above proves nothing: %v", err)
	}
	if got != activeID {
		t.Errorf("authenticated as %q, want the active credential %q", got, activeID)
	}
}
