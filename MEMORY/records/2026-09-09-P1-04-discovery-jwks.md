# P1-04 — Discovery Document and JWKS Endpoint

| | |
|---|---|
| **Date** | 2026-09-09 |
| **Task** | `TASKS/PHASE-1-MVP-CORE-AUTH-SSO.md` § P1-04 |
| **Phase** | Phase 1 |
| **Surface** | backend |
| **Author** | Zed |
| **Commits / PR** | `feat/P1-04-discovery-jwks` |
| **Status** | Partially completed — two DoD items depend on P1-06/P1-07 and remain unticked |

---

## What Changed

The service now serves `GET /.well-known/openid-configuration` and `GET /.well-known/jwks.json`. Both are declared in `openapi/openapi.yaml` and served through the generated strict router, so they are part of the contract rather than routes registered beside it. The discovery document is **derived** from a `Capabilities` value describing what this build actually implements, and the JWKS is rendered per request from `P1-03`'s key cache.

Three things changed around the endpoints to make them behave correctly. `AUTH_JWT_SIGNING_KEY_REF` is now actively **refused** at startup rather than merely ignored — since `P1-03` keys come from the `signing_keys` table, a deployment still setting it holds a false belief about where its key comes from. `HeadAsGet` routes HEAD to the GET handler, because chi matches methods exactly and every endpoint was answering 405 to HEAD. And `Server.Handler()` now returns the served handler instead of the bare mux, so tests exercise what production serves.

## Why

`docs/PLAN/05-API-CONTRACT.md` § Core Endpoints and `docs/PLAN/03-ARCHITECTURE.md` § OIDC/OAuth2 Provider. The point of discovery is that an integrator points a library at a URL instead of copying values out of a wiki page that has been wrong since the last rotation.

That only works if the document is true, which makes this a governance problem more than a serialisation one. `P1-04` step 2 and `CLAUDE.md`'s rule about never describing unshipped capability apply harder to a machine-readable document than to marketing copy: a human reading a brochure is sceptical, a client library is not. A client that configures successfully from a document naming `/oauth/token` and then fails at its first login produces an error in the *consumer's* logs, not ours.

## How

`internal/oidc.Capabilities` is a struct of facts about running code, and the document is rendered from it. There is no JSON literal anywhere in the package — that would be a second source of truth that starts correct and drifts on the first release where an endpoint moves. An endpoint that does not exist leaves its field empty, `optional()` leaves the generated pointer nil, and the field is absent from the JSON entirely.

So `main.go` currently sets only `Issuer`, `JWKSURI` and `SigningAlgorithms`. The authorization and token endpoints arrive in `P1-06`/`P1-07`, and until then it is *not possible* to advertise them: nothing else writes those fields.

`Validate()` runs at startup, so a misconfiguration is a refusal to boot rather than a document that quietly lies to every client that reads it. It rejects an empty issuer, an issuer with a trailing slash (clients compare `iss` byte for byte), a missing `jwks_uri`, and any appearance of `implicit` or `password` in the grant list — `docs/PLAN/05` rules both out permanently, and checking at construction means a grant added to a slice far from this file fails loudly instead of being published.

`code_challenge_methods_supported` is `S256` and is only emitted once there is an authorization endpoint to apply it to. `plain` is in the PKCE spec and is deliberately absent: it transmits the verifier unprotected, which removes the entire reason PKCE exists, and advertising it invites a client library to negotiate down to it.

The JWKS is built per request from the cache rather than pre-rendered, because the key set changing on rotation is the event this endpoint exists to communicate; the cache underneath makes that a memory read. `toGeneratedJWKS` walks go-jose's marshalled *output* rather than the keys themselves, so nothing private can reach the response even if a future change to the signing package got it wrong.

Cache lifetimes are derived rather than picked: one hour for discovery (it changes on deploy), five minutes for the JWKS — matching the service's own key-cache TTL, so the window in which any consumer holds a stale key set is bounded by the same value on both sides.

**The non-obvious part** is where the handlers live. The generated `StrictServerInterface` covers every documented endpoint, and those are implemented by two packages: the probes in `httpserver`, discovery in `oidc`. Go cannot express "these two types satisfy this interface between them", so `httpserver.apiRoutes` embeds both and carries the assertion. Registering discovery by hand on the router would have been simpler and would have put these two endpoints outside ADR-013's guarantee that the served paths are exactly the documented ones — which is precisely where they must not be, since a consumer's only view of this service *is* the spec.

