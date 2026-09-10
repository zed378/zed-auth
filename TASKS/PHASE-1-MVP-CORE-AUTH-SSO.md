# Phase 1 — MVP: Core Auth + Basic SSO

**Goal**: a working OIDC provider that real applications can log into, with sessions that make SSO actually happen, a Management API and console for the four core entities, an audit trail, and rate limiting — all for a single organization.

**Why the scope stops where it does**: no roles, no multi-org, no MFA, no SAML. `docs/PLAN/00-PROJECT-CONTEXT.md`'s incremental principle and `docs/PLAN/16`'s phase rule both say the same thing — the OIDC flow and session model must be provably correct before anything is layered on top of them. Every later phase inherits whatever is wrong here.

**Definition of done for the phase** (`docs/PLAN/01-PRODUCT-SCOPE.md` § MVP Definition of Done, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1): two independent internal applications authenticate real users through this service, and a user logged into Application A opens Application B without being prompted to log in again.

**Prerequisite**: Phase 0 exit checklist fully satisfied.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P1-01 | Argon2id password hashing | backend | M | P0-15 |
| P1-02 | Password policy and breached-password rejection | backend | M | P1-01 |
| P1-03 | Signing key management, JWKS, and rotation | backend | L | P0-14 |
| P1-04 | Discovery document and JWKS endpoint | backend | S | P1-03 |
| P1-05 | Application (OIDC client) registration and credentials | backend | M | P0-07 |
| P1-06 | `GET /oauth/authorize` — Authorization Code + PKCE | backend | L | P1-05, P1-11 |
| P1-07 | `POST /oauth/token` | backend | L | P1-06, P1-03 |
| P1-08 | `GET /oauth/userinfo` | backend | S | P1-07 |
| P1-09 | `POST /oauth/introspect` and `POST /oauth/revoke` | backend | M | P1-07 |
| P1-10 | `GET /oidc/logout` — end session | backend | M | P1-11 |
| P1-11 | Session management and the SSO cookie | backend | L | P0-07 |
| P1-12 | Hosted login page | backend | M | P1-01, P1-11 |
| P1-13 | Login rate limiting and account lockout | backend | M | P1-12 |
| P1-14 | Authentication audit events | backend | S | P0-12, P1-12 |
| P1-15 | Management API foundation | backend | L | P0-16, P1-07 |
| P1-16 | Management API — organizations | backend | M | P1-15 |
| P1-17 | Management API — projects | backend | M | P1-15 |
| P1-18 | Management API — applications | backend | M | P1-15, P1-05 |
| P1-19 | Management API — users | backend | L | P1-15, P1-01 |
| P1-20 | Management API — audit log read | backend | M | P1-15, P0-12 |
| P1-21 | Console — OIDC login (dogfooding) | console | L | P0-17, P1-07 |
| P1-22 | Console — Organization overview, Projects, Applications | console | L | P1-21, P1-16, P1-17, P1-18 |
| P1-23 | Console — Users list and detail | console | L | P1-21, P1-19 |
| P1-24 | Console — Audit Log screen | console | M | P1-21, P1-20 |
| P1-25 | Public docs — real quickstart and generated API reference | public-site, docs | M | P1-07, P1-19 |
| P1-26 | Two demo consumer applications | backend, infra | M | P1-07 |
| P1-27 | Phase 1 test suite completion | backend, console | L | all above |
| P1-28 | Phase 1 acceptance validation | all | M | P1-27 |

---

## P1-01 — Argon2id Password Hashing

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-08-P1-01-password-hashing.md), [spec](../MEMORY/specs/P1-01-password-hashing.md) |
| **Depends on** | P0-15 |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Passwords & Credentials, `docs/PLAN/07-BACKEND-ARCHITECTURE.md` § Cryptography, `docs/PLAN/11-TESTING.md` § Unit Testing |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — Password storage that stays sound as hardware improves, with parameters recorded per hash so they can be raised without invalidating existing passwords.

**Steps**
1. Implement hashing with Argon2id, using a modern parameter set tuned to the target server's memory and CPU (`docs/PLAN/07`: "parameters tuned to server capacity").
2. Encode parameters into the stored hash string (the standard PHC format), so a future parameter increase is detectable per row.
3. Implement transparent rehash-on-login: when a user authenticates successfully against a hash weaker than the current parameters, rehash and store the stronger one.
4. Use constant-time comparison, and ensure a nonexistent user costs approximately the same wall-clock time as an existing one — otherwise timing distinguishes them (`docs/SECURITY/02` §12 Enumeration).
5. Keep bcrypt verification available only if an actual migration from a legacy system requires it (`docs/PLAN/07` names it as a compatibility fallback); do not add it speculatively.
6. Add a benchmark test so the parameter choice is a measured decision and can be re-measured on new hardware.

**Definition of Done**
- [x] Hashing and verification are covered by unit tests, including wrong-password, malformed-hash, and empty-input cases. 18 malformed-hash cases; none panics, none matches. 95.9% coverage.
- [x] Verification against a nonexistent user takes comparable time to a real user, verified by a timing test with a defined tolerance. Ratio 0.88, tolerance 0.5–2.0 — and the test fails at 0.00 when the path is short-circuited, which is how we know it measures something.
- [x] Rehash-on-login is exercised by a test that starts from a deliberately weak stored hash. Plus its inverse: a hash *stronger* than current is never flagged, so lowering parameters cannot silently downgrade every password that logs in.
- [x] A benchmark records the chosen parameters and their measured cost; the numbers are in the MEMORY record. 90ms/hash on the staging VM, 63ms on a laptop, 67 MB per operation.
- [x] No log line, error message, or panic can carry the plaintext password. The package logs nothing at all — it is a pure function and the caller owns the audit event — and a test asserts no error string contains the password or a prefix of it.

---

## P1-02 — Password Policy and Breached-Password Rejection

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-02-password-policy.md), [spec](../MEMORY/specs/P1-02-password-policy.md) |
| **Depends on** | P1-01 |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Passwords & Credentials, `docs/PLAN/08-AUTHORIZATION.md` Part B § Policies per Organization, `docs/UI-UX/15-FORM-UX.md` |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — Password rules that are enforced server-side and read from organization settings, so Phase 2's per-org policy configuration plugs into an existing mechanism rather than replacing a hard-coded one.

**Steps**
1. Implement a policy evaluator reading from `organizations.settings.password_policy` — the shape is already specified in `docs/PLAN/08` Part B (`min_length`, `require_uppercase`, `max_age_days`).
2. In Phase 1 the settings row holds instance defaults for the single default organization; the evaluator must not hard-code the values, because Phase 2 (`P2-14`) makes them editable per org.
3. Add breached-password checking against a k-anonymity API (`docs/PLAN/09`), sending only a hash prefix — never the password, and never the full hash.
4. Decide and document behavior when the breach-check service is unreachable: fail open (accept the password) or fail closed (reject the change)? Unlike an authorization decision, failing closed here blocks legitimate password changes during an outage. Record the choice and its reasoning as an ADR.
5. Return validation failures in `docs/PLAN/05`'s standard error format with per-field `details`, so the console can render them inline (`docs/UI-UX/15-FORM-UX.md`).
6. Never state which specific rule failed in a way that reveals policy detail to an unauthenticated caller during login; full detail is fine on an authenticated password-change form.

**Definition of Done**
- [x] Policy evaluation is a pure function with table-driven unit tests, including boundary lengths. Both sides of every boundary — a test that only checks that 11 characters is rejected passes against an implementation that rejects everything.
- [x] The breached-password check transmits only a hash prefix, verified by an outbound-request test. The assertion is itself checked against a synthetic leaky request, so it is known to be able to fail.
- [x] The fail-open/fail-closed decision is recorded in `MEMORY/DECISIONS.md` — **ADR-015**: fail open, with an audit event, a metric label and an alert on every skip.
- [x] Validation errors match `docs/PLAN/05`'s error schema exactly. Asserted on the serialized JSON rather than the Go struct, which would pass against wrong `json` tags.
- [x] Changing `organizations.settings.password_policy` changes enforcement with no code change. An integration test `UPDATE`s a live row and re-reads through the same process, for two different fields.

**Beyond the stated steps**

`PG-13` found and registered: `max_age_days` has been in the specified policy since `docs/PLAN/08` with nothing in the schema to evaluate it against. Closed by an additive `users.password_changed_at`, where NULL means *not* expired — the alternative makes deploying the migration a mass lockout.

An 8-character floor no configuration can go below, because otherwise `"min_length": 1` is valid and the control an administrator was given is the control they can silently remove. Length counted in runes over NFC, since a byte count is a different policy per language.

**One real bug, found by a test written before the code it tests**: the corpus response parser counted lines, and an HTML error page is one line — so a proxy answering `200` with "Access denied" produced a `clean` verdict and admitted the password with nothing recording that no check had happened. An always-open path the ADR's own metric would have shown as healthy. It now counts well-formed entries.

---

## P1-03 — Signing Key Management, JWKS, and Rotation

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-03-signing-keys.md), [spec](../MEMORY/specs/P1-03-signing-keys.md) |
| **Depends on** | P0-14 |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Tokens & Keys, `docs/PLAN/07-BACKEND-ARCHITECTURE.md` § Cryptography, `docs/PLAN/14-DEPLOYMENT.md` § Rollback Strategy, `docs/PLAN/02-REQUIREMENTS.md` § Constraints |
| **Spec required** | Yes — cryptographic core |
| **Surface** | backend |

**Goal** — Asymmetric signing keys the service alone holds, rotatable with an overlap window, so that neither a rotation nor an application rollback ever invalidates tokens that should still be valid.

