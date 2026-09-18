# 02 - TypeScript Client

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-16 (generated client, done); no task schedules a published `@auth/sdk` package &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the TypeScript API client that actually exists — generated for, and consumed only by, the console — and separate it clearly from a published, installable TypeScript SDK, which does not exist.

## Scope

This document covers `console/src/lib/api/` (the generated client and its wrapper) as it is used inside `console/`. It does not cover the console's authentication code, which is `03-REACT-AUTH-PROVIDER-AND-HOOKS.md`'s subject, nor any HTTP middleware, which is `04-HTTP-MIDDLEWARE-SPECIFICATION.md`'s.

**There is no `@auth/sdk` npm package.** Nothing under `console/` is published to a registry; it ships only inside the console's own built bundle. An integrator outside this repository cannot `npm install` anything from this project today — see "What To Do Today" below.

## As Built

The console's TypeScript client is generated, not hand-written, and consumed entirely inside `console/`:

1. **`console/src/lib/api/schema.gen.ts`** is generated from `openapi/openapi.yaml` by `openapi-typescript` (`console/package.json`, script `api:generate`: `openapi-typescript ../openapi/openapi.yaml --default-non-nullable false -o src/lib/api/schema.gen.ts`). It is committed, and `scripts/check.sh` fails the build if it has drifted from the spec (ADR-013's guarantee applied to the console side of the contract).
2. **`console/src/lib/api/client.ts`** wraps the generated `paths` type with `openapi-fetch` (`createClient<paths>(...)`), giving every call full request/response typing without a hand-maintained interface. Per its own header comment: *"Never hand-write a request against a path string. A hand-written call is exactly the drift the generation exists to prevent."*
3. **Base URL** is same-origin by default, resolved to `window.location.origin` when `VITE_API_BASE_URL` is not set at build time — deliberately not runtime-configurable, since the console is a static bundle on a CDN and a mutable API base URL would let anyone who can influence it redirect every bearer token the console holds (`client.ts` comments).
4. **`credentials: "omit"` on every call** — the console authenticates purely with a bearer token; it never sends cookies to `/v1/*`, and `/v1/*` never reads one (`client.ts`, citing P1-29).
5. **The bearer token is attached per request, read from `console/src/lib/auth/tokens.ts` at call time**, not captured once at client construction — because the token is renewed in place (ADR-019) and a client built with a stale closure over the first token would keep presenting an expired one.
6. **`console/src/lib/api/patterns.gen.ts`** is a second, separate generated artifact: two regular expressions (`PermissionKeyPattern`, `RoleKeyPattern`) generated from the OpenAPI schema by `console/scripts/gen-patterns.mjs`, so the console's client-side format validation cannot drift from the server's. The same schema also generates `backend/internal/role/pattern.gen.go` for the Go side — one schema, two generated targets, no third hand-maintained copy. This is validation-pattern generation, not part of the request client, and is not itself a distributable package either.
7. **`console/src/lib/api/queries.ts` and `settings.gen.ts`** exist alongside the client but are console application code (TanStack Query hooks and generated settings) rather than part of a general-purpose SDK surface; they are out of scope for this document.

## What To Do Today (for an integrator outside this repository)

Since none of this is published:

1. **Generate your own TypeScript client** the same way the console does: run `openapi-typescript` against `openapi/openapi.yaml` to get typed request/response models, and call the API with `openapi-fetch` or any typed fetch wrapper of your choice. This reproduces exactly what `console/package.json`'s `api:generate` script does, against the same spec.
2. **Use a standard OIDC client library** for the browser-based PKCE flow (for example a maintained library such as `oidc-client-ts`, or an equivalent for your framework) rather than reimplementing PKCE by hand, unless you specifically need the console's exact behavior — in which case `console/src/lib/auth/oidc.ts` and `pkce.ts` are readable reference code, not an importable package (see `03-REACT-AUTH-PROVIDER-AND-HOOKS.md`).
3. **Follow `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`** for the endpoint sequence: register an application, run the authorization code + PKCE flow, validate tokens against the published JWKS, and call `/v1/authz/check` for anything sensitive.

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Client generation source | `openapi/openapi.yaml`, no other source of truth | `console/package.json` `api:generate` |
| Generated file drift check | Build/CI fails if `schema.gen.ts` does not match the spec | `scripts/check.sh` |
| API base URL | Build-time only (`VITE_API_BASE_URL`), same-origin fallback; never runtime-configurable | `console/src/lib/api/client.ts` |
| Credentials on API calls | `omit` — bearer token only, no cookies | `console/src/lib/api/client.ts` |
| Token source | Read per-request from `console/src/lib/auth/tokens.ts`, never captured at client construction | `console/src/lib/api/client.ts` |
| Validation pattern generation | `PermissionKeyPattern`/`RoleKeyPattern` generated once from the OpenAPI schema, consumed identically by console and backend | `console/scripts/gen-patterns.mjs`, `console/src/lib/api/patterns.gen.ts`, `backend/internal/role/pattern.gen.go` |
| Published npm package | None exists | — |

## Interfaces

Not applicable as a public interface — `console/src/lib/api/client.ts` exports a single `api` client instance and a `setUnauthorizedHandler`/`onUnauthorized` pair (per the file's own contents) for internal console use; it is not a package with a versioned public API. The types it exposes are exactly the `paths` type generated from `openapi/openapi.yaml`, which is the actual interface — see `docs/API/` for the endpoint-by-endpoint contract those types describe.

## Security Considerations

- **No credentialed cross-origin requests**: `credentials: "omit"` means a CSRF-style attack cannot ride an ambient cookie on a Management API call — the bearer token is the only credential, and it must be explicitly attached (`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §5, CSRF).
- **Base URL is not attacker-influenceable**: fixed at build time or same-origin, closing the redirect-token exfiltration path a runtime-configurable base URL would open.
- **Client-side validation is UX only**: `patterns.gen.ts`'s regular expressions save a round trip on an obviously invalid key; the server (`backend/internal/role/pattern.gen.go`, same pattern) is the actual enforcement point, consistent with `CLAUDE.md`'s "authorization checks are server-side, always" — the same principle applies to input validation generally.

## Verification

- `scripts/check.sh` — fails the build if `schema.gen.ts` or `patterns.gen.ts` has drifted from `openapi/openapi.yaml`.
- `console/src/lib/auth/auth.test.ts` — covers the auth layer that sits alongside this client (see `03-REACT-AUTH-PROVIDER-AND-HOOKS.md` for what it tests).
- No dedicated test suite exists for a published TypeScript SDK, because none is published; there is nothing external to verify.

## Not Yet Built / Open Questions

- **No published `@auth/sdk` package.** Everything above is internal to `console/`'s own build; there is no npm package, no independent versioning, and no support surface for an external consumer.
- No task in `TASKS/` schedules extracting this into a publishable package. Flagged as a gap rather than assumed — see `00-SDK-ARCHITECTURE.md`.
- If this is ever built, `00-SDK-ARCHITECTURE.md`'s acceptance criteria (generated models, `operationId` coverage checks, version pinned to the spec) apply.

## Related Documents

- `docs/SDK/00-SDK-ARCHITECTURE.md`
- `docs/SDK/03-REACT-AUTH-PROVIDER-AND-HOOKS.md`
- `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`
- `docs/PLAN/06-FRONTEND-ARCHITECTURE.md`
- `openapi/openapi.yaml`, `openapi/README.md`
- `MEMORY/DECISIONS.md` ADR-013, ADR-019
