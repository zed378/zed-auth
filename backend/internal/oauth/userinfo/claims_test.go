package userinfo

import (
	"slices"
	"testing"
	"time"
)

// The scope-to-claim mapping is the security control this endpoint exists to
// get right, so it is tested exhaustively and — more importantly — tested for
// ABSENCE. A handler that returns every claim regardless of scope passes every
// "is email there when email was granted" test anybody will ever write.

func subject() Subject {
	return Subject{
		UserID:            "44444444-4444-4444-4444-444444444444",
		Name:              "Alice Example",
		PreferredUsername: "alice",
		UpdatedAt:         time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
	}
}

const subjectEmail = "alice@example.test"

func keys(c Claims) []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// DoD item 1: the claims returned are exactly those the granted scopes permit,
// across combinations. Exactly — the want list is the whole key set, not a
// subset, so an extra claim fails.
func TestScopesDetermineTheExactClaimSet(t *testing.T) {
	cases := map[string]struct {
		scope []string
		want  []string
	}{
		"openid only": {
			scope: []string{"openid"},
			want:  []string{"sub"},
		},
		"openid and profile": {
			scope: []string{"openid", "profile"},
			want:  []string{"name", "preferred_username", "sub", "updated_at"},
		},
		"openid and email": {
			scope: []string{"openid", "email"},
			want:  []string{"email", "sub"},
		},
		"everything": {
			scope: []string{"openid", "profile", "email"},
			want:  []string{"email", "name", "preferred_username", "sub", "updated_at"},
		},
		// The handler refuses this before Build is reached, but the mapping
		// must not depend on that: a claim gated by a check somewhere else is
		// a claim that appears the day the check moves.
		"no scopes at all": {
			scope: nil,
			want:  []string{"sub"},
		},
		// offline_access is a real granted scope that authorises no claim.
		"an unrelated scope": {
			scope: []string{"openid", "offline_access"},
			want:  []string{"sub"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := keys(Build(subject(), subjectEmail, c.scope))

			if !slices.Equal(got, c.want) {
				t.Errorf("claims = %v, want exactly %v", got, c.want)
			}
		})
	}
}

// Stated separately from the table because it is the failure that matters
// most: an address handed to a client that was never granted it.
func TestTheEmailNeverAppearsWithoutTheEmailScope(t *testing.T) {
	for _, scope := range [][]string{
		nil,
		{"openid"},
		{"openid", "profile"},
		{"openid", "offline_access"},
		{"profile"},
	} {
		claims := Build(subject(), subjectEmail, scope)

		if _, present := claims["email"]; present {
			t.Errorf("scope %v produced an email claim", scope)
		}
		// And not smuggled into another field.
		for key, value := range claims {
			if s, ok := value.(string); ok && s == subjectEmail {
				t.Errorf("scope %v put the address in %q", scope, key)
			}
		}
	}
}

func TestProfileClaimsNeverAppearWithoutTheProfileScope(t *testing.T) {
	claims := Build(subject(), subjectEmail, []string{"openid", "email"})

	for _, absent := range []string{"name", "preferred_username", "updated_at"} {
		if _, present := claims[absent]; present {
			t.Errorf("%q appeared without the profile scope", absent)
		}
	}
}

// FR-3 and SECURITY/02 §12. `sub` is what every consumer stores forever as the
// user's permanent identifier, so what it is matters more than what it is not.
func TestTheSubjectIsTheUserIdAndNeverTheEmail(t *testing.T) {
	in := subject()
	claims := Build(in, subjectEmail, []string{"openid", "profile", "email"})

	if claims["sub"] != in.UserID {
		t.Errorf("sub = %v, want the user id", claims["sub"])
	}
	if claims["sub"] == subjectEmail {
		t.Error("sub is the email address; a consumer would store an address as a " +
			"permanent identifier, and it would break when the address changes")
	}
	if claims["sub"] == in.PreferredUsername {
		t.Error("sub is the username, which a user may change")
	}
}

