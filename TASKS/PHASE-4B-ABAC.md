# Phase 4b — ABAC (Conditional)

> **This phase may never run, and that is a valid outcome.**
>
> `docs/PLAN/08-AUTHORIZATION.md` Part D: "ABAC is **optional** — most organizations may never need it if RBAC + Project Grants already cover their needs... don't build it speculatively, only once a concrete need appears that RBAC genuinely can't express."
>
> `docs/PLAN/00-PROJECT-CONTEXT.md` says the same as a design principle: don't over-engineer ahead of need.

**Entry gate**: `P4-16` records an explicit go/no-go decision. Starting this phase requires a **documented, concrete authorization requirement that RBAC plus Project Grants cannot express**, named in `MEMORY/DECISIONS.md` — not an anticipated one, and not "it would be more flexible."

**Goal, if undertaken**: attribute-based authorization evaluated by an embedded OPA policy engine, with a rollout process safe enough that a bad policy cannot lock an organization out of its own systems.

**Roadmap reference**: `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 4b. **Source of truth**: `docs/PLAN/08-AUTHORIZATION.md` Part D.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P4B-00 | Justification gate | docs | S | P4-16 |
| P4B-01 | ABAC data model — `user_attributes` and `policies` | backend | M | P4B-00 |
| P4B-02 | Embedded OPA integration | backend | L | P4B-01 |
| P4B-03 | Extended `/v1/authz/check` with attributes | backend | L | P4B-02, P2-06 |
| P4B-04 | Policy lifecycle — draft, version, activate, rollback | backend | L | P4B-02 |
| P4B-05 | Dry-run evaluation | backend | L | P4B-04 |
| P4B-06 | User attribute management API | backend | M | P4B-01 |
| P4B-07 | Console — Policies (ABAC) tab with Rego editor | console | L | P4B-04, P4B-05 |
| P4B-08 | Docs — ABAC concepts and policy authoring guide | docs | M | P4B-07 |
| P4B-09 | Phase 4b test suite | backend, console | L | all above |
| P4B-10 | Phase 4b acceptance validation | all | M | P4B-09 |

---

## P4B-00 — Justification Gate

| | |
|---|---|
| **Status** | BLOCKED — awaiting a concrete requirement |
| **Depends on** | P4-16 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part D § Roadmap Placement, `docs/PLAN/00-PROJECT-CONTEXT.md` § Design Principles |
| **Spec required** | No |
| **Surface** | docs |

**Goal** — Force the "do we actually need this?" conversation to happen explicitly, and to be answerable later.

**Steps**
1. Document the concrete authorization requirement that triggered this phase, in the requester's own terms.
2. Demonstrate that RBAC plus Project Grants cannot express it. `docs/PLAN/08` Part D gives the canonical shape of a genuine case: "an approver can only approve requests from their own department, up to their personal limit" — where modelling every combination as a role explodes combinatorially.
3. If the requirement *can* be expressed with roles at acceptable cost, do that instead and close this phase as unnecessary.
4. Record the decision either way in `MEMORY/DECISIONS.md`. A recorded "we decided not to build ABAC because X" is as valuable as a decision to build it — it stops the question being reopened every quarter without new information.

**Definition of Done**
- [ ] A concrete requirement is documented, or the phase is closed as unnecessary.
- [ ] The RBAC-insufficiency argument is written down and reviewed.
- [ ] The decision is recorded in `MEMORY/DECISIONS.md`.

---

## P4B-01 — ABAC Data Model

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-00 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `user_attributes`, § `policies`, `docs/PLAN/08-AUTHORIZATION.md` Part D § Data Model Additions |
| **Spec required** | Yes — data model change |
| **Surface** | backend |

**Steps**
1. Implement `user_attributes` (`user_id`, `org_id`, `attributes` jsonb) if `P0-07` deferred it.
2. Implement `policies` (`org_id`, `project_id` nullable, `name`, `rego_source`, `status` in {`draft`, `active`, `disabled`}, `version`).
3. Enforce that attributes are org-scoped and covered by RLS like every other tenant-scoped table.
4. Version policies as immutable rows rather than in-place edits, so `docs/PLAN/08`'s single-action rollback is a matter of pointing at a previous version rather than reconstructing one.
5. Constrain attribute payloads: bound size and nesting depth. Unbounded JSONB attached to every authorization decision is a performance and denial-of-service problem.
6. Index for the read pattern: attributes are fetched on every decision, so this is a hot path.
7. Audit attribute changes — an attribute is now an input to authorization, which makes editing one a permission change.

**Definition of Done**
- [ ] Both tables exist per `docs/PLAN/04`, with RLS active.
- [ ] Policy versions are immutable; activation points at a version.
- [ ] Attribute size and depth are bounded.
- [ ] Attribute changes are audited as permission-affecting events.

---

## P4B-02 — Embedded OPA Integration

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-01 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part D § Policy Engine, `docs/PLAN/07-BACKEND-ARCHITECTURE.md`, `docs/PLAN/12-PERFORMANCE.md` |
| **Spec required** | Yes — authorization engine |
| **Surface** | backend |

**Goal** — OPA embedded as a Go library, in-process, because `docs/PLAN/08` and `docs/PLAN/12` both make the same point: a network hop per authorization check is unaffordable on this path.

**Steps**
1. Embed OPA as a library. Do not deploy it as a sidecar or separate service — that is the design the plan explicitly rejected.
2. Compile and cache prepared queries per active policy version; compiling Rego per request would defeat the purpose of embedding it.
3. Bound evaluation with a hard timeout and a memory ceiling. Rego is expressive enough to write an accidentally expensive policy, and an authorization path that can hang is a denial of service against every consumer application.
4. On evaluation error or timeout, **deny** (`docs/PLAN/13` § Graceful Degradation).
5. Instrument evaluation duration (`docs/PLAN/13` names OPA policy evaluation duration as a required metric).
6. Isolate policy evaluation from the rest of the process: a policy must not be able to reach the network, the filesystem, or arbitrary host state.
7. Reload policies on activation without a service restart, and confirm propagation across all instances within a bounded window.

**Definition of Done**
- [ ] OPA runs in-process with no network hop, verified by trace inspection.
- [ ] Prepared queries are cached per version.
- [ ] Evaluation timeout and memory ceiling are enforced, verified with a deliberately expensive policy.
- [ ] Errors and timeouts deny.
- [ ] Evaluation duration is a published metric.
- [ ] Policies reload without restart and propagate within the documented window.

---

## P4B-03 — Extended `/v1/authz/check` with Attributes

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-02, P2-06 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Authorization Check Endpoint, `docs/PLAN/08-AUTHORIZATION.md` Part D, `docs/PLAN/12-PERFORMANCE.md`, `docs/PLAN/13-OBSERVABILITY.md` |
| **Spec required** | Yes — authorization core |
| **Surface** | backend |

**Goal** — The same endpoint, now evaluating policies against subject, resource, action, and context — matching `docs/PLAN/05`'s documented example exactly, `matched_policy` and `reasons` included.

**Steps**
1. Extend the request to carry `resource.attributes` and `context`, per `docs/PLAN/05`'s worked example.
2. Layer RBAC and ABAC rather than treating them as alternatives: `docs/PLAN/08` Part D states that "RBAC roles become just one more attribute available to policies (`subject.roles`)."
3. Populate `matched_policy` and `reasons` from the evaluation — `docs/PLAN/08` notes that returning *why* a decision was made matters for both support and audit, and a policy engine that returns a bare boolean is unsupportable in production.
4. **Never log the raw resource attributes** (`docs/PLAN/13`, `CLAUDE.md`, and `docs/PLAN/10` § Information Disclosure all state this). Log the decision, the policy, and an attribute *fingerprint* if correlation is needed — never the values, which are the consumer application's business data.
5. Meet `docs/PLAN/12`'s ABAC targets: p50 < 40ms, p95 < 150ms, p99 < 300ms.
6. Cache subject attributes with a short TTL and proactive invalidation, reusing `P2-07`'s approach.
7. Define behavior when no policy matches: fall back to the RBAC decision, or deny? Make this explicit and document it — an ambiguous default here is a security bug waiting to be discovered by an auditor.

**Definition of Done**
- [ ] The request and response match `docs/PLAN/05`'s example exactly, including `matched_policy` and `reasons`.
- [ ] RBAC and ABAC compose, with roles available to policies as `subject.roles`.
- [ ] Raw resource attributes never reach any log, verified by test.
- [ ] `docs/PLAN/12`'s ABAC latency targets are met under load.
- [ ] The no-matching-policy behavior is explicit and documented.

**Abuse cases to test**
- Attribute injection through `resource.attributes` reaching a log or a query.
- Policy bypass by omitting an attribute the policy relies on — the policy must deny on missing input, not skip the clause.
- Using the endpoint to enumerate policies or attributes through response differences.
- Timing side channels revealing which policy matched.

---

## P4B-04 — Policy Lifecycle: Draft, Version, Activate, Rollback

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-02 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part D § Safe Rollout, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4b |
| **Spec required** | Yes — a bad policy can lock out an organization |
| **Surface** | backend |

**Steps**
1. New and edited policies start as `draft` and have no effect on real decisions (`docs/PLAN/08` Part D).
2. Every activation creates a new `version`; versions are immutable.
3. Rollback to a previous version is a **single action** — `docs/PLAN/17`'s Phase 4b criterion states this literally.
4. Validate Rego syntax and compile at save time; a policy that cannot compile must never reach `active`.
5. Apply the same review discipline as code changes (`docs/PLAN/08` Part D, `docs/PLAN/09`): activation requires an appropriate manager role, re-authentication, and an audit entry.
6. Detect the lockout case before it happens: warn if activation would deny an action the activating administrator currently relies on.
7. Support `disabled` as distinct from `draft` — a previously-active policy turned off retains its history and can be re-enabled.
8. Audit every status transition with actor, version, and diff.

**Definition of Done**
- [ ] A `draft` policy provably does not affect real decisions.
- [ ] Activation creates a new immutable version.
- [ ] Rollback is a single action, verified by test.
- [ ] An uncompilable policy cannot be activated.
- [ ] Activation requires elevated permission and re-authentication, and is audited.
- [ ] `disabled` and `draft` are distinct states with distinct semantics.

---

## P4B-05 — Dry-Run Evaluation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-04 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part D § Safe Rollout, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4b |
| **Spec required** | Yes |
| **Surface** | backend |

**Goal** — Run a draft policy against real recent requests before activating it, "catching unintended lockouts before production" (`docs/PLAN/08` Part D).

**Steps**
1. Capture a sample of recent authorization requests per organization for replay. Store them with attribute values redacted or hashed wherever possible — a request archive containing raw business attributes is a new and significant data-exposure surface, and it must not become one.
2. Evaluate a draft policy against the sample, producing a comparison: which decisions would change, and in which direction.
3. Surface denials that would newly appear especially prominently — a new denial is a potential lockout, and it is the outcome dry-run exists to catch.
4. Support ad-hoc evaluation against a hand-written sample input, for testing a policy before any real traffic exists.
5. Enforce the same timeout and memory bounds as production evaluation.
6. Guarantee dry-run has zero effect on real decisions, and prove it by test rather than by inspection.
7. Bound the retention of captured requests, and document it.

**Definition of Done**
- [ ] A draft policy can be dry-run against sample input without affecting real decisions — `docs/PLAN/17`'s Phase 4b criterion, verified by test.
- [ ] The comparison clearly identifies newly-denied decisions.
- [ ] Captured request data is minimized, retention-bounded, and documented.
- [ ] Ad-hoc evaluation works before any traffic exists.
- [ ] Dry-run evaluation is bounded identically to production evaluation.

---

## P4B-06 — User Attribute Management API

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-01 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `user_attributes`, `docs/PLAN/08-AUTHORIZATION.md` Part D, `docs/PLAN/02-REQUIREMENTS.md` FR-13 |
| **Spec required** | Yes — attributes are authorization inputs |
| **Surface** | backend |

**Steps**
1. CRUD for user attributes under the user resource, with the same permission model as grants — editing an attribute that a policy reads is a permission change in everything but name.
2. Define a per-organization attribute schema so `department` means one thing consistently. Free-form attributes across many policies become unmaintainable quickly.
3. Validate values against the schema on write.
4. Audit every attribute change with before and after values — this is exactly the trail an access review will need.
5. Bound attribute count and size per user (`P4B-01`).
6. Expose attributes in the console's user detail view for the roles permitted to see them; an attribute may itself be sensitive (a clearance level, a spending limit).

**Definition of Done**
- [ ] Attribute changes require the same permission level as grant changes.
- [ ] A per-org schema is enforced on write.
- [ ] Every change is audited with before/after values.
- [ ] Size and count limits are enforced.
- [ ] Attribute visibility is permission-scoped.

---

## P4B-07 — Console: Policies (ABAC) Tab with Rego Editor

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-04, P4B-05 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Policies — ABAC tab), `docs/UI-UX/04-USER-FLOWS.md` Flow 5, `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md` |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — Implement `docs/UI-UX/04` Flow 5 exactly, per `docs/UI-UX/08`'s note: a Rego editor, a diff view, and a confirmation dialog.

**Steps**
1. Run the full `docs/UI-UX/19` chain.
2. Rego editor with syntax highlighting and inline compile errors from the server-side validator in `P4B-04`.
3. Diff view between the current active version and the draft, since an activation is a change to a live security control and reviewing it should feel like reviewing a code change.
4. Dry-run interface presenting `P4B-05`'s comparison, with newly-denied decisions visually dominant.
5. Activation confirmation dialog using the typed-confirmation variant, stating the blast radius plainly.
6. Version history with single-action rollback per version.
7. `color-danger` only for the genuinely destructive actions — activation that would deny, and deletion.
8. Accessibility of the code editor is the hard part here: it must be keyboard-navigable and screen-reader usable, or it fails `docs/UI-UX/13`'s WCAG 2.1 AA requirement. Choose the editor component with this in mind rather than discovering it during the Phase 5 audit.

**Definition of Done**
- [ ] Flow 5 from `docs/UI-UX/04` is implemented exactly and covered by an E2E test.
- [ ] Compile errors render inline against the offending line.
- [ ] The diff view shows changes clearly before activation.
- [ ] Dry-run results emphasize new denials.
- [ ] Rollback is one action from version history.
- [ ] The editor meets WCAG 2.1 AA, including keyboard and screen-reader use.

---

## P4B-08 — Docs: ABAC Concepts and Policy Authoring

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-07 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part D, `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md`, `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| **Spec required** | No |
| **Surface** | docs |

