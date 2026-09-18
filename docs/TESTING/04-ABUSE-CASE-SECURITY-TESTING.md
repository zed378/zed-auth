# 04 - Abuse-Case and Security Testing

> Category: **Testing** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-08, P1-27, P2-16, P3-14, P4-01…P4-03 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how attacks are tested rather than assumed: where the abuse cases come from, how the tests are kept honest, and the practice of proving a control is load-bearing.

## Scope

`backend/tests/security/` and the abuse-case tests inside feature packages. The threat model itself is `docs/SECURITY/`.

## As Built

### Where an abuse case comes from

`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` enumerates scenarios per asset and trust boundary. Every task card names the ones its feature must be tested against, and the feature specification in `MEMORY/specs/` restates them as an `A-n` table. A card cannot be `DONE` without those tests (`CLAUDE.md`).

### The coverage map

`backend/tests/security/isolation_test.go` carries a map from abuse case to the tests that cover it, and `coveragemap_test.go` fails when a named test no longer exists. Without it, a renamed or deleted test leaves a map that claims coverage that is gone — the map went stale for three tasks once, which is why the check exists.

Cases still unimplementable are listed explicitly as "not yet testable — the feature does not exist", with the owning task. That list shrinks as features land: the `P4-02` row moved from that section to eight named tests.

### Cross-cutting security suites

- **Tenant isolation** — several organizations, and the property that a third sees nothing.
- **RLS behaviour** including query plans (`rls_plans_test.go`), so a policy cannot silently stop being applied.
- **Tenant resolution** and **multi-organization** flows.
- **Full-flow tests** that drive sign-in through to an authorization decision.

### Mutation verification

Every task's record reports it: each control is broken in turn — a subset check removed, a lock dropped, a filter widened, a trigger's condition falsified — and the suite must turn red, naming which test caught it. Recent examples: 8 mutations for `P4-02`, 6 for `P4-03`, 6 for the console pagination fix.

This is how the project's recurring defect class, "vacuous verification", is caught: a test that passes whether or not the control exists.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| A security feature without abuse-case tests | Not `DONE` | `CLAUDE.md`, card DoD |
| Named tests in the coverage map | Must exist | `backend/tests/security/coveragemap_test.go` |
| Database rules | Tested from the owner connection too | feature integration tests |
| Mutation results | Recorded in the task's `MEMORY/records/` entry | repository convention |

## Security Considerations

The suites deliberately include tests for controls that cannot yet be reached — for example, that delegated rows are invisible to the granting organization today. When `P4-04` makes them readable, that test is the thing that will force the reader-side join to land in the same change (threat review T4-2).

## Verification

- `go test -tags integration ./tests/security/...`
- `scripts/check.sh` § Security and § Integration.
- The Phase 4 threat review (`MEMORY/records/2026-09-15-P3-15-phase-4-threat-review.md`) lists T4-1…T4-14 with the tasks that must answer each.

## Not Yet Built / Open Questions

- No automated red-team or fuzzing job yet (`P4-15`, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`).
- No dependency vulnerability scanning beyond what the gate runs.

## Related Documents

- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`, `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`.
- `docs/AUTHORIZATION/`, `docs/MULTI-TENANCY/02-ROW-LEVEL-SECURITY-AND-TENANT-ISOLATION.md`.
