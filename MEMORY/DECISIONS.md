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
| ~~PG-01..PG-11~~ | ~~Plan gaps~~ — **resolved 2026-09-08** by amending the plan documents. See ADR-002 through ADR-004 | — |
| OQ-09 | Audit log retention period, and pseudonymization-over-deletion for erasure requests | 24 months is a working default set by ADR-004, not a researched obligation; it is a legal and business question |
| P4B-07 | Which code editor component to use for the Rego editor | It must meet WCAG 2.1 AA for keyboard and screen-reader use; discovering it does not during the `PF-50` audit means replacing it late |

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

### ADR-002 — Amend the plan documents to close the eleven gaps, rather than working around them

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | `TASKS/BACKLOG.md` PG-01 … PG-11 |
| **Deciders** | Project owner (explicit instruction) |

**Context**

Writing `TASKS/` surfaced eleven things the plan requires functionally but does not specify — six of them missing tables in `PLAN/04-DATA-MODEL.md` — plus two places where plan documents contradicted each other. Two of the missing tables (`signing_keys`, `user_tokens`) block Phase 1 tasks directly, one of which is on Phase 1's critical path.

`AGENTS.md` rule 9 makes the plan folders reference documentation that must not change as a side effect of feature work, while allowing a change that is "a deliberate, separate action the user should be aware of."

**Decision**

Amend the plan documents directly. `PLAN/04` gained six tables, a retention policy, and a "what is deliberately not stored" table; `PLAN/05`, `PLAN/07`, `PLAN/18`, and `UI-UX/08` each received a targeted correction.

**Alternatives considered**

- *Record implementation decisions in `MEMORY/DECISIONS.md` and leave the plan unchanged.* Rejected: `CLAUDE.md` tells every agent to treat `PLAN/` as the source of truth and to look up answers there rather than re-derive them. A `PLAN/04` that describes a data model the system does not have trains readers to stop trusting it, and a source of truth that is known to be incomplete stops being consulted at all.
- *Specify the missing tables inside the `TASKS/` files only.* Rejected for the same reason, plus it puts design decisions in an execution document — the exact mixing the `TASKS/`/`PLAN/` separation exists to prevent.
- *Defer each gap to the phase that needs it.* Rejected because the gaps interact. `refresh_tokens`' rotation-family columns must exist in Phase 0's schema even though Phase 3 uses them; discovering that in Phase 3 means a migration against a live token store.

**Consequences**

Easier: no Phase 1 task is blocked on a missing specification; the schema is designed once, coherently, instead of six times under time pressure.

Harder: `PLAN/04` is now 295 lines rather than 158, and a longer document has more surface to contradict itself. `P0-07`'s requirement that a generated ERD match `PLAN/04`'s diagram is the only automated guard against drift.

Accepted cost: three of these additions (webhooks, federated identity, MFA factors) specify tables for phases that may be a long way off, which is mild over-specification against `PLAN/00`'s "don't over-engineer ahead of need" principle. Mitigated by `P0-07` explicitly allowing later-phase tables to be *created* in their own phase — the specification exists early, the migration does not have to.

**Plan impact**

This ADR *is* the plan impact. Full detail in [`records/2026-09-08-plan-gap-remediation.md`](./records/2026-09-08-plan-gap-remediation.md).

---

### ADR-003 — PostgreSQL is authoritative for sessions; Redis is a cache in front of it

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | PG-11, affects `P1-11` |
| **Deciders** | Project owner (via the instruction to close the gaps) |

**Context**

`PLAN/04` modelled `sessions` as a PostgreSQL table. `PLAN/07` assigned session storage to Redis. Both are reasonable and both were probably intended, but nothing said how they reconcile — and two stores with no stated authority drift.

The constraint that forces the answer: `PLAN/17`'s Phase 3 criterion requires a revoked session to be **immediately** unusable, while `PLAN/12` requires the silent-SSO path to hit a p95 of 150ms, which a database round-trip per request makes hard.

**Decision**

PostgreSQL is authoritative and holds the durable record backing the sessions screen, admin session management, and audit. Redis holds a short-TTL lookup copy for the hot path. A revocation writes `revoked_at` in PostgreSQL and invalidates the Redis entry in the same operation. A Redis cache miss falls through to PostgreSQL and is never treated as "no session."

**Alternatives considered**

- *Redis authoritative, PostgreSQL as an async record.* Faster, and how many implementations do it. Rejected: sessions would be lost on a Redis failure, and `PLAN/04` needs a queryable durable record for the "active sessions" screen and for audit. Losing session history to a cache eviction is unacceptable for an audit-bearing system.
- *PostgreSQL only, no cache.* Simplest and safest, but `PLAN/12`'s silent-SSO target is the one latency number an end user actually feels. Rejected on that basis — though worth revisiting if measurement shows PostgreSQL alone meets it, since removing a cache removes a whole class of consistency bug.
- *Two independent stores with TTL-based eventual consistency.* Rejected outright: it makes revocation TTL-bound, which directly violates `PLAN/17`'s "immediately unusable."

**Consequences**

Easier: revocation is genuinely immediate; session history survives a Redis outage; the durable record is queryable for the sessions screen and for incident investigation.

