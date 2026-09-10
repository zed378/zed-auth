package token

import (
	"encoding/json"
	"fmt"
	"time"
)

// Claim assembly for the two JWTs this endpoint issues.
//
// The ID token and the access token are different things and are kept
// different on purpose. An ID token is an assertion ABOUT the user, addressed
// to the client that asked; an access token is a capability AT a resource
// server. Conflating them is abuse case A-5, and the separation is carried by
// two fields rather than by convention:
//
//	              typ         aud
//	ID token      JWT         the client_id
//	access token  at+jwt      the resource
//
// `typ: at+jwt` is RFC 9068's media type for access tokens, and it exists
// precisely so a resource server can refuse an ID token presented as a bearer
// credential — and so a client can refuse the reverse.

// Lifetimes.
//
// docs/PLAN/09 § Tokens & Keys asks for "short-lived access tokens (5-15 minutes)".
// Ten is the middle of that and the number to argue with: shorter multiplies
// refresh traffic on the endpoint docs/PLAN/12 already calls the most
// latency-sensitive; longer widens the window in which a stolen access token
// is useful, and an access token cannot be revoked because nothing looks it up
// (docs/PLAN/04: "storing them would add a lookup to the hottest path in the system
// for no security gain"). Ten minutes is where those meet.
const (
	AccessTokenLifetime = 10 * time.Minute

	// The ID token is an assertion about an authentication that just happened.
	// It is consumed once, immediately, to establish a local session — so it
	// does not need to outlive the exchange by much.
	IDTokenLifetime = 5 * time.Minute

	// RefreshTokenLifetime is deliberately shorter than it could be.
	//
	// Phase 1 does NOT rotate refresh tokens — P3-06 owns that — so a stolen
	// one is replayable for its whole life. Fourteen days is a working
	// compromise while that is true, and P3-06 can lengthen it once reuse
	// detection makes theft detectable.
	RefreshTokenLifetime = 14 * 24 * time.Hour

	// FamilyLifetime caps the whole rotation lineage, so that continuous
	// refreshing cannot extend one authentication forever. Enforced from
	// Phase 1 even though nothing rotates yet, because the column exists and
	// leaving it unset would make P3-06 a schema change rather than a
	// behaviour change.
	FamilyLifetime = 90 * 24 * time.Hour
)

// RoleClaimNamespace builds the claim key docs/PLAN/08 Part A specifies.
//
// Reserved here and populated by P2-04. Reserving it now means the claim
// appears in the token's shape from the first release, so a consumer written
// against Phase 1 does not have to change when roles arrive — it reads an
// empty object today and a populated one later.
func RoleClaimNamespace(projectID string) string {
	return "urn:authservice:iam:org:project:" + projectID + ":roles"
}

// Claims is one token's payload.
type Claims map[string]any

// IDTokenClaims assembles the ID token.
//
// `amr` comes from the session and nothing else. P1-11 requires a session to
// record the factors actually used, precisely so this claim is true from the
// first release: Phase 3's step-up authentication reads it, and a value that
// was never trustworthy cannot be made trustworthy later.
func IDTokenClaims(in Subject, now time.Time) (Claims, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	if len(in.AuthMethods) == 0 {
		// An ID token asserts how somebody authenticated. Issuing one that
		// cannot say is worse than not issuing it.
		return nil, fmt.Errorf("token: cannot issue an id_token with no auth_methods")
	}

	claims := Claims{
		"iss":       in.Issuer,
		"sub":       in.UserID,
		"aud":       in.ClientID,
		"iat":       now.Unix(),
		"exp":       now.Add(IDTokenLifetime).Unix(),
		"auth_time": in.AuthTime.Unix(),
		"amr":       in.AuthMethods,
		"org_id":    in.OrgID,
	}

	// Echoed only when the client supplied one. An unsolicited nonce claim
	// would be a value the client never chose and cannot check.
	if in.Nonce != "" {
		claims["nonce"] = in.Nonce
	}

	if in.SessionID != "" {
		// RFC 7519 has no session claim; `sid` is OIDC's, and P1-10's
		// back-channel logout will need it to say which session ended.
		claims["sid"] = in.SessionID
	}

	return claims, nil
}

// AccessTokenClaims assembles the access token.
//
// `aud` is the resource, not the client. A resource server that validates
// `aud` against itself will refuse an ID token presented as a bearer token,
// which is half of abuse case A-5; `typ: at+jwt` in the header is the other
// half, and it is the half that works even when a resource server is lax about
// `aud`.
func AccessTokenClaims(in Subject, now time.Time) (Claims, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	claims := Claims{
		"iss":       in.Issuer,
		"sub":       in.UserID,
		"aud":       in.Audience,
		"iat":       now.Unix(),
		"exp":       now.Add(AccessTokenLifetime).Unix(),
		"client_id": in.ClientID,
		"org_id":    in.OrgID,
		"scope":     joinScope(in.Scope),
	}

	if in.SessionID != "" {
		claims["sid"] = in.SessionID
	}

	// The role namespace, reserved and empty until P2-04.
	//
	// Present rather than absent, so the token's shape does not change when
	// roles arrive — a consumer reading `claims[ns] ?? {}` today keeps working,
	// and one that would have crashed on a missing key never gets written.
	if in.ProjectID != "" {
		claims[RoleClaimNamespace(in.ProjectID)] = map[string]any{}
	}

	return claims, nil
}

// ClientCredentialsClaims assembles an access token with no user.
//
// `sub` is the client itself, which is RFC 9068's guidance for a two-legged
// token: there is nobody else it could be, and omitting `sub` would make the
// token unattributable in a resource server's own audit log.
func ClientCredentialsClaims(in Subject, now time.Time) (Claims, error) {
	if in.ClientID == "" || in.Issuer == "" || in.OrgID == "" {
		return nil, fmt.Errorf("token: client credentials require issuer, client and organization")
	}

	claims := Claims{
		"iss":       in.Issuer,
		"sub":       in.ClientID,
		"aud":       in.Audience,
		"iat":       now.Unix(),
		"exp":       now.Add(AccessTokenLifetime).Unix(),
		"client_id": in.ClientID,
		"org_id":    in.OrgID,
		"scope":     joinScope(in.Scope),
	}
	if in.ProjectID != "" {
		claims[RoleClaimNamespace(in.ProjectID)] = map[string]any{}
	}
	return claims, nil
}

// Subject is everything the claims are built from.
type Subject struct {
	Issuer    string
	Audience  string
	ClientID  string
	ProjectID string
	OrgID     string

	UserID      string
	SessionID   string
	AuthMethods []string
	AuthTime    time.Time
	Nonce       string
	Scope       []string
}

func (s Subject) validate() error {
	switch {
	case s.Issuer == "":
		return fmt.Errorf("token: issuer is required")
	case s.ClientID == "":
		return fmt.Errorf("token: client is required")
	case s.UserID == "":
		return fmt.Errorf("token: subject is required")
	case s.OrgID == "":
		return fmt.Errorf("token: organization is required")
	}
	return nil
}

func joinScope(scope []string) string {
	out := ""
	for i, s := range scope {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}

// Encode serialises claims for signing.
func (c Claims) Encode() ([]byte, error) {
	encoded, err := json.Marshal(map[string]any(c))
	if err != nil {
		return nil, fmt.Errorf("token: encoding claims: %w", err)
	}
	return encoded, nil
}
