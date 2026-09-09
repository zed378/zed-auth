// Package client holds the rules an OIDC client is registered under: what a
// redirect URI may be, which grants a client type may ask for, and how a
// client secret is made and checked.
//
// Everything here is a pure function of its arguments. The store lives beside
// it in store.go; the HTTP surface is P1-18's and is deliberately not here.
//
// The rule that matters most is redirect matching, and it is one line:
// compare the strings. Every open-redirect vulnerability in this class comes
// from an implementation that did something more clever than that — matched a
// prefix, normalised before comparing, resolved dot segments, allowed a
// wildcard. `PLAN/09` § Protection Against Common Attacks says exact match,
// and the surrounding tests exist to keep it that way when somebody
// reasonably suggests being more forgiving.
//
// Specification: MEMORY/specs/P1-05-application-registration.md.
package client

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
)

// Type is the client type from PLAN/04 § applications.
type Type string

const (
	// TypeWeb is a confidential server-side application. It can keep a secret
	// because the secret never leaves its server.
	TypeWeb Type = "web"

	// TypeNative is a mobile or desktop application. Public: whatever is
	// shipped in the binary is readable by anyone who has the binary.
	TypeNative Type = "native"

	// TypeSPA is a browser application. Public for the same reason, more
	// obviously so — its source is served to the client.
	TypeSPA Type = "spa"

	// TypeAPI is a service with no user present. Confidential, and the only
	// type whose primary grant is client_credentials.
	TypeAPI Type = "api"

	// TypeSAML is a SAML service provider. It is a record here and nothing
	// more until Phase 4; no OIDC grant applies to it.
	TypeSAML Type = "saml"
)

// Types is every valid type, for validation and for tests that must cover all
// of them rather than the ones somebody remembered.
var Types = []Type{TypeWeb, TypeNative, TypeSPA, TypeAPI, TypeSAML}

// IsPublic reports whether the client cannot keep a secret.
//
// This is not a property of how the client is deployed but of where its code
// runs: a browser application and a shipped binary both hand their contents to
// the user. PKCE exists because of this, and a public client with a secret is
// a false sense of security rather than a weak one.
func (t Type) IsPublic() bool {
	return t == TypeSPA || t == TypeNative
}

// IsConfidential reports whether the client can hold a secret.
//
// Not simply !IsPublic: TypeSAML is neither, because it does not participate
// in OIDC client authentication at all. Writing it as a negation would quietly
// give SAML clients a secret the moment somebody added the type.
func (t Type) IsConfidential() bool {
	return t == TypeWeb || t == TypeAPI
}

func (t Type) Valid() bool { return slices.Contains(Types, t) }

// --- grant types ------------------------------------------------------------

const (
	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
	GrantClientCredentials = "client_credentials"
)

// forbiddenGrants are refused for every client type, permanently.
//
// The same two PLAN/05 rules out that P1-04's discovery document refuses to
// advertise. Both refusals exist because a grant that is advertised or
// registerable is a grant somebody builds against.
var forbiddenGrants = map[string]string{
	"implicit": "deprecated in OAuth 2.1 (PLAN/05 § Supported Grant Types)",
	"password": "resource owner password credentials are not supported (PLAN/05)",
}

// allowedGrants is what each type may ask for.
//
// The interesting entries are the omissions. A public client has no
// client_credentials because that grant is the client authenticating as
// itself, and a client with no secret has nothing to authenticate with — the
// combination is not merely disallowed, it is meaningless. An `api` client has
// no authorization_code because there is no user at a browser to redirect.
var allowedGrants = map[Type][]string{
	TypeWeb:    {GrantAuthorizationCode, GrantRefreshToken, GrantClientCredentials},
	TypeAPI:    {GrantClientCredentials, GrantRefreshToken},
	TypeSPA:    {GrantAuthorizationCode, GrantRefreshToken},
	TypeNative: {GrantAuthorizationCode, GrantRefreshToken},
	TypeSAML:   {},
}