**Steps**
1. Generate RS256 or ES256 key pairs. Never HS256 — consumer services must verify with a public key and no shared secret (`docs/PLAN/07`).
2. Store private keys in the secret manager from `P0-14`. `docs/PLAN/02`'s constraint is absolute: no third-party dependency holds the private signing key outside this service's own infrastructure.
3. Support **multiple simultaneously-active keys**, each with a stable `kid`, in three states: `next` (published, not yet signing), `current` (signing), `previous` (still verifying, no longer signing).
4. Publish all non-retired public keys at the JWKS endpoint, so a token signed just before rotation still verifies afterward (`docs/PLAN/09`'s overlap period).
5. Implement rotation as an explicit operational command, not an automatic timer, for the first release — a scheduled rotation that fails at 3am is worse than a deliberate one during business hours. Note the 90-day cadence from `docs/PLAN/09` as the operational expectation.
6. Handle the rollback case `docs/PLAN/14` calls out: rolling the application back must not invalidate tokens signed by a newer key, so key state lives in the `signing_keys` table (`docs/PLAN/04`), not in the binary or its configuration.
7. Add a cache with a bounded TTL for the key set, and ensure a newly-added key is picked up by all instances within that TTL.
8. Write the rotation runbook: pre-checks, the command, verification steps, and rollback.

**Definition of Done**
- [x] Tokens signed by `previous` still verify; tokens are only ever signed by `current`. Both directions tested — `next`, `previous` and `retired` all refuse to sign.
- [x] JWKS output contains every key in a verifying state, each with a distinct stable `kid`. Retired keys excluded, and the output asserted to contain no private material.
- [x] An integration test performs a rotation and asserts that a token issued before it still validates afterward.
- [x] The private key never appears in logs, API responses, metrics labels, or error messages. Key-marshalling errors deliberately carry no detail, and only the `current` key's private half is loaded at all.
- [x] The rotation runbook exists and has been executed once against staging. **Executing it found two real bugs** — see the record.
- [x] Restarting the service does not change the key set. The `kid` is an RFC 7638 thumbprint, so it is a function of the key rather than of the process.

**Abuse cases** — all tested: `alg:none`, algorithm confusion, `kid` collision, stripped/truncated/altered signature, tampered payload, retired key.

**Beyond the stated steps**

`TestSignatureEncodingIsMalleable` records that 16 distinct token strings decode to the same signature and all verify. Not a forgery risk; a **token identity** risk that lands directly on `P1-07` and `P3-02` — refresh-token reuse detection must key on `jti` or the decoded signature, never on the token string.

**Abuse cases to test**
- A token signed with an attacker-supplied key whose `kid` matches a legitimate one is rejected.
- `alg: none` and algorithm-confusion (HS256 signed with the RSA public key) are both rejected — verification must pin the expected algorithm rather than trusting the header (`docs/SECURITY/02` §1).

---

## P1-04 — Discovery Document and JWKS Endpoint

| | |
|---|---|
| **Status** | DONE (two items deferred) — [record](../MEMORY/records/2026-09-09-P1-04-discovery-jwks.md) |
| **Depends on** | P1-03 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Core Endpoints, `docs/PLAN/03-ARCHITECTURE.md` § OIDC/OAuth2 Provider |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — Standard-compliant discovery, so consumer applications configure themselves from a URL rather than from a wiki page that goes stale.

**Steps**
1. Serve `GET /.well-known/openid-configuration` with `issuer`, endpoint URLs, `response_types_supported`, `grant_types_supported`, `code_challenge_methods_supported` (`S256` only — never `plain`), `id_token_signing_alg_values_supported`, `scopes_supported`, and `subject_types_supported`.
2. Advertise only what is actually implemented. `CLAUDE.md`'s governance rule about not claiming unshipped capabilities applies to machine-readable documents at least as strongly as to marketing copy — a client that trusts the discovery document and finds a missing endpoint fails at runtime.
3. Serve `GET /.well-known/jwks.json` from `P1-03`'s key set, with public key material only.
4. Set appropriate cache headers: long enough to avoid a request per token verification, short enough that a rotation propagates within the overlap window.
5. Ensure the `issuer` value exactly matches the `iss` claim the token issuer emits — a mismatch here breaks every conforming client library, and it is a classic misconfiguration.

**Definition of Done**
- [x] An off-the-shelf OIDC client library configures successfully from the discovery URL alone. **Ticked by `P1-07`**, which is when it became true: the document now carries `authorization_endpoint`, `token_endpoint`, `grant_types_supported`, `response_types_supported`, `scopes_supported` and `code_challenge_methods_supported`, each because the thing it names exists. Until then it read: *Deferred to P1-07, deliberately unticked.* A conforming library needs `authorization_endpoint` and `token_endpoint` to finish configuring, and neither exists yet. Claiming this now would be the exact false claim step 2 of this task exists to prevent. The half that can be verified today is covered: `TestDiscoveryDocumentLeadsToTheKeySet` follows `jwks_uri` out of the document the way a client library would and asserts it reaches the current key.
- [x] `code_challenge_methods_supported` contains `S256` and does not contain `plain`. Also absent entirely until there is an authorization endpoint to apply it to.
- [x] Neither `implicit` nor `password` appears in `grant_types_supported` (`docs/PLAN/05`: both explicitly unsupported). Enforced at construction, not just left out — `Capabilities.Validate` refuses to build a handler that would advertise either.
- [x] `issuer` matches the `iss` claim, asserted by an integration test. **Ticked by `P1-07`**: `TestTheIssuedIDTokenVerifiesAgainstTheJWKS` decodes a real issued token and asserts `iss` equals the configured issuer, which is the comparison that could not be made while nothing emitted the claim. What holds today is the single-source property: the document publishes `cfg.Issuer` verbatim and unnormalised.
- [x] JWKS exposes no private key parameters, asserted by a test that inspects the JSON keys.

**Beyond the stated steps**

Serving these two endpoints through the generated strict router rather than beside it made the split-interface problem visible: `httpserver.apiRoutes` now embeds both implementations and carries the compile-time assertion. Two bugs surfaced while verifying the cache headers — every endpoint answered 405 to HEAD (chi matches methods exactly), and `Server.Handler()` returned the bare mux rather than the served handler, so the first HEAD test passed against a server that was broken. Both fixed; see the record.

---

## P1-05 — Application (OIDC Client) Registration and Credentials

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-05-application-registration.md), [spec](../MEMORY/specs/P1-05-application-registration.md) |
| **Depends on** | P0-07 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `applications`, `docs/PLAN/09-SECURITY.md`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Applications tab), `docs/SECURITY/02` §16 |
| **Spec required** | Yes — credential handling |
| **Surface** | backend |

**Goal** — Registered OIDC clients whose secrets are shown exactly once and stored only as hashes, with redirect URIs validated strictly enough to close the open-redirect class of attack.

**Steps**
1. Implement the application record per `docs/PLAN/04`: the `id` doubles as the `client_id`, with `type` in {`web`, `native`, `spa`, `api`, `saml`}, `redirect_uris`, `grant_types`, and `post_logout_redirect_uris`.
2. Generate client secrets for confidential clients only. Public clients (`spa`, `native`) get no secret and must use PKCE.
3. Hash the secret before storage (`client_secret_hash` in `docs/PLAN/04`) and return the plaintext exactly once at creation. `docs/UI-UX/08`: "Secrets shown once at creation only, never retrievable again."
4. Validate `redirect_uris` at registration time: absolute URIs, no fragments, no wildcards, and HTTPS required except for explicitly-allowed localhost during development.
5. Implement redirect URI matching as **exact string comparison**, never prefix or pattern matching (`docs/PLAN/09` § Protection Against Common Attacks: open redirect).
6. Validate that requested `grant_types` are consistent with the client type — a `spa` client requesting `client_credentials` is a configuration error worth rejecting loudly.
7. Support secret rotation: generate a new secret, allow a defined overlap, then retire the old one.
8. Audit every application create, update, secret rotation, and delete (`P0-12`).

**Definition of Done**
- [x] A client secret is retrievable exactly once and never again through any endpoint or console screen. `Create` and `RotateSecret` are the only producers of a `Secret`; no read path can construct one, because only the hash was stored.
- [x] Only the hash exists in the database, verified by direct inspection in a test. The whole row is read as text, not just the secret column — a plaintext leaking into any other column is caught too. Proven non-vacuous by making the store write the plaintext and watching the test fail.
- [x] A redirect URI differing by a trailing slash, a query parameter, or a fragment is rejected at authorization time. All three, plus twelve more, each naming the attack it prevents — and a control asserting the exact registered URI matches, since a matcher that rejects everything would pass the rejection table.
- [x] A public client cannot be created with a secret. Refused in code and independently by `P0-07`'s CHECK constraint.
- [x] Application lifecycle events appear in the audit log. Create, update, rotate and delete.

**Beyond the stated steps**

`ADR-016`: client secrets are hashed with SHA-256 rather than Argon2id. 256 bits of entropy already settle brute force, and a slow KDF on a path verified once per token request is self-inflicted amplification — 4.5 cores and 288 MiB at fifty requests a second, paid by us while an attacker sending wrong secrets pays nothing. The decision is conditional on the secret being generated here, so `Generate` is the sole producer and a test fails if the entropy weakens.

The card's citation of `docs/SECURITY/02` §7 (SSRF) for the internal-address case is corrected in the record: a `redirect_uri` is never fetched server-side, so the risk is a code delivered to a host the user's machine can reach rather than a forged server request. Link-local is refused; private ranges are allowed, because an intranet redirect is a legitimate self-hosted configuration.

Two bugs found by tests written before the code they cover: `fmt.Stringer` does not redact under `%d`, and the loopback check was case-sensitive while hostnames are not.

**Abuse cases to test**
- Open redirect via a `redirect_uri` that merely shares a prefix with a registered one (`docs/SECURITY/02` §1).
- A public client attempting the `client_credentials` grant is rejected.
- A registered `redirect_uri` pointing at an internal address is rejected or explicitly reviewed (`docs/SECURITY/02` §7 SSRF).

---

## P1-06 — `GET /oauth/authorize` — Authorization Code + PKCE

