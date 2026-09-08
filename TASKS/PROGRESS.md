# Progress Board

Single source of truth for where the project stands. Updated in the same commit as the work it describes (`00-TASK-CONVENTIONS.md` global DoD item 8).

**Last updated**: 2026-09-08
**Current phase**: Phase 0 — Foundation
**Overall**: 1 / 124 tasks done

Status values: `TODO` · `BLOCKED` · `SPEC` · `WIP` · `REVIEW` · `DONE` · `DROPPED`
Sizes: `S` under half a day · `M` one to two days · `L` several days · `XL` must be split

---

## Phase Summary

| Phase | Tasks | Done | Status | Gate to enter |
|---|---|---|---|---|
| [Phase 0 — Foundation](./PHASE-0-FOUNDATION.md) | 21 | 1 | **ACTIVE** | — |
| [Phase 1 — MVP: Core Auth + SSO](./PHASE-1-MVP-CORE-AUTH-SSO.md) | 28 | 0 | Not started | Phase 0 exit checklist |
| [Phase 2 — RBAC & Multi-Tenancy](./PHASE-2-RBAC-MULTITENANCY.md) | 17 | 0 | Not started | Phase 1 exit + `P1-28` |
| [Phase 3 — Advanced Security](./PHASE-3-ADVANCED-SECURITY.md) | 15 | 0 | Not started | Phase 2 exit + threat model review |
| [Phase 4 — Enterprise Interop](./PHASE-4-ENTERPRISE-INTEROP.md) | 16 | 0 | Not started | Phase 3 exit + threat model review |
| [Phase 4b — ABAC](./PHASE-4B-ABAC.md) | 11 | 0 | **CONDITIONAL** | A concrete requirement RBAC cannot express (`P4B-00`) |
| [Phase 5 — Hardening](./PHASE-5-HARDENING.md) | 16 | 0 | Not started | Phase 4 exit; 4b done or declined |

> Phase 3 and Phase 4 may be swapped **as whole phases** if business need demands it (`PLAN/16`'s note). They are never interleaved task by task.

---

## Phase 0 — Foundation

| ID | Task | Size | Status | Depends on |
|---|---|---|---|---|
| P0-01 | Confirm and freeze the tech stack (ADRs) | S | TODO | — |
| P0-02 | Initialize repository structure | S | TODO | P0-01 |
| P0-03 | Git conventions, branch strategy, PR template | S | TODO | P0-02 |
| P0-04 | Go service skeleton | M | TODO | P0-02 |
| P0-05 | Local environment via Docker Compose | M | TODO | P0-04 |
| P0-06 | Migration tooling and baseline migration | S | TODO | P0-05 |
| P0-07 | Core schema implementation | L | TODO | P0-06 |
| P0-08 | Row-level security scaffolding | M | TODO | P0-07 |
| P0-09 | Structured logging with redaction | M | TODO | P0-04 |
| P0-10 | Health and readiness endpoints | S | TODO | P0-04 |
| P0-11 | Metrics and tracing baseline | M | TODO | P0-09 |
| P0-12 | Audit event writer | M | TODO | P0-07, P0-09 |
| P0-13 | CI pipeline | M | TODO | P0-04 |
| P0-14 | Secrets and configuration conventions | S | TODO | P0-04 |
| P0-15 | Test harness | M | TODO | P0-05, P0-16 |
| P0-16 | OpenAPI spec skeleton and client generation | M | TODO | P0-13 |
| P0-17 | Console skeleton with design tokens | L | TODO | P0-02, P0-16 |
| P0-18 | Public site skeleton | L | TODO | P0-02 |
| P0-19 | Landing, About, docs skeleton content | M | TODO | P0-18 |
| P0-20 | Staging environment provisioning | L | TODO | P0-13 |
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
