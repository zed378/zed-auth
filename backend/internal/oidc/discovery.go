// Package oidc serves the two documents a consumer application needs to
// configure itself: the discovery document and the JWKS.
//
// The whole point of these endpoints is that an integrator points a library at
// a URL instead of copying values from a wiki page that has been wrong since
// the last rotation. That only works if what the documents say is true — which
// makes them a governance problem as much as a serialisation one.
//
// `P1-04` step 2 puts it directly: advertise only what is actually
// implemented, because "a client that trusts the discovery document and finds
// a missing endpoint fails at runtime". `CLAUDE.md`'s rule about not claiming
// unshipped capability applies at least as strongly to a machine-readable
// document as to marketing copy — a human reading a brochure is sceptical, a
// client library is not.
//
// So the document is DERIVED from the capabilities the router actually
// registered, never written out as a literal. See Capabilities.
package oidc

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/zed378/zed-auth/backend/internal/api"
	"github.com/zed378/zed-auth/backend/internal/signing"
)

// Capabilities describes what this deployment actually serves.
//
// Every field here is a statement of fact about running code, and the
// discovery document is rendered from it. The alternative — a handwritten JSON
// literal — is a second source of truth that starts correct and drifts on the
// first release where an endpoint moves.
//
// A field left false means the corresponding endpoint is absent from the
// document entirely. That is deliberate and it is why this type exists: it is
// not possible to advertise `/oauth/token` without the code that serves it
// having set TokenEndpoint, because nothing else writes that field.
type Capabilities struct {
	// Issuer is the identifier clients validate the `iss` claim against.
	//
	// It must match byte for byte what the token issuer emits. A mismatch here
	// is the classic OIDC misconfiguration: every conforming library rejects
	// every token, and the error surfaces in the client rather than here.
	Issuer string

	// Endpoints that exist. Empty means "not implemented", and an empty value
	// is omitted from the document rather than published as a broken URL.
	AuthorizationEndpoint string
	TokenEndpoint         string
	UserInfoEndpoint      string
	JWKSURI               string
	RevocationEndpoint    string
	IntrospectionEndpoint string
	EndSessionEndpoint    string

	// GrantTypes actually accepted by the token endpoint.
	//
	// PLAN/05 § Supported Grant Types rules two out permanently: the implicit
	// flow (deprecated in OAuth 2.1) and resource owner password credentials.
	// Neither may ever appear here, and a test asserts it — advertising a
	// grant this service refuses invites a client to build against it.
	GrantTypes []string

	// ResponseTypes accepted by the authorization endpoint.
	ResponseTypes []string

	// Scopes recognised at the token endpoint.
	Scopes []string

	// SigningAlgorithms the ID token may be signed with.
	SigningAlgorithms []string
}

// forbiddenGrants are the two PLAN/05 rules out permanently.
//
// Checked at construction rather than trusted: a grant added to a slice
// somewhere far from this file should fail loudly here, not be published.
var forbiddenGrants = map[string]string{
	"implicit": "deprecated in OAuth 2.1 (PLAN/05 § Supported Grant Types)",
	"password": "resource owner password credentials are not supported (PLAN/05)",
}

// Validate reports whether the capabilities are internally coherent.
//
// Called at startup, so a misconfiguration is a refusal to boot rather than a
// document that quietly lies to every client that reads it.
func (c Capabilities) Validate() error {
	if c.Issuer == "" {
		return fmt.Errorf("issuer is required: it is what clients validate the iss claim against")
	}

	// The issuer is an identifier, not a URL prefix to be normalised. A
	// trailing slash makes it a different string, and clients compare strings.
	if strings.HasSuffix(c.Issuer, "/") {
		return fmt.Errorf("issuer %q has a trailing slash: clients compare the iss claim "+
			"byte for byte, so this will not match the tokens this service issues", c.Issuer)
	}

	if c.JWKSURI == "" {
		return fmt.Errorf("jwks_uri is required: without it a client cannot verify a token")
	}

	for _, grant := range c.GrantTypes {
		if reason, forbidden := forbiddenGrants[grant]; forbidden {
			return fmt.Errorf("grant type %q must never be advertised: %s", grant, reason)
		}
	}

	return nil
}

// codeChallengeMethods is S256 and nothing else.
//
// `plain` is in the PKCE specification and is not offered here. It transmits
// the verifier unprotected, so an attacker who can observe the authorization
// request can complete the exchange — which removes the entire reason PKCE
// exists. Advertising it invites a client library to negotiate down to it.
var codeChallengeMethods = []string{"S256"}

