# 00 - SDK Architecture Overview

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: none — no phase in `TASKS/PROGRESS.md` schedules a published client SDK &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State plainly what exists today for integrating with the Auth Service, and specify — for whenever the work is scheduled — what a family of official client SDKs (Go, TypeScript, React) would need to provide without weakening the guarantees the API already makes.

## Why It Is Not Built Yet

No published SDK package exists anywhere in this repository. There is no `pkg/client`, no `@auth/sdk`, no `@auth/react`, and no top-level `sdk/` directory; `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` (Status: Implemented) says so directly under "Not Yet Built / Open Questions": *"No SAML, no social login, no webhooks, no published SDK."*

What exists instead, all confirmed in the code:

- **A generated TypeScript client for the console's own use**, not a distributable package: `console/src/lib/api/schema.gen.ts` is generated from `openapi/openapi.yaml` by `openapi-typescript` (`console/package.json` script `api:generate`), and `console/src/lib/api/client.ts` wraps it with `openapi-fetch`. It ships inside the console's bundle, is not published to a registry, and has no version of its own independent of the console (`04-GENERATED-API-CLIENTS`-equivalent detail lives in `02-TYPESCRIPT-SDK.md`).
- **The console's own hand-written OIDC/PKCE client**, not a reusable library: `console/src/lib/auth/` (`AuthProvider.tsx`, `oidc.ts`, `pkce.ts`, `tokens.ts`, `config.ts`) implements Authorization Code with PKCE the same way any `type: spa` client would have to, per `docs/PLAN/02` and `docs/PLAN/06` — "the console must log in through the same flow, with no endpoint, parameter, or exemption of its own" (`console/src/lib/auth/oidc.ts` header comment). It is application code, not an SDK: it is not importable outside the console and makes console-specific choices (token kept in a module-scoped variable, never `localStorage`, per ADR-019).
- **Generated validation patterns shared by both sides of the contract, not a client SDK**: `console/src/lib/api/patterns.gen.ts` and `backend/internal/role/pattern.gen.go` are both generated from the OpenAPI `PermissionKey`/`RoleKey` schemas by `console/scripts/gen-patterns.mjs`, and only re-implement two regular expressions — they do not wrap any endpoint.
- **Two demo consumer applications**, not a library either: `deploy/demo/docker-compose.yml` runs two Go binaries built from `demo/webapp` and `demo/spa` (module `demo/go.mod`, separate from `backend/`). `demo/README.md` describes them as "the reference a consumer team copies instead of writing their own JWT handling" — a worked example, not something `go get`-able. Their token verification lives in `demo/internal/verify/` (Go's `internal/` visibility rule already makes this unimportable from outside the `demo` module, which is deliberate: it is meant to be read and copied, not depended on).
- **`backend/internal/api/generate.go` and `backend/internal/api/oapi-codegen.yaml` explicitly turn off Go client generation** (`generate: client: false`), with the reasoning recorded in the config: *"No client here. The console generates a TypeScript client from the same spec; a Go client would be a third artifact with nobody to use it."* This is a standing decision, not an oversight — a future Go SDK would need to either revisit it or generate independently of the server's own `oapi-codegen.yaml`.

`TASKS/PHASE-4-ENTERPRISE-INTEROP.md` and every other phase file in `TASKS/` (`PHASE-0` through `PHASE-5`, `PHASE-F-FRONTEND-IMPLEMENTATION.md`) contain no line item for building a published Go, TypeScript, or React SDK. This is a genuine gap in the roadmap, not a scheduling detail this document can resolve — see Open Questions.

## Constraints Already Decided

These are not proposals; they are decisions already on record that any future SDK work must respect:

| Constraint | Source |
|---|---|
| `openapi/openapi.yaml` is hand-written and is the single source of the contract; it generates the Go server interface, the console's TypeScript client, and the public API reference | ADR-013, `openapi/README.md` |
| A generated Go client was considered and explicitly rejected ("nobody to use it") | `backend/internal/api/oapi-codegen.yaml` |
| Every capability exposed anywhere must also exist as a documented `/v1` or `/oauth` route — an SDK can only wrap what `openapi/openapi.yaml` already documents, never a shadow API | `CLAUDE.md` non-negotiable constraint (API-first, FR-14) |
| Authorization checks are server-side, always; a client-side permission helper is a UX convenience, never a security control | `CLAUDE.md` non-negotiable constraint |
| The console keeps its access token in memory only and renews it silently against the SSO session — not `localStorage`, not a cookie | ADR-019 |
| Refresh tokens rotate on every exchange; presenting a spent one outside the grace window revokes the whole family | `docs/SESSION-MANAGEMENT/02-REFRESH-TOKENS-AND-ROTATION.md`, `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` step 4 |

## Key Topics To Specify

- Which languages, in what order, and in which roadmap phase — currently undecided (see Open Questions).
- Generated-vs-hand-written split per language, mirroring the pattern already used successfully for the TypeScript client: models and request/response shapes generated from `openapi/openapi.yaml`; token handling, retry policy, and pagination/idempotency helpers hand-written on top.
- Distribution and versioning: how an SDK's version ties to the `openapi/openapi.yaml` version it was generated from, so a consumer can tell whether their SDK matches the deployed API.
- Error mapping from the `Error` envelope (`docs/API/04-ERROR-HANDLING.md`) to language-idiomatic exceptions or result types.
- Retry/backoff behavior consistent with `docs/API/05-RATE-LIMITING.md` (respecting `429` and `X-RateLimit-*`/`Retry-After`), and an explicit statement of what an SDK must never retry automatically (a `POST` without an `Idempotency-Key`, per `docs/API/07-IDEMPOTENCY.md`).
- Pagination helpers consistent with the cursor model in `docs/API/06-PAGINATION.md`.
- Token lifecycle helpers (local JWKS-based validation, refresh rotation) that reproduce the checks already proven correct in `demo/internal/verify/` rather than reinventing them per language.

## Acceptance Criteria

- [ ] A task exists in `TASKS/` with an assigned phase before any SDK implementation work starts, per `CLAUDE.md`'s "never build Phase N+1 while Phase N is incomplete" rule and the roadmap discipline in `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md`.
- [ ] Every SDK method corresponds 1:1 to a documented `operationId` in `openapi/openapi.yaml`; a static check fails the build if an SDK method has no matching operation, mirroring `scripts/openapi-shipped-paths.py`'s role for the API itself.
- [ ] Models and request/response types are generated, not hand-maintained, wherever a maintained generator exists for the target language.
- [ ] Any bundled local token-verification code passes the same negative-path tests as `demo/internal/verify/` — wrong `aud`, algorithm confusion, expired token, wrong `typ` — before it ships.
- [ ] Documentation for the SDK states explicitly, next to every permission-check helper, that the check is a UX convenience and the API re-verifies independently — no SDK doc may imply a client-side check is sufficient.
- [ ] Retry, pagination, and idempotency-key helpers are covered by conformance tests run against the real API's documented behavior in `docs/API/05-RATE-LIMITING.md`, `06-PAGINATION.md`, and `07-IDEMPOTENCY.md`.
- [ ] The published package version is pinned to the `openapi/openapi.yaml` version (or commit) it was generated from, and this is checked in CI.

## Open Questions

- Which languages beyond Go, TypeScript, and React, and in which phase — not decided anywhere in `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` or `TASKS/PROGRESS.md`. This document cannot invent a roadmap slot; flagging it here per `CLAUDE.md`'s "flag a gap rather than guess" instruction.
- Whether a Go client will ever be generated at all, given `backend/internal/api/oapi-codegen.yaml`'s explicit `client: false` and its stated reasoning — no ADR revisits this decision.
- Registry/namespace ownership (npm scope, Go module path) — not decided.

## Related Documents

- `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` — the actual, implemented path to integrate today.
- `docs/API/README.md` and `docs/API/04-ERROR-HANDLING.md`, `05-RATE-LIMITING.md`, `06-PAGINATION.md`, `07-IDEMPOTENCY.md` — the contract behavior any SDK must reproduce.
- `openapi/openapi.yaml`, `openapi/README.md` — the canonical contract.
- `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` — the console's own generation pipeline, the closest existing precedent.
- `MEMORY/DECISIONS.md` ADR-013, ADR-019.
- `docs/SDK/01-GO-SDK.md`, `02-TYPESCRIPT-SDK.md`, `03-REACT-AUTH-PROVIDER-AND-HOOKS.md`, `04-HTTP-MIDDLEWARE-SPECIFICATION.md`.
