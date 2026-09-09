# Memory Index

Every change record, newest first. One line each: date, task ID, title, and the hook that tells you whether this is the record you need.

Add a line here as part of writing the record — an unindexed record is a record nobody finds.

---

## Records

| Date | Task | Record | Hook |
|---|---|---|---|
| 2026-09-08 | P0-11 | [Metrics, tracing, and alerting](./records/2026-09-08-P0-11-metrics-and-tracing.md) | 16 instruments on a separate internal listener, 15 promtool-validated alerts. Closes both P0-12 follow-ups. The principle worth keeping: absence is not health |
| 2026-09-08 | P0-11 | [Metrics endpoint authentication](./records/2026-09-08-P0-11-metrics-endpoint-authentication.md) | A bearer token in front of the metrics endpoint, and three deployment failures: a guard catching its own compose file, `chmod 400` on a directory the service cannot traverse, and a startup ordering that reported a closed database instead of an unreadable secret |
| 2026-09-08 | P0-16 | [The API contract becomes executable](./records/2026-09-08-P0-16-api-contract.md) | Spec-first, generating Go server interfaces so a contract mismatch is a compile error rather than a CI message. Found a probe vocabulary inconsistency by writing the spec against the running code, and a shell function shadowing `head` that had been discarding two failure paths' diagnostic output |
| 2026-09-08 | P0-17 | [The console shell](./records/2026-09-08-P0-17-console-skeleton.md) | Tokens, routing, navigation and accessibility, with the token discipline enforced by lint. Every type token was defined, named correctly, and generated no CSS at all — the tests passed because they read the source file rather than the compiler's output |
| 2026-09-08 | P0-18 | [The public site](./records/2026-09-08-P0-18-public-site-skeleton.md) | Docs versioning, local search, and an API reference generated from the same spec the backend and console are generated from. `PLAN/20` and `UI-UX/20` wanted different things; ADR-014 records which won. A dark mode that would have shipped unreadable, caught by computing contrast rather than looking |
| 2026-09-08 | P0-19 | [Site content and the capability audit](./records/2026-09-08-P0-19-site-content.md) | The DoD asked for an audit confirming no page claims an unshipped capability. A read confirms it today; the rule has to hold in six months — so it is a document plus two checks, one of which scans for verbatim phrases from the documents that must never be published |
| 2026-09-08 | P0-15 | [The test harness](./records/2026-09-08-P0-15-test-harness.md) | The integration suite reported success without running — it skipped every database test when none was reachable. Containers started by the tests themselves, all four pyramid layers with real tests, and a harness bug that broke the audit log's append-only guarantee, caught by a test the skip had been hiding |
| 2026-09-08 | P1-01 | [Argon2id password hashing](./records/2026-09-08-P1-01-password-hashing.md) | Parameters measured on the VM rather than copied — 90ms/hash, bounded by concurrency rather than latency. The not-found path does real work so response time cannot say which addresses have accounts, verified by short-circuiting it. First package the coverage floors applied to |
| 2026-09-09 | P1-03 | [Signing keys, JWKS and rotation](./records/2026-09-09-P1-03-signing-keys.md) | A four-state key lifecycle whose overlap window is the whole design. Found that 16 distinct token strings decode to the same signature — a trap set for P1-07's reuse detection — and that a runbook is a hypothesis until it is executed |
| 2026-09-09 | P1-06 | [The authorization endpoint](./records/2026-09-09-P1-06-authorize.md) | Two-phase validation, because an error reported before `redirect_uri` is validated is an open redirect delivered by the code meant to prevent one. Atomic code redemption proven with 32 racers — `GET`-then-`DEL` lets all of them succeed while the sequential test stays green. One documented exception to ADR-013 |
| 2026-09-09 | P1-11 | [Session management and the SSO cookie](./records/2026-09-09-P1-11-session-management.md) | `PG-14`: the cookie was specified to carry the primary key, which would have made the sessions API a credential-disclosure endpoint. Revocation is immediate because the cache is invalidated after the commit and a tombstone closes the repopulate race. Instance scope turns out to read no sessions at all, which is RLS working correctly |
| 2026-09-09 | P1-05 | [OIDC client registration and credentials](./records/2026-09-09-P1-05-application-registration.md) | Redirect matching is exact and every rule lives at registration, because validation belongs where a human is and matching belongs where an attacker is. ADR-016 chooses SHA-256 over Argon2id for client secrets and says what the choice depends on. Found that `fmt.Stringer` does not redact under `%d` |
| 2026-09-09 | P1-02 | [Password policy and breached-password rejection](./records/2026-09-09-P1-02-password-policy.md) | Policy values read from the database from day one, with a floor no administrator can configure below. ADR-015 decides fail-open and pays for it with an audit event, a metric and an alert. Found a parser whose success condition was satisfied by a failure — an error page read as a clean answer |
| 2026-09-09 | P1-04 | [Discovery document and JWKS endpoint](./records/2026-09-09-P1-04-discovery-jwks.md) | The document is derived from what the router actually serves, so advertising an unbuilt endpoint is not expressible rather than merely discouraged. Two DoD items left deliberately unticked because P1-06/P1-07 are what make them true. `curl -I` reported a caching bug that did not exist — chi answers 405 to HEAD, so the tool was reading a 405's headers |
| 2026-09-08 | P0-12 | [Audit event writer](./records/2026-09-08-P0-12-audit-writer.md) | Events commit with the action that caused them. Kills the partition time bomb flagged two records ago. Six stdlib vulnerabilities found and fixed. A check script that was silently discarding uncommitted work |
| 2026-09-08 | P0-08 | [Row-level security](./records/2026-09-08-P0-08-row-level-security.md) | Cross-tenant isolation becomes a database guarantee: 11 policies, a storage API with no unscoped query path, a startup assertion refusing an RLS-bypassing role, and a CI gate. Opens DV-02 |
| 2026-09-08 | P0-14, P0-20 | [Secrets conventions and VM deployment](./records/2026-09-08-P0-14-P0-20-secrets-and-vm-deployment.md) | OQ-03 answered (self-managed VM). Secret-reference abstraction, rotation runbooks, full VM deploy. Found and fixed an env_file bug handing the service RLS-bypassing owner credentials. Opens DV-01: single VM misses PLAN/14's Multi-AZ requirement |
| 2026-09-08 | P0-01…P0-13 | [Phase 0 foundation, first eleven tasks](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md) | Repo goes from docs-only to a running, tested service: Go skeleton, local stack, full 14-table schema, CI. 11/21 of Phase 0 done |
| 2026-09-08 | — | [Phase F frontend track added](./records/2026-09-08-phase-f-frontend-track.md) | 53 frontend tasks; resolves the tension with PLAN/16's lockstep rule by gating each page on its backend task; found a 4-screen omission in UI-UX/08 |
| 2026-09-08 | — | [Plan gap remediation](./records/2026-09-08-plan-gap-remediation.md) | All 11 gaps and both contradictions closed by amending the plan; PLAN/04 grew 158 → 295 lines; 2 further gaps found while fixing them |
| 2026-09-08 | P0-21 | [TASKS and MEMORY scaffolding](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md) | Execution layer established; 124 tasks across 7 phases; 2 plan contradictions and 11 plan gaps found while writing it |

