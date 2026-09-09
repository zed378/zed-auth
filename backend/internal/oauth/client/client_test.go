package client

import (
	"strings"
	"testing"
)

// --- redirect matching ------------------------------------------------------

// The registered URI every matching test compares against.
const registered = "https://app.example.com/oauth/callback"

func app(uris ...string) Application {
	return Application{
		ID:           "11111111-1111-1111-1111-111111111111",
		Type:         TypeWeb,
		RedirectURIs: uris,
	}
}

// P1-05 DoD item 3, and abuse cases A-1 and A-2.
//
// Each row names the attack it prevents rather than just the input, because a
// table of strings with no reasons is a table somebody deletes a row from when
// it becomes inconvenient.
func TestRedirectURIMatchingIsExact(t *testing.T) {
	a := app(registered)

	rejected := []struct {
		presented string
		why       string
	}{
		{registered + "/", "a trailing slash is a different string, and tolerating it is the precedent that makes the next tolerance look reasonable"},
		{registered + "?next=/", "an extra query parameter changes where the code is delivered"},
		{registered + "#frag", "a fragment never reaches the server and cannot take part in the match"},
		{"https://app.example.com/oauth/callback/../../evil", "dot segments resolve to a different path only if something resolves them; nothing here does"},
		{"https://app.example.com/oauth", "a prefix of the registered URI"},
		{"https://app.example.com/oauth/callback/extra", "the registered URI as a prefix of the presented one — the classic open redirect"},
		{"https://app.example.com.attacker.net/oauth/callback", "suffix confusion: only prefix matching would ever accept this"},
		{"https://attacker.net/oauth/callback", "a different host entirely"},
		{"HTTPS://APP.EXAMPLE.COM/oauth/callback", "registration lowercases scheme and host, so the canonical form is what must be presented"},
		{"https://app.example.com/OAuth/Callback", "paths are case-sensitive"},
		{"https://app.example.com/oauth%2Fcallback", "percent-encoding is not decoded before comparing; decoding is where traversal gets in"},
		{"http://app.example.com/oauth/callback", "a different scheme"},
		{"https://app.example.com:443/oauth/callback", "an explicit default port is a different string"},
		{"", "the empty string must not match anything"},
		{" " + registered, "leading whitespace"},
	}

	for _, tc := range rejected {
		t.Run(tc.presented, func(t *testing.T) {
			if a.MatchesRedirectURI(tc.presented) {
				t.Errorf("MatchesRedirectURI(%q) = true, want false — %s", tc.presented, tc.why)
			}
		})
	}
}

// The control on the table above.
//
// A matcher that rejects everything passes every row of TestRedirectURIMatching
// IsExact while being completely broken, so the exact registered string must be
// asserted to match. This is the shape of vacuous check this project keeps
// finding.
func TestRedirectURIMatchingAcceptsTheRegisteredURI(t *testing.T) {
	a := app(registered)

	if !a.MatchesRedirectURI(registered) {
		t.Fatal("the exact registered URI does not match; the matcher rejects everything " +
			"and the rejection table above proves nothing")
	}

	// And with several registered, each one matches — so matching is not an
	// accident of there being exactly one entry.
	second := "https://app.example.com/other/callback"
	multi := app(registered, second)
	for _, uri := range []string{registered, second} {
		if !multi.MatchesRedirectURI(uri) {
			t.Errorf("MatchesRedirectURI(%q) = false with two registered URIs", uri)
		}
	}
}

// Post-logout redirects go through the same discipline: P1-10 redirects a
// browser too, so a loose match there is the same hole through a different
// door — and the two lists must not be confused with each other.
func TestPostLogoutRedirectMatchingIsSeparateAndExact(t *testing.T) {
	a := Application{
		Type:                   TypeWeb,
		RedirectURIs:           []string{registered},
		PostLogoutRedirectURIs: []string{"https://app.example.com/signed-out"},
	}

	if !a.MatchesPostLogoutRedirectURI("https://app.example.com/signed-out") {
		t.Error("the registered post-logout URI does not match")
	}
	if a.MatchesPostLogoutRedirectURI("https://app.example.com/signed-out/") {
		t.Error("a trailing slash matched a post-logout URI")
	}

	// The two lists are separate. A redirect URI must not work as a
	// post-logout URI just because it is registered on the same client.
	if a.MatchesPostLogoutRedirectURI(registered) {
		t.Error("a redirect URI matched as a post-logout redirect URI; the lists are separate")
	}
	if a.MatchesRedirectURI("https://app.example.com/signed-out") {
		t.Error("a post-logout URI matched as a redirect URI")
	}
}

