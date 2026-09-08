# MEMORY/ — Change Record and Decision Log

`TASKS/` is what will be done. `MEMORY/` is what **was** done, and why it ended up that way.

This folder exists because the plan documents describe the intended system, and the code describes the current system — but neither explains how one became the other. Six months from now, the question "why is refresh token rotation implemented with a two-second grace window?" is answerable from here and nowhere else. The code shows the window; only the record shows what it cost to get there.

For a project whose entire premise is that audit trails matter, the development process having one is not an affectation.

---

## Structure

| Path | Purpose |
|---|---|
| [`MEMORY-INDEX.md`](./MEMORY-INDEX.md) | One line per record, newest first. The entry point. |
| [`CHANGELOG.md`](./CHANGELOG.md) | Chronological summary of what changed, at a coarser grain than the records. |
| [`DECISIONS.md`](./DECISIONS.md) | Architecture Decision Records — every choice the plan left open, and every deviation from it. |
| [`records/`](./records/) | One file per completed task: what changed, why, and what to watch. |
| [`templates/`](./templates/) | The change record and phase summary templates. |

---

## What Gets a Record

**Every task that reaches `DONE`.** That is global Definition of Done item 7 in `TASKS/00-TASK-CONVENTIONS.md`, and it is not negotiable — a task without a record is not done, however finished the code looks.

Also recorded:

- **Any deviation from `PLAN/`, `UI-UX/`, or `SECURITY/`** — as an ADR, per the deviation protocol. A documented deviation is a decision; an undocumented one is a bug nobody has found yet.
- **Any decision the plan deliberately left open** — the OIDC library, the migration tool, token storage in the console, cache TTLs, fail-open versus fail-closed choices.
- **Phase completions** — a summary of what shipped, what deviated, what was deferred, and what to watch.
- **Operational events with lasting consequence** — a key rotation, a DR drill, a pentest, a load test result, a production incident.

## What Does Not Get a Record

- Work in progress. Records describe completed changes.
- Anything the git history already tells you accurately. A record explains *why*, not *what changed on which line*.
- Restating a plan document. Link to it instead.

---

## Writing a Record

1. Copy [`templates/CHANGE-RECORD-TEMPLATE.md`](./templates/CHANGE-RECORD-TEMPLATE.md).
2. Name it `records/YYYY-MM-DD-<task-id>-<slug>.md`.
3. Fill in every section. "Not applicable" is a valid answer; a blank section is not — the template's sections are there because each one has been the missing piece in someone's later investigation.
4. Add a one-line pointer to [`MEMORY-INDEX.md`](./MEMORY-INDEX.md) at the top of the list.
5. Add a `CHANGELOG.md` entry if the change is user-visible or operationally significant.
6. Add an ADR to [`DECISIONS.md`](./DECISIONS.md) if a decision was made or a plan deviated from.
7. Commit all of it **with the code**, not afterward. A record written a week later is a reconstruction, and reconstructions quietly omit the parts that were confusing at the time — which are exactly the parts worth having.

## Writing an ADR

Use the format in `DECISIONS.md`. The two sections that matter most are the ones easiest to skip:

- **Alternatives considered** — the whole value of an ADR is that a future reader can tell whether their new idea was already evaluated and rejected, or genuinely never considered.
- **Consequences** — including the bad ones. An ADR that lists only benefits is marketing, and it will not be trusted when someone needs to decide whether to revisit the choice.

---

## Honesty Rules

These matter more here than anywhere else in the repository, because a record is only worth reading if it can be trusted.

- **Record what happened, not what was supposed to happen.** If a test was skipped, say so. If a Definition of Done item was waived, say which one and who agreed.
- **Record failures.** A load test that missed its target, an approach abandoned after two days, a migration rolled back — these are the highest-value records in the folder, because they stop the same ground being covered twice.
- **Do not retroactively edit a record to look better.** Add a follow-up record instead. The point of an append-oriented log is that it can be trusted, which is the same reason `events` is append-only at the database level (`PLAN/04`, `SECURITY/02` §19).
- **Record open questions found during the work**, and add them to `TASKS/BACKLOG.md` so they have a consequence rather than only a mention.

---

## Relationship to Other Folders

| Folder | Direction | Nature |
|---|---|---|
| `PLAN/`, `UI-UX/`, `SECURITY/` | Reference | What was decided before building. Reference-only (`AGENTS.md` rule 9). |
| `TASKS/` | Forward | What will be built, in what order, and how it will be judged done. |
| `MEMORY/` | Backward | What was built, what it cost, and what to watch. |

A plan document changing is itself an event worth a MEMORY record — it means reality taught the plan something.
