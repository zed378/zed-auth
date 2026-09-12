package mfa

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The framework, without a factor in it (P3-01).
//
// Nothing here is end-to-end demonstrable — TOTP arrives in `P3-02` — so these
// tests carry more weight than usual: there is no screen to look at and no
// login to walk through. What they pin is the set of properties the spec calls
// the highest-risk object in the system.

// --- test doubles ------------------------------------------------------------

// fakeVerifier is a factor that says yes to one code.
type fakeVerifier struct {
	kind     Type
	correct  string
	fail     error // returned instead of a verdict, for the outage case
	verified int
}

func (f *fakeVerifier) Type() Type { return f.kind }

func (f *fakeVerifier) Begin(context.Context, string, string, string) (Enrolment, error) {
	return Enrolment{FactorID: "new", Secret: "s3cret"}, nil
}

func (f *fakeVerifier) Confirm(context.Context, string, string) error { return nil }

func (f *fakeVerifier) Verify(_ context.Context, _, code string) error {
	f.verified++
	if f.fail != nil {
		return f.fail
	}
	if code != f.correct {
		return ErrWrongCode
	}
	return nil
}

func (f *fakeVerifier) Remove(context.Context, string) error { return nil }

// memoryChallenges is the store, in a map.
type memoryChallenges struct {
	mu sync.Mutex
	at map[string]Challenge
}

func newMemoryChallenges() *memoryChallenges {
	return &memoryChallenges{at: map[string]Challenge{}}
}

func (m *memoryChallenges) Put(_ context.Context, c Challenge, _ time.Duration) (string, error) {
	handle, err := NewHandle()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.at[handle] = c
	return handle, nil
}

func (m *memoryChallenges) Get(_ context.Context, handle string) (Challenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.at[handle]
	if !ok {
		return Challenge{}, ErrNoChallenge
	}
	return c, nil
}

func (m *memoryChallenges) Replace(_ context.Context, handle string, c Challenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.at[handle]; ok {
		m.at[handle] = c
	}
	return nil
}

func (m *memoryChallenges) Delete(_ context.Context, handle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.at, handle)
	return nil
}

// memoryStore returns a fixed set of factors.
type memoryStore struct {
	factors []Factor
	err     error
}

// countingStore records how often it was asked, so a test can assert a read
// did NOT happen — which is the only observable difference an early return
// makes when the outcome is the same either way.
type countingStore struct {
	factors []Factor
	reads   int
}

func (c *countingStore) Confirmed(context.Context, string) ([]Factor, error) {
	c.reads++
	return c.factors, nil
}

// growingStore lets a test enrol a factor after a challenge has been issued.
type growingStore struct {
	factors []Factor
}

func (g *growingStore) Confirmed(context.Context, string) ([]Factor, error) {
	return g.factors, nil
}

func (m *memoryStore) Confirmed(context.Context, string) ([]Factor, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.factors, nil
}

func framework(t *testing.T, verifiers []Verifier, factors []Factor) (*Framework, *memoryChallenges) {
	t.Helper()
	challenges := newMemoryChallenges()
	return &Framework{
		Registry:   NewRegistry(verifiers...),
		Store:      &memoryStore{factors: factors},
		Challenges: challenges,
	}, challenges
}

func confirmedTOTP(id string) Factor {
	return Factor{ID: id, UserID: "u1", OrgID: "o1", Type: TypeTOTP, Confirmed: true}
}

// --- amr ---------------------------------------------------------------------

// `amr` says what was used, never what is enrolled.
//
// This is the task's goal sentence and abuse case A-6. A consumer refusing a
// payment unless `amr` contains `otp` is trusting this service to have actually
// challenged.
func TestAMRReflectsWhatWasUsed(t *testing.T) {
	for _, c := range []struct {
		what     string
		password bool
		used     []Type
		want     []string
	}{
		{"a password alone", true, nil, []string{"pwd"}},
		{"a password and an authenticator app", true, []Type{TypeTOTP}, []string{"mfa", "otp", "pwd"}},
		{"a password and a passkey", true, []Type{TypeWebAuthn}, []string{"hwk", "mfa", "pwd"}},
		{"a passkey alone", false, []Type{TypeWebAuthn}, []string{"hwk"}},
		{"nothing at all", false, nil, nil},
	} {
		got := AuthMethods(c.password, c.used...)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: amr is %v, want %v", c.what, got, c.want)
		}
	}
}

