# Phase 1 — MVP: Core Auth + Basic SSO

**Goal**: a working OIDC provider that real applications can log into, with sessions that make SSO actually happen, a Management API and console for the four core entities, an audit trail, and rate limiting — all for a single organization.

**Why the scope stops where it does**: no roles, no multi-org, no MFA, no SAML. `PLAN/00-PROJECT-CONTEXT.md`'s incremental principle and `PLAN/16`'s phase rule both say the same thing — the OIDC flow and session model must be provably correct before anything is layered on top of them. Every later phase inherits whatever is wrong here.

**Definition of done for the phase** (`PLAN/01-PRODUCT-SCOPE.md` § MVP Definition of Done, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1): two independent internal applications authenticate real users through this service, and a user logged into Application A opens Application B without being prompted to log in again.

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
| P1-12 | Hosted login page | backend, console | M | P1-01, P1-11 |
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
| **Plan refs** | `PLAN/09-SECURITY.md` § Passwords & Credentials, `PLAN/07-BACKEND-ARCHITECTURE.md` § Cryptography, `PLAN/11-TESTING.md` § Unit Testing |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — Password storage that stays sound as hardware improves, with parameters recorded per hash so they can be raised without invalidating existing passwords.

**Steps**
1. Implement hashing with Argon2id, using a modern parameter set tuned to the target server's memory and CPU (`PLAN/07`: "parameters tuned to server capacity").
2. Encode parameters into the stored hash string (the standard PHC format), so a future parameter increase is detectable per row.
3. Implement transparent rehash-on-login: when a user authenticates successfully against a hash weaker than the current parameters, rehash and store the stronger one.
4. Use constant-time comparison, and ensure a nonexistent user costs approximately the same wall-clock time as an existing one — otherwise timing distinguishes them (`SECURITY/02` §12 Enumeration).
5. Keep bcrypt verification available only if an actual migration from a legacy system requires it (`PLAN/07` names it as a compatibility fallback); do not add it speculatively.
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
| **Plan refs** | `PLAN/09-SECURITY.md` § Passwords & Credentials, `PLAN/08-AUTHORIZATION.md` Part B § Policies per Organization, `UI-UX/15-FORM-UX.md` |
| **Spec required** | Yes — authentication |
| **Surface** | backend |

**Goal** — Password rules that are enforced server-side and read from organization settings, so Phase 2's per-org policy configuration plugs into an existing mechanism rather than replacing a hard-coded one.

**Steps**
1. Implement a policy evaluator reading from `organizations.settings.password_policy` — the shape is already specified in `PLAN/08` Part B (`min_length`, `require_uppercase`, `max_age_days`).
2. In Phase 1 the settings row holds instance defaults for the single default organization; the evaluator must not hard-code the values, because Phase 2 (`P2-14`) makes them editable per org.
3. Add breached-password checking against a k-anonymity API (`PLAN/09`), sending only a hash prefix — never the password, and never the full hash.
4. Decide and document behavior when the breach-check service is unreachable: fail open (accept the password) or fail closed (reject the change)? Unlike an authorization decision, failing closed here blocks legitimate password changes during an outage. Record the choice and its reasoning as an ADR.
5. Return validation failures in `PLAN/05`'s standard error format with per-field `details`, so the console can render them inline (`UI-UX/15-FORM-UX.md`).
6. Never state which specific rule failed in a way that reveals policy detail to an unauthenticated caller during login; full detail is fine on an authenticated password-change form.

**Definition of Done**
- [x] Policy evaluation is a pure function with table-driven unit tests, including boundary lengths. Both sides of every boundary — a test that only checks that 11 characters is rejected passes against an implementation that rejects everything.
- [x] The breached-password check transmits only a hash prefix, verified by an outbound-request test. The assertion is itself checked against a synthetic leaky request, so it is known to be able to fail.
- [x] The fail-open/fail-closed decision is recorded in `MEMORY/DECISIONS.md` — **ADR-015**: fail open, with an audit event, a metric label and an alert on every skip.
- [x] Validation errors match `PLAN/05`'s error schema exactly. Asserted on the serialized JSON rather than the Go struct, which would pass against wrong `json` tags.
- [x] Changing `organizations.settings.password_policy` changes enforcement with no code change. An integration test `UPDATE`s a live row and re-reads through the same process, for two different fields.

**Beyond the stated steps**

`PG-13` found and registered: `max_age_days` has been in the specified policy since `PLAN/08` with nothing in the schema to evaluate it against. Closed by an additive `users.password_changed_at`, where NULL means *not* expired — the alternative makes deploying the migration a mass lockout.

An 8-character floor no configuration can go below, because otherwise `"min_length": 1` is valid and the control an administrator was given is the control they can silently remove. Length counted in runes over NFC, since a byte count is a different policy per language.

**One real bug, found by a test written before the code it tests**: the corpus response parser counted lines, and an HTML error page is one line — so a proxy answering `200` with "Access denied" produced a `clean` verdict and admitted the password with nothing recording that no check had happened. An always-open path the ADR's own metric would have shown as healthy. It now counts well-formed entries.

---

## P1-03 — Signing Key Management, JWKS, and Rotation

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-03-signing-keys.md), [spec](../MEMORY/specs/P1-03-signing-keys.md) |
| **Depends on** | P0-14 |
| **Plan refs** | `PLAN/09-SECURITY.md` § Tokens & Keys, `PLAN/07-BACKEND-ARCHITECTURE.md` § Cryptography, `PLAN/14-DEPLOYMENT.md` § Rollback Strategy, `PLAN/02-REQUIREMENTS.md` § Constraints |
| **Spec required** | Yes — cryptographic core |
| **Surface** | backend |

**Goal** — Asymmetric signing keys the service alone holds, rotatable with an overlap window, so that neither a rotation nor an application rollback ever invalidates tokens that should still be valid.