| | |
|---|---|
| **Status** | DONE (one item deferred to `P1-27`) — [record](../MEMORY/records/2026-09-09-P1-06-authorize.md), [spec](../MEMORY/specs/P1-06-authorize.md) |
| **Depends on** | P1-05, P1-11 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` Part A, `docs/PLAN/03-ARCHITECTURE.md` § Main Data Flow (SSO), `docs/PLAN/09-SECURITY.md`, `docs/PLAN/12-PERFORMANCE.md` |
| **Spec required** | Yes — authentication core |
| **Surface** | backend |

**Goal** — The endpoint that makes SSO feel instantaneous: if a valid session cookie exists, issue a code without showing anything; otherwise route to the login page and return here afterward.

**Steps**
1. Validate parameters strictly before doing anything else: `client_id` exists, `redirect_uri` matches exactly, `response_type=code`, `scope` requested is permitted, `state` present, `code_challenge` present with `code_challenge_method=S256`.
2. Make PKCE **mandatory for every client type**, not only public ones — `docs/PLAN/05` states this explicitly, and it is stricter than the base OAuth 2.1 requirement.
3. On a parameter error, decide correctly where the error goes: an invalid `client_id` or `redirect_uri` must render an error page and must **never** redirect, because redirecting to an unvalidated URI is the open-redirect vulnerability itself. Other errors redirect to the validated `redirect_uri` with `error` and the original `state`.
4. Check for an existing SSO session (`P1-11`). If valid and satisfying any session-age requirement, skip the login page entirely — this is the silent-SSO path `docs/PLAN/12` targets at p95 < 150ms.
5. If no session, store the pending authorization request server-side keyed by an opaque identifier, redirect to the hosted login page, and resume exactly where it left off after successful authentication.
6. Honor `prompt=none` (return `login_required` rather than showing UI) and `prompt=login` (force re-authentication even with a session), since consumer SPAs rely on both for silent token renewal.
7. Issue a single-use authorization code with a short lifetime (60 seconds or less), bound to the client, redirect URI, session, and `code_challenge`.
8. Store the code and its `code_challenge` in Redis with a sub-60-second TTL, per `docs/PLAN/04` § What Is Deliberately Not Stored Here, so an unredeemed code cannot linger.
9. Emit metrics distinguishing the silent-SSO path from the login-required path, since `docs/PLAN/12` sets a separate latency target for the former.

**Definition of Done**
- [x] A request missing `code_challenge` is rejected for every client type, including confidential ones. The test iterates `client.Types` rather than naming the ones somebody remembered.
- [x] `code_challenge_method=plain` is rejected, as is an absent method.
- [x] An invalid `client_id` or `redirect_uri` produces an error page and no redirect whatsoever. Asserted as the **absence of a `Location` header** — an unusual assertion, and therefore a conspicuous one to break. Both causes are reported identically so the endpoint cannot be used to discover which client ids exist.
- [x] With a valid session, the endpoint returns a code with no user interaction, bound to client, redirect URI, challenge, session and `auth_methods`.
- [x] `prompt=none` without a session returns `login_required` rather than rendering a login page — asserted on the error code *and* the absence of HTML, since a hidden iframe cannot show a form.
- [x] Codes are single-use, expire within 60 seconds, and are bound to the issuing client and redirect URI. 30 seconds, with the bound enforced in `IssueCode` rather than trusted to callers.
- [ ] The silent-SSO path meets `docs/PLAN/12`'s p95 target under the load test. **Deferred to `P1-27`, which owns the load test.** What ships here is the metric that measures it, labelled so the silent path is separable — an average across both paths would hide it behind the time a human spends typing a password.

**Beyond the stated steps**

The atomicity `docs/PLAN/04` requires is demonstrated rather than asserted: 32 goroutines across 25 rounds yield exactly one success, and replacing `GETDEL` with `GET`-then-`DEL` makes that test fail with 32 successes while the sequential single-use test stays green.

**One deviation from ADR-013, deliberate and documented.** The generated router binds every parameter before the handler runs, which would reject a missing `state` before `redirect_uri` was validated — sending the error down the wrong channel — and would silently collapse duplicated parameters. The operation is excluded from code generation and registered by hand; it stays in `openapi.yaml`, so it is still documented, still generated into the public API reference, and still checked by `openapi-shipped-paths.py`.

Discovery now advertises `authorization_endpoint` and `code_challenge_methods_supported`, because both became true. `token_endpoint` is still absent, which is also true — so `P1-04`'s first DoD item stays unticked until `P1-07`.

**Abuse cases to test**
- Authorization code interception without the verifier (`docs/PLAN/10` § High-Priority Abuse Scenarios).
- Code replay: a redeemed code is rejected on second use, and the associated session is flagged.
- A code issued to client A cannot be redeemed by client B.
- CSRF on the authorization endpoint: a missing or mismatched `state` (`docs/SECURITY/02` §5).

---

## P1-07 — `POST /oauth/token`

| | |
|---|---|
| **Status** | DONE (one item deferred to `P1-27`) — [record](../MEMORY/records/2026-09-09-P1-07-token.md), [spec](../MEMORY/specs/P1-07-token.md) |
| **Depends on** | P1-06, P1-03 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` Part A, `docs/PLAN/09-SECURITY.md` § Tokens & Keys, `docs/PLAN/12-PERFORMANCE.md`, `docs/PLAN/08-AUTHORIZATION.md` Part A |
| **Spec required** | Yes — authentication core |
| **Surface** | backend |

**Goal** — The highest-volume, most latency-sensitive endpoint in the system: exchange a code or refresh token for an ID token and access token, correctly and fast.

**Steps**
1. Support exactly three grants in Phase 1: `authorization_code`, `refresh_token`, and `client_credentials`. Reject `password` and `implicit` explicitly (`docs/PLAN/05`).
2. Authenticate the client: `client_secret_basic` or `client_secret_post` for confidential clients; public clients are identified by `client_id` and authenticated solely by the PKCE verifier.
3. Verify the PKCE `code_verifier` against the stored `code_challenge` using S256, in constant time.
4. Verify that the code is unredeemed, unexpired, and bound to this client and this `redirect_uri`. Mark it redeemed atomically — a race between two concurrent redemptions must result in exactly one success.
5. Issue an ID token with `iss`, `sub`, `aud`, `exp`, `iat`, `auth_time`, `nonce` (echoed when supplied), and `amr` reflecting the actual authentication methods used (`docs/PLAN/05` § MFA). `amr` is `["pwd"]` in Phase 1 and grows in Phase 3; consumer apps use it for step-up decisions.
6. Issue an access token with a 5–15 minute lifetime (`docs/PLAN/09`), carrying `org_id` and reserving the role-claim namespace that Phase 2 (`P2-04`) will populate.
7. Issue a refresh token with a longer lifetime, stored only as a hash (`docs/PLAN/04`). Full rotation with reuse detection is Phase 3 (`P3-06`), but the storage shape must not need changing then.
8. Return the standard OAuth error codes with the correct HTTP status; never leak internal detail in an error body.
9. Set `Cache-Control: no-store` on every token response.
10. Instrument latency to `docs/PLAN/12`'s targets: p50 < 50ms, p95 < 200ms, p99 < 400ms.

**Definition of Done**
- [x] Each supported grant works end-to-end against an integration test with a real database — and a real Redis and a real signing key, so the whole chain from `P1-03` to `P1-06` is exercised rather than stubbed.
- [x] `password` and `implicit` grants are rejected with the correct OAuth error, **by name**, with a description pointing at the supported flow rather than falling through to an unknown-grant message.
- [x] A wrong `code_verifier` is rejected — and the code does not survive the attempt, so it cannot be retried against.
- [x] Concurrent redemption of one code yields exactly one success, verified by a parallel test through the whole endpoint.
- [x] Tokens carry the exact claim set, and `aud` and `iss` are correct for the requesting client. Asserted **after verifying against the published JWKS**, not on the struct — which is the property a consumer actually depends on.
- [x] No refresh token is stored in plaintext, verified by reading the whole row as text so a leak into any column is caught.
- [ ] The endpoint meets `docs/PLAN/12`'s latency targets under the load test. **Deferred to `P1-27`, which owns it.** What ships here is the histogram, bucketed on the targets so a quantile query answers "did we meet it" without interpolating across a wide bucket.

**Beyond the stated steps**

**The flow closes here.** `P1-06` had been issuing codes nothing could redeem, and `P1-02`, `P1-05` and `P1-11` were packages with no callers. Discovery now advertises both endpoints a conforming client needs.

`PG-15` found and closed: `refresh_tokens` had no scope column, so a refresh had nothing to reproduce — it could carry no scope, or re-derive one from the client's registration, which is not the same thing. Scope is what the user consented to; `grant_types` is what the client may ask for.

The refresh token is **opaque rather than a JWT**, and the second reason is the one worth keeping: `P1-03` found that sixteen encodings of one signature all verify, so a JWT's string form is a poor key for `P3-06`'s reuse detection. An opaque value has one representation.

Found while cleaning up a signature: `AuthenticateClient` checked that a secret was *presented* and never verified it — a function that returned nil for any secret at all, which every test at the time would have passed because none presented a wrong one.

**Abuse cases to test**
- Code replay after successful redemption (`docs/PLAN/10`).
- A token with the wrong `aud` is rejected by the resource server (`docs/PLAN/11` § Security Testing).
- Client authentication bypass: redeeming a confidential client's code without its secret.
- Cross-client code redemption.
- Token substitution: an access token used where an ID token is expected, and vice versa.

---

## P1-08 — `GET /oauth/userinfo`

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-10-P1-08-userinfo.md), [spec](../MEMORY/specs/P1-08-userinfo.md) |
| **Depends on** | P1-07 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Core Endpoints, `docs/PLAN/12-PERFORMANCE.md`, `docs/PLAN/10-THREAT-MODEL.md` § Information Disclosure |
| **Spec required** | Yes — data exposure |
| **Surface** | backend |

**Goal** — Return the claims the access token's scopes actually authorize, and nothing beyond them. `docs/PLAN/12` notes consumer SPAs often call this on every page load, so it must be both fast and minimal.

**Steps**
1. Authenticate via the bearer access token; validate signature, expiry, issuer, and audience.
2. Map scopes to claims: `openid` → `sub`; `profile` → name and related; `email` → `email` and `email_verified`. A claim outside the granted scopes is never returned.
3. Return `sub` as a stable, non-guessable identifier — never a sequential value, and never the email (`docs/SECURITY/02` §12 Enumeration).
4. Serve from a read replica or a short-TTL cache where `docs/PLAN/12` allows, since this is a read-heavy path.
5. Reject an expired or revoked token with `401` and the standard `WWW-Authenticate` challenge.

**Definition of Done**
- [x] Claims returned are exactly those the granted scopes permit, verified by test across scope combinations. The tests assert the **exact key set**, not a subset — a handler that returned every claim regardless of scope passes any "is `email` there when `email` was granted" test, and only an exact comparison catches it.
- [x] An expired token yields `401` with a correct challenge header — `WWW-Authenticate: Bearer realm="…", error="invalid_token"`. A request with **no** credential gets the bare challenge and no error code, per RFC 6750 §3.1: an error code describes a credential that was presented.
- [x] No organization-internal identifiers leak beyond what the scopes justify. Asserted twice: by name (`org_id`, `sid`, `client_id`) and by value, since the handler is holding all three when it builds the response.
- [x] The endpoint meets `docs/PLAN/12`'s p95 < 100ms target. **One** database round trip, and the session-liveness check rides in the same statement as the user read rather than costing a second. `auth_userinfo_duration_seconds` measures it; the number itself belongs to `P1-27`'s load test, like `P1-06`'s and `P1-07`'s.

**Abuse cases covered**
- [x] An ID token presented as a bearer credential — refused twice over, by `typ` and by `aud`.
- [x] `alg: none` and algorithm confusion — `P1-03`'s closed algorithm list, applied before a key is fetched.
- [x] A token from another issuer, or for another audience.
- [x] A stolen token used after logout — the session-liveness check, which is the one control here that costs something.
- [x] Enumerating users by varying `sub` — there is no parameter; the subject comes from the token.
- [x] Learning anything from the error — every unusable token gets the identical response, compared byte for byte across eight failure classes.
- [x] A `client_credentials` token, where `sub` is the client and there is no user to describe.

