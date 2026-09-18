# 02 - Backend Architecture

> Category: **Architecture** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P1-15, P2-05, P3-03, P4-01 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Map the Go service: its binaries, its packages, and the layering that keeps protocol code, management code and storage code apart.

## Scope

Internals of `backend/`. The contract is `docs/API/`; the schema is `docs/DATABASE/`.

## As Built

### Binaries (`backend/cmd/`)

| Binary | Purpose |
|---|---|
| `authservice` | The service. Wires every dependency and starts the public and admin listeners |
| `migrate` | Applies and rolls back migrations, run as the database owner role |
| `keyctl` | Signing-key operations (see `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`) |
| `passwordhash` | Hashes a password from the environment, for bootstrap and fixtures — never from argv, which is visible in `ps` |

### Packages (`backend/internal/`)

**Protocol and sign-in**

- `oauth/authorize`, `oauth/token` — the authorization and token endpoints, PKCE, refresh rotation.
- `oidc` — discovery and JWKS.
- `login` — the hosted login pages, the password step and the MFA challenge.
- `authn` — organization policies: password rules, session lifetime, permitted sign-in methods, the MFA mandate and its grace (`MandateCheck`).
- `session` — the sign-in session behind SSO.
- `mfa` — TOTP, WebAuthn and recovery codes; `mfaapi` exposes them over `/v1`.
- `signing` — key material, signer and verifier, token types.

**Management API**

- `management` — the cross-cutting chain every `/v1` route runs through: authentication, `Authorize`, the `Policy` table, tenant scoping, pagination, idempotency, rate limiting, the error envelope and the audit guard.
- Resource packages, one per family: `organization`, `project`, `application`, `role`, `grant`, `projectgrant`, `user`, `sessionapi`, `account`, `auditlog`.

**Cross-cutting**

- `authz` — the live permission check and its Redis cache.
- `audit` — the append-only event writer.
- `ratelimit`, `anomaly`, `mail`, `observability`, `config`, `httpserver`, `storage/postgres`, `api` (generated), `testsupport`, `docsdrift`.

### Layering rules

1. A handler never opens its own connection: it runs inside the tenant transaction the chain established (`management.InScope` → `postgres.WithTenant`).
2. A store never decides permission; a handler never writes SQL that bypasses its store.
3. Packages do not import in a ring. When `projectgrant` needed the direct-grant row shape, it grew its own type rather than importing `grant`, because `grant`'s tests wire `projectgrant` into a server (the cycle appeared as a real compile error during `P4-02`).
4. Dependencies are injected as interfaces at wiring time (`backend/cmd/authservice/main.go`), which is what lets a test assemble the same server with fakes. Helper constructors return a **nil interface** rather than a typed nil, because a typed nil in an interface is not nil — a bug that reached staging once (`MEMORY/records/2026-09-15-P3-15-acceptance.md`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Generated server interface | `oapi-codegen` strict server; regenerate and diff in CI | `backend/internal/api/`, `scripts/check.sh` |
| Every `/v1` route declares a permission | Missing entry ⇒ unreachable | `backend/internal/management/policy.go` + route test |
| Database access | Tenant transaction, or an audited instance-scoped one | `backend/internal/storage/postgres/` |
| Logging | Structured `slog`; no tokens, passwords or resource attributes | log-hygiene gates in `scripts/check.sh` |
| Integration tests | Real Postgres and Redis via testcontainers, behind the `integration` build tag | `backend/internal/testsupport/` |

## Interfaces

- Wiring: `backend/cmd/authservice/main.go` (also the place a missing dependency panics at startup rather than nil-pointering at request time).
- Configuration: `backend/internal/config/` — environment variables, secret references, defaults.

## Security Considerations

- The chain is the single place authentication and permission run, so a new endpoint cannot forget them: it can only fail closed (`docs/AUTHORIZATION/05-ROUTE-PERMISSION-TABLE.md`).
- Architecture tests pin a few invariants that reviews miss, for example that the login handler is wired with its MFA challenger (`backend/internal/login/architecture_test.go`) and that wiring stays complete (`backend/cmd/authservice/wiring_test.go`).

## Verification

- Package-level unit tests; integration tests per package under the `integration` tag.
- `backend/tests/security/` — isolation and RLS behaviour across packages.
- `scripts/check.sh` — build, vet, generated-code drift, log hygiene, migration rules, coverage.

## Not Yet Built / Open Questions

- SAML, social login and webhooks have no packages yet (`P4-07`…`P4-12`).
- Tracing is configured but thin; see `docs/OBSERVABILITY/04-DISTRIBUTED-TRACING.md`.

## Related Documents

- `docs/PLAN/07-BACKEND-ARCHITECTURE.md`; `docs/API/`; `docs/DATABASE/`; `docs/TESTING/`.