**Steps**
1. Generate RS256 or ES256 key pairs. Never HS256 — consumer services must verify with a public key and no shared secret (`PLAN/07`).
2. Store private keys in the secret manager from `P0-14`. `PLAN/02`'s constraint is absolute: no third-party dependency holds the private signing key outside this service's own infrastructure.
3. Support **multiple simultaneously-active keys**, each with a stable `kid`, in three states: `next` (published, not yet signing), `current` (signing), `previous` (still verifying, no longer signing).
4. Publish all non-retired public keys at the JWKS endpoint, so a token signed just before rotation still verifies afterward (`PLAN/09`'s overlap period).
5. Implement rotation as an explicit operational command, not an automatic timer, for the first release — a scheduled rotation that fails at 3am is worse than a deliberate one during business hours. Note the 90-day cadence from `PLAN/09` as the operational expectation.
6. Handle the rollback case `PLAN/14` calls out: rolling the application back must not invalidate tokens signed by a newer key, so key state lives in the `signing_keys` table (`PLAN/04`), not in the binary or its configuration.
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
- `alg: none` and algorithm-confusion (HS256 signed with the RSA public key) are both rejected — verification must pin the expected algorithm rather than trusting the header (`SECURITY/02` §1).

---

## P1-04 — Discovery Document and JWKS Endpoint

| | |
|---|---|
| **Status** | DONE (two items deferred) — [record](../MEMORY/records/2026-09-09-P1-04-discovery-jwks.md) |
| **Depends on** | P1-03 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Core Endpoints, `PLAN/03-ARCHITECTURE.md` § OIDC/OAuth2 Provider |
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
- [ ] An off-the-shelf OIDC client library configures successfully from the discovery URL alone. **Deferred to P1-07, deliberately unticked.** A conforming library needs `authorization_endpoint` and `token_endpoint` to finish configuring, and neither exists yet. Claiming this now would be the exact false claim step 2 of this task exists to prevent. The half that can be verified today is covered: `TestDiscoveryDocumentLeadsToTheKeySet` follows `jwks_uri` out of the document the way a client library would and asserts it reaches the current key.
- [x] `code_challenge_methods_supported` contains `S256` and does not contain `plain`. Also absent entirely until there is an authorization endpoint to apply it to.
- [x] Neither `implicit` nor `password` appears in `grant_types_supported` (`PLAN/05`: both explicitly unsupported). Enforced at construction, not just left out — `Capabilities.Validate` refuses to build a handler that would advertise either.
- [ ] `issuer` matches the `iss` claim, asserted by an integration test. **Deferred to P1-07, deliberately unticked** — nothing emits an `iss` claim yet. What holds today is the single-source property: the document publishes `cfg.Issuer` verbatim and unnormalised.
- [x] JWKS exposes no private key parameters, asserted by a test that inspects the JSON keys.

**Beyond the stated steps**

Serving these two endpoints through the generated strict router rather than beside it made the split-interface problem visible: `httpserver.apiRoutes` now embeds both implementations and carries the compile-time assertion. Two bugs surfaced while verifying the cache headers — every endpoint answered 405 to HEAD (chi matches methods exactly), and `Server.Handler()` returned the bare mux rather than the served handler, so the first HEAD test passed against a server that was broken. Both fixed; see the record.

---

## P1-05 — Application (OIDC Client) Registration and Credentials

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-09-P1-05-application-registration.md), [spec](../MEMORY/specs/P1-05-application-registration.md) |
| **Depends on** | P0-07 |
| **Plan refs** | `PLAN/04-DATA-MODEL.md` § `applications`, `PLAN/09-SECURITY.md`, `UI-UX/08-PAGE-SPECIFICATIONS.md` (Applications tab), `SECURITY/02` §16 |
| **Spec required** | Yes — credential handling |
| **Surface** | backend |

**Goal** — Registered OIDC clients whose secrets are shown exactly once and stored only as hashes, with redirect URIs validated strictly enough to close the open-redirect class of attack.

**Steps**
1. Implement the application record per `PLAN/04`: the `id` doubles as the `client_id`, with `type` in {`web`, `native`, `spa`, `api`, `saml`}, `redirect_uris`, `grant_types`, and `post_logout_redirect_uris`.
2. Generate client secrets for confidential clients only. Public clients (`spa`, `native`) get no secret and must use PKCE.
3. Hash the secret before storage (`client_secret_hash` in `PLAN/04`) and return the plaintext exactly once at creation. `UI-UX/08`: "Secrets shown once at creation only, never retrievable again."
4. Validate `redirect_uris` at registration time: absolute URIs, no fragments, no wildcards, and HTTPS required except for explicitly-allowed localhost during development.
5. Implement redirect URI matching as **exact string comparison**, never prefix or pattern matching (`PLAN/09` § Protection Against Common Attacks: open redirect).
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

The card's citation of `SECURITY/02` §7 (SSRF) for the internal-address case is corrected in the record: a `redirect_uri` is never fetched server-side, so the risk is a code delivered to a host the user's machine can reach rather than a forged server request. Link-local is refused; private ranges are allowed, because an intranet redirect is a legitimate self-hosted configuration.

Two bugs found by tests written before the code they cover: `fmt.Stringer` does not redact under `%d`, and the loopback check was case-sensitive while hostnames are not.

**Abuse cases to test**
- Open redirect via a `redirect_uri` that merely shares a prefix with a registered one (`SECURITY/02` §1).
- A public client attempting the `client_credentials` grant is rejected.
- A registered `redirect_uri` pointing at an internal address is rejected or explicitly reviewed (`SECURITY/02` §7 SSRF).

---

