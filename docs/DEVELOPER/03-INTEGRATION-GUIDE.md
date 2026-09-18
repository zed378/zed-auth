# 03 - Integration Guide

> Category: **Developer** (`docs/DEVELOPER/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-05…P1-12, P2-04, P2-06, P3-06 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Connect an application to this service: register it, sign a user in, validate the token, and check a permission.

## Scope

The integrator's path. Endpoint-by-endpoint detail is `docs/API/` and `docs/IDENTITY-PROTOCOL/`; the published guides on the public site cover the same ground for an external audience.

## As Built

### 1. Register an application

An administrator creates it under a project:

```
POST /v1/organizations/{org_id}/projects/{project_id}/applications
{
  "name": "Billing portal",
  "type": "spa",
  "redirect_uris": ["https://billing.example.com/callback"]
}
```

The application's id **is** the `client_id`. A confidential client's secret is returned exactly once, at creation or rotation — it is stored hashed and cannot be read back. Redirect URIs are matched exactly; allowed origins are per application, which is what makes the browser flow work from that origin and nowhere else.

### 2. Sign a user in

Authorization code with PKCE (`S256`), against the endpoints discovery advertises:

```
GET /.well-known/openid-configuration      → issuer, endpoints, jwks_uri
GET /oauth/authorize?response_type=code&client_id=…&redirect_uri=…
      &scope=openid%20offline_access&state=…&nonce=…
      &code_challenge=…&code_challenge_method=S256
POST /oauth/token   grant_type=authorization_code&code=…&code_verifier=…
```

The user reaches the hosted login page on the issuer's own origin; the organization's policy decides what is asked of them there (password, a second factor, which methods are permitted). Your application never sees a password.

### 3. Validate tokens

Fetch and cache the JWKS from `jwks_uri`, then check, on every request: the signature, `iss`, `aud`, `exp`, and the token type. A token minted for another application must be refused — one of the demo applications exists specifically to prove that path (`console/e2e/sso.spec.ts`).

### 4. Keep the session

`offline_access` yields a refresh token. Rotation is on: each exchange returns a new refresh token and spends the old one. **Store the new one immediately.** Presenting a spent token outside the short grace window is treated as reuse and revokes the whole family — a real protection, and a real source of bugs in clients that keep the original (`docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`).

### 5. Decide what the user may do

Two ways, with different freshness:

- **Role claims in the token** — fast, and true as of issuance.
- **`POST /v1/authz/check`** — live, and what the plan tells you to prefer for anything sensitive, because a role revoked a minute ago is still in an unexpired token.

```
POST /v1/authz/check
{ "subject": { "user_id": "…" }, "action": "read", "resource": { "type": "invoice" } }
→ { "allowed": true, "matched_policy": "billing-admin", "reasons": [...] }
```

`resource.id` and `resource.attributes` are accepted and ignored today; sending them now means no client change when policy evaluation arrives (Phase 4b).

### 6. Administer from your own systems

Everything the console does is a documented `/v1` route (`docs/API/`). Management calls need a token whose user holds the right manager role; the permission for every route is listed in `docs/AUTHORIZATION/05-ROUTE-PERMISSION-TABLE.md`.

## Rules and Defaults

| Rule | Value | Where |
|---|---|---|
| PKCE | `S256` required; `plain` refused | `docs/IDENTITY-PROTOCOL/02-OAUTH21-AUTHORIZATION-SERVER.md` |
| Redirect URIs | Exact match | application registration |
| Client secret | Shown once; stored hashed | `docs/API/11-PROJECT-API.md` |
| Refresh rotation | New token per exchange; reuse revokes the family | `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md` |
| Live check requirement | `MEMBER` — any valid token in the organization | `docs/AUTHORIZATION/05-ROUTE-PERMISSION-TABLE.md` |
| Pagination | `page_size` with an opaque `page_token`; follow it to the end | `docs/API/06-PAGINATION.md` |
| Idempotency | `Idempotency-Key` on unsafe requests | `docs/API/07-IDEMPOTENCY.md` |

## Security Considerations

- Validate `aud` and the token type, not just the signature: a valid token for another application is still a valid signature.
- Never put a client secret in a browser; use PKCE and a public client.
- Treat `/v1/authz/check` results as decisions about *your* organization's project only — the endpoint answers within the caller's tenant.

## Not Yet Built / Open Questions

- **Delegated roles from a Project Grant do not appear in tokens or in `/v1/authz/check` yet** (`P4-04`). Delegation today is administrative only: a partner can be given roles, but those roles do not grant access.
- No SAML, no social login, no webhooks, no published SDK.

## Related Documents

- `docs/API/`, `docs/IDENTITY-PROTOCOL/`, `docs/SESSION-MANAGEMENT/`, `docs/AUTHORIZATION/`.
- Public guides under `public-site/docs/guides/`.