// `mfa` is not a synonym for "a second factor exists".
//
// The case a convenience shortcut breaks: a user who HAS a factor enrolled and
// signed in with a password alone.
func TestAPasswordOnlyLoginByAnEnrolledUserIsNotMultiFactor(t *testing.T) {
	got := AuthMethods(true) // the user's enrolment is not an input here, deliberately

	for _, method := range got {
		if method == MethodMultiFactor {
			t.Fatalf("a password-only login carries %q: amr is being written from what is "+
				"enrolled rather than from what was used", MethodMultiFactor)
		}
	}
	if len(got) != 1 || got[0] != MethodPassword {
		t.Errorf("amr is %v, want just [%q]", got, MethodPassword)
	}
}

// Two credentials of ONE kind are not multi-factor.
func TestTwoFactorsOfTheSameKindAreNotMultiFactor(t *testing.T) {
	got := AuthMethods(false, TypeWebAuthn, TypeWebAuthn)

	for _, method := range got {
		if method == MethodMultiFactor {
			t.Errorf("two passkeys produced %q — a consumer asking for it is asking "+
				"whether more than one KIND of thing was involved", MethodMultiFactor)
		}
	}
}

// --- the challenge decision ---------------------------------------------------

// No factor type implemented means no challenge, and no read either.
//
// The deployment state this task ships in. A challenge offering nothing is a
// dead end a user cannot get out of.
//
// The assertion that matters is the SECOND one. Without the early return the
// decision is the same — every factor fails its per-factor verifier lookup and
// nothing is answerable — so a test that only checked the outcome passed
// against a build with the check removed. What the early return actually buys
// is that a deployment with no factors pays nothing for the feature: no query,
// on every login.
func TestABuildWithNoFactorsNeverChallengesAndNeverReads(t *testing.T) {
	store := &countingStore{factors: []Factor{confirmedTOTP("f1")}}
	f := &Framework{
		Registry:   NewRegistry(),
		Store:      store,
		Challenges: newMemoryChallenges(),
	}

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if decision.Challenge {
		t.Error("a build with no factor verifiers issued a challenge nobody could answer")
	}
	if store.reads != 0 {
		t.Errorf("the factor store was read %d time(s) on a build that implements no "+
			"factors — every login pays for a query that cannot change the answer", store.reads)
	}
}

// A user with no confirmed factor is not challenged.
func TestAUserWithNoFactorIsNotChallenged(t *testing.T) {
	f, _ := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP}}, nil)

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if decision.Challenge {
		t.Error("a user with nothing enrolled was challenged")
	}
}

// An unconfirmed enrolment is not a factor.
//
// A half-finished enrolment must never become answerable — otherwise a user who
// scanned a QR code and closed the tab holds a factor they cannot use and
// cannot get past.
func TestAnUnconfirmedFactorIsNotAnswerable(t *testing.T) {
	unconfirmed := Factor{ID: "f1", UserID: "u1", OrgID: "o1", Type: TypeTOTP, Confirmed: false}
	f, _ := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP}}, []Factor{unconfirmed})

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if decision.Challenge {
		t.Error("an unconfirmed enrolment produced a challenge")
	}
}

// A factor whose type this build no longer implements is not offered.
func TestAFactorWithNoVerifierIsNotOffered(t *testing.T) {
	orphan := Factor{ID: "f1", UserID: "u1", OrgID: "o1", Type: TypeWebAuthn, Confirmed: true}
	f, _ := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP}}, []Factor{orphan})

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if decision.Challenge {
		t.Error("a factor with no verifier in this build was offered as answerable")
	}
}

