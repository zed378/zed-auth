package mfa

import (
	"context"
	"errors"
	"testing"
)

// `AnswerWebAuthn` (P3-05).
//
// The ceremony itself is tested against a real signature in
// `webauthn_integration_test.go`. What is here is the framework's half: the
// bindings that make a passkey answer as safe as a TOTP one, tested with a fake
// ceremony so each branch can be reached without a database.
//
// The division matters. A fake here could not prove anything about phishing —
// it would be the thing deciding — but it is exactly right for proving that the
// challenge decides WHO, that an answer cannot complete a different login, and
// that a passkey is recorded as a passkey.

// fakeCeremony is a WebAuthn ceremony in a map.
type fakeCeremony struct {
	// options and session are what Options hands back.
	options []byte
	session []byte

	// factorID is what a correct assertion resolves to; err is what a wrong
	// one produces.
	factorID string
	err      error

	// optionsErr fails the ceremony at the point a challenge is raised.
	optionsErr error

	// seenOrg and seenUser record the scope, so a test can assert the
	// framework passed the challenge's own identity in the right ORDER —
	// a transposition the compiler cannot see, since both are strings.
	seenOrg     string
	seenUser    string
	seenSession []byte
	verifies    int
}

func (c *fakeCeremony) Options(_ context.Context, orgID, userID string) ([]byte, []byte, error) {
	if c.optionsErr != nil {
		return nil, nil, c.optionsErr
	}
	c.seenOrg, c.seenUser = orgID, userID
	return c.options, c.session, nil
}

func (c *fakeCeremony) Verify(
	_ context.Context, orgID, userID string, session, assertion []byte,
) (string, error) {
	c.verifies++
	c.seenOrg, c.seenUser = orgID, userID
	c.seenSession = session
	if c.err != nil {
		return "", c.err
	}
	return c.factorID, nil
}

// passkeyFramework builds one with a fake ceremony attached.
func passkeyFramework(t *testing.T) (*Framework, *memoryChallenges, *fakeCeremony) {
	t.Helper()

	challenges := newMemoryChallenges()
	ceremony := &fakeCeremony{
		options:  []byte(`{"publicKey":{"challenge":"abc"}}`),
		session:  []byte(`{"challenge":"abc"}`),
		factorID: "f1",
	}

	return &Framework{
		Registry: NewRegistry(&fakeVerifier{kind: TypeWebAuthn, correct: "unused"}),
		Store: &memoryStore{factors: []Factor{
			{ID: "f1", UserID: "u1", OrgID: "o1", Type: TypeWebAuthn, Status: StatusActive},
		}},
		Challenges: challenges,
		WebAuthn:   ceremony,
	}, challenges, ceremony
}

// issuedPasskey raises a challenge through Required, so the ceremony state is
// stored the way production stores it.
func issuedPasskey(t *testing.T, f *Framework, pendingID string) Decision {
	t.Helper()

	decision, err := f.Required(context.Background(), "u1", "o1", pendingID)
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if !decision.Challenge {
		t.Fatal("no challenge was raised for a user holding a passkey")
	}
	return decision
}

// --- the happy path -------------------------------------------------------------

func TestAPasskeyCompletesAChallenge(t *testing.T) {
	f, _, ceremony := passkeyFramework(t)
	decision := issuedPasskey(t, f, "pending-1")

	out, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("assertion"))
	if err != nil {
		t.Fatalf("AnswerWebAuthn: %v", err)
	}
	if !out.Complete {
		t.Fatal("a valid assertion did not complete the challenge")
	}
	if out.UserID != "u1" || out.OrgID != "o1" {
		t.Errorf("the outcome carries %+v, not the challenge's identity", out)
	}

	// The scope, in the right order.
	if ceremony.seenOrg != "o1" || ceremony.seenUser != "u1" {
		t.Errorf("Verify was scoped to org=%q user=%q, want org=o1 user=u1",
			ceremony.seenOrg, ceremony.seenUser)
	}
	// And the session it was checked against is the one the challenge stored,
	// not one built from the request.
	if string(ceremony.seenSession) != string(ceremony.session) {
		t.Errorf("Verify was given session %q, want the one the challenge stored", ceremony.seenSession)
	}
}