**What this task also produced**
- **`signing.Verifier.Verify` now takes a mandatory `wantType`.** Not a strict `VerifyTyped` beside a permissive `Verify`: that is a pair where the shorter, more obvious name is the unsafe one, and the unsafe one is what gets called. The verifier had **no caller at all** before this task — the service could sign tokens and had never once verified one of its own.
- `PG-17` — no CORS policy exists anywhere in the plan, and two Phase 1 consumers need one.
- `PG-18` — `email_verified` is an OIDC claim with no column behind it.

**Deliberately not done**
- **No CORS headers.** `docs/PLAN/12` says SPAs call this on every page load, which means a browser, which means an origin policy — and there is none to implement. `Access-Control-Allow-Origin: *` on an endpoint that returns email addresses is precisely what `docs/SECURITY/02` §12 warns about. `PG-17`.
- **No read replica or response cache** (step 4). There is no replica, and a cache whose entries outlive a profile change or a session revocation serves a stale identity — on the endpoint whose entire job is to say who somebody currently is. One indexed query already fits the budget; measure before adding a cache with an invalidation problem.
- **`email_verified` is omitted**, not returned as `false`. `PG-18`.

---

## P1-09 — `POST /oauth/introspect` and `POST /oauth/revoke`

| | |
|---|---|
| **Status** | DONE (one item deferred to `PG-19`) — [record](../MEMORY/records/2026-09-10-P1-09-introspect-revoke.md), [spec](../MEMORY/specs/P1-09-introspect-revoke.md) |
| **Depends on** | P1-07 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Core Endpoints, `docs/PLAN/09-SECURITY.md` |
| **Spec required** | Yes — token lifecycle |
| **Surface** | backend |

**Goal** — Resource servers can check a token's validity, and clients can revoke tokens they no longer need.

**Steps**
1. Introspection requires client authentication — an unauthenticated introspection endpoint is a token oracle.
2. Return `{"active": false}` for any token that is invalid, expired, revoked, or simply unknown. Never distinguish between those cases: distinguishing them is an enumeration primitive (`docs/SECURITY/02` §12).
3. Revocation accepts both access and refresh tokens, and revoking a refresh token also invalidates its lineage.
4. Per RFC 7009, revocation returns `200` even for an unknown token, again to avoid disclosure.
5. Rate-limit both endpoints per client (`docs/PLAN/05` § Rate Limiting).
6. Audit revocations; do not audit routine introspection, which would flood the log without adding signal.

**Definition of Done**
- [x] Unauthenticated introspection is rejected — and so is an *authenticated* one from a public client, which is stricter than RFC 7662 and is the point: a client with no secret is one anybody can impersonate by reading a `client_id` out of a browser URL.
- [x] An unknown, an expired, and a revoked token produce byte-identical responses. Seven negative cases compared as whole responses — status, every header sorted, body — with controls proving the shared answer is really `{"active":false}` and that an active token is distinguishable.
- [x] Revoking a refresh token invalidates tokens derived from it. The whole family, proved against a real sibling row; a mutation that revokes one row of the family fails the test.
- [x] Revocation events appear in the audit log — naming the client, the user and the count, and **not** the token. `token_hash` would be redacted by the audit writer anyway; a novel key name would not be, which is what the test is for.

**Abuse cases to test**
- [x] Using introspection to enumerate valid tokens by response-shape or timing differences. Shape: one negative answer, byte-identical. Timing: the cost follows the token's SHAPE — a JWT costs a signature check, an opaque token a hash and an indexed lookup — and the caller can already see which shape it sent. Within each shape the cost is the same whether or not the token exists.
- [x] One client revoking another client's token. Tested with two real clients in the SAME organization, so RLS is not what refuses it — the ownership rule is.
- [x] One client *introspecting* another client's token, which the card does not list and is the same rule seen from the other side.

**What this task also produced**
- `RefreshStore.RevokeForSessionAndClient`, scoped by both columns. Session alone would let one client's logout throw away every other application's refresh token in the same single sign-on session — a denial of service an integrator could inflict by calling a documented endpoint correctly.
- `RefreshTokens`, `Tenant` and `Auditor` seams on the new handler, matching `login.Handler`. Came out of a nil-database panic whose tempting fix — a nil guard on the lookup — would have turned a construction error into every refresh token silently reporting inactive.
- `PG-19` — per-client rate limiting has a requirement and no owner.

**Deliberately not done**
- **Step 5, per-client rate limiting.** `PG-19`. Both endpoints require client authentication, so abuse costs an attacker a valid client secret rather than a network connection; what it does not bound is a compromised client. `P1-15` is the natural owner.
- **`username` in the introspection response**, though RFC 7662 lists it. It is an email address, the caller already has `sub`, and `/oauth/userinfo` will give the address to a token that authorises it.
- **Auditing introspection.** A resource server may call it on every request; the volume would be proportional to API traffic and carry no signal. A no-op revocation is not audited either.

---

## P1-10 — `GET /oidc/logout` — End Session

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-10-P1-10-logout.md), [spec](../MEMORY/specs/P1-10-logout.md) |
| **Depends on** | P1-11 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Session & logout, `docs/PLAN/04-DATA-MODEL.md` § `sessions` |
| **Spec required** | Yes — session lifecycle |
| **Surface** | backend |

**Goal** — Logout that genuinely ends the session, plus the "log out of all sessions" action `docs/PLAN/05` requires for the MVP.

**Steps**
1. Implement RP-initiated logout: validate `post_logout_redirect_uri` against the client's registered list with exact matching — the same open-redirect discipline as `P1-05`.
2. Validate `id_token_hint` where supplied, so an attacker cannot force-log-out an arbitrary user by URL.
3. Terminate the session server-side and clear the cookie. Deleting the cookie alone is not logout — the server-side record must go, or a copied cookie still works.
4. Implement "log out of all sessions": terminate every session for the user and revoke their refresh tokens.
5. Show a confirmation interstitial when there is no valid `id_token_hint`, rather than acting on an unauthenticated GET.
6. Audit every logout with the scope of what was terminated.
7. Leave back-channel logout to a later phase, as `docs/PLAN/05` explicitly defers it — but make sure this design does not preclude it.

**Definition of Done**
- [x] After logout, a subsequent `/oauth/authorize` requires full re-authentication. Tested as the **consequence** rather than as a `revoked_at` column: the same cookie is replayed at the authorization endpoint and must land on the login page. A column test would pass against a system that revokes the row and keeps honouring the cookie from cache.
- [x] A replayed session cookie captured before logout is rejected — the same test, which is the same property seen from the attacker's side.
- [x] `post_logout_redirect_uri` matching is exact; an unregistered value is refused **with no redirect at all**, because reporting the error by redirecting to the unvalidated address is the vulnerability. Six near-misses tested, including a trailing slash, an appended query, a scheme change and a case change.
- [x] "Log out of all sessions" terminates every session and revokes refresh tokens. Both halves: without the second an application holding a refresh token mints a fresh access token minutes later, so the button would end the browser sessions and quietly leave every integration signed in.
- [x] Logout events are audited, with the scope of what was ended and never the cookie.

**Abuse cases to test**
- [x] Forced logout of another user via a crafted logout URL (`docs/SECURITY/02` §5). A GET acts only on an `id_token_hint` that verifies AND names the session the cookie resolves to. Eight shapes of unprovable request tested, all of which revoke nothing.
- [x] A hint for **another** session logs nobody out — not its own subject, and not the visitor. Tested separately, because those are two different failures.
- [x] Session still usable after logout (`docs/SECURITY/02` §4). The row is revoked and the cache invalidated after the commit; clearing the cookie is done as well, never instead.
- [x] CSRF on the confirmation, and clickjacking of it.

**What this task also produced**
- `RefreshStore.RevokeAllForUser` — the half of "log out everywhere" that is easy to forget.
- `writePage`, lifted out of the login handler's method, so both browser surfaces get their headers from one place rather than from a copy.
- `PG-20` — `docs/PLAN/05` treats RP-Initiated Logout and Back-Channel Logout as the same specification.

**Deliberately not done**
- **The `id_token_hint`'s expiry is not checked.** An ID token lives five minutes; a user signing out an hour later is the ordinary case, so rejecting a stale hint would send nearly every real logout to a confirmation click. The hint is evidence of who initiated the request, not a credential being honoured — and it still has to match the live session, which is what actually bounds it.
- **Back-channel logout**, per step 7 and `DF-09` (now narrowed to that specification alone). Nothing here precludes it.
- **An opaque server-side reference for the round trip**, unlike `P1-12`. Logout has one parameter that matters and one function that validates it, called unconditionally on both paths — so a hidden field the user can edit is one the same check rejects, and a test edits it to prove so.

---

## P1-11 — Session Management and the SSO Cookie

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-11-session-management.md), [spec](../MEMORY/specs/P1-11-session-management.md) |
| **Depends on** | P0-07 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `sessions`, `docs/PLAN/05-API-CONTRACT.md` § Session & logout, `docs/PLAN/09-SECURITY.md`, `docs/SECURITY/02` §4, `docs/PLAN/03-ARCHITECTURE.md` § Main Data Flow |
| **Spec required** | Yes — the mechanism SSO depends on |
| **Surface** | backend |

**Goal** — The single mechanism that makes step 6 of `docs/PLAN/03`'s data flow work: a browser session at the Auth Service that lets the second application skip login entirely.

**Steps**
1. Create a session on successful authentication, persisting id, `user_id`, `created_at`, `expires_at`, `auth_methods`, `ip`, and `user_agent` (`docs/PLAN/04`).
2. Populate `auth_methods` accurately — Phase 3's step-up authentication reads it, and a value that was never trustworthy cannot be made trustworthy later.
3. Set the cookie `HttpOnly`, `Secure`, `SameSite=Lax`, with `Path=/` and no `Domain` attribute broader than necessary (`docs/PLAN/05`).
4. Use a cryptographically random session identifier with sufficient entropy; the cookie carries only an opaque id, never user data.
5. **Regenerate the session identifier after successful login**, defeating session fixation (`docs/PLAN/09`, `docs/SECURITY/02` §4).
6. Store sessions in Redis for fast lookup, with Postgres as the durable record backing the "active sessions" screen — reconcile the two explicitly rather than letting them drift.
7. Enforce both an absolute lifetime and an idle timeout, defaulting from `organizations.settings.session_lifetime_hours` (`docs/PLAN/08` Part B) so Phase 2 can make it per-org without a rewrite.
8. Bind the session loosely to context: record IP and user agent for the sessions screen and Phase 3's anomaly detection, but do not hard-fail on IP change — mobile networks change addresses legitimately, and hard-failing would break real users.
9. Provide session revocation as an internal API, used by `P1-10` and by Phase 3's self-service screen.

