package authn

import "testing"

// The address is compared against the `lower(email)` unique index P0-07's
// migration creates, so the normalisation has to be exactly that and no more.
func TestNormaliseEmail(t *testing.T) {
	cases := map[string]string{
		"  Alice@Example.TEST  ": "alice@example.test",
		"ALICE@EXAMPLE.TEST":     "alice@example.test",
		"alice@example.test":     "alice@example.test",
		"   ":                    "",
		"":                       "",
	}

	for in, want := range cases {
		if got := NormaliseEmail(in); got != want {
			t.Errorf("NormaliseEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

// Deliberately NOT clever. Stripping dots or plus-addressing is
// provider-specific behaviour, and a service that decides two different
// addresses are the same person has decided something it was never told —
// which, at a login endpoint, means letting one person's password work against
// another person's account.
func TestNormaliseEmailDoesNotCanonicaliseTheLocalPart(t *testing.T) {
	for _, address := range []string{
		"a.lice@example.test",
		"alice+billing@example.test",
	} {
		if NormaliseEmail(address) == "alice@example.test" {
			t.Errorf("%q was folded into another address", address)
		}
	}
}

// Only an active account may sign in. The other three statuses are refused,
// and the caller must give the same answer for all of them as for a wrong
// password — "this account is locked" confirms the account exists.
func TestOnlyActiveAccountsMaySignIn(t *testing.T) {
	if !(User{Status: StatusActive}).CanSignIn() {
		t.Error("an active user cannot sign in")
	}
	for _, status := range []string{StatusLocked, StatusInvited, StatusDeactivated, "", "unknown"} {
		if (User{Status: status}).CanSignIn() {
			t.Errorf("a %q user may sign in", status)
		}
	}
}
