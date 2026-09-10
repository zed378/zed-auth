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
| ~~P0-01~~ | ~~Backend stack~~ — **decided 2026-09-08**, ADR-006. The **OIDC provider library** is deliberately still open: confirming JWKS rotation with overlap and refresh-token reuse detection requires building against it, so it moves to `P1-03` | `docs/PLAN/07` labels these "initial recommendations, not final decisions" |
| P0-12 | Audit write semantics: inside the business transaction, or after it | Determines whether a failed audit write blocks the action it records |
| ~~P1-02~~ | ~~Breached-password check: fail open or fail closed~~ — **decided 2026-09-09**, ADR-015: fail open, with an audit event, a counter and an alert on every skip | Failing closed blocks legitimate password changes during a third-party outage |
| ~~P1-05~~ | ~~Client secret hashing~~ — **decided 2026-09-09**, ADR-016: SHA-256, because 256 bits of entropy already settles brute force and a slow KDF would be self-inflicted amplification on the token endpoint | A slow KDF on a hot verification path is a denial-of-service surface |
| P1-13 | Rate limiting behavior when Redis is unavailable | Fail open means no rate limiting; fail closed means no logins at all |
| P1-21 | Console token storage: in-memory with silent renewal, or `localStorage` | In-memory resists XSS token theft but depends on silent renewal being solid |
| P2-04 | Token bloat mitigation for a heavily-granted user | An oversized token breaks at the HTTP header limit, in production, under load |
| P2-07 | Authorization cache TTL, and the revocation window it implies | The window must be documented publicly; integrators build security models on it |
| P2-09 | Tenant resolution: subdomain, path, email domain, or single default | Each has real infrastructure and UX costs; changing it later is disruptive |
| P3-06 | Refresh rotation retry grace window | Too strict logs real users out constantly; too loose weakens reuse detection |
| P3-08 | Anomaly response: step-up authentication, or notify only | Step-up on a false positive is disruptive; notification alone is passive |
| P4-16 | Whether to undertake Phase 4b (ABAC) at all | `docs/PLAN/08` Part D and `docs/PLAN/16` both say build it only on concrete need |
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

The repository held 49 planning documents across `docs/PLAN/`, `docs/UI-UX/`, and `docs/SECURITY/` and no implementation. The plan is unusually complete on *what* to build and *why*, but has no execution layer: no ordered task list, no dependency graph, no per-task definition of done, and no place to record what actually happened during implementation.

`AGENTS.md` rule 9 makes the three plan folders reference-only, so execution state could not simply be tracked inside them. `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` gives phase-level checkboxes, but a checkbox like "Basic OIDC provider implementation" is a month of work with a dozen security-critical decisions inside it — not a unit anyone can pick up, finish, and verify.

**Decision**

Add two folders:

- `TASKS/` — the execution plan: conventions, seven phase files with 124 individually-specified tasks, a progress board, and a backlog of open questions, plan gaps, and deferrals. Every task names the plan documents it implements, its dependencies, its definition of done, and its abuse cases. No task invents design; where the plan does not answer a question, that becomes a backlog entry rather than an assumption.
- `MEMORY/` — the record: one change record per completed task, a decision log, a changelog, and templates. A task is not `DONE` until its record exists.

Phase structure follows `docs/PLAN/16` exactly, including keeping Phase 4b conditional rather than assumed.

**Alternatives considered**

- *A GitHub Projects board or issue tracker instead.* Better for day-to-day flow, worse as a durable artifact: it lives outside the repository, is not reviewable in a PR, and is not readable by an agent working from a checkout. The two are compatible — issues can be generated from these files — but the file-based version is the one that survives.
- *Extending `docs/PLAN/16` with more detail.* Rejected on `AGENTS.md` rule 9: the plan folders are reference documentation, and mixing mutable execution state into them makes it impossible to tell a design change from a progress update in a diff.
- *No formal execution layer; work from the roadmap directly.* Rejected because the roadmap's phase items are too coarse to be picked up and verified, and because `docs/PLAN/17`'s acceptance criteria have no per-task counterpart — which is exactly how a phase gets declared done with three of its eight criteria unverified.
- *One folder combining tasks and records.* Rejected: forward-looking plans get edited, backward-looking records must not. Keeping them separate keeps the record trustworthy.

**Consequences**

Easier: a contributor or agent can find the next actionable task and know exactly what "done" means; security requirements are attached to the specific tasks that must satisfy them rather than living in a separate document nobody opens mid-implementation; plan gaps surface before implementation instead of during it.

Harder: two more folders to keep current. `PROGRESS.md` becomes stale the moment someone forgets to update it, which is why the global DoD requires updating it in the same commit as the work.

Cost: writing a change record per task is real overhead. The justification is that this is an identity provider — the same argument that makes `events` append-only at the database level applies to the development process, and a project that will be audited benefits from being able to explain how it got here.

**Plan impact**

`OQ-01` in `TASKS/BACKLOG.md` proposes adding `TASKS/` and `MEMORY/` to the documentation maps in `CLAUDE.md` and `AGENTS.md`. Both files govern agent behavior, so that edit awaits explicit approval rather than being made as a side effect.

Two contradictions between existing plan documents were found while writing the task breakdown and are recorded as `PG-08` (`docs/PLAN/05` places SAML in Phase 2 while `docs/PLAN/16`, `docs/PLAN/17`, and `docs/PLAN/03` place it in Phase 4) and `PG-09` (`docs/PLAN/18` R-04 states the Project Grant subset-validation rule as applying at grant creation, while `CLAUDE.md`, `AGENTS.md`, `docs/PLAN/08` Part C, and `docs/PLAN/19` all require it on every request). Neither has been edited — both are flagged for the deliberate plan-change process.

