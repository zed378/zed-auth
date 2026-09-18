# 01 - Backend Unit and Integration Tests

> Category: **Testing** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-15, P2-16, P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how Go tests are organised, what infrastructure integration tests use, and the fixtures that make them readable.

## Scope

`backend/`. Console tests are `02-...`; cross-package security tests are `04-...`.

## As Built

### Unit tests

Ordinary `go test` files beside the code. They cover pure logic: authorization resolution (`backend/internal/management/roles_test.go`), token and claim handling, password and recovery-code primitives, policy evaluation, pagination cursors, configuration loading.

One unit suite is worth singling out: `backend/internal/management/hierarchy_exhaustive_test.go` walks every combination of held role, scope id, required role, required scope and target — 23,520 of them — and compares `Authorize` against an expectation written from the plan's prose rather than from the implementation. Two independently written descriptions that agree on every input is a much stronger statement than one.

### Integration tests

Behind the `integration` build tag, so `go test ./...` stays fast and needs no Docker.

- **Infrastructure**: `backend/internal/testsupport/` starts real PostgreSQL and Redis with testcontainers, applies every migration, and exposes both DSNs — the owner connection and the restricted application connection. Tests that must prove a rule holds for *every* writer use the owner connection, which bypasses row-level security.
- **Fixtures**: a `Factory` creates instances, organizations, projects, applications, users, roles and grants with one call each, plus `Exec`/`TryExec` for the rare row a method would over-fit. `Truncate` resets state between tests.
- **Shape**: most suites assemble the real `/v1` chain and server (`httpserver.New` with real handlers) and drive it with `httptest`, so the middleware, the permission table, RLS and the handler are all in the path. A test that mocked the store would prove the handler returns an array.

### What integration tests are used for that unit tests cannot do

- Triggers and constraints (for example the delegation rules in migration `20260917000037`), tested from both the application and the owner connection.
- Row-level security, including the "a third organization sees nothing" property.
- Concurrency: the revoke-versus-assign race is tested by holding a row lock open in one transaction while a request runs in another.
- Idempotency, pagination and rate limiting, which live in the chain rather than in a handler.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Build tag for integration tests | `integration` | file headers |
| Containers per package | Started once per package (`StartForPackage`) | `backend/internal/testsupport/` |
| Test isolation | `Truncate` between tests rather than fresh containers | `testsupport` |
| Owner-connection tests | Used where a rule must bind every writer | e.g. `backend/internal/projectgrant/delegated_integration_test.go` |

## Security Considerations

Database-level rules are the last line when application code is wrong, so they are tested through the connection that ignores RLS. A rule that is only ever exercised as the application role proves less than it appears to.

## Verification

- `scripts/check.sh` § Go and § Integration run both layers; CI repeats them with the race detector.
- Coverage is measured once per run, and failing test names are printed rather than buried (`scripts/check-coverage.sh`).

## Not Yet Built / Open Questions

- No fuzz targets yet (`P4-15`).
- Integration tests are single-instance; nothing exercises two service instances against one database.

## Related Documents

- `docs/PLAN/11-TESTING.md`; `docs/DATABASE/`; `04-ABUSE-CASE-SECURITY-TESTING.md`.
