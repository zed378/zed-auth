# Progress Board

Single source of truth for where the project stands. Updated in the same commit as the work it describes (`00-TASK-CONVENTIONS.md` global DoD item 8).

**Last updated**: 2026-09-11
**Current phase**: Phase 1 — MVP Core Auth (28 / 29 done — `P1-29` was added mid-phase; see its card). Only `P1-28`, the acceptance walk, remains. Phase 0 is 20 / 21; `P0-20` stays WIP pending the pull-based deployment re-read
**Overall**: 42 / 178 tasks done

Status values: `TODO` · `BLOCKED` · `SPEC` · `WIP` · `REVIEW` · `DONE` · `DROPPED`
Sizes: `S` under half a day · `M` one to two days · `L` several days · `XL` must be split

---

## Phase Summary

| Phase | Tasks | Done | Status | Gate to enter |
|---|---|---|---|---|
| [Phase 0 — Foundation](./PHASE-0-FOUNDATION.md) | 21 | 20 | **ACTIVE** — `P0-20` only | — |
| [Phase 1 — MVP: Core Auth + SSO](./PHASE-1-MVP-CORE-AUTH-SSO.md) | 29 | 29 | **COMPLETE** — [summary](../MEMORY/records/2026-09-12-P1-phase-1-summary.md), tagged `v0.1.0-phase1` | Phase 0 exit checklist |
| [Phase 2 — RBAC & Multi-Tenancy](./PHASE-2-RBAC-MULTITENANCY.md) | 17 | 9 | **ACTIVE** | Phase 1 exit + `P1-28` — **met** 2026-09-12 |
| [Phase 3 — Advanced Security](./PHASE-3-ADVANCED-SECURITY.md) | 15 | 0 | Not started | Phase 2 exit + threat model review |
| [Phase 4 — Enterprise Interop](./PHASE-4-ENTERPRISE-INTEROP.md) | 16 | 0 | Not started | Phase 3 exit + threat model review |
| [Phase 4b — ABAC](./PHASE-4B-ABAC.md) | 11 | 0 | **CONDITIONAL** | A concrete requirement RBAC cannot express (`P4B-00`) |
| [Phase 5 — Hardening](./PHASE-5-HARDENING.md) | 16 | 0 | Not started | Phase 4 exit; 4b done or declined |
| [Phase F — Frontend Implementation](./PHASE-F-FRONTEND-IMPLEMENTATION.md) | 53 | 0 | **TRACK** — runs alongside | Foundation: `P0-17`. Pages: each carries its own gate |

