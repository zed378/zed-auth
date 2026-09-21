# 00 - Engineering Context

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-01…P0-22 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

Say what governs code in this repository and in what order, so a disagreement about "how
should we do X" resolves by looking rather than by arguing.

## Scope

Everything under `backend/`, `console/`, `public-site/`, `demo/`, `deploy/` and
`scripts/`.

## As Built

### The order of authority

1. **`docs/PLAN/`, `docs/UI-UX/`, `docs/SECURITY/`** — design intent, frozen. Amended only
   by a deliberate plan change, which `.github/CODEOWNERS` makes a separate review.
2. **`AGENTS.md` and `CLAUDE.md`** — the short forms at the repository root: hard rules,
   where to find the spec for a task, and the feature workflow.
3. **`TASKS/`** — what is in scope now, and the Definition of Done each card inherits from
   `TASKS/00-TASK-CONVENTIONS.md`.
4. **This category** — how the code is written, when the above do not say.
5. **The existing code** — when nothing above says, match the package you are in.

A conflict between 1 and reality is not resolved by editing 1. It is recorded as an ADR in
`MEMORY/DECISIONS.md` or as a plan gap (`PG-xx`) in `TASKS/BACKLOG.md`, and the plan stays
the statement of intent. `AGENTS.md` rule 9 says this in one line: the plan documents are
not implementation output.

### The workflow a non-trivial change goes through

```
spec (docs/PLAN/19 template)  →  branch  →  code + tests  →  mutations  →  gate  →  merge  →  record
```

- **Spec** for anything touching authentication, authorization or the data model. It lives
  in `MEMORY/specs/`. Trivial changes skip it; the judgement call is stated in
  `AGENTS.md`.
- **Branch per task**, named for the task ID. Nothing lands on `main` directly.
- **Mutations**: break the code deliberately and confirm the test that should catch it
  does. The repository's recurring defect class is a check that cannot fail, and this is
  the only routine defence against it.
- **Record** in `MEMORY/records/`, naming what was found, not only what was built.

### Two invariants that outrank convenience

**Authorization is decided in one place.** `backend/internal/management/architecture_test.go`
reads the source of every other package and fails if any of them compares a role by name,
iterates a caller's grants, or calls `.Satisfies(`. The failure it prevents is undramatic
and nearly invisible in review: a handler that needs one extra condition writes the check
inline, which looks reasonable, works, and quietly skips the scope match, the
`INSTANCE_OWNER` path and every future change to the hierarchy.

Reading the source is the only way to check a negative like that. A behavioural test can
show that the endpoints we thought of are correct; it cannot show that no other file
decides permissions.

**The contract is the interface.** `openapi/openapi.yaml` generates the server's handler
interface (ADR-013) and the console's client. A route that is not in the spec cannot be
served, and a request the console writes by hand is the drift generation exists to
prevent.

### Honesty is a convention here

The repository documents what it found, including what it got wrong. `MEMORY/records/`
carries the mutation that proved a test vacuous, the check that turned out to be
unfalsifiable, and the bug a passing suite had been hiding. A record that lists only
successes is less useful than one that says which assertion could not be made.

`P4-04` is the standing example: its policy predicate could not be made to leak by
removal, because the subquery reads under the caller's own RLS. The record says so rather
than claiming six of six mutations were caught.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Plan documents are not edited as a side effect | invariant | `.github/CODEOWNERS`, `AGENTS.md` rule 9 |
| One task per branch, task ID in the subject | required | `scripts/hooks/commit-msg` |
| Authorization decided in one package | test over the source | `backend/internal/management/architecture_test.go` |
| Every endpoint exists in the contract first | required | `scripts/check.sh`, `scripts/openapi-shipped-paths.py` |
| Security-sensitive features ship with an abuse-case test | required | `AGENTS.md` rule 7, `docs/PLAN/11` |
| Documentation citations resolve | checked | `scripts/check-docs.py`, run by `scripts/check.sh` |
| Later-phase work before an earlier phase is complete | refused | `docs/PLAN/16`, `TASKS/PROGRESS.md` |

## Verification

- `scripts/check.sh` — the whole gate, 47 checks at the time of writing.
- `scripts/check-docs.py` — every claim in the domain categories cites something real.
  Wired into the gate; a renamed file that a document still names fails the build.
- `TASKS/PROGRESS.md` — the single source of truth for status, updated in the same commit
  as the work it describes.

## Not Yet Built / Open Questions

- **No `golangci-lint`.** `gofmt`, `go vet`, `gosec` and `govulncheck` run; a broader
  linter does not.

## Related Documents

- [`01-GO-CODING-STANDARDS.md`](./01-GO-CODING-STANDARDS.md)
- [`11-GIT-AND-REVIEW-CONVENTIONS.md`](./11-GIT-AND-REVIEW-CONVENTIONS.md)
- [`14-CODE-REVIEW-CHECKLIST.md`](./14-CODE-REVIEW-CHECKLIST.md)