// A passkey login records `webauthn`, so `amr` says `hwk` and not `otp`.
//
// The two are not interchangeable: a consumer demanding `hwk` is asking for a
// hardware-backed credential and must not be satisfied by an authenticator app.
func TestAPasskeyLoginRecordsItsOwnFactorType(t *testing.T) {
	f, _, _ := passkeyFramework(t)
	decision := issuedPasskey(t, f, "pending-1")

	out, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("assertion"))
	if err != nil {
		t.Fatalf("AnswerWebAuthn: %v", err)
	}

	if len(out.Methods) != 1 || out.Methods[0] != TypeWebAuthn {
		t.Fatalf("Methods = %v, want [webauthn]", out.Methods)
	}

	amr := AuthMethods(true, out.Methods...)
	var hasHWK, hasOTP bool
	for _, m := range amr {
		switch m {
		case TypeWebAuthn.AMR():
			hasHWK = true
		case TypeTOTP.AMR():
			hasOTP = true
		}
	}
	if !hasHWK {
		t.Errorf("amr = %v, missing %q", amr, TypeWebAuthn.AMR())
	}
	if hasOTP {
		t.Errorf("amr = %v claims %q for a passkey login", amr, TypeTOTP.AMR())
	}
}

// The challenge is consumed, so an assertion cannot complete two logins.
func TestACompletedPasskeyChallengeCannotBeReused(t *testing.T) {
	f, _, _ := passkeyFramework(t)
	decision := issuedPasskey(t, f, "pending-1")

	if _, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("a")); err != nil {
		t.Fatalf("AnswerWebAuthn: %v", err)
	}

	_, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("a"))
	if !errors.Is(err, ErrNoChallenge) {
		t.Errorf("a completed challenge answered again gave %v, want ErrNoChallenge", err)
	}
}

// --- the bindings ------------------------------------------------------------------

// An assertion completes the request its challenge was issued for, and no other.
func TestAPasskeyAnswerDoesNotCompleteADifferentRequest(t *testing.T) {
	f, _, ceremony := passkeyFramework(t)
	decision := issuedPasskey(t, f, "pending-1")

	out, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-2", []byte("assertion"))

	if !errors.Is(err, ErrNoChallenge) {
		t.Errorf("AnswerWebAuthn gave %v, want ErrNoChallenge", err)
	}
	if out.Complete {
		t.Fatal("a challenge issued for one request completed another")
	}
	if ceremony.verifies != 0 {
		t.Error("a mismatched request still reached the ceremony")
	}
}

// An assertion needs a ceremony THIS login issued.
//
// Without the check, a challenge raised for a TOTP-only user could be answered
// with an assertion checked against a session built from the request — which is
// not a check at all, since the expected value and the answer would come from
// the same place.
func TestAPasskeyNeedsACeremonyThisLoginIssued(t *testing.T) {
	challenges := newMemoryChallenges()
	ceremony := &fakeCeremony{factorID: "f1"}
	f := &Framework{
		Registry:   NewRegistry(&fakeVerifier{kind: TypeTOTP, correct: "123456"}),
		Store:      &memoryStore{factors: []Factor{confirmedTOTP("f1")}},
		Challenges: challenges,
		WebAuthn:   ceremony,
	}

	// A TOTP-only challenge: no ceremony was issued.
	decision, err := f.Required(context.Background(), "u1", "o1", "pending-1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if len(decision.WebAuthnOptions) != 0 {
		t.Fatal("a ceremony was issued for a user with no passkey")
	}

	_, err = f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("assertion"))
	if !errors.Is(err, ErrNoSuchFactor) {
		t.Errorf("AnswerWebAuthn gave %v, want ErrNoSuchFactor", err)
	}
	if ceremony.verifies != 0 {
		t.Error("an assertion was verified against a ceremony that was never issued")
	}
}

// Passkey guesses are charged against the same per-user bound as everything
// else — one allowance, not one per credential kind.
func TestAnExhaustedBoundRefusesAPasskey(t *testing.T) {
	f, _, ceremony := passkeyFramework(t)
	decision := issuedPasskey(t, f, "pending-1")
	f.Attempts = &countingAttempts{allow: false}

	_, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("assertion"))

	if !errors.Is(err, ErrTooManyAttempts) {
		t.Errorf("AnswerWebAuthn gave %v, want ErrTooManyAttempts", err)
	}
	if ceremony.verifies != 0 {
		t.Error("an exhausted user still reached the ceremony")
	}
}

