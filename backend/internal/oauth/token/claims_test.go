package token

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func subject() Subject {
	return Subject{
		Issuer:      "https://auth.example",
		Audience:    "https://auth.example",
		ClientID:    "11111111-1111-1111-1111-111111111111",
		ProjectID:   "55555555-5555-5555-5555-555555555555",
		OrgID:       "22222222-2222-2222-2222-222222222222",
		UserID:      "44444444-4444-4444-4444-444444444444",
		SessionID:   "33333333-3333-3333-3333-333333333333",
		AuthMethods: []string{"pwd"},
		AuthTime:    time.Now().Add(-time.Minute),
		Scope:       []string{"openid", "profile"},
	}
}

// P1-07 DoD item 5: tokens carry the exact claim set, and `iss`/`aud` are
// correct for the requesting client.
func TestIDTokenClaims(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	in := subject()

	claims, err := IDTokenClaims(in, now)
	if err != nil {
		t.Fatalf("IDTokenClaims: %v", err)
	}

	for _, required := range []string{"iss", "sub", "aud", "exp", "iat", "auth_time", "amr"} {
		if _, ok := claims[required]; !ok {
			t.Errorf("the id_token is missing the required claim %q", required)
		}
	}

	if claims["iss"] != in.Issuer {
		t.Errorf("iss = %v, want %q", claims["iss"], in.Issuer)
	}
	// An ID token is an assertion ABOUT the user, addressed to the client that
	// asked — so its audience is the client, not the resource.
	if claims["aud"] != in.ClientID {
		t.Errorf("aud = %v, want the client_id %q", claims["aud"], in.ClientID)
	}
	if claims["sub"] != in.UserID {
		t.Errorf("sub = %v, want the user id", claims["sub"])
	}
	if claims["exp"].(int64) != now.Add(IDTokenLifetime).Unix() {
		t.Errorf("exp is not iat + IDTokenLifetime")
	}
	if claims["auth_time"].(int64) != in.AuthTime.Unix() {
		t.Error("auth_time does not come from the session")
	}
}

// The nonce is echoed only when the client supplied one. An unsolicited nonce
// claim would be a value the client never chose and cannot check.
func TestNonceIsEchoedOnlyWhenSupplied(t *testing.T) {
	now := time.Now()

	without, _ := IDTokenClaims(subject(), now)
	if _, present := without["nonce"]; present {
		t.Error("a nonce claim appeared without one being requested")
	}

	in := subject()
	in.Nonce = "n-0S6_WzA2Mj"
	with, _ := IDTokenClaims(in, now)
	if with["nonce"] != in.Nonce {
		t.Errorf("nonce = %v, want the supplied value", with["nonce"])
	}
}

// P1-11 requires a session to record the factors actually used, precisely so
// this claim is true from the first release. An ID token that cannot say how
// somebody authenticated is worse than no ID token.
func TestIDTokenRequiresAuthMethods(t *testing.T) {
	in := subject()
	in.AuthMethods = nil

	if _, err := IDTokenClaims(in, time.Now()); err == nil {
		t.Error("an id_token was issued with no amr")
	}
}

func TestAmrComesFromTheSession(t *testing.T) {
	in := subject()
	in.AuthMethods = []string{"pwd", "otp"}

	claims, err := IDTokenClaims(in, time.Now())
	if err != nil {
		t.Fatalf("IDTokenClaims: %v", err)
	}

	amr, ok := claims["amr"].([]string)
	if !ok || len(amr) != 2 || amr[0] != "pwd" || amr[1] != "otp" {
		t.Errorf("amr = %v, want the session's methods", claims["amr"])
	}
}

// --- token substitution ----------------------------------------------------------

