# Threat-model review before Phase 2

**Date**: 2026-09-11
**Task**: `P1-28` — Phase 1 acceptance validation
**Against**: `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`'s nineteen categories, each of which names a verification scenario and says whether it should be automated.

---

## Why this axis and not the other one

There is already a coverage map: the package comment in `backend/tests/security/isolation_test.go`, which tracks `docs/PLAN/11` § Security Testing's seven scenarios and was re-audited at `P1-27`.

That map answers "is every abuse case in the testing plan covered". It cannot answer "is every category in the threat model verified", because it was built from a different list. `docs/SECURITY/05` has nineteen rows; `docs/PLAN/11` has seven. The seven are a subset chosen for Phase 1, and the gap between the lists is exactly where an unwritten test hides — so this review walks the nineteen.

Each row below names the test, gate or deployment fact that verifies it **today**, or says plainly that nothing does.

---

## The nineteen

| # | Category | Verified by | Verdict |
|---|---|---|---|
| 1 | Authentication attacks | `login`: `TestASimulatedBruteForceIsBlocked`, `TestTheCooldownClearsWithoutAdministrativeAction`, `TestRotatingTheForwardedHeaderDoesNotResetTheLimit`, `TestAnUntrustedPeerCannotChooseItsOwnIdentity`; re-confirmed live on staging today (blocked after 7 attempts) | **Verified** |
| 2 | Authorization bypass / IDOR | `tests/security`: `TestCrossTenantReadsAreEmpty`, `TestUnfilteredQueryCannotSeeAnotherTenant`, `TestRuntimeRoleCannotBypassRLS`; plus per-resource integration tests in `organization`, `project`, `user`, `management` | **Verified** for every resource that exists |
| 3 | Privilege escalation / delegation abuse | Project Grants are Phase 4 and `manager_roles` has no API before Phase 2, so the delegation half has no surface yet. The half that exists — a token for another audience administering this one — is `management`: `TestATokenForAnotherAudienceCannotAdminister` | **Partly out of scope**, and the in-scope half is verified |
| 4 | Session attacks | Regeneration: `login`: `TestReauthenticationReplacesTheSessionItSupersedes` — sign-in always mints a new token and revokes the superseded one, so a planted cookie is never adopted. Flags: `session`: `TestCookieAttributes`, `TestClearCookieMatchesTheSetCookie`. Fixation has no classic form here: there is no session before authentication, only a signed `request` parameter | **Verified** |
| 5 | CSRF | `login`: `TestCSRFTokensAreUnguessable`, `TestCSRFComparison`, `TestCSRFCookieAttributes`, `TestCSRFCookieHasNoInsecureMode`, `TestSubmissionRequiresACSRFToken` | **Verified** |
| 6 | XSS | `login`: `TestRenderedValuesAreEscaped`; `gosec` in CI; the console renders through React with no `dangerouslySetInnerHTML` anywhere in the tree | **Verified** for the surfaces that render user input today |
| 7 | SSRF | One outbound call exists: `authn/breach.go`, to a fixed endpoint with a **five-hex-character hash prefix** appended. Nothing user-supplied reaches a URL. SAML metadata fetching — the scenario the plan names — is Phase 5 | **No surface yet**, and the one call is bounded by construction |
| 8 | SQL/NoSQL injection | Every query in the tree is parameterised through `pgx`; `gosec` flags string-built SQL; the tenant is set with `WithTenant` rather than interpolated | **Verified** |
| 9 | File upload abuse | No upload endpoint exists. Branding is a settings row, not a file | **No surface yet** |
| 10 | API abuse / rate-limit bypass | `ratelimit`: quota tests; management quota 600/min per client; the header-rotation and untrusted-peer tests above; and today's load test, which is the first time the plan's "under simulated load" half was actually run | **Verified** |
| 11 | Business-logic abuse | `management`: `TestOnlyOneOfManyConcurrentDuplicatesClaimsTheKey` (the insert is the lock, not a check-then-insert); authorization codes are redeemed with `GETDEL` so a race cannot spend one twice | **Verified** for the races identified so far — this row is "partially automatable" by the plan's own account and stays a review item |
| 12 | Enumeration | `login`: `TestAWrongPasswordAndAnUnknownAccountAreByteIdentical`, `TestTheUnknownAddressPathCostsTheSame`, `TestAnAddressWithNoAccountIsLimitedIdentically`; `authn`: `TestNonexistentUserCostsTheSameAsARealOne`; `httpserver`: an unregistered origin gets the same status as a registered one, so preflight cannot enumerate either | **Verified** |
| 13 | Credential stuffing / token leakage | CI refuses a raw header, body or query string reaching a log call; `login`: `TestNoPasswordReachesTheAuditLog`; the lockout event names neither the account nor the address, asserted live today | **Verified** |
| 14 | IDOR via client-side trust | `demo/` is the reference implementation and verifies tokens itself rather than trusting anything the browser sends — `demo/internal/verify`, whose `TestATokenMintedForTheOtherApplicationIsRefused` is the whole point. The console treats an absent `roles` claim as *unknown* and defers to the API (`P1-27`) | **Verified** |
| 15 | Supply-chain / dependency risks | `govulncheck` on every build; every GitHub Action pinned to a commit SHA; the demo module has no dependencies at all and a gate that fails if it gains one. **No SBOM is produced or diffed** | **Gap — see below** |
| 16 | Secret exposure | `gitleaks` in CI; a pre-commit hook; `check.sh` refuses committed PEM blocks and credential-shaped tokens; the service container is given no owner credentials, and that gate was itself repaired today | **Verified** |
| 17 | Container / runtime security | `distroless/static:nonroot`, `USER nonroot`, and CI asserts the built image is not root; on staging every service runs `no-new-privileges`, `cap_drop: ALL`, and the service `read_only`. **No image vulnerability scan** | **Gap — see below** |
| 18 | CI/CD attack surface | Reviewed today: workflow-level `permissions: contents: read`, raised only to `security-events: write` for the scanning job; `concurrency` cancels superseded runs; every action SHA-pinned; no `pull_request_target`; no secret reaches a fork-triggered run | **Verified for this quarter** |
| 19 | Logging / audit integrity | `tests/security`: `TestAuditLogCannotBeAltered`; re-confirmed live on staging today — `UPDATE` on `events` as `auth_app` is refused by the database | **Verified** |

