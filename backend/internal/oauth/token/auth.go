// Package token implements POST /oauth/token — the exchange of an
// authorization code, a refresh token, or client credentials for tokens.
//
// PLAN/12 calls this the highest-volume, most latency-sensitive endpoint in
// the system: every consumer's login latency is this endpoint's latency, and
// so is every silent renewal. ADR-016's choice of SHA-256 over Argon2id for
// client secrets was made for this path specifically, and it is spent here.
//
// Specification: MEMORY/specs/P1-07-token.md.
package token

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zed378/zed-auth/backend/internal/oauth/client"
)

// OAuth 2.1 token-endpoint errors.
//
// A JSON body here rather than the redirect parameters /oauth/authorize uses,
// and different again from PLAN/05's envelope. Three vocabularies in one
// service sounds like a mess and is not: each is what the caller at that
// boundary already parses, and inventing a fourth to unify them would mean
// every consumer library needed special handling for us.
const (
	ErrInvalidRequest       = "invalid_request"
	ErrInvalidClient        = "invalid_client"
	ErrInvalidGrant         = "invalid_grant"
	ErrUnauthorizedClient   = "unauthorized_client"
	ErrUnsupportedGrantType = "unsupported_grant_type"
	ErrInvalidScope         = "invalid_scope"
	ErrServerError          = "server_error"
)

// Error is an OAuth error with the status it travels on.
type Error struct {
	Code        string
	Description string
	Status      int

	// Basic reports whether the client attempted Basic authentication, so the
	// handler can add WWW-Authenticate. RFC 6749 § 5.2 requires it on a 401
	// answering a Basic attempt.
	Basic bool
}

func (e Error) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code string, status int, format string, args ...any) Error {
	return Error{Code: code, Status: status, Description: fmt.Sprintf(format, args...)}
}

func badRequest(code, format string, args ...any) Error {
	return oauthErr(code, http.StatusBadRequest, format, args...)
}

// --- grants -------------------------------------------------------------------

const (
	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
	GrantClientCredentials = "client_credentials"
)

// refusedGrants are the two PLAN/05 rules out permanently.
//
// Named explicitly rather than falling through to "unknown grant". A client
// built against the password grant should be told the service will never
// support it, not left wondering whether it typed the name wrong.
var refusedGrants = map[string]string{
	"password": "resource owner password credentials are not supported (PLAN/05); " +
		"use the authorization code flow",
	"implicit": "the implicit flow is deprecated in OAuth 2.1 and is not supported (PLAN/05); " +
		"use the authorization code flow with PKCE",
	// The URN forms, since a library may send either.
	"urn:ietf:params:oauth:grant-type:jwt-bearer":   "assertion grants are not supported",
	"urn:ietf:params:oauth:grant-type:saml2-bearer": "assertion grants are not supported",
}

var supportedGrants = []string{GrantAuthorizationCode, GrantRefreshToken, GrantClientCredentials}

// ValidateGrant reports whether the grant is one this service implements.
func ValidateGrant(grant string) error {
	if grant == "" {
		return badRequest(ErrInvalidRequest, "grant_type is required")
	}
	if reason, refused := refusedGrants[grant]; refused {
		return badRequest(ErrUnsupportedGrantType, "%s", reason)
	}
	for _, supported := range supportedGrants {
		if grant == supported {
			return nil
		}
	}
	return badRequest(ErrUnsupportedGrantType, "grant_type %q is not supported", grant)
}

// --- client authentication -------------------------------------------------------

// Credentials are what a request presented to identify its client.
type Credentials struct {
	ClientID string
	Secret   string

	// UsedBasic records that the secret arrived in an Authorization header,
	// so a 401 can carry WWW-Authenticate.
	UsedBasic bool

	// Presented is whether a secret was supplied at all. Distinct from an
	// empty secret: a public client presents none, and a confidential client
	// presenting an empty one is a different mistake.
	Presented bool
}

// ParseCredentials extracts client credentials from a request.
//
// Both client_secret_basic and client_secret_post are accepted, and presenting
// BOTH is refused. RFC 6749 § 2.3.1 forbids it, and the practical reason is
// better than the citation: a request that authenticates two ways is one where
// something is confused about which credential it holds, and picking a winner
// would hide that.
func ParseCredentials(r *http.Request, form url.Values) (Credentials, error) {
	var creds Credentials

	basicID, basicSecret, hasBasic := r.BasicAuth()
	formID := form.Get("client_id")
	formSecret := form.Get("client_secret")

	if hasBasic && formSecret != "" {
		return Credentials{}, Error{
			Code:   ErrInvalidRequest,
			Status: http.StatusBadRequest,
			Basic:  true,
			Description: "client credentials were presented both in the Authorization header " +
				"and in the request body; use one",
		}
	}

	switch {
	case hasBasic:
		// RFC 6749 § 2.3.1: both parts are form-urlencoded inside Basic.
		// Skipping this decode is a real interoperability bug for any secret
		// containing a character that needs escaping.
		id, err := url.QueryUnescape(basicID)
		if err != nil {
			return Credentials{}, Error{
				Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: true,
				Description: "the Authorization header is malformed",
			}
		}
		secret, err := url.QueryUnescape(basicSecret)
		if err != nil {
			return Credentials{}, Error{
				Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: true,
				Description: "the Authorization header is malformed",
			}
		}

		// A client_id in both places must agree. Disagreement is the same
		// confusion as presenting two secrets.
		if formID != "" && formID != id {
			return Credentials{}, Error{
				Code: ErrInvalidRequest, Status: http.StatusBadRequest, Basic: true,
				Description: "client_id in the Authorization header does not match the request body",
			}
		}

		creds = Credentials{ClientID: id, Secret: secret, UsedBasic: true, Presented: true}

	case formSecret != "":
		creds = Credentials{ClientID: formID, Secret: formSecret, Presented: true}

	default:
		creds = Credentials{ClientID: formID}
	}

	if creds.ClientID == "" {
		return Credentials{}, Error{
			Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
			Description: "client_id is required",
		}
	}

	return creds, nil
}

