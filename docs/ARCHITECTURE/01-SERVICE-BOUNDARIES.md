# 01 - Service Boundaries

> Category: **Architecture** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-17, P0-18, P1-15, P1-29 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State what each deployable unit owns, what it must never do, and which of those rules are enforced automatically.

## Scope

The boundaries between the service, the console, the public site and consumer applications. Tenant boundaries inside the service are `docs/MULTI-TENANCY/`.

## As Built

| Unit | Owns | Must never |
|---|---|---|
| `authservice` | Authentication, token issuance, tenant data, authorization decisions, audit | Trust a client-supplied organization id; serve an undocumented path |
| Console (`console/`) | Administrative UI | Call an endpoint that is not in the contract; treat a hidden control as a security measure; hold a client secret |
| Public site (`public-site/`) | Marketing, guides, generated API reference | Describe a capability that has not shipped; share code with the console |
| Demo apps (`deploy/demo`) | Proof that SSO works for real consumers | Be relied on in production |
| Consumer applications | Their own product's authorization decisions, using claims or `/v1/authz/check` | Assume a token from another application is valid for them |

### Enforced rather than agreed

- **The console has no private API.** Every capability it uses is a documented `/v1` route; an E2E test watches the network traffic and fails if the console calls a path the contract does not document (`console/e2e/consistency.spec.ts`).
- **Served paths equal documented paths** by construction: the router is generated from `openapi/openapi.yaml` (ADR-013), and `scripts/openapi-shipped-paths.py` keeps the shipped list explicit.
- **No shared code between the public site and the console** — a CI gate asserts it, so a shared component cannot leak console behaviour into a public page.
- **The public site cannot claim unshipped capabilities**: a capability audit runs over the built site in CI (`scripts/check.sh`, `public-site/CLAIMS.md`).
- **The console is a public OIDC client**: it holds no secret and uses authorization code with PKCE, like any other browser application.

### What crosses a boundary

- Browser → service: hosted login pages, the OAuth endpoints, and `/v1` with a bearer token.
- Console → service: `/v1` only, with a bearer token obtained through the standard flow.
- Service → Postgres: always inside a tenant transaction (or an explicitly audited instance-scoped one).
- Service → Redis: cache and counters only; never a source of truth.
- Service → SMTP: invitations, password resets and notifications.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Console API surface | Documented `/v1` paths only | `console/e2e/consistency.spec.ts` |
| Public site claims | Only shipped capabilities | capability audit in `scripts/check.sh` |
| Shared code between site and console | None | dedicated gate in `scripts/check.sh` |
| Client secrets in browsers | Never; public clients use PKCE | `backend/internal/oauth/`, `console/src/lib/auth/` |

## Security Considerations

- A boundary that is only a convention drifts. The four gates above exist because each of these rules had a plausible way to be broken quietly: an extra `fetch`, a copied component, a marketing sentence, a secret pasted into a bundle.
- `docs/SECURITY/00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md` numbers these boundaries (TB-x); the attack scenarios reference those numbers.

## Verification

- `console/e2e/consistency.spec.ts`, `console/e2e/shell.spec.ts`.
- `scripts/check.sh` — public-site capability audit, shared-code gate, shipped-paths gate.

## Not Yet Built / Open Questions

- Webhooks (`P4-12`) will add an **outbound** boundary the system does not have today: the service calling a customer-controlled URL. Its threat surface (SSRF, replay) is covered in `docs/WEBHOOK/`.

## Related Documents

- `docs/PLAN/03-ARCHITECTURE.md`, `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`.
- `00-SYSTEM-ARCHITECTURE.md`, `03-FRONTEND-ARCHITECTURE.md`, `04-PUBLIC-SITE-ARCHITECTURE.md`.
