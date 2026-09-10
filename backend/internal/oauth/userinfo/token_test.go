package userinfo

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// --- the Authorization header ------------------------------------------------

func TestBearerToken(t *testing.T) {
	cases := map[string]struct {
		header string
		want   string
		ok     bool
	}{
		"a bearer token":        {"Bearer abc.def.ghi", "abc.def.ghi", true},
		"lowercase scheme":      {"bearer abc.def.ghi", "abc.def.ghi", true},
		"mixed-case scheme":     {"BeArEr abc.def.ghi", "abc.def.ghi", true},
		"trailing whitespace":   {"Bearer abc.def.ghi   ", "abc.def.ghi", true},
		"no header":             {"", "", false},
		"no scheme":             {"abc.def.ghi", "", false},
		"the wrong scheme":      {"Basic dXNlcjpwYXNz", "", false},
		"scheme with no token":  {"Bearer", "", false},
		"scheme and only space": {"Bearer   ", "", false},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := BearerToken(c.header)
			if ok != c.ok || got != c.want {
				t.Errorf("BearerToken(%q) = (%q, %v), want (%q, %v)", c.header, got, ok, c.want, c.ok)
			}
		})
	}
}

// A credential containing a space is not one, and taking the first piece of it
// would hand the verifier a truncated token — which fails with a confusing
// signature error rather than a clear one.
func TestATokenWithAnInternalSpaceIsNotSplit(t *testing.T) {
	got, ok := BearerToken("Bearer abc def")

	if !ok {
		t.Fatal("the header was rejected outright")
	}
	if got != "abc def" {
		t.Errorf("credential = %q; the header was split at the wrong space", got)
	}
}

// --- claim validation ----------------------------------------------------------

const testIssuer = "https://auth.example"

func accessToken(now time.Time) AccessToken {
	return AccessToken{
		Issuer:    testIssuer,
		Subject:   "44444444-4444-4444-4444-444444444444",
		Audience:  testIssuer,
		ExpiresAt: now.Add(10 * time.Minute).Unix(),
		IssuedAt:  now.Unix(),
		ClientID:  "11111111-1111-1111-1111-111111111111",
		OrgID:     "22222222-2222-2222-2222-222222222222",
		SessionID: "33333333-3333-3333-3333-333333333333",
		Scope:     "openid profile email",
	}
}

func payloadOf(t *testing.T, token AccessToken) []byte {
	t.Helper()
	raw, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	return raw
}

func TestAValidTokenPasses(t *testing.T) {
	now := time.Now()

	got, reason, err := Validate(payloadOf(t, accessToken(now)), testIssuer, now)
	if err != nil {
		t.Fatalf("Validate: %v (%s)", err, reason)
	}
	if got.Subject != accessToken(now).Subject {
		t.Errorf("subject = %q", got.Subject)
	}
	if len(got.Scopes()) != 3 {
		t.Errorf("scopes = %v", got.Scopes())
	}
}

func TestClaimValidation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	cases := map[string]func(*AccessToken){
		// Signed by a key this service holds, but minted for another
		// deployment. The signature alone would not notice — a key set can
		// legitimately be shared across issuers.
		"another issuer": func(a *AccessToken) { a.Issuer = "https://other.example" },
		"no issuer":      func(a *AccessToken) { a.Issuer = "" },

		// P1-07 sets an ID token's aud to the CLIENT. This is the second half
		// of refusing an ID token here, after `typ`.
		"the client as audience": func(a *AccessToken) { a.Audience = a.ClientID },
		"no audience":            func(a *AccessToken) { a.Audience = "" },

		"expired":   func(a *AccessToken) { a.ExpiresAt = now.Add(-time.Second).Unix() },
		"no expiry": func(a *AccessToken) { a.ExpiresAt = 0 },
		"from the future": func(a *AccessToken) {
			a.IssuedAt = now.Add(10 * time.Minute).Unix()
		},

		"no subject":      func(a *AccessToken) { a.Subject = "" },
		"no organization": func(a *AccessToken) { a.OrgID = "" },

		// No session means client_credentials, where `sub` is the client
		// rather than a person. Answering would describe an application as if
		// it were a user.
		"no session": func(a *AccessToken) { a.SessionID = "" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			token := accessToken(now)
			mutate(&token)

			_, reason, err := Validate(payloadOf(t, token), testIssuer, now)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Validate accepted it: err = %v", err)
			}
			if reason == "" {
				t.Error("no reason was recorded, so the log cannot say why")
			}
		})
	}
}

