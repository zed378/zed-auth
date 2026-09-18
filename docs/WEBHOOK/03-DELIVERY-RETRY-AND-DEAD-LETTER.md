# 03 - Delivery, Retry, and Dead-Letter Handling

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: `P4-12` &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Specify the retry, backoff, disablement, and observability behavior a future webhook delivery engine must provide, since no delivery mechanism exists today.

## Why It Is Not Built Yet

No queue, worker, retry logic, or delivery log exists in this codebase (`00-WEBHOOK-OVERVIEW.md`). `webhook_deliveries` is a table name that appears only in `docs/PLAN/04-DATA-MODEL.md`'s design intent, not in any migration.

## Constraints Already Decided

From `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § `P4-12` and the threat review's amendments:

- **Delivery must never block the originating request** (`P4-12` step 5) — the action that triggers an event (a role assignment, a login) completes and responds regardless of webhook delivery outcome; delivery is queued asynchronously.
- **Retries are bounded, with exponential backoff, and a sustained-failure endpoint is auto-disabled with a notification to the organization** (`P4-12` step 4).
- **Per-endpoint rate limiting** prevents a webhook storm — many events queued at once — from amplifying an incident by hammering a receiver or, per the SSRF concerns in `00-WEBHOOK-OVERVIEW.md`, an internal target (`P4-12` step 8).
- **The delivery log is visible to the organization's own admins** so they can debug their own integration without opening a support ticket (`P4-12` step 7) — but it must record status and latency **only**, never the response body or headers, because storing the body turns a blind SSRF into a readable one (threat review T4-11).
- **Alert on the age of the oldest undelivered event, not on failure counts.** Threat review T4-12, directly: *"Consumers will deprovision from these events. A delivery worker that stops is `BL-01` again: no errors, only an absence."* A stalled worker produces zero failures and zero deliveries — a failure-count alert never fires, and the only detectable signal is that the oldest queued event keeps getting older. This mirrors a known failure pattern already documented for this project's own backup tooling: an absence of successful activity, not a count of failures, is what must be alarmed on.
- **A stale, out-of-order retry must not silently restore access.** Threat review T4-12's example: an endpoint receives `role.assigned`, then later `role.revoked`; if a retry of the earlier `role.assigned` arrives after the `role.revoked` delivery (network reordering, a retried attempt that finally succeeds late), a consumer that applies deliveries in arrival order re-grants access it should not have. The fix belongs to the payload, not the retry logic: a per-endpoint sequence number (`01-EVENT-TYPES-AND-PAYLOADS.md`) lets a receiver detect and discard a delivery older than the last one it applied.
- **A webhook subscription is itself a form of persistence that must be considered during account compromise.** Threat review T4-12: *"A compromised `ORG_ADMIN` who registers a `user.login.success` endpoint keeps receiving events after their password is reset and their sessions are revoked."* Endpoint registration/rotation therefore needs the elevated controls specified in `02-HMAC-SIGNATURE-VERIFICATION.md` (`ORG_OWNER`, recent sign-in). `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md` does not currently have a playbook step for revoking webhook endpoints — its "Credential Stuffing Attack Detected" playbook covers only forced password reset and session revocation. Per T4-12's ask, that gap needs to be closed (either by extending that playbook or adding a new one) so an incident responder knows to check and revoke webhook subscriptions, not only sessions and tokens.

## Key Topics To Specify

- The exact backoff schedule (attempt count, delay progression, and success/timeout thresholds) — not yet decided; earlier scaffold content in this file specified a five-attempt, 24-hour schedule with no citation to any plan or code, and that content has been removed because it was never verified against anything real.
- What "sustained failure" means precisely (a consecutive-failure count, a time window, or both) before an endpoint is auto-disabled.
- The notification channel and content when an endpoint is disabled or when the oldest-undelivered-event alert fires.
- Whether a disabled endpoint's queued deliveries are discarded, retained for manual replay, or something else.
- The specific dead-letter destination (a queryable table the organization's admins can see, matching `P4-12` step 7) and its retention period, distinct from `webhook_deliveries`'s general debugging-aid retention already noted in `docs/PLAN/04-DATA-MODEL.md`.

## Acceptance Criteria

- [ ] A load or fault-injection test proves the originating request's latency and success are unaffected by a slow, failing, or unreachable webhook receiver.
- [ ] Retries are bounded and follow a documented, specified backoff — the schedule itself must be decided and cited in a revised version of this document (or its owning spec) before implementation, not invented at code-review time.
- [ ] An endpoint that fails past the documented threshold is disabled automatically, and a notification is sent to the organization; both are covered by tests.
- [ ] Per-endpoint rate limiting is enforced and tested against a burst of queued events.
- [ ] The delivery log a test reads back never contains a response body or header, only status and latency.
- [ ] An alert fires when the oldest undelivered event exceeds a documented age threshold, verified by a test that seeds a stalled queue and confirms the alert condition — not only a test of failure-count-based alerting.
- [ ] A per-endpoint sequence number is included in every delivery, and a documented reference example shows a receiver correctly discarding an out-of-order retry.
- [ ] `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md` is updated (extending "Credential Stuffing Attack Detected" or adding a new playbook) to include revoking webhook endpoint registrations as an explicit response step — closing the gap noted above, per threat review T4-12.

## Open Questions

- The exact backoff schedule and disablement threshold — undecided; must be fixed in the `P4-12` spec before implementation, not assumed here.
- Whether disabled-endpoint deliveries are retained for manual replay after an administrator fixes the receiver, or discarded.

## Related Documents

- `docs/WEBHOOK/00-WEBHOOK-OVERVIEW.md`, `01-EVENT-TYPES-AND-PAYLOADS.md`, `02-HMAC-SIGNATURE-VERIFICATION.md`
- `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § `P4-12`
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` §§ T4-11, T4-12
- `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md`
- `docs/PLAN/04-DATA-MODEL.md` § `webhook_deliveries`
