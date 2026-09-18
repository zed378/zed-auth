# 02 - HMAC Signature Verification

> Category: **WEBHOOK** (`docs/WEBHOOK/`) &nbsp;|&nbsp; Status: Draft specification &nbsp;|&nbsp; Owner task: `P4-12` &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Specify what payload signing and verification a future webhook system must provide, and record a specific, already-identified data-model defect that must be fixed before signing can work at all.

## Why It Is Not Built Yet

No webhook endpoint, secret, or signature exists anywhere in this system (`00-WEBHOOK-OVERVIEW.md`). There is nothing to verify today.

## Constraints Already Decided

**The current planned data model cannot sign anything, and this is a known, recorded defect — not a detail left for implementation.** `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` § T4-12, verbatim:

> "The data model cannot sign. `docs/PLAN/04` stores `webhook_endpoints.secret_hash`, but HMAC signing needs the key itself at send time. Someone following the model either cannot sign, or stores the plaintext in a column named `_hash`. It needs to be sealed the way `P3-02` seals factor secrets."

Confirmed in `docs/PLAN/04-DATA-MODEL.md` § `webhook_endpoints`: the column is named `secret_hash`, documented as "per-endpoint signing secret; receivers verify payload authenticity with it." A one-way hash cannot be used to compute an HMAC at delivery time — HMAC-SHA256 needs the key itself, not a hash of it. This is a genuine gap between the plan document and what the feature requires, and per `CLAUDE.md`'s instruction, it is recorded here rather than silently resolved: **the threat review's ask is to route this through the plan-change process** (amend `docs/PLAN/04`), not to have an implementer quietly rename or reinterpret the column.

**The precedent for the fix already exists in this codebase.** `backend/internal/mfa/seal.go` (`P3-02` step 1) encrypts TOTP factor secrets at rest — column `secret_encrypted`, AES-256-GCM, key from the deployment's secret resolver, authenticated so a tampered ciphertext fails rather than silently verifying nothing, and the encryption function **refuses to fall back to plaintext** if no key is configured (`ErrNoSealKey`) rather than silently storing an unencrypted secret. The threat review's instruction — "sealed the way `P3-02` seals factor secrets" — points at this exact mechanism as the model a webhook secret should follow: reversibly sealed (so the plaintext key is recoverable at send time), not one-way hashed.

Beyond the storage defect, the threat review specifies the signing scheme itself (§ T4-12):

| Constraint | Detail |
|---|---|
| Signed content | `id.timestamp.body` — the event id and timestamp are part of what is signed, not just the body |
| Secret rotation | Two active secrets accepted simultaneously during a rotation window, so rotating an endpoint's secret does not drop deliveries signed with the old one mid-rotation |
| Never logged | `CLAUDE.md`'s non-negotiable constraint — no webhook secret may appear in a log line, error message, or the delivery log's stored fields |
| Registration/rotation authorization | `ORG_OWNER` and a recent sign-in required to register or change an endpoint (which includes issuing or rotating its secret); audited at elevated visibility and notifies the organization's owners |

## Key Topics To Specify

- Exact header name and format for the signature (e.g. a timestamp and one or more versioned signature values, in the style of common webhook signing schemes, but a specific choice has not been made here).
- The replay-tolerance window: how much clock drift between sender and receiver is accepted before a valid signature is treated as an unacceptably old delivery.
- The sealing mechanism's exact parameters: whether it reuses `backend/internal/mfa/seal.go`'s AES-256-GCM implementation directly or a package-level equivalent, and how the sealing key is provisioned per `docs/DEVOPS/04-SECRET-MANAGEMENT.md`.
- Verification reference implementations/test vectors to publish for integrators, once the scheme is fixed — none exist today because the scheme itself is unbuilt.
- How the rotation overlap window is bounded (a maximum age for the old secret before it stops being accepted).

## Acceptance Criteria

- [ ] `docs/PLAN/04-DATA-MODEL.md`'s `webhook_endpoints` table has gone through the plan-change process and stores a reversibly sealed secret (not a one-way hash) before any signing code is written.
- [ ] The sealing mechanism matches or reuses the precedent in `backend/internal/mfa/seal.go`: authenticated encryption, a configured key required with no plaintext fallback, and a test proving `ErrNoSealKey`-equivalent behavior when no key is configured.
- [ ] Signature covers `id.timestamp.body` (or an equivalently unambiguous scheme decided in the eventual spec), verified by test vectors published alongside the spec.
- [ ] Two active secrets are accepted during a bounded rotation window; a test rotates a secret mid-delivery-queue and confirms no delivery is spuriously rejected.
- [ ] No test, log statement, error message, or the delivery log itself ever contains the plaintext secret — verified by a dedicated no-secret-in-logs test, consistent with the pattern already used for other sensitive values in this codebase.
- [ ] Registering or rotating an endpoint's secret requires `ORG_OWNER` and a recent sign-in, is audited at elevated visibility, and triggers an owner notification — each independently tested.

## Open Questions

- Whether `docs/PLAN/04`'s amendment renames `secret_hash` to something like `secret_sealed` or `secret_encrypted` (matching the MFA precedent's naming) — not yet decided, and blocked on the plan-change process the threat review calls for.
- The specific header name and signature format — no scheme has been chosen yet.

## Related Documents

- `docs/WEBHOOK/00-WEBHOOK-OVERVIEW.md`
- `MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md` § T4-12
- `docs/PLAN/04-DATA-MODEL.md` § `webhook_endpoints`
- `backend/internal/mfa/seal.go`
- `docs/DEVOPS/04-SECRET-MANAGEMENT.md`
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §16 (Secret Exposure)
