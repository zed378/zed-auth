package saml

import (
	"errors"
	"testing"
)

// The assertion describes what happened, never what was asked for (P4-08 C-6).

func TestTheAssertedClassComesFromTheSessionsMethods(t *testing.T) {
	for name, tc := range map[string]struct {
		methods []string
		want    string
	}{
		"password only": {[]string{"pwd"}, ClassPasswordProtectedTransport},

		// `mfa` is what internal/mfa emits when two distinct factor CATEGORIES
		// were used, and it is the only thing that earns MultiFactor here.
		"password and a certified second factor": {[]string{"pwd", "otp", "mfa"}, ClassMultiFactor},

		// A passkey alone is NOT multi-factor, and neither is a one-time code
		// alone. This service's own `amr` logic refuses to call either `mfa`,
		// and SAML is not the place to decide otherwise — the first version of
		// ClassFor did, which is the overclaim C-6 exists to prevent.
		"passkey alone":                {[]string{"hwk"}, ClassUnspecified},
		"one-time code alone":          {[]string{"otp"}, ClassUnspecified},
		"a second factor, uncertified": {[]string{"pwd", "hwk"}, ClassPasswordProtectedTransport},

		"nothing recorded":  {nil, ClassUnspecified},
		"something unknown": {[]string{"telepathy"}, ClassUnspecified},
	} {
		if got := ClassFor(tc.methods); got != tc.want {
			t.Errorf("%s: ClassFor(%v) = %q, want %q", name, tc.methods, got, tc.want)
		}
	}
}

// The failure this rule exists to prevent: a service provider asks for
// multi-factor, the session is password-only, and an assertion claims
// multi-factor because that is what was asked for. Everything succeeds and the
// service provider is wrong.
func TestARequestForMoreThanTheSessionEarnedIsRefused(t *testing.T) {
	asserted, err := CheckAuthnContext(ClassMultiFactor, []string{"pwd"})
	if !errors.Is(err, ErrNoAuthnContext) {
		t.Fatalf("a password-only session satisfied a multi-factor request, asserting %q (err %v)", asserted, err)
	}
	if asserted != "" {
		t.Errorf("a class came back with the refusal: %q", asserted)
	}
}

func TestASessionThatMeetsTheRequestAssertsWhatItEarned(t *testing.T) {
	asserted, err := CheckAuthnContext(ClassMultiFactor, []string{"pwd", "otp", "mfa"})
	if err != nil {
		t.Fatalf("a multi-factor session was refused a multi-factor request: %v", err)
	}
	if asserted != ClassMultiFactor {
		t.Errorf("asserted %q, want MultiFactor", asserted)
	}
}

// Stronger than requested is not a failure to meet a weaker requirement.
func TestMultiFactorSatisfiesAPasswordRequest(t *testing.T) {
	asserted, err := CheckAuthnContext(ClassPasswordProtectedTransport, []string{"pwd", "hwk", "mfa"})
	if err != nil {
		t.Fatalf("a multi-factor session was refused a password request: %v", err)
	}
	// And it still asserts what actually happened, not what was asked for.
	if asserted != ClassMultiFactor {
		t.Errorf("asserted %q — the assertion must describe the session, not the request", asserted)
	}
}

func TestNoRequestedContextIsSatisfiedByAnything(t *testing.T) {
	for _, methods := range [][]string{nil, {"pwd"}, {"pwd", "otp", "mfa"}} {
		asserted, err := CheckAuthnContext("", methods)
		if err != nil {
			t.Errorf("methods %v: an unrequested context was refused: %v", methods, err)
		}
		if asserted != ClassFor(methods) {
			t.Errorf("methods %v: asserted %q, want the session's own class", methods, asserted)
		}
	}
}

// An unknown class asked for by a service provider is refused rather than
// approximated. Guessing which of our classes a stranger's URI means is how a
// service provider ends up told something untrue.
func TestAnUnrecognisedRequestedContextIsRefused(t *testing.T) {
	if _, err := CheckAuthnContext("urn:example:custom:VeryStrong", []string{"pwd", "otp", "mfa"}); !errors.Is(err, ErrNoAuthnContext) {
		t.Error("an unrecognised requested context was satisfied by a multi-factor session")
	}
}

// The OIDC and SAML descriptions of one session cannot disagree, because both
// read session.AuthMethods. This pins the mapping's inputs to the `amr` values
// P1-07 actually records.
func TestTheMappingCoversTheAmrValuesTheServiceRecords(t *testing.T) {
	// The four spellings internal/mfa writes (RFC 8176). Two of them earn a
	// class on their own; two do not, and that asymmetry is the point rather
	// than a gap — `otp` and `hwk` describe WHICH factor, while `mfa` is the
	// service's own statement that more than one category was used.
	if ClassFor([]string{"pwd"}) != ClassPasswordProtectedTransport {
		t.Error("`pwd` does not map to PasswordProtectedTransport")
	}
	if ClassFor([]string{"mfa"}) != ClassMultiFactor {
		t.Error("`mfa` does not map to MultiFactor")
	}
	for _, alone := range []string{"otp", "hwk"} {
		if ClassFor([]string{alone}) != ClassUnspecified {
			t.Errorf("%q alone claims a class — only `mfa` certifies multi-factor", alone)
		}
	}
}
