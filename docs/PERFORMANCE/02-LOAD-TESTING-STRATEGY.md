# 02 - Load Testing Strategy

> Category: **Performance** (`docs/PERFORMANCE/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P1-28, P2-17, P3-15, P5-04, P5-05, P5-06 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Document the load-testing harness that actually exists (`scripts/loadtest/`), how it is run, what it has been used to measure, and which parts of `docs/PLAN/12-PERFORMANCE.md`'s Load Testing Plan it does not yet cover.

## Scope

Covers the Go load generator and its shell wrappers under `scripts/loadtest/`. Measured results are recorded in `00-PERFORMANCE-TARGETS.md`; connection/cache sizing the harness's findings fed back into is in `01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md`.

## As Built

**The harness.** `scripts/loadtest/main.go` ("Command loadgen") is a standard-library-only Go program — deliberately dependency-free, on the same reasoning as `demo/`: a load result is evidence, and evidence that needs a dependency tree to reproduce is weaker. It drives phases in the order `docs/PLAN/12`'s target table lists them: `authorize-silent`, `token-refresh`, `userinfo`, `management`, and `mixed` (all four at once, "because production never sees one traffic type"). It never follows redirects — every redirect in the OAuth flow carries something the test needs to read (a code, an error, a login request id), and a followed redirect would measure two requests and report them as one.

**Where it runs.** `run.sh` targets `127.0.0.1:10800` — the service's own port on the staging VM — specifically to avoid measuring the Cloudflare tunnel, the internet, and a residential uplink as if they were the service's own latency. It requires `~/auth-state/.env` and the staging compose file, so it only works when SSH'd into the VM.

**A second harness that does not need the VM.** `run-authz.sh` was added in Phase 2 after "this session spent a week unable to measure anything because the VM was unreachable" (`MEMORY/records/2026-09-12-P2-17-acceptance.md`). It seeds everything it needs — a project, a role, a user, a grant, and a service client — **entirely through the Management API**, so it can run against any deployment it can obtain a token for (a local Docker stack via `scripts/e2e-up.sh`, or staging). It smoke-checks that its fixture actually decides `allow` before measuring, because measuring the denial path silently measures a shorter one. It also reads its pacing rate directly out of `backend/internal/ratelimit/quota.go`'s `PerClientAuthz` constant with `sed`, rather than a hand-typed number, and refuses to run if it cannot find the constant — one source of truth for "how fast can this legitimately be driven."

**The concurrency sweep.** `sweep.sh` walks `STEPS="2 4 8 16 24 32"` workers for one phase, sampling `docker stats` for the service, PostgreSQL, and Redis containers throughout, so a throughput ceiling is attributed to a specific resource rather than guessed at. This is what identified PostgreSQL CPU, not the Go service or the connection pool, as the limiting resource in every sweep run to date (`00-PERFORMANCE-TARGETS.md`).

**Controls built into every run:**
- `LOAD_STRICT=N` divides every target by N — the harness's own mutation test. A load-testing tool that has only ever printed "within targets" has not demonstrated it is capable of printing anything else; `LOAD_STRICT=8` is used to confirm the harness can and does report `OVER` and exit non-zero.
- `LOAD_WORKERS` (default 20) and `LOAD_SECONDS` (default 30) are the two knobs every wrapper script exposes.
- Every run exits non-zero when a phase misses a target or sees failed requests, so it is usable as a CI-style gate even though it is not currently wired into CI (see Not Yet Built).

**What has actually been run, and when** — see `00-PERFORMANCE-TARGETS.md` for the numbers:
- Phase 1 (`P1-28`): full `run.sh` suite on staging, plus a sweep of `/oauth/token` and `/oauth/authorize`.
- Phase 2 (`P2-17`): `run-authz.sh` against a local Docker stack (staging unreachable), which found and fixed a rate-limit-quota bug before it could produce a valid RBAC latency measurement.
- Phase 3 (`P3-15`): staging, with an isolated-vs-mixed and MFA-on-vs-off A/B comparison to attribute the refresh-latency regression, plus a small manual sample of interactive sign-in timing (not part of the automated harness).

## Rules and Defaults

| Rule / setting | Value | Enforced in |
|---|---|---|
| Harness language/dependencies | Go, standard library only | `scripts/loadtest/main.go` |
| Redirect following | Disabled | `scripts/loadtest/main.go` client |
| Default workers / duration | 20 workers, 30 seconds per phase | `run.sh`, `run-authz.sh` (`LOAD_WORKERS`, `LOAD_SECONDS`) |
| Mutation self-test | `LOAD_STRICT=N` divides every target by N | all wrapper scripts |
| `/v1/authz/check` pacing source | Read from `ratelimit.PerClientAuthz` via `sed`, never hand-typed | `run-authz.sh` |
| VM-only harness | `run.sh` requires `~/auth-state/.env` and the staging compose file | `run.sh` |
| API-only harness | `run-authz.sh` seeds fixtures through the Management API only | `run-authz.sh` |

## Interfaces

Not an API surface. Entry points: `bash scripts/loadtest/run.sh` (VM only), `bash scripts/loadtest/run-authz.sh` (any deployment, requires sourcing an environment first), `PHASE=token bash scripts/loadtest/sweep.sh` (VM only, one phase at a time).

## Security Considerations

- `run-authz.sh`'s cleanup trap explicitly removes the grant, deactivates the created user, and deletes the service-client application after every run — but deliberately leaves the project and role in place (deleting a role is refused while a grant references it), and says so in its own output rather than silently leaving orphaned fixtures.
- Load-testing `/v1/authz/check` past its own rate-limit bound does not test the endpoint's latency; it tests the rate limiter's fast-rejection path, and blending the two in one percentile calculation produces a number that describes neither — `P2-17`'s record documents this as a real mistake made and then fixed, not a hypothetical one.

## Verification

- `MEMORY/records/2026-09-12-P1-phase-1-summary.md`, `MEMORY/records/2026-09-12-P2-17-acceptance.md`, `MEMORY/records/2026-09-15-P3-15-acceptance.md` — each records a real run, its numbers, and what the run found (including two harness bugs found and fixed, and one rate-limit-quota bug in the service itself).
- The harness's own `LOAD_STRICT` mode is its self-verification that a failing run is reported as failing.

## Not Yet Built / Open Questions

`docs/PLAN/12-PERFORMANCE.md` § Load Testing Plan asks for more than has been done:

- **Degraded-dependency scenarios are not tested by this harness or any other**: Redis slow, database read replica lagging, OPA policy evaluation artificially slowed. There is no read replica and no OPA evaluator yet (Phase 4b), so two of the three scenarios have no surface to test against. Owned by `TASKS/PHASE-5-HARDENING.md` § P5-05.
- **Horizontal scaling has never been verified in practice.** Every measurement in `00-PERFORMANCE-TARGETS.md` ran against a single service replica. Owned by `TASKS/PHASE-5-HARDENING.md` § P5-06.
- **The load test is not part of CI** and is not run automatically before a release; it is invoked by hand as part of a phase's acceptance task. `docs/PLAN/12`'s ask ("before each major release, not just before initial launch") is not yet an automated gate.
- **Realistic data volumes** (many organizations, many users, many grants) have not been used in any recorded run — `run-authz.sh` seeds one project, one role, one user, and one grant per run. `TASKS/PHASE-5-HARDENING.md` § P5-04 names this explicitly as a requirement ("performance against an empty database measures nothing").
- **A full re-measurement against Phase 4's delegation features** (Project Grants, delegated roles) has not been done; every recorded measurement predates or excludes them.

## Related Documents

- `docs/PLAN/12-PERFORMANCE.md` § Load Testing Plan, § Capacity Planning
- [`00-PERFORMANCE-TARGETS.md`](./00-PERFORMANCE-TARGETS.md), [`01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md`](./01-CONNECTION-POOLING-AND-CACHE-BUDGETS.md)
- `TASKS/PHASE-5-HARDENING.md` § P5-04, P5-05, P5-06
