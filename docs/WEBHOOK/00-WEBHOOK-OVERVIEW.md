# 00 - Webhook Overview

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: `P4-12` &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State plainly that outbound webhooks do not exist in this system today, what an integrator should do instead to react to events, and specify what the future webhook delivery engine (`P4-12`) must provide before it can ship.

## Why It Is Not Built Yet

Confirmed absent, not merely undocumented:

- **No webhook tables exist.** `backend/migrations/*.up.sql` contains no `webhook_endpoints` or `webhook_deliveries` table (searched all 37 migrations).
- **No webhook paths exist in the API.** `openapi/openapi.yaml` has no `/v1/webhooks*` operation, and `scripts/openapi-shipped-paths.py`'s `SHIPPED` set — the canonical list of endpoints that have actually shipped — contains no webhook path. `docs/API/00-API-OVERVIEW.md` already states this directly: *"Webhooks for platform events — `P4-12` (`TODO`). No `/v1/webhooks*` namespace exists."*
- **No route permission entries exist.** `docs/AUTHORIZATION/05-ROUTE-PERMISSION-TABLE.md`: *"Routes for SAML, social login, webhooks and SCIM do not exist yet; they will need entries here when they do."*
- **The owning task is `TODO`.** `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § `P4-12` — status `TODO`, depends on `P0-12` (the audit event writer, done), no spec written yet (`MEMORY/specs/` has no `P4-12` entry).
- **The design has already been reviewed and found incomplete before any code was written.** `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` §§ T4-11, T4-12 reviewed `P4-12` against the code it would have to change and found problems serious enough to require amending the card before work starts (see Constraints Already Decided).

## What To Do Today

There is no push mechanism. To react to something that happened in this system, an integrator must **poll the audit log**:

- `GET /v1/organizations/{org_id}/events` (`operationId: listEvents`, requires `ORG_ADMIN` over the organization) — the one read-only route onto the append-only `events` table, documented in `docs/API/15-AUDIT-LOG-API.md`. It supports filtering by `event_type` (repeatable), `actor_id`, and a `from`/`to` time window, ordered newest first.
- This is pull-based and requires an `ORG_ADMIN` token; it is not a substitute for push delivery, and payloads are audit-shaped (redacted for an internal investigator), not integration-shaped (see `01-EVENT-TYPES-AND-PAYLOADS.md`).

## Constraints Already Decided

`P4-12`'s own steps, plus the threat review's amendments (which the review explicitly says must change the card before work starts):

| Constraint | Source |
|---|---|
| SSRF defense is the central concern, not an afterthought | `P4-12` step 2, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §7 |
| Check the **connected** IP in `net.Dialer.Control`, not a pre-flight DNS check — a separate check-then-connect step is a TOCTOU gap that DNS rebinding exploits | Threat review T4-11 (amends step 2's wording) |
| No redirects followed; no `HTTP_PROXY`/environment proxy honored | Threat review T4-11 |
| `https` on port 443 only outside development | Threat review T4-11 |
| Deny loopback, private, link-local, CGNAT, IPv4-mapped IPv6, single-label hostnames, and this deployment's own issuer and console hostnames (a webhook target pointing at the service's own public hostname passes every private-range filter and loops back in through the tunnel) | Threat review T4-11 |
| Bounded connect, read, and total time per delivery attempt | `P4-12` step 2; threat review T4-11 |
| The delivery log records status and latency **only**, never the response body or headers — storing the body turns a blind SSRF into a read | Threat review T4-11 |
| The SSRF test suite must run against a target that would actually answer (e.g. `http://mailpit:8025/api/v1/messages` on the compose network) — a suite that only proves refusals against addresses nothing answers on proves nothing | Threat review T4-11 |
| Delivery is asynchronous and must never block the originating request | `P4-12` step 5 |
| Retries are bounded with exponential backoff; a sustained-failure endpoint is auto-disabled and the organization notified | `P4-12` step 4 |
| Rate-limited per endpoint, so a webhook storm cannot amplify an incident | `P4-12` step 8 |
| No token, password, or raw resource attribute ever appears in a payload | `P4-12` step 6, `CLAUDE.md` non-negotiable constraint |
| Registering or changing an endpoint requires `ORG_OWNER` and a recent sign-in, is audited at elevated visibility, and notifies the organization's owners | Threat review T4-12 |
| Alert on the **age of the oldest undelivered event**, not on failure counts — a stopped delivery worker produces silence, not errors | Threat review T4-12 |