### ADR-002 — Amend the plan documents to close the eleven gaps, rather than working around them

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | `TASKS/BACKLOG.md` PG-01 … PG-11 |
| **Deciders** | Project owner (explicit instruction) |

**Context**

Writing `TASKS/` surfaced eleven things the plan requires functionally but does not specify — six of them missing tables in `docs/PLAN/04-DATA-MODEL.md` — plus two places where plan documents contradicted each other. Two of the missing tables (`signing_keys`, `user_tokens`) block Phase 1 tasks directly, one of which is on Phase 1's critical path.

`AGENTS.md` rule 9 makes the plan folders reference documentation that must not change as a side effect of feature work, while allowing a change that is "a deliberate, separate action the user should be aware of."

**Decision**

Amend the plan documents directly. `docs/PLAN/04` gained six tables, a retention policy, and a "what is deliberately not stored" table; `docs/PLAN/05`, `docs/PLAN/07`, `docs/PLAN/18`, and `docs/UI-UX/08` each received a targeted correction.

**Alternatives considered**

- *Record implementation decisions in `MEMORY/DECISIONS.md` and leave the plan unchanged.* Rejected: `CLAUDE.md` tells every agent to treat `docs/PLAN/` as the source of truth and to look up answers there rather than re-derive them. A `docs/PLAN/04` that describes a data model the system does not have trains readers to stop trusting it, and a source of truth that is known to be incomplete stops being consulted at all.
- *Specify the missing tables inside the `TASKS/` files only.* Rejected for the same reason, plus it puts design decisions in an execution document — the exact mixing the `TASKS/`/`docs/PLAN/` separation exists to prevent.
- *Defer each gap to the phase that needs it.* Rejected because the gaps interact. `refresh_tokens`' rotation-family columns must exist in Phase 0's schema even though Phase 3 uses them; discovering that in Phase 3 means a migration against a live token store.

**Consequences**

Easier: no Phase 1 task is blocked on a missing specification; the schema is designed once, coherently, instead of six times under time pressure.

Harder: `docs/PLAN/04` is now 295 lines rather than 158, and a longer document has more surface to contradict itself. `P0-07`'s requirement that a generated ERD match `docs/PLAN/04`'s diagram is the only automated guard against drift.

Accepted cost: three of these additions (webhooks, federated identity, MFA factors) specify tables for phases that may be a long way off, which is mild over-specification against `docs/PLAN/00`'s "don't over-engineer ahead of need" principle. Mitigated by `P0-07` explicitly allowing later-phase tables to be *created* in their own phase — the specification exists early, the migration does not have to.

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

`docs/PLAN/04` modelled `sessions` as a PostgreSQL table. `docs/PLAN/07` assigned session storage to Redis. Both are reasonable and both were probably intended, but nothing said how they reconcile — and two stores with no stated authority drift.

The constraint that forces the answer: `docs/PLAN/17`'s Phase 3 criterion requires a revoked session to be **immediately** unusable, while `docs/PLAN/12` requires the silent-SSO path to hit a p95 of 150ms, which a database round-trip per request makes hard.

**Decision**

PostgreSQL is authoritative and holds the durable record backing the sessions screen, admin session management, and audit. Redis holds a short-TTL lookup copy for the hot path. A revocation writes `revoked_at` in PostgreSQL and invalidates the Redis entry in the same operation. A Redis cache miss falls through to PostgreSQL and is never treated as "no session."

**Alternatives considered**

- *Redis authoritative, PostgreSQL as an async record.* Faster, and how many implementations do it. Rejected: sessions would be lost on a Redis failure, and `docs/PLAN/04` needs a queryable durable record for the "active sessions" screen and for audit. Losing session history to a cache eviction is unacceptable for an audit-bearing system.
- *PostgreSQL only, no cache.* Simplest and safest, but `docs/PLAN/12`'s silent-SSO target is the one latency number an end user actually feels. Rejected on that basis — though worth revisiting if measurement shows PostgreSQL alone meets it, since removing a cache removes a whole class of consistency bug.
- *Two independent stores with TTL-based eventual consistency.* Rejected outright: it makes revocation TTL-bound, which directly violates `docs/PLAN/17`'s "immediately unusable."

**Consequences**

Easier: revocation is genuinely immediate; session history survives a Redis outage; the durable record is queryable for the sessions screen and for incident investigation.

Harder: every session write touches two stores, and the invalidation must be part of the same operation rather than a follow-up — a revocation that updates PostgreSQL and fails to invalidate Redis leaves a revoked session working until TTL. That is the failure mode to test explicitly in `P1-11`.

**Plan impact**

`docs/PLAN/04` § `sessions` gained the storage note plus `org_id`, `last_seen_at`, and `revoked_at`. `docs/PLAN/07`'s Redis row now states it is a cache, not a second source of truth.

---

### ADR-004 — Audit log: monthly partitioning, 24-month default retention, pseudonymize rather than delete

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted, with the retention period pending confirmation (OQ-09) |
| **Task** | PG-10, affects `P0-07`, `P1-20`, `P5-10` |
| **Deciders** | Project owner (via the instruction to close the gaps) |

**Context**

