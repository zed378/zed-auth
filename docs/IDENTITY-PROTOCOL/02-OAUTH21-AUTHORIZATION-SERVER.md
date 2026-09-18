# 02 - OAuth 2.1 Authorization Server

> Category: **Identity Protocol** (`docs/IDENTITY-PROTOCOL/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-05, P1-06, P1-07, P1-09 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describes the authorization-code-with-PKCE flow, the supported grants, and the validation ordering at `GET /oauth/authorize` that is itself the defense against open-redirect delivery.

## Scope

Covers `backend/internal/oauth/authorize/` and the grant-handling parts of `backend/internal/oauth/token/`. Token contents are `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`; refresh rotation is `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`; the login page behind the interactive path is `06-MULTI-FACTOR-AUTHENTICATION.md` for its MFA aspects.

## As Built

### Grants

Three grants are implemented: `authorization_code`, `refresh_token`, `client_credentials`. Two are **permanently refused**, named explicitly rather than falling through to a generic "unknown grant" error, so an integrator learns the service will never support them rather than wondering if they mistyped: `password` (resource owner password credentials) and `implicit` (deprecated in OAuth 2.1), plus their JWT/SAML-bearer assertion-grant URNs (`backend/internal/oauth/token/auth.go`, `refusedGrants`). A client must additionally hold the grant it is trying to use in its own `GrantTypes` registration, checked again at the token endpoint even though registration already refuses nonsensical combinations.

### `GET /oauth/authorize` — two-phase validation

The handler (`backend/internal/oauth/authorize/handler.go`) is structured in an order that is itself the security property, documented in its own header comment:

- **Phase 1** resolves `client_id` and matches `redirect_uri` **exactly** against the client's registration — no default-URI convenience, so the exact-match rule does not become conditional on how many URIs happen to be registered. Any failure here renders a plain HTML error page and issues **no redirect at all**, because there is no validated destination to redirect to yet — reporting this class of error via redirect would itself be the open-redirect vulnerability, delivered by the code meant to prevent it. An unknown `client_id` and a mismatched `redirect_uri` are answered with the identical error message, so a prober cannot learn which client ids exist.
- **Phase 2** validates everything else (`response_type=code` only; `state` required, non-empty; PKCE challenge/method; `scope`; `nonce`; `prompt`; `max_age`) once the redirect target is known-good — every failure from here on **is** reported by redirecting to that validated URI, with an OAuth error code and the original `state` echoed back.

**PKCE (S256) is mandatory for every client type, including confidential ones** — stricter than the OAuth 2.1 baseline. The reasoning stated in the code: a confidential client's secret protects the *token* request, not the authorization code in transit through the browser (a referrer leak, a malicious app claiming the same custom scheme, a shoulder-surfed URL bar). `plain` is never accepted at this layer, never advertised in discovery, and there is no code path that could apply it.

`prompt` values `none`, `login`, `consent`, `select_account` are supported. `prompt=none` combined with any other value is refused as self-contradictory. `max_age` compares against the session's actual authentication time (`Session.CreatedAt`), not its last-activity time.

A resolved session must belong to the **same organization** as the requesting client's project — a session for another organization is treated as no session at all, on both the silent and the login-resumption path (`h.session`, `h.Resume`). This is the check that currently makes cross-organization sign-in (ADR-025) unbuildable without a deliberate, separate change.

The organization's MFA mandate is re-checked here too (`Mandate` interface, satisfied by `authn.MandateCheck`, wired at P3-14): a live session for a user who now falls outside their organization's MFA grace period is treated as no session, so silent renewal cannot bypass a mandate that started after the session began. See `06-MULTI-FACTOR-AUTHENTICATION.md`.

Authorization codes are single-use (Redis `GETDEL`, atomic — two concurrent redemptions yield exactly one success), 30-second TTL (well under the 60-second ceiling), and bind `client_id`, `redirect_uri`, the PKCE challenge, `scope`, `nonce`, and the session's `auth_methods`/`auth_time` — every field checked again at redemption. A code is redeemed *first*, before its client/redirect/PKCE fields are checked, so a wrong guess against any of them burns the code rather than leaving it alive as a brute-force oracle.

### `POST /oauth/token` — client authentication

`client_secret_basic` and `client_secret_post` are both accepted; presenting both at once is refused (RFC 6749 §2.3.1) rather than picking a winner. A public client must present **no** secret at all — if one is presented anyway, it is refused as `invalid_client` rather than silently downgraded to public-client handling, because that downgrade is exactly how a confidential client's leaked secret would stop mattering. Secret verification is constant-time and honors a rotation-overlap window, so an operator can rotate a client secret without a simultaneous redeploy (`P1-05`).

### CORS

Per `MEMORY/DECISIONS.md` ADR-020, CORS policy is split by what an endpoint actually is:

| Endpoints | Policy | Credentials |
|---|---|---|
| `/.well-known/*`, `/oauth/token` | `Access-Control-Allow-Origin: *` | Never |
| `/oauth/userinfo`, `/v1/*` | The calling application's registered `allowed_origins` | Never |
| Everything else | No CORS headers | — |

`/oauth/token` is wildcarded because the token exchange happens before the client presents any credential the server could resolve an application-specific allowlist from — a public single-page application could not otherwise complete a login from its own origin.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| PKCE | Mandatory, `S256` only, for every client type | `authorize.validatePKCE` |
| `response_type` | `code` only | `authorize.ParsePhase2` |
| `state` | Required, non-empty, ≤1024 chars | same |
| Authorization code TTL | 30 seconds (60s ceiling) | `authorize.CodeTTL` |
| Pending (interrupted) authorization TTL | 10 minutes | `authorize.PendingTTL` |
| Refused grants | `password`, `implicit`, JWT/SAML-bearer URNs | `token.refusedGrants` |
| Client secret presentation | Basic or post, never both | `token.ParseCredentials` |
| Public client presenting a secret | Refused (`invalid_client`), never downgraded | `token.AuthenticateClient` |

## Interfaces

| Method | Path | operationId | Notes |
|---|---|---|---|
| GET | `/oauth/authorize` | `authorize` | |
| POST | `/oauth/token` | `token` | `grant_type` one of `authorization_code`, `refresh_token`, `client_credentials` |
| POST | `/oauth/introspect` | `introspect` | Confidential clients only |
| POST | `/oauth/revoke` | `revoke` | Public or confidential |

## Security Considerations

- **Open redirect**: closed by the two-phase ordering — no redirect is emitted until `redirect_uri` is exactly matched. `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §2.
- **Authorization code interception**: closed by mandatory PKCE on every client type, not only public ones.
- **Client enumeration**: unknown `client_id` and wrong `redirect_uri`, and unknown client and wrong secret, are answered identically. `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §12.
- **Cross-tenant code issuance**: the session-organization-matches-client-organization check in `h.session`/`h.Resume`.

## Verification

Authorization and token-endpoint ordering, PKCE, client authentication, and grant refusal are covered by the unit and integration suites in `backend/internal/oauth/authorize/` and `backend/internal/oauth/token/` (see `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md` and `02-REFRESH-TOKENS-AND-ROTATION.md` Verification sections for the specific token-path tests). Discovery's own tests (`docs/IDENTITY-PROTOCOL/01-OIDC-DISCOVERY-AND-JWKS.md`) assert that refused grants can never even be advertised.

## Not Yet Built / Open Questions

- PAR/JAR, DPoP, token exchange (RFC 8693), and mTLS/`private_key_jwt` client authentication are not implemented.
- Cross-organization (delegated) authorization is decided (ADR-025) but unbuilt — owning task `P4-04`.

## Related Documents

- `docs/SESSION-MANAGEMENT/00-SESSION-ARCHITECTURE.md`
- `docs/SESSION-MANAGEMENT/01-JWT-ISSUANCE-AND-STRUCTURE.md`
- `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`
- `docs/IDENTITY-PROTOCOL/06-MULTI-FACTOR-AUTHENTICATION.md`
- `MEMORY/specs/P1-05-application-registration.md`, `P1-06-authorize.md`, `P1-07-token.md`
- `MEMORY/DECISIONS.md` ADR-020
