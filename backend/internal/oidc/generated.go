package oidc

import (
	"context"

	"github.com/zed378/zed-auth/backend/internal/api"
)

// The generated contract, implemented rather than routed around.
//
// Adding these two endpoints to openapi/openapi.yaml broke the build until
// this file existed, which is ADR-013 working as intended: the spec generates
// the server interface, so a documented endpoint with no handler is a compile
// error rather than a 404 somebody finds later.
//
// It would have been easier to register the handlers on the router by hand and
// leave them out of the spec. That is the shortcut worth naming: the public
// API reference generates from the spec, so an endpoint missing from it is an
// endpoint no consumer can discover — and these two exist entirely to be
// discovered.
//
// The response types are the generated ones, so the Content-Type and the
// Cache-Control policy come from the contract rather than from a handler
// remembering to set them.

// httpserver.apiRoutes embeds this type alongside the probe implementation and
// asserts that together they satisfy api.StrictServerInterface. The assertion
// lives there rather than here because that is where both halves are in scope.

// GetOpenIDConfiguration serves GET /.well-known/openid-configuration.
func (h *Handler) GetOpenIDConfiguration(
	context.Context, api.GetOpenIDConfigurationRequestObject,
) (api.GetOpenIDConfigurationResponseObject, error) {
	return api.GetOpenIDConfiguration200JSONResponse{
		Body:    h.configuration,
		Headers: api.GetOpenIDConfiguration200ResponseHeaders{CacheControl: cacheControl(discoveryMaxAge)},
	}, nil
}

// GetJWKS serves GET /.well-known/jwks.json.
//
// Built per request from the key cache rather than pre-rendered: the key set
// changes on rotation, which is the event this endpoint exists to communicate.
// The cache underneath makes it a memory read, not a database query.
func (h *Handler) GetJWKS(
	context.Context, api.GetJWKSRequestObject,
) (api.GetJWKSResponseObject, error) {
	set, err := h.keys.Get()
	if err != nil {
		// No detail. This endpoint is public and unauthenticated, and "which
		// dependency is down" is not something to tell an anonymous caller
		// (docs/SECURITY/02 §12). The reason is in the logs.
		return api.GetJWKS503JSONResponse(unavailable()), nil
	}

	return api.GetJWKS200ApplicationJwkSetPlusJSONResponse{
		Body:    toGeneratedJWKS(set.JWKS()),
		Headers: api.GetJWKS200ResponseHeaders{CacheControl: cacheControl(jwksMaxAge)},
	}, nil
}

func unavailable() api.Error {
	var e api.Error
	e.Error.Code = api.INTERNAL
	e.Error.Message = "The key set is temporarily unavailable"
	return e
}
