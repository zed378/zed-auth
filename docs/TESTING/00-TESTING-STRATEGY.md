# 00 - Testing Strategy

> Category: **Testing** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-15, P1-27, P1-28, P2-16, P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State what is tested, at which layer, and what each layer is trusted to prove. `docs/PLAN/11-TESTING.md` is the intent; this describes the suites that exist.

## Scope

The strategy and the vocabulary. Each layer has its own document; the CI gates are `docs/DEVOPS/03-CICD-PIPELINE-SPECIFICATION.md`.

## As Built

| Layer | Runs against | Proves | Where |
|---|---|---|---|
| Unit (Go) | Pure functions | Decisions, parsing, hashing, policy resolution | `backend/internal/**/*_test.go` |
| Integration (Go, `integration` tag) | Real Postgres and Redis via testcontainers | Handlers, SQL, triggers, RLS, the whole `/v1` chain | `backend/internal/**/*_integration_test.go` |
| Security | Real database, several tenants | Isolation and abuse cases, with a coverage map | `backend/tests/security/` |
| Component (console) | jsdom + Testing Library + axe | Screens, states, accessibility | `console/src/**/*.test.tsx` |
| End-to-end | A real service, a real browser | Sign-in, SSO, console flows, contract adherence | `console/e2e/*.spec.ts` |
| Load | A running stack | Latency against `docs/PLAN/12` targets | `scripts/loadtest/` |
| Acceptance | A running stack (staging) | A phase's documented criteria | `scripts/acceptance-phase*.sh` |

### Two conventions that matter more than the pyramid

1. **Every security-relevant feature has an abuse-case test.** The card names the abuse cases from `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`; the tests are named after them and indexed by a coverage map (`04-ABUSE-CASE-SECURITY-TESTING.md`).
2. **Every control is proved to be load-bearing** by breaking it and watching a specific test fail (`05-...`). A passing suite says nothing about whether it would notice the control's absence.

### What the layers are deliberately not asked to prove

- Unit tests do not touch SQL: triggers and RLS are database behaviour, so they are tested through a real database, including from the owner connection where a rule must hold for every writer.
- Component tests do not prove the API contract; the E2E suite asserts console behaviour **and** the API's view of the same fact.
- E2E tests are not a substitute for integration tests: they are few, slow, and chosen for the properties only a real browser and a real service can show.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Integration tests behind a build tag | `integration` | package files; `go test ./...` stays fast |
| A security feature without an abuse-case test | Not `DONE` | `CLAUDE.md`, card DoD |
| Coverage | Measured each gate run; failing tests printed | `scripts/check-coverage.sh` |
| Flakes | Fixed, not retried — timing tests were reworked rather than quarantined | `MEMORY/records/` (P3-14) |

## Verification

The suites verify the system; the gate verifies the suites run (`scripts/check.sh`), and the coverage map verifies that the security tests still exist under the names the map claims (`backend/tests/security/coveragemap_test.go`).

## Not Yet Built / Open Questions

- No fuzzing job yet (planned with `P4-15`).
- No mutation-testing tool; the mutation step is a manual convention.
- The race detector runs only in CI, because cgo is unavailable on the development host.

## Related Documents

- `docs/PLAN/11-TESTING.md`; `docs/DEVOPS/03-CICD-PIPELINE-SPECIFICATION.md`; `docs/SECURITY/05-VERIFICATION-AND-REDTEAM-PLAN.md`.