// Every enrolled type is offered, so losing one device is not a lockout.
func TestEveryEnrolledTypeIsOffered(t *testing.T) {
	factors := []Factor{
		confirmedTOTP("f1"),
		{ID: "f2", UserID: "u1", OrgID: "o1", Type: TypeWebAuthn, Confirmed: true},
	}
	f, _ := framework(t, []Verifier{
		&fakeVerifier{kind: TypeTOTP},
		&fakeVerifier{kind: TypeWebAuthn},
	}, factors)

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil {
		t.Fatalf("Required: %v", err)
	}
	if !decision.Challenge {
		t.Fatal("a user with two factors was not challenged")
	}
	if len(decision.Offered) != 2 {
		t.Errorf("offered %v, want both types — a user who lost one device must not "+
			"be shown a dead end", decision.Offered)
	}
}

// A factor-store failure refuses rather than letting the login through.
func TestAStoreFailureRefusesRatherThanPassing(t *testing.T) {
	f := &Framework{
		Registry:   NewRegistry(&fakeVerifier{kind: TypeTOTP}),
		Store:      &memoryStore{err: errors.New("the database went away")},
		Challenges: newMemoryChallenges(),
	}

	if _, err := f.Required(context.Background(), "u1", "o1", "p1"); err == nil {
		t.Error("an unreachable factor store produced no error, so the caller would " +
			"complete a login that was never checked")
	}
}

// --- answering ---------------------------------------------------------------

func TestACorrectCodeCompletesTheChallenge(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}},
		[]Factor{confirmedTOTP("f1")})

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil || !decision.Challenge {
		t.Fatalf("Required: %v, challenge=%v", err, decision.Challenge)
	}

	used, err := f.Answer(context.Background(), decision.Handle, "f1", "123456")
	if err != nil {
		t.Fatalf("a correct code was refused: %v", err)
	}
	if len(used) != 1 || used[0] != TypeTOTP {
		t.Errorf("the completed challenge reports %v as used", used)
	}

	// Consumed, so it cannot be replayed.
	if _, err := store.Get(context.Background(), decision.Handle); !errors.Is(err, ErrNoChallenge) {
		t.Error("a completed challenge is still readable, so it can be answered twice")
	}
}

// Abuse case A-2: brute-forcing a six-digit code inside its window.
func TestAChallengeIsSpentAfterTooManyAttempts(t *testing.T) {
	f, _ := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}},
		[]Factor{confirmedTOTP("f1")})

	decision, _ := f.Required(context.Background(), "u1", "o1", "p1")

	for i := 0; i < MaxAttempts-1; i++ {
		if _, err := f.Answer(context.Background(), decision.Handle, "f1", "000000"); !errors.Is(err, ErrWrongCode) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}

	// The last one spends it.
	if _, err := f.Answer(context.Background(), decision.Handle, "f1", "000000"); !errors.Is(err, ErrChallengeSpent) {
		t.Fatalf("the %dth wrong answer did not spend the challenge: %v", MaxAttempts, err)
	}

	// And now the CORRECT code does not work either — which is the half that
	// makes this a bound rather than a speed bump.
	if _, err := f.Answer(context.Background(), decision.Handle, "f1", "123456"); err == nil {
		t.Error("a correct code was accepted after the attempt bound was reached")
	}
}

// A spent challenge that is still present is refused before the verifier.
//
// The case the early check guards, and the one the test above does not reach:
// a challenge whose delete failed, or a store that outlived the process that
// spent it. Without the early return the attempt reaches the verifier, and a
// spent challenge that still verifies is not a bound.
func TestASpentChallengeIsRefusedWithoutReachingTheVerifier(t *testing.T) {
	verifier := &fakeVerifier{kind: TypeTOTP, correct: "123456"}
	challenges := newMemoryChallenges()
	f := &Framework{
		Registry:   NewRegistry(verifier),
		Store:      &memoryStore{factors: []Factor{confirmedTOTP("f1")}},
		Challenges: challenges,
	}

	// Spent, and still there — which is what a failed delete leaves behind.
	handle, err := challenges.Put(context.Background(), Challenge{
		UserID: "u1", OrgID: "o1", PendingID: "p1",
		FactorIDs: []string{"f1"}, Attempts: MaxAttempts,
	}, ChallengeTTL)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := f.Answer(context.Background(), handle, "f1", "123456"); !errors.Is(err, ErrChallengeSpent) {
		t.Errorf("a spent challenge gave %v, want ErrChallengeSpent", err)
	}
	if verifier.verified != 0 {
		t.Error("a spent challenge reached the verifier — the bound is applied after " +
			"the credential is used rather than before")
	}
}