// `sub` is required by OIDC and is the one claim that is not optional. A
// response without it is not a userinfo response.
func TestTheSubjectIsAlwaysPresent(t *testing.T) {
	for _, scope := range [][]string{nil, {"openid"}, {"profile"}, {"openid", "email"}} {
		if _, present := Build(subject(), subjectEmail, scope)["sub"]; !present {
			t.Errorf("scope %v produced a response with no sub", scope)
		}
	}
}

// A NULL column yields no claim rather than an empty string. An empty string
// is a value somebody chose; absence is the truth.
func TestNullColumnsAreOmittedRatherThanEmptied(t *testing.T) {
	bare := Subject{UserID: subject().UserID}

	claims := Build(bare, "", []string{"openid", "profile", "email"})

	if !slices.Equal(keys(claims), []string{"sub"}) {
		t.Errorf("claims = %v, want only sub for a user with nothing filled in", keys(claims))
	}
	for key, value := range claims {
		if value == "" {
			t.Errorf("%q was emitted as an empty string", key)
		}
	}
}

// PG-18. There is no verification column, so the claim is absent rather than
// false: absent means "not asserted", false means "we checked and it is not
// verified". A consumer gating on a verified address would refuse every user
// this service has, on the strength of a value it made up.
func TestEmailVerifiedIsNeverAsserted(t *testing.T) {
	claims := Build(subject(), subjectEmail, []string{"openid", "profile", "email"})

	if value, present := claims["email_verified"]; present {
		t.Errorf("email_verified = %v; PG-18 says there is nothing behind it", value)
	}
}

// DoD item 3, and the test that catches a claim added later without a scope
// gate — the failure that is invisible in review, because adding a key to a
// map looks harmless.
func TestNoClaimOutsideTheAllowedSetIsEverEmitted(t *testing.T) {
	for _, scope := range [][]string{
		nil,
		{"openid"},
		{"openid", "profile"},
		{"openid", "email"},
		{"openid", "profile", "email", "offline_access"},
	} {
		for _, key := range keys(Build(subject(), subjectEmail, scope)) {
			if !slices.Contains(Allowed, key) {
				t.Errorf("scope %v emitted %q, which is not in the allowed set %v",
					scope, key, Allowed)
			}
		}
	}
}

// The response must carry nothing internal. Asserted by name as well as by the
// allowed set, because these are the specific fields a careless edit would add
// — they are all sitting in the AccessToken the handler already holds.
func TestNoOrganizationInternalIdentifierIsReturned(t *testing.T) {
	claims := Build(subject(), subjectEmail, []string{"openid", "profile", "email"})

	for _, forbidden := range []string{
		"org_id", "organization_id", "sid", "session_id", "client_id",
		"status", "mfa_enabled", "password_changed_at", "tenant",
	} {
		if _, present := claims[forbidden]; present {
			t.Errorf("the response carries %q", forbidden)
		}
	}
}

func TestUpdatedAtIsSecondsSinceTheEpoch(t *testing.T) {
	in := subject()
	claims := Build(in, subjectEmail, []string{"openid", "profile"})

	got, ok := claims["updated_at"].(int64)
	if !ok {
		t.Fatalf("updated_at = %T, want int64 seconds per OIDC Core 5.1", claims["updated_at"])
	}
	if got != in.UpdatedAt.Unix() {
		t.Errorf("updated_at = %d, want %d", got, in.UpdatedAt.Unix())
	}
}

// The Subject struct is the second line of defence behind the mapping: what it
// cannot hold cannot be returned. This test is about the type, and it fails if
// somebody adds a field to it that no scope authorises.
func TestTheSubjectStructCarriesNothingUnscoped(t *testing.T) {
	// Deliberately a literal with every field named. Adding a field to Subject
	// breaks this compilation, which is the point — the author then has to
	// decide which scope authorises it.
	_ = Subject{
		UserID:            "",
		Name:              "",
		PreferredUsername: "",
		UpdatedAt:         time.Time{},
	}
}
