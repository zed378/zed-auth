// Package userinfo implements GET and POST /oauth/userinfo — the endpoint a
// consumer calls to find out who is holding an access token.
//
// The whole job of this endpoint is to hand personal data to a caller, so
// every decision in it is about how much. The rule it is built around is
// stated as a negative: return exactly the claims the granted scopes
// authorise, and nothing else. Every extra field is a field that leaks through
// every consumer that stores the response.
//
// Specification: MEMORY/specs/P1-08-userinfo.md.
package userinfo

import (
	"slices"
	"time"
)

// Claims is the response body.
type Claims map[string]any

// Subject is the user, as this endpoint needs them.
//
// Deliberately not authn.User: that type carries `Status`, `MFAEnabled` and
// `PasswordChangedAt`, none of which any scope authorises, and a struct that
// holds them is a struct somebody eventually ranges over. What cannot be in
// the struct cannot be in the response.
type Subject struct {
	UserID string

	// Optional columns. Empty means the column is NULL, and a NULL column
	// yields no claim rather than an empty string — an empty string is a value
	// somebody chose.
	Name              string
	PreferredUsername string

	UpdatedAt time.Time
}

// Scope names, from P1-06's supported set.
const (
	ScopeOpenID  = "openid"
	ScopeProfile = "profile"
	ScopeEmail   = "email"
)

// Build assembles the response for one subject and one set of granted scopes.
//
// Pure, and takes the scopes rather than reading them from anywhere, so the
// mapping can be tested exhaustively without a token, a database, or a
// request. The mapping is the security control; the plumbing around it is not.
func Build(s Subject, email string, scope []string) Claims {
	claims := Claims{
		// `sub` is always present and is the user's UUID.
		//
		// Not sequential, not guessable, and NOT the email address —
		// docs/SECURITY/02 §12 names all three. A `sub` that is an email is a
		// permanent link between an identifier a consumer stores forever and a
		// value the user may change, and it hands every consumer a working
		// address list.
		"sub": s.UserID,
	}

	if slices.Contains(scope, ScopeProfile) {
		// Omitted when NULL rather than emitted as "". A consumer that shows
		// `name` would render an empty heading; one that checks presence gets
		// the truth.
		if s.Name != "" {
			claims["name"] = s.Name
		}
		if s.PreferredUsername != "" {
			claims["preferred_username"] = s.PreferredUsername
		}
		if !s.UpdatedAt.IsZero() {
			// Seconds since the epoch, per OIDC Core 5.1. A consumer uses it
			// to decide whether its cached copy is stale.
			claims["updated_at"] = s.UpdatedAt.Unix()
		}
	}

	if slices.Contains(scope, ScopeEmail) && email != "" {
		claims["email"] = email

		// `email_verified` is deliberately ABSENT — see PG-18.
		//
		// OIDC's email scope defines both claims and docs/PLAN/04 § users has no
		// verification column, so there is nothing behind it. Returning
		// `false` would be worse than omitting: absent means "not asserted",
		// which is true, while false means "we checked and it is not
		// verified", which we did not. A consumer gating on a verified email
		// would then refuse every user forever, on the strength of a value
		// this service made up.
	}

	return claims
}

// Allowed is every claim name this endpoint may ever emit.
//
// Enumerated so a test can assert that a response contains nothing outside it.
// That test is what catches a claim added later without a scope gate — the
// failure mode the card's third DoD item is about, and the one that is
// invisible in review because adding a field to a map looks harmless.
var Allowed = []string{"sub", "name", "preferred_username", "updated_at", "email"}
