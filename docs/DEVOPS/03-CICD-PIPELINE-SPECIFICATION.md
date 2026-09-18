# 03 - CI/CD Pipeline

> Category: **DevOps** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-13, P1-27, P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe what runs on every push, what runs locally before a merge, and why deployment is not part of the pipeline.

## Scope

Continuous integration and the local gate. The deployment procedure is `00-DEVOPS-OVERVIEW.md`; the test layers themselves are `docs/TESTING/`.

## As Built

### GitHub Actions (`.github/workflows/ci.yml`)

Jobs: `commit-convention`, `backend`, `integration`, `api-contract`, `console`, `public-site`, `migration-safety`, `security`, `shell`, `docker`, plus the aggregating gate. Concurrency is grouped per ref with in-progress cancellation.

Notable properties:

- **Integration tests run against real Postgres and Redis** via testcontainers, inside a container with the Docker socket mounted, so the race detector can run in an environment where cgo is available.
- **`api-contract`** regenerates the server interface, the console client and the published API reference, then fails if any differ from what is committed. The contract cannot drift from the code or the docs.
- **`migration-safety`** enforces the expand/contract rule and that every up migration has a down.
- **`shell`** runs shellcheck and actionlint (the workflow files check themselves).
- **`commit-convention`** requires the task-id subject convention the repository uses.

### The local gate (`scripts/check.sh`)

Thirteen sections: Go, demo applications, integration, migrations, shell, security, coverage, API contract, brand, console, public site, deployment — ending in a pass/fail summary. It is the same content as CI, ordered to fail fast, and it is what a task must be green on before it merges. `CHECK_FULL=1` adds the public-site build with its capability audit and link check, and the console E2E suite (both skipped by default because they are slow or need a browser).

### Delivery

There is **no automated deployment**. The staging VM has no public ingress for a runner to reach, so deployment is pull-based and manual, in the documented order (back up, pull, build, migrate, roll out, verify). Each backend task's record in `MEMORY/records/` states the deployment and the verification that followed.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| A task merges only on a green gate | `scripts/check.sh` with 0 failures | repository convention; CI repeats it |
| Generated artefacts are committed | Server interface, console client, API reference | `api-contract` job |
| Coverage | Measured once per run, with failing tests printed | `scripts/check-coverage.sh` |
| Race detector | Runs in CI (cgo available); skipped locally with a stated reason | `scripts/check.sh` § Go |
| Workflow files | Linted by actionlint | `shell` job and local section |

## Interfaces

- `bash scripts/check.sh` — local gate; `CHECK_FULL=1 bash scripts/check.sh` — everything.
- `scripts/e2e-up.sh` / `e2e-down.sh` — the stack the E2E suite needs.
- `scripts/acceptance-phase2.sh`, `scripts/acceptance-phase3.sh` — phase acceptance against a running service.

## Security Considerations

- CI holds no deployment credentials, because it performs no deployment. That removes the "compromise the pipeline, own production" path entirely, at the cost of manual releases.
- Log-hygiene and secret-shaped-string gates run in the `security` section, so a token or key accidentally logged fails the build rather than reaching a log.

## Verification

- The workflow and the local gate check each other: the `shell` section lints the workflow, and the workflow runs the same families the local gate does.
- CI has been green on `main` since it was repaired in `P3-14` (`MEMORY/records/2026-09-15-P3-14-test-suite.md`).

## Not Yet Built / Open Questions

- No continuous deployment, no environment promotion, no release tagging automation beyond the manual phase tags.
- No supply-chain attestation (SBOM, provenance) is produced.

## Related Documents

- `docs/TESTING/00-TESTING-STRATEGY.md` and its CI-gate document.
- `docs/PLAN/14-DEPLOYMENT.md`; `.github/workflows/ci.yml`; `scripts/check.sh`.