## P1-06 — `GET /oauth/authorize` — Authorization Code + PKCE

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-05, P1-11 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` Part A, `PLAN/03-ARCHITECTURE.md` § Main Data Flow (SSO), `PLAN/09-SECURITY.md`, `PLAN/12-PERFORMANCE.md` |
| **Spec required** | Yes — authentication core |
| **Surface** | backend |

**Goal** — The endpoint that makes SSO feel instantaneous: if a valid session cookie exists, issue a code without showing anything; otherwise route to the login page and return here afterward.

**Steps**
1. Validate parameters strictly before doing anything else: `client_id` exists, `redirect_uri` matches exactly, `response_type=code`, `scope` requested is permitted, `state` present, `code_challenge` present with `code_challenge_method=S256`.
2. Make PKCE **mandatory for every client type**, not only public ones — `PLAN/05` states this explicitly, and it is stricter than the base OAuth 2.1 requirement.
3. On a parameter error, decide correctly where the error goes: an invalid `client_id` or `redirect_uri` must render an error page and must **never** redirect, because redirecting to an unvalidated URI is the open-redirect vulnerability itself. Other errors redirect to the validated `redirect_uri` with `error` and the original `state`.
4. Check for an existing SSO session (`P1-11`). If valid and satisfying any session-age requirement, skip the login page entirely — this is the silent-SSO path `PLAN/12` targets at p95 < 150ms.
5. If no session, store the pending authorization request server-side keyed by an opaque identifier, redirect to the hosted login page, and resume exactly where it left off after successful authentication.
6. Honor `prompt=none` (return `login_required` rather than showing UI) and `prompt=login` (force re-authentication even with a session), since consumer SPAs rely on both for silent token renewal.
7. Issue a single-use authorization code with a short lifetime (60 seconds or less), bound to the client, redirect URI, session, and `code_challenge`.
8. Store the code and its `code_challenge` in Redis with a sub-60-second TTL, per `PLAN/04` § What Is Deliberately Not Stored Here, so an unredeemed code cannot linger.
9. Emit metrics distinguishing the silent-SSO path from the login-required path, since `PLAN/12` sets a separate latency target for the former.

**Definition of Done**
- [ ] A request missing `code_challenge` is rejected for every client type, including confidential ones.
- [ ] `code_challenge_method=plain` is rejected.
- [ ] An invalid `client_id` or `redirect_uri` produces an error page and no redirect whatsoever.
- [ ] With a valid session, the endpoint returns a code with no user interaction.
- [ ] `prompt=none` without a session returns `login_required` to the client rather than rendering a login page.
- [ ] Codes are single-use, expire within 60 seconds, and are bound to the issuing client and redirect URI.
- [ ] The silent-SSO path meets `PLAN/12`'s p95 target under the load test.

**Abuse cases to test**
- Authorization code interception without the verifier (`PLAN/10` § High-Priority Abuse Scenarios).
- Code replay: a redeemed code is rejected on second use, and the associated session is flagged.
- A code issued to client A cannot be redeemed by client B.
- CSRF on the authorization endpoint: a missing or mismatched `state` (`SECURITY/02` §5).

---

## P1-07 — `POST /oauth/token`

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-06, P1-03 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` Part A, `PLAN/09-SECURITY.md` § Tokens & Keys, `PLAN/12-PERFORMANCE.md`, `PLAN/08-AUTHORIZATION.md` Part A |
| **Spec required** | Yes — authentication core |
| **Surface** | backend |

**Goal** — The highest-volume, most latency-sensitive endpoint in the system: exchange a code or refresh token for an ID token and access token, correctly and fast.

**Steps**
1. Support exactly three grants in Phase 1: `authorization_code`, `refresh_token`, and `client_credentials`. Reject `password` and `implicit` explicitly (`PLAN/05`).
2. Authenticate the client: `client_secret_basic` or `client_secret_post` for confidential clients; public clients are identified by `client_id` and authenticated solely by the PKCE verifier.
3. Verify the PKCE `code_verifier` against the stored `code_challenge` using S256, in constant time.
4. Verify that the code is unredeemed, unexpired, and bound to this client and this `redirect_uri`. Mark it redeemed atomically — a race between two concurrent redemptions must result in exactly one success.
5. Issue an ID token with `iss`, `sub`, `aud`, `exp`, `iat`, `auth_time`, `nonce` (echoed when supplied), and `amr` reflecting the actual authentication methods used (`PLAN/05` § MFA). `amr` is `["pwd"]` in Phase 1 and grows in Phase 3; consumer apps use it for step-up decisions.
6. Issue an access token with a 5–15 minute lifetime (`PLAN/09`), carrying `org_id` and reserving the role-claim namespace that Phase 2 (`P2-04`) will populate.
7. Issue a refresh token with a longer lifetime, stored only as a hash (`PLAN/04`). Full rotation with reuse detection is Phase 3 (`P3-06`), but the storage shape must not need changing then.
8. Return the standard OAuth error codes with the correct HTTP status; never leak internal detail in an error body.
9. Set `Cache-Control: no-store` on every token response.
10. Instrument latency to `PLAN/12`'s targets: p50 < 50ms, p95 < 200ms, p99 < 400ms.

**Definition of Done**
- [ ] Each supported grant works end-to-end against an integration test with a real database.
- [ ] `password` and `implicit` grants are rejected with the correct OAuth error.
- [ ] A wrong `code_verifier` is rejected.
- [ ] Concurrent redemption of one code yields exactly one success, verified by a parallel test.
- [ ] Tokens carry the exact claim set above, and `aud` and `iss` are correct for the requesting client.
- [ ] No refresh token is stored in plaintext.
- [ ] The endpoint meets `PLAN/12`'s latency targets under the load test.

**Abuse cases to test**
- Code replay after successful redemption (`PLAN/10`).
- A token with the wrong `aud` is rejected by the resource server (`PLAN/11` § Security Testing).
- Client authentication bypass: redeeming a confidential client's code without its secret.
- Cross-client code redemption.
- Token substitution: an access token used where an ID token is expected, and vice versa.

---

## P1-08 — `GET /oauth/userinfo`

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-07 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Core Endpoints, `PLAN/12-PERFORMANCE.md`, `PLAN/10-THREAT-MODEL.md` § Information Disclosure |
| **Spec required** | Yes — data exposure |
| **Surface** | backend |

**Goal** — Return the claims the access token's scopes actually authorize, and nothing beyond them. `PLAN/12` notes consumer SPAs often call this on every page load, so it must be both fast and minimal.

**Steps**
1. Authenticate via the bearer access token; validate signature, expiry, issuer, and audience.
2. Map scopes to claims: `openid` → `sub`; `profile` → name and related; `email` → `email` and `email_verified`. A claim outside the granted scopes is never returned.
3. Return `sub` as a stable, non-guessable identifier — never a sequential value, and never the email (`SECURITY/02` §12 Enumeration).
4. Serve from a read replica or a short-TTL cache where `PLAN/12` allows, since this is a read-heavy path.
5. Reject an expired or revoked token with `401` and the standard `WWW-Authenticate` challenge.

**Definition of Done**
- [ ] Claims returned are exactly those the granted scopes permit, verified by test across scope combinations.
- [ ] An expired token yields `401` with a correct challenge header.
- [ ] No organization-internal identifiers leak beyond what the scopes justify.
- [ ] The endpoint meets `PLAN/12`'s p95 < 100ms target.

---

## P1-09 — `POST /oauth/introspect` and `POST /oauth/revoke`

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-07 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Core Endpoints, `PLAN/09-SECURITY.md` |
| **Spec required** | Yes — token lifecycle |
| **Surface** | backend |

**Goal** — Resource servers can check a token's validity, and clients can revoke tokens they no longer need.

