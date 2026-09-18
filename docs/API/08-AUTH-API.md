# 08 - Auth API (OAuth 2.1 / OIDC Protocol Surface)

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-04, P1-06, P1-07, P1-08, P1-09, P1-10 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

List the OAuth 2.1 / OIDC endpoints this service serves, as a REST-surface index — method, path, `operationId`, and authentication model. This document does not re-derive PKCE, token lifetimes, JWT structure, or OIDC discovery semantics; those belong to `docs/IDENTITY-PROTOCOL/` and are linked, not restated.

## Scope

`/oauth/*`, `/oidc/logout`, and the two `/.well-known/*` discovery documents — the **protocol surface**, distinct in kind from the `/v1` Management API (`09`–`16`): unauthenticated at the transport level (`security: []` throughout `openapi/openapi.yaml`), largely form/query encoded rather than JSON bodies, and answering with the protocol's own error vocabulary rather than the `Error` envelope (`04-ERROR-HANDLING.md`). The hosted, server-rendered login pages (`/login`, `/login/forgot`) that `/oauth/authorize` and `/oidc/logout` redirect to are HTML, not JSON API endpoints, and are not part of `openapi.yaml`; they exist (`backend/internal/login/handler.go`, `Path = "/login"`, `ForgotPath = "/login/forgot"`) and are covered by `docs/IDENTITY-PROTOCOL/`, not this document.

## As Built

| Method & path | `operationId` | Purpose |
|---|---|---|
| `GET /.well-known/openid-configuration` | `getOpenIDConfiguration` | OIDC discovery document. Lists only endpoints that actually exist — an endpoint that has not shipped is absent, not present-and-404. |
| `GET /.well-known/jwks.json` | `getJWKS` | Public signature verification keys (RFC 7517). Never a private key parameter. |
| `GET /oauth/authorize` | `authorize` | Authorization Code + PKCE. PKCE (`S256` only) is mandatory for **every** client type, public and confidential alike — stricter than the OAuth 2.1 baseline. |
| `POST /oauth/token` | `token` | Token endpoint. `authorization_code`, `refresh_token`, `client_credentials` only; `password` and `implicit` are refused by name. |
| `GET /oauth/userinfo` | `userinfo` | Claims the presented token's granted scopes authorise. |
| `POST /oauth/introspect` | `introspect` | RFC 7662. Confidential clients only; answers about their own tokens only. |
| `POST /oauth/revoke` | `revoke` | RFC 7009. Always `200`; revoking a refresh token revokes its whole lineage. |
| `GET /oidc/logout` | `logout` | RP-Initiated Logout 1.0. |
| `POST /oidc/logout` | `logoutConfirm` | The hosted confirmation page's own submit; not an endpoint to integrate against directly. |

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Client authentication (`/oauth/token`) | `client_secret_basic` or `client_secret_post` for confidential clients; PKCE verifier for public clients; presenting both a Basic and a post secret is refused | `backend/internal/oauth/token/` |
| PKCE | Mandatory for every client type; `code_challenge_method` must be `S256` (`plain` is never accepted) | `backend/internal/oauth/authorize/` |
| `redirect_uri` / `post_logout_redirect_uri` matching | Exact string comparison — no prefix, no pattern | `backend/internal/oauth/authorize/`, `backend/internal/login/logout.go` |
| Invalid `client_id`/`redirect_uri` on `/oauth/authorize` | `400`, rendered HTML, **no redirect** (redirecting to an unvalidated URI to report the error is the vulnerability) | `backend/internal/oauth/authorize/` |
| `/oauth/introspect` on another client's token, or an unknown token | `{"active": false}` — identical to any other negative case, never an error | `backend/internal/oauth/token/` |
| `/oauth/revoke` outcome | Always `200`, whether something was revoked, nothing matched, or it was already revoked | `backend/internal/oauth/token/` |
| Access token lifetime | 10 minutes (referenced throughout `openapi.yaml`; see `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md` for the full token design) | `backend/internal/signing/` |
| Error format | `OAuthError` (`{"error": "...", "error_description": "..."}`) for `/oauth/*`; a redirect with `?error=...&error_description=...&state=...` for `/oauth/authorize` and `/oidc/logout` — **not** the `Error` envelope `04-ERROR-HANDLING.md` documents | `openapi/openapi.yaml` |

## Interfaces

Full parameter lists, request/response schemas, and every status code are in the generated reference — see `public-site/docs/api-reference/authorize.api.mdx`, `token.api.mdx`, `userinfo.api.mdx`, `introspect.api.mdx`, `revoke.api.mdx`, `logout.api.mdx`, `get-open-id-configuration.api.mdx`, `get-jwks.api.mdx`. Full protocol behaviour — token claim shapes, PKCE mechanics, discovery document fields, JWKS rotation overlap — is `docs/IDENTITY-PROTOCOL/00-IDENTITY-PROTOCOL-OVERVIEW.md`, `01-OIDC-DISCOVERY-AND-JWKS.md`, `02-OAUTH21-AUTHORIZATION-SERVER.md`, and `03-USERINFO-ENDPOINT.md`.

Audit events written by this surface (`backend/internal/audit/audit.go`): `token.issued`, `token.revoked`, `token.refresh.reuse_detected`, `token.reuse_detected`, `user.login.success`, `user.login.failed`, `user.logout`, `session.created`, `session.revoked`.

## Security Considerations

- Every negative answer on `/oauth/introspect` and `/oauth/revoke` is identical whether a token is unknown, expired, revoked, malformed, or belongs to another client — a distinguishing answer would make either endpoint a token oracle (`openapi/openapi.yaml` operation descriptions; `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12).
- `redirect_uri`/`post_logout_redirect_uri` exact-match and the "no redirect on validation failure" rule are the open-redirect defence (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`).
- `/oidc/logout` requires a usable `id_token_hint` to act on a bare `GET` without confirmation, because a `GET` is triggerable by any page on the internet.

## Verification

Protocol-level test suites live under `backend/internal/oauth/`, `backend/internal/login/`, and `backend/tests/security/`; see `docs/IDENTITY-PROTOCOL/` for the specific test names tied to each protocol guarantee, and `docs/PLAN/11-TESTING.md` for the pyramid this surface follows.

## Not Yet Built / Open Questions

- No per-client request quota exists yet on `/oauth/token`, `/oauth/introspect`, or `/oauth/revoke` — see `05-RATE-LIMITING.md`, `TASKS/BACKLOG.md` PG-19.
- SAML 2.0 (`P4-07`…`P4-09`) and social login federation (`P4-10`, `P4-11`) are not part of this surface today; `docs/IDENTITY-PROTOCOL/04-SAML-20-FEDERATION.md` and `05-WEBAUTHN-AND-PASSKEYS.md` describe design intent, not shipped endpoints.

## Related Documents

- `docs/IDENTITY-PROTOCOL/00-IDENTITY-PROTOCOL-OVERVIEW.md`, `01-OIDC-DISCOVERY-AND-JWKS.md`, `02-OAUTH21-AUTHORIZATION-SERVER.md`, `03-USERINFO-ENDPOINT.md`
- `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`, `02-REFRESH-TOKENS-AND-ROTATION.md`
- `public-site/docs/api-reference/`
