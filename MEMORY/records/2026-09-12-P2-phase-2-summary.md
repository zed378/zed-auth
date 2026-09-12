# Phase 2 — RBAC & Multi-Tenancy

| | |
|---|---|
| **Closed** | 2026-09-12 |
| **Tasks** | 17 of 17 |
| **Closing task** | `P2-17` — acceptance validation |
| **Tag** | `v0.2.0-phase2` |

---

## What it is now possible to do

Define what a role carries, give it to somebody, read it out of their token,
and ask at the moment of an action whether it still holds — with a documented
number for how stale that answer can be.

Run more than one organization on one deployment, with isolation enforced by
the database rather than by remembering to filter, and switch an administrator
between the organizations they actually administer.

## The four acceptance criteria, verified

`scripts/acceptance-phase2.sh`, against a running service: **19 checks, 0
failures.** It prints the evidence — the actual grant, the actual decision, the
actual refusal — rather than a tick, because the second half of a criterion like
"scoped to Project A **only**" is satisfied by a user with no grants anywhere.

Its first run failed four checks, and all four were the harness being wrong
rather than the service:

- `/v1/authz/check` takes its organization and project from the **caller's**
  token, which is the whole point of the endpoint. The script asked with the
  console administrator's token and got the right answer about the wrong
  project. It now registers a service client inside the project under test, the
  way a consumer would.
- `PATCH /v1/organizations/{org_id}` requires `ORG_OWNER`, and the script
  discarded the 403 and reported the unchanged value as a persistence failure.
  The refusal is correct and is now asserted as such.

That second one is the first time [`PG-31`](../../TASKS/BACKLOG.md) — no API
grants a manager role — has cost anything concrete. No script can promote itself
to perform that write, so the enforcement half of criterion 3 is proven by
`P2-10`'s integration tests instead, and the acceptance script names them rather
than claiming to have checked something it cannot reach.

## The load test found a real ceiling

`/v1/authz/check` against `docs/PLAN/12`'s RBAC targets, paced at the endpoint's
own documented bound:

```
phase                        n      p50      p95      p99      max    rps
/v1/authz/check (RBAC)    2880    6.3ms   22.0ms   30.4ms   74.5ms     96
  target                          20.0ms   80.0ms  150.0ms
```

Comfortably inside, zero failures. Under `LOAD_STRICT=8` the same run reports
OVER and exits non-zero, so the harness is known to be capable of failing.

Getting there took two discoveries.

**The endpoint shared the Management API's rate limit.** The first run refused
19,658 of 40,515 requests. `ratelimit.PerClient` is 600 a minute and its own
comment says what it was sized for — "far above any console session and
comfortably above a provisioning script". `/v1/authz/check` is neither: it is
called on every protected request, and ten a second per client is a ceiling one
busy consumer reaches with a handful of users.

Fixed with `PerClientAuthz` at 6,000 a minute, its own counter namespace (two
quotas sharing a key share an allowance, which is the same as having one), and a
`HotRoutes` table in the middleware. Four tests, including one asserting that
every ordinary management route stays on the tighter bound — a fix that quietly
widened the whole API would be worse than the problem.

**Then the measurement itself was wrong.** At 724 rps against a 6,000/minute
bound, a third of the samples were the limiter's fast rejection path and the
percentiles blended two code paths. The harness already knew this — its
management phase says "measuring an endpoint past its own documented bound
measures the limiter" — and the authz phase was not paced. It is now, at a rate
`run-authz.sh` reads out of the Go constant so the two cannot drift.

## Three bugs the work found

**A settings update naming one password rule discarded the other two.** The API
promised a merge "key by key"; `jsonb || jsonb` merges one level deep. Raising
a minimum password length silently reset a deliberate "uppercase not required"
and dropped a deliberate "never expires". Found by `P2-14`, fixed with a general
recursive merge.

**`Modal`'s focus effect re-ran on every keystroke**, so typing in any dialog
was limited to one character per field. Two existing callers were safe by
accident. Found by `P2-11`.

**`/v1/me/organizations` answered 500 for every caller who administered
anything** — a `text[]` scanned as a string. Two tests failed; four passed,
because a 500 renders as an empty list and that is what they asserted. Found by
`P2-13`, and the fix was to the shared decoder: it now refuses to read a body
that did not come with a 200.

## What the phase added that the plan did not ask for

**`GET /v1/me/organizations`**, because `P2-13`'s Definition of Done asks the
switcher to list the organizations the caller administers "per server-side
truth" and nothing could answer that. Recorded as `PG-37`: a console card that
needs a new endpoint is not a console card, and the roadmap sized two more the
same way.

**The settings defaults in the contract**, so the console can show what a
setting is changing *from* without becoming a fourth copy of a number that
already exists three times. A generated Go file exists solely so a test can
compare what the contract publishes to what the service applies.

**A portable load-test runner.** `run.sh` only works on the staging VM, and this
phase spent days unable to measure anything because the VM was unreachable.
`run-authz.sh` seeds entirely through the Management API and runs against any
deployment it can get a token for.

## Verification, in total

| | |
|---|---|
| Backend unit | Full suite, including 6,300 exhaustive hierarchy combinations against an independently-derived oracle |
| Backend integration | Full suite against real Postgres and Redis, including 6 claim-tampering tests presenting validly-signed tokens whose claims contradict the database |
| Security suite | Its own CI target, now including RLS query-plan assertions at 400 tenants |
| Console | 230 component tests |
| End-to-end | 24 Playwright tests against a real service |
| Acceptance | 19 checks against a running deployment |
| Load | `/v1/authz/check` within every `docs/PLAN/12` target |
| Gates | 46 passed, 0 failed |

Every implementation task also carried a mutation run: each control reverted one
at a time, each required to turn its own test red. Two vacuous tests were caught
that way — one where a fixture's ordering made a project filter irrelevant, one
where a 500 was indistinguishable from the empty result being asserted.

## Staging

**Phase 2 has not run on staging.** The VM has been unreachable since the deploy
key went with a session scratchpad, and every task in this phase carries the
note.

What changed mid-phase is that it stopped being a blocker for *verification*:
Docker is available locally, so `scripts/e2e-up.sh` brings up a real stack —
service, database, Redis, browser — and the E2E suite, the acceptance script and
the load test all ran against it. That is a stronger result than `P2-01`…`P2-10`
managed and it is still not staging.

The first task of the next phase should be restoring VM access, and `PG-29`
(the backup cannot restore the signing key) is worth closing at the same time.

## Open going into Phase 3

| | |
|---|---|
| `PG-31` | No API grants a manager role — now demonstrably blocking, see above |
| `PG-36` | Nothing can answer "who has access to this project?" |
| `PG-37` | Console cards whose DoD needs endpoints that do not exist |
| `PG-34`, `PG-35` | Two smaller plan/implementation disagreements, both recorded with what was built and why |
| VM | Unreachable; Phase 2 unverified on staging |

The [Phase 3 threat review](2026-09-12-P2-17-phase-3-threat-review.md) is done
and found no new category of risk — but ten findings, five of which are the same
observation: every mechanism Phase 3 adds arrives with a recovery path, and the
recovery path is the new attack surface.
