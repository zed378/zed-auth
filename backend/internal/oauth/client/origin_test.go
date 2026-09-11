package client

import (
	"errors"
	"strings"
	"testing"
)

// Allowed-origin validation (P1-29).
//
// The value stored here is compared to a browser's `Origin` header by exact
// string comparison, so the only useful validation is the one that refuses
// anything that could never match. A permissive validator does not produce a
// security hole here — it produces a registration that silently never works,
// which a consumer team debugs by reading their own code for an hour.

func TestAnOriginIsSchemeAndHostAndNothingElse(t *testing.T) {
	for _, given := range []struct{ raw, want string }{
		{"https://app.example.com", "https://app.example.com"},
		{"https://app.example.com:8443", "https://app.example.com:8443"},

		// Case is normalised once, at registration, so the comparison at
		// request time never has to. A browser lowercases the host anyway.
		{"HTTPS://App.Example.COM", "https://app.example.com"},

		// Loopback over http, for local development. Three spellings, because
		// a developer will use whichever their tooling picked.
		{"http://localhost:5173", "http://localhost:5173"},
		{"http://127.0.0.1:4173", "http://127.0.0.1:4173"},
		{"http://[::1]:3000", "http://[::1]:3000"},
	} {
		got, err := ValidateAllowedOrigin(given.raw)
		if err != nil {
			t.Errorf("ValidateAllowedOrigin(%q) refused it: %v", given.raw, err)
			continue
		}
		if got != given.want {
			t.Errorf("ValidateAllowedOrigin(%q) = %q, want %q", given.raw, got, given.want)
		}
	}
}

func TestAnOriginThatCouldNeverMatchIsRefused(t *testing.T) {
	for _, given := range []struct{ raw, because string }{
		{"", "empty"},
		{"  ", "whitespace only"},
		{" https://app.example.com", "leading whitespace"},
		{"https://app.example.com ", "trailing whitespace"},

		// The one a registrant will genuinely expect to work. An `Origin`
		// header carries one concrete origin and the match is exact, so this
		// would be stored and never match anything.
		{"https://*.example.com", "wildcard"},

		// A trailing slash is a path. A browser never sends one in Origin, so
		// this entry matches nothing — and the failure looks like "CORS is
		// broken" rather than like a typo.
		{"https://app.example.com/", "trailing slash"},
		{"https://app.example.com/callback", "path"},
		{"https://app.example.com?a=1", "query"},
		{"https://app.example.com#f", "fragment"},

		{"app.example.com", "no scheme"},
		{"https://", "no host"},
		{"https://user:pass@app.example.com", "credentials"},

		// `null` is what a sandboxed iframe or a file:// document sends.
		// Registering it would hand every such document access.
		{"null", "the literal null origin"},

		// A custom scheme is how a NATIVE client receives a redirect. It is
		// never an origin a browser sends.
		{"com.example.app://callback", "custom scheme"},
		{"javascript:alert(1)", "javascript scheme"},
		{"data:text/html,x", "data scheme"},
	} {
		if _, err := ValidateAllowedOrigin(given.raw); err == nil {
			t.Errorf("ValidateAllowedOrigin(%q) was accepted (%s)", given.raw, given.because)
		} else if !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidateAllowedOrigin(%q) failed with %v, which does not wrap ErrInvalid", given.raw, err)
		}
	}
}

// The same rule redirect URIs live by, and for a related reason: a response a
// cleartext page can read is a response on the wire in cleartext.
func TestPlainHttpIsRefusedAwayFromLoopback(t *testing.T) {
	_, err := ValidateAllowedOrigin("http://app.example.com")
	if err == nil {
		t.Fatal("http://app.example.com was accepted")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
}

func TestMatchesOriginIsExactAndEmptyMatchesNothing(t *testing.T) {
	app := Application{AllowedOrigins: []string{"https://app.example.com"}}

	if !app.MatchesOrigin("https://app.example.com") {
		t.Error("the registered origin does not match itself")
	}

	for _, presented := range []string{
		"",
		"https://app.example.com/",
		"https://app.example.com:443",
		"http://app.example.com",
		"https://APP.example.com",
		"https://app.example.com.evil.test",
		"https://evil.test",
	} {
		if app.MatchesOrigin(presented) {
			t.Errorf("%q matched a registration of https://app.example.com", presented)
		}
	}

	// The default state of every application, and the one that must match
	// nothing at all rather than everything.
	none := Application{}
	for _, presented := range []string{"", "https://app.example.com", "*"} {
		if none.MatchesOrigin(presented) {
			t.Errorf("an application with no registered origins matched %q", presented)
		}
	}
}

func TestValidateRejectsAnApplicationWithAnUnusableOrigin(t *testing.T) {
	app := Application{
		Name:           "example",
		Type:           TypeSPA,
		RedirectURIs:   []string{"https://app.example.com/callback"},
		GrantTypes:     []string{GrantAuthorizationCode},
		AllowedOrigins: []string{"https://app.example.com", "https://*.example.com"},
	}

	// Caught at registration rather than at the first failed fetch. The whole
	// value of validating here is that the error names the wildcard.
	if err := app.Validate(); err == nil {
		t.Fatal("an application with a wildcard origin was accepted")
	}
}
