# 01 - OIDC Discovery and JWKS

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-04, P1-08, P1-09, P1-10 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes the two documents an integrator's OIDC library configures itself from — `/.well-known/openid-configuration` and `/.well-known/jwks.json` — and the rule that keeps them from ever describing a capability this deployment does not actually serve.

## Scope

Covers `backend/internal/oidc/` and how `cmd/authservice` assembles the `oidc.Capabilities` struct. Key rotation mechanics behind `jwks_uri` are `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`. The endpoints the discovery document points at are documented individually elsewhere in this category.

## As Built

**The discovery document is derived from running capability, never handwritten.** `oidc.Capabilities` (`backend/internal/oidc/discovery.go`) is a struct of fields that are each either an implemented endpoint's URL or an empty string. `oidc.NewHandler` builds the JSON document once at startup from this struct; a field left empty is **omitted from the document entirely**, never published as a URL that would 404. This is a governance rule enforced in code, not merely a convention: the type system makes it impossible to advertise `/oauth/token` without the code that serves it having explicitly set `TokenEndpoint`. `Capabilities.Validate()` runs at startup and refuses to boot if the issuer has a trailing slash, `jwks_uri` is empty, or a forbidden grant type (`implicit`, `password`) appears in `GrantTypes` — a misconfiguration is a boot failure, not a document that quietly lies to every client that reads it.

As wired in `backend/cmd/authservice/main.go`, this deployment currently advertises:

| Field | Value | Endpoint status |
|---|---|---|
| `issuer` | the configured issuer URL | — |
| `authorization_endpoint` | `{issuer}/oauth/authorize` | Implemented |
| `token_endpoint` | `{issuer}/oauth/token` | Implemented |
| `userinfo_endpoint` | `{issuer}/oauth/userinfo` | Implemented |
| `revocation_endpoint` | `{issuer}/oauth/revoke` | Implemented |
| `introspection_endpoint` | `{issuer}/oauth/introspect` | Implemented |
| `end_session_endpoint` | `{issuer}/oidc/logout` | Implemented — **RP-Initiated Logout 1.0 (front-channel) only.** Back-channel logout is a different specification and is deliberately **not** advertised, because it is not implemented (`TASKS/BACKLOG.md` `PG-20`) |
| `jwks_uri` | `{issuer}/.well-known/jwks.json` | Implemented |
| `grant_types_supported` | `authorization_code`, `refresh_token`, `client_credentials` | `password` and `implicit` are permanently excluded; `Capabilities.Validate()` would refuse to boot if either were ever added here |
| `response_types_supported` | `code` | — |
| `scopes_supported` | `openid`, `profile`, `email`, `offline_access` | — |
| `code_challenge_methods_supported` | `S256` only | `plain` is never advertised or accepted, at any layer |
| `subject_types_supported` | `public` only | No pairwise subject identifiers |
| `id_token_signing_alg_values_supported` | `RS256`, `ES256` | |

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| An endpoint absent from `Capabilities` is omitted from the document | — | `oidc.NewHandler`, `optional()` |
| Forbidden grant types can never be advertised | `implicit`, `password` | `oidc.forbiddenGrants`, checked in `Capabilities.Validate` |
| `plain` PKCE method never advertised | — | `codeChallengeMethods = []string{"S256"}` |
| Code-challenge methods only appear once an authorization endpoint exists | — | `NewHandler` |
| Discovery document cache | `Cache-Control: public, max-age=3600` | `discoveryMaxAge` |
| JWKS cache | `Cache-Control: public, max-age=300` | `jwksMaxAge`, matched to the 5-minute signing-key cache TTL |
| Trailing-slash issuer refused at boot | — | `Capabilities.Validate` |

## Interfaces

| Method | Path | operationId |
|---|---|---|
| GET | `/.well-known/openid-configuration` | `getOpenIDConfiguration` |
| GET | `/.well-known/jwks.json` | `getJWKS` |

## Security Considerations

- **A client that trusts the discovery document and finds a missing endpoint fails at configuration time**, not halfway through a login — this is the explicit design goal named in `backend/internal/oidc/discovery.go`'s package comment, and it is what the derivation-from-capability rule exists to guarantee.
- The JWKS response is walked through `go-jose`'s own marshaling of public parameters (`toGeneratedJWKS`) rather than serializing the internal key type directly, so a future bug in the signing package cannot leak a private key field into this response even by accident. See `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §1.

## Verification

| Test | File |
|---|---|
| `TestUnimplementedEndpointsAreAbsentEntirely`, `TestEndpointsAppearWhenImplemented` | `backend/internal/oidc/discovery_test.go` |
| `TestPKCEAdvertisesS256AndNeverPlain`, `TestPKCEIsNotAdvertisedWithoutAnAuthorizationEndpoint` | same |
| `TestForbiddenGrantsAreRejectedAtConstruction`, `TestIssuerWithATrailingSlashIsRejected`, `TestIssuerAndJWKSURIAreRequired` | same |
| `TestJWKSExposesNoPrivateParameters`, `TestJWKSFailureRevealsNothing` | same |
| `TestCacheHeadersAreSetAndJWKSIsShorterThanDiscovery`, `TestContentTypes`, `TestAlwaysPresentFields` | same |

## Not Yet Built / Open Questions

- Back-channel logout (not advertised, not implemented — `PG-20`).
- No `mtls_endpoint_aliases`, `pushed_authorization_request_endpoint`, or `dpop_signing_alg_values_supported` — none of PAR, DPoP, or mTLS client auth exist in this deployment.

## Related Documents

- `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`
- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`
- `MEMORY/records/2026-09-09-P1-04-discovery-jwks.md`
- `MEMORY/DECISIONS.md` ADR-020 (CORS split across `/.well-known/*`, `/oauth/token`, `/oauth/userinfo`, `/v1/*`)