// --- redirect validation ----------------------------------------------------

func TestValidateRedirectURIRejects(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		typ  Type
	}{
		{"relative", "/callback", TypeWeb},
		{"scheme-relative", "//app.example.com/cb", TypeWeb},
		{"empty", "", TypeWeb},
		{"whitespace only", "   ", TypeWeb},
		{"trailing whitespace", "https://app.example.com/cb ", TypeWeb},
		{"fragment", "https://app.example.com/cb#x", TypeWeb},
		{"bare fragment marker", "https://app.example.com/cb#", TypeWeb},
		{"wildcard in host", "https://*.example.com/cb", TypeWeb},
		{"wildcard in path", "https://app.example.com/*", TypeWeb},
		{"http on a public host", "http://app.example.com/cb", TypeWeb},
		{"no host", "https:///cb", TypeWeb},
		{"link-local", "https://169.254.169.254/cb", TypeWeb},
		{"link-local over http", "http://169.254.169.254/cb", TypeWeb},
		{"IPv6 link-local", "https://[fe80::1]/cb", TypeWeb},
		{"custom scheme on a web client", "com.example.app:/cb", TypeWeb},
		{"custom scheme on an SPA", "com.example.app:/cb", TypeSPA},
		{"custom scheme without a dot", "myapp:/cb", TypeNative},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateRedirectURI(tc.uri, tc.typ); err == nil {
				t.Errorf("ValidateRedirectURI(%q, %s) accepted it", tc.uri, tc.typ)
			}
		})
	}
}

func TestValidateRedirectURIAccepts(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		typ  Type
		want string
	}{
		{"https", "https://app.example.com/cb", TypeWeb, "https://app.example.com/cb"},
		{"https with query", "https://app.example.com/cb?tenant=a", TypeWeb, "https://app.example.com/cb?tenant=a"},
		{"https with port", "https://app.example.com:8443/cb", TypeWeb, "https://app.example.com:8443/cb"},
		{"loopback v4", "http://127.0.0.1:3000/cb", TypeNative, "http://127.0.0.1:3000/cb"},
		{"loopback v6", "http://[::1]:3000/cb", TypeNative, "http://[::1]:3000/cb"},
		{"localhost", "http://localhost:5173/cb", TypeSPA, "http://localhost:5173/cb"},
		{"private range is allowed", "https://10.0.4.2/cb", TypeWeb, "https://10.0.4.2/cb"},
		{"custom scheme on native", "com.example.app:/cb", TypeNative, "com.example.app:/cb"},

		// Canonicalisation: scheme and host lowercased, nothing else touched.
		{"scheme and host lowercased", "HTTPS://App.Example.COM/CB", TypeWeb, "https://app.example.com/CB"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateRedirectURI(tc.uri, tc.typ)
			if err != nil {
				t.Fatalf("ValidateRedirectURI(%q, %s): %v", tc.uri, tc.typ, err)
			}
			if got != tc.want {
				t.Errorf("canonical form = %q, want %q", got, tc.want)
			}
		})
	}
}

// Canonicalising at registration is what makes exact comparison predictable
// later, so the canonical form must be stable: validating it again changes
// nothing.
func TestCanonicalFormIsStable(t *testing.T) {
	for _, raw := range []string{
		"HTTPS://App.Example.COM/CB",
		"https://app.example.com/cb?a=1",
		"http://LOCALHOST:5173/cb",
	} {
		once, err := ValidateRedirectURI(raw, TypeNative)
		if err != nil {
			t.Fatalf("ValidateRedirectURI(%q): %v", raw, err)
		}
		twice, err := ValidateRedirectURI(once, TypeNative)
		if err != nil {
			t.Fatalf("re-validating %q: %v", once, err)
		}
		if once != twice {
			t.Errorf("canonical form is not stable: %q -> %q -> %q", raw, once, twice)
		}
	}
}

// --- grant types ------------------------------------------------------------

// Abuse case A-3, and the reason it is not merely a table lookup: a public
// client has nothing to authenticate with, so client_credentials is
// meaningless rather than disallowed. The error has to say that, because an
// administrator hitting it has a misunderstanding worth correcting.
func TestPublicClientsCannotUseClientCredentials(t *testing.T) {
	for _, typ := range []Type{TypeSPA, TypeNative} {
		t.Run(string(typ), func(t *testing.T) {
			err := ValidateGrantTypes(typ, []string{GrantClientCredentials})
			if err == nil {
				t.Fatalf("%s was allowed to use %s", typ, GrantClientCredentials)
			}
			if !strings.Contains(err.Error(), "no secret") {
				t.Errorf("the error does not explain why: %v", err)
			}
		})
	}
}