// A refused assertion counts against the bound and eventually spends the
// challenge, exactly as a wrong TOTP code does.
func TestRefusedAssertionsSpendTheChallenge(t *testing.T) {
	f, _, ceremony := passkeyFramework(t)
	ceremony.err = ErrOriginMismatch
	bound := &countingAttempts{allow: true}
	f.Attempts = bound
	decision := issuedPasskey(t, f, "pending-1")

	var last error
	for i := 0; i < MaxAttempts; i++ {
		_, last = f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("a"))
	}

	if !errors.Is(last, ErrChallengeSpent) {
		t.Errorf("after %d refused assertions the challenge gave %v, want ErrChallengeSpent",
			MaxAttempts, last)
	}
	if bound.failed == 0 {
		t.Error("a refused assertion did not count against the attempt bound")
	}
}

// An origin mismatch reaches the caller as itself, so the handler can log what
// it is while telling the browser what every failure is told.
func TestAnOriginMismatchIsDistinguishableToTheCaller(t *testing.T) {
	f, _, ceremony := passkeyFramework(t)
	ceremony.err = ErrOriginMismatch
	decision := issuedPasskey(t, f, "pending-1")

	_, err := f.AnswerWebAuthn(context.Background(), decision.Handle, "pending-1", []byte("a"))
	if !errors.Is(err, ErrOriginMismatch) {
		t.Errorf("AnswerWebAuthn gave %v, want ErrOriginMismatch", err)
	}
}

// --- raising the challenge ------------------------------------------------------------

// A ceremony that cannot be begun REFUSES the login rather than quietly
// offering nothing.
//
// The opposite of the recovery-code lookup, which answers "no" on failure
// because the user's factor still works. Here the ceremony IS the user's
// factor: degrading would lock out somebody whose only credential is a passkey,
// and it would look like they simply have none.
func TestACeremonyFailureRefusesTheLogin(t *testing.T) {
	f, _, ceremony := passkeyFramework(t)
	ceremony.optionsErr = errors.New("the credential store is unreachable")

	_, err := f.Required(context.Background(), "u1", "o1", "pending-1")
	if err == nil {
		t.Fatal("a ceremony that could not be begun let the login continue with no passkey offered")
	}
}

// The options are issued once and reused by a re-render, so a refresh is not a
// supply of fresh challenges.
func TestARerenderReusesTheSameCeremony(t *testing.T) {
	f, _, _ := passkeyFramework(t)
	decision := issuedPasskey(t, f, "pending-1")

	offer, err := f.Peek(context.Background(), decision.Handle)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if string(offer.WebAuthnOptions) != string(decision.WebAuthnOptions) {
		t.Errorf("a re-render offered a different ceremony:\n got %s\nwant %s",
			offer.WebAuthnOptions, decision.WebAuthnOptions)
	}
	if !offer.Passkey() {
		t.Error("the offer does not report a passkey, so the form would not render")
	}
}

// A build with no ceremony refuses passkeys rather than panicking.
func TestABuildWithoutACeremonyRefusesPasskeys(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}},
		[]Factor{confirmedTOTP("f1")})
	handle := issued(t, store, "pending-1", "f1")

	if _, err := f.AnswerWebAuthn(context.Background(), handle, "pending-1", []byte("a")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("AnswerWebAuthn on a build with no ceremony gave %v, want ErrUnsupported", err)
	}
}

// An offer with options but no WebAuthn factor does not render a passkey form —
// which is the state a rollback produces.
func TestAnOfferWithoutAWebAuthnFactorIsNotAPasskey(t *testing.T) {
	offer := Offer{Types: []Type{TypeTOTP}, WebAuthnOptions: []byte(`{"publicKey":{}}`)}
	if offer.Passkey() {
		t.Error("a passkey form would render for a user holding only TOTP")
	}

	empty := Offer{Types: []Type{TypeWebAuthn}}
	if empty.Passkey() {
		t.Error("a passkey form would render with no ceremony behind it")
	}
}