// Abuse case A-3: answering with a factor the challenge never named.
//
// The factor used here **exists and belongs to this user** — it is simply not
// one the challenge offered, which is what a factor enrolled after the
// challenge was issued looks like. An id that does not exist at all is refused
// by the store lookup underneath, so a test using one proves nothing about
// this check: the mutation run caught exactly that.
func TestAFactorTheChallengeDidNotNameIsRefused(t *testing.T) {
	verifier := &fakeVerifier{kind: TypeTOTP, correct: "123456"}

	// Two real factors, and the challenge is issued while only one exists.
	store := &growingStore{factors: []Factor{confirmedTOTP("f1")}}
	f := &Framework{
		Registry:   NewRegistry(verifier),
		Store:      store,
		Challenges: newMemoryChallenges(),
	}

	decision, err := f.Required(context.Background(), "u1", "o1", "p1")
	if err != nil || !decision.Challenge {
		t.Fatalf("Required: %v, challenge=%v", err, decision.Challenge)
	}

	// Enrolled after the challenge was issued: real, confirmed, this user's,
	// and not something this challenge may be answered with.
	store.factors = append(store.factors, Factor{
		ID: "f2", UserID: "u1", OrgID: "o1", Type: TypeTOTP, Confirmed: true,
	})

	if _, err := f.Answer(context.Background(), decision.Handle, "f2", "123456"); !errors.Is(err, ErrNoSuchFactor) {
		t.Errorf("answering with a factor the challenge did not name gave %v, want ErrNoSuchFactor", err)
	}
	if verifier.verified != 0 {
		t.Error("the verifier was asked about a factor the challenge never named — " +
			"a factor enrolled mid-challenge became answerable by it")
	}
}

// Abuse case A-1: a handle that names nothing.
func TestAnUnknownHandleNamesNothing(t *testing.T) {
	f, _ := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP}}, []Factor{confirmedTOTP("f1")})

	if _, err := f.Answer(context.Background(), "not-a-real-handle", "f1", "123456"); !errors.Is(err, ErrNoChallenge) {
		t.Errorf("an invented handle gave %v, want ErrNoChallenge", err)
	}
}

// A verifier that cannot decide is not a pass.
func TestAVerifierOutageIsNotAPass(t *testing.T) {
	broken := &fakeVerifier{kind: TypeTOTP, fail: errors.New("the factor store went away")}
	f, _ := framework(t, []Verifier{broken}, []Factor{confirmedTOTP("f1")})

	decision, _ := f.Required(context.Background(), "u1", "o1", "p1")

	_, err := f.Answer(context.Background(), decision.Handle, "f1", "123456")
	if err == nil {
		t.Fatal("a verifier that could not decide was treated as a pass")
	}
	if errors.Is(err, ErrWrongCode) {
		t.Error("an outage was reported as a wrong code, so the user is told to " +
			"check their authenticator app while the service is broken")
	}
}

// A failed attempt does not extend the window.
//
// Checked here on the counter rather than the TTL — the TTL half is Redis's and
// is asserted in the integration test — but the intent is the same: an attacker
// who could refresh the clock by guessing wrong would have removed the time
// bound by using the thing it bounds.
func TestAFailedAttemptIsRecorded(t *testing.T) {
	f, store := framework(t, []Verifier{&fakeVerifier{kind: TypeTOTP, correct: "123456"}},
		[]Factor{confirmedTOTP("f1")})

	decision, _ := f.Required(context.Background(), "u1", "o1", "p1")
	_, _ = f.Answer(context.Background(), decision.Handle, "f1", "000000")

	after, err := store.Get(context.Background(), decision.Handle)
	if err != nil {
		t.Fatalf("reading the challenge back: %v", err)
	}
	if after.Attempts != 1 {
		t.Errorf("after one wrong answer the challenge records %d attempts", after.Attempts)
	}
}

// --- step-up ------------------------------------------------------------------

