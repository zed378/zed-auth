# Plan Gap Remediation — Closing PG-01 through PG-11

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Task** | `TASKS/BACKLOG.md` § Plan Gaps (PG-01 … PG-11) |
| **Phase** | Pre-Phase-0 |
| **Surface** | docs (plan amendments) |
| **Author** | Claude Code, at the user's explicit instruction ("perbaiki dahulu celah dan kontradiksinya") |
| **Commits / PR** | Not yet committed — repository is not under version control (`P0-03`) |
| **Status** | Completed |

---

## What Changed

Eleven gaps and two internal contradictions in `PLAN/` were closed by amending the plan documents directly. `PLAN/04-DATA-MODEL.md` grew from 158 to 295 lines with six new tables, a retention policy, and a table stating what is deliberately *not* stored. `PLAN/05`, `PLAN/07`, and `PLAN/18` each received a targeted correction. A twelfth gap found during the work — `refresh_tokens` had no rotation-family tracking — was closed at the same time.

A thirteenth gap, found later while writing Phase F, was also closed: `UI-UX/08-PAGE-SPECIFICATIONS.md` claimed to be the "full screen inventory" but omitted four screens present in the IA.

## Why

`AGENTS.md` rule 9 makes `PLAN/`, `UI-UX/`, and `SECURITY/` reference documentation that must not change as a side effect of feature work — but it also says a plan change is legitimate when it is "a deliberate, separate action the user should be aware of." The user instructed this directly, which is exactly that action.

The alternative — recording implementation decisions in `MEMORY/DECISIONS.md` and leaving the plan documents unchanged — was available and rejected. It would have left `PLAN/04` describing a data model the system does not have, which defeats the purpose of a plan that `CLAUDE.md` tells every agent to treat as the source of truth. A source of truth that is known to be incomplete stops being consulted.

## How

Each gap was closed by writing the missing specification in the plan's own voice and structure, rather than appending a "gaps" appendix. Three principles governed the additions:

1. **Follow decisions the plan had already made elsewhere.** `signing_keys` holds a secret-manager *reference*, not key material, because `PLAN/02`'s constraint says no third party holds the private key. `user_identities` matches on the provider's stable subject rather than email, because `PLAN/05` already identifies email trust as the account-takeover path.
2. **Where the plan was genuinely silent, choose, state the reasoning, and mark anything needing business confirmation.** The 24-month audit retention is a working default, not a decision I can make — it is flagged as `OQ-09`.
3. **Resolve contradictions by weight of evidence, not by preference.** SAML's phase and the subset-validation timing were each decided by counting which documents said what, and the minority document was corrected.

### The two contradictions

**PG-08 — SAML phase.** `PLAN/05`'s standards table said Phase 2; `PLAN/03`, `PLAN/16`, and `PLAN/17` all said Phase 4. Corrected `PLAN/05` to Phase 4. Almost certainly a leftover from an earlier phase numbering.

**PG-09 — Subset validation timing.** `PLAN/18` R-04's mitigation read "server-side subset validation on every **grant creation**." `CLAUDE.md`, `AGENTS.md` rule 3, `PLAN/08` Part C, and `PLAN/19`'s worked example all require it **on every request**.

This one mattered more than a wording nit, and is the single most consequential edit in this change. The difference is exactly the attack: a grant that is narrowed or revoked after creation must stop working immediately, and R-04's phrasing described a system where it would not. Left uncorrected, the risk register would eventually have become the document someone cited to argue that creation-time validation was sufficient — and the risk register is precisely where a reviewer looks for the authoritative statement of a control.

### The twelfth gap, found while fixing the others

`refresh_tokens` as specified had `id`, `user_id`, `client_id`, `token_hash`, `expires_at`, and `revoked` — no way to identify a rotation lineage. Phase 3's reuse detection (`P3-06`, and `PLAN/17`'s Phase 3 acceptance criterion) depends entirely on being able to say "this token was already rotated, so its whole family is compromised." Added `family_id`, `replaced_by`, `session_id`, and `family_expires_at`, so Phase 3 changes behavior rather than storage shape.

### The thirteenth, found while writing Phase F

`UI-UX/08-PAGE-SPECIFICATIONS.md` presents itself as the "full screen inventory" and is cited as such throughout `TASKS/`. It was missing four screens that `UI-UX/03-INFORMATION-ARCHITECTURE.md` and `PLAN/06-FRONTEND-ARCHITECTURE.md` both include: Organization Overview (which `UI-UX/18` separately specs in detail — so the inventory omitted a screen the design plan elsewhere specifies fully), Organization Settings, Instance-wide policies, and Instance audit log. Added all four with their users, actions, and composing components.

## Files Touched