---

## By Phase

### Pre-Phase-0 — Planning and plan amendments
- [Plan gap remediation](./records/2026-09-08-plan-gap-remediation.md) — closes PG-01…PG-11 plus two found during the work
- [Phase F frontend track added](./records/2026-09-08-phase-f-frontend-track.md) — 53 tasks covering every component and page

### Phase 0 — Foundation
- `P0-01`…`P0-13` — [Foundation, first eleven tasks](./records/2026-09-08-P0-phase-0-foundation-first-eleven.md) — service skeleton, local stack, schema, CI
- `P0-11` — [Metrics, tracing, and alerting](./records/2026-09-08-P0-11-metrics-and-tracing.md) — measurement before the thing measured
- `P0-11` — [Metrics endpoint authentication](./records/2026-09-08-P0-11-metrics-endpoint-authentication.md) — the endpoint authenticates on its own behalf, not only by where it is bound
- `P0-16` — [The API contract becomes executable](./records/2026-09-08-P0-16-api-contract.md) — the spec generates the code, so drift is impossible rather than merely detectable
- `P0-17` — [The console shell](./records/2026-09-08-P0-17-console-skeleton.md) — a test that reads the input to a compiler cannot tell you what the compiler did
- `P0-18` — [The public site](./records/2026-09-08-P0-18-public-site-skeleton.md) — when two plan documents disagree, the deviation is a decision, not a silence
- `P0-19` — [Site content and the capability audit](./records/2026-09-08-P0-19-site-content.md) — a governance rule that is only ever read is a rule that erodes
- `P0-15` — [The test harness](./records/2026-09-08-P0-15-test-harness.md) — a green suite that skipped its tests is a false statement everyone acts on
- `P0-12` — [Audit event writer](./records/2026-09-08-P0-12-audit-writer.md) — append-only log, partition maintenance
- `P0-08` — [Row-level security](./records/2026-09-08-P0-08-row-level-security.md) — isolation enforced by the database
- `P0-14`, `P0-20` — [Secrets conventions and VM deployment](./records/2026-09-08-P0-14-P0-20-secrets-and-vm-deployment.md)
- `P0-21` — [TASKS and MEMORY scaffolding](./records/2026-09-08-P0-21-tasks-and-memory-scaffolding.md)