// ValidateGrantTypes reports whether the requested grants suit the type.
//
// Rejects loudly rather than filtering silently: an administrator who asked
// for client_credentials on an SPA has a misunderstanding about their own
// architecture, and quietly dropping the grant leaves them to discover it when
// the flow fails in production.
func ValidateGrantTypes(t Type, grants []string) error {
	if !t.Valid() {
		return fmt.Errorf("unknown client type %q", t)
	}

	allowed := allowedGrants[t]

	for _, grant := range grants {
		if reason, forbidden := forbiddenGrants[grant]; forbidden {
			return fmt.Errorf("grant type %q is never supported: %s", grant, reason)
		}
		if !slices.Contains(allowed, grant) {
			if t.IsPublic() && grant == GrantClientCredentials {
				return fmt.Errorf(
					"a %s client cannot use %q: that grant is the client authenticating "+
						"as itself, and a public client has no secret to authenticate with. "+
						"Use authorization_code with PKCE",
					t, grant)
			}
			if len(allowed) == 0 {
				return fmt.Errorf("a %s client takes no OIDC grant types; %q was requested", t, grant)
			}
			return fmt.Errorf("a %s client cannot use %q; allowed: %s",
				t, grant, strings.Join(allowed, ", "))
		}
	}

	return nil
}

// --- redirect URIs ----------------------------------------------------------

// ValidateRedirectURI checks a URI at REGISTRATION time.
//
// This is the permissive end of the system and it is where every check
// belongs, because the matching end (MatchesRedirectURI) does nothing but
// compare strings. Anything not caught here is registered forever.
//
// The returned URI is the canonical form to store: scheme and host lowercased,
// nothing else touched. Normalising at registration and comparing exactly
// afterwards is the whole design — normalising at comparison time is how a
// matcher starts accepting inputs nobody intended.
func ValidateRedirectURI(raw string, t Type) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("redirect URI is empty")
	}
	if raw != strings.TrimSpace(raw) {
		return "", fmt.Errorf("redirect URI %q has leading or trailing whitespace", raw)
	}

	// Before parsing. A wildcard parses fine as a path character, so a URI
	// containing one would be stored and then matched literally — the
	// registrant believing it matches a family of URLs and it matching none.
	if strings.Contains(raw, "*") {
		return "", fmt.Errorf(
			"redirect URI %q contains a wildcard. Redirect URIs are matched by exact "+
				"string comparison, so a wildcard would be matched literally and never "+
				"match anything. Register each URI in full", raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("redirect URI %q is not a valid URI: %w", raw, err)
	}

	if parsed.Scheme == "" {
		return "", fmt.Errorf(
			"redirect URI %q is relative; it must be absolute, because a relative URI "+
				"resolves against whatever page the browser happened to be on", raw)
	}

	// url.Parse keeps the fragment separately, and Fragment != "" only catches
	// "#something". A bare trailing "#" is also a fragment and is also wrong.
	if parsed.Fragment != "" || strings.Contains(raw, "#") {
		return "", fmt.Errorf(
			"redirect URI %q contains a fragment. A fragment is never sent to the "+
				"server, so it cannot take part in the match", raw)
	}

	scheme := strings.ToLower(parsed.Scheme)

	switch {
	case scheme == "https":
		// The normal case.

	case scheme == "http":
		if !isLoopback(parsed.Hostname()) {
			return "", fmt.Errorf(
				"redirect URI %q uses http. An authorization code sent over cleartext is "+
					"a code anyone on the path keeps. Use https, or a loopback address "+
					"(127.0.0.1, [::1], localhost) for local development", raw)
		}

	default:
		// RFC 8252 private-use scheme, e.g. com.example.app:/callback. Only a
		// native client can receive one — a browser has no way to dispatch it.
		if t != TypeNative {
			return "", fmt.Errorf(
				"redirect URI %q uses the custom scheme %q, which only a native client "+
					"can receive; this client is %s", raw, scheme, t)
		}
		if !strings.Contains(scheme, ".") {
			return "", fmt.Errorf(
				"custom scheme %q in %q should be a reverse-DNS name the app owns "+
					"(RFC 8252 § 7.1), e.g. com.example.app", scheme, raw)
		}
		return canonical(parsed, scheme), nil
	}

	if parsed.Hostname() == "" {
		return "", fmt.Errorf("redirect URI %q has no host", raw)
	}

	// A code delivered to a link-local address is a code delivered somewhere
	// the registrant cannot observe and does not control. 169.254.169.254 is
	// the cloud metadata service on every major provider.
	//
	// Private ranges (10/8, 192.168/16) are deliberately NOT refused: a
	// self-hosted deployment redirecting to an intranet application is a
	// legitimate configuration, and registration is already an authenticated,
	// audited, admin-only action.
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", fmt.Errorf(
				"redirect URI %q points at a link-local address. Nothing a client owns "+
					"lives there, and 169.254.169.254 is the cloud metadata service", raw)
		}
	}

	return canonical(parsed, scheme), nil
}