## Key Topics To Specify

- The full data model amendment `docs/PLAN/04` needs before `P4-12` can proceed — see `02-HMAC-SIGNATURE-VERIFICATION.md` for the specific `secret_hash` problem.
- Per-event-type payload shape and an allowlist of fields, distinct from the internal audit row (see `01-EVENT-TYPES-AND-PAYLOADS.md`).
- Which organization's endpoints receive an event that spans two tenants (a delegated user's login, per Project Grants) — threat review T4-12 flags this as undecided.
- The event-name mismatch between `docs/PLAN/05-API-CONTRACT.md` (`login.success`, `login.failed`, `user.deleted`) and the audit vocabulary actually emitted by `backend/internal/audit/audit.go` (`user.login.success`, `user.login.failed`, `user.deactivated`) — see `01-EVENT-TYPES-AND-PAYLOADS.md`.
- Sequencing: a per-endpoint sequence number so a consumer can discard a stale, out-of-order retry (threat review T4-12's concern that a `role.assigned` retried after a later `role.revoked` would restore access at the consumer).

## Acceptance Criteria

- [ ] `webhook_endpoints` and `webhook_deliveries` tables exist per an updated `docs/PLAN/04-DATA-MODEL.md` that has been through the plan-change process referenced by the threat review (not implemented against the current `secret_hash` design as-is — see `02-HMAC-SIGNATURE-VERIFICATION.md`).
- [ ] `/v1/organizations/{org_id}/webhook-endpoints` (or equivalent path, to be fixed in the spec) appears in `openapi/openapi.yaml` and in `scripts/openapi-shipped-paths.py`'s `SHIPPED` set only once it actually returns something other than 404.
- [ ] SSRF defenses check the connected IP at dial time (`net.Dialer.Control`), not a separate pre-check; disable redirects and environment proxies; and are proven by a test suite that runs against a real in-network target and a DNS-rebinding attempt, per threat review T4-11.
- [ ] The delivery log never persists a response body or header.
- [ ] Delivery is asynchronous and a load test confirms the originating request's latency is unaffected by delivery failures or slow receivers.
- [ ] A failing endpoint is auto-disabled after a bounded number of attempts, and the organization is notified.
- [ ] Endpoint registration and modification require `ORG_OWNER` with a recent sign-in and are audited at elevated visibility. `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md` currently has no playbook step for revoking webhook endpoints — its closest existing playbook, "Credential Stuffing Attack Detected," covers password reset and session revocation only. Per threat review T4-12, that playbook (or a new one) must be extended to include revoking webhook endpoint registrations during an account-compromise response, with a test or runbook check confirming the step exists before this criterion is considered met.
- [ ] Alerting is based on the age of the oldest undelivered event, with a test that seeds a stalled queue and confirms the alert fires — not solely on delivery failure counts.
- [ ] An abuse-case test suite exists for every scenario `P4-12`'s "Abuse cases to test" lists: SSRF to internal services, DNS rebinding between validation and request, cross-tenant data exfiltration via payload content, and amplification via a self-triggering webhook.

## Open Questions

- Everything listed under Key Topics To Specify above is unresolved and blocks implementation, per the threat review's own assessment ("Yes" in the summary table for both T4-11 and T4-12 under "Change a card before work starts?").
- Whether `P4-12` proceeds before or after `P4-13` (SCIM) is undecided; both are Phase 4, `TODO`, and Phase 4 is currently 4/16 done.

## Related Documents

- `TASKS/PHASE-4-ENTERPRISE-INTEROP.md` § `P4-12`
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` §§ T4-11, T4-12
- `docs/API/00-API-OVERVIEW.md`, `docs/API/15-AUDIT-LOG-API.md`
- `docs/AUTHORIZATION/05-ROUTE-PERMISSION-TABLE.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §7
- `docs/PLAN/04-DATA-MODEL.md` §§ `webhook_endpoints`, `webhook_deliveries`, `events`
- `docs/PLAN/05-API-CONTRACT.md` § Rate Limiting, Idempotency, Webhooks
- `docs/WEBHOOK/01-EVENT-TYPES-AND-PAYLOADS.md`, `02-HMAC-SIGNATURE-VERIFICATION.md`, `03-DELIVERY-RETRY-AND-DEAD-LETTER.md`
