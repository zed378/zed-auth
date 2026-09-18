# 04 - HTTP Middleware Specification

> Category: **SDK** (`docs/SDK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: none — no phase in `TASKS/PROGRESS.md` schedules published HTTP middleware &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State what a backend service integrator should do today to validate tokens and enforce authorization at the edge of their own service, with no official middleware package available, and specify what future middleware packages (Go `chi`, Express, Next.js, or others) would need to provide.

## Why It Is Not Built Yet

No middleware package exists in this repository for any framework or language. There is no `r.Use(authmiddleware.RequireAuth(...))`-style helper, no `authMiddleware({ domain: ... })` for Express, and nothing published for Next.js API routes.

What exists instead is per-application, hand-written verification, proven correct for exactly two consumer applications:

- `demo/internal/verify/` implements a `Verifier` (constructed with `verify.New(cfg.issuer, cfg.clientID)`) that both demo applications use directly in their own request-handling code (`demo/webapp/main.go`, `demo/spa/main.go`) — it is not wired in as a generic middleware layer, and it lives in an `internal/` Go package inside the `demo/go.mod` module, which cannot be imported from outside that module.
- Nothing in `backend/` provides a middleware helper for a *different* service to import either — `backend/internal/management/` implements the Auth Service's own request pipeline (`chain.go`'s `Require -> RateLimit -> BufferBody -> Idempotency -> AuditGuard` chain, `docs/API/00-API-OVERVIEW.md`), which enforces the Auth Service's own routes and is not packaged for reuse by an unrelated downstream service.

`docs/DEVELOPER/03-INTEGRATION-GUIDE.md`, the implemented and verified integration path, describes token validation as something an integrating application does itself (step 3: "Fetch and cache the JWKS from `jwks_uri`, then check, on every request: the signature, `iss`, `aud`, `exp`, and the token type") — it does not point to any middleware package, because none exists.

## What To Do Today

1. **Write your own request-level check**, following exactly the sequence `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` step 3 describes and `demo/internal/verify/verify.go` implements: extract the bearer token, verify its signature against the cached JWKS, and check `iss`, `aud` (your own `client_id`), `exp`, and token type — never derive the verification algorithm from the token's own header.
2. **Use a standard JWT/JOSE library for your language** to do the cryptographic verification (signature checking, key parsing) rather than implementing it by hand; `demo/internal/verify/` is standard-library-only by choice for the demo, not a recommendation against using a library elsewhere.
3. **Wrap that check in whatever middleware convention your framework already has** (Go `net/http` middleware, Express middleware, a Next.js API route helper) — this repository does not provide one, but the check itself is small and framework-independent, matching `demo/README.md`'s description of `internal/verify/` as "about 200 lines, standard library only, and the only security control either [demo] application has."
4. **Decide access with `/v1/authz/check` for anything sensitive**, not token claims alone — `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` step 5 explains the freshness trade-off: a role revoked a minute ago is still present in an unexpired token.

## Constraints Already Decided

- **The algorithm is pinned, never chosen from the token.** `demo/internal/verify/` compares `alg` against an accepted allow-list; it does not dispatch on the header, which is what makes `alg: none` and algorithm-confusion attacks possible in a naive implementation.
- **`typ` must be checked**, not just presence of a JWT shape — the service marks access tokens `at+jwt` (RFC 9068) specifically so an access token cannot be used where an ID token is expected.
- **`iss` is an exact string comparison**, not a prefix or substring match.
- **`aud` must contain the caller's own `client_id`.** This is the check that prevents one compromised low-value client's token from being accepted by a higher-value one.
- **No network call is required once the key set is cached** — `docs/PLAN/12-PERFORMANCE.md`'s largest available latency win, proven in the demo apps by `TestVerifyingMakesNoNetworkCallOnceTheKeySetIsCached` (`demo/internal/verify`). Any future middleware must preserve this property rather than calling back to the Auth Service on every request.
- **Middleware can only ever be a UX/performance convenience, never the authorization boundary.** `CLAUDE.md`'s non-negotiable constraint applies here without exception: the Auth Service's own API independently enforces every permission check regardless of what a downstream service's middleware decided.

## Key Topics To Specify

- Target frameworks and languages, and the order they would be built in — not decided anywhere in the roadmap.
- Exact context/request-object injection contract (what a downstream handler receives after the middleware runs: user id, org id, decoded claims) per framework.
- Configuration surface: issuer URL, expected audience, JWKS cache TTL and refresh-on-`kid`-miss behavior (`docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`'s rotation model is what any cache must tolerate without downtime).
- How the middleware would optionally call `/v1/authz/check` for a caller that wants live authorization rather than claims-only, and how it communicates the associated latency trade-off rather than hiding it.
- Error/response shape on rejection (401 vs 403, and what body if any) for each target framework's idioms.

## Acceptance Criteria

- [ ] The verification core (signature, `alg` allow-list, `iss`, `aud`, `exp`, `typ`) is implemented once per language and shared across every framework-specific adapter for that language, not duplicated per framework.
- [ ] A test suite reproduces every negative case `demo/internal/verify/` already covers: wrong `aud`, algorithm confusion, expired token, wrong `typ`, and a signature computed over a re-encoded (rather than original) header/payload.
- [ ] No request path requires a network call once the JWKS is cached; a test counts outbound requests and fails if verification alone triggers one, matching `TestVerifyingMakesNoNetworkCallOnceTheKeySetIsCached`'s intent.
- [ ] JWKS rotation (`docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`) is handled without downtime — a token signed with a key rotated in after the cache was last refreshed is still accepted, verified by a test that rotates the key mid-suite.
- [ ] Documentation for every middleware package states plainly, next to the injected context/claims, that they are not an authorization decision and the API re-checks independently.
- [ ] A named task with an assigned roadmap phase exists in `TASKS/` before implementation begins.

## Open Questions

- Whether this work is scheduled at all, and for which frameworks first — undecided, no roadmap entry.
- Whether an optional `/v1/authz/check`-backed mode belongs in the same package or a separate one, given the latency and freshness trade-off `docs/DEVELOPER/03-INTEGRATION-GUIDE.md` already documents.

## Related Documents

- `docs/SDK/00-SDK-ARCHITECTURE.md`, `01-GO-SDK.md`
- `docs/DEVELOPER/03-INTEGRATION-GUIDE.md`
- `demo/internal/verify/verify.go`, `signature.go`, `demo/webapp/main.go`, `demo/spa/main.go`, `demo/README.md`
- `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md`
- `docs/API/02-AUTHENTICATION-AND-AUTHORIZATION.md`
- `docs/PLAN/12-PERFORMANCE.md`
