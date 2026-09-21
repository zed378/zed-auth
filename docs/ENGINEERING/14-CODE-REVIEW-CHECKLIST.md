# 14 - Code Review Checklist

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-03 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

What a reviewer checks **that no tool can**. Everything the gate catches is deliberately
absent from this list: formatting, vet, coverage floors, generated-code drift and
credential scanning are already failures by the time a human looks.

## Scope

Any pull request.

## As Built

### Against the spec, not only against the diff

- [ ] Does the change implement the `docs/PLAN/` or `docs/UI-UX/` section the pull request
      names? A diff can be internally coherent and still not be the thing that was asked
      for.
- [ ] If it **deviates** from the plan, is the deviation stated as one — in the PR, and as
      an ADR or a `PG-xx` — rather than applied quietly?
- [ ] Is the card's scope respected? A change that quietly widens is harder to review than
      two changes.

### Can each new test fail?

This is the highest-value question in the list, because it is the repository's recurring
defect class.

- [ ] Was a mutation run for each load-bearing assertion, and is the result in the record?
- [ ] Does any test assert on a value it also computed? Does any ignore an error it should
      have asserted on?
- [ ] Does any new test pass against an empty table, an empty list, or a nil map — and
      would it still pass if the feature were deleted?
- [ ] If a mutation **could not** be made to fail, does the record say so rather than
      rounding up?

### Authorization and tenancy

- [ ] Is the permission decided in `internal/management` and nowhere else? (The
      architecture test catches the obvious forms; a new spelling would not be caught.)
- [ ] Does every query filter on the tenant column that expresses **authority**, not merely
      rely on RLS for visibility? For a two-sided table, which side is this route?
- [ ] Does a cross-tenant request answer **404**, not 403?
- [ ] For a delegated write: is the subset re-checked against the grant **as it stands
      now**, in the same transaction, and does a trigger repeat the rule?

### Data and migrations

- [ ] Is the migration additive, or does it carry an expand/contract note that actually
      describes the rollout?
- [ ] Does a new table need RLS — and if the answer is "no", is the reason written down?
- [ ] Is a `SECURITY DEFINER` function bounded by its own `WHERE`, with a fixed
      `search_path` and no `PUBLIC` execute? Is there a test that calls it as somebody
      else?
- [ ] Does anything compare two timestamps that were written by different clocks?

### Errors and output

- [ ] Does the error class match what the caller can do about it? (`Unavailable` versus
      `Internal`; `Reauthenticate` versus `Forbidden`.)
- [ ] Does any message tell the caller something `Reason` should have told only the log?
- [ ] Could any new log attribute carry a credential under a name the deny list does not
      have?

### Console

- [ ] Is every server rule generated rather than re-written? A regex in a component is a
      rule that will drift.
- [ ] Does the screen render a control the server would refuse — or, worse, a **disabled**
      one, which promises that somebody could enable it?
- [ ] Are all four states handled, and is the empty state the right *kind* of empty?
- [ ] Does a delegated role carry its source in the accessibility tree, not only in a
      `title`?
- [ ] Is `color-danger` used for a destructive action and nothing else?
- [ ] Was the implementation chain committed?

### Documentation and the trail

- [ ] Does the API change appear in `openapi/openapi.yaml` **first**, with the console and
      the public reference regenerated?
- [ ] Is `TASKS/PROGRESS.md` updated in this commit, not a later one?
- [ ] Does the `MEMORY/` record say what was **found**, not only what was built?
- [ ] Does any public-site copy now claim a capability that is not shipped?

### The question that catches the rest

- [ ] **What would have to be true for this to be wrong, and is anything checking it?**

## Not Yet Built / Open Questions

- **Review is single-person on this project.** The checklist is a self-review instrument
  as much as a reviewer's, which is a weaker arrangement than it describes and is worth
  saying plainly.
- **No `CODEOWNERS` rule outside `docs/`.** The plan directories require a separate review;
  code does not.

## Related Documents

- [`09-TESTING-CONVENTIONS.md`](./09-TESTING-CONVENTIONS.md)
- [`13-SECURITY-CODING-RULES.md`](./13-SECURITY-CODING-RULES.md)
- [`../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`](../UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md)