---

## The two gaps, stated plainly

**No SBOM (§15).** The plan asks for "SBOM diff review on every release". `govulncheck` answers "does anything we import have a known CVE", which is the more useful question day to day, but it is not the same question: an SBOM diff catches a dependency *appearing*, including one added by a transitive bump that no CVE has been filed against yet.

**No image scan (§17).** The plan asks for "automated container image scanning". CI proves the image is non-root and the base is `distroless/static`, which has almost nothing in it to scan — that is a real mitigation and not a substitute for the check.

Neither is a Phase 1 acceptance criterion. `docs/PLAN/17`'s Phase 1 list does not mention them and `docs/SECURITY/05`'s cadence puts the SBOM at "every release", which Phase 1 is not. Both are recorded in the backlog as `PG-27` and `PG-28` rather than done quietly or forgotten quietly.

## What this review is not

It is a review of **categories against tests**, and a category with a test is not a category that is safe. Three of the nineteen rows say "no surface yet", and each of those will need its verification scenario written the moment the feature lands — file upload with branding, SSRF with SAML metadata, delegation abuse with Project Grants. The value of writing this down now is that the next person does not have to re-derive which rows were empty because the feature was missing versus empty because nobody looked.

The honest summary: **sixteen verified, three not yet applicable, two gaps inside verified rows**, and no category where something exists and nothing checks it.