## Files and Components Touched

| Path | Change |
|---|---|
| `backend/internal/oidc/discovery.go` | New — `Capabilities`, `Validate`, document derivation, JWKS conversion, cache policy |
| `backend/internal/oidc/generated.go` | New — the two generated strict-interface methods |
| `backend/internal/oidc/discovery_test.go` | New — 12 tests |
| `openapi/openapi.yaml` | Both endpoints, plus `OpenIDConfiguration`, `JWK`, `JWKS` schemas |
| `backend/internal/api/*` | Regenerated |
| `scripts/openapi-shipped-paths.py` | SHIPPED set extended with the two well-known paths |
| `backend/internal/httpserver/server.go` | `apiRoutes` composite; discovery registered through the generated router; `Handler()` returns the served handler |
| `backend/internal/httpserver/health.go` | Interface assertion moved to the composite |
| `backend/internal/httpserver/middleware.go` | `HeadAsGet` |
| `backend/internal/httpserver/server_test.go` | HEAD routing, unsupported-method control, discovery→JWKS wiring |
| `backend/cmd/authservice/main.go` | Capabilities wiring; handler construction fails startup |
| `backend/internal/config/config.go` | `AUTH_JWT_SIGNING_KEY_REF` now refused with a remedy |
| `deploy/vm/docker-compose*.yml`, `.env.example`, `deploy/vm/SECRETS.md` | Retired variable removed |
| `console/src/lib/api/schema.gen.ts`, `public-site/docs/api-reference/*` | Regenerated from the spec |
| `public-site/package.json`, `scripts/check.sh` | `api:generate` cleans first, so the staleness gate covers the sidebar too |

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| The discovery document is derived from `Capabilities`, never written as a literal | A literal is a second source of truth that drifts; deriving makes advertising an unimplemented endpoint structurally impossible | — |
| Discovery is served through the generated router, not registered beside it | ADR-013's guarantee is that served paths equal documented paths; the two endpoints whose whole purpose is discoverability must not be the exception | ADR-013 |
| `AUTH_JWT_SIGNING_KEY_REF` refused rather than ignored | An ignored variable lets an operator believe the key comes from a file when it comes from the database — a belief that would surface during an incident | — |
| HEAD routed to GET globally, rather than per endpoint | chi's exact method matching makes 405-on-HEAD a whole-router property; fixing it per route would leave the next endpoint broken | — |
| `id_token_signing_alg_values_supported` lists both RS256 and ES256 | Both are implemented by the signer and the verifier, and either may be the current key after a rotation; the list describes the service's capability, not the current key set | — |

## Deviations from the Plan

None.

## Tests Added

| Layer | What it covers |
|---|---|
| Unit | Unimplemented endpoints absent entirely; endpoints appear when implemented; `S256` present and `plain` absent; PKCE not advertised without an authorization endpoint; forbidden grants rejected at construction; trailing-slash issuer rejected; issuer and `jwks_uri` required; JWKS carries no private parameters; cache headers set and JWKS shorter than discovery; content types; JWKS failure reveals nothing; always-present fields |
| Integration | Discovery document followed to the key set through a real HTTP server — `jwks_uri` is fetched the way a client library would fetch it, and the returned key set must contain the current `kid` |
| E2E | — |
| Security | JWKS private-parameter inspection; the 503 path asserting no infrastructure detail leaks to an anonymous caller |

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Private key material leaked through the JWKS | `docs/SECURITY/02` § Information Disclosure | `TestJWKSExposesNoPrivateParameters` |
| Dependency failure disclosed to an anonymous caller | `docs/SECURITY/02` §12 | `TestJWKSFailureRevealsNothing` |
| Client negotiated down to PKCE `plain` | `docs/SECURITY/02` §1 | `TestPKCEAdvertisesS256AndNeverPlain` |
| A deprecated grant advertised and then built against | `docs/PLAN/05` § Supported Grant Types | `TestForbiddenGrantsAreRejectedAtConstruction` |

## Definition of Done Verification