| Path | Change |
|---|---|
| `PLAN/04-DATA-MODEL.md` | +137 lines. Added `signing_keys`, `user_mfa_factors`, `user_recovery_codes`, `user_tokens`, `user_identities`, `webhook_endpoints`, `webhook_deliveries`. Extended `roles`, `sessions`, `refresh_tokens`, `users`, `events`. Added § Retention and Growth and § What Is Deliberately Not Stored Here. Updated the entity hierarchy and ER diagram |
| `PLAN/05-API-CONTRACT.md` | SAML 2.0: Phase 2 → Phase 4 |
| `PLAN/07-BACKEND-ARCHITECTURE.md` | Redis row now states it is a cache in front of PostgreSQL for sessions, not a second source of truth |
| `PLAN/18-RISK-REGISTER.md` | R-04 mitigation rewritten to "on every request, not only at grant-creation time" |
| `UI-UX/08-PAGE-SPECIFICATIONS.md` | Added four screens missing from the inventory |
| `TASKS/PHASE-0-FOUNDATION.md` | `P0-07` rewritten for the expanded schema, deferrable-table policy, partitioning, and the hash-only column list |
| `TASKS/PHASE-1/2/3/4-*.md` | Eight tasks now name the tables they operate on |
| `TASKS/BACKLOG.md` | Plan Gaps section replaced with the resolution record; `OQ-09` added |

## Decisions Made

| Decision | Rationale | ADR |
|---|---|---|
| Amend the plan rather than record implementation decisions around it | A plan known to be incomplete stops being consulted | [ADR-002](../DECISIONS.md) |
| `permission_keys` as an array column, not a join table | Matches `user_grants.role_keys` and `project_grants.granted_role_keys`; permission keys are the consumer app's vocabulary, not rows needing referential integrity | ADR-002 |
| One `user_tokens` table with a `purpose` discriminator | Invite, reset, and verification share identical security properties and identical abuse surface | ADR-002 |
| PostgreSQL authoritative for sessions, Redis a cache | Reconciles `PLAN/04` and `PLAN/07`; makes revocation immediate rather than TTL-bound, which `PLAN/17` Phase 3 requires | [ADR-003](../DECISIONS.md) |
| `events`: monthly partitioning, 24-month default retention, pseudonymize rather than delete for erasure | Partition pruning keeps the hot path fast; pseudonymization is the standard reconciliation of an immutable audit log with a right to erasure | [ADR-004](../DECISIONS.md) |
| Authorization codes in Redis with a sub-60-second TTL | No durability requirement; automatic expiry is stronger than a cleanup job that can fail silently | ADR-002 |

## Deviations from the Plan

This change *is* a plan change, made deliberately at the user's instruction under `AGENTS.md` rule 9's carve-out. Nothing was changed that the plan had already decided — every edit either filled a silence or corrected an internal inconsistency by following the majority of the plan's own documents.

No security control was weakened. `PLAN/18` R-04 was strengthened to match the four documents that already stated the stricter rule.

## Tests Added

None — documentation only. `P0-07`'s Definition of Done now requires an ERD generated from the live schema to match `PLAN/04`'s diagram, which is where these additions get verified against reality.

## Definition of Done Verification

- [x] All eleven original gaps closed, each traceable to a specific plan amendment
- [x] Both contradictions resolved by weight of evidence, with the minority document corrected
- [x] Two further gaps found during the work and closed
- [x] Downstream tasks updated so no task references a table that does not exist in the plan
- [x] `BACKLOG.md` records what changed and why
- [x] Anything needing business confirmation raised as an open question rather than silently decided (`OQ-09`)
- [x] `PLAN/`, `UI-UX/` edits were the deliberate, user-instructed action `AGENTS.md` rule 9 requires

## What Did Not Work

Two mechanical failures worth recording, both from trying to script the edits.

**Heredocs containing Markdown.** Passing content with backticks through `bash -c` let the shell interpret them as command substitution, producing dozens of "command not found" errors and a corrupted Python string. Backtick-heavy Markdown must go through the Write tool or a fully quoted heredoc, never an inline shell string.

**Renumbering an ordered list by inserting steps.** Inserting three steps into `P0-07` left the list numbered 1–6 then restarting at 4. Markdown renders it fine, which is exactly why it survived until a manual read caught it. Worth re-reading any list after inserting into the middle of one.

## Follow-Ups and Open Questions

- **OQ-09** (new): confirm the audit log retention period and the pseudonymization-over-deletion approach with whoever owns data protection. 24 months is a defensible default, not a researched obligation.
- Eight original open questions remain, unaffected by this change.
- `PLAN/04`'s ABAC tables (`user_attributes`, `policies`) were left as they were — they were already specified, and Phase 4b may never run.

## What to Watch

**The plan is now larger and therefore more able to disagree with itself.** Two contradictions existed across 49 documents; adding 137 lines to the most-referenced document raises the odds of a third. `P0-07`'s ERD-matching requirement is the one automated check that will catch drift between `PLAN/04` and reality — everything else relies on someone reading carefully.

**The 24-month retention default will get built before it gets confirmed.** `P0-07` sets up partitioning in Phase 0; the confirmation is a business conversation that may not happen until much later. Partitioning makes changing the retention window cheap, which is most of the mitigation, but the number will be in the migration long before anyone signs off on it.

**`signing_keys.private_key_ref` is the single most sensitive design decision in the new schema.** If anyone ever "simplifies" it by storing key material in the column, `PLAN/02`'s hard constraint is silently violated and nothing in CI would notice. It deserves an explicit test in `P1-03`.