func TestGrantTypesByClientType(t *testing.T) {
	cases := []struct {
		typ    Type
		grants []string
		ok     bool
	}{
		{TypeWeb, []string{GrantAuthorizationCode, GrantRefreshToken}, true},
		{TypeWeb, []string{GrantClientCredentials}, true},
		{TypeAPI, []string{GrantClientCredentials}, true},
		{TypeAPI, []string{GrantAuthorizationCode}, false}, // no user to redirect
		{TypeSPA, []string{GrantAuthorizationCode, GrantRefreshToken}, true},
		{TypeNative, []string{GrantAuthorizationCode}, true},
		{TypeSAML, []string{GrantAuthorizationCode}, false}, // SAML takes no OIDC grant
		{TypeSAML, nil, true},
		{Type("nonsense"), []string{GrantAuthorizationCode}, false},
	}

	for _, tc := range cases {
		name := string(tc.typ) + "/" + strings.Join(tc.grants, "+")
		t.Run(name, func(t *testing.T) {
			err := ValidateGrantTypes(tc.typ, tc.grants)
			if tc.ok && err != nil {
				t.Errorf("ValidateGrantTypes(%s, %v) = %v, want nil", tc.typ, tc.grants, err)
			}
			if !tc.ok && err == nil {
				t.Errorf("ValidateGrantTypes(%s, %v) = nil, want an error", tc.typ, tc.grants)
			}
		})
	}
}

// The two grants PLAN/05 rules out permanently are refused for EVERY type, so
// no client type is a way around the rule P1-04's discovery document also
// enforces.
func TestForbiddenGrantsAreRefusedForEveryClientType(t *testing.T) {
	for _, typ := range Types {
		for _, grant := range []string{"implicit", "password"} {
			if err := ValidateGrantTypes(typ, []string{grant}); err == nil {
				t.Errorf("%s was allowed to use %q, which PLAN/05 rules out permanently", typ, grant)
			}
		}
	}
}

// --- client types -----------------------------------------------------------

// IsConfidential is deliberately not !IsPublic. SAML is neither, and writing
// it as a negation would hand a SAML client a secret the moment the type
// existed.
func TestSAMLIsNeitherPublicNorConfidential(t *testing.T) {
	if TypeSAML.IsPublic() {
		t.Error("saml reported as public")
	}
	if TypeSAML.IsConfidential() {
		t.Error("saml reported as confidential; it would then be issued a client secret")
	}
}

func TestPublicAndConfidentialCoverTheOIDCTypes(t *testing.T) {
	for _, typ := range []Type{TypeWeb, TypeAPI} {
		if !typ.IsConfidential() || typ.IsPublic() {
			t.Errorf("%s should be confidential", typ)
		}
	}
	for _, typ := range []Type{TypeSPA, TypeNative} {
		if !typ.IsPublic() || typ.IsConfidential() {
			t.Errorf("%s should be public", typ)
		}
	}
}

// --- whole-application validation -------------------------------------------

func TestApplicationValidate(t *testing.T) {
	valid := Application{
		Name:         "Billing",
		Type:         TypeWeb,
		GrantTypes:   []string{GrantAuthorizationCode},
		RedirectURIs: []string{registered},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid application was rejected: %v", err)
	}

	t.Run("authorization_code needs a redirect URI", func(t *testing.T) {
		a := valid
		a.RedirectURIs = nil
		if err := a.Validate(); err == nil {
			t.Error("accepted a client that can start a flow it cannot finish")
		}
	})

	t.Run("client_credentials needs none", func(t *testing.T) {
		a := Application{Name: "Sync", Type: TypeAPI, GrantTypes: []string{GrantClientCredentials}}
		if err := a.Validate(); err != nil {
			t.Errorf("an api client with no redirect URI was rejected: %v", err)
		}
	})

	t.Run("blank name", func(t *testing.T) {
		a := valid
		a.Name = "  "
		if err := a.Validate(); err == nil {
			t.Error("accepted a blank name")
		}
	})

	t.Run("post-logout URIs are validated too", func(t *testing.T) {
		a := valid
		a.PostLogoutRedirectURIs = []string{"http://app.example.com/out"}
		if err := a.Validate(); err == nil {
			t.Error("accepted an http post-logout URI on a public host")
		}
	})
}
