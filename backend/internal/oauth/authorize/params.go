// Package authorize implements GET /oauth/authorize — the authorization code
// flow with PKCE.
//
// This is the endpoint where single sign-on becomes visible, and the one with
// the most ways to be wrong. It takes seven parameters from an untrusted
// redirect, decides whether to authenticate somebody, and ends by sending a
// browser somewhere with a credential in the URL.
//
// The security property that organises everything here is the ORDER of
// validation. `client_id` and `redirect_uri` are validated before anything
// else, because every other error is reported by redirecting — and reporting
// an error by redirecting to an unvalidated redirect_uri IS the open-redirect
// vulnerability. See Phase1 and Phase2.
//
// Specification: MEMORY/specs/P1-06-authorize.md.
package authorize

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// OAuth 2.1 error codes.
//
// A different vocabulary from docs/PLAN/05's JSON error envelope, and deliberately
// so: this endpoint answers by redirecting with query parameters, which is
// what a consumer library parses. The two coexist — every other endpoint
// speaks the envelope, this one speaks OAuth.
const (
	ErrInvalidRequest          = "invalid_request"
	ErrUnauthorizedClient      = "unauthorized_client"
	ErrAccessDenied            = "access_denied"
	ErrUnsupportedResponseType = "unsupported_response_type"
	ErrInvalidScope            = "invalid_scope"
	ErrServerError             = "server_error"
	ErrLoginRequired           = "login_required"
	ErrInteractionRequired     = "interaction_required"
)

// Error is an OAuth error on its way back to the client.
type Error struct {
	Code        string
	Description string
}

func (e Error) Error() string { return e.Code + ": " + e.Description }

func oauthErr(code, format string, args ...any) Error {
	return Error{Code: code, Description: fmt.Sprintf(format, args...)}
}

// --- bounds -------------------------------------------------------------------

// A query string is attacker-controlled and unbounded. These are generous
// against anything real and stop the endpoint being a place to push bytes.
const (
	maxParamLength   = 2048
	maxScopeValues   = 32
	minCodeChallenge = 43 // 32 bytes, base64url, unpadded — RFC 7636
	maxCodeChallenge = 128
	maxStateLength   = 1024
	maxNonceLength   = 1024
	maxPromptValues  = 4
)

// --- scopes -------------------------------------------------------------------

const (
	ScopeOpenID        = "openid"
	ScopeProfile       = "profile"
	ScopeEmail         = "email"
	ScopeOfflineAccess = "offline_access"
)

// supportedScopes is what this service understands.
//
// The same list P1-04's discovery document publishes, because a scope that is
// advertised and refused, or accepted and unadvertised, are both a document
// that lies about the service.
var supportedScopes = []string{ScopeOpenID, ScopeProfile, ScopeEmail, ScopeOfflineAccess}

func SupportedScopes() []string { return slices.Clone(supportedScopes) }

// --- prompt --------------------------------------------------------------------

const (
	PromptNone          = "none"
	PromptLogin         = "login"
	PromptConsent       = "consent"
	PromptSelectAccount = "select_account"
)

var supportedPrompts = []string{PromptNone, PromptLogin, PromptConsent, PromptSelectAccount}

// --- the two phases ---------------------------------------------------------------

// Phase1 is everything that must be settled BEFORE a redirect is possible.
//
// Only two parameters, and that is the point: until `client_id` resolves to a
// registered application and `redirect_uri` is one of its own, there is nowhere
// safe to send an error. An error reported by redirecting to an unvalidated URI
// is the open-redirect vulnerability itself, delivered by the very code meant
// to reject the request.
type Phase1 struct {
	ClientID    string
	RedirectURI string
}

// ParsePhase1 extracts the two parameters that gate every redirect.
//
// Returns a plain error rather than an oauth Error, because a failure here can
// only be rendered — there is no validated target to redirect to, so an OAuth
// error code would have no channel to travel on.
func ParsePhase1(q url.Values) (Phase1, error) {
	clientID, err := single(q, "client_id")
	if err != nil {
		return Phase1{}, err
	}
	if clientID == "" {
		return Phase1{}, fmt.Errorf("client_id is required")
	}

	redirectURI, err := single(q, "redirect_uri")
	if err != nil {
		return Phase1{}, err
	}
	if redirectURI == "" {
		// Deliberately not defaulted to the client's single registered URI.
		//
		// Some servers do, as a convenience. It makes the exact-match rule
		// conditional on how many URIs happen to be registered, which is
		// exactly the kind of "usually fine" behaviour that turns into a
		// vulnerability when a second URI is added years later.
		return Phase1{}, fmt.Errorf("redirect_uri is required")
	}

	return Phase1{ClientID: clientID, RedirectURI: redirectURI}, nil
}

