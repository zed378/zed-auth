# 17 — Acceptance Criteria

Sign-off checklist per phase, referencing the detailed requirements in earlier documents. Each phase in `16-IMPLEMENTATION-ROADMAP.md` is considered done only when every item below is checked, not just when the feature "seems to work."

## Phase 1 (MVP) Acceptance Criteria

- [ ] Two independent internal applications authenticate real users through this service.
- [ ] A user logged into Application A opens Application B and is **not** prompted to log in again.
- [ ] `POST /oauth/token` and `GET /oauth/authorize` conform exactly to `05-API-CONTRACT.md`.
- [ ] Organizations, projects, applications, and users can be created via both the REST API and the console, and the two stay consistent (creating via one is visible via the other).
- [ ] Failed and successful logins appear in the audit log with correct actor/timestamp.
- [ ] Login rate limiting demonstrably blocks a simulated brute-force attempt (see `10-THREAT-MODEL.md`).
- [ ] All Phase 1 items in `11-TESTING.md`'s pyramid have passing automated tests in CI.
- [ ] Public landing page and docs quickstart exist and accurately reflect the real MVP flow — no described capability that isn't actually shipped (`UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` governance rule).

## Phase 2 (RBAC & Multi-Tenancy) Acceptance Criteria

- [ ] A role assigned to a user in Project A is correctly reflected in that user's access token claim, scoped to Project A only.
- [ ] `/v1/authz/check` returns a correct allow/deny decision for a role-based query.
- [ ] Per-organization password policy and MFA-required settings are enforced at login time, not just stored.
- [ ] A user with no grant has zero access by default (least privilege verified, not assumed).

## Phase 3 (Advanced Security) Acceptance Criteria

- [ ] TOTP enrollment and verification work end-to-end, including recovery from a lost device (documented process, even if manual at first).
- [ ] A rotated refresh token cannot be reused (verified by an automated test, per `10-THREAT-MODEL.md`).
- [ ] A user can view and revoke their own active sessions, and a revoked session is immediately unusable.

## Phase 4 (Enterprise) Acceptance Criteria

- [ ] A SAML-only legacy application can complete a full SP-initiated login.
- [ ] A Project Grant restricts the receiving organization to exactly the granted roles — an attempt to assign a non-granted role is rejected with a clear error.
- [ ] Revoking a Project Grant immediately removes access for all users who held roles through it.

## Phase 4b (ABAC, if undertaken) Acceptance Criteria

- [ ] A policy in `draft` status can be dry-run against sample input without affecting real authorization decisions.
- [ ] Activating a policy creates a new version, and reverting is a single action.
- [ ] `/v1/authz/check` with resource/context attributes returns a `reasons` field explaining the decision.

## Phase 5 (Hardening) Acceptance Criteria

- [ ] External pentest findings rated critical/high are all remediated or have a documented, accepted-risk sign-off in `18-RISK-REGISTER.md`.
- [ ] Load test results meet every target in `12-PERFORMANCE.md`.
- [ ] A disaster recovery drill has been executed successfully within the last 12 months, per `15-DISASTER-RECOVERY.md`.
- [ ] The console passes a WCAG 2.1 AA accessibility audit — see `UI-UX/17-UX-ACCEPTANCE-CRITERIA.md` for the detailed checklist.

## General Definition of Done (Applies to Every Feature, Any Phase)

A feature is not "done" until:
1. It has automated test coverage per `11-TESTING.md`.
2. It's reflected in the OpenAPI spec (if API-facing) per `05-API-CONTRACT.md`.
3. Any new sensitive action is captured in the audit log per `04-DATA-MODEL.md`/`09-SECURITY.md`.
4. Any new trust boundary or attack surface has been considered against `10-THREAT-MODEL.md`.

Continue to [18 — Risk Register](./18-RISK-REGISTER.md).
