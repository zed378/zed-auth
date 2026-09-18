# 01 - Event Types & Payloads

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: `P4-12` &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Present the existing audit event catalogue (`backend/internal/audit/audit.go`) as the realistic candidate list for a future webhook subscription taxonomy, and state clearly, without qualification, that **none of these events are delivered by webhook today** — the only way to read any of them is the audit log API.

## Why It Is Not Built Yet

Webhooks do not exist (`00-WEBHOOK-OVERVIEW.md`). What does exist is the audit event system built for `P0-12`, which already assigns a stable `EventType` string to every identity- or permission-changing action in the system. That vocabulary is the natural starting point for a webhook taxonomy, but it was designed for a different audience and a different guarantee:

> `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` § T4-12: *"Payloads are not the audit row. `webhook_deliveries.event_id` points at `events`, which invites sending the row as the payload. `P1-14` built `user.login.failed` for operators: IP address, user agent, reason. A third-party endpoint is a different audience."*

The same review also found that `docs/PLAN/05-API-CONTRACT.md`'s proposed webhook event names do not match what the code actually emits — see the table below.

## How To Read Today's Events

There is no subscription mechanism. To see any of these events, call:

```
GET /v1/organizations/{org_id}/events
```

`operationId: listEvents`, requires `ORG_ADMIN` over the organization, returns pages newest-first, and supports `event_type` (repeatable), `actor_id`, and `from`/`to` filters. Full detail: `docs/API/15-AUDIT-LOG-API.md`. Payloads are redacted **before storage** (`backend/internal/audit/audit.go` `redactPayload`) using the same rules as the structured logger, so nothing returned ever carries a token, password, or raw `/v1/authz/check` resource attribute — but the payload is still shaped for an operator reading an incident timeline, not for a third-party integration (see Constraints below).

## Candidate Event Catalogue

Every constant below is defined in `backend/internal/audit/audit.go` today and is written to the `events` table on the corresponding action. None is currently delivered by webhook. The "Plan name (if different)" column shows where `docs/PLAN/05-API-CONTRACT.md`'s webhook list already disagrees with the code — flagged here rather than silently resolved, per the threat review's finding.

| Category | `EventType` constant | Value written to `events.event_type` | Plan name (if different) |
|---|---|---|---|
| Authentication | `EventLoginSucceeded` | `user.login.success` | plan says `login.success` |
| | `EventLoginFailed` | `user.login.failed` | plan says `login.failed` |
| | `EventLogout` | `user.logout` | |
| | `EventUserLockedOut` | `user.lockout` | |
| Session | `EventSessionCreated` | `session.created` | |
| | `EventSessionRevoked` | `session.revoked` | |
| MFA | `EventMFASucceeded` | `user.mfa.success` | |
| | `EventMFAFailed` | `user.mfa.failed` | |
| | `EventMFAChallenged` | `user.mfa.challenged` | |
| | `EventMFARecoveryUsed` | `user.mfa.recovery_used` | |
| | `EventMFACodesIssued` | `user.mfa.codes_generated` | |
| | `EventMFAResetByAdmin` | `user.mfa.reset_by_admin` | |
| | `EventMFAMandateEnabled` | `organization.mfa_required.enabled` | |
| | `EventMFAMandateDisabled` | `organization.mfa_required.disabled` | |
| | `EventMFAEnrolmentForced` | `user.mfa.enrolment_forced` | |
| | `EventMFAEnrolled` | `user.mfa.enrolled` | |
| | `EventMFAEnrolmentStarted` | `user.mfa.enrolment_started` | |
| | `EventMFARemoved` | `user.mfa.removed` | |
| Anomaly | `EventLoginAnomaly` | `user.login.anomaly` | |
| | `EventLoginReportedNotMe` | `user.login.reported_not_me` | |
| Tokens | `EventTokenIssued` | `token.issued` | |
| | `EventTokenRevoked` | `token.revoked` | |
| | `EventRefreshReuseDetected` | `token.refresh.reuse_detected` | |
| | `EventTokenReuse` | `token.reuse_detected` | |
| User lifecycle | `EventUserCreated` | `user.created` | |
| | `EventUserUpdated` | `user.updated` | |
| | `EventUserDeactivated` | `user.deactivated` | plan says `user.deleted` — there is no hard-delete event; see note below |
| | `EventUserInvited` | `user.invited` | |
| | `EventUserReactivated` | `user.reactivated` | |
| | `EventUserInviteAccepted` | `user.invite_accepted` | |
| Password | `EventPasswordChanged` | `user.password.changed` | |
| | `EventPasswordResetSent` | `user.password.reset_requested` | |
| | `EventPasswordRejected` | `user.password.rejected` | |
| | `EventPasswordBreachCheckSkipped` | `user.password.breach_check_skipped` | |
| Roles | `EventRoleCreated` | `role.created` | |
| | `EventRoleUpdated` | `role.updated` | |
| | `EventRoleDeleted` | `role.deleted` | |
| | `EventRoleAssigned` | `role.assigned` | |
| | `EventRoleRevoked` | `role.revoked` | |
| Project Grants | `EventProjectGrantCreated` | `project_grant.created` | |
| | `EventProjectGrantRevoked` | `project_grant.revoked` | |
| | `EventDelegatedRoleAssigned` | `delegated_role.assigned` | |
| | `EventDelegatedRoleReplaced` | `delegated_role.replaced` | |
| | `EventDelegatedRoleRemoved` | `delegated_role.removed` | |
| | `EventManagerRoleAssigned` | `manager_role.assigned` | |
| | `EventManagerRoleRevoked` | `manager_role.revoked` | |
| Organization | `EventOrganizationCreated` | `organization.created` | |
| | `EventOrganizationSuspended` | `organization.suspended` | |
| | `EventOrganizationUpdated` | `organization.updated` | plan says `org.updated` |
| | `EventOrganizationReactivated` | `organization.reactivated` | |
| | `EventOrganizationDeleted` | `organization.deleted` | |
| Policy | `EventPolicyUpdated` | `policy.updated` | |
| | `EventPolicyActivated` | `policy.activated` | |
| Project | `EventProjectCreated` | `project.created` | |
| | `EventProjectUpdated` | `project.updated` | |
| | `EventProjectDeleted` | `project.deleted` | |
| Application | `EventApplicationCreated` | `application.created` | |
| | `EventApplicationSecretRotated` | `application.secret_rotated` | |
| | `EventApplicationDeleted` | `application.deleted` | |
| | `EventApplicationUpdated` | `application.updated` | |
| Signing | `EventSigningKeyRotated` | `signing_key.rotated` | |
| Instance | `EventInstanceScopedAccess` | `instance.scoped_access` | |