// Request is the fully validated authorization request.
type Request struct {
	ClientID      string
	RedirectURI   string
	Scope         []string
	State         string
	CodeChallenge string
	Nonce         string
	Prompt        []string
	MaxAge        *time.Duration
}

// WantsScope reports whether a scope was requested.
func (r Request) WantsScope(scope string) bool { return slices.Contains(r.Scope, scope) }

// HasPrompt reports whether a prompt value was requested.
func (r Request) HasPrompt(p string) bool { return slices.Contains(r.Prompt, p) }

// ParsePhase2 validates everything else.
//
// Every failure here is an OAuth error, because by now there is a validated
// redirect URI to deliver it to.
//
// `allowsRefresh` is whether the client holds the refresh_token grant, which
// is the only per-client scope restriction the data model can express today.
func ParsePhase2(q url.Values, p1 Phase1, allowsRefresh bool) (Request, error) {
	responseType, err := singleOAuth(q, "response_type")
	if err != nil {
		return Request{}, err
	}
	if responseType != "code" {
		return Request{}, oauthErr(ErrUnsupportedResponseType,
			"response_type must be code; this service implements the authorization code flow only")
	}

	state, err := singleOAuth(q, "state")
	if err != nil {
		return Request{}, err
	}
	// Required, and this is stricter than OAuth strictly demands.
	//
	// What this endpoint cannot do is CHECK it: state is opaque to us and only
	// the client knows what it sent. Mismatch is detected by the consumer —
	// that is the entire mechanism. Requiring presence means a client that
	// forgot its CSRF defence learns at integration time rather than in
	// production.
	if state == "" {
		return Request{}, oauthErr(ErrInvalidRequest,
			"state is required; it is how your application detects a forged callback")
	}
	if len(state) > maxStateLength {
		return Request{}, oauthErr(ErrInvalidRequest, "state is too long")
	}

	challenge, err := singleOAuth(q, "code_challenge")
	if err != nil {
		return Request{}, err
	}
	method, err := singleOAuth(q, "code_challenge_method")
	if err != nil {
		return Request{}, err
	}
	if err := validatePKCE(challenge, method); err != nil {
		return Request{}, err
	}

	scope, err := parseScope(q, allowsRefresh)
	if err != nil {
		return Request{}, err
	}

	nonce, err := singleOAuth(q, "nonce")
	if err != nil {
		return Request{}, err
	}
	if len(nonce) > maxNonceLength {
		return Request{}, oauthErr(ErrInvalidRequest, "nonce is too long")
	}

	prompt, err := parsePrompt(q)
	if err != nil {
		return Request{}, err
	}

	maxAge, err := parseMaxAge(q)
	if err != nil {
		return Request{}, err
	}

	return Request{
		ClientID:      p1.ClientID,
		RedirectURI:   p1.RedirectURI,
		Scope:         scope,
		State:         state,
		CodeChallenge: challenge,
		Nonce:         nonce,
		Prompt:        prompt,
		MaxAge:        maxAge,
	}, nil
}

// validatePKCE enforces PKCE for every client type.
//
// docs/PLAN/05 states it plainly — "Mandatory for all clients (not just public
// clients)" — and that is stricter than OAuth 2.1's baseline. The reason it is
// worth being stricter: a confidential client's secret protects the TOKEN
// request, not the authorization code in transit. A code intercepted from a
// browser redirect (a referrer leak, a malicious app claiming the same custom
// scheme, a URL bar over somebody's shoulder) is useless without the verifier
// no matter what the client can prove afterwards.
func validatePKCE(challenge, method string) error {
	if challenge == "" {
		return oauthErr(ErrInvalidRequest,
			"code_challenge is required for every client type, including confidential ones")
	}
	if len(challenge) < minCodeChallenge || len(challenge) > maxCodeChallenge {
		return oauthErr(ErrInvalidRequest,
			"code_challenge must be between %d and %d characters", minCodeChallenge, maxCodeChallenge)
	}
	if !isBase64URL(challenge) {
		return oauthErr(ErrInvalidRequest, "code_challenge must be base64url without padding")
	}

	// `plain` is in RFC 7636 and is not offered. It transmits the verifier
	// unprotected, so an attacker who can observe the authorization request can
	// complete the exchange — which removes the entire reason PKCE exists.
	// Accepting it here would also contradict P1-04's discovery document, which
	// advertises S256 and nothing else.
	if method != "S256" {
		if method == "" {
			return oauthErr(ErrInvalidRequest,
				"code_challenge_method is required and must be S256")
		}
		return oauthErr(ErrInvalidRequest,
			"code_challenge_method must be S256; %q is not accepted", method)
	}
	return nil
}

