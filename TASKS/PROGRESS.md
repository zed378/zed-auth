# Progress Board

Single source of truth for where the project stands. Updated in the same commit as the work it describes (`00-TASK-CONVENTIONS.md` global DoD item 8).

**Last updated**: 2026-09-08
**Current phase**: Phase 0 — Foundation (11 / 21 done)
**Overall**: 11 / 177 tasks done

Status values: `TODO` · `BLOCKED` · `SPEC` · `WIP` · `REVIEW` · `DONE` · `DROPPED`
Sizes: `S` under half a day · `M` one to two days · `L` several days · `XL` must be split

---

## Phase Summary

| Phase | Tasks | Done | Status | Gate to enter |
|---|---|---|---|---|
| [Phase 0 — Foundation](./PHASE-0-FOUNDATION.md) | 21 | 16 | **ACTIVE** | — |
| [Phase 1 — MVP: Core Auth + SSO](./PHASE-1-MVP-CORE-AUTH-SSO.md) | 28 | 0 | Not started | Phase 0 exit checklist |
| [Phase 2 — RBAC & Multi-Tenancy](./PHASE-2-RBAC-MULTITENANCY.md) | 17 | 0 | Not started | Phase 1 exit + `P1-28` |
| [Phase 3 — Advanced Security](./PHASE-3-ADVANCED-SECURITY.md) | 15 | 0 | Not started | Phase 2 exit + threat model review |
| [Phase 4 — Enterprise Interop](./PHASE-4-ENTERPRISE-INTEROP.md) | 16 | 0 | Not started | Phase 3 exit + threat model review |
| [Phase 4b — ABAC](./PHASE-4B-ABAC.md) | 11 | 0 | **CONDITIONAL** | A concrete requirement RBAC cannot express (`P4B-00`) |
| [Phase 5 — Hardening](./PHASE-5-HARDENING.md) | 16 | 0 | Not started | Phase 4 exit; 4b done or declined |
| [Phase F — Frontend Implementation](./PHASE-F-FRONTEND-IMPLEMENTATION.md) | 53 | 0 | **TRACK** — runs alongside | Foundation: `P0-17`. Pages: each carries its own gate |

