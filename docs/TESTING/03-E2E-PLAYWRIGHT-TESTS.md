# 03 - End-to-End Tests

> Category: **Testing** (`docs/TESTING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P1-27, P1-28, P2-12, P3-13, P4-05 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe the browser-level suite: what it runs against, what it is for, and the properties only it can demonstrate.

## Scope

`console/e2e/`. The stack it needs is `scripts/e2e-up.sh`.

## As Built

- **Playwright, against a real service** — a real Postgres, a real Redis, the real hosted login pages, and two demo consumer applications. There is no mock: a suite that faked the issuer would report success having tested nothing.
- **Fixtures** (`console/e2e/fixtures.ts`) seed per-test data through the Management API as the bootstrap administrator and clean up afterwards. The bootstrap credentials come from the environment `scripts/e2e-up.sh` prints.
- **Access tokens are minted from a refresh token** rather than handed to the suite once, because a run can outlive a ten-minute access token. Rotation makes that subtle: each exchange spends the old refresh token, so the current one is kept in a lock-protected file shared by workers. Presenting a spent token outside the grace window would revoke the family and fail every later test — which is exactly what happened before the lock existed.

### What the suite proves that nothing else does

| Test | Property |
|---|---|
| `login.spec.ts` | A real sign-in through the hosted page, and that a guarded URL is unreachable by typing it |
| `sso.spec.ts` | Signing into demo A means demo B asks nothing; signing out of A ends the shared session; B refuses a token minted for A |
| `consistency.spec.ts` | What the console shows and what the API reports are the same fact — and the console calls only documented paths |
| `orgswitcher.spec.ts` | An organization forced into the URL is refused server-side, not merely absent from the switcher |
| `projectgrants.spec.ts` | Creating and revoking a Project Grant in the console, each checked against the API |
| `mfa.spec.ts`, `sessions.spec.ts`, `account.spec.ts` | Second factors, session revocation and self-service against the real service |

The recurring pattern: **assert on screen, then assert against the API**. A console that wrote somewhere the API cannot see would satisfy every on-screen assertion.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Real service required | No mock fallback; the suite fails rather than skipping | `console/e2e/fixtures.ts` |
| Per-test data | Created and removed by fixtures | `fixtures.ts` |
| Bootstrap token handling | Rotation-aware, lock-protected | `fixtures.ts` |
| When it runs | `npm run e2e` locally; `CHECK_FULL=1` in the gate; CI job | `scripts/check.sh`, `.github/workflows/ci.yml` |

## Security Considerations

Several assertions here are security properties: cross-application token rejection, server-side refusal of a forced organization, and the console using no undocumented endpoint. They belong in a browser because that is where the real client, the real cookies and the real redirects are.

## Verification

The suite currently runs 33 tests and passes against a locally seeded stack; the pagination fix in `PF-20` was found by one of them failing honestly on a stack with more than 100 users (`MEMORY/records/2026-09-17-console-list-pagination.md`).

## Not Yet Built / Open Questions

- The suite runs in Chromium only.
- Passkey flows are exercised with a virtual authenticator; no hardware key is tested.
- The typed-confirmation branch of Project Grant revocation cannot be exercised until delegated assignment can create holders (`P4-02` shipped the write path; the branch needs `P4-04` before it is meaningful end to end).

## Related Documents

- `scripts/e2e-up.sh`; `docs/ARCHITECTURE/03-FRONTEND-ARCHITECTURE.md`; `docs/DEVOPS/01-ENVIRONMENTS-AND-CONFIGURATION.md`.