func parseScope(q url.Values, allowsRefresh bool) ([]string, error) {
	raw, err := singleOAuth(q, "scope")
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(raw)
	if len(fields) > maxScopeValues {
		return nil, oauthErr(ErrInvalidScope, "too many scope values")
	}

	// Deduplicated rather than rejected: repeats are harmless and common in
	// hand-built URLs, and refusing them would be pedantry that costs an
	// integrator an afternoon.
	var scope []string
	for _, value := range fields {
		if !slices.Contains(supportedScopes, value) {
			return nil, oauthErr(ErrInvalidScope, "unknown scope %q", value)
		}
		if !slices.Contains(scope, value) {
			scope = append(scope, value)
		}
	}

	if !slices.Contains(scope, ScopeOpenID) {
		return nil, oauthErr(ErrInvalidScope,
			"the openid scope is required; this endpoint issues OpenID Connect authentication")
	}

	// The only per-client scope restriction the data model can express: asking
	// for a refresh token when the client is not allowed refresh tokens.
	if slices.Contains(scope, ScopeOfflineAccess) && !allowsRefresh {
		return nil, oauthErr(ErrInvalidScope,
			"offline_access requires the refresh_token grant, which this client does not have")
	}

	return scope, nil
}

func parsePrompt(q url.Values) ([]string, error) {
	raw, err := singleOAuth(q, "prompt")
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}

	fields := strings.Fields(raw)
	if len(fields) > maxPromptValues {
		return nil, oauthErr(ErrInvalidRequest, "too many prompt values")
	}

	var prompt []string
	for _, value := range fields {
		if !slices.Contains(supportedPrompts, value) {
			return nil, oauthErr(ErrInvalidRequest, "unknown prompt value %q", value)
		}
		if !slices.Contains(prompt, value) {
			prompt = append(prompt, value)
		}
	}

	// `none` means "do not interact under any circumstances". Combined with
	// anything else it is self-contradictory, and guessing which half was meant
	// is worse than refusing: one guess silently shows UI to a hidden iframe,
	// the other silently fails a flow the user was watching.
	if slices.Contains(prompt, PromptNone) && len(prompt) > 1 {
		return nil, oauthErr(ErrInvalidRequest,
			"prompt=none cannot be combined with other prompt values")
	}

	return prompt, nil
}

func parseMaxAge(q url.Values) (*time.Duration, error) {
	raw, err := singleOAuth(q, "max_age")
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}

	seconds, convErr := strconv.Atoi(raw)
	if convErr != nil || seconds < 0 {
		return nil, oauthErr(ErrInvalidRequest, "max_age must be a non-negative number of seconds")
	}

	d := time.Duration(seconds) * time.Second
	return &d, nil
}

// --- parameter plumbing -----------------------------------------------------------

// single returns a parameter, refusing duplicates.
//
// Taking the first or the last value is how parameter-pollution bypasses get
// in: a validator reads one and the consumer reads the other. Refusing the
// whole request is the only reading that cannot be split.
func single(q url.Values, name string) (string, error) {
	values := q[name]
	if len(values) > 1 {
		return "", fmt.Errorf("%s appears %d times; each parameter may appear at most once", name, len(values))
	}
	if len(values) == 0 {
		return "", nil
	}
	if len(values[0]) > maxParamLength {
		return "", fmt.Errorf("%s is too long", name)
	}
	return values[0], nil
}

func singleOAuth(q url.Values, name string) (string, error) {
	value, err := single(q, name)
	if err != nil {
		return "", oauthErr(ErrInvalidRequest, "%s", err.Error())
	}
	return value, nil
}

func isBase64URL(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return s != ""
}