**Steps**
1. Introspection requires client authentication — an unauthenticated introspection endpoint is a token oracle.
2. Return `{"active": false}` for any token that is invalid, expired, revoked, or simply unknown. Never distinguish between those cases: distinguishing them is an enumeration primitive (`SECURITY/02` §12).
3. Revocation accepts both access and refresh tokens, and revoking a refresh token also invalidates its lineage.
4. Per RFC 7009, revocation returns `200` even for an unknown token, again to avoid disclosure.
5. Rate-limit both endpoints per client (`PLAN/05` § Rate Limiting).
6. Audit revocations; do not audit routine introspection, which would flood the log without adding signal.

**Definition of Done**
- [ ] Unauthenticated introspection is rejected.
- [ ] An unknown, an expired, and a revoked token produce byte-identical responses.
- [ ] Revoking a refresh token invalidates tokens derived from it.
- [ ] Revocation events appear in the audit log.

**Abuse cases to test**
- Using introspection to enumerate valid tokens by response-shape or timing differences.
- One client revoking another client's token.

---

## P1-10 — `GET /oidc/logout` — End Session

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-11 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Session & logout, `PLAN/04-DATA-MODEL.md` § `sessions` |
| **Spec required** | Yes — session lifecycle |
| **Surface** | backend |

**Goal** — Logout that genuinely ends the session, plus the "log out of all sessions" action `PLAN/05` requires for the MVP.

**Steps**
1. Implement RP-initiated logout: validate `post_logout_redirect_uri` against the client's registered list with exact matching — the same open-redirect discipline as `P1-05`.
2. Validate `id_token_hint` where supplied, so an attacker cannot force-log-out an arbitrary user by URL.
3. Terminate the session server-side and clear the cookie. Deleting the cookie alone is not logout — the server-side record must go, or a copied cookie still works.
4. Implement "log out of all sessions": terminate every session for the user and revoke their refresh tokens.
5. Show a confirmation interstitial when there is no valid `id_token_hint`, rather than acting on an unauthenticated GET.
6. Audit every logout with the scope of what was terminated.
7. Leave back-channel logout to a later phase, as `PLAN/05` explicitly defers it — but make sure this design does not preclude it.

**Definition of Done**
- [ ] After logout, a subsequent `/oauth/authorize` requires full re-authentication.
- [ ] A replayed session cookie captured before logout is rejected.
- [ ] `post_logout_redirect_uri` matching is exact; an unregistered value is refused.
- [ ] "Log out of all sessions" terminates every session and revokes refresh tokens.
- [ ] Logout events are audited.

**Abuse cases to test**
- Forced logout of another user via a crafted logout URL (`SECURITY/02` §5).
- Session still usable after logout (`SECURITY/02` §4).

---

## P1-11 — Session Management and the SSO Cookie

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-07 |
| **Plan refs** | `PLAN/04-DATA-MODEL.md` § `sessions`, `PLAN/05-API-CONTRACT.md` § Session & logout, `PLAN/09-SECURITY.md`, `SECURITY/02` §4, `PLAN/03-ARCHITECTURE.md` § Main Data Flow |
| **Spec required** | Yes — the mechanism SSO depends on |
| **Surface** | backend |

**Goal** — The single mechanism that makes step 6 of `PLAN/03`'s data flow work: a browser session at the Auth Service that lets the second application skip login entirely.

**Steps**
1. Create a session on successful authentication, persisting id, `user_id`, `created_at`, `expires_at`, `auth_methods`, `ip`, and `user_agent` (`PLAN/04`).
2. Populate `auth_methods` accurately — Phase 3's step-up authentication reads it, and a value that was never trustworthy cannot be made trustworthy later.
3. Set the cookie `HttpOnly`, `Secure`, `SameSite=Lax`, with `Path=/` and no `Domain` attribute broader than necessary (`PLAN/05`).
4. Use a cryptographically random session identifier with sufficient entropy; the cookie carries only an opaque id, never user data.
5. **Regenerate the session identifier after successful login**, defeating session fixation (`PLAN/09`, `SECURITY/02` §4).
6. Store sessions in Redis for fast lookup, with Postgres as the durable record backing the "active sessions" screen — reconcile the two explicitly rather than letting them drift.
7. Enforce both an absolute lifetime and an idle timeout, defaulting from `organizations.settings.session_lifetime_hours` (`PLAN/08` Part B) so Phase 2 can make it per-org without a rewrite.
8. Bind the session loosely to context: record IP and user agent for the sessions screen and Phase 3's anomaly detection, but do not hard-fail on IP change — mobile networks change addresses legitimately, and hard-failing would break real users.
9. Provide session revocation as an internal API, used by `P1-10` and by Phase 3's self-service screen.

**Definition of Done**
- [ ] The session identifier changes after login, verified by test (fixation defense).
- [ ] Cookie attributes are exactly as specified, asserted by an integration test on the `Set-Cookie` header.
- [ ] Session lookup adds no more latency than `PLAN/12`'s silent-SSO budget allows.
- [ ] Expired sessions are unusable and are cleaned up rather than accumulating.
- [ ] Revoking a session takes effect immediately for the next request, not after a cache TTL.
- [ ] `auth_methods` reflects reality and is used to build `amr` in `P1-07`.

**Abuse cases to test**
- Session fixation: a pre-login session identifier is not honored post-login.
- Session hijacking via a stolen cookie: detectable in the sessions list and revocable.
- Cookie sent over plain HTTP is never accepted (`Secure` enforced).
- Concurrent session limits, if any, behave predictably.

---

## P1-12 — Hosted Login Page

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-01, P1-11 |
| **Plan refs** | `PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 1, `UI-UX/15-FORM-UX.md`, `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `UI-UX/13-ACCESSIBILITY.md`, `SECURITY/02` §5, §6 |
| **Spec required** | Yes — authentication surface |
| **Surface** | backend, console |

**Goal** — The centralized login page every application redirects to: minimal, fast, accessible, and free of the information leaks that make credential attacks cheap.