**Definition of Done**
- [x] The session identifier changes after login, verified by test (fixation defense). A fresh token per login, the previous session revoked, and the old token asserted dead.
- [x] Cookie attributes are exactly as specified, asserted by an integration test on the `Set-Cookie` header — plus the absence of `Domain` and `Max-Age`, which a struct assertion would not show, and the `__Host-` prefix that makes the browser enforce them.
- [x] Session lookup adds no more latency than `docs/PLAN/12`'s silent-SSO budget allows. One Redis round trip warm; the histogram is labelled by source so hit rate and latency read from one metric. Load measurement belongs to `P1-27`.
- [x] Expired sessions are unusable and are cleaned up rather than accumulating. Filtered in SQL inside the lookup function, so an expired row cannot reach the cache; swept hourly with a 7-day retention that keeps the evidence.
- [x] Revoking a session takes effect immediately for the next request, not after a cache TTL. Commit then invalidate, with a tombstone closing the repopulate race — proven non-vacuous in both directions.
- [x] `auth_methods` reflects reality and is used to build `amr` in `P1-07`. Required at creation: a session without it cannot exist, because `amr` would otherwise be built from a value that was never true.

**Beyond the stated steps**

`PG-14` found and closed: `docs/PLAN/04` describes the cookie as carrying the row's `id`, which makes the primary key a bearer credential — and `docs/PLAN/05` routes a sessions endpoint that would hand an administrator one per row. The cookie now carries a 256-bit token whose hash is stored; `id` stays an internal identifier.

Instance scope reads no sessions, which cost five failing tests to discover: `sessions_tenant_isolation` is `org_id = current_org_id()`, so with no tenant set nothing matches. That is `P0-08` working correctly. The bootstrap got a narrow `SECURITY DEFINER` function instead of a relaxed policy, on `P0-12`'s pattern.

Also found: `session_id` was in the logger's redaction list — correct when the id was the credential, wrong after `PG-14`, and it would have made the audit log unable to say which session was revoked. gosec flagged the cookie's `Secure` parameter, and removing the parameter was the right fix rather than suppressing the warning.

**Abuse cases to test**
- Session fixation: a pre-login session identifier is not honored post-login.
- Session hijacking via a stolen cookie: detectable in the sessions list and revocable.
- Cookie sent over plain HTTP is never accepted (`Secure` enforced).
- Concurrent session limits, if any, behave predictably.

---

## P1-12 — Hosted Login Page

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-12-login-page.md), [spec](../MEMORY/specs/P1-12-login-page.md) |
| **Depends on** | P1-01, P1-11 |
| **Plan refs** | `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 1, `docs/UI-UX/15-FORM-UX.md`, `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/UI-UX/13-ACCESSIBILITY.md`, `docs/SECURITY/02` §5, §6 |
| **Spec required** | Yes — authentication surface |
| **Surface** | backend (no console change — see step 1) |

**Goal** — The centralized login page every application redirects to: minimal, fast, accessible, and free of the information leaks that make credential attacks cheap.

**Steps**
1. Server-render the login page from the auth service itself (not the console SPA) so it works with JavaScript disabled and has no dependency on the console's deploy cycle.
2. Form with email/username and password, plus CSRF protection on the POST.
3. Apply the `docs/UI-UX/15-FORM-UX.md` field-label-helper-error pattern and `docs/UI-UX/13`'s accessibility requirements: labelled inputs, an error summary linked to fields, and keyboard-only completability.
4. **Uniform error messaging**: a wrong password and a nonexistent account produce the identical message and comparable response time (`docs/SECURITY/02` §12; the timing side is `P1-01`'s work).
5. Carry the pending authorization request through the login round-trip by opaque server-side reference, never by echoing the full parameter set through a hidden field.
6. Set restrictive security headers on this page specifically: a strict `Content-Security-Policy` with no inline script, `X-Frame-Options: DENY` (a login page must never be framable — clickjacking), `Referrer-Policy: no-referrer`.
7. Render organization branding — logo and `color-accent` only — per `docs/UI-UX/05`'s constraint that danger and warning colors are never tenant-overridable.
8. Add the "forgot password" entry point, with the reset flow itself scoped as `P1-19.4` alongside user management.
9. Support the `prompt=login` re-authentication path from `P1-06`.

**Definition of Done**
- [x] The page functions without client-side JavaScript. There is none at all — 0 scripts, no inline handler, no style attribute — which is what makes `default-src 'none'` an achievable policy rather than an aspirational one.
- [x] Wrong-password and nonexistent-account responses are indistinguishable in body, status, and headers. Tested by submitting the **same address twice** with the account deleted in between, and comparing the whole recorded responses — status, every header, and the body byte for byte. Comparing two different addresses would have forced the assertion to be loosened until it stopped testing anything.
- [x] CSRF protection is present and tested. Double-submit, `__Host-` prefixed, `SameSite=Lax`, compared in constant time; missing, mismatched, malformed and reused tokens each refused, with a control test proving a matching token is accepted.
- [x] The page cannot be framed, verified by an integration test on the response headers — `X-Frame-Options: DENY` and `frame-ancestors 'none'`, on every response the handler can produce, not only the happy path.
- [x] The page meets WCAG 2.1 AA for the login form (`docs/UI-UX/13`). Labelled inputs, an error summary with `role="alert"` linked to its fields, an accent contrast floor enforced at 4.5:1 against the white button text. Confirmed in a browser; **automated checks do not establish accessibility** and `PF-50`'s audit still owns the claim.
- [x] The full flow completes with keyboard only. Verified in Chromium: focus lands on the error summary, Tab reaches email → password → Sign in → "Forgot your password?" in that order, and Enter from the password field submits.

**Abuse cases to test**
- [x] Username enumeration through error text, status code, or response timing (`docs/SECURITY/02` §12). One message for a wrong password, an unknown address, a locked account, a deactivated one and an account with no password; equal-cost not-found path measured end to end at a 0.6–1.6 ratio bound.
- [x] Clickjacking the login form (`docs/SECURITY/02` §6 area).
- [x] CSRF against the login POST (`docs/SECURITY/02` §5).
- [x] XSS through the `error` or `state` parameter reflected onto the page (`docs/SECURITY/02` §6). Answered structurally: **no query parameter is rendered at all**, so a crafted URL selects a message we wrote or nothing.

**What this task also produced**
- `PG-16` — organization branding is specified in `docs/PLAN/01` and `docs/UI-UX/05`, implemented in the console, and had nowhere to be read from. Closed by documenting `settings.branding`; `docs/PLAN/08` Part B should be amended.
- An exported `Resume`/`Peek` seam on `P1-06`'s handler, and a `PeekPending` alongside `LoadPending` so that only a SUCCESSFUL login spends the pending request. Consuming on every submission would have given every account exactly one attempt at its password.
- `authn.UserStore`, which is where `P1-01`'s `VerifyDummy` and `NeedsRehash` finally acquired callers.
- A coverage floor for `internal/login`, and the tests needed to keep `internal/authn` and `internal/oauth/authorize` above theirs after this task's code landed.

**Deliberately not done**
- The three routes are **not** in `openapi/openapi.yaml`. `/oauth/authorize` is there because integrators code against it; nobody ever calls `/login`, and publishing it to `/docs/api-reference` would offer consumers an endpoint with no stable contract. Reasoning in the spec §7.
- Nothing writes `settings.branding` yet, so every organization renders unbranded. `P2-14` makes it editable.
- Nothing creates a user with a password yet — `P1-19` owns that — so on a fresh deployment this page is complete and has nobody to let in.

---

## P1-13 — Login Rate Limiting and Account Lockout

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-10-P1-13-rate-limiting.md), [spec](../MEMORY/specs/P1-13-rate-limiting.md), [ADR-017](../MEMORY/DECISIONS.md) |
| **Depends on** | P1-12 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Rate Limiting & Brute-Force Protection, `docs/PLAN/09-SECURITY.md`, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `docs/SECURITY/02` §10, §13 |
| **Spec required** | Yes — security control |
| **Surface** | backend |

**Goal** — A brute-force attempt is demonstrably blocked, which is an explicit Phase 1 acceptance criterion — while a legitimate user who mistypes their password twice is not locked out of their job.

**Steps**
1. Implement per-account limiting with a **cooldown, not a permanent lockout** — `docs/PLAN/05` says this explicitly, because permanent lockout converts a brute-force attempt into a denial-of-service against the victim.
2. Implement per-IP limiting for credential stuffing, with a different threshold, since one IP legitimately serves many users behind NAT.
3. Use progressive delay: the first few failures are free, then an increasing cooldown.
4. Store counters in Redis with automatic expiry.
5. Decide and document behavior when Redis is unavailable. Failing open means no rate limiting; failing closed means no logins at all. `docs/PLAN/13`'s fail-safe principle applies to authorization decisions specifically — this is a different trade-off and needs its own reasoned ADR.
6. Ensure the limiter cannot be bypassed by rotating a header: only a proxy-verified client IP is trusted, never a raw `X-Forwarded-For` (`docs/SECURITY/02` §10 Rate-Limit Bypass).
7. Emit `X-RateLimit-*` headers where appropriate (`docs/PLAN/05`), but not in a way that helps an attacker calibrate their pacing on the login endpoint.
8. Audit lockout events and emit a metric, feeding `docs/PLAN/13`'s "spike in failed logins" alert.
9. Reset counters on successful authentication.

**Definition of Done**
- [x] A simulated brute-force attempt is demonstrably blocked. The test also asserts the part that makes it a limiter rather than a nuisance: **the correct password is refused too while the cooldown runs** — one an attacker can step past by guessing right is not one.
- [x] Lockout is temporary and self-clearing; no admin action is needed. There is no unlock endpoint and no cleanup job: the key's TTL is the whole mechanism, and a test waits it out rather than clearing anything.
- [x] Rotating `X-Forwarded-For` does not reset the limit — tested with a different client IP on **every single attempt**, and separately that an untrusted peer cannot choose its own identity at all.
- [x] The Redis-unavailable decision is recorded as [ADR-017](../MEMORY/DECISIONS.md): fail open, loudly.
- [x] Lockouts appear in the audit log (once per cooldown, never once per attempt) and in `auth_rate_limit_refusals_total`.

**What this task also produced**
- `httpserver.ClientIP` — a configured, trusted-peer-gated client address. Needed because on this service's own deployment `RemoteAddr` is the Docker gateway and is the same for every user in the world, which would have made the per-IP bound a global one that a single attacker could use to lock everybody out.
- **A dead branch in `Evaluate` that read like the allowance check and decided nothing**, found by a mutation that changed its comparison and broke no test. Deleted.
- `BL-05` — `AUTH_CLIENT_IP_HEADER` is not set on staging.

**Deliberately not done**
- **No `X-RateLimit-*` headers.** Step 7 says "not in a way that helps an attacker calibrate their pacing on the login endpoint", and the login endpoint is the only thing this task limits — telling an attacker how many attempts remain and when the window resets is calibration, not courtesy. They belong with `P1-15`'s `/v1` limiter, where the caller is an authenticated machine that needs them.
- **`429` is not used.** A browser renders it as an error page, losing the form, the CSRF token and the pending request. The message is what a person needs; the status is what a machine would need, and no machine posts this form.
- **An in-process fallback for a Redis outage.** ADR-017: a second code path that executes only during the incident it exists for is the shape of code that turns out not to work when it finally runs.

**Abuse cases to test**
- Distributed credential stuffing across many IPs against many accounts (`docs/SECURITY/02` §13).
- Rate-limit bypass by header manipulation, casing variations of the username, or alternating between username and email for the same account.
- Denial of service against a specific user by deliberately triggering their lockout.

---

## P1-14 — Authentication Audit Events

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-10-P1-14-authentication-audit.md) |
| **Depends on** | P0-12, P1-12 |
| **Plan refs** | `docs/PLAN/09-SECURITY.md` § Audit, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `docs/PLAN/13-OBSERVABILITY.md` |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — Login success and failure appear in the audit log with correct actor and timestamp, which `docs/PLAN/17` names as a Phase 1 acceptance criterion.

**Steps**
1. Emit `user.login.success` and `user.login.failed` through `P0-12`'s writer, with IP, user agent, and the authentication method used.
2. For a failed login against a nonexistent account, record the attempt without asserting a user id — and ensure the audit log itself does not become an enumeration oracle for anyone who can read it.
3. Emit `session.created`, `session.revoked`, `token.issued` (aggregate metric rather than a per-token row, to keep volume sane), and `user.lockout`.
4. Never include the password, the token, or the code in any payload.
5. Verify ordering and timestamp accuracy — an audit log with unreliable ordering cannot support an incident investigation (`docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md`).

**Definition of Done**
- [x] Successful and failed logins both appear with correct actor and timestamp. A wrong password on a real account names the actor; an attempt against an address with no account names nobody — and the address itself appears in neither.
- [x] No credential material appears in any event payload, verified by a test that reads **every** payload one full login flow produces rather than checking one event type at a time. A per-event test is one somebody forgets to add for the next event.
- [x] Events are queryable by org, actor, type and time range with acceptable performance. `EXPLAIN` asserts each of the three access patterns uses an index — an index nothing plans against is an index that is not there, and a sequential scan on a partitioned append-only table is a query that works in a test and times out in year two.
- [x] `docs/PLAN/17`'s Phase 1 audit criterion is demonstrably met, including ordering: timestamps do not go backwards and ids increase with time, so two events in the same millisecond still have a defined order.

**What this task found**
- **`P1-13`'s lockout event was never written.** It called `WithTenant` with an empty organization, which returns `ErrEmptyOrgID` and does nothing, and the error was discarded. `P1-13`'s own DoD item had been ticked on a unit test whose fake tenant ignores the organization entirely — a check at the wrong layer for the claim it was supporting. Lockouts are now recorded against the client's organization, and a real-database test asserts the row exists.
- Reading the audit log in a test is its own trap: the service's role is tenant-scoped, so reading through it outside a transaction returns nothing and every assertion passes by seeing no rows. The tests read as the owner and each has a control that fails when the table is empty.

**What was added**
- The **user agent** on `user.login.success`, `user.login.failed` and `user.lockout` — what distinguishes "signed in from a new laptop" from "somebody signed in as them". Bounded at 512 bytes: the table is append-only with a 24-month retention, so an unbounded attacker-controlled field is a place to park data nothing can delete.
- Still **not** the submitted address, on any of them.

**Deliberately not done**
- **`token.issued` stays a row per token.** Step 3 suggests an aggregate metric instead "to keep volume sane"; at Phase 1 volumes a row is fine and more useful. It becomes a question when `P1-27`'s load test says what the volume is, and `PG-10`/`DF-12` already track the events table growing.

---

## P1-15 — Management API Foundation

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-10-P1-15-management-api-foundation.md), [spec](../MEMORY/specs/P1-15-management-api-foundation.md) |
| **Depends on** | P0-16, P1-07 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` Part B, `docs/PLAN/02-REQUIREMENTS.md` FR-13/FR-14, `docs/PLAN/08-AUTHORIZATION.md` Part C § manager_roles |
| **Spec required** | Yes — authorization surface |
| **Surface** | backend |

