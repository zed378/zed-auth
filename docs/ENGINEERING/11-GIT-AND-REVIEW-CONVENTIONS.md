# 11 - Git and Review Conventions

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-03, P0-11, P0-13 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

How work gets from a branch into `main`, and what the history is expected to say.

## Scope

Branching, commits, hooks, pull requests, merge policy.

## As Built

### One branch per task, and nothing on `main` directly

```
feat/P4-06-granted-projects
docs/engineering-frontend-website
```

Merges are `--no-ff`, so the branch remains visible as a unit and a card's whole change can
be read as one merge commit.

### The commit subject carries the task ID

```
P4-06: the receiving side can find the projects it was lent, and act on them
feat(P1-07): implement POST /oauth/token
```

`scripts/hooks/commit-msg` rejects anything else, and the gate exercises the hook rather
than assuming it works. The ID is the join key across branch, commit, pull request,
`MEMORY/` record and `TASKS/PROGRESS.md` — a commit without one breaks the chain, and the
chain is the audit trail.

### Commit messages explain the decision, not the diff

The body is the interesting part. A message here typically says:

1. **What was actually wrong**, in the first paragraph. `P4-06`'s opens with "The card is a
   console screen. It could not be built, because the API it needed did not exist."
2. **The decision and its alternative** — the filter that is a control, the shape that was
   rejected.
3. **What the mutations proved**, listed.
4. **What was found on the way**: the 404-versus-403 convention, the latent race in an
   older test.

`git log` is the only documentation that is never out of date, and a message that restates
the diff wastes it.

### Status is updated in the same commit as the work

`TASKS/PROGRESS.md` and the phase file move to `DONE` in the commit that finishes the
work, not afterwards. `TASKS/00-TASK-CONVENTIONS.md` makes it global Definition of Done
item 8. A board updated later is a board that is wrong in between, and "in between" is
where everyone reads it.

### A record accompanies the merge

`MEMORY/records/<date>-<task>-<slug>.md`, naming what was built, what was verified, which
mutations were run, what was **found**, and what still cannot be done. A record listing
only successes is less useful than one stating which assertion could not be made.

### What a pull request says

`AGENTS.md`:

- Link the specific `docs/PLAN/`, `docs/UI-UX/` or `docs/SECURITY/` sections implemented,
  so a reviewer can check against the spec rather than only against the diff.
- Name which tests were added or run, and which layer of `docs/PLAN/11`'s pyramid they sit
  in.
- State any deviation from the plan explicitly, as a deviation. A silent divergence is the
  thing the whole plan-first arrangement exists to prevent.

### Secrets never reach a remote branch

`scripts/hooks/pre-commit` refuses to stage `.env` files, private-key extensions and
credential-shaped strings. CI's `gitleaks` is the real gate; the hook exists because of the
gap between them. Once a secret reaches a remote branch, deleting the commit does not
un-leak it — the value has to be rotated. Catching it locally is the difference between
deleting a line and rotating a production key.

If you bypass with `--no-verify`, rotate whatever you just committed.

### CI

`.github/workflows/ci.yml` runs the same `scripts/check.sh` with the full flags. There is
**no deploy workflow**: the staging VM has no public IP, so deployment is pull-based from
the VM and remains manual (`P0-20` is open on it).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Branch per task | required | convention |
| Commit subject | starts with a task ID | `scripts/hooks/commit-msg` |
| Merge | `--no-ff` | convention |
| Board updated in the same commit | required | `TASKS/00-TASK-CONVENTIONS.md` |
| Record per card | required | same |
| Credentials in a commit | refused | `scripts/hooks/pre-commit`, gitleaks |
| CI | the same gate | `.github/workflows/ci.yml` |

## Verification

- `scripts/check.sh` — feeds the commit-msg hook a bad subject and a good one.
- `TASKS/PROGRESS.md` — the board, and whether it matches `git log`.

## Not Yet Built / Open Questions

- **No signed commits.**
- **No automated changelog.** `MEMORY/CHANGELOG.md` is written by hand.
- **No deploy workflow**, by necessity rather than choice.

## Related Documents

- [`03-NAMING-CONVENTIONS.md`](./03-NAMING-CONVENTIONS.md)
- [`14-CODE-REVIEW-CHECKLIST.md`](./14-CODE-REVIEW-CHECKLIST.md)
- [`../../TASKS/00-TASK-CONVENTIONS.md`](../../TASKS/00-TASK-CONVENTIONS.md)