**Steps**
1. Concepts page: subject, resource, action, environment, policy — using `docs/PLAN/08` Part D's table, which is already written at the right level.
2. Explain when ABAC is warranted and, just as importantly, when it is not. `docs/PLAN/08`'s own framing is that most organizations will never need it; the docs should say so rather than encouraging adoption for its own sake.
3. Policy authoring guide with the worked example from `docs/PLAN/08` Part D (`authz.purchase_approval`).
4. Guide to the safe rollout process: draft, dry-run, activate, roll back.
5. Document how RBAC and ABAC compose, since `subject.roles` being available to policies is the key integration point.
6. Regenerate the API reference; publish the changelog.

**Definition of Done**
- [ ] The concepts page matches `docs/PLAN/08` Part D's model.
- [ ] The docs are honest that ABAC is often unnecessary.
- [ ] The worked example compiles and evaluates as documented.
- [ ] The safe rollout process is documented step by step.

---

## P4B-09 — Phase 4b Test Suite

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all Phase 4b implementation tasks |
| **Plan refs** | `docs/PLAN/11-TESTING.md`, `docs/PLAN/08-AUTHORIZATION.md` Part D ("testable in CI") |
| **Spec required** | No |
| **Surface** | backend, console |

**Steps**
1. **Unit**: policy evaluation against fixed inputs; the layering of RBAC and ABAC; missing-attribute handling.
2. **Policy tests**: every shipped example policy has its own tests in CI — `docs/PLAN/08` Part D names testability in CI as a reason for choosing Rego, so this should actually be done.
3. **Integration**: the full lifecycle — draft, dry-run, activate, evaluate, roll back.
4. **E2E**: Flow 5 through the console.
5. **Security**: every abuse case on every Phase 4b task; policy evaluation resource exhaustion; attribute injection.
6. **Performance**: `docs/PLAN/12`'s ABAC latency targets under realistic policy complexity, not against a trivial policy.

