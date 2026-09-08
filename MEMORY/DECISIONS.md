# Decision Log (ADRs)

Every architectural decision the plan left open, and every deviation from what the plan decided.

**When to write one**: a decision the plan does not contain; a deviation from the plan (mandatory, per the deviation protocol in `TASKS/00-TASK-CONVENTIONS.md`); a choice that will be questioned later; or a decision **not** to build something.

**When not to**: an implementation detail that is obvious from the code and would not be questioned.

## Format

```
## ADR-NNN — <Title>

| | |
|---|---|
| **Date** | YYYY-MM-DD |
| **Status** | Proposed / Accepted / Superseded by ADR-NNN / Rejected |
| **Task** | <task id> |
| **Deciders** | |

**Context** — the situation forcing a choice, and the constraints on it.

**Decision** — what was chosen, stated plainly.

**Alternatives considered** — each option, and the specific reason it was not chosen.
This is the section that makes an ADR worth keeping: it tells a future reader
whether their idea was already evaluated or genuinely never considered.

**Consequences** — what this makes easier, what it makes harder, and what it
forecloses. Include the drawbacks. An ADR listing only benefits will not be
trusted when someone needs to decide whether to revisit it.

**Plan impact** — none, or: which document should be amended, and whether it has been.
```

Numbers are sequential and permanent. A superseded ADR stays in place with its status updated and a pointer forward; deleting it destroys the reasoning trail that is the entire point.

---

## Decisions Pending

Decisions `TASKS/` has identified as needing an ADR, listed here so they are not discovered late. Each moves into the log above when made.

| Task | Decision needed | Why it matters |
|---|---|---|
| P0-01 | Backend language, OIDC library, HTTP router, migration tool, console build tool, public-site generator, docs framework | `PLAN/07` labels these "initial recommendations, not final decisions" |
| P0-12 | Audit write semantics: inside the business transaction, or after it | Determines whether a failed audit write blocks the action it records |
| P1-02 | Breached-password check: fail open or fail closed when the service is unreachable | Failing closed blocks legitimate password changes during a third-party outage |
| P1-13 | Rate limiting behavior when Redis is unavailable | Fail open means no rate limiting; fail closed means no logins at all |
| P1-21 | Console token storage: in-memory with silent renewal, or `localStorage` | In-memory resists XSS token theft but depends on silent renewal being solid |
| P2-04 | Token bloat mitigation for a heavily-granted user | An oversized token breaks at the HTTP header limit, in production, under load |
| P2-07 | Authorization cache TTL, and the revocation window it implies | The window must be documented publicly; integrators build security models on it |
| P2-09 | Tenant resolution: subdomain, path, email domain, or single default | Each has real infrastructure and UX costs; changing it later is disruptive |
| P3-06 | Refresh rotation retry grace window | Too strict logs real users out constantly; too loose weakens reuse detection |
| P3-08 | Anomaly response: step-up authentication, or notify only | Step-up on a false positive is disruptive; notification alone is passive |
| P4-16 | Whether to undertake Phase 4b (ABAC) at all | `PLAN/08` Part D and `PLAN/16` both say build it only on concrete need |
| P4B-03 | Behavior when no ABAC policy matches: fall back to RBAC, or deny | An ambiguous default here is a security bug waiting for an auditor to find |
| PG-01..PG-11 | Plan gaps in `TASKS/BACKLOG.md` — each needs a plan amendment or a recorded implementation decision | The plan implies these but does not model them |

---

## Log

### ADR-001 — Establish `TASKS/` and `MEMORY/` as the execution layer

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-21 |
| **Deciders** | Project owner |

**Context**

The repository held 49 planning documents across `PLAN/`, `UI-UX/`, and `SECURITY/` and no implementation. The plan is unusually complete on *what* to build and *why*, but has no execution layer: no ordered task list, no dependency graph, no per-task definition of done, and no place to record what actually happened during implementation.

`AGENTS.md` rule 9 makes the three plan folders reference-only, so execution state could not simply be tracked inside them. `PLAN/16-IMPLEMENTATION-ROADMAP.md` gives phase-level checkboxes, but a checkbox like "Basic OIDC provider implementation" is a month of work with a dozen security-critical decisions inside it — not a unit anyone can pick up, finish, and verify.

**Decision**

Add two folders:

- `TASKS/` — the execution plan: conventions, seven phase files with 124 individually-specified tasks, a progress board, and a backlog of open questions, plan gaps, and deferrals. Every task names the plan documents it implements, its dependencies, its definition of done, and its abuse cases. No task invents design; where the plan does not answer a question, that becomes a backlog entry rather than an assumption.
- `MEMORY/` — the record: one change record per completed task, a decision log, a changelog, and templates. A task is not `DONE` until its record exists.

Phase structure follows `PLAN/16` exactly, including keeping Phase 4b conditional rather than assumed.

**Alternatives considered**

- *A GitHub Projects board or issue tracker instead.* Better for day-to-day flow, worse as a durable artifact: it lives outside the repository, is not reviewable in a PR, and is not readable by an agent working from a checkout. The two are compatible — issues can be generated from these files — but the file-based version is the one that survives.
- *Extending `PLAN/16` with more detail.* Rejected on `AGENTS.md` rule 9: the plan folders are reference documentation, and mixing mutable execution state into them makes it impossible to tell a design change from a progress update in a diff.
- *No formal execution layer; work from the roadmap directly.* Rejected because the roadmap's phase items are too coarse to be picked up and verified, and because `PLAN/17`'s acceptance criteria have no per-task counterpart — which is exactly how a phase gets declared done with three of its eight criteria unverified.
- *One folder combining tasks and records.* Rejected: forward-looking plans get edited, backward-looking records must not. Keeping them separate keeps the record trustworthy.

**Consequences**

Easier: a contributor or agent can find the next actionable task and know exactly what "done" means; security requirements are attached to the specific tasks that must satisfy them rather than living in a separate document nobody opens mid-implementation; plan gaps surface before implementation instead of during it.

Harder: two more folders to keep current. `PROGRESS.md` becomes stale the moment someone forgets to update it, which is why the global DoD requires updating it in the same commit as the work.

Cost: writing a change record per task is real overhead. The justification is that this is an identity provider — the same argument that makes `events` append-only at the database level applies to the development process, and a project that will be audited benefits from being able to explain how it got here.

**Plan impact**

`OQ-01` in `TASKS/BACKLOG.md` proposes adding `TASKS/` and `MEMORY/` to the documentation maps in `CLAUDE.md` and `AGENTS.md`. Both files govern agent behavior, so that edit awaits explicit approval rather than being made as a side effect.

Two contradictions between existing plan documents were found while writing the task breakdown and are recorded as `PG-08` (`PLAN/05` places SAML in Phase 2 while `PLAN/16`, `PLAN/17`, and `PLAN/03` place it in Phase 4) and `PG-09` (`PLAN/18` R-04 states the Project Grant subset-validation rule as applying at grant creation, while `CLAUDE.md`, `AGENTS.md`, `PLAN/08` Part C, and `PLAN/19` all require it on every request). Neither has been edited — both are flagged for the deliberate plan-change process.