> Phase 3 and Phase 4 may be swapped **as whole phases** if business need demands it (`docs/PLAN/16`'s note). They are never interleaved task by task.
>
> Phase F is a **track, not a sequence position**. Its foundation tasks (`PF-01`–`PF-20`, `PF-48`–`PF-52`) are phase-independent and should start as soon as `P0-17` lands. Its page tasks (`PF-21`–`PF-47`) each carry a binding gate, which is how `docs/PLAN/16`'s lockstep rule is enforced per screen rather than per phase.

---

## Phase 0 — Foundation

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P0-01 | Confirm and freeze the tech stack (ADRs) | S | **DONE** | — |
| P0-02 | Initialize repository structure | S | **DONE** | P0-01 |
| P0-03 | Git conventions, branch strategy, PR template | S | **DONE** | P0-02 |
| P0-04 | Go service skeleton | M | **DONE** | P0-02 |
| P0-05 | Local environment via Docker Compose | M | **DONE** | P0-04 |
| P0-06 | Migration tooling and baseline migration | S | **DONE** | P0-05 |
| P0-07 | Core schema implementation | L | **DONE** | P0-06 |
| P0-08 | Row-level security scaffolding | M | **DONE** | P0-07 |
| P0-09 | Structured logging with redaction | M | **DONE** | P0-04 |
| P0-10 | Health and readiness endpoints | S | **DONE** | P0-04 |
| P0-11 | Metrics and tracing baseline | M | **DONE** | P0-09 |
| P0-12 | Audit event writer | M | **DONE** | P0-07, P0-09 |
| P0-13 | CI pipeline | M | **DONE** | P0-04 |
| P0-14 | Secrets and configuration conventions | S | **DONE** | P0-04 |
| P0-15 | Test harness | M | **DONE** | P0-05, P0-16 |
| P0-16 | OpenAPI spec skeleton and client generation | M | **DONE** — one spec generates the backend's server interface, the console's client and the public API reference | P0-13 |
| P0-17 | Console skeleton with design tokens | L | **DONE** | P0-02, P0-16 |
| P0-18 | Public site skeleton | L | **DONE** | P0-02 |
| P0-19 | Landing, About, docs skeleton content | M | **DONE** | P0-18 |
| P0-20 | Staging environment provisioning | L | **WIP** — TLS-only staging live, restore verified and automated. `OQ-11` answered: no GitHub Actions (no public IP), so deployment is pull-based and the "merge deploys automatically" DoD item needs re-reading rather than satisfying. `OQ-12` answered: local backups accepted, S3/NFS later | P0-13 |
| P0-21 | Adopt the TASKS/MEMORY working discipline | S | **DONE** | — |

**Suggested parallel tracks** once `P0-02` lands: backend (`P0-04` → `P0-05` → `P0-06` → `P0-07` → `P0-08`), platform (`P0-13` → `P0-14` → `P0-20`), frontend (`P0-17`), and public site (`P0-18` → `P0-19`). `P0-16` gates both `P0-15` and `P0-17`, so it should not wait.

---

## Phase 1 — MVP: Core Auth + Basic SSO

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P1-01 | Argon2id password hashing | M | **DONE** | P0-15 |
| P1-02 | Password policy and breached-password rejection | M | **DONE** — ADR-015 (fail open, loudly); `PG-13` found and closed by `users.password_changed_at` | P1-01 |
| P1-03 | Signing key management, JWKS, rotation | L | **DONE** | P0-14 |
| P1-04 | Discovery document and JWKS endpoint | S | **DONE** — complete. Its two deferred DoD items were ticked by `P1-07`, which is when they became true | P1-03 |
| P1-05 | Application registration and credentials | M | **DONE** — ADR-016 (SHA-256 for client secrets); redirect matching is exact, with a 15-row rejection table naming each attack | P0-07 |
| P1-06 | `GET /oauth/authorize` — code + PKCE | L | **DONE** — one item deferred to `P1-27`'s load test. Two-phase validation so a phase-1 error can never redirect; atomic code redemption proven with 32 concurrent racers | P1-05, P1-11 |
| P1-07 | `POST /oauth/token` | L | **DONE** — one item deferred to `P1-27`'s load test. The flow closes: a consumer can complete a login, and `P1-04`'s last two DoD items are now true | P1-06, P1-03 |
| P1-08 | `GET /oauth/userinfo` | S | **DONE** — the claims the scopes authorise and nothing else, with the exact key set asserted rather than a subset. Revoking the session invalidates the token immediately, which costs nothing because the liveness check rides in the user read. `signing.Verifier` finally has a caller | P1-07 |
| P1-09 | `/oauth/introspect` and `/oauth/revoke` | M | **DONE** — one item deferred to `PG-19`. A client may only see or destroy its own tokens, and another client's is answered as unknown rather than forbidden. Revoking a refresh token takes its whole family; an access token cannot be revoked and says so | P1-07 |
| P1-10 | `GET /oidc/logout` | M | **DONE** — a GET logs somebody out only when the request proves it came from a party holding a token for THAT session; everything else is a confirmation click. Clearing the cookie is done as well as revoking the row, never instead. `PG-20` found: the plan treats RP-initiated and back-channel logout as one specification | P1-11 |
| P1-11 | Session management and SSO cookie | L | **DONE** — `PG-14` closed (the cookie is no longer the primary key); revocation is immediate, with the cache repopulate race closed by a tombstone | P0-07 |
| P1-12 | Hosted login page | M | **DONE** — a human can now log in. A wrong password and an unknown address are byte-identical responses, proved by submitting the same address with the account deleted in between. `PG-16` closed: branding was specified, designed and half-built with nowhere to be read from | P1-01, P1-11 |
| P1-13 | Login rate limiting and lockout | M | **DONE** — ADR-017 (fail open, loudly). Counters are keyed on the SUBMITTED ADDRESS, not a resolved user, so the refusal can say something true without confirming an account exists. Found dead code in `Evaluate` that read like the allowance check and decided nothing | P1-12 |
| P1-14 | Authentication audit events | S | **DONE** — mostly verification, and it earned its keep: `P1-13`'s lockout event was never actually written, because a DoD item about the audit log had been ticked on a fake auditor. Now asserted against a real database, with `EXPLAIN` on the three documented access patterns | P0-12, P1-12 |
| P1-15 | Management API foundation | L | DONE | P0-16, P1-07 |
| P1-16 | Management API — organizations | M | DONE | P1-15 |
| P1-17 | Management API — projects | M | DONE | P1-15 |
| P1-18 | Management API — applications | M | DONE | P1-15, P1-05 |
| P1-19 | Management API — users | L | DONE | P1-15, P1-01 |
| P1-20 | Management API — audit log read | M | DONE | P1-15, P0-12 |
| P1-21 | Console — OIDC login (dogfooding) | L | DONE | P0-17, P1-07 |
| P1-22 | Console — Org overview, Projects, Applications | L | DONE | P1-21, P1-16..18 |
| P1-23 | Console — Users list and detail | L | DONE | P1-21, P1-19 |
| P1-24 | Console — Audit Log screen | M | DONE | P1-21, P1-20 |
| P1-25 | Public docs — quickstart and API reference | M | **DONE** — the quickstart was executed against staging, not proofread: 52 assertions, one failure, three factual errors corrected. The capability audit now runs in both directions and had been misreading this board since `P0-19` | P1-07, P1-19 |
| P1-26 | Two demo consumer applications | M | **DONE** — 33 assertions on staging, 0 failures: one login, two applications, and App B refuses App A's token. Two public hostnames outstanding (`DV-03`); the Playwright half of one DoD item is `P1-27`'s | P1-07 |
| P1-27 | Phase 1 test suite completion | L | **DONE** — the end-to-end environment found four production bugs in its first hour, each of which had passed every other layer. Twelve browser tests; six reverted security controls, six red builds; 944k fuzz executions | all above |
| P1-28 | Phase 1 acceptance validation | M | **DONE** — the eight criteria walked against the deployed service, 35 assertions, 0 failures. The first load test: every endpoint within target in isolation, `/oauth/token` over on p50 and p95 in the **mixed** workload — accepted, with the capacity curve recorded. Found a missing Client ID column, a suite that expired at noon, a gate that passed without reading its input, and a backup that cannot restore the signing key (`PG-29`) | P1-27 |
| P1-29 | Cross-origin access policy | M | **DONE** — added mid-phase. `PG-17` stopped being a forecast the first time a browser was pointed at the login flow: no public client could complete one, and the console could not call the API at all. Closed with a split policy, ADR-020 | P1-08, P1-15, P1-21 |

**Critical path**: `P1-11` → `P1-06` → `P1-07` → everything else. Session management and the token endpoint are the two tasks that block the most downstream work; start them first and give them the most review attention.

---

## Phase 2 — Full RBAC & Multi-Tenancy

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P2-01 | Role model and permission keys | M | **DONE** — the table has existed since `P0-07`; what it never had was any rule about what may go in it. Adds the permission-key format (no wildcards, deliberately a one-way door), an org/project agreement trigger, and built-in immutability in the database rather than only in the store. One definition of each pattern, generated into Go and TypeScript and gated against drift. Found `PG-30`: the two "built-in roles" `docs/PLAN/08` names are manager roles, a different table. **Not yet on staging** | P1-17 |
| P2-02 | Management API — roles | M | **DONE** — the first project-scoped routes in the API. Found that a `PROJECT_OWNER` refused on another project was told **403**, which confirms it exists, in the narrowest role in the system: visibility on a project-scoped route now follows grants rather than organization membership. A mutation test also caught a control with no test behind it — removing the handler's project check broke nothing, because the policy layer refuses the case the test used. **Not yet on staging** | P2-01 |
| P2-03 | User grants | L | **DONE** (one item is `P2-06`'s) — the row that actually grants access. Least privilege asserted as an **absence**, because that is what it is. A grant naming a role that does not exist is refused by a trigger that names the key. Phase 4's delegation slot is closed by a trigger whose body `P4-01` replaces, not a CHECK it would have to drop. Self-granting is refused in the handler, because no scope can express "not this one user". **Not yet on staging** | P2-01, P1-19 |
| P2-04 | Role claims in the access token | L | **DONE** — the claim shape is a one-way door, so it is asserted against `docs/PLAN/08`'s literal JSON. `org_id` inside each value is emitted now because it costs nothing today and is a breaking change later. Scoped to the requesting client's project, bounded at 64 and **truncating rather than dropping** ([ADR-021](../MEMORY/DECISIONS.md)): denying access that should have been granted is recoverable, the reverse is not. **Not yet on staging** | P2-03, P1-07 |
| P2-05 | Manager role enforcement, full hierarchy | L | **DONE** (two steps blocked) — taken **before** `P2-02`, which requires a `PROJECT_OWNER` that satisfied nothing. Adds the project scope, with an empty project id refused rather than treated as a wildcard. The hierarchy diagram needed a reading (`PG-32`): `ORG_ADMIN` and `PROJECT_OWNER` are drawn as siblings, which would make every project endpoint from `P1-17` wrong. An architecture test now reads the source for permission decisions made outside the one resolution function. Steps 6–7 blocked by `PG-31`: **nothing anywhere writes `manager_roles`** | P1-15 |
| P2-06 | `POST /v1/authz/check` — RBAC | L | **DONE** (latency unmeasured — needs the VM) — a revocation is honoured on the next check, proven with a token minted **before** it. A nonexistent subject and an unauthorized one answer byte-identically, because this endpoint is cheap, constant and deliberately unaudited — an excellent enumeration tool otherwise. A dependency failure answers **503** rather than claiming a decision was reached. Added `Member` (the table could only say "an administrator" or nothing) and `UNAVAILABLE`. **Not yet on staging** | P2-03 |
| P2-07 | Authorization decision caching | M | **DONE** — invalidation is the mechanism and the 30s TTL is the backstop, so the revocation window is stateable: immediate normally, at most 30s if an invalidation was lost ([ADR-022](../MEMORY/DECISIONS.md)). Inputs are cached, never decisions — a cached decision would be wrong the moment a policy reads `resource.attributes`. The fault-injection test found that an unreachable cache made the check **time out** rather than fall through: a cache outage was a service outage. **Not yet on staging** | P2-06 |
| P2-08 | Multi-organization activation | L | **DONE** (load unmeasured — needs the VM) — mostly verification, and it found a real hole: `applications` had no rule that its organization owns its project, so a client could be filed under the wrong tenant and then hidden by RLS from the organization that owns it. Writable that way since Phase 0. One trigger function now serves all four tables — and **raises** rather than silently passing on a table whose organization column it does not recognise, which the first version would have done to `project_grants`. An architecture test pins the six reads that legitimately run unscoped. **Not yet on staging** | P0-08, P1-16 |
| P2-09 | Tenant resolution strategy | M | **DONE** — writing the decision down revealed one had already been made: the tenant is resolved from the **OIDC client**, which is none of the four options `docs/PLAN/08` Part B lists (`PG-33`, [ADR-023](../MEMORY/DECISIONS.md)). Better than all four for one reason — there is nothing for a caller to supply and therefore nothing to forge. Step 4 is tested by reading the source, because "no header can change it" is a claim about every header. **Not yet on staging** | P2-08 |
| P2-10 | Per-organization policy enforcement | M | TODO | P2-08, P1-02 |
| P2-11 | Console — Roles tab | M | TODO | P2-02 |
| P2-12 | Console — Authorizations tab | L | TODO | P2-03 |
| P2-13 | Console — organization switcher | M | TODO | P2-08 |
| P2-14 | Console — Policies (Access) screen | M | TODO | P2-10 |
| P2-15 | Docs — RBAC and multi-tenancy guides | M | TODO | P2-06, P2-08 |
| P2-16 | Phase 2 test suite | L | TODO | all above |
| P2-17 | Phase 2 acceptance validation | M | TODO | P2-16 |

**Highest-risk task**: `P2-04`. The token claim format is the hardest thing in the project to change later, because every consumer application reads it.

---

## Phase 3 — Advanced Security

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P3-01 | MFA framework and step-up architecture | L | TODO | P1-11, P2-10 |
| P3-02 | TOTP enrollment | M | TODO | P3-01 |
| P3-03 | TOTP verification at login | M | TODO | P3-02 |
| P3-04 | Recovery codes and lost-device process | M | TODO | P3-02 |
| P3-05 | WebAuthn / passkey support | L | TODO | P3-01 |
| P3-06 | Refresh token rotation with reuse detection | L | TODO | P1-07 |
| P3-07 | Organization-mandated MFA enforcement | M | TODO | P3-03, P2-10 |
| P3-08 | Login anomaly detection | L | TODO | P1-11, P1-14 |
| P3-09 | Session management API | M | TODO | P1-11 |
| P3-10 | Console — MFA tab | M | TODO | P3-02, P3-05 |
| P3-11 | Console — Sessions tab | M | TODO | P3-09 |
| P3-12 | Console — personal account settings | L | TODO | P3-09, P3-10 |
| P3-13 | Docs — MFA and session security | M | TODO | P3-05, P3-06 |
| P3-14 | Phase 3 test suite | L | TODO | all above |
| P3-15 | Phase 3 acceptance validation | M | TODO | P3-14 |

---

## Phase 4 — Enterprise Interoperability

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P4-01 | Project Grants — data and lifecycle | L | TODO | P2-03, P2-05 |
| P4-02 | Delegated user grants with subset validation | L | TODO | P4-01 |
| P4-03 | `PROJECT_GRANT_OWNER` enforcement | M | TODO | P4-01, P2-05 |
| P4-04 | Delegated claims and revocation propagation | L | TODO | P4-02, P2-04 |
| P4-05 | Console — Project Grants tab | L | TODO | P4-01 |
| P4-06 | Console — Granted Projects list | L | TODO | P4-02 |
| P4-07 | SAML 2.0 IdP — core | L | TODO | P1-11 |
| P4-08 | SAML SP- and IdP-initiated flows | L | TODO | P4-07 |
| P4-09 | SAML application type and metadata | M | TODO | P4-07, P1-18 |
| P4-10 | Social login federation | L | TODO | P1-11, P1-19 |
| P4-11 | Account linking and reconciliation | L | TODO | P4-10 |
| P4-12 | Webhooks | L | TODO | P0-12 |
| P4-13 | SCIM provisioning (optional) | L | TODO | P1-19 |
| P4-14 | Docs — delegation, SAML, social login | M | TODO | P4-06, P4-09, P4-10 |
| P4-15 | Phase 4 test suite | L | TODO | all above |
| P4-16 | Phase 4 acceptance validation | M | TODO | P4-15 |

**Highest-risk task**: `P4-02`. Four separate documents state the same subset-validation rule independently, which is the plan's way of saying this is where a mistake becomes a breach.

---

## Phase 4b — ABAC (Conditional)

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P4B-00 | Justification gate | S | **BLOCKED** — no concrete requirement yet | P4-16 |
| P4B-01 | ABAC data model | M | TODO | P4B-00 |
| P4B-02 | Embedded OPA integration | L | TODO | P4B-01 |
| P4B-03 | Extended `/v1/authz/check` | L | TODO | P4B-02, P2-06 |
| P4B-04 | Policy lifecycle | L | TODO | P4B-02 |
| P4B-05 | Dry-run evaluation | L | TODO | P4B-04 |
| P4B-06 | User attribute management API | M | TODO | P4B-01 |
| P4B-07 | Console — Policies (ABAC) tab | L | TODO | P4B-04, P4B-05 |
| P4B-08 | Docs — ABAC concepts and authoring | M | TODO | P4B-07 |
| P4B-09 | Phase 4b test suite | L | TODO | all above |
| P4B-10 | Phase 4b acceptance validation | M | TODO | P4B-09 |

> This phase not running is a valid, plan-endorsed outcome. `docs/PLAN/08` Part D: build it only once a concrete need appears that RBAC genuinely cannot express.

---

## Phase 5 — Hardening & Production Scale

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P5-01 | Internal security verification sweep | L | TODO | Phase 4 exit |
| P5-02 | External penetration test | L | TODO | P5-01 |
| P5-03 | Pentest remediation and re-test | L | TODO | P5-02 |
| P5-04 | Load testing against `docs/PLAN/12` | L | TODO | Phase 4 exit |
| P5-05 | Degraded-dependency testing | L | TODO | P5-04 |
| P5-06 | Horizontal scale-out verification | M | TODO | P5-04 |
| P5-07 | Disaster recovery drill | L | TODO | P0-20 |
| P5-08 | Detection and monitoring completion | L | TODO | P0-11, P1-14 |
| P5-09 | Incident response playbook rehearsal | M | TODO | P5-08 |
| P5-10 | Console — audit log filtering and export | M | TODO | P1-24 |
| P5-11 | Console — accessibility audit | L | TODO | all console tasks |
| P5-12 | Console — performance pass | M | TODO | all console tasks |
| P5-13 | Public security and trust page | M | TODO | P5-03 |
| P5-14 | Integrator documentation completion | L | TODO | Phase 4 exit |
| P5-15 | Risk register reconciliation | M | TODO | P5-03 |
| P5-16 | Production launch readiness review | M | TODO | all above |

---

---

## Phase F — Frontend Implementation (Track)

Runs alongside Phases 0–5, not after them. **Foundation** tasks are phase-independent — start them as soon as `P0-17` lands, because every page task depends on them. **Page** tasks each carry a binding gate; the gate is `docs/PLAN/16`'s lockstep rule made explicit per screen.

### Foundation — start early, no phase gate

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| PF-01 | Motion tokens and the reduced-motion contract | S | TODO | P0-17 |
| PF-02 | Component library scaffolding and workbench | M | TODO | P0-17 |
| PF-03 | Button | S | TODO | PF-02 |
| PF-04 | Table | L | TODO | PF-02 |
| PF-05 | Form field primitives | L | TODO | PF-02 |
| PF-06 | Modal and Side Panel | M | TODO | PF-02, PF-01 |
| PF-07 | Badge and Tag (incl. role-source badge) | S | TODO | PF-02 |
| PF-08 | Confirmation Dialog (incl. typed-confirmation) | M | TODO | PF-06 |
| PF-09 | Breadcrumb | S | TODO | PF-02 |
| PF-10 | Search Input — global and scoped | M | TODO | PF-02 |
| PF-11 | Supporting components (tabs, step indicator, toast, copy, KPI card, pagination, error banner) | L | TODO | PF-02 |
| PF-12 | Routing and permission-gated navigation | L | TODO | PF-02, P1-21 |
| PF-13 | Navigation chrome and organization context | M | TODO | PF-12, PF-09 |
| PF-14 | Global search | M | TODO | PF-10, PF-12 |
| PF-15 | Progressive disclosure rules | S | TODO | PF-13 |
| PF-16 | Four-state system (loading/empty/error/permission) | M | TODO | PF-04 |
| PF-17 | Form system | L | TODO | PF-05 |
| PF-18 | Responsive grid and unsupported-width boundary | M | TODO | PF-13 |
| PF-19 | Accessibility infrastructure | M | TODO | PF-02 |
| PF-20 | Data layer conventions | M | TODO | P0-16, P1-21 |

### Console screens — each gated on its backend task

| ID | Screen | Size | Status | Gate |
|---|---|---|---|---|
| PF-21 | Organization Overview | L | TODO | P1-16 |
| PF-22 | Project list | M | TODO | P1-17 |
| PF-23 | Applications tab | L | TODO | P1-18 |
| PF-24 | User list (incl. invite Flow 1) | L | TODO | P1-19 |
| PF-25 | User detail shell + Profile tab | M | TODO | P1-19 |
| PF-26 | Audit Log | M | TODO | P1-20 |
| PF-27 | Organization Settings | M | TODO | P1-16 |
| PF-28 | Instance screens (org list, policies, audit) | L | TODO | P1-16, P1-20 |
| PF-29 | Organization switcher | M | TODO | P2-08 |
| PF-30 | Roles tab | M | TODO | P2-02 |
| PF-31 | Authorizations tab + User Grants tab | L | TODO | P2-03 |
| PF-32 | Policies — Access tab | M | TODO | P2-10 |
| PF-33 | Sessions tab (Flow 4) | M | TODO | P3-09 |
| PF-34 | MFA tab | M | TODO | P3-02, P3-05 |
| PF-35 | Personal account settings (mobile) | L | TODO | P3-09, P3-02 |
| PF-36 | Project Grants tab (Flow 2) | L | TODO | P4-01 |
| PF-37 | Granted Projects list (Flow 3) | L | TODO | P4-02 |
| PF-38 | Policies — ABAC tab (Flow 5) | L | TODO | P4B-04, P4B-05 |

### Hosted authentication screens

| ID | Screen | Size | Status | Gate |
|---|---|---|---|---|
| PF-39 | Login page | M | TODO | P1-12 |
| PF-40 | MFA challenge and enrollment | M | TODO | P3-03 |
| PF-41 | Password reset and invitation acceptance | M | TODO | P1-19.4, P1-19.5 |
| PF-42 | Logout, consent, protocol error screens | M | TODO | P1-10 |

### Public site

| ID | Task | Size | Status | Gate |
|---|---|---|---|---|
| PF-43 | Layout and shared visual language | M | TODO | P0-18 |
| PF-44 | Landing page | L | TODO | P0-18 |
| PF-45 | About, Contact, Changelog | M | TODO | P0-18 |
| PF-46 | Docs shell and generated API reference | L | TODO | P0-16, P0-18 |
| PF-47 | Security and trust page | M | TODO | P5-03 |

### Frontend quality

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| PF-48 | Component test suite | L | TODO | Track A |
| PF-49 | E2E suite for console flows | L | TODO | Track D |
| PF-50 | Accessibility CI and manual audit | L | TODO | Tracks D, E |
| PF-51 | Visual regression testing | M | TODO | Track A |
| PF-52 | Frontend performance budget | M | TODO | Tracks D, F |
| PF-53 | Frontend acceptance validation | M | TODO | all above |

**Critical path within Phase F**: `PF-02` → `PF-04`/`PF-05` → `PF-16`/`PF-17` → every page task. The table and form primitives block more downstream work than anything else here, and `PF-07`'s role-source badge should be built with both values from the start even though "delegated" does not render until `PF-37`.

**Highest-risk task**: `PF-12`. It is the only frontend task marked `Spec required`, because "genuinely unreachable, not merely hidden" is an authorization property, and getting it wrong produces a UI that disagrees with the API about what a user may do.

## Cross-Phase Watch List

Things that are easy to get wrong once and expensive to fix later. Re-check each at every phase boundary.

| Item | Why it matters | First set in |
|---|---|---|
| Token claim format | Every consumer application parses it; changing it is a coordinated breaking change | `P2-04` |
| `org_id` nested inside role claims | Required from day one to disambiguate delegated roles in Phase 4 | `P2-04` |
| Subset validation on every request | Four documents state this rule; it is where delegation becomes a breach | `P4-02` |
| Server-side authorization on every request | UI hiding is never a control | `P1-15` |
| A tenant-scoped store takes no `org_id` parameter | The predicate that reads as defence in depth is the one that makes the RLS test pass whether or not the policy exists. Confine the transaction, then query without a tenant predicate, so the test has only one thing it can be measuring | `P1-17` |
| RLS is the tenant boundary, not a general-purpose filter | `applications.project_id` is not a tenant column, so two projects in one organization are one tenant to the database. Every non-tenant boundary is an ordinary predicate, and assuming otherwise is how one silently stops existing | `P1-18` |
| A rate limit keyed on the wrong thing bounds nothing | Invite flooding is aimed at a THIRD PARTY's mailbox, so the counter is keyed on the recipient. A per-caller bound leaves it untouched, because the caller is entitled to be there | `P1-19` |
| A concurrency test must CAUSE the race, not hope for one | Sixteen goroutines racing on one token let a read-then-write survive: scheduling and connection acquisition kept them from overlapping, and serially a read-then-write is correct. Hold one transaction open and run the second inside it | `P1-19` |
| A corrected migration never reaches a database that already ran it | golang-migrate records a version as applied and never repeats it, so an edited migration leaves deployed schemas in the state the OLD text produced. Found as `events` being writable by `auth_app` on staging while both migrations read correctly (`PG-24`) | `P1-20` |
| "Append-only" is scoped to the application role | The owner keeps `UPDATE` and `DELETE` on `events`, necessarily — it runs the migrations. Anyone holding the deploy credential can rewrite history (`PG-25`) | `P1-20` |
| A destructive probe against a live audit log is not a test | It is the thing the test exists to prevent. Ask `has_table_privilege`, which is non-destructive, precise about which privilege is asserted, and covers every partition rather than the rows a write would touch | `P1-20` |
| Refresh token storage shape | Must not need changing when rotation arrives in Phase 3 | `P1-07` |
| `auth_methods` accuracy | Step-up in Phase 3 depends on it being trustworthy from Phase 1 | `P1-11` |
| Audit log append-only guarantee | Enforced at the database level, not by convention | `P0-07` |
| No unshipped claims in copy | Applies to marketing, docs, the console, and the OIDC discovery document | `P0-19` |
| Fail-closed authorization | A check that cannot complete denies | `P2-06` |
| RLS as the isolation backstop | Application-layer filtering is not the control | `P0-08` |
| `manager_roles` still has no tenant RLS (DV-02) | Deliberate — needs the user context `P2-05` introduces. `P2-05` cannot be done while it is open | `P0-08` |
| Every new table needs an RLS policy | The migration adds them per table; a table added without one is silently unisolated | `P0-08` |
| Events partition runway | Maintained by the service at startup and daily. If that goroutine stops, the failure appears at a month boundary, not immediately | `P0-12` |
| `SECURITY DEFINER` functions | Two exist for partition maintenance. Their `search_path` is pinned; unpinning it would let a caller have the owner execute their code | `P0-12` |
| Go toolchain patch level | `govulncheck` found six stdlib vulnerabilities at 1.26.5. The `toolchain` directive pins 1.26.6 — keep it current | `P0-12` |
| Metric label cardinality | Route labels use the chi pattern, never the concrete path; unmatched paths collapse to one label. A new route that interpolates an id would be unbounded | `P0-11` |
| Runtime state lives outside the code checkout | Secrets, backups and artifacts in `/home/infra/auth-state/`, so `git pull` and a full re-clone are non-destructive. An operation is only safe if the layout makes it safe — "do not delete that directory" is not a control | `P0-14` |
| Scripts are executable in the INDEX, not just on disk | Windows git ignores the mode bit, so `chmod +x` never reaches the repository and a fresh clone gets files nothing can run. Checked with `git ls-files -s` | `P0-14` |
| Credential verification is not authorization | `internal/authn` answers whether a password matches a hash and nothing about what that permits. A package answering both is one where "the password matched" quietly becomes "the request is allowed" | `P1-01` |
| The not-found path costs what the found path costs | Otherwise response time is an oracle for which addresses have accounts. A real hash, not a sleep — a sleep guesses a duration and does not consume the CPU that makes timings match under load | `P1-01` |
| Hash parameters are measured on the target | 90ms on the VM against 63ms on a laptop. The binding constraint is concurrency, not latency: memory cost multiplies by simultaneous logins | `P1-01` |
| Integration packages run one binary at a time | Each starts its own containers; four providers initialising at once races and fails in a different package each run. `-p 1`. A suite whose failures nobody believes is worse than a slower one | `P1-03` |
| Coverage floors measure the tests that actually run | `internal/signing` reads 44% from unit tests and 83% with integration. A floor against the smaller number rewards testing pure functions and skipping the rotation logic — the part where a bug logs everyone out | `P1-03`, `P0-15` |
| The algorithm is server policy, never the token header | `alg:none` and HS256-with-the-RSA-public-key are both defeated by passing a fixed algorithm list to the parser, which rejects before a key is looked up | `P1-03` |
| A `kid` is derived from the key, not generated | RFC 7638 thumbprint: stable across restarts so consumer caches survive a deploy, and uncrackable so a collision cannot be chosen | `P1-03` |
| Token strings are malleable; token identity is not | 16 distinct encodings of one signature all verify. Reuse detection, denylists and replay caches must key on `jti` or the decoded signature, never the raw string | `P1-03` |
| A runbook is a hypothesis until executed | Running this one found no Go on the VM and a key reference pointing at a path only the tool could see | `P1-03` |
| A machine-readable claim is held to a higher bar than a written one | A human reading marketing copy is sceptical; a client library is not. `CLAUDE.md`'s no-unshipped-capability rule binds harder on the discovery document than on the landing page | `P1-04` |
| Derive the document from the router, never write it out | A JSON literal is a second source of truth that starts correct and drifts on the first release where an endpoint moves. Deriving makes the false claim unrepresentable rather than merely discouraged | `P1-04` |
| A DoD item that a later task makes true is left unticked | Two of `P1-04`'s five need an authorization endpoint and a token issuer. Ticking them would have been the exact false claim the task exists to prevent; `P1-06`/`P1-07` tick them | `P1-04` |
| A tool can lie about the service | `curl -I` reported `no-store` on an endpoint serving `max-age=300`. chi matches methods exactly, so HEAD reached no route and returned a 405's headers. Check what the server does before believing what the client says it did | `P1-04` |
| A parser's success condition can be satisfied by a failure | The corpus check counted lines; an HTML error page is one line, so a proxy's "Access denied" read as a clean answer and admitted the password. Not a test that did not run — a check whose definition of success was wrong | `P1-02` |
| Configurable security needs a floor the configuration cannot cross | Without one, `"min_length": 1` is valid and the control an administrator was given is the control they can silently remove | `P1-02` |
| Fail open is only defensible when the skip is loud | ADR-015 accepts an unchecked password rather than blocking rotation during an incident. The audit event, the metric label and the alert are not extras — they are the half that makes the trade honest | `P1-02` |
| A length rule in bytes is a different rule per language | Twelve characters means twelve in English and four in Japanese. Runes over NFC, and normalization can only make it stricter | `P1-02` |
| Absent and false must stay distinguishable in a settings document | A plain `bool` collapses them and the default silently wins — a failure in the insecure direction | `P1-02` |
| A healthy timer says nothing about the job it triggers | The nightly backup died at `226/NAMESPACE` for a day while `systemctl list-timers` reported it fine — the timer fired correctly every time. Watch for the absence of a recent success, not for a failure that a job too broken to start never emits | `P0-20`, `BL-01` |
| A slow hash is right for passwords and wrong for high-entropy secrets | 256 bits already puts brute force at 10^52 years, so Argon2id on a client secret buys nothing and hands an attacker 90ms plus 64 MiB per wrong guess — on a path verified once per token request | `P1-05` |
| `fmt.Stringer` does not redact under every verb | `%d` on a struct falls back to printing its fields; the secret came out inside a `%!d(string=...)` marker. `fmt.Formatter` is the only thing that covers all of them | `P1-05` |
| Validation belongs where a human is, matching belongs where an attacker is | Every redirect rule runs at registration, and the matcher does nothing but compare strings. Cleverness at the matching end is what the open-redirect class is made of | `P1-05` |
| A rejection table needs a row that must pass | Fifteen URIs asserted rejected prove nothing about a matcher that rejects everything | `P1-05` |
| Instance scope is "no tenant", not "every tenant" | An RLS policy of `org_id = current_org_id()` matches nothing when no tenant is set. The bootstrap read that resolves a session cookie needs its own granted function, not a relaxed policy | `P1-11` |
| A cache in front of an authoritative store defers revocation unless you make it not | Commit then invalidate — the other order lets a concurrent reader repopulate the pre-commit state — and a tombstone closes the read-then-write-back race the ordering leaves | `P1-11` |
| A redaction list encodes an assumption about what is a credential | `session_id` was redacted because the id used to be the cookie. After `PG-14` it is the safe identifier, and redacting it made the audit log unable to say which session was revoked | `P1-11` |
| A security flag that takes a parameter is a flag someone passes false to | gosec flagged `Secure: secure` on the session cookie. The real fix was deleting the parameter: no case needs it off, and `__Host-` requires it on | `P1-11` |
| Validate the redirect target before anything else can report an error | Every other parameter error is delivered by redirecting, so an error reported before `redirect_uri` is validated is an open redirect delivered by the code meant to prevent one | `P1-06` |
| A sequential test cannot see a lost race | `GET`-then-`DEL` code redemption passes single-use tests and lets 32 concurrent racers all succeed. `docs/PLAN/04` asked for atomicity; only a concurrent test checks it | `P1-06` |
| Assert the absence of the header, not the presence of the status | "Returns 400" would still pass if the handler also set `Location`. "Has no `Location`" is the property, and it is unusual enough to be conspicuous when broken | `P1-06` |
| A generated router can be the wrong tool for one endpoint | Parameter binding runs before the handler, which defeats ordered validation and collapses duplicate parameters. Excluding one operation and saying why beats bending the endpoint around the generator | `P1-06` |
| A capability is not one task | The capability audit called single sign-on shipped when `P1-06` landed, while a consumer still had nowhere to exchange a code and no page to log in on. A check that maps a user-visible capability to a single task will eventually make the false claim it exists to prevent | `P1-06`, `P0-19` |
| A credential's storage form is a decision about the NEXT phase too | The refresh token is opaque rather than a JWT partly because `P1-03` found a JWT's string is non-canonical — so reuse detection keyed on it, which `P3-06` will build, could be defeated by changing one character | `P1-07` |
| Consume the single-use credential before checking anything else | Leaving the code alive through a failed verifier turns it into an oracle to brute-force against, one request at a time | `P1-07` |
| A function can check that a credential was supplied and never check it | `AuthenticateClient` returned nil for any secret at all, and every test then written would have passed because none presented a wrong one | `P1-07` |
| A deferred DoD item has an owner, and the owner ticks it | `P1-04` left two items unticked naming the tasks that would make them true. `P1-07` made them true and ticked them — which only works because the deferral said whose job it was | `P1-04`, `P1-07` |
| A backup is verified by restoring it | Not by a hardcoded list of tables — that reported "all 14 restored" against a database with 19, and would not have checked a table added by a later migration. Compare the source: table set and per-table row counts | `P0-20` |
| Tests never skip a missing dependency | A suite that skips when a database is unreachable reports success having run nothing. Integration tests start their own containers and `Fatal` if they cannot — a green suite that skipped its tests is a false statement everyone acts on | `P0-15` |
| A test harness mirrors production's privilege model | Approximating it produced a harness where the audit log was not append-only, so the suite tested a database nothing would run. Same grants, same order as `deploy/postgres/init` | `P0-15` |
| Coverage floors name packages, never an average | A repo-wide percentage is satisfied by testing whatever is easiest, and that is rarely where a bug matters | `P0-15` |
| Name the address family on both sides | `localhost` resolves to `::1` first while `listen 80` is IPv4-only. Hit three times in one day; the symptom is a healthy server nothing can reach, which never looks like name resolution | `P0-15`, `P0-17` |
| Public copy claims nothing unshipped | `CLAIMS.md` plus two checks: every capability carries a phase label and no label contradicts the board; the built pages contain no verbatim phrase from `docs/SECURITY/02`, `docs/PLAN/14` or `docs/PLAN/18`. Neither can read prose — that part stays human | `P0-19` |
| Separation between site and console is checked | `docs/PLAN/20` says they share the visual language and nothing else. Three scripts enforce it: token values match, no code is imported across, contrast holds in both themes. All erode by convenience rather than decision | `P0-18` |
| A dark palette is not a light palette | Carrying the console's colours to a dark surface put every semantic token below AA. Same meaning, different value per surface — and computed, not eyeballed | `P0-18` |
| Missing pages return 404, not 200 | `try_files` with a file fallback serves a styled 404 page with a 200 status. Crawlers are then told missing URLs exist, on the surface whose job is discovery | `P0-18` |
| Token discipline is enforced, not asked for | Three local ESLint rules reject raw hex, Tailwind arbitrary values, and inline styles in console code (`docs/UI-UX/05` § Governance). Its first run found a real bug: a class referencing a token that did not exist | `P0-17` |
| Design tokens must be verified against the compiler | Every type token was defined and correctly named and generated no CSS, because Tailwind v4's namespace is `--text-*` not `--font-size-*`. Source-reading tests passed throughout. `utilities.test.ts` compiles Tailwind and asserts the classes actually produce rules | `P0-17` |
| `color-danger` cannot be rebranded | A union of literal token names plus a runtime filter, because branding arrives as untyped JSON. Tested | `P0-17` |
| Static hosting needs a history fallback | `try_files $uri $uri/ /index.html`, and the healthcheck probes a route with no file behind it so the fallback cannot be dropped unnoticed | `P0-17` |
| nginx `add_header` does not accumulate | A `location` block with any `add_header` of its own discards every server-level header. Keep all headers in one context; put varying values in a `map` | `P0-17` |
| API contract direction | The spec generates the code, never the reverse (ADR-013). Handlers implement a generated interface, so a signature that stops matching the contract fails to compile rather than failing a CI check after the push | `P0-16` |
| Spec documents only shipped endpoints | `/docs/api-reference` renders from `openapi.yaml`, so a documented endpoint is a public claim it exists. `scripts/openapi-shipped-paths.py` gates it; adding an endpoint means adding it to `SHIPPED` in the same commit | `P0-16` |
| Metrics port publication | Not published to the host. Reachable inside the compose network only; `docker-compose.metrics-port.yml` is the opt-in override, loopback-only with no interface variable | `P0-11` |
| Metrics listener binding | Disclosing, so it authenticates on its own behalf: a bearer token compared in constant time, rejecting with a bare `404`. Required whenever the bind is not loopback, enforced by refusing to boot. Under Docker the bind is always `0.0.0.0` inside the container, so the token is not optional there | `P0-11` |
| Secret file ownership | Owned by the service's uid (`65532`), not the operator's. The runtime image is distroless `:nonroot`, so a secrets directory owned by the operator at mode `700` cannot be traversed and the mode on the file inside is never reached. `chmod` alone is necessary and not sufficient | `P0-11`, `P0-14` |
| Startup failure ordering | Secrets resolve before any pool is opened or goroutine started, so the first thing to fail is the thing that is wrong. Acquiring a resource before validation completes produces a second, louder, misleading error | `P0-11` |
| Alerts must handle absence | A counter with no observations produces no series, so `== 0` never fires when something stops. Use `absent()` or a gauge | `P0-11` |
| Role-source badge built with both values | Retrofitting it means auditing every role-displaying screen twice | `PF-07` |
| Org-scoped query keys | Without them, a context switch renders the previous org's cached data — a leak in the UI even with a correct API | `PF-20` |
| `color-danger` reserved for destructive actions only | Its meaning must stay reliable; one misuse on a late screen degrades every earlier one | `PF-03` |
| Accessibility built in, not retrofitted | `docs/PLAN/18` R-10 names late retrofitting as a real risk; `docs/UI-UX/13` says it is far more expensive | `PF-19` |
| Audit log retention confirmed | 24 months is a working default, not a confirmed obligation (`BACKLOG.md` OQ-09) | `P0-07` |
| Single-VM production (DV-01) | A VM reboot takes authentication down platform-wide; `docs/PLAN/14` requires Multi-AZ. Must close or be formally risk-accepted before a consumer app depends on it | `P0-20` |
| ~~`AUTH_ISSUER` placeholder~~ | Resolved 2026-09-08: `https://auth.zedth.my.id`, verified in the running process | `P0-20` |
| ~~Service bound to `0.0.0.0`~~ | Resolved 2026-09-08: narrowed to `127.0.0.1`; LAN access confirmed closed | `P0-20` |
| Issuer must match on every environment | The `iss` claim, the discovery document, and each client's configured issuer must agree exactly; a mismatch fails verification with an error naming none of them | `P1-04` |
| `AUTH_BACKUP_REMOTE` actually set | Unset means backups sit on the disk they protect — safety-feeling without safety | `P0-20` |
| Service never receives owner credentials | The owner bypasses RLS and can alter the audit log; asserted in CI against the rendered compose config | `P0-20` |
