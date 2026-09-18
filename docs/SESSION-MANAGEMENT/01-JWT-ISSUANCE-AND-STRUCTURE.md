# 01 - JWT Issuance and Structure

> Category: **Session Management** (`docs/SESSION-MANAGEMENT/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-07, P2-04 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes the two JWTs `POST /oauth/token` issues — the ID token and the access token — their exact claim shapes, lifetimes, and the type-confusion defense that keeps one from being accepted where the other belongs.

## Scope

Covers claim assembly (`backend/internal/oauth/token/claims.go`) and how tokens are signed (`backend/internal/oauth/token/handler.go`). Key material and rotation are `03-JWKS-KEY-ROTATION.md`. Refresh tokens are a separate, opaque credential — see `02-REFRESH-TOKENS-AND-ROTATION.md`. Role/authorization semantics of the claims are `docs/PLAN/08-AUTHORIZATION.md`.

## As Built

Two distinct token kinds are issued, distinguished by the JWS `typ` header — a security control, not bookkeeping, per `backend/internal/oauth/token/claims.go`:

| | `typ` header | `aud` claim |
|---|---|---|
| ID token | `JWT` | the requesting client's `client_id` |
| Access token | `at+jwt` (RFC 9068) | the issuer itself (`Subject.Audience` is set to `h.Issuer` on every issuance path in `backend/internal/oauth/token/handler.go`) |

`signing.Verifier.Verify(compact, wantType)` requires the caller to name which `typ` it expects and refuses a mismatch (`ErrWrongType`) — this is what stops an ID token being replayed as a bearer access token and vice versa (abuse case A-5).

### ID token claims (`IDTokenClaims`)

`iss`, `sub` (user UUID), `aud` (client_id), `iat`, `exp`, `auth_time` (from the session's `CreatedAt`), `amr` (from the session's recorded `auth_methods` — never regenerated at issuance), `org_id`, `nonce` (only if the authorization request supplied one), `sid` (only if a session backs the token). Refuses to build a claim set with no `auth_methods` at all — an ID token that cannot say how somebody authenticated is refused rather than issued with a false claim.

### Access token claims (`AccessTokenClaims`)

`iss`, `sub` (user UUID, or the client's own id for `client_credentials`), `aud`, `iat`, `exp`, `client_id`, `org_id`, `scope` (space-joined), `sid` (if a session backs it), plus two role claims:

- **`urn:authservice:iam:org:project:<project_id>:roles`** — a per-project object, e.g. `{"cashier": {"org_id": "3f2b8c1e-5d4a-4f7b-9c2e-1a6d8e0b7c45"}}` (`docs/PLAN/08` writes the example with a prefixed id such as `org_acme`; the service emits UUIDs — `TASKS/BACKLOG.md` PG-23). Present (possibly `{}`) whenever the token is issued against a project, even with zero roles, so the shape does not change the first time a role is granted. The `org_id` is nested inside each value rather than only at the top level, deliberately, so a future context (Phase 4 delegation) that reaches the same role name from two organizations remains distinguishable without a breaking claim-shape change.
- **`urn:authservice:manager_roles`** — flat, administrative roles (`docs/PLAN/08` Part C). Omitted entirely (not `[]`) when the user holds none.

Role keys are truncated to `MaxRoleClaims` = 64 per project (ADR-021) to keep the token under common header-size limits; truncation, not refusal, so a token still carries a true (if incomplete) subset rather than none.

A failure to read role claims does **not** fail token issuance — a role-less token is logged at `ERROR` and issued anyway, because a consumer that reads fewer roles than it should is recoverable, whereas refusing every login in the deployment over one failed query is not.

### `client_credentials` claims (`ClientCredentialsClaims`)

`sub` is the client itself (RFC 9068 guidance for a two-legged token — there is no user). No refresh token is ever issued for this grant. The role namespace is present but always empty.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Access token lifetime | 10 minutes | `token.AccessTokenLifetime` |
| ID token lifetime | 5 minutes | `token.IDTokenLifetime` |
| Signing algorithms | `RS256` (min 2048-bit RSA), `ES256` (P-256 only) — never `HS256`, never `none` | `signing.Algorithm`, `signing.allowedAlgorithms` |
| `typ` values | `JWT` (ID token), `at+jwt` (access token) | `signing.TypeJWT`, `signing.TypeAccessToken` |
| Max role keys per project claim | 64 (truncated, not dropped) | `token.MaxRoleClaims` |
| `kid` header | Always present, RFC 7638 thumbprint | `signing.Signer.SignWithType` |

## Interfaces

`POST /oauth/token` (`token`) is the only issuance path; see `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md` for the grant flows around it. Response body per RFC 6749 §5.1: `access_token`, `token_type` (`Bearer`), `expires_in`, `id_token` (omitted unless `openid` scope and the authorization-code grant), `refresh_token` (omitted unless `offline_access` scope and the client holds the refresh grant), `scope`. Response headers always include `Cache-Control: no-store` and `Pragma: no-cache`.

## Security Considerations

- **Algorithm confusion and `alg: none`** are structurally excluded — the allowlist is checked before any key lookup (see `03-JWKS-KEY-ROTATION.md`). `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §1.
- **Token substitution (A-5)**: the `typ` header plus `Verify`'s required `wantType` parameter stop an ID token being presented at a resource server, or an access token being presented as a logout `id_token_hint`.
- **JWTs carry identifiers, not secrets or PII**: no password, no raw resource attribute, and email/name only appear via the separate, scope-gated `/oauth/userinfo` endpoint (`docs/IDENTITY-PROTOCOL/03-USERINFO-ENDPOINT.md`), never in the access or ID token itself.
- **Header-size denial of service via role growth** is bounded by `MaxRoleClaims` (ADR-021).

## Verification

| Test | File |
|---|---|
| `TestAlgNoneIsRejected`, `TestAlgorithmConfusionIsRejected` | `backend/internal/signing/sign_test.go` |
| `TestATokenOfTheWrongTypeIsRefused`, `TestAnUntypedTokenIsRefused`, `TestVerifyRequiresAWantedType` | same |
| Claim assembly and role-claim shape | `backend/internal/oauth/token/claims_test.go`, `docsclaims_test.go`, `roleclaims_test.go`, `roleclaims_integration_test.go` |

## Not Yet Built / Open Questions

- No token exchange (RFC 8693), DPoP, or PAR/JAR — not attempted in the current roadmap phase.
- The access token's `aud` is always the issuer itself; there is no multi-resource-server audience model.

## Related Documents

- `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`
- `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`
- `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md`
- `docs/PLAN/08-AUTHORIZATION.md` Part A, Part C
- `MEMORY/specs/P1-07-token.md`
- `MEMORY/DECISIONS.md` ADR-021