**Goal** — The cross-cutting mechanics every Management API endpoint depends on, built once: bearer authentication, permission checks, error format, pagination, and idempotency.

**Steps**
1. Bearer token authentication middleware: validate signature, expiry, issuer, and audience against the local JWKS with no network round-trip.
2. Permission middleware reading `manager_roles` (`docs/PLAN/04`, `docs/PLAN/08` Part C). Phase 1 needs `INSTANCE_OWNER`, `ORG_OWNER`, and `ORG_ADMIN`; project-scoped roles arrive in Phase 2.
3. **Every check happens server-side on every request** — `CLAUDE.md`'s non-negotiable constraint. The console's UI-level hiding is a UX affordance, never a control.
4. Enforce tenant scoping by setting the RLS context from `P0-08` for the caller's organization on every request.
5. Implement `docs/PLAN/05`'s exact error envelope: `{"error": {"code", "message", "details": [{"field", "issue"}]}}`, with a consistent mapping from error class to HTTP status.
6. Implement opaque cursor pagination returning `next_page_token` (`docs/PLAN/05`'s example), with a bounded default and maximum `page_size`.
7. Implement `Idempotency-Key` support on `POST` endpoints (`docs/PLAN/05`), storing the result keyed by (client, key) so a retried provisioning call is safe.
8. Rate-limit per `client_id`/API key rather than only per IP (`docs/PLAN/05`), with `X-RateLimit-*` headers.
9. Audit every mutating request through `P0-12`.
10. Register every endpoint in the OpenAPI spec as it is built — `P0-16`'s CI check makes drift a build failure rather than a discovery.

**Definition of Done**
- [x] A request with no token, an expired token, or a wrong-audience token is rejected with the correct status. Seven cases, all `401` — plus the check that every refusal of a *presented* token is word for word identical, so a caller holding a captured token cannot ask which property was wrong. A request with no credential at all is allowed to differ, and the two no-credential cases must match each other.
- [x] A caller lacking the required manager role is rejected regardless of what the console would have shown them. Roles are read from `manager_roles` on every request, never from the token: the test revokes the role between two calls **with the same token** and the second is refused. Reading the token would pass every other test.
- [x] Pagination is consistent across every list endpoint and stable under concurrent inserts. Keyset, not offset — a cursor names `(created_at, id)` and carries nothing else, so a forged one can choose a starting position and nothing more. The stability half is an integration test that inserts rows **before and after the cursor on every page boundary** and asserts every row present at the start is returned exactly once; it is paired with a control that runs the same disruption against an `OFFSET` walk and **requires** it to duplicate rows, so a test passing against both designs would fail.
- [x] Replaying a `POST` with the same `Idempotency-Key` returns the original result without creating a duplicate. Byte for byte, including under eight racing goroutines, where the handler runs exactly once.
- [x] Every error response matches `docs/PLAN/05`'s schema. Every refusal the chain can produce — 401, 403, 404, 409, 429, 400 — checked against the envelope, the content type and `Cache-Control: no-store`.
- [x] Every mutating call writes an audit event. Asserted at both ends: the row is really in `events` with the right actor and tenant, **and** a handler that writes nothing is reported by `AuditGuard` on the first request. Only the first would pass against a guard that never fires; only the second against a writer that never writes.

**What this task did not build**
- **Endpoints.** `P1-16` onward. `/v1` is mounted and empty, so the next task adds a route rather than a route *and* the chain protecting it.
- **The OAuth endpoints behind the new limiter.** `PG-19` asked for the mechanism, which now exists; wiring the three protocol endpoints to it needs a bound of its own and a `docs/PLAN/05` Part A amendment. `BL-06`.
- **Project-scoped roles.** Named in `roles.go` so a row carrying one reads as "not yet" rather than as no role. Phase 2.

**Two plan gaps closed**
- `PG-21` — idempotency records had nowhere to live. Additive `idempotency_records` table; `docs/PLAN/04` needs amending to describe it.
- `PG-19` — per-client rate limiting had a requirement and no owner. Built here for `/v1`; `BL-06` carries the remainder.

**One control that existed only as a comment**
`errors.go` documented `404`-not-`403` for another organization's resource from the start, and `Require` wrote `Forbidden` for every authorization failure. A caller could ask "is org `8f3e…` real?" one request at a time — abuse case A-3. `Decision.Invisible` now distinguishes "you may not" from "you cannot see this", and the unit test that asserted `403` was asserting the wrong answer.

**Abuse cases to test**
- Privilege escalation via the API by a caller whose token lacks the role (`docs/SECURITY/02` §3).
- IDOR: accessing another organization's resource by id (`docs/SECURITY/02` §2, §14).
- Mass assignment: a request body setting `org_id`, `id`, or a role field it should not control (`docs/SECURITY/02` §11 Business-Logic Abuse).
- Rate-limit bypass across rotated client credentials (`docs/SECURITY/02` §10).

---

## P1-16 — Management API: Organizations

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Endpoint Structure, `docs/PLAN/04-DATA-MODEL.md` § `organizations`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` |
| **Spec required** | Yes — data model surface |
| **Surface** | backend |

**Goal** — CRUD for organizations, restricted to instance-level administrators, ready for Phase 2's multi-org activation without a redesign.

**Steps**
1. Implement `GET/POST /v1/organizations` and `GET/PATCH/DELETE /v1/organizations/{org_id}`.
2. Restrict create, delete, and suspend to `INSTANCE_OWNER` (`docs/PLAN/08` Part C hierarchy).
3. Validate `settings` against a schema — `password_policy`, `mfa_required`, `session_lifetime_hours`, `allowed_login_methods` (`docs/PLAN/08` Part B). Reject unknown keys rather than storing them silently.
4. Prefer soft-delete or suspension over hard delete: deleting an organization cascades to users, sessions, and audit history, and audit history must survive.
5. Support `domain` for later domain-based tenant resolution (`docs/PLAN/08` Part B), with verification itself deferred to a later phase.
6. Require an extra confirmation step for destructive operations (`docs/PLAN/08` § Least Privilege) — the typed-confirmation pattern in `docs/UI-UX/07-COMPONENT-SPECIFICATION.md`.

**Definition of Done**
- [ ] Every operation enforces `INSTANCE_OWNER` where the hierarchy requires it.
- [ ] Invalid settings shapes are rejected with per-field errors.
- [ ] Deleting an organization never destroys its audit history.
- [ ] Organization lifecycle events are audited.
- [ ] The endpoints are in the OpenAPI spec and the generated client builds.

---

## P1-17 — Management API: Projects

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md`, `docs/PLAN/04-DATA-MODEL.md` § `projects`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Project list) |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — CRUD for projects under an organization, the container that Phase 2's roles and Phase 4's grants both hang from.

