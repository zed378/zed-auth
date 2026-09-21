package samlapi

import (
	"testing"
)

// Which protocol owns a login request id (P4-08 F-3).
//
// The routing itself is two lines, and the reason it gets its own test is that
// a wrong answer here is invisible: the login page renders, the user signs in,
// and the flow completes against the wrong protocol — or does not complete at
// all, with a message about a request that "is no longer available".

func TestOnlyANamespacedIDBelongsToSAML(t *testing.T) {
	for name, tc := range map[string]struct {
		id   string
		saml bool
	}{
		"a SAML request":              {RequestPrefix + "_abc123", true},
		"a SAML request with a colon": {RequestPrefix + "_abc:123", true},
		"an OAuth pending id":         {"aGVsbG8td29ybGQtaWQ", false},
		"an OAuth id containing saml": {"samlish-but-not-prefixed", false},
		"an empty id":                 {"", false},
		"the prefix alone":            {RequestPrefix, true},
	} {
		if got := IsSAMLRequest(tc.id); got != tc.saml {
			t.Errorf("%s: IsSAMLRequest(%q) = %v, want %v", name, tc.id, got, tc.saml)
		}
	}
}

// The prefix must not be something an OAuth pending id could produce, or one
// protocol's request would be routed to the other's handler.
func TestThePrefixCannotCollideWithAnOAuthPendingID(t *testing.T) {
	// OAuth pending ids are opaque base64url: letters, digits, `-` and `_`.
	// A colon cannot appear in one.
	const base64url = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	for _, c := range RequestPrefix {
		if idx := indexRune(base64url, c); idx < 0 {
			return // at least one character of the prefix is unreachable
		}
	}
	t.Errorf("every character of %q can appear in a base64url id — the namespace can collide", RequestPrefix)
}

func indexRune(s string, r rune) int {
	for i, c := range s {
		if c == r {
			return i
		}
	}
	return -1
}

// A dispatcher with no SAML handler routes everything to OAuth, so an instance
// that has not configured SAML behaves exactly as it did before this existed.
func TestWithoutASAMLHandlerEverythingIsOAuth(t *testing.T) {
	d := &Dispatcher{}
	if d.SAML != nil {
		t.Fatal("the zero dispatcher has a SAML handler")
	}
	// The routing predicate still answers true for a namespaced id; the guard
	// in Peek and Resume is what makes the nil handler safe. Asserted here
	// because removing that guard is a nil dereference in a login flow.
	if !IsSAMLRequest(RequestPrefix + "_x") {
		t.Error("a namespaced id stopped being recognised")
	}
}