> Phase 3 and Phase 4 may be swapped **as whole phases** if business need demands it (`PLAN/16`'s note). They are never interleaved task by task.
>
> Phase F is a **track, not a sequence position**. Its foundation tasks (`PF-01`–`PF-20`, `PF-48`–`PF-52`) are phase-independent and should start as soon as `P0-17` lands. Its page tasks (`PF-21`–`PF-47`) each carry a binding gate, which is how `PLAN/16`'s lockstep rule is enforced per screen rather than per phase.

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
| P0-15 | Test harness | M | TODO | P0-05, P0-16 |
| P0-16 | OpenAPI spec skeleton and client generation | M | **WIP** — contract, Go generation and CI gates done; console client (step 3) and public-site rendering (step 4) carried into `P0-17` and `P0-18`, which create those surfaces | P0-13 |
| P0-17 | Console skeleton with design tokens | L | TODO | P0-02, P0-16 |
| P0-18 | Public site skeleton | L | TODO | P0-02 |
| P0-19 | Landing, About, docs skeleton content | M | TODO | P0-18 |
| P0-20 | Staging environment provisioning | L | **DONE** | P0-13 |
| P0-21 | Adopt the TASKS/MEMORY working discipline | S | **DONE** | — |

**Suggested parallel tracks** once `P0-02` lands: backend (`P0-04` → `P0-05` → `P0-06` → `P0-07` → `P0-08`), platform (`P0-13` → `P0-14` → `P0-20`), frontend (`P0-17`), and public site (`P0-18` → `P0-19`). `P0-16` gates both `P0-15` and `P0-17`, so it should not wait.

---

## Phase 1 — MVP: Core Auth + Basic SSO

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P1-01 | Argon2id password hashing | M | TODO | P0-15 |
| P1-02 | Password policy and breached-password rejection | M | TODO | P1-01 |
| P1-03 | Signing key management, JWKS, rotation | L | TODO | P0-14 |
| P1-04 | Discovery document and JWKS endpoint | S | TODO | P1-03 |
| P1-05 | Application registration and credentials | M | TODO | P0-07 |
| P1-06 | `GET /oauth/authorize` — code + PKCE | L | TODO | P1-05, P1-11 |
| P1-07 | `POST /oauth/token` | L | TODO | P1-06, P1-03 |
| P1-08 | `GET /oauth/userinfo` | S | TODO | P1-07 |
| P1-09 | `/oauth/introspect` and `/oauth/revoke` | M | TODO | P1-07 |
| P1-10 | `GET /oidc/logout` | M | TODO | P1-11 |
| P1-11 | Session management and SSO cookie | L | TODO | P0-07 |
| P1-12 | Hosted login page | M | TODO | P1-01, P1-11 |
| P1-13 | Login rate limiting and lockout | M | TODO | P1-12 |
| P1-14 | Authentication audit events | S | TODO | P0-12, P1-12 |
| P1-15 | Management API foundation | L | TODO | P0-16, P1-07 |
| P1-16 | Management API — organizations | M | TODO | P1-15 |
| P1-17 | Management API — projects | M | TODO | P1-15 |
| P1-18 | Management API — applications | M | TODO | P1-15, P1-05 |
| P1-19 | Management API — users | L | TODO | P1-15, P1-01 |
| P1-20 | Management API — audit log read | M | TODO | P1-15, P0-12 |
| P1-21 | Console — OIDC login (dogfooding) | L | TODO | P0-17, P1-07 |
| P1-22 | Console — Org overview, Projects, Applications | L | TODO | P1-21, P1-16..18 |
| P1-23 | Console — Users list and detail | L | TODO | P1-21, P1-19 |
| P1-24 | Console — Audit Log screen | M | TODO | P1-21, P1-20 |
| P1-25 | Public docs — quickstart and API reference | M | TODO | P1-07, P1-19 |
| P1-26 | Two demo consumer applications | M | TODO | P1-07 |
| P1-27 | Phase 1 test suite completion | L | TODO | all above |
| P1-28 | Phase 1 acceptance validation | M | TODO | P1-27 |

**Critical path**: `P1-11` → `P1-06` → `P1-07` → everything else. Session management and the token endpoint are the two tasks that block the most downstream work; start them first and give them the most review attention.

---

## Phase 2 — Full RBAC & Multi-Tenancy

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P2-01 | Role model and permission keys | M | TODO | P1-17 |
| P2-02 | Management API — roles | M | TODO | P2-01 |
| P2-03 | User grants | L | TODO | P2-01, P1-19 |
| P2-04 | Role claims in the access token | L | TODO | P2-03, P1-07 |
| P2-05 | Manager role enforcement, full hierarchy | L | TODO | P1-15 |
| P2-06 | `POST /v1/authz/check` — RBAC | L | TODO | P2-03 |
| P2-07 | Authorization decision caching | M | TODO | P2-06 |
| P2-08 | Multi-organization activation | L | TODO | P0-08, P1-16 |
| P2-09 | Tenant resolution strategy | M | TODO | P2-08 |
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

> This phase not running is a valid, plan-endorsed outcome. `PLAN/08` Part D: build it only once a concrete need appears that RBAC genuinely cannot express.

---

## Phase 5 — Hardening & Production Scale

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P5-01 | Internal security verification sweep | L | TODO | Phase 4 exit |
| P5-02 | External penetration test | L | TODO | P5-01 |
| P5-03 | Pentest remediation and re-test | L | TODO | P5-02 |
| P5-04 | Load testing against `PLAN/12` | L | TODO | Phase 4 exit |
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

Runs alongside Phases 0–5, not after them. **Foundation** tasks are phase-independent — start them as soon as `P0-17` lands, because every page task depends on them. **Page** tasks each carry a binding gate; the gate is `PLAN/16`'s lockstep rule made explicit per screen.

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
| Accessibility built in, not retrofitted | `PLAN/18` R-10 names late retrofitting as a real risk; `UI-UX/13` says it is far more expensive | `PF-19` |
| Audit log retention confirmed | 24 months is a working default, not a confirmed obligation (`BACKLOG.md` OQ-09) | `P0-07` |
| Single-VM production (DV-01) | A VM reboot takes authentication down platform-wide; `PLAN/14` requires Multi-AZ. Must close or be formally risk-accepted before a consumer app depends on it | `P0-20` |
| ~~`AUTH_ISSUER` placeholder~~ | Resolved 2026-09-08: `https://auth.zedth.my.id`, verified in the running process | `P0-20` |
| ~~Service bound to `0.0.0.0`~~ | Resolved 2026-09-08: narrowed to `127.0.0.1`; LAN access confirmed closed | `P0-20` |
| Issuer must match on every environment | The `iss` claim, the discovery document, and each client's configured issuer must agree exactly; a mismatch fails verification with an error naming none of them | `P1-04` |
| `AUTH_BACKUP_REMOTE` actually set | Unset means backups sit on the disk they protect — safety-feeling without safety | `P0-20` |
| Service never receives owner credentials | The owner bypasses RLS and can alter the audit log; asserted in CI against the rendered compose config | `P0-20` |