// AuthenticateClient checks the presented credentials against the application.
//
// The rule that carries abuse case A-4: a confidential client presenting no
// secret is invalid_client, never quietly treated as public. That downgrade is
// the client-authentication bypass, and it is the kind that looks like
// leniency until somebody notices a `web` client's code can be redeemed by
// anyone who saw it.
func AuthenticateClient(
	app client.Application, creds Credentials, stored client.Credentials, now time.Time,
) error {
	switch {
	case app.Type.IsPublic():
		// A public client has no secret. Presenting one means the caller holds
		// a credential it should not have — a copied configuration, or the
		// wrong client_id — and accepting it would confirm the mistake works.
		if creds.Presented {
			return Error{
				Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
				Description: "this client is public and has no client secret",
			}
		}
		// Authenticated by the PKCE verifier alone, which the caller checks.
		return nil

	case !app.Type.IsConfidential():
		// saml, which participates in no OIDC grant at all.
		return Error{
			Code: ErrUnauthorizedClient, Status: http.StatusBadRequest,
			Description: "this client type cannot use the token endpoint",
		}
	}

	// Confidential from here.
	if !creds.Presented {
		return Error{
			Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
			Description: "client authentication is required for this client",
		}
	}

	// Verify(...) is constant-time and checks the rotation overlap: a secret
	// replaced within its window still authenticates, which is what lets an
	// operator rotate without a simultaneous redeploy (P1-05).
	if !stored.Verify(creds.Secret, now) {
		// The same answer as "no such client", deliberately. Distinguishing
		// them would confirm which client ids exist to anyone who can send a
		// request (SECURITY/02 §12).
		return Error{
			Code: ErrInvalidClient, Status: http.StatusUnauthorized, Basic: creds.UsedBasic,
			Description: "client authentication failed",
		}
	}

	return nil
}

// --- PKCE ---------------------------------------------------------------------------

const (
	minVerifier = 43
	maxVerifier = 128
)

// VerifyPKCE checks a verifier against a stored challenge, using S256.
//
// Constant-time, over the base64url digests. The comparison is on fixed-length
// output, so it leaks neither the verifier nor its length.
//
// `plain` is not implemented at any layer: /oauth/authorize refuses to accept
// it, P1-04's discovery document refuses to advertise it, and there is no code
// path here that could apply it. Three refusals rather than one, because the
// interesting failure is a future change that adds it back in only one place.
func VerifyPKCE(challenge, verifier string) error {
	if verifier == "" {
		return badRequest(ErrInvalidGrant, "code_verifier is required")
	}
	if len(verifier) < minVerifier || len(verifier) > maxVerifier {
		return badRequest(ErrInvalidGrant, "code_verifier must be between %d and %d characters",
			minVerifier, maxVerifier)
	}
	if !isVerifierAlphabet(verifier) {
		return badRequest(ErrInvalidGrant, "code_verifier contains characters outside the permitted set")
	}
	if challenge == "" {
		// A code with no challenge should not exist after P1-06. If one
		// appears, refusing is the only safe reading — the alternative is a
		// code redeemable with no proof at all.
		return badRequest(ErrInvalidGrant, "this authorization code carries no PKCE challenge")
	}

	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])

	if subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) != 1 {
		return badRequest(ErrInvalidGrant, "the code_verifier does not match the code_challenge")
	}
	return nil
}

// isVerifierAlphabet implements RFC 7636's unreserved set.
//
// Wider than base64url — it includes '.' and '~' — because the verifier is
// generated by the client and the RFC says so. Narrowing it to base64url would
// reject conforming clients for no gain.
func isVerifierAlphabet(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.' || r == '_' || r == '~':
		default:
			return false
		}
	}
	return true
}

// --- scope --------------------------------------------------------------------------

// NarrowScope returns the scope for a refresh, refusing any widening.
//
// Narrowing is the client's right; widening is not. A refresh that could gain
// authority would make the refresh token more powerful than the consent that
// created it, which is the whole thing a refresh token must not be.
func NarrowScope(original, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return original, nil
	}

	for _, want := range requested {
		if !contains(original, want) {
			return nil, badRequest(ErrInvalidScope,
				"scope %q was not granted originally; refreshing cannot widen scope", want)
		}
	}
	return requested, nil
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// ParseScope splits a space-delimited scope parameter.
func ParseScope(raw string) []string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}