// canonical lowercases scheme and host and leaves everything else alone.
//
// Path, query and percent-encoding are untouched on purpose. Lowercasing a
// path would make two different resources compare equal; decoding
// percent-escapes would make %2F and / compare equal, which is exactly the
// kind of helpfulness that turns into a traversal.
func canonical(u *url.URL, scheme string) string {
	out := *u
	out.Scheme = scheme
	out.Host = strings.ToLower(u.Host)
	return out.String()
}

// isLoopback reports whether the host is a loopback address.
//
// Lowercases first: hostnames are case-insensitive, and the raw comparison
// refused a perfectly valid `http://LOCALHOST:5173/cb`. Caught by the test
// asserting the canonical form is stable — validate, canonicalise, validate
// again — which is a property worth having a test for precisely because this
// kind of asymmetry is invisible when you only ever try the lowercase form.
func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// --- matching ---------------------------------------------------------------

// Application is a registered OIDC client.
//
// The secret is not a field. There is deliberately no struct here that a read
// path could serialise a secret out of, because the way secrets leak is that
// somebody adds a JSON tag to a field that was only ever meant to be internal.
type Application struct {
	ID        string // doubles as the client_id (PLAN/04)
	OrgID     string
	ProjectID string
	Name      string
	Type      Type

	RedirectURIs           []string
	PostLogoutRedirectURIs []string
	GrantTypes             []string
}

// MatchesRedirectURI reports whether the presented URI is registered.
//
// Exact string comparison against the stored canonical form, and nothing else.
// No normalisation, no trailing-slash tolerance, no case folding, no dot
// segment resolution, no prefix.
//
// Every one of those would be a kindness, and every one of them is how the
// open-redirect class of bug gets in:
//
//   - prefix matching accepts https://app.example/cb.attacker.net
//   - normalising accepts https://app.example/a/../cb for a registered /cb
//   - decoding accepts %2e%2e%2f
//   - trailing-slash tolerance is harmless alone and is the precedent that
//     makes the next tolerance look reasonable
//
// If an integrator's framework rewrites the URI before sending it, the fix is
// to register the rewritten form. That is a five-minute conversation; an open
// redirect on an identity provider is not.
func (a Application) MatchesRedirectURI(presented string) bool {
	for _, registered := range a.RedirectURIs {
		if registered == presented {
			return true
		}
	}
	return false
}

// MatchesPostLogoutRedirectURI applies the same rule to RP-initiated logout.
//
// Separate list, same discipline: P1-10's logout endpoint redirects the
// browser too, so a loose match there is the same vulnerability reached by a
// different door.
func (a Application) MatchesPostLogoutRedirectURI(presented string) bool {
	for _, registered := range a.PostLogoutRedirectURIs {
		if registered == presented {
			return true
		}
	}
	return false
}

// Validate checks an application as a whole, at registration.
func (a Application) Validate() error {
	if !a.Type.Valid() {
		return fmt.Errorf("unknown client type %q", a.Type)
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("application name is required")
	}
	if err := ValidateGrantTypes(a.Type, a.GrantTypes); err != nil {
		return err
	}

	for _, uri := range a.RedirectURIs {
		if _, err := ValidateRedirectURI(uri, a.Type); err != nil {
			return err
		}
	}
	for _, uri := range a.PostLogoutRedirectURIs {
		if _, err := ValidateRedirectURI(uri, a.Type); err != nil {
			return fmt.Errorf("post-logout %w", err)
		}
	}

	// A client that can start a flow it cannot finish is a configuration error
	// that surfaces as a failed login rather than as a failed registration.
	if slices.Contains(a.GrantTypes, GrantAuthorizationCode) && len(a.RedirectURIs) == 0 {
		return fmt.Errorf(
			"a client using %s needs at least one redirect URI; without one the "+
				"authorization endpoint has nowhere to send the code", GrantAuthorizationCode)
	}

	return nil
}