- [x] Tests at the appropriate pyramid layer
- [x] Every abuse case has an automated test
- [x] OpenAPI spec updated and generated server rebuilt
- [x] No sensitive action here to audit — both endpoints are public and read-only
- [x] Nothing sensitive is logged
- [x] `scripts/check.sh` green
- [x] `TASKS/PROGRESS.md` and the phase file updated

Task-specific DoD:

- [ ] **An off-the-shelf OIDC client library configures successfully from the discovery URL alone.** Not claimed. A conforming library needs `authorization_endpoint` and `token_endpoint` to complete configuration, and those arrive with `P1-06`/`P1-07`. Ticking this now would be exactly the false claim the rest of this task exists to prevent. The verifiable half — a client following `jwks_uri` from the document and reaching a real key set — is covered by `TestDiscoveryDocumentLeadsToTheKeySet`.
- [x] `code_challenge_methods_supported` contains `S256` and does not contain `plain`.
- [x] Neither `implicit` nor `password` appears in `grant_types_supported`.
- [ ] **`issuer` matches the `iss` claim, asserted by an integration test.** Nothing emits an `iss` claim yet — the token issuer is `P1-07`. What is asserted today is the single-source property: the document publishes `cfg.Issuer` verbatim, unnormalised. `P1-07` must issue from the same value and tick this then.
- [x] JWKS exposes no private key parameters, asserted by a test that inspects the JSON keys.

## What Did Not Work

**`curl -I` against the deployed service reported `Cache-Control: no-store` on the JWKS endpoint.** That looked like a caching bug in the handler and was not: chi matches methods exactly, so HEAD reached no route at all and the 405 carried the middleware's default headers instead of the handler's. The endpoint's own GET response had been correct the whole time. Fixed with `HeadAsGet`, which clones the request before rewriting the method — `net/http` decides whether to suppress the response body from the *original* method, so mutating in place produces a HEAD response with a body.

**The first attempt at a HEAD test passed against a server that was broken.** `Server.Handler()` returned the bare chi mux rather than the handler actually being served, so the test bypassed the very wrapper it was meant to exercise. Same shape as the vacuous-check failures collected in earlier records: the check ran, reported success, and covered nothing.

**The public site's API-reference staleness gate passed while the site was broken.** Adding two endpoints made `docusaurus gen-api-docs` write two operation pages and a new `Discovery` tag page — but not a new `sidebar.ts`, which it leaves alone when one already exists. The tag page calls `useCurrentSidebarCategory()`, so with no sidebar entry the site build failed on a page it could not place. The gate had already reported "generated API reference matches the spec", because it diffs the directory after running the same incomplete generator. `api:generate` now cleans before it generates, which makes both the gate and the build honest. A staleness check that only sees the files its generator chooses to overwrite is not a staleness check.

**Registering the two routes directly on the mux** was the first implementation and worked. It was replaced because it put the endpoints outside the generated router, and therefore outside the guarantee that served paths match documented ones — a shortcut that costs nothing today and silently produces an undocumented endpoint later.

## Follow-Ups and Open Questions

- `P1-06`/`P1-07` must set `AuthorizationEndpoint`, `TokenEndpoint`, `ResponseTypes`, `GrantTypes` and `Scopes` on the capabilities value, and tick this task's two remaining DoD items. Nothing else needs to change for the document to grow.
- `P1-07` must issue `iss` from `cfg.Issuer` — the same value discovery publishes — and add the integration test that compares them.
- `jwt-signing-current.pem` is still present in the VM secrets directory and is now referenced by nothing. Remove it once P1-04 is confirmed stable in staging.

## What to Watch

**A stale discovery document after `P1-06`/`P1-07` ship.** Clients may cache it for an hour, so a consumer configured in the hour before the deploy will not see the new endpoints until its cache expires. This is the intended trade, but it means the first hour after those tasks land is not a good window to judge whether integration works.

**An empty JWKS on a fresh deployment.** The service deliberately starts with no keys (`keyctl generate && keyctl rotate` has not run yet) and logs a warning rather than refusing to boot, because refusing would make bootstrap impossible. The symptom downstream is every consumer failing verification with "no matching key". The startup log line `no signing keys are available` is the first place it shows.

**A `jwks_uri` that stops resolving.** It is assembled as a string in `main.go` while the route comes from the generated router. `TestDiscoveryDocumentLeadsToTheKeySet` is what keeps them in agreement; if that test is ever weakened, this failure becomes invisible until a consumer reports it.