// Abuse case A-5. The two token types must be distinguishable by more than
// their claims, or a resource server accepting one will accept the other.
func TestAccessAndIDTokensAreNotInterchangeable(t *testing.T) {
	now := time.Now()
	in := subject()

	id, err := IDTokenClaims(in, now)
	if err != nil {
		t.Fatalf("IDTokenClaims: %v", err)
	}
	access, err := AccessTokenClaims(in, now)
	if err != nil {
		t.Fatalf("AccessTokenClaims: %v", err)
	}

	// Different audiences: the ID token addresses the client, the access token
	// addresses the resource.
	if id["aud"] == access["aud"] {
		t.Errorf("both tokens carry aud=%v; a resource server validating aud "+
			"would accept an id_token as a bearer credential", id["aud"])
	}
	if access["aud"] != in.Audience {
		t.Errorf("access token aud = %v, want the resource %q", access["aud"], in.Audience)
	}

	// The access token carries what a resource server needs and the ID token
	// does not carry a scope at all — an ID token is not a capability.
	if _, hasScope := id["scope"]; hasScope {
		t.Error("the id_token carries a scope claim; it is an assertion, not a capability")
	}
	if access["scope"] != "openid profile" {
		t.Errorf("access token scope = %v", access["scope"])
	}

	// amr belongs to the authentication assertion, not to the capability.
	if _, hasAMR := access["amr"]; hasAMR {
		t.Error("the access token carries amr; that belongs to the id_token")
	}
}

// P2-04 populates this; reserving it now means a consumer written against
// Phase 1 reads an empty object rather than a missing key, and does not have
// to change when roles arrive.
func TestTheRoleNamespaceIsReservedAndEmpty(t *testing.T) {
	in := subject()

	claims, err := AccessTokenClaims(in, time.Now())
	if err != nil {
		t.Fatalf("AccessTokenClaims: %v", err)
	}

	key := RoleClaimNamespace(in.ProjectID)
	if !strings.HasPrefix(key, "urn:authservice:iam:org:project:") {
		t.Errorf("the namespace does not match PLAN/08 Part A: %q", key)
	}

	roles, present := claims[key]
	if !present {
		t.Fatal("the role namespace is absent; a consumer reading it would have to handle a missing key")
	}
	if m, ok := roles.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("the reserved namespace is not an empty object: %v", roles)
	}
}

// --- client credentials --------------------------------------------------------------

// There is nobody else `sub` could be, and omitting it would make the token
// unattributable in a resource server's own audit log.
func TestClientCredentialsSubjectIsTheClient(t *testing.T) {
	in := subject()

	claims, err := ClientCredentialsClaims(in, time.Now())
	if err != nil {
		t.Fatalf("ClientCredentialsClaims: %v", err)
	}

	if claims["sub"] != in.ClientID {
		t.Errorf("sub = %v, want the client id", claims["sub"])
	}
	// No user means no user claims.
	for _, absent := range []string{"amr", "auth_time", "nonce"} {
		if _, present := claims[absent]; present {
			t.Errorf("a client_credentials token carries %q, which needs a user", absent)
		}
	}
}

// --- validation -------------------------------------------------------------------------

func TestClaimsRequireTheirInputs(t *testing.T) {
	cases := map[string]func(*Subject){
		"no issuer":       func(s *Subject) { s.Issuer = "" },
		"no client":       func(s *Subject) { s.ClientID = "" },
		"no subject":      func(s *Subject) { s.UserID = "" },
		"no organization": func(s *Subject) { s.OrgID = "" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := subject()
			mutate(&in)

			if _, err := AccessTokenClaims(in, time.Now()); err == nil {
				t.Error("AccessTokenClaims accepted it")
			}
			if _, err := IDTokenClaims(in, time.Now()); err == nil {
				t.Error("IDTokenClaims accepted it")
			}
		})
	}
}

// The lifetimes PLAN/09 asks for.
func TestLifetimesAreWithinThePlansBounds(t *testing.T) {
	if AccessTokenLifetime < 5*time.Minute || AccessTokenLifetime > 15*time.Minute {
		t.Errorf("AccessTokenLifetime is %s; PLAN/09 § Tokens & Keys asks for 5-15 minutes",
			AccessTokenLifetime)
	}
	if RefreshTokenLifetime <= AccessTokenLifetime {
		t.Error("the refresh token does not outlive the access token, which is its whole purpose")
	}
	if FamilyLifetime < RefreshTokenLifetime {
		t.Error("the family expires before its tokens; continuous refreshing would be capped " +
			"by the wrong bound")
	}
}

func TestClaimsEncode(t *testing.T) {
	claims, err := AccessTokenClaims(subject(), time.Now())
	if err != nil {
		t.Fatalf("AccessTokenClaims: %v", err)
	}

	encoded, err := claims.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var round map[string]any
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatalf("the encoded claims are not valid JSON: %v", err)
	}
	if round["iss"] != "https://auth.example" {
		t.Errorf("iss did not survive encoding: %v", round["iss"])
	}
}
