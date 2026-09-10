# 19 — Feature Specification Template

Every feature added to this system — whether it's a small addition to an existing module or a whole new capability — must be broken down using this template before implementation starts. This turns `16-IMPLEMENTATION-ROADMAP.md`'s checklist items from a vague to-do ("Project Grants tab") into something a developer can actually build, review, and test against without re-deriving decisions ad hoc.

This is a **template**, not a one-time document — copy this structure into a new file (e.g. `PLAN/features/FEATURE-project-grant-creation.md`) for each non-trivial feature as it enters active development.

## Template Sections

### 1. Business Objective
What real-world outcome does this feature serve, in one or two sentences. Not "implement X" — the *reason* X matters (see `01-PRODUCT-SCOPE.md` for how this ties back to overall goals).

### 2. User / System Actors
Which personas (`UI-UX/01-USER-PERSONAS.md`) or system components initiate or are affected by this feature. Be specific about roles (`manager_roles`, `08-AUTHORIZATION.md`), not just "admin."

### 3. Functional Requirements
What the feature must do, as testable statements (in the style of `02-REQUIREMENTS.md`'s FR-N numbering).

### 4. Non-Functional Requirements
Latency, availability, scale expectations specific to this feature — reference `12-PERFORMANCE.md` targets where applicable, or set feature-specific ones if the general targets don't apply.

### 5. Dependencies
Other features, services, or infrastructure this depends on, and what depends on it. Call out anything that blocks a rollback (see §19 below).

### 6. Database Changes
New tables/columns, migrations required, and whether they're backward-compatible per `14-DEPLOYMENT.md`'s expand/contract pattern.

### 7. API Contract
New or changed endpoints, request/response shapes, following the conventions in `05-API-CONTRACT.md`. Include the OpenAPI diff, not just prose.

### 8. Frontend Changes
Which screens/components (`UI-UX/08-PAGE-SPECIFICATIONS.md`, `07-COMPONENT-SPECIFICATION.md`) are added or modified.

### 9. Backend Changes
Which internal modules/packages (`07-BACKEND-ARCHITECTURE.md`) are touched.

### 10. Authorization Rules
Exactly which roles/manager_roles/ABAC policies gate this feature (`08-AUTHORIZATION.md`). State both the positive rule (who can) and the negative rule (who explicitly cannot, even if adjacent roles might seem like they should).

### 11. Validation
Input validation rules, both client-side (`UI-UX/15-FORM-UX.md`) and server-side (never trust client-side validation alone).

### 12. Error Handling
Map each failure mode to its user-facing treatment (`UI-UX/14-EMPTY-LOADING-ERROR-STATES.md`) and its API error code (`05-API-CONTRACT.md`'s error format).

### 13. Edge Cases
Boundary conditions: empty inputs, maximum values, concurrent edits, partial failures mid-flow.

### 14. Abuse Cases
How could this feature be misused or turned against the system's own security model — cross-reference the relevant entries in `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`. Every feature that touches access control must explicitly consider privilege escalation and IDOR-style risks here, even if the answer is "not applicable, because X."

### 15. Logging / Audit Requirements
What gets written to the `events` table (`04-DATA-MODEL.md`), with what level of detail, and whether it needs to feed the SIEM per `SECURITY/03-DETECTION-AND-MONITORING.md`.

### 16. Security Controls
Specific controls this feature needs beyond the baseline in `09-SECURITY.md` (e.g. rate limiting on a new endpoint, additional confirmation step per `UI-UX/09-INTERACTION-DESIGN.md`).

### 17. Testing Strategy
Which layers of the pyramid (`11-TESTING.md`) apply, and specifically which abuse-case tests (from §14 above) need automated coverage.

### 18. Acceptance Criteria
Concrete, checkable statements — feeds into `17-ACCEPTANCE-CRITERIA.md`.

### 19. Definition of Done
The general DoD from `17-ACCEPTANCE-CRITERIA.md` plus anything feature-specific.

### 20. Implementation Sequence
Order of work (e.g. migration → backend → API contract published → frontend → E2E tests), so dependent work isn't started before its prerequisite lands.

### 21. Rollback Strategy
How to disable/revert this feature in production without a full deployment rollback where possible (feature flag, per `14-DEPLOYMENT.md`) — and what data cleanup, if any, a rollback requires.

### 22. Technical Risks
Feed into `18-RISK-REGISTER.md` if the risk is significant enough to track at the project level, not just the feature level.

---

## Worked Example: Project Grant Creation

To make the template concrete, here it is applied to a real feature already scoped in this plan (`08-AUTHORIZATION.md` Part C, `UI-UX/04-USER-FLOWS.md` Flow 2):

| Section | Content |
|---|---|
| Business Objective | Let a project owner delegate a subset of their project's roles to a partner organization, so the partner can self-manage their own team's access without ongoing manual work from the owning org. |
| Actors | Project Owner (Sari), Project Grant Owner at the receiving org (Reza) — see `UI-UX/01-USER-PERSONAS.md`. |
| Functional Requirements | FR-8 (`02-REQUIREMENTS.md`): a project can be delegated to another org with a restricted role subset. |
| Non-Functional Requirements | Grant creation/revocation must propagate to authorization decisions within the cache TTL window defined in `08-AUTHORIZATION.md` (not instantly for cached decisions, but bounded and documented). |
| Dependencies | Requires multi-organization support to be active (Phase 2, `16-IMPLEMENTATION-ROADMAP.md`) before Phase 4's Project Grants can be meaningfully used. |
| Database Changes | New `project_grants` table, `user_grants.project_grant_id` column addition (`04-DATA-MODEL.md`) — additive, backward-compatible. |
| API Contract | `POST/GET/DELETE /v1/organizations/{org_id}/projects/{project_id}/grants` (`05-API-CONTRACT.md`). |
| Frontend Changes | Project Grants tab, Granted Projects list (`UI-UX/08-PAGE-SPECIFICATIONS.md`). |
| Backend Changes | Authorization module: grant validation, extended token claim logic (`08-AUTHORIZATION.md` Part C). |
| Authorization Rules | Only `PROJECT_OWNER` can create/revoke a grant on their own project. The receiving org's `PROJECT_GRANT_OWNER` can only assign roles that are a subset of `granted_role_keys` — explicitly cannot assign any other project role, even ones they might administratively see elsewhere. |
| Validation | `granted_role_keys` must all exist as real roles on the project; `granted_org_id` must not equal `granting_org_id` (can't delegate to yourself). |
| Error Handling | Attempting to assign a non-granted role returns `403` with a specific error code distinguishing it from a generic permission failure, so the frontend can show "this role isn't part of your grant" rather than a generic "forbidden." |
| Edge Cases | Revoking a grant while a user-grant-assignment request from the receiving org is in flight; a project role being deleted while it's referenced by an active grant. |
| Abuse Cases | A receiving org attempts to assign a role outside `granted_role_keys` directly via the API (bypassing the UI) — must be rejected server-side, not just hidden in the UI. See `SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` "Authorization Bypass / IDOR." |
| Logging / Audit | Grant creation, role-subset assignment, and revocation are all logged with actor, org context, and affected role list. |
| Security Controls | Server-side subset validation on every assignment request, not just at grant-creation time. |
| Testing Strategy | Integration test for the full delegation flow; a dedicated security test attempting to assign a non-granted role directly via API (`11-TESTING.md`). |
| Acceptance Criteria | See `17-ACCEPTANCE-CRITERIA.md` Phase 4 criteria. |
| Definition of Done | Template §19 general DoD + this feature's specific tests passing in CI. |
| Implementation Sequence | Migration → backend grant CRUD + validation → API contract published → frontend Project Grants tab → E2E test. |
| Rollback Strategy | Feature flag gating grant creation; existing grants remain valid data even if the flag is disabled, so disabling doesn't orphan data. |
| Technical Risks | R-04 in `18-RISK-REGISTER.md` (privilege escalation via delegation). |

---

This template is self-contained — copy it as the starting point for every new feature file. For the full security threat pipeline referenced in §14/§16 above, see the `SECURITY/` folder.
