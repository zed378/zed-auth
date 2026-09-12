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

	// The role namespace. Reserved empty by `P1-07`, populated here.
	//
	// Always present when there is a project, even with no roles — so the
	// token's shape does not change when a user is granted their first role,
	// and a consumer reading `claims[ns] ?? {}` keeps working either way.
	if in.ProjectID != "" {
		claims[RoleClaimNamespace(in.ProjectID)] = roleClaim(in.RoleKeys, in.OrgID)
	}

	// Administrative roles, flat, because they are not project-scoped:
	// `docs/PLAN/08` Part C shows exactly this shape.
	//
	// Omitted entirely when there are none, rather than emitted as `[]`. An
	// empty array in a token is a claim asserting "this user is administratively
	// nothing", which is true and is also 30 bytes in every token this service
	// issues — and the overwhelming majority of users are administratively
	// nothing.
	if len(in.ManagerRoles) > 0 {
		claims[ManagerRoleClaim] = bounded(in.ManagerRoles)
	}

	return claims, nil
}

// ManagerRoleClaim is the administrative role claim key (docs/PLAN/08 Part C).
const ManagerRoleClaim = "urn:authservice:manager_roles"

// roleClaim builds the per-project role object.
//
// `{"cashier": {"org_id": "org_acme"}}` — the org_id nested INSIDE each value,
// which looks redundant in Phase 2 because there is only one organizational
// context a role can come from. `docs/PLAN/08` Part A says why it is there
// anyway: once Phase 4's delegation makes the same role name reachable from two
// contexts, a consumer needs to tell them apart — and adding a field to a claim
// consumers already parse is a breaking change for every one of them.
//
// So it is emitted now, when it costs nothing, rather than when it is needed.
func roleClaim(keys []string, orgID string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, key := range bounded(keys) {
		// A map, so a duplicated key collapses rather than producing a
		// malformed claim. The grant store already refuses duplicates; this is
		// the layer that cannot produce one even if it did.
		out[key] = map[string]any{"org_id": orgID}
	}
	return out
}

// bounded truncates a role list to MaxRoleClaims. See the constant.
func bounded(keys []string) []string {
	if len(keys) <= MaxRoleClaims {
		return keys
	}
	return keys[:MaxRoleClaims]
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

	// RoleKeys are the roles this user holds IN THIS CLIENT'S PROJECT, and
	// nowhere else (P2-04).
	//
	// Scoped that way deliberately. A user may hold roles in every project an
	// organization has, and a token carrying all of them grows with the
	// organization rather than with the request — while the consumer reading
	// it only ever cares about its own. `docs/PLAN/08` Part A's claim key is
	// per-project for the same reason.
	RoleKeys []string

	// ManagerRoles are the administrative roles the user holds
	// (`docs/PLAN/08` Part C). Present in the token so a consumer can tell an
	// administrator from an ordinary user without a second call.
	ManagerRoles []string
}

// MaxRoleClaims bounds how many role keys a token will carry.
//
// ADR-021. A token is carried in an `Authorization` header, and most proxies
// and application servers cap header size at 8 KB — so a token that grows
// without bound eventually stops working, and it stops working at the
// consumer, in production, for whichever user happened to accumulate the most
// roles. That failure is a 431 or a truncated header, neither of which points
// at this service.
//
// Sixty-four matches `grant.MaxRoleKeys`: a grant cannot carry more, so a
// token cannot need more for one project. If the bound is ever reached the
// claim is TRUNCATED rather than dropped and the token still carries a true
// subset, because a consumer denying access it should have granted is
// recoverable and a consumer granting access it should not have is not.
const MaxRoleClaims = 64

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
