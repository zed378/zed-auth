# backend/

The Auth Service: OIDC/OAuth2 provider, authentication, authorization, and the Management REST API. A single Go binary structured as a modular monolith.

**Governing documents**: `docs/PLAN/07-BACKEND-ARCHITECTURE.md` (structure and stack), `docs/PLAN/05-API-CONTRACT.md` (every endpoint), `docs/PLAN/04-DATA-MODEL.md` (schema), `docs/PLAN/08-AUTHORIZATION.md` (all authorization logic).

## Layout

| Path | Responsibility |
|---|---|
| `cmd/authservice/` | Entry point. Wiring only — no business logic |
| `internal/config/` | Configuration loading and validation. Fails fast on a missing required value |
| `internal/httpserver/` | Router, middleware chain, graceful shutdown |
| `internal/authn/` | Login, sessions, MFA (Phase 1 / Phase 3) |
| `internal/authz/` | RBAC, ABAC, decision logic (Phase 2 / Phase 4b) |
| `internal/oidc/` | OIDC/OAuth2 provider (Phase 1) |
| `internal/saml/` | SAML IdP (Phase 4) |
| `internal/management/` | Management REST API (Phase 1+) |
| `internal/storage/postgres/` | Authoritative persistence. Owns the tenant-scoped transaction helper |
| `internal/storage/redis/` | Cache, sessions lookup, rate limiting, authorization codes |
| `internal/audit/` | Append-only event writer |
| `internal/observability/` | Structured logging, metrics, tracing |
| `migrations/` | Versioned SQL migrations. Run as a separate step, never at startup |

The package boundaries exist so the monolith can be split later without a rewrite, per `docs/PLAN/07`. Cross-package calls go through interfaces defined by the consumer, not the provider.

## Commands

```bash
go build ./...
go test ./...
go vet ./...
```

## Rules That Are Not Negotiable

- Parameterized queries only — never string-concatenated SQL (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §8).
- Authorization is enforced server-side on every request, independently of any UI (`docs/PLAN/08-AUTHORIZATION.md`).
- Never log tokens, passwords, or raw `/v1/authz/check` resource attributes (`docs/PLAN/13-OBSERVABILITY.md`).
- Migrations are additive/backward-compatible by default (expand/contract, `docs/PLAN/14-DEPLOYMENT.md`).