`events` is append-only and unbounded, sits on the read path of the console's Audit Log screen, and is enforced non-deletable at the database level (`docs/SECURITY/02` §19). No retention, archival, or partitioning policy existed anywhere.

There is also a genuine tension the plan gestures at but does not resolve: `docs/PLAN/09` mentions GDPR's right to erasure, which is difficult to reconcile with an immutable audit log.

**Decision**

Monthly range partitioning on `created_at`. 24 months hot retention, then archive to cold storage rather than delete. For erasure requests, **pseudonymize**: erase the personal data in `users`, retain `actor_user_id` as an opaque identifier that no longer resolves to a person.

**Alternatives considered**

- *No policy; revisit when it becomes a problem.* Rejected — this is how a table becomes unqueryable at exactly the moment an incident makes it urgent.
- *Delete audit rows on an erasure request.* Rejected: it destroys precisely the evidence an incident investigation depends on (`docs/SECURITY/04`), and a deletion path into an append-only table undermines the database-level guarantee that makes the log trustworthy.
- *Hash the actor id instead of pseudonymizing.* Equivalent in effect but harder to operate — a stable opaque identifier keeps the event chain joinable for investigation, which a per-request hash would not.
- *Indefinite retention.* Rejected: unnecessary liability, and unbounded growth on a hot read path.

**Consequences**

Easier: dropping an expired partition is cheap where deleting rows would not be; partition pruning keeps the Audit Log screen fast as the table grows; the erasure position is defensible and does not require breaking append-only.

Harder: partitioned tables complicate some queries and add operational maintenance. Pseudonymization means an erasure request touches two subsystems rather than one.

Open: **24 months is a working default, not a researched obligation.** Too short fails an applicable retention requirement; too long is unnecessary liability. Raised as `OQ-09`. The pseudonymization approach is the standard reconciliation but is a position, not a certainty, and should be confirmed with whoever owns data protection.

**Plan impact**

`docs/PLAN/04` gained § Retention and Growth. `P0-07` now requires partitioning from the start, since retrofitting it onto a large table is far more disruptive.

---

### ADR-005 — Phase F is a track with per-page gates, not a sequenced phase

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | `TASKS/PHASE-F-FRONTEND-IMPLEMENTATION.md` |
| **Deciders** | Project owner (explicit request) |

**Context**

The user asked for a frontend phase covering all pages and components. Frontend work had been distributed across Phases 0–5 as fourteen coarse tasks, with no home for the component library that `docs/UI-UX/07` specifies and no single view of the whole surface.

But `docs/PLAN/16` states: "The management console is built **in lockstep with these phases, not as a separate track**." A frontend phase is, read plainly, the separate track that sentence forbids.

**Decision**

Add Phase F as a **track**, split into two kinds of task with different rules. Foundation tasks (design system, app shell, cross-cutting behavior, test infrastructure) are phase-independent and run early. Page tasks each carry a binding **Gate** naming the backend task that must be `DONE` first. The existing per-phase console tasks are retained as the phase gates that verify screens against `docs/PLAN/17`'s criteria; a mapping table states the relationship for all fourteen.

**Alternatives considered**

- *Decline, and expand the existing per-phase console tasks in place.* Most faithful to `docs/PLAN/16`'s letter, and rejected because it does not give the user what they asked for and leaves the component library homeless — components would keep being built incidentally inside page tasks, which is what `docs/UI-UX/05`'s governance rule exists to prevent.
- *A fully sequenced frontend phase running after Phase 5.* Rejected: it violates `docs/PLAN/16`'s intent completely, and building the console after the backend is finished means integration problems surface at the worst possible moment.
- *Delete the per-phase console tasks and move everything into Phase F.* Rejected: those tasks carry the per-phase acceptance verification from `docs/PLAN/17`, which is phase-shaped and does not belong in a track. Removing them would drop a `docs/PLAN/17` verification point.
- *Amend `docs/PLAN/16` to acknowledge a frontend track.* Available, and deliberately not taken. The resolution keeps the rule's intent and strengthens its enforcement, so an amendment seemed unnecessary — but this is flagged in the change record as the user's call rather than assumed.

**Consequences**

Easier: the whole frontend surface is visible at once; the component library has an owner; the lockstep rule is enforced per screen with a named task ID rather than per phase by convention.

Harder: two documents now describe the same screen from different angles, and they can drift. The mapping table is the mitigation, and it needs maintaining.

Risk accepted: making all the frontend work visible in one place makes it tempting to build a screen before its endpoint exists. The gates are binding, but nothing mechanically enforces them — this is discipline, and it is named in the record's "What to Watch" as the failure mode to catch early.

**Plan impact**

None yet. `docs/PLAN/16`'s lockstep sentence is preserved in intent; whether it should also be amended in wording is raised for the user rather than decided.

---

### ADR-006 — Backend stack: Go 1.26, chi, pgx, golang-migrate

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-01 |
| **Deciders** | Project owner |

**Context**

`docs/PLAN/07-BACKEND-ARCHITECTURE.md` labels its stack table "initial recommendations, not final decisions" and asks for them to be confirmed before implementation. `P0-01` is that confirmation.

**Decision**