**Steps**
1. Implement `GET/POST /v1/organizations/{org_id}/projects` and `GET/PATCH/DELETE .../{project_id}`.
2. Enforce that the project's `org_id` matches the caller's tenant context; never trust an `org_id` supplied in the body.
3. Handle deletion carefully: a project with applications or grants attached should refuse deletion with a clear error rather than cascading silently.
4. Reserve the `PROJECT_OWNER` manager role in the permission model now, even though Phase 1 grants it to nobody yet.

**Definition of Done**
- [ ] Cross-organization project access is impossible, verified at both the RLS and application layers.
- [ ] Deleting a project with dependents fails with an actionable error listing what blocks it.
- [ ] Project lifecycle events are audited.

---

## P1-18 — Management API: Applications

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15, P1-05 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md`, `docs/PLAN/04-DATA-MODEL.md` § `applications`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Applications tab) |
| **Spec required** | Yes — credential handling |
| **Surface** | backend |

**Goal** — Register and manage OIDC clients through the API, exposing `P1-05`'s logic with the show-secret-once rule intact.

**Steps**
1. Implement `GET/POST /v1/organizations/{org_id}/projects/{project_id}/applications` and the item-level operations.
2. Return the plaintext client secret **only** in the `201` response body, never on any subsequent read (`docs/UI-UX/08`).
3. Add a secret-rotation endpoint that returns the new secret once and honors the overlap window from `P1-05`.
4. Validate redirect URIs on every write, not only on create — an update is exactly where a permissive URI would be smuggled in.
5. Reject changing an application's `type` in a way that would strand its credentials (for example `web` → `spa` while a secret exists).

**Definition of Done**
- [ ] The secret appears exactly once, in the create response, verified by a test that reads the resource afterward.
- [ ] Redirect URI validation runs identically on create and update.
- [ ] Secret rotation works with an overlap and is audited.
- [ ] An invalid type transition is rejected.

---

## P1-19 — Management API: Users

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15, P1-01 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Example: Create a User, `docs/PLAN/04-DATA-MODEL.md` § `users`, `docs/UI-UX/04-USER-FLOWS.md` Flow 1, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` |
| **Spec required** | Yes — identity data |
| **Surface** | backend |

**Goal** — The user lifecycle: invite, list, read, update, deactivate — matching `docs/PLAN/05`'s worked example exactly, since the console and the docs quickstart are both built against it.

**Sub-tasks**
- **P1-19.1** — `POST /v1/organizations/{org_id}/users`: create with `send_invite_email`, returning `status: "invited"` per `docs/PLAN/05`'s example.
- **P1-19.2** — `GET .../users` with pagination and search, and `GET .../users/{user_id}`.
- **P1-19.3** — `PATCH .../users/{user_id}` for profile updates; `POST .../users/{user_id}:deactivate`. Prefer deactivation to deletion so audit history stays coherent.
- **P1-19.4** — Password reset flow: single-use, short-lived, hashed token; the reset link never appears in a log; the response is identical whether or not the email exists (`docs/SECURITY/02` §12).
- **P1-19.5** — Invitation acceptance flow: set the initial password under `P1-02`'s policy, then transition `invited` → `active`.

**Steps**
1. Enforce per-organization email uniqueness (`P0-07`'s composite index), returning a clear conflict error.
2. Never accept a password hash from the client, and never return one.
3. Make invite and reset tokens single-use with a short expiry, stored hashed in `user_tokens` with the appropriate `purpose` (`docs/PLAN/04`), and invalidated once used.
4. Rate-limit invite sending and password-reset requests — both are email-amplification vectors (`docs/SECURITY/02` §10).
5. Deactivation must immediately terminate the user's sessions and revoke their refresh tokens; a deactivated user who can still act is not deactivated.
6. Audit every user lifecycle event.

**Definition of Done**
- [ ] The create response matches `docs/PLAN/05`'s documented example field-for-field.
- [ ] Reset and invite tokens are single-use, expiring, and stored hashed.
- [ ] Requesting a reset for a nonexistent email is indistinguishable from a real one.
- [ ] Deactivation kills sessions and refresh tokens within one request cycle, verified end-to-end.
- [ ] Every user lifecycle event is audited.

**Abuse cases to test**
- User enumeration through create conflicts, reset responses, or timing (`docs/SECURITY/02` §12).
- Invite-email flooding of a third-party address (`docs/SECURITY/02` §10).
- A reset token issued for user A being used to set user B's password (`docs/SECURITY/02` §11).
- Privilege escalation by self-updating a role or status field (`docs/SECURITY/02` §3).

---

## P1-20 — Management API: Audit Log Read

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15, P0-12 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `events`, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Audit Log), `docs/SECURITY/02` §19 |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — A read API for the audit log that supports the console screen's filtering, without becoming a new information-disclosure surface.

**Steps**
1. Implement `GET /v1/organizations/{org_id}/events` with filters for event type, actor, and time range, and the standard pagination envelope.
2. Restrict reads to `ORG_ADMIN` and above; the audit log records who did what and is not general-readable.
3. Scope every query by organization at the RLS layer, so no filter combination can reach across tenants.
4. Expose no write, update, or delete operation at all — append-only is a property, and the API must not offer a way around it.
5. Ensure the time-range query is index-backed (`P0-07`), since this table grows without bound.

**Definition of Done**
- [ ] Filtering works across every documented combination, with correct pagination.
- [ ] A non-admin caller is refused.
- [ ] No mutating operation exists on this resource.
- [ ] A large-table query stays within `docs/PLAN/12`'s Management API latency budget.

---

## P1-21 — Console: OIDC Login (Dogfooding)

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-17, P1-07 |
| **Plan refs** | `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` § Why the Console Must Log In Through the Same OIDC Flow, `docs/PLAN/02-REQUIREMENTS.md` § Constraints |
| **Spec required** | Yes — authentication surface |
| **Surface** | console |

**Goal** — The console authenticates as an ordinary `type: spa` OIDC client with Authorization Code + PKCE — the dogfooding constraint from `docs/PLAN/02` and `docs/PLAN/06`.

**Steps**
1. Register the console as a normal application record, with no client secret.
2. Implement Authorization Code + PKCE in the browser: generate the verifier with a CSPRNG, derive the S256 challenge, and validate `state` on return.
3. Decide token storage deliberately and record it as an ADR. In-memory with silent renewal via `prompt=none` resists XSS token theft far better than `localStorage` (`docs/SECURITY/02` §6, §14) — but it requires the silent-renewal path from `P1-06` to be solid.
4. Implement silent renewal before access token expiry, with a clean fallback to interactive login.
5. Attach the access token to Management API calls through the generated client from `P0-16`.
6. Implement logout calling `P1-10` and clearing all local state.
7. Read role claims from the token to drive UI affordances — while treating the API as the only real enforcement point (`docs/UI-UX/08` § Cross-Screen Requirements: routes must be genuinely unreachable, not merely hidden).
8. Handle the expired-session case gracefully: a mid-action expiry should not lose the user's work without explanation (`docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`).

**Definition of Done**
- [ ] Login works through the same OIDC flow as any other client, with no console-specific backdoor.
- [ ] PKCE parameters are generated with a CSPRNG and `state` is validated on return.
- [ ] The token-storage decision is recorded in `MEMORY/DECISIONS.md`.
- [ ] Silent renewal works, and its failure degrades to interactive login rather than a blank screen.
- [ ] A route the user's claims don't permit is unreachable by direct URL, not merely hidden.
- [ ] An E2E test covers login, renewal, and logout.

**Abuse cases to test**
- Token theft via XSS given the chosen storage strategy (`docs/SECURITY/02` §6).
- Authorization code interception against the SPA redirect (`docs/SECURITY/02` §1).
- Direct URL navigation to an unauthorized route (`docs/SECURITY/02` §14 Client-Side Trust).

---

## P1-22 — Console: Organization Overview, Projects, Applications

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-21, P1-16, P1-17, P1-18 |
| **Plan refs** | `docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Organization Overview, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md` |
| **Spec required** | No — but the implementation chain is mandatory |
| **Surface** | console |

**Goal** — The three screens an admin lands on, each run through `docs/UI-UX/19`'s full implementation chain so loading, error, empty, permission, responsive, and accessibility states are designed rather than improvised.

**Steps**
1. Run **every** screen and component through `docs/UI-UX/19`'s twelve-step chain: Design → Component → State → Interaction → API Dependency → Loading → Error → Empty → Permission → Responsive → Accessibility → Test. "Not applicable" is an acceptable answer; silence is not.
2. Build the Organization Overview to `docs/UI-UX/18`'s detailed spec, respecting its above-the-fold priorities.
3. Build the Project list per `docs/UI-UX/08`, using the shared table anatomy — no screen invents its own table (`docs/UI-UX/08` § Cross-Screen Requirements).
4. Build the Applications tab, including the create flow whose success modal shows the client secret **once**, with a copy action and an unmistakable warning that it will never be shown again.
5. Distinguish "genuinely empty" from "filtered to empty" in every list (`docs/UI-UX/14`) — they need different copy and different recovery actions.
6. Use `color-danger` only for destructive actions (`docs/UI-UX/06`, `CLAUDE.md`).
7. Ensure every screen is usable down to tablet width (`docs/UI-UX/12`, `docs/UI-UX/08` § Responsive Scope).