Harder: every session write touches two stores, and the invalidation must be part of the same operation rather than a follow-up — a revocation that updates PostgreSQL and fails to invalidate Redis leaves a revoked session working until TTL. That is the failure mode to test explicitly in `P1-11`.

**Plan impact**

`PLAN/04` § `sessions` gained the storage note plus `org_id`, `last_seen_at`, and `revoked_at`. `PLAN/07`'s Redis row now states it is a cache, not a second source of truth.

---

### ADR-004 — Audit log: monthly partitioning, 24-month default retention, pseudonymize rather than delete

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted, with the retention period pending confirmation (OQ-09) |
| **Task** | PG-10, affects `P0-07`, `P1-20`, `P5-10` |
| **Deciders** | Project owner (via the instruction to close the gaps) |

**Context**

`events` is append-only and unbounded, sits on the read path of the console's Audit Log screen, and is enforced non-deletable at the database level (`SECURITY/02` §19). No retention, archival, or partitioning policy existed anywhere.

There is also a genuine tension the plan gestures at but does not resolve: `PLAN/09` mentions GDPR's right to erasure, which is difficult to reconcile with an immutable audit log.

**Decision**

Monthly range partitioning on `created_at`. 24 months hot retention, then archive to cold storage rather than delete. For erasure requests, **pseudonymize**: erase the personal data in `users`, retain `actor_user_id` as an opaque identifier that no longer resolves to a person.

**Alternatives considered**

- *No policy; revisit when it becomes a problem.* Rejected — this is how a table becomes unqueryable at exactly the moment an incident makes it urgent.
- *Delete audit rows on an erasure request.* Rejected: it destroys precisely the evidence an incident investigation depends on (`SECURITY/04`), and a deletion path into an append-only table undermines the database-level guarantee that makes the log trustworthy.
- *Hash the actor id instead of pseudonymizing.* Equivalent in effect but harder to operate — a stable opaque identifier keeps the event chain joinable for investigation, which a per-request hash would not.
- *Indefinite retention.* Rejected: unnecessary liability, and unbounded growth on a hot read path.

**Consequences**

Easier: dropping an expired partition is cheap where deleting rows would not be; partition pruning keeps the Audit Log screen fast as the table grows; the erasure position is defensible and does not require breaking append-only.

Harder: partitioned tables complicate some queries and add operational maintenance. Pseudonymization means an erasure request touches two subsystems rather than one.

Open: **24 months is a working default, not a researched obligation.** Too short fails an applicable retention requirement; too long is unnecessary liability. Raised as `OQ-09`. The pseudonymization approach is the standard reconciliation but is a position, not a certainty, and should be confirmed with whoever owns data protection.

**Plan impact**

`PLAN/04` gained § Retention and Growth. `P0-07` now requires partitioning from the start, since retrofitting it onto a large table is far more disruptive.

---

### ADR-005 — Phase F is a track with per-page gates, not a sequenced phase

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` |
| **Deciders** | Project owner (explicit request) |

**Context**

The user asked for a frontend phase covering all pages and components. Frontend work had been distributed across Phases 0–5 as fourteen coarse tasks, with no home for the component library that `UI-UX/07` specifies and no single view of the whole surface.

But `PLAN/16` states: "The management console is built **in lockstep with these phases, not as a separate track**." A frontend phase is, read plainly, the separate track that sentence forbids.

**Decision**

Add Phase F as a **track**, split into two kinds of task with different rules. Foundation tasks (design system, app shell, cross-cutting behavior, test infrastructure) are phase-independent and run early. Page tasks each carry a binding **Gate** naming the backend task that must be `DONE` first. The existing per-phase console tasks are retained as the phase gates that verify screens against `PLAN/17`'s criteria; a mapping table states the relationship for all fourteen.

**Alternatives considered**

- *Decline, and expand the existing per-phase console tasks in place.* Most faithful to `PLAN/16`'s letter, and rejected because it does not give the user what they asked for and leaves the component library homeless — components would keep being built incidentally inside page tasks, which is what `UI-UX/05`'s governance rule exists to prevent.
- *A fully sequenced frontend phase running after Phase 5.* Rejected: it violates `PLAN/16`'s intent completely, and building the console after the backend is finished means integration problems surface at the worst possible moment.
- *Delete the per-phase console tasks and move everything into Phase F.* Rejected: those tasks carry the per-phase acceptance verification from `PLAN/17`, which is phase-shaped and does not belong in a track. Removing them would drop a `PLAN/17` verification point.
- *Amend `PLAN/16` to acknowledge a frontend track.* Available, and deliberately not taken. The resolution keeps the rule's intent and strengthens its enforcement, so an amendment seemed unnecessary — but this is flagged in the change record as the user's call rather than assumed.

**Consequences**

Easier: the whole frontend surface is visible at once; the component library has an owner; the lockstep rule is enforced per screen with a named task ID rather than per phase by convention.

Harder: two documents now describe the same screen from different angles, and they can drift. The mapping table is the mitigation, and it needs maintaining.

Risk accepted: making all the frontend work visible in one place makes it tempting to build a screen before its endpoint exists. The gates are binding, but nothing mechanically enforces them — this is discipline, and it is named in the record's "What to Watch" as the failure mode to catch early.

**Plan impact**

None yet. `PLAN/16`'s lockstep sentence is preserved in intent; whether it should also be amended in wording is raised for the user rather than decided.