| Layer | Choice |
|---|---|
| Language | Go 1.26.5 (the plan's recommendation, confirmed) |
| HTTP router | `go-chi/chi/v5` |
| PostgreSQL driver | `jackc/pgx/v5`, via `database/sql` for now |
| Migrations | `golang-migrate/migrate/v4`, embedded source |
| Logging | `log/slog` (standard library) |

**Alternatives considered**

- *Plain `net/http` instead of chi.* Go 1.22+ `ServeMux` handles method-and-path patterns natively, and zero dependencies is a real supply-chain advantage for an auth service (`docs/SECURITY/02` §15). Rejected because the Management API is deeply nested (`/v1/organizations/{org_id}/projects/{project_id}/roles`) and every `/v1` route needs the same bearer-auth and tenant-scoping middleware. chi's route groups express that directly; the stdlib equivalent is manual wrapping at every mount point, which is exactly where one route ends up subtly less protected than the rest. chi is small and has no transitive dependencies, so the supply-chain cost is low. Revisitable if the middleware story stays simple.
- *A third-party logging library (zap, zerolog).* Rejected: `log/slog` is in the standard library, its `ReplaceAttr` hook is exactly the right place for the redaction `docs/PLAN/13` requires, and a logging dependency in an identity provider is a dependency that sees every credential the redaction layer exists to protect.
- *`goose` or `atlas` for migrations.* Both are good. golang-migrate was chosen for advisory locking (concurrent runners during a rolling deploy) and explicit dirty-state tracking, which is the failure mode that actually bites.
- *TypeScript/NestJS or Java/Spring Authorization Server*, which `docs/PLAN/07` names as acceptable substitutes. Not chosen; no reason to deviate from the plan's default.

**Consequences**

Deferred, deliberately: the OIDC provider library (`ory/fosite` vs `zitadel/oidc`) is **not** decided here. `P0-01`'s Definition of Done requires the ADR to confirm support for JWKS rotation with an overlap window and refresh-token reuse detection, and confirming that honestly means building against the library rather than reading its documentation. That decision moves to `P1-03`, the first task that actually needs it, and is recorded as an open item rather than guessed at.

**Plan impact**

None. Every choice matches `docs/PLAN/07`'s first-named recommendation.

---

### ADR-007 — Migrations are embedded in the binary, not read from disk

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-06 |
| **Deciders** | Project owner |

**Context**

golang-migrate's default `file://` source could not resolve a Windows absolute path — `file:///C:/...` produced "The filename, directory name, or volume label syntax is incorrect". That was the trigger, but a worse problem sat underneath it: a container image can ship a `/migrations` directory at a different revision from the binary that reads it, and nothing detects the mismatch until a migration does the wrong thing.

**Decision**

Embed the `.sql` files with `embed.FS` and read them through golang-migrate's `iofs` source.

**Alternatives considered**

- *Fix the Windows path handling.* Solves the immediate error and leaves the drift problem untouched.
- *Require the migration CLI to be installed separately.* Adds a toolchain dependency to every developer machine and every CI job, for no benefit.

**Consequences**

The binary and its migrations cannot diverge, and there is no filesystem path to get wrong on any operating system. The cost: adding a migration requires rebuilding the binary — which is correct, because a migration *is* a code change.

**Plan impact**

None. `docs/PLAN/14` requires migrations to run as a separate step, which they still do. This changes only where the SQL is read from.

---

### ADR-008 — Runtime image is distroless, and the healthcheck is the binary itself

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-05 |
| **Deciders** | Project owner |

**Context**

`docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §17 covers container and runtime security. A container healthcheck conventionally shells out to `curl` or `wget`, which requires the runtime image to contain a shell and an HTTP client.

**Decision**

The runtime image is `gcr.io/distroless/static-debian12:nonroot` — no shell, no package manager, no libc. The container healthcheck invokes the service binary with a `-healthcheck` flag, which probes the local readiness endpoint and exits 0 or 1.

**Alternatives considered**

- *Alpine with `curl` installed.* Familiar and easier to debug, but it means an attacker who achieves code execution lands in an image with a shell, a package manager, and a working HTTP client — everything needed to pivot. Adding that permanently, to serve a healthcheck, is a poor trade for the most security-sensitive service in the platform.
- *No container healthcheck; rely on the Kubernetes probe.* Kubernetes probes need no in-image client, so this would work in production. Rejected because Docker Compose is the local environment (`docs/PLAN/14`), and a healthcheck that exists in only one environment is one that gets broken in the other without anyone noticing.

**Consequences**

Debugging inside a running container is genuinely harder — there is no shell to exec into. The mitigation is that the service is stateless and its logs are structured, so diagnosis happens from logs and metrics rather than from inside the container. That is the posture `docs/PLAN/13` assumes anyway.

**Plan impact**

None.

---

### ADR-009 — Status columns are text + CHECK, not native PostgreSQL enums

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-07 |
| **Deciders** | Project owner |

**Context**

`docs/PLAN/04-DATA-MODEL.md` specifies several columns as "enum" — `users.status`, `project_grants.status`, `applications.type`, `signing_keys.status` among them. `P0-07` step 3 requires "real enums or check constraints, never free text", leaving the choice open.

**Decision**

`text` columns with `CHECK (col IN (...))` constraints.

**Alternatives considered**

- *Native `CREATE TYPE ... AS ENUM`.* Better ergonomics and marginally smaller storage. Rejected because altering one fights the expand/contract discipline `docs/PLAN/14` requires: adding a value could not run inside a transaction on older PostgreSQL, and removing one is effectively impossible without recreating the type and every column that uses it. The roadmap adds values to exactly these columns in later phases — `applications.type` gains `saml` in Phase 4, `signing_keys.purpose` likewise — so a type that resists alteration is the wrong shape here.
- *Free text with application-layer validation only.* Rejected outright: `P0-07` requires the constraint, and the entire point of a database-level check is that it holds when the application layer has a bug.

**Consequences**

Adding an allowed value is a one-line `DROP CONSTRAINT` / `ADD CONSTRAINT`, which runs inside a transaction and is trivially reversible. The costs are marginally more storage and no automatic type safety in Go — though the application defines its own constants either way.

**Plan impact**

None. `docs/PLAN/04` accepts either.

---

### ADR-010 — `events` has no foreign keys, deliberately

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-07 |
| **Deciders** | Project owner |

**Context**

`events.org_id` and `events.actor_user_id` reference real rows in `organizations` and `users`. Foreign keys would be the default choice and would guarantee referential integrity.

**Decision**

No foreign key constraints on either column.

**Alternatives considered**

- *`ON DELETE CASCADE`.* Deleting a user would delete their audit history — destroying exactly the evidence an incident investigation needs, precisely when a departing or compromised account makes that history most valuable.
- *`ON DELETE RESTRICT`.* Makes a GDPR erasure request impossible to satisfy without first deleting audit rows, which the append-only guarantee forbids and which the application role has no privilege to do anyway.
- *`ON DELETE SET NULL`.* Loses the ability to correlate one actor's actions across the log, which is most of what an investigation does.

None of the three is compatible with `docs/PLAN/04` § Retention and Growth, which resolves the tension between an immutable audit log and a right to erasure by **pseudonymizing**: personal data in `users` is erased while `actor_user_id` is retained as an opaque identifier that no longer resolves to a person. The audit trail must be able to outlive the rows it refers to.

**Consequences**

An `actor_user_id` may reference a row that no longer exists. That is intended behavior, not a defect, and any query joining `events` to `users` must use an outer join and handle the miss. Referential integrity is given up deliberately, to keep the audit trail intact and erasure satisfiable.

**Plan impact**

None. This implements `docs/PLAN/04` § Retention and Growth as written.

---

### ADR-011 — Deploy to a self-managed VM now, Kubernetes later

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted, with an open deviation (DV-01) |
| **Task** | P0-14, P0-20 — answers `TASKS/BACKLOG.md` OQ-03 |
| **Deciders** | Project owner |

**Context**

`docs/PLAN/14-DEPLOYMENT.md` specifies Docker containers on Kubernetes for production, with Docker Compose "fine for local dev/small staging", and names "Vault / cloud secret manager" for secrets. It does not say which environment, and `OQ-03` recorded that gap because it determines the secret store, the managed database offering, the backup mechanism, and the ingress and TLS approach.

The project owner's answer: a self-managed VM for now, with Kubernetes support built later.

**Decision**

Deploy to a single self-managed VM using Docker Compose, and treat Kubernetes as a target the architecture must not foreclose rather than one it must reach now.

Concretely:

| Concern | Now (VM) | Later (Kubernetes) | What keeps the move cheap |
|---|---|---|---|
| Orchestration | Docker Compose + a systemd unit | Deployment + HPA | The service is already stateless with no local disk state |
| Configuration | Environment variables from a root-owned `0600` file | ConfigMap + Secret | Already environment-variable driven, so no code changes |
| Secrets | Files under a restricted directory, referenced by URI | Vault or a cloud secret manager, same URI scheme | A `SecretRef` indirection with pluggable schemes (`P0-14`) |
| TLS | Caddy on the host, automatic ACME | Ingress controller | TLS terminates outside the service either way, exactly as `docs/PLAN/14` assumes |
| Postgres | Container on the same host, WAL archived offsite | Managed service or an operator | `AUTH_POSTGRES_DSN` is the only coupling |
| Redis | Container on the same host | Managed service or an operator | `AUTH_REDIS_ADDR` is the only coupling |

The rule that makes this work is the one already followed: the service reads everything from the environment, keeps no state on local disk, and terminates TLS elsewhere. That is what `docs/PLAN/07` means by "stateless service, separate stateful store", and it is what makes a Kubernetes migration a manifest exercise.

**Alternatives considered**

- *Managed Kubernetes on a cloud provider now.* Matches `docs/PLAN/14` exactly and would close DV-01 immediately. Not chosen: it is the owner's call, it carries cost and operational overhead disproportionate to a project with no live consumer applications yet, and `docs/PLAN/00`'s incremental principle argues directly against it.
- *A single VM with no Kubernetes intent at all.* Simpler, and would let cheaper choices in — writing sessions to local disk, baking configuration into the image, terminating TLS inside the service. Rejected because each of those is individually tempting and collectively the thing that turns a later migration into a rewrite.
- *Vault on the same VM.* `docs/PLAN/07` names Vault, and it would keep the secret story identical across both targets. Rejected for now as disproportionate: running, unsealing, and backing up Vault on a single host is more operational surface than the secrets it protects. The `SecretRef` indirection means adopting it later is a scheme change, not a refactor.

**Consequences**

Cheaper now, and the migration path stays open. `docs/PLAN/02`'s constraint that no third party holds the private signing key is satisfied trivially — the key never leaves the owner's own machine.

Harder, and this is the real cost: **a single VM is a single point of failure for every consumer application's authentication.** `docs/PLAN/14` and `docs/PLAN/15` both specify Multi-AZ minimum for production, and this does not meet that. Recorded as **DV-01** in `TASKS/BACKLOG.md` rather than absorbed silently, because a deviation that is not written down is a bug nobody has found yet.

Two Phase 5 tasks are affected and should not be marked done without acknowledging it. `P5-06` requires horizontal scaling to be "verified in practice… by actually running a scale-out test" (`docs/PLAN/12`), which one host cannot demonstrate. `P5-07`'s DR drill requires restoring into an isolated environment, which means a second machine.

Operational burden shifts onto the owner: OS patching, database backups, certificate renewal, and monitoring are all self-managed where a cloud provider would supply them. `P0-20` addresses each explicitly rather than leaving them implied.

**Plan impact**

None yet. `docs/PLAN/14` still describes the target architecture correctly, and `docs/PLAN/16` Phase 5 still requires the hardening that a single VM cannot fully satisfy. If the VM becomes the permanent production environment rather than an interim one, `docs/PLAN/14` § Environment Strategy and `docs/PLAN/15` § High Availability both need amending, and `docs/PLAN/18` needs an owned, dated accepted-risk entry. Neither has been done, because the stated intent is that this is temporary.

---

### ADR-012 — Audit writes commit inside the transaction of the action they record

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-12 |
| **Deciders** | Project owner |

**Context**

`P0-12` step 4 required an explicit choice: does the audit write happen inside the business transaction, or after it? The two options fail in opposite directions, and neither failure is free.

Inside the transaction, a failed audit write rolls back the action that caused it. A database hiccup while recording a role assignment means the role assignment does not happen.

After the transaction, the action succeeds even if auditing fails. A role assignment can then exist with no record of who made it. `docs/PLAN/09` § Audit calls the audit log the mechanism by which "we can answer who did what, when" — a log with silent holes cannot answer that, and worse, cannot be *known* to have holes.

This was recorded in the code and in the change record but never written here, which `P0-12`'s Definition of Done required. Correcting that now.

**Decision**

Business events are written inside the caller's transaction. `audit.Write(ctx, tx, Event)` takes the transaction rather than opening its own, so the event and the action commit or fail together.

Two paths deliberately do not follow this rule:

- `WriteStandalone` opens its own transaction, for events with no business transaction to join — a failed login has no action to roll back.
- The instance-scope hook (`docs/PLAN/08` Part B's cross-tenant access record) runs *outside* the scoped transaction. Failing to record the access should not roll back the access. This is the opposite trade, made deliberately, because the record is observational rather than constitutive: nobody's authority derives from it.

**Alternatives considered**

- *Write after the transaction, with a retry queue.* Closes most of the hole and adds a durable queue that itself needs auditing, monitoring, and a failure policy. The complexity is real and the residual hole — a crash between commit and enqueue — remains.
- *Write to both the database and a log stream, treating the stream as the record of truth.* Defers the problem to log-pipeline reliability, which is generally weaker than the database's, not stronger.

**Consequences**

An audit-write failure takes down the action it was recording. That is an outage, and it is the intended trade: the service refuses to act rather than acting unrecorded. `AuditWriteFailures` alerts on it as critical, and its annotation says exactly this, so whoever is paged is not left deciding whether the alert matters.

The audit log is append-only at the privilege level, so a credential accidentally written there cannot be deleted by anyone. Redaction therefore happens in the writer, not at call sites — a call site that forgets is a permanent mistake.

**Plan impact**

None. `docs/PLAN/09` § Audit requires the log to be complete and tamper-evident; this is the stronger of the two readings of that requirement.

---

### ADR-013 — The OpenAPI spec is hand-written and generates the code, not the reverse

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-16 |
| **Deciders** | Project owner |

**Context**

`P0-16` step 2 required choosing between spec-first and code-first generation. `docs/PLAN/05` § Documentation accepts either — "generated from code or validated in CI" — so the decision turns on which one makes drift *impossible* rather than merely *detectable*.

The stakes are set by `CLAUDE.md`'s hard rule that API reference documentation is never hand-written, and by two consumers that both generate from this artifact: the console's typed client and the public API reference. A spec that has drifted from the implementation is worse than no spec, because both consumers will confidently render the wrong thing.

**Decision**

Spec-first. `openapi/openapi.yaml` is authored by hand and is the source of truth. From it:

- `oapi-codegen` generates **Go server interfaces** into `backend/internal/api/`. Handlers implement a generated interface, so an endpoint whose signature no longer matches the spec **fails to compile**.
- `oapi-codegen` generates TypeScript types for the console.
- The public site renders the reference from the same file.

**Alternatives considered**

- *Code-first from Go annotations (swaggo).* The annotation sits next to the handler, which feels like it prevents drift, but an annotation is a comment: it can say `200` while the handler returns `201` and nothing objects. It converts a compile-time property into a review-time one.
- *Spec-first with CI validation only.* This is what `P0-16` literally asked for, and it is weaker. CI validation catches drift after it is written and pushed; a generated interface catches it in the editor. The CI check stays as a backstop for the generated artifacts being stale, but it is no longer the primary mechanism.

**Consequences**

Adding an endpoint means editing the spec first. That ordering is a discipline cost and it is also the point — the contract is designed before the handler, which is what "API-first" (`docs/PLAN/02` FR-14) means in practice rather than as an aspiration.

The generated files are committed, so a reviewer sees the contract change and its consequences in one diff, and a fresh clone builds without a code-generation step. CI regenerates and fails on any difference.

The compiler enforcement is limited to shapes — paths, methods, status codes, request and response types. It cannot check that a handler's *behaviour* matches its description. Authorization semantics in particular are invisible to it and remain the reviewer's job.

**Plan impact**

None. `docs/PLAN/05` § Documentation permits this and `docs/PLAN/20` § API Reference Generation requires exactly one renderable source, which this is.

---

### ADR-014 — The public site is one Docusaurus project, not a marketing SSG plus a docs framework

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Status** | Accepted |
| **Task** | P0-18 |
| **Deciders** | Project owner |

**Context**

`docs/PLAN/20` § Recommended Stack lists the two concerns separately — "Landing/marketing pages: static site generator (e.g. Next.js in static export mode, or Astro)" and "Documentation site: Docusaurus (or similar)" — and `P0-01` step 5 restates it as "a static generator for marketing plus a docs framework with native versioning". Read as an instruction, that is two projects.

`docs/UI-UX/20` § Cross-Page Requirements pulls the other way: "Every page shares the same header/footer navigation and the same visual tokens, so moving between Landing → Docs → About never feels like a different product."

Two projects make that shared navigation a duplicated component in two codebases with two build systems. Duplicated navigation is not a theoretical risk — it is the specific thing that drifts, because a link added to one is a link somebody has to remember to add to the other, and the failure is invisible until a visitor takes the path nobody tested.

Neither document is wrong. They are optimising for different things, and the tension is real rather than a misreading.

**Decision**

One Docusaurus project serving landing, about, contact, docs, and the changelog.

`docs/PLAN/20`'s table is a list of concerns with illustrative examples ("e.g."), not a mandate of two deployments; Docusaurus is itself a static site generator, so using it for the marketing pages satisfies the letter of that row. What it does not satisfy is the implied separation, which is why this is recorded rather than done quietly.

The marketing pages are ordinary React and MDX under `src/pages/`. Moving them to a separate Astro build later is a move, not a rewrite, if the reasons below stop holding.

**Alternatives considered**

- *Two projects, as `docs/PLAN/20` implies.* Buys an independent marketing deploy cadence and a lighter marketing bundle. The cadence benefit needs a marketing team that can be blocked by a docs release, and there is one person. The bundle benefit is real and small: Docusaurus ships a React runtime the landing page does not need.
- *Two projects sharing a header package.* Solves the drift and reintroduces the coupling `docs/PLAN/20` § Why a Separate Surface exists to prevent — now with a third package to version.
- *Astro with Starlight for both.* Better first-paint than Docusaurus, and Starlight has no native docs versioning. `docs/PLAN/20` § Versioning Strategy makes versioning a requirement, and `P0-18`'s Definition of Done asks for it demonstrated. Choosing a stack that needs a third-party plugin for a stated requirement is the wrong trade.

**Consequences**

The shared header and footer are one definition in `docusaurus.config.ts` and cannot drift. One dependency tree, one build, one deploy.

The cost is a heavier landing page than a purpose-built marketing SSG would produce — a React runtime on a page that is static text. If Lighthouse on the landing page becomes a real problem rather than a hypothetical one, the marketing pages move out and the docs stay put.

`P0-01`'s Definition of Done asks for an ADR naming the public-site generator and the docs framework separately. Here they are the same choice, and this record is that ADR for both.

**Plan impact**

`docs/PLAN/20` § Recommended Stack's first two rows now describe an option that was considered and not taken. The document is not wrong about the concerns; it is one implementation short of describing what was built. If the single-project arrangement survives Phase 1, that table is worth amending to say so — and `docs/UI-UX/20` § Cross-Page Requirements is worth citing there as the reason.

---

### ADR-015 — The breached-password check fails open, and says so every time

| | |
|---|---|
| **Date** | 2026-09-09 |
| **Status** | Accepted |
| **Task** | `P1-02` |
| **Deciders** | Zed |

**Context**

`docs/PLAN/09` § Passwords & Credentials requires new passwords to be checked against a breached-password corpus. The corpus is a third-party HTTP service outside our trust boundary (`docs/SECURITY/00`), so it will sometimes be slow, rate-limited, or unreachable. `P1-02` step 4 makes the behaviour in that case an explicit decision rather than whatever the code happens to do.

The two options are not symmetrical, and the asymmetry is not about which risk is larger in the abstract. It is about *when* each one bites.

Failing closed makes a third party a hard dependency of password changes. The moment that dependency matters most is the worst possible moment for it to be down: during a credential incident, users are told to rotate their passwords, traffic to this exact path spikes, and if the corpus service is rate-limiting us — which a spike makes more likely, not less — fail-closed blocks the specific remediation the incident calls for. We would convert someone else's outage into our own, in the direction of keeping known-compromised passwords in place.

This differs from an authorization decision, where failing closed is almost always right. A denied authorization leaves the system in its previous, safe state. A blocked password change leaves the user holding the password they were trying to replace.

**Decision**

**Fail open, never silently.** When the corpus cannot be consulted, the password is accepted — subject to every composition rule, and hashed with Argon2id as always — and the skip is recorded three ways: an audit event naming the user whose password went unchecked, a counter labelled `skipped`, and an alert that fires when the skip rate exceeds a tenth over fifteen minutes.

Fail-open covers **service failure only**. A definitive "this password is in the corpus" is always a rejection. A malformed or empty response is a service failure, not an answer — a body that does not look like a range response has not answered the question, and reading it as clean would be an always-open path wearing a fail-open's clothes.

The three accepting outcomes (`clean`, `skipped`, `disabled`) are distinct metric values, and a test asserts they stay distinct. Collapsing any two removes exactly the signal this decision rests on.

**Alternatives considered**

*Fail closed.* Rejected for the reason above: it turns a third-party outage into a block on the operation users perform during an incident. It is also the option that looks more secure on a checklist and is worse in the situation that matters.

*Fail open silently.* Rejected, and it is the option worth naming because it is what fail-open becomes without deliberate effort. A skipped check that increments nothing is indistinguishable from a breach check that was never wired up, and this project has already found several checks that passed vacuously (`MEMORY/MEMORY-INDEX.md` § lessons). The observability is not a nicety attached to the decision; it is the half that makes it defensible.

*Queue the password for re-checking when the service returns.* Attractive and deferred. It requires storing something derived from the password until the check runs, which is a new place a password-derived value lives, for a benefit that the audit event already mostly delivers — the event names the user, so a re-check campaign can be driven from it at the cost of asking those users to rotate. Worth revisiting if the skip rate is ever material.

*Cache a local corpus.* The right answer at scale and disproportionate now: hundreds of millions of hashes to host, update and operate, to remove a dependency that is currently 273ms and available. The `BreachChecker` interface exists so this becomes one new implementation rather than a refactor.

**Consequences**

Easier: password changes keep working when the corpus does not, and a corpus outage cannot be used to deny password rotation.

Harder: a window exists in which breached passwords are accepted. It is bounded by the alert's fifteen minutes plus response time, and every password admitted in it is individually identified in the audit log.

Foreclosed: nothing. Moving to fail-closed later is a one-line change, and the metric would tell us how much it would have cost.

The alert is load-bearing. If it is ever silenced or the metric stops being scraped, this decision quietly becomes "no breach checking", and the code will not complain. That is the thing to watch.

**Plan impact**

None. `docs/PLAN/09` requires the check and does not specify the failure behaviour, which is why `P1-02` asked for this ADR.

---

### ADR-016 — Client secrets are hashed with SHA-256, not Argon2id

| | |
|---|---|
| **Date** | 2026-09-09 |
| **Status** | Accepted |
| **Task** | `P1-05` |
| **Deciders** | Zed |

**Context**

`internal/authn` hashes passwords with Argon2id (`P1-01`, ADR pending in that record). `P1-05` needs to store client secrets, and the obvious move — reuse the password hasher — is wrong for two independent reasons. Since a codebase containing both a slow KDF and a fast hash for two credential types invites exactly one question in review, the answer is written down rather than left in a comment.

A password is chosen by a human and carries on the order of 30 bits of entropy. The slow KDF is what stands between a leaked hash and a leaked password; without it, a wordlist recovers most of a user table in an afternoon.

A client secret here is 256 bits from `crypto/rand`. Brute-forcing it behind an infinitely fast hash takes on the order of 10^52 years at 10^18 guesses per second. The hash's speed is not what protects it. The entropy already did, by a margin key stretching cannot meaningfully extend.

**Decision**

Client secrets are stored as an unsalted SHA-256 hex digest and verified with `crypto/subtle.ConstantTimeCompare`.

Unsalted deliberately: a salt defends against precomputation across many low-entropy inputs, and there is no precomputing a table over 2^256. Adding one would imply the entropy assumption is not being relied on, which is worse than useless — it would suggest to the next reader that a weaker secret would be safe here.

**This decision is conditional, and the condition is enforced in code.** It holds only while the secret is high-entropy and generated by this service. `Generate` is the only producer, no function in the package accepts a caller-supplied secret, and `TestSecretEntropy` fails if the length or the alphabet is ever weakened. If an administrator could ever choose a client secret, SHA-256 would become the wrong answer that afternoon.

**Alternatives considered**

*Argon2id, for consistency with passwords.* Rejected, and not merely as overkill — it would be an unauthenticated amplification vector pointed at ourselves. Client secrets are verified on every `client_credentials` token request. At `P1-01`'s measured parameters (90ms, 64 MiB on the staging VM), fifty token requests per second is 4.5 cores and 288 MiB resident; two hundred is 18 cores and over a gigabyte. An attacker sending deliberately **wrong** secrets pays nothing and we pay all of it. We would have bought a denial-of-service surface with no security in return.

*bcrypt or scrypt at reduced cost parameters.* Same shape of problem, smaller. Reducing the cost until it is affordable on the hot path is an admission that the cost was not buying anything.

*HMAC-SHA256 with a server-side pepper.* Genuinely appealing: it means a database dump alone cannot verify a guessed secret. Rejected for now because it introduces a key that must be stored, rotated and available at every verification — the whole apparatus `P1-03` built for signing keys — to defend against an attacker who, by assumption, cannot guess a 256-bit secret anyway. Worth revisiting if secrets ever become shorter or user-chosen, which are the same condition as above.

**Consequences**

Verification is a single SHA-256 over a short string, so the token endpoint's client-authentication step is not a capacity concern and cannot be turned into one by an attacker.

The cost is that this decision is load-bearing on a property enforced elsewhere. Someone adding a "bring your own client secret" feature would silently invalidate it, and the failure would be invisible — no test breaks, nothing logs, and the secrets are simply weakly protected from then on. That risk is why `Generate` is the sole producer and why the entropy test names this ADR.

Secrets already issued cannot be re-hashed under a different scheme without reissuing them, since the plaintext is never retained. Changing this decision later means a rotation for every confidential client, which is exactly what `RotateSecret` exists to make routine.

**Plan impact**

None. `docs/PLAN/09` requires that credentials be hashed and does not specify the algorithm per credential type. `docs/PLAN/04` already models `client_secret_hash` as opaque text.
