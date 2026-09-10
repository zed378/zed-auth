# 18 — Risk Register

A living document. Update this whenever `10-THREAT-MODEL.md` review, an incident postmortem (`15-DISASTER-RECOVERY.md`), or a pentest (`11-TESTING.md`) surfaces a new risk. Each entry should have an owner and a review date, not sit unowned indefinitely.

## Risk Scoring

| Likelihood | Impact | Resulting Priority |
|---|---|---|
| Low / Medium / High | Low / Medium / High | Priority = combination, e.g. High likelihood + High impact = **Critical** |

## Register

| ID | Risk | Likelihood | Impact | Priority | Mitigation | Owner | Status |
|---|---|---|---|---|---|---|---|
| R-01 | Signing key compromise (private key leaked) | Low | Critical | Critical | Key stored in secret manager, never in code/env plaintext; rotation plan in `09-SECURITY.md` | Backend lead | Open |
| R-02 | Cross-tenant data leak via a misscoped query | Medium | Critical | Critical | PostgreSQL row-level security enforced at DB layer, tested per `11-TESTING.md` | Backend lead | Open |
| R-03 | Big-bang migration attempted instead of gradual rollout, causing a platform-wide login outage | Medium | Critical | Critical | Explicit constraint in `01-PRODUCT-SCOPE.md`: migration must be gradual, app by app | Project lead | Open |
| R-04 | Receiving organization escalates privilege beyond a Project Grant's allowed roles | Medium | High | High | Server-side validation that requested `role_keys` ⊆ `granted_role_keys` **on every request**, not only at grant-creation time — a grant narrowed or revoked after creation must stop working immediately (`08-AUTHORIZATION.md` Part C, `19-FEATURE-SPECIFICATION-TEMPLATE.md` worked example); covered by automated tests (`10-THREAT-MODEL.md`, `11-TESTING.md`) | Backend lead | Open |
| R-05 | A bad ABAC policy silently locks out or over-grants access in production | Medium | High | High | Mandatory dry-run before activation, versioned rollback (`08-AUTHORIZATION.md` Part D) | Backend lead | Open (only if Phase 4b undertaken) |
| R-06 | Auth Service outage cascades into every consumer application being unable to authenticate | Medium | Critical | Critical | Stateless horizontal scaling, graceful degradation, DR drills (`12-PERFORMANCE.md`, `13-OBSERVABILITY.md`, `15-DISASTER-RECOVERY.md`) | Infra lead | Open |
| R-07 | Backup exists but has never been restore-tested, and fails when actually needed | Low | Critical | High | Scheduled restore drills (`15-DISASTER-RECOVERY.md`) | Infra lead | Open |
| R-08 | Console UI silently allows an action the API would reject, confusing admins about actual permissions | Low | Medium | Medium | API-first principle enforced via generated client + contract tests (`06-FRONTEND-ARCHITECTURE.md`) | Frontend lead | Open |
| R-09 | Team under-estimates complexity of multi-org (workspace switching) and builds it prematurely, delaying MVP | Medium | Medium | Medium | Explicitly deferred in `01-PRODUCT-SCOPE.md`; revisit only on confirmed need | Project lead | Open |
| R-10 | Accessibility retrofitted late, requiring expensive rework across the whole console | Medium | Medium | Medium | WCAG AA practices built in from first screens, not deferred (`UI-UX/13-ACCESSIBILITY.md`) | Frontend lead | Open |

## Review Cadence

- Review this register at the start of every roadmap phase (`16-IMPLEMENTATION-ROADMAP.md`).
- Any risk marked **Critical** must have an owner and an active mitigation plan before the phase it blocks can be marked done in `17-ACCEPTANCE-CRITERIA.md`.

---

This concludes the `PLAN/` folder. For the management console's design planning (personas, journeys, information architecture, visual language, component specs, and more), see the `UI-UX/` folder.