**Steps**
1. Server-render the login page from the auth service itself (not the console SPA) so it works with JavaScript disabled and has no dependency on the console's deploy cycle.
2. Form with email/username and password, plus CSRF protection on the POST.
3. Apply the `UI-UX/15-FORM-UX.md` field-label-helper-error pattern and `UI-UX/13`'s accessibility requirements: labelled inputs, an error summary linked to fields, and keyboard-only completability.
4. **Uniform error messaging**: a wrong password and a nonexistent account produce the identical message and comparable response time (`SECURITY/02` §12; the timing side is `P1-01`'s work).
5. Carry the pending authorization request through the login round-trip by opaque server-side reference, never by echoing the full parameter set through a hidden field.
6. Set restrictive security headers on this page specifically: a strict `Content-Security-Policy` with no inline script, `X-Frame-Options: DENY` (a login page must never be framable — clickjacking), `Referrer-Policy: no-referrer`.
7. Render organization branding — logo and `color-accent` only — per `UI-UX/05`'s constraint that danger and warning colors are never tenant-overridable.
8. Add the "forgot password" entry point, with the reset flow itself scoped as `P1-19.4` alongside user management.
9. Support the `prompt=login` re-authentication path from `P1-06`.

**Definition of Done**
- [ ] The page functions without client-side JavaScript.
- [ ] Wrong-password and nonexistent-account responses are indistinguishable in body, status, and headers.
- [ ] CSRF protection is present and tested.
- [ ] The page cannot be framed, verified by an integration test on the response headers.
- [ ] The page meets WCAG 2.1 AA for the login form (`UI-UX/13`).
- [ ] The full flow completes with keyboard only.

**Abuse cases to test**
- Username enumeration through error text, status code, or response timing (`SECURITY/02` §12).
- Clickjacking the login form (`SECURITY/02` §6 area).
- CSRF against the login POST (`SECURITY/02` §5).
- XSS through the `error` or `state` parameter reflected onto the page (`SECURITY/02` §6).

---

## P1-13 — Login Rate Limiting and Account Lockout

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-12 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Rate Limiting & Brute-Force Protection, `PLAN/09-SECURITY.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `SECURITY/02` §10, §13 |
| **Spec required** | Yes — security control |
| **Surface** | backend |

**Goal** — A brute-force attempt is demonstrably blocked, which is an explicit Phase 1 acceptance criterion — while a legitimate user who mistypes their password twice is not locked out of their job.

**Steps**
1. Implement per-account limiting with a **cooldown, not a permanent lockout** — `PLAN/05` says this explicitly, because permanent lockout converts a brute-force attempt into a denial-of-service against the victim.
2. Implement per-IP limiting for credential stuffing, with a different threshold, since one IP legitimately serves many users behind NAT.
3. Use progressive delay: the first few failures are free, then an increasing cooldown.
4. Store counters in Redis with automatic expiry.
5. Decide and document behavior when Redis is unavailable. Failing open means no rate limiting; failing closed means no logins at all. `PLAN/13`'s fail-safe principle applies to authorization decisions specifically — this is a different trade-off and needs its own reasoned ADR.
6. Ensure the limiter cannot be bypassed by rotating a header: only a proxy-verified client IP is trusted, never a raw `X-Forwarded-For` (`SECURITY/02` §10 Rate-Limit Bypass).
7. Emit `X-RateLimit-*` headers where appropriate (`PLAN/05`), but not in a way that helps an attacker calibrate their pacing on the login endpoint.
8. Audit lockout events and emit a metric, feeding `PLAN/13`'s "spike in failed logins" alert.
9. Reset counters on successful authentication.

**Definition of Done**
- [ ] A simulated brute-force attempt is demonstrably blocked — the literal Phase 1 acceptance criterion in `PLAN/17`.
- [ ] Lockout is temporary and self-clearing; no admin action is needed to restore a legitimate user.
- [ ] Rotating `X-Forwarded-For` does not reset the limit.
- [ ] The Redis-unavailable decision is recorded in `MEMORY/DECISIONS.md`.
- [ ] Lockouts appear in the audit log and in metrics.

**Abuse cases to test**
- Distributed credential stuffing across many IPs against many accounts (`SECURITY/02` §13).
- Rate-limit bypass by header manipulation, casing variations of the username, or alternating between username and email for the same account.
- Denial of service against a specific user by deliberately triggering their lockout.

---

## P1-14 — Authentication Audit Events

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-12, P1-12 |
| **Plan refs** | `PLAN/09-SECURITY.md` § Audit, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `PLAN/13-OBSERVABILITY.md` |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — Login success and failure appear in the audit log with correct actor and timestamp, which `PLAN/17` names as a Phase 1 acceptance criterion.

**Steps**
1. Emit `user.login.success` and `user.login.failed` through `P0-12`'s writer, with IP, user agent, and the authentication method used.
2. For a failed login against a nonexistent account, record the attempt without asserting a user id — and ensure the audit log itself does not become an enumeration oracle for anyone who can read it.
3. Emit `session.created`, `session.revoked`, `token.issued` (aggregate metric rather than a per-token row, to keep volume sane), and `user.lockout`.
4. Never include the password, the token, or the code in any payload.
5. Verify ordering and timestamp accuracy — an audit log with unreliable ordering cannot support an incident investigation (`SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md`).

**Definition of Done**
- [ ] Successful and failed logins both appear with correct actor and timestamp.
- [ ] No credential material appears in any event payload, verified by test.
- [ ] Events are queryable by org, actor, type, and time range with acceptable performance.
- [ ] `PLAN/17`'s Phase 1 audit criterion is demonstrably met.

---

## P1-15 — Management API Foundation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-16, P1-07 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` Part B, `PLAN/02-REQUIREMENTS.md` FR-13/FR-14, `PLAN/08-AUTHORIZATION.md` Part C § manager_roles |
| **Spec required** | Yes — authorization surface |
| **Surface** | backend |

**Goal** — The cross-cutting mechanics every Management API endpoint depends on, built once: bearer authentication, permission checks, error format, pagination, and idempotency.

**Steps**
1. Bearer token authentication middleware: validate signature, expiry, issuer, and audience against the local JWKS with no network round-trip.
2. Permission middleware reading `manager_roles` (`PLAN/04`, `PLAN/08` Part C). Phase 1 needs `INSTANCE_OWNER`, `ORG_OWNER`, and `ORG_ADMIN`; project-scoped roles arrive in Phase 2.
3. **Every check happens server-side on every request** — `CLAUDE.md`'s non-negotiable constraint. The console's UI-level hiding is a UX affordance, never a control.
4. Enforce tenant scoping by setting the RLS context from `P0-08` for the caller's organization on every request.
5. Implement `PLAN/05`'s exact error envelope: `{"error": {"code", "message", "details": [{"field", "issue"}]}}`, with a consistent mapping from error class to HTTP status.
6. Implement opaque cursor pagination returning `next_page_token` (`PLAN/05`'s example), with a bounded default and maximum `page_size`.
7. Implement `Idempotency-Key` support on `POST` endpoints (`PLAN/05`), storing the result keyed by (client, key) so a retried provisioning call is safe.
8. Rate-limit per `client_id`/API key rather than only per IP (`PLAN/05`), with `X-RateLimit-*` headers.
9. Audit every mutating request through `P0-12`.
10. Register every endpoint in the OpenAPI spec as it is built — `P0-16`'s CI check makes drift a build failure rather than a discovery.

**Definition of Done**
- [ ] A request with no token, an expired token, or a wrong-audience token is rejected with the correct status.
- [ ] A caller lacking the required manager role is rejected regardless of what the console would have shown them.
- [ ] Pagination is consistent across every list endpoint and stable under concurrent inserts.
- [ ] Replaying a `POST` with the same `Idempotency-Key` returns the original result without creating a duplicate.
- [ ] Every error response matches `PLAN/05`'s schema.
- [ ] Every mutating call writes an audit event.

**Abuse cases to test**
- Privilege escalation via the API by a caller whose token lacks the role (`SECURITY/02` §3).
- IDOR: accessing another organization's resource by id (`SECURITY/02` §2, §14).
- Mass assignment: a request body setting `org_id`, `id`, or a role field it should not control (`SECURITY/02` §11 Business-Logic Abuse).
- Rate-limit bypass across rotated client credentials (`SECURITY/02` §10).

---

## P1-16 — Management API: Organizations

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15 |
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Endpoint Structure, `PLAN/04-DATA-MODEL.md` § `organizations`, `UI-UX/08-PAGE-SPECIFICATIONS.md` |
| **Spec required** | Yes — data model surface |
| **Surface** | backend |

**Goal** — CRUD for organizations, restricted to instance-level administrators, ready for Phase 2's multi-org activation without a redesign.

**Steps**
1. Implement `GET/POST /v1/organizations` and `GET/PATCH/DELETE /v1/organizations/{org_id}`.
2. Restrict create, delete, and suspend to `INSTANCE_OWNER` (`PLAN/08` Part C hierarchy).
3. Validate `settings` against a schema — `password_policy`, `mfa_required`, `session_lifetime_hours`, `allowed_login_methods` (`PLAN/08` Part B). Reject unknown keys rather than storing them silently.
4. Prefer soft-delete or suspension over hard delete: deleting an organization cascades to users, sessions, and audit history, and audit history must survive.
5. Support `domain` for later domain-based tenant resolution (`PLAN/08` Part B), with verification itself deferred to a later phase.
6. Require an extra confirmation step for destructive operations (`PLAN/08` § Least Privilege) — the typed-confirmation pattern in `UI-UX/07-COMPONENT-SPECIFICATION.md`.

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
| **Plan refs** | `PLAN/05-API-CONTRACT.md`, `PLAN/04-DATA-MODEL.md` § `projects`, `UI-UX/08-PAGE-SPECIFICATIONS.md` (Project list) |
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
| **Plan refs** | `PLAN/05-API-CONTRACT.md`, `PLAN/04-DATA-MODEL.md` § `applications`, `UI-UX/08-PAGE-SPECIFICATIONS.md` (Applications tab) |
| **Spec required** | Yes — credential handling |
| **Surface** | backend |

**Goal** — Register and manage OIDC clients through the API, exposing `P1-05`'s logic with the show-secret-once rule intact.

**Steps**
1. Implement `GET/POST /v1/organizations/{org_id}/projects/{project_id}/applications` and the item-level operations.
2. Return the plaintext client secret **only** in the `201` response body, never on any subsequent read (`UI-UX/08`).
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
| **Plan refs** | `PLAN/05-API-CONTRACT.md` § Example: Create a User, `PLAN/04-DATA-MODEL.md` § `users`, `UI-UX/04-USER-FLOWS.md` Flow 1, `UI-UX/08-PAGE-SPECIFICATIONS.md` |
| **Spec required** | Yes — identity data |
| **Surface** | backend |

**Goal** — The user lifecycle: invite, list, read, update, deactivate — matching `PLAN/05`'s worked example exactly, since the console and the docs quickstart are both built against it.

**Sub-tasks**
- **P1-19.1** — `POST /v1/organizations/{org_id}/users`: create with `send_invite_email`, returning `status: "invited"` per `PLAN/05`'s example.
- **P1-19.2** — `GET .../users` with pagination and search, and `GET .../users/{user_id}`.
- **P1-19.3** — `PATCH .../users/{user_id}` for profile updates; `POST .../users/{user_id}:deactivate`. Prefer deactivation to deletion so audit history stays coherent.
- **P1-19.4** — Password reset flow: single-use, short-lived, hashed token; the reset link never appears in a log; the response is identical whether or not the email exists (`SECURITY/02` §12).
- **P1-19.5** — Invitation acceptance flow: set the initial password under `P1-02`'s policy, then transition `invited` → `active`.

**Steps**
1. Enforce per-organization email uniqueness (`P0-07`'s composite index), returning a clear conflict error.
2. Never accept a password hash from the client, and never return one.
3. Make invite and reset tokens single-use with a short expiry, stored hashed in `user_tokens` with the appropriate `purpose` (`PLAN/04`), and invalidated once used.
4. Rate-limit invite sending and password-reset requests — both are email-amplification vectors (`SECURITY/02` §10).
5. Deactivation must immediately terminate the user's sessions and revoke their refresh tokens; a deactivated user who can still act is not deactivated.
6. Audit every user lifecycle event.

**Definition of Done**
- [ ] The create response matches `PLAN/05`'s documented example field-for-field.
- [ ] Reset and invite tokens are single-use, expiring, and stored hashed.
- [ ] Requesting a reset for a nonexistent email is indistinguishable from a real one.
- [ ] Deactivation kills sessions and refresh tokens within one request cycle, verified end-to-end.
- [ ] Every user lifecycle event is audited.

**Abuse cases to test**
- User enumeration through create conflicts, reset responses, or timing (`SECURITY/02` §12).
- Invite-email flooding of a third-party address (`SECURITY/02` §10).
- A reset token issued for user A being used to set user B's password (`SECURITY/02` §11).
- Privilege escalation by self-updating a role or status field (`SECURITY/02` §3).

---

## P1-20 — Management API: Audit Log Read

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-15, P0-12 |
| **Plan refs** | `PLAN/04-DATA-MODEL.md` § `events`, `UI-UX/08-PAGE-SPECIFICATIONS.md` (Audit Log), `SECURITY/02` §19 |
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
- [ ] A large-table query stays within `PLAN/12`'s Management API latency budget.

---

## P1-21 — Console: OIDC Login (Dogfooding)

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P0-17, P1-07 |
| **Plan refs** | `PLAN/06-FRONTEND-ARCHITECTURE.md` § Why the Console Must Log In Through the Same OIDC Flow, `PLAN/02-REQUIREMENTS.md` § Constraints |
| **Spec required** | Yes — authentication surface |
| **Surface** | console |

**Goal** — The console authenticates as an ordinary `type: spa` OIDC client with Authorization Code + PKCE — the dogfooding constraint from `PLAN/02` and `PLAN/06`.

**Steps**
1. Register the console as a normal application record, with no client secret.
2. Implement Authorization Code + PKCE in the browser: generate the verifier with a CSPRNG, derive the S256 challenge, and validate `state` on return.
3. Decide token storage deliberately and record it as an ADR. In-memory with silent renewal via `prompt=none` resists XSS token theft far better than `localStorage` (`SECURITY/02` §6, §14) — but it requires the silent-renewal path from `P1-06` to be solid.
4. Implement silent renewal before access token expiry, with a clean fallback to interactive login.
5. Attach the access token to Management API calls through the generated client from `P0-16`.
6. Implement logout calling `P1-10` and clearing all local state.
7. Read role claims from the token to drive UI affordances — while treating the API as the only real enforcement point (`UI-UX/08` § Cross-Screen Requirements: routes must be genuinely unreachable, not merely hidden).
8. Handle the expired-session case gracefully: a mid-action expiry should not lose the user's work without explanation (`UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`).

**Definition of Done**
- [ ] Login works through the same OIDC flow as any other client, with no console-specific backdoor.
- [ ] PKCE parameters are generated with a CSPRNG and `state` is validated on return.
- [ ] The token-storage decision is recorded in `MEMORY/DECISIONS.md`.
- [ ] Silent renewal works, and its failure degrades to interactive login rather than a blank screen.
- [ ] A route the user's claims don't permit is unreachable by direct URL, not merely hidden.
- [ ] An E2E test covers login, renewal, and logout.

**Abuse cases to test**
- Token theft via XSS given the chosen storage strategy (`SECURITY/02` §6).
- Authorization code interception against the SPA redirect (`SECURITY/02` §1).
- Direct URL navigation to an unauthorized route (`SECURITY/02` §14 Client-Side Trust).

---

## P1-22 — Console: Organization Overview, Projects, Applications

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-21, P1-16, P1-17, P1-18 |
| **Plan refs** | `UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Organization Overview, `UI-UX/08-PAGE-SPECIFICATIONS.md`, `UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md` |
| **Spec required** | No — but the implementation chain is mandatory |
| **Surface** | console |

**Goal** — The three screens an admin lands on, each run through `UI-UX/19`'s full implementation chain so loading, error, empty, permission, responsive, and accessibility states are designed rather than improvised.

**Steps**
1. Run **every** screen and component through `UI-UX/19`'s twelve-step chain: Design → Component → State → Interaction → API Dependency → Loading → Error → Empty → Permission → Responsive → Accessibility → Test. "Not applicable" is an acceptable answer; silence is not.
2. Build the Organization Overview to `UI-UX/18`'s detailed spec, respecting its above-the-fold priorities.
3. Build the Project list per `UI-UX/08`, using the shared table anatomy — no screen invents its own table (`UI-UX/08` § Cross-Screen Requirements).
4. Build the Applications tab, including the create flow whose success modal shows the client secret **once**, with a copy action and an unmistakable warning that it will never be shown again.
5. Distinguish "genuinely empty" from "filtered to empty" in every list (`UI-UX/14`) — they need different copy and different recovery actions.
6. Use `color-danger` only for destructive actions (`UI-UX/06`, `CLAUDE.md`).
7. Ensure every screen is usable down to tablet width (`UI-UX/12`, `UI-UX/08` § Responsive Scope).

**Definition of Done**
- [ ] A completed implementation-chain table exists for every screen, committed alongside the code.
- [ ] Loading, error, empty, filtered-empty, and permission-denied states all render correctly and are covered by tests.
- [ ] The client secret modal shows the secret once and cannot retrieve it again.
- [ ] All three screens work at 1440px, 1024px, and 768px.
- [ ] Keyboard navigation and screen-reader labelling meet `UI-UX/13`.
- [ ] Every list uses the shared table pattern.

---

## P1-23 — Console: Users List and Detail

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-21, P1-19 |
| **Plan refs** | `UI-UX/18-DETAILED-PAGE-SPECIFICATIONS.md` § Users List, `UI-UX/04-USER-FLOWS.md` Flow 1, `UI-UX/08-PAGE-SPECIFICATIONS.md`, `UI-UX/15-FORM-UX.md` |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — The screens where an admin spends most of their time: find a user, invite one, deactivate one — implementing `UI-UX/04` Flow 1 exactly.

**Steps**
1. Build the Users list to `UI-UX/18`'s detailed spec: search, status filter, and the shared table anatomy.
2. Implement the invite flow as the side panel `UI-UX/08` specifies, following Flow 1 step by step.
3. Build the User detail Profile tab. The Grants, Sessions, and MFA tabs belong to Phases 2 and 3 — render them as clearly-labelled unavailable states rather than as broken or empty tabs, so the console never implies a capability that isn't shipped.
4. Deactivation uses a confirmation dialog and `color-danger`, and states plainly that it terminates the user's sessions immediately.
5. Use status badges (active, invited, deactivated) from the design system rather than ad-hoc styling (`UI-UX/05` § Badges/tags).
6. Show validation errors inline per `UI-UX/15`, driven by the `details[]` array from `PLAN/05`'s error format.
7. Run the full `UI-UX/19` chain for every component.

**Definition of Done**
- [ ] Flow 1 from `UI-UX/04` is implemented end-to-end and covered by an E2E test.
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
| **Plan refs** | `UI-UX/08-PAGE-SPECIFICATIONS.md` (Audit Log), `UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md` |
| **Spec required** | No |
| **Surface** | console |

**Goal** — A basic, readable audit log view — enough for `PLAN/17`'s Phase 1 criterion that logins appear with correct actor and timestamp. Filtering polish and export are deliberately Phase 5.

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
| **Plan refs** | `PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `UI-UX/20-PUBLIC-SITE-SPECIFICATIONS.md` § API Reference, `UI-UX/21-CONTENT-AND-COPY-STRATEGY.md`, `PLAN/17-ACCEPTANCE-CRITERIA.md` |
| **Spec required** | No |
| **Surface** | public-site, docs |

**Goal** — Replace `P0-19`'s placeholder with a quickstart that actually works, and publish the generated API reference for the endpoints that exist.

**Steps**
1. Write the quickstart as a real, followable path: register an application, configure redirect URIs, run the Authorization Code + PKCE flow, validate the token. Include working code for at least one language.
2. **Verify the quickstart by following it from scratch** against staging. A quickstart that has never been executed end-to-end is a hypothesis, and it is the first thing every evaluator tries.
3. Generate the API reference from `openapi/openapi.yaml` (`P0-16`). Never hand-write it (`CLAUDE.md` hard rule).
4. Publish concepts pages for the entities that now exist; leave roles, grants, and ABAC out until their phases ship (`UI-UX/21` governance rule).
5. Add the changelog entry for the MVP release.
6. Audit every published page against the shipped feature set — this is a Phase 1 acceptance criterion in `PLAN/17`, not a nicety.

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
| **Plan refs** | `PLAN/01-PRODUCT-SCOPE.md` § MVP Definition of Done, `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `PLAN/03-ARCHITECTURE.md` § Main Data Flow |
| **Spec required** | No |
| **Surface** | backend, infra |

**Goal** — Two independent applications that prove SSO works — the literal MVP definition of done in `PLAN/01`.

**Steps**
1. Build two minimal, genuinely separate applications with distinct `client_id`s, hostnames, and sessions. Two routes in one app do not demonstrate SSO.
2. Make one a confidential web client (backend code exchange) and the other a public SPA, so both client profiles are exercised.
3. Each validates tokens locally against the JWKS, with no call back to the auth service — this is `PLAN/12`'s biggest available latency win and should be demonstrated, not assumed.
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
| **Plan refs** | `PLAN/11-TESTING.md` (all), `PLAN/10-THREAT-MODEL.md` § High-Priority Abuse Scenarios, `SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md` |
| **Spec required** | No |
| **Surface** | backend, console |

**Goal** — Every layer of `PLAN/11`'s pyramid populated for Phase 1, and every abuse case named on a Phase 1 task actually implemented as a test.

**Steps**
1. **Unit**: password hashing, JWT creation and verification, PKCE verification, redirect URI matching, email and role-key validation, password policy evaluation.
2. **Integration**: the full flow `PLAN/11` specifies — create organization → create project → create application → create user → log in → verify token claims — against real Postgres and Redis.
3. **E2E (Playwright)**: successful login and redirect with a code; SSO between the two apps from `P1-26`; logout genuinely ending the session; the console's invite flow.
4. **Security**: every abuse case listed on every Phase 1 task, plus `PLAN/11` § Security Testing's explicit list — wrong `aud` rejected, non-exact `redirect_uri` rejected, rate limiting triggering under simulated brute force, RLS preventing cross-org leakage independent of application filtering.
5. Add fuzz tests for JWT parsing (`PLAN/11`).
6. Verify CI runs everything and that a deliberately-introduced regression in any abuse case fails the build.
7. Confirm the coverage floor for `internal/authn`, `internal/authz`, and `internal/oidc` from `P0-15`.

**Definition of Done**
- [ ] Every abuse case named in Phase 1 tasks has a passing automated test.
- [ ] All four pyramid layers have real Phase 1 coverage.
- [ ] The full suite runs in CI on every PR within an acceptable duration.
- [ ] A deliberately-reverted security control causes a red build, demonstrated once.
- [ ] SAST and dependency scans show no unaddressed critical or high findings (`PLAN/11` § Production-Ready).

---

## P1-28 — Phase 1 Acceptance Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P1-27 |
| **Plan refs** | `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1, `PLAN/01-PRODUCT-SCOPE.md` § MVP Definition of Done, `PLAN/12-PERFORMANCE.md` |
| **Spec required** | No |
| **Surface** | all |

**Goal** — Walk `PLAN/17`'s Phase 1 checklist deliberately, with evidence, rather than declaring the phase done because the tickets are closed.

**Steps**
1. Verify each of `PLAN/17`'s eight Phase 1 criteria individually, capturing evidence — a test name, a screenshot, a log excerpt, a metric.
2. Run a first load test against `/oauth/token` and `/oauth/authorize` and compare to `PLAN/12`'s targets. Missing a target is not automatically a blocker, but it must be a recorded, conscious decision rather than an unnoticed one.
3. Confirm API/console consistency: an entity created through one is visible and identical through the other (`PLAN/17`, FR-14).
4. Confirm the public site claims nothing unshipped.
5. Run a threat-model review before Phase 2 begins (`PLAN/09` § Secure Development Practices: threat modeling before each major new phase).
6. Write the phase summary in `MEMORY/`: what shipped, what deviated, what deferred, what to watch.
7. Update `PROGRESS.md` and tag the release.

**Definition of Done**
- [ ] Every `PLAN/17` Phase 1 criterion is verified with recorded evidence.
- [ ] Load test results are recorded against `PLAN/12`'s targets, with any gap explicitly accepted or scheduled.
- [ ] API and console consistency is demonstrated.
- [ ] The Phase 2 threat-model review is complete.
- [ ] A phase summary exists in `MEMORY/`.
- [ ] Anything deferred out of Phase 1 is in `BACKLOG.md`, not merely remembered.

---

## Phase 1 Exit Checklist

Directly from `PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 1:

- [ ] Two independent internal applications authenticate real users through this service.
- [ ] A user logged into Application A opens Application B and is not prompted to log in again.
- [ ] `POST /oauth/token` and `GET /oauth/authorize` conform exactly to `PLAN/05-API-CONTRACT.md`.
- [ ] Organizations, projects, applications, and users are creatable via both the REST API and the console, and the two stay consistent.
- [ ] Failed and successful logins appear in the audit log with correct actor and timestamp.
- [ ] Login rate limiting demonstrably blocks a simulated brute-force attempt.
- [ ] All Phase 1 items in `PLAN/11-TESTING.md`'s pyramid have passing automated tests in CI.
- [ ] The public landing page and docs quickstart exist and accurately reflect the real MVP flow.