// Abuse case A-4: a requirement for a stronger factor is not met by a weaker one.
func TestStepUpIsNotSatisfiedByAWeakerFactor(t *testing.T) {
	session := AuthMethods(true, TypeTOTP) // pwd, otp, mfa

	if !StepUp(session, []string{"otp"}) {
		t.Error("a session that used an authenticator app does not satisfy a requirement for one")
	}
	if StepUp(session, []string{"hwk"}) {
		t.Error("a requirement for a hardware-backed credential was satisfied by an " +
			"authenticator app — which is the downgrade this distinction exists to prevent")
	}
}

func TestStepUpWithNoRequirementIsAlwaysSatisfied(t *testing.T) {
	if !StepUp(nil, nil) {
		t.Error("a session with no requirement to meet was refused")
	}
}

func TestStepUpNeedsEveryRequiredMethod(t *testing.T) {
	session := AuthMethods(true, TypeTOTP)

	if StepUp(session, []string{"pwd", "hwk"}) {
		t.Error("a requirement for two methods was satisfied by a session holding one of them")
	}
	if !StepUp(session, []string{"pwd", "otp"}) {
		t.Error("a requirement for two methods the session DID use was refused")
	}
}

// --- the handle ---------------------------------------------------------------

// The handle carries nothing. It is a lookup key, not a container.
func TestAHandleCarriesNothing(t *testing.T) {
	handle, err := NewHandle()
	if err != nil {
		t.Fatalf("NewHandle: %v", err)
	}

	for _, leak := range []string{"u1", "o1", "user", "org", "totp"} {
		if strings.Contains(handle, leak) {
			t.Errorf("the handle contains %q — there is a field in it for a client to edit", leak)
		}
	}
	if len(handle) < 40 {
		t.Errorf("a %d-character handle is short enough to be worth guessing", len(handle))
	}
}

// Two handles are never the same.
func TestHandlesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		handle, err := NewHandle()
		if err != nil {
			t.Fatalf("NewHandle: %v", err)
		}
		if seen[handle] {
			t.Fatal("two handles collided in a thousand draws")
		}
		seen[handle] = true
	}
}

// The storage key is a hash, so the store holds nothing presentable.
func TestTheStorageKeyIsNotTheHandle(t *testing.T) {
	handle, _ := NewHandle()
	key := HandleKey(handle)

	if strings.Contains(key, handle) {
		t.Error("the storage key contains the handle, so a dump of the store — a KEYS, " +
			"a backup, a slow-command log — hands out working challenge handles")
	}
	if HandleKey(handle) != key {
		t.Error("the same handle hashed to two different keys")
	}
}

// --- the registry -------------------------------------------------------------

func TestAnUnimplementedTypeIsRefused(t *testing.T) {
	r := NewRegistry(&fakeVerifier{kind: TypeTOTP})

	if _, err := r.For(TypeWebAuthn); !errors.Is(err, ErrUnsupported) {
		t.Errorf("an unimplemented type gave %v, want ErrUnsupported", err)
	}
	if _, err := r.For(Type("sms")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("an invented type gave %v, want ErrUnsupported", err)
	}
}

func TestOnlyImplementedTypesAreValid(t *testing.T) {
	for _, valid := range []Type{TypeTOTP, TypeWebAuthn} {
		if !valid.Valid() {
			t.Errorf("%q is not valid, but the schema allows it", valid)
		}
		if valid.AMR() == "" {
			t.Errorf("%q contributes no amr value, so a consumer cannot tell it was used", valid)
		}
	}
	for _, invalid := range []Type{"", "sms", "email", "push"} {
		if invalid.Valid() {
			t.Errorf("%q is treated as a valid factor type and nothing implements it", invalid)
		}
	}
}

// The two implemented types contribute DIFFERENT amr values.
//
// If they collapsed to one, a consumer requiring a hardware credential would be
// satisfied by an authenticator app and the step-up distinction would be
// decorative.
func TestTheTwoFactorTypesAreDistinguishableInAMR(t *testing.T) {
	if TypeTOTP.AMR() == TypeWebAuthn.AMR() {
		t.Fatalf("both factor types report %q, so a consumer cannot ask for one "+
			"specifically", TypeTOTP.AMR())
	}
}

// --- a store that is not there ------------------------------------------------

