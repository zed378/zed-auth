# Category: SECURITY

The threat model. Assets, trust boundaries, who attacks them, how, and what stops it. These six documents are reference material, written before and alongside implementation, and amended deliberately (`.github/CODEOWNERS`).

`02-ATTACK-SURFACE-AND-SCENARIOS.md` is mandatory reading for any change to authentication, authorization or sessions: every task card in `../../TASKS/` names the abuse cases it must be tested against, and they come from there.

## Documents

| Document | Covers |
|---|---|
| [`00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md`](./00-ASSET-AND-TRUST-BOUNDARY-INVENTORY.md) | What is worth protecting, and the boundaries (TB-x) it sits behind |
| [`01-THREAT-ACTOR-PROFILES.md`](./01-THREAT-ACTOR-PROFILES.md) | Who the adversaries are, their capability and motivation |
| [`02-ATTACK-SURFACE-AND-SCENARIOS.md`](./02-ATTACK-SURFACE-AND-SCENARIOS.md) | Scenario by scenario: asset → boundary → actor → surface → impact → mitigation → detection → response |
| [`03-DETECTION-AND-MONITORING.md`](./03-DETECTION-AND-MONITORING.md) | What must be detectable, and the signals that make it so |
| [`04-INCIDENT-RESPONSE-PLAYBOOKS.md`](./04-INCIDENT-RESPONSE-PLAYBOOKS.md) | Step-by-step response per incident class |
| [`05-VERIFICATION-AND-REDTEAM-PLAN.md`](./05-VERIFICATION-AND-REDTEAM-PLAN.md) | How the controls are verified, including authorized red-team scope |

## Where the implemented controls are described

The threat model says what must be true. These describe what the code does about it:

- [`../AUTHORIZATION/`](../AUTHORIZATION/) — the authorization model as built.
- [`../MULTI-TENANCY/`](../MULTI-TENANCY/) — tenant isolation and row-level security.
- [`../SESSION-MANAGEMENT/`](../SESSION-MANAGEMENT/) — tokens, rotation, revocation.
- [`../IDENTITY-PROTOCOL/`](../IDENTITY-PROTOCOL/) — OIDC/OAuth, MFA, passkeys.
- [`../TESTING/04-ABUSE-CASE-SECURITY-TESTING.md`](../TESTING/04-ABUSE-CASE-SECURITY-TESTING.md) — the abuse-case tests and the coverage map that keeps them honest.
- [`../../MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md`](../../MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md) — the Phase 4 threat review (T4-1 … T4-14) and its follow-up.