// Exactly at expiry is expired. A token whose `exp` is now has no life left,
// and `>=` rather than `>` is the difference between honouring it for a second
// and not.
func TestExpiryIsExclusive(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	token := accessToken(now)
	token.ExpiresAt = now.Unix()

	if _, _, err := Validate(payloadOf(t, token), testIssuer, now); !errors.Is(err, ErrInvalidToken) {
		t.Error("a token expiring exactly now was accepted")
	}
}

// Skew is tolerated on `iat` and NOT on `exp`. Leeway on expiry would extend
// the life of every access token this service issues by the tolerance.
func TestSkewIsToleratedOnIssuanceOnly(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	slightlyAhead := accessToken(now)
	slightlyAhead.IssuedAt = now.Add(10 * time.Second).Unix()
	if _, reason, err := Validate(payloadOf(t, slightlyAhead), testIssuer, now); err != nil {
		t.Errorf("a token issued 10s ahead was refused: %v (%s)", err, reason)
	}

	justExpired := accessToken(now)
	justExpired.ExpiresAt = now.Add(-time.Second).Unix()
	if _, _, err := Validate(payloadOf(t, justExpired), testIssuer, now); !errors.Is(err, ErrInvalidToken) {
		t.Error("a token that expired one second ago was accepted; skew leeway " +
			"on exp extends every token's life")
	}
}

func TestAPayloadThatIsNotJSONIsRefused(t *testing.T) {
	if _, _, err := Validate([]byte("not json"), testIssuer, time.Now()); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

// --- the scope check, and its position ------------------------------------------

func TestAMissingOpenIDScopeIsInsufficientScope(t *testing.T) {
	now := time.Now()
	token := accessToken(now)
	token.Scope = "profile email"

	_, _, err := Validate(payloadOf(t, token), testIssuer, now)

	if !errors.Is(err, ErrInsufficientScope) {
		t.Errorf("err = %v, want ErrInsufficientScope", err)
	}
	if errors.Is(err, ErrInvalidToken) {
		t.Error("a valid token without the scope was reported as invalid, which " +
			"sends a working client into a re-authentication loop that cannot help it")
	}
}

// The ordering that matters: scope is checked LAST, so a token that is invalid
// AND lacks the scope is reported as invalid. The other way round would answer
// the more informative insufficient_scope to a forged token.
func TestAnInvalidTokenIsNeverReportedAsInsufficientScope(t *testing.T) {
	now := time.Now()

	for name, mutate := range map[string]func(*AccessToken){
		"expired":        func(a *AccessToken) { a.ExpiresAt = now.Add(-time.Hour).Unix() },
		"another issuer": func(a *AccessToken) { a.Issuer = "https://other.example" },
		"no session":     func(a *AccessToken) { a.SessionID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			token := accessToken(now)
			token.Scope = "profile" // also lacks openid
			mutate(&token)

			_, _, err := Validate(payloadOf(t, token), testIssuer, now)

			if errors.Is(err, ErrInsufficientScope) {
				t.Error("an unusable token was answered with insufficient_scope, " +
					"which tells the caller its token was otherwise fine")
			}
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestScopesSplitOnWhitespace(t *testing.T) {
	token := AccessToken{Scope: "openid  profile\temail "}

	got := token.Scopes()
	if len(got) != 3 || got[0] != "openid" || got[2] != "email" {
		t.Errorf("Scopes() = %v", got)
	}
}