// Every operation on an unconfigured store refuses rather than panicking.
//
// A nil client is a deployment mistake, not an attack, and it should read like
// one: an error naming the missing configuration, not a nil dereference in the
// middle of a login.
func TestAnUnconfiguredStoreRefusesEveryOperation(t *testing.T) {
	var store *RedisChallenges
	ctx := context.Background()

	if _, err := store.Put(ctx, aTestChallenge(), ChallengeTTL); err == nil {
		t.Error("Put against an unconfigured store returned no error")
	}
	if _, err := store.Get(ctx, "anything"); err == nil {
		t.Error("Get against an unconfigured store returned no error")
	}
	if err := store.Replace(ctx, "anything", aTestChallenge()); err == nil {
		t.Error("Replace against an unconfigured store returned no error")
	}
	if err := store.Delete(ctx, "anything"); err == nil {
		t.Error("Delete against an unconfigured store returned no error")
	}
}

// An empty handle is not a lookup.
//
// It is what an absent form field produces, and turning it into a storage key
// would mean every request with no handle at all probing the same key.
func TestAnEmptyHandleIsNotAKey(t *testing.T) {
	if key := HandleKey(""); key != "" {
		t.Errorf("an empty handle produced the storage key %q", key)
	}
}

// A stored challenge with no subject is refused rather than returned.
//
// The one thing downstream does with a decoded challenge is trust its user id,
// so a document that has lost it must not come back as a usable value.
func TestAChallengeWithNoSubjectIsRefused(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"org_id":"o1"}`,
		`{"user_id":"u1"}`,
	} {
		if _, err := DecodeChallenge([]byte(raw)); err == nil {
			t.Errorf("%s decoded into a usable challenge", raw)
		}
	}

	// And one that does name a user comes back.
	good := `{"user_id":"u1","org_id":"o1"}`
	if _, err := DecodeChallenge([]byte(good)); err != nil {
		t.Errorf("a complete challenge failed to decode: %v", err)
	}
}

// Malformed stored bytes are an error, not an empty challenge.
func TestMalformedStoredBytesAreAnError(t *testing.T) {
	if _, err := DecodeChallenge([]byte("not json at all")); err == nil {
		t.Error("malformed bytes decoded into a challenge")
	}
}

// A challenge round-trips through its own encoding.
func TestAChallengeSurvivesEncoding(t *testing.T) {
	original := aTestChallenge()
	original.Attempts = 3
	original.Methods = []Type{TypeTOTP}

	raw, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeChallenge(raw)
	if err != nil {
		t.Fatalf("DecodeChallenge: %v", err)
	}

	if got.UserID != original.UserID || got.Attempts != original.Attempts ||
		len(got.Methods) != 1 || got.Methods[0] != TypeTOTP {
		t.Errorf("the challenge came back as %+v", got)
	}
}

// The registry reports what it implements, in a stable order.
//
// Map iteration order is random in Go, so a caller rendering the list would
// otherwise get a different order per request — a page that reshuffles its
// options on every load looks broken.
func TestTheRegistryListsWhatItImplementsInAStableOrder(t *testing.T) {
	r := NewRegistry(&fakeVerifier{kind: TypeWebAuthn}, &fakeVerifier{kind: TypeTOTP})

	first := r.Implemented()
	if len(first) != 2 {
		t.Fatalf("the registry reports %v", first)
	}
	for i := 0; i < 20; i++ {
		if got := r.Implemented(); !reflect.DeepEqual(got, first) {
			t.Fatalf("the order changed between calls: %v then %v", first, got)
		}
	}

	var empty *Registry
	if !empty.Empty() {
		t.Error("a nil registry does not report itself as empty")
	}
	if got := empty.Implemented(); got != nil {
		t.Errorf("a nil registry listed %v", got)
	}
	if _, err := empty.For(TypeTOTP); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a nil registry gave %v for a lookup, want ErrUnsupported", err)
	}
}

func aTestChallenge() Challenge {
	return Challenge{
		UserID:    "u1",
		OrgID:     "o1",
		PendingID: "p1",
		FactorIDs: []string{"f1"},
		CreatedAt: time.Now(),
	}
}