### Phase 1 — MVP: Core Auth + SSO
- `P1-01` — [Argon2id password hashing](./records/2026-09-08-P1-01-password-hashing.md) — a defence you have not tried to break is a defence you are guessing about
- `P1-03` — [Signing keys, JWKS and rotation](./records/2026-09-09-P1-03-signing-keys.md) — the overlap window is the design; a runbook is a hypothesis until executed
- `P1-06` — [The authorization endpoint](./records/2026-09-09-P1-06-authorize.md) — assert the absence of the header, not the presence of the status
- `P1-11` — [Session management and the SSO cookie](./records/2026-09-09-P1-11-session-management.md) — a cache defers revocation unless you deliberately make it not
- `P1-05` — [OIDC client registration and credentials](./records/2026-09-09-P1-05-application-registration.md) — put the cleverness at registration, where a rejection can be explained
- `P1-02` — [Password policy and breached-password rejection](./records/2026-09-09-P1-02-password-policy.md) — a check can be wrong about what success means, not only about whether it ran
- `P1-04` — [Discovery document and JWKS endpoint](./records/2026-09-09-P1-04-discovery-jwks.md) — a machine-readable document that claims an unshipped capability fails in someone else's logs

### Phase 2 — RBAC & Multi-Tenancy
_No records yet._

### Phase 3 — Advanced Security
_No records yet._

### Phase 4 — Enterprise Interoperability
_No records yet._

### Phase 4b — ABAC
_Conditional phase; not started._

### Phase 5 — Hardening
_No records yet._

---

## Phase Summaries

Written at each phase completion from `templates/PHASE-SUMMARY-TEMPLATE.md`.

| Phase | Summary | Completed |
|---|---|---|
| Phase 0 | — | — |
| Phase 1 | — | — |
| Phase 2 | — | — |
| Phase 3 | — | — |
| Phase 4 | — | — |
| Phase 4b | — | — |
| Phase 5 | — | — |
| Phase F (track) | — | — |

---

## Operational Events

Key rotations, DR drills, pentests, load tests, and production incidents — anything with lasting consequence that is not a code change.

| Date | Event | Record |
|---|---|---|
| 2026-09-08 | **Re-clone destroyed the signing key, the metrics token and every backup** — runtime state lived inside the code directory. Recovered; state moved out, so `git pull` is now safe | [record](./records/2026-09-08-clone-based-deploy-and-state-separation.md) |
| 2026-09-09 | **The nightly backup had been failing silently since 2026-09-08** — `P0-14` moved runtime state out of the checkout and the systemd unit's `ReadWritePaths` did not follow, so systemd refused to start the service before `backup.sh` ran a line. `systemctl list-timers` reported the timer healthy throughout, because the timer was. Paths fixed, a verified backup taken; `BL-01` opened for the freshness alert that would have caught it | [P1-02](./records/2026-09-09-P1-02-password-policy.md) |
| 2026-09-08 | **Backup restore verified** — staging dump restored into a throwaway database, 19 tables and every row count matching the source. Backups now run daily by timer | [P0-20](./records/2026-09-08-P0-20-backup-verification.md) |