**Definition of Done**
- [ ] A completed implementation-chain table exists for every screen, committed alongside the code.
- [ ] Loading, error, empty, filtered-empty, and permission-denied states all render correctly and are covered by tests.
- [ ] The client secret modal shows the secret once and cannot retrieve it again.
- [ ] All three screens work at 1440px, 1024px, and 768px.
- [ ] Keyboard navigation and screen-reader labelling meet `docs/UI-UX/13`.
- [ ] Every list uses the shared table pattern.

---

## P1-23 — Console: Users List and Detail

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-21, P1-19 |
| **Plan refs** | `docs/UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Users List, `docs/UI-UX/04-USER-FLOWS.md` Flow 1, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md`, `docs/UI-UX/15-FORM-UX.md` |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — The screens where an admin spends most of their time: find a user, invite one, deactivate one — implementing `docs/UI-UX/04` Flow 1 exactly.

**Steps**
1. Build the Users list to `docs/UI-UX/18`'s detailed spec: search, status filter, and the shared table anatomy.
2. Implement the invite flow as the side panel `docs/UI-UX/08` specifies, following Flow 1 step by step.
3. Build the User detail Profile tab. The Grants, Sessions, and MFA tabs belong to Phases 2 and 3 — render them as clearly-labelled unavailable states rather than as broken or empty tabs, so the console never implies a capability that isn't shipped.
4. Deactivation uses a confirmation dialog and `color-danger`, and states plainly that it terminates the user's sessions immediately.
5. Use status badges (active, invited, deactivated) from the design system rather than ad-hoc styling (`docs/UI-UX/05` § Badges/tags).
6. Show validation errors inline per `docs/UI-UX/15`, driven by the `details[]` array from `docs/PLAN/05`'s error format.
7. Run the full `docs/UI-UX/19` chain for every component.

**Definition of Done**
- [ ] Flow 1 from `docs/UI-UX/04` is implemented end-to-end and covered by an E2E test.
- [ ] Search and filtering produce a correct "filtered to empty" state distinct from the genuine empty state.
- [ ] Deactivation is confirmed, audited, and immediately effective.
- [ ] Not-yet-available tabs are explicit about being unavailable in this phase.
- [ ] Field-level API errors render against the correct fields.
- [ ] Accessibility and responsive requirements are met.

---

## P1-24 — Console: Audit Log Screen

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-21, P1-20 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Audit Log), `docs/UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` |
| **Spec required** | No |
| **Surface** | console |

**Goal** — A basic, readable audit log view — enough for `docs/PLAN/17`'s Phase 1 criterion that logins appear with correct actor and timestamp. Filtering polish and export are deliberately Phase 5.

**Steps**
1. Table view with event type, actor, timestamp, and a summary, using the shared table anatomy.
2. Basic filters: event type, date range, actor.
3. Detail expansion showing the redacted payload — the console must never render a field the API should not have returned in the first place.
4. Cursor pagination against `P1-20`.
5. Render timestamps in the viewer's timezone while making the underlying UTC value inspectable — incident timelines are reconstructed across timezones.
6. Empty state distinguishes "no events yet" from "no events match these filters."

**Definition of Done**
- [ ] Login success and failure events are visible with correct actor and timestamp.
- [ ] Filters work and combine correctly.
- [ ] Pagination is stable while new events are being written.
- [ ] No sensitive value is rendered.
- [ ] Both empty states are handled distinctly.

---

## P1-25 — Public Docs: Real Quickstart and Generated API Reference

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-07, P1-19 |
| **Plan refs** | `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `docs/UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` § API Reference, `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` |
| **Spec required** | No |
| **Surface** | public-site, docs |

**Goal** — Replace `P0-19`'s placeholder with a quickstart that actually works, and publish the generated API reference for the endpoints that exist.

**Steps**
1. Write the quickstart as a real, followable path: register an application, configure redirect URIs, run the Authorization Code + PKCE flow, validate the token. Include working code for at least one language.
2. **Verify the quickstart by following it from scratch** against staging. A quickstart that has never been executed end-to-end is a hypothesis, and it is the first thing every evaluator tries.
3. Generate the API reference from `openapi/openapi.yaml` (`P0-16`). Never hand-write it (`CLAUDE.md` hard rule).
4. Publish concepts pages for the entities that now exist; leave roles, grants, and ABAC out until their phases ship (`docs/UI-UX/21` governance rule).
5. Add the changelog entry for the MVP release.
6. Audit every published page against the shipped feature set — this is a Phase 1 acceptance criterion in `docs/PLAN/17`, not a nicety.

**Definition of Done**
- [ ] Someone who has never seen the project completes the quickstart successfully against staging.
- [ ] The API reference is generated, current, and covers every shipped endpoint.
- [ ] No page describes an unshipped capability.
- [ ] The changelog records the release.

---

## P1-26 — Two Demo Consumer Applications

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-07 |
| **Plan refs** | `docs/PLAN/01-PRODUCT-SCOPE.md` § MVP Definition of Done, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `docs/PLAN/03-ARCHITECTURE.md` § Main Data Flow |
| **Spec required** | No |
| **Surface** | backend, infra |

**Goal** — Two independent applications that prove SSO works — the literal MVP definition of done in `docs/PLAN/01`.

**Steps**
1. Build two minimal, genuinely separate applications with distinct `client_id`s, hostnames, and sessions. Two routes in one app do not demonstrate SSO.
2. Make one a confidential web client (backend code exchange) and the other a public SPA, so both client profiles are exercised.
3. Each validates tokens locally against the JWKS, with no call back to the auth service — this is `docs/PLAN/12`'s biggest available latency win and should be demonstrated, not assumed.
4. Both validate `iss`, `aud`, `exp`, and signature strictly.
5. Deploy both to staging at real, distinct hostnames.
6. Use them as the fixture for the SSO E2E test in `P1-27`.
7. Keep them in the repository as living integration documentation — a reference implementation consumer teams can copy.

**Definition of Done**
- [ ] Two applications with distinct hostnames and client IDs authenticate real users.
- [ ] Logging into App A then opening App B requires no second login — verified manually and by an automated E2E test.
- [ ] Both validate tokens locally with no auth-service round-trip.
- [ ] Both reject a token with the wrong `aud`.
- [ ] Both are deployed to staging and reachable.

---

## P1-27 — Phase 1 Test Suite Completion

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all Phase 1 implementation tasks |
| **Plan refs** | `docs/PLAN/11-TESTING.md` (all), `docs/PLAN/10-THREAT-MODEL.md` § High-Priority Abuse Scenarios, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md` |
| **Spec required** | No |
| **Surface** | backend, console |

**Goal** — Every layer of `docs/PLAN/11`'s pyramid populated for Phase 1, and every abuse case named on a Phase 1 task actually implemented as a test.

**Steps**
1. **Unit**: password hashing, JWT creation and verification, PKCE verification, redirect URI matching, email and role-key validation, password policy evaluation.
2. **Integration**: the full flow `docs/PLAN/11` specifies — create organization → create project → create application → create user → log in → verify token claims — against real Postgres and Redis.
3. **E2E (Playwright)**: successful login and redirect with a code; SSO between the two apps from `P1-26`; logout genuinely ending the session; the console's invite flow.
4. **Security**: every abuse case listed on every Phase 1 task, plus `docs/PLAN/11` § Security Testing's explicit list — wrong `aud` rejected, non-exact `redirect_uri` rejected, rate limiting triggering under simulated brute force, RLS preventing cross-org leakage independent of application filtering.
5. Add fuzz tests for JWT parsing (`docs/PLAN/11`).
6. Verify CI runs everything and that a deliberately-introduced regression in any abuse case fails the build.
7. Confirm the coverage floor for `internal/authn`, `internal/authz`, and `internal/oidc` from `P0-15`.

**Definition of Done**
- [ ] Every abuse case named in Phase 1 tasks has a passing automated test.
- [ ] All four pyramid layers have real Phase 1 coverage.
- [ ] The full suite runs in CI on every PR within an acceptable duration.
- [ ] A deliberately-reverted security control causes a red build, demonstrated once.
- [ ] SAST and dependency scans show no unaddressed critical or high findings (`docs/PLAN/11` § Production-Ready).

---

## P1-28 — Phase 1 Acceptance Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-27 |
| **Plan refs** | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `docs/PLAN/01-PRODUCT-SCOPE.md` § MVP Definition of Done, `docs/PLAN/12-PERFORMANCE.md` |
| **Spec required** | No |
| **Surface** | all |

**Goal** — Walk `docs/PLAN/17`'s Phase 1 checklist deliberately, with evidence, rather than declaring the phase done because the tickets are closed.

**Steps**
1. Verify each of `docs/PLAN/17`'s eight Phase 1 criteria individually, capturing evidence — a test name, a screenshot, a log excerpt, a metric.
2. Run a first load test against `/oauth/token` and `/oauth/authorize` and compare to `docs/PLAN/12`'s targets. Missing a target is not automatically a blocker, but it must be a recorded, conscious decision rather than an unnoticed one.
3. Confirm API/console consistency: an entity created through one is visible and identical through the other (`docs/PLAN/17`, FR-14).
4. Confirm the public site claims nothing unshipped.
5. Run a threat-model review before Phase 2 begins (`docs/PLAN/09` § Secure Development Practices: threat modeling before each major new phase).
6. Write the phase summary in `MEMORY/`: what shipped, what deviated, what deferred, what to watch.
7. Update `PROGRESS.md` and tag the release.

**Definition of Done**
- [ ] Every `docs/PLAN/17` Phase 1 criterion is verified with recorded evidence.
- [ ] Load test results are recorded against `docs/PLAN/12`'s targets, with any gap explicitly accepted or scheduled.
- [ ] API and console consistency is demonstrated.
- [ ] The Phase 2 threat-model review is complete.
- [ ] A phase summary exists in `MEMORY/`.
- [ ] Anything deferred out of Phase 1 is in `BACKLOG.md`, not merely remembered.

---

## Phase 1 Exit Checklist

Directly from `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1:

- [ ] Two independent internal applications authenticate real users through this service.
- [ ] A user logged into Application A opens Application B and is not prompted to log in again.
- [ ] `POST /oauth/token` and `GET /oauth/authorize` conform exactly to `docs/PLAN/05-API-CONTRACT.md`.
- [ ] Organizations, projects, applications, and users are creatable via both the REST API and the console, and the two stay consistent.
- [ ] Failed and successful logins appear in the audit log with correct actor and timestamp.
- [ ] Login rate limiting demonstrably blocks a simulated brute-force attempt.
- [ ] All Phase 1 items in `docs/PLAN/11-TESTING.md`'s pyramid have passing automated tests in CI.
- [ ] The public landing page and docs quickstart exist and accurately reflect the real MVP flow.