**Note on `user.deleted`:** `docs/PLAN/05-API-CONTRACT.md` names `user.deleted` as a webhook event, but no such audit event exists. Users are deactivated, never hard-deleted (`docs/PLAN/04-DATA-MODEL.md`: *"Deactivation, not deletion. A deleted user makes every audit entry naming them unresolvable"*), so the closest real analog is `user.deactivated`. A future webhook taxonomy should use the name that matches an event the system actually emits, not the plan's provisional name.

## Constraints Already Decided

- **A webhook payload must be built from a per-event-type allowlist, not the audit row.** Threat review T4-12's direct instruction: *"Build payloads from a per-event-type allowlist and reject unknown `event_types` on write."* The audit payload for `user.login.failed` (IP address, user agent, reason — `P1-14`) is shaped for an operator; a third-party endpoint gets a narrower, deliberately-chosen subset.
- **Payloads never carry a token, password, or raw `/v1/authz/check` resource attribute** (`CLAUDE.md` non-negotiable constraint; `P4-12` step 6) — the same rule the audit log's own redaction already enforces, applied independently at the webhook payload boundary rather than assumed to carry over.
- **Each payload needs a per-endpoint sequence number** so a receiver can discard a stale, out-of-order retry — threat review T4-12's example: a `role.assigned` retried after a later `role.revoked` must not restore access at the consumer if the consumer checks the sequence.
- **Delegation creates two audiences for one event.** A Project Grant delegated user's login at organization A's application is an event both the granting and the receiving organization may have reason to subscribe to; the spec must decide who receives what, and a test must show the non-subscribed organization receives nothing (threat review T4-12).

## Acceptance Criteria

- [ ] A published webhook event taxonomy uses names that match audit events the code actually emits (or is accompanied by a mapping table), closing the `login.success`/`login.failed`/`user.deleted` mismatches identified above.
- [ ] Every webhook payload is built from an explicit per-event-type allowlist; a test attempting to subscribe to an unknown `event_type` at write time is rejected.
- [ ] No payload contains a token, password, or raw resource attribute — verified by a redaction test independent of the audit log's own redaction test.
- [ ] Every payload carries a per-endpoint monotonic sequence number.
- [ ] A test proves that for a delegated-user event, only the organizations entitled to see it (per the finished spec) receive a delivery.

## Open Questions

- The full target list of event types for the first webhook release — the roadmap does not scope this to a subset; whether all ~55 audit event types above become subscribable or only a curated subset is undecided.
- Payload versioning: whether a payload schema version is included so a consumer can detect a future field addition or shape change.

## Related Documents

- `backend/internal/audit/audit.go`
- `docs/API/15-AUDIT-LOG-API.md`, `docs/API/00-API-OVERVIEW.md`
- `docs/PLAN/04-DATA-MODEL.md` § `events`, § "What Is Deliberately Not Stored Here"
- `docs/PLAN/05-API-CONTRACT.md` § Rate Limiting, Idempotency, Webhooks
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` § T4-12
- `docs/WEBHOOK/00-WEBHOOK-OVERVIEW.md`
