# WEBHOOK

This category specifies outbound webhook event delivery — a Phase 4 feature (`P4-12`) that is **not built**. No webhook table, endpoint, or delivery mechanism exists in this codebase today; `docs/API/00-API-OVERVIEW.md` and `docs/AUTHORIZATION/05-ROUTE-PERMISSION-TABLE.md` already say so directly. Every document in this category states plainly what does not exist, what to do today instead (poll the audit log via `GET /v1/organizations/{org_id}/events`, documented in `docs/API/15-AUDIT-LOG-API.md`), and then specifies testable acceptance criteria for the eventual feature — grounded in `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § `P4-12` and the Phase 4 threat review (`MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md`), which found the feature's own design intent (`docs/PLAN/04-DATA-MODEL.md`, `docs/PLAN/05-API-CONTRACT.md`) incomplete before any code was written. This category does not restate or override those plan documents; where the threat review asks for a change to them, this category says so and cites the finding rather than silently deciding.

## Documents

| File | Topic | Status |
|---|---|---|
| [`00-WEBHOOK-OVERVIEW.md`](./00-WEBHOOK-OVERVIEW.md) | What doesn't exist, what to do today, and `P4-12`'s constraints and acceptance criteria | Draft specification |
| [`01-EVENT-TYPES-AND-PAYLOADS.md`](./01-EVENT-TYPES-AND-PAYLOADS.md) | The audit event catalogue as candidate webhook events; not delivered by webhook today | Draft specification |
| [`02-HMAC-SIGNATURE-VERIFICATION.md`](./02-HMAC-SIGNATURE-VERIFICATION.md) | Payload signing; a known data-model defect (`secret_hash` cannot sign) that must be fixed first | Draft specification |
| [`03-DELIVERY-RETRY-AND-DEAD-LETTER.md`](./03-DELIVERY-RETRY-AND-DEAD-LETTER.md) | Retry, backoff, disablement, and alerting requirements | Draft specification |

## Related Documents

- `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § `P4-12` — the owning task, status `TODO`.
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` §§ T4-11, T4-12 — the pre-implementation security review this category is built around.
- `docs/API/00-API-OVERVIEW.md`, `docs/API/15-AUDIT-LOG-API.md` — what exists today for reading events (pull, not push).
- `docs/PLAN/04-DATA-MODEL.md`, `docs/PLAN/05-API-CONTRACT.md` — design intent; not to be edited here, and where the threat review finds a gap in them, this category names it rather than resolving it unilaterally.
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §7 (SSRF) — the central risk in any future implementation.
- `backend/internal/audit/audit.go` — the actual event vocabulary.
