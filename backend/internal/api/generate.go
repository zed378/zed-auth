// Package api holds the generated server contract for the Management REST API.
//
// Everything in api.gen.go is generated from openapi/openapi.yaml and must not
// be edited by hand — regenerate instead. The generated ServerInterface is the
// mechanism by which the spec is enforced rather than merely published: a
// handler that stops matching the contract fails to compile (ADR-013).
//
// Hand-written code may live in this package alongside the generated file, as
// long as it does not duplicate anything generated.
package api

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../openapi/openapi.yaml