**Definition of Done**
- [ ] Every Phase 4b abuse case has a passing test.
- [ ] Example policies have their own CI tests.
- [ ] A deliberately expensive policy is bounded rather than hanging the service.
- [ ] ABAC latency targets are met with realistic policy complexity.

---

## P4B-10 — Phase 4b Acceptance Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P4B-09 |
| **Plan refs** | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4b |
| **Spec required** | No |
| **Surface** | all |

**Steps**
1. Verify each `docs/PLAN/17` Phase 4b criterion with evidence:
   - A `draft` policy can be dry-run against sample input without affecting real authorization decisions.
   - Activating a policy creates a new version, and reverting is a single action.
   - `/v1/authz/check` with resource and context attributes returns a `reasons` field explaining the decision.
2. Confirm the original justifying requirement from `P4B-00` is actually satisfied — the point of the phase was to solve that specific problem, not to have an engine.
3. Load-test with ABAC in the path.
4. Write the phase summary in `MEMORY/`; update `PROGRESS.md`; tag; publish the changelog.

**Definition of Done**
- [ ] All three `docs/PLAN/17` Phase 4b criteria verified with evidence.
- [ ] The requirement from `P4B-00` is demonstrably satisfied.
- [ ] Load test results are recorded.
- [ ] A phase summary exists in `MEMORY/`.

---

## Phase 4b Exit Checklist

From `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 4b:

- [ ] A policy in `draft` status can be dry-run against sample input without affecting real authorization decisions.
- [ ] Activating a policy creates a new version, and reverting is a single action.
- [ ] `/v1/authz/check` with resource/context attributes returns a `reasons` field explaining the decision.

Plus:

- [ ] OPA is embedded in-process with bounded evaluation.
- [ ] Raw resource attributes never appear in logs.
- [ ] The Rego editor meets WCAG 2.1 AA.
- [ ] The requirement that justified this phase is solved.