// subjectTypes: public only.
//
// Pairwise subject identifiers give a different `sub` to each client, which is
// a privacy feature this service does not implement. Advertising it would be a
// claim about behaviour that does not exist.
var subjectTypes = []string{"public"}

// Handler serves the discovery document and the JWKS.
type Handler struct {
	capabilities  Capabilities
	keys          *signing.Cache
	configuration api.OpenIDConfiguration
}

// NewHandler validates the capabilities and builds the discovery document.
//
// Built once, because it changes only when the deployment does. An endpoint
// that does not exist leaves its field nil, and the generated type omits it —
// so an unimplemented endpoint is absent from the document rather than
// published as a URL that returns 404.
func NewHandler(capabilities Capabilities, keys *signing.Cache) (*Handler, error) {
	if err := capabilities.Validate(); err != nil {
		return nil, fmt.Errorf("openid-configuration: %w", err)
	}

	doc := api.OpenIDConfiguration{
		Issuer:                           capabilities.Issuer,
		JwksUri:                          capabilities.JWKSURI,
		SubjectTypesSupported:            subjectTypes,
		IdTokenSigningAlgValuesSupported: capabilities.SigningAlgorithms,
	}

	optional(&doc.AuthorizationEndpoint, capabilities.AuthorizationEndpoint)
	optional(&doc.TokenEndpoint, capabilities.TokenEndpoint)
	optional(&doc.UserinfoEndpoint, capabilities.UserInfoEndpoint)
	optional(&doc.RevocationEndpoint, capabilities.RevocationEndpoint)
	optional(&doc.IntrospectionEndpoint, capabilities.IntrospectionEndpoint)
	optional(&doc.EndSessionEndpoint, capabilities.EndSessionEndpoint)

	if len(capabilities.GrantTypes) > 0 {
		doc.GrantTypesSupported = &capabilities.GrantTypes
	}
	if len(capabilities.ResponseTypes) > 0 {
		doc.ResponseTypesSupported = &capabilities.ResponseTypes
	}
	if len(capabilities.Scopes) > 0 {
		doc.ScopesSupported = &capabilities.Scopes
	}

	// PKCE is only meaningful once there is an authorization endpoint to apply
	// it to. Advertising the methods before then would describe a negotiation
	// that cannot happen.
	if capabilities.AuthorizationEndpoint != "" {
		doc.CodeChallengeMethodsSupported = &codeChallengeMethods
	}

	return &Handler{capabilities: capabilities, keys: keys, configuration: doc}, nil
}

// optional sets a pointer field only when the value is non-empty, so an
// unimplemented endpoint is omitted rather than emitted as "".
func optional(field **string, value string) {
	if value == "" {
		return
	}
	v := value
	*field = &v
}

// discoveryMaxAge is how long a client may cache the discovery document.
//
// An hour. The document changes when endpoints are added or removed, which is
// a deploy rather than a routine event — and a client holding a stale copy for
// an hour after a deploy finds the same endpoints it found before.
const discoveryMaxAge = time.Hour

// jwksMaxAge is how long a client may cache the key set.
//
// Deliberately shorter, and derived rather than chosen: a rotation publishes a
// `next` key, and consumers must have fetched it before it starts signing.
// Five minutes matches the service's own key cache TTL, so the window in which
// any consumer holds a stale key set is bounded by the same value on both
// sides.
//
// This is the trade `P1-04` step 4 names: long enough that verification does
// not become a request per token, short enough that a rotation propagates
// inside the overlap window.
const jwksMaxAge = 5 * time.Minute

// toGeneratedJWKS converts the signing package's key set into the contract's
// shape.
//
// go-jose marshals only public parameters for a public key; this walks that
// output rather than the keys themselves, so nothing private can reach the
// response even if a future change to the signing package got it wrong.
func toGeneratedJWKS(set jose.JSONWebKeySet) api.JWKS {
	out := api.JWKS{Keys: make([]api.JWK, 0, len(set.Keys))}

	for _, key := range set.Keys {
		encoded, err := key.MarshalJSON()
		if err != nil {
			continue
		}

		var generic api.JWK
		if err := json.Unmarshal(encoded, &generic); err != nil {
			continue
		}
		out.Keys = append(out.Keys, generic)
	}

	return out
}

func cacheControl(d time.Duration) string {
	// `public`, because both documents are public by design and there is real
	// benefit in a CDN or a corporate proxy holding them.
	return fmt.Sprintf("public, max-age=%d", int(d.Seconds()))
}
