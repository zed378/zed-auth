# Phase 2 — Full RBAC & Multi-Tenancy

**Goal**: turn authentication into authorization. Roles defined per project, assigned to users, carried in tokens, and queryable in real time — plus activating the multi-organization capability the schema has carried since Phase 0.

**Why now**: Phase 1 proved who a user is. Everything downstream — Project Grants in Phase 4, ABAC in Phase 4b — is a refinement of the role model built here. Getting the claim format and the decision path right now avoids a breaking token-format change later, which would force every consumer application to update in lockstep.

**Prerequisite**: Phase 1 exit checklist fully satisfied, including its acceptance validation.

**Roadmap reference**: `docs/PLAN/16-IMPLEMENTATION-ROADMAP.md` § Phase 2. **Authorization source of truth**: `docs/PLAN/08-AUTHORIZATION.md` Parts A and B.

---

## Task Summary

| ID | Task | Surface | Size | Depends on |
|---|---|---|---|---|
| P2-01 | Role model and permission keys | backend | M | P1-17 |
| P2-02 | Management API — roles | backend | M | P2-01 |
| P2-03 | User grants (user × project × roles) | backend | L | P2-01, P1-19 |
| P2-04 | Role claims in the access token | backend | L | P2-03, P1-07 |
| P2-05 | Manager role enforcement, full hierarchy | backend | L | P1-15 |
| P2-06 | `POST /v1/authz/check` — RBAC decisions | backend | L | P2-03 |
| P2-07 | Authorization decision caching | backend | M | P2-06 |
| P2-08 | Multi-organization activation | backend | L | P0-08, P1-16 |
| P2-09 | Tenant resolution strategy | backend | M | P2-08 |
| P2-10 | Per-organization policy enforcement | backend | M | P2-08, P1-02 |
| P2-11 | Console — Roles tab | console | M | P2-02 |
| P2-12 | Console — Authorizations tab | console | L | P2-03 |
| P2-13 | Console — organization switcher | console | M | P2-08 |
| P2-14 | Console — Policies (Access) screen | console | M | P2-10 |
| P2-15 | Docs — RBAC and multi-tenancy guides | docs | M | P2-06, P2-08 |
| P2-16 | Phase 2 test suite | backend, console | L | all above |
| P2-17 | Phase 2 acceptance validation | all | M | P2-16 |

---

## P2-01 — Role Model and Permission Keys

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-12-P2-01-role-model.md), [spec](../MEMORY/specs/P2-01-role-model.md). Not yet on staging: the VM was unreachable (see the record) |
| **Depends on** | P1-17 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part A, `docs/PLAN/04-DATA-MODEL.md` § `roles` |
| **Spec required** | Yes — authorization model |
| **Surface** | backend |

**Goal** — Roles scoped to a project, each carrying permission keys, exactly as `docs/PLAN/08` Part A specifies: "admin" in Project A must never imply "admin" in Project B.

**Steps**
1. Implement the `roles` entity per `docs/PLAN/04`: `project_id`, `key`, `display_name`, `permission_keys text[]`, and `is_builtin`, unique on `(project_id, key)`.
2. Define the permission key format as `resource:action` (`docs/PLAN/08`'s example: `user:read`, `billing:write`), validated by a regex that is documented and shared with the console's form validation (`docs/PLAN/11` names role key format as a unit-test target).
3. Distinguish built-in roles (`org_owner`, `org_admin` per `docs/PLAN/08`) from custom roles created by an organization admin. Built-in roles are not deletable and not renamable.
4. Prevent role key collisions with reserved names.
5. Handle role deletion by refusing while grants reference it, or by cascading with an explicit, audited confirmation — silent cascade is how permissions vanish mysteriously.
6. Audit role creation, modification, and deletion.

**Definition of Done**
- [x] A role key is unique within its project and may repeat across projects. `TestARoleKeyIsUniqueWithinItsProject`, and `TestARoleKeyRepeatsAcrossProjectsAndCarriesDifferentPermissions` — which asserts the **absence** of project A's permission from project B's identically-named role, because a test that only checks the first one exists would pass against a schema with no scoping at all.
- [x] Permission key format validation is a pure, unit-tested function shared by API and console. One definition in `openapi/openapi.yaml`, generated into Go and TypeScript; `check.sh` regenerates and diffs, and that gate was mutation-tested.
- [x] Built-in roles cannot be deleted or renamed. In the database, not only in the store — re-key, delete, and clearing the flag are all refused. `display_name` stays editable on purpose: grants reference `key`.
- [x] Deleting a role in use is **refused**, with the count. Not cascaded: a cascade removes access from every user holding the role in response to a request that looks like tidying up. Auditing lands with `P2-02`, which is the first thing with an actor to attribute it to — the store has no caller identity.
- [x] A role in Project A grants nothing in Project B, verified by test. And enforced by the schema rather than by the caller remembering: a trigger refuses a role whose `org_id` disagrees with its project's.

**Deferred, named rather than implied**
- The `role.created` / `role.updated` / `role.deleted` audit events land with `P2-02`. The store has no actor: an audit row with no `actor_user_id` is what `P1-14` found and fixed, and writing one here would be the same mistake deliberately.
- `TestDeletingAReferencedRoleIsRefused` is the abuse-case test for A-4 and is already written — `P2-03` is what makes references creatable through an API rather than through the test's own INSERT.

---

## P2-02 — Management API: Roles

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-12-P2-02-roles-api.md). Taken after `P2-05`, which had to make `PROJECT_OWNER` real first. Not yet on staging |
| **Depends on** | P2-01 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Endpoint Structure, `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Roles tab) |
| **Spec required** | No |
| **Surface** | backend |

**Goal** — CRUD for roles under `/v1/organizations/{org_id}/projects/{project_id}/roles`, restricted to project-level administrators.

**Steps**
1. Implement list, create, read, update, and delete against the documented path.
2. Require `PROJECT_OWNER` or higher (`docs/PLAN/08` Part C hierarchy) — an ordinary org member must not be able to mint roles.
3. Validate permission keys on write, returning per-field errors in `docs/PLAN/05`'s format.
4. Return the count of grants referencing each role in list responses, so the console can warn before deletion.
5. Register in the OpenAPI spec; the public API reference regenerates automatically (`docs/PLAN/20`).

**Definition of Done**
- [x] All five operations work and enforce `PROJECT_OWNER` or higher. Tested through the whole `/v1` chain rather than against the handler — a test calling the handler directly passes with no policy entry at all, which is the bug it should fail on.
- [x] Invalid permission keys are rejected with field-level errors, naming the **index** so a form can point at the offending row rather than the whole field.
- [x] Grant counts are accurate, and are one query for the page: a count per row is an N+1 against a table that grows with every user.
- [x] The generated console client and public API reference both build, and the shipped-paths allowlist was updated in the same commit that added the paths.

**Found while doing it**
- A `PROJECT_OWNER` asking about another project in the same organization was answered **403**, which confirms that project exists — and listing projects requires `ORG_ADMIN`, so it was an enumeration oracle in the narrowest role in the system. Visibility on a project-scoped route now follows grants rather than organization membership.

---

## P2-03 — User Grants (User × Project × Roles)

| | |
|---|---|
| **Status** | DONE (one item is `P2-06`'s) — [record](../MEMORY/records/2026-09-12-P2-03-user-grants.md), [spec](../MEMORY/specs/P2-03-user-grants.md). Not yet on staging |
| **Depends on** | P2-01, P1-19 |
| **Plan refs** | `docs/PLAN/04-DATA-MODEL.md` § `user_grants`, `docs/PLAN/08-AUTHORIZATION.md` Part A, § Least Privilege |
| **Spec required** | Yes — authorization core |
| **Surface** | backend |

**Goal** — The mapping that actually grants access, built now so that Phase 4's delegated grants are a variation on it rather than a parallel system.

**Steps**
1. Implement `user_grants` per `docs/PLAN/04`: `user_id`, `project_id`, `project_grant_id` (nullable, unused until Phase 4), and `role_keys[]`.
2. Enforce the default from `docs/PLAN/08` § Least Privilege: a new user has **no access** until explicitly granted. There is no implicit or default role.
3. Validate on every write that each `role_key` exists in that project. A grant referencing a nonexistent role is a silent permission hole.
4. Implement `GET/POST/PATCH/DELETE /v1/organizations/{org_id}/users/{user_id}/grants`.
5. Leave the `project_grant_id` path deliberately unimplemented in this phase — but write the subset-validation hook point now, with a test asserting it currently rejects any non-null value, so Phase 4 fills in a designed slot rather than retrofitting the most security-critical check in the system.
6. Require re-authentication or MFA step-up for sensitive grant changes (`docs/PLAN/08` § Least Privilege recommends this; decide and record whether Phase 2 or Phase 3 delivers it, since MFA itself is Phase 3).
7. Audit every grant assignment and revocation with actor, subject, project, and the exact role keys.
8. Make revocation immediately effective: it must not wait for the access token to expire, which is what `P2-06`'s real-time check exists for.

**Definition of Done**
- [x] A user with no grant has zero access, verified by test. Asserted as an **absence**, because the absence is the control: nothing writes a grant except these endpoints, so there is no code path to test — and a future "default role for new users" convenience would break it with no other assertion noticing.
- [x] A grant referencing a nonexistent role key is rejected, by a trigger as well as by the application, and the refusal **names which key**.
- [x] Grant changes are audited with full detail: actor, subject, project, and the exact keys added or removed. The revocation event keeps what the user could do, because afterwards it is the only record.
- [x] A non-null `project_grant_id` is rejected in this phase, by a trigger whose **body** `P4-01` replaces rather than a CHECK it would have to drop — dropping a constraint is how a window opens between removing the refusal and adding the real check. The test is written to be inverted, not deleted.
- [ ] **Revocation is reflected by `/v1/authz/check` immediately. Blocked: that endpoint is `P2-06`.** The property underneath is delivered and tested — the row is deleted rather than flagged, so nothing can read it afterwards — and the end-to-end assertion lands with `P2-06`.

**Decided, as step 6 asked**
- Re-authentication for sensitive grant changes is **Phase 3**. MFA itself is `P3-01`, and a re-authentication prompt with no second factor behind it is a password re-entry: friction without assurance.

**Abuse cases to test**
- Self-granting: a user assigning themselves a role they cannot administer (`docs/SECURITY/02` §3).
- Assigning a role from a project the caller does not administer (`docs/SECURITY/02` §2).
- Mass assignment of `role_keys` through an unrelated endpoint's body (`docs/SECURITY/02` §11).

---

## P2-04 — Role Claims in the Access Token

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-12-P2-04-role-claims.md), [ADR-021](../MEMORY/DECISIONS.md). Not yet on staging |
| **Depends on** | P2-03, P1-07 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part A § How Role Claims Get Into the Token, Part C § Token Claim Format |
| **Spec required** | Yes — token format, hard to change later |
| **Surface** | backend |

**Goal** — Emit the claim format `docs/PLAN/08` specifies exactly, including the `org_id` nested inside each role value — because that detail is what prevents ambiguity once Phase 4's delegation makes the same role name reachable from two organizational contexts.

**Steps**
1. Emit claims in the documented shape:
   ```json
   "urn:authservice:iam:org:project:proj_pos:roles": {
     "cashier": { "org_id": "org_acme" }
   }
   ```
2. Include `org_id` inside the value even though Phase 2 has only one meaningful context — `docs/PLAN/08` states the reason plainly, and adding it later is a breaking change for every consumer.
3. Emit `urn:authservice:manager_roles` for administrative roles (`docs/PLAN/08` Part C).
4. Include only roles relevant to the requesting client's project by default, to keep tokens small; document the behavior precisely, since consumer apps will build against it.
5. Guard against token bloat: a user with many grants must not produce a token that exceeds header size limits. Decide the mitigation (scope-limited claims, or a reference token for the pathological case) and record it as an ADR before it becomes a production incident.
6. Document in the public docs that claims are a **point-in-time snapshot**: a revocation after issuance is not reflected until expiry, which is exactly why `docs/PLAN/08` says to prefer `/v1/authz/check` for sensitive actions.

**Definition of Done**
- [x] The claim format matches `docs/PLAN/08`'s documented JSON exactly, asserted against the **literal JSON** rather than a Go structure that marshals into something similar — a struct test passes when a field is renamed in both places at once.
- [x] `org_id` is present inside every role value, with a test naming it specifically because it is genuinely redundant today and is therefore the field somebody will remove.
- [x] A role assigned in Project A appears only in the Project A claim. The assertion is the **absence** from project B's token; checking only that the granted role appears would pass against a service that puts every role in every token.
- [x] Token size stays bounded for a heavily-granted user — 64 keys, truncating rather than dropping, [ADR-021](../MEMORY/DECISIONS.md), recorded before it became an incident as step 5 asked.
- [x] The snapshot semantics are documented publicly, on the authorization concepts page, with the reason rather than the rule.

**Abuse cases to test**
- Token tampering to add a role, defeated by signature verification (`docs/SECURITY/02` §1).
- Claim injection through a role key or display name containing JSON metacharacters.
- A role claim from org A honored while acting in org B's context (`docs/PLAN/08` Part C, step 3a of the permission check flow).

---

## P2-05 — Manager Role Enforcement, Full Hierarchy

| | |
|---|---|
| **Status** | DONE (two steps blocked) — [record](../MEMORY/records/2026-09-12-P2-05-manager-hierarchy.md), [spec](../MEMORY/specs/P2-05-manager-hierarchy.md). Taken **before** `P2-02`, which needs `PROJECT_OWNER` to exist. Not yet on staging |
| **Depends on** | P1-15 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part C § Manager Role Hierarchy & Inheritance, `docs/PLAN/04-DATA-MODEL.md` § `manager_roles` |
| **Spec required** | Yes — administrative authorization |
| **Surface** | backend |

**Goal** — The full administrative hierarchy from `docs/PLAN/08`, with inheritance flowing strictly downward and never upward.

**Steps**
1. Implement all five roles: `INSTANCE_OWNER`, `ORG_OWNER`, `ORG_ADMIN`, `PROJECT_OWNER`, `PROJECT_GRANT_OWNER` (the last is reserved and unused until Phase 4).
2. Implement downward inheritance exactly as `docs/PLAN/08` states: an `INSTANCE_OWNER` holds every `ORG_OWNER` right in every organization; the reverse never holds.
3. Encode the specific carve-out `docs/PLAN/08` names: `ORG_ADMIN` has org access **except** deleting the organization or changing its owner.
4. Scope each manager role by `scope_id` (`docs/PLAN/04`) — a `PROJECT_OWNER` on project X is not one on project Y.
5. Build a single permission-resolution function used by every endpoint. Multiple bespoke checks scattered across handlers is how one endpoint ends up subtly more permissive than the rest.
6. Require confirmation and audit for granting `INSTANCE_OWNER` or `ORG_OWNER` — these are the keys to the system (`docs/PLAN/08` § Least Privilege).
7. Guard against removing the last `ORG_OWNER` from an organization, which would orphan it.

**Definition of Done**
- [x] The hierarchy matches `docs/PLAN/08`'s diagram, with an exhaustive table-driven test over every (role, scope, action) combination. `TestTheHierarchyMatchesThePlan` covers all 25 pairs. The diagram needed a **reading** — `ORG_ADMIN` and `PROJECT_OWNER` are drawn as siblings, which would make every project endpoint shipped in `P1-17`/`P1-18` wrong. Resolved as "an organization-scoped role covers every project in its organization", recorded as `PG-32`.
- [x] Upward inheritance is impossible, tested explicitly **and as a property** — if A satisfies B and they differ, B must not satisfy A — so an edit that accidentally makes two roles equivalent fails even if somebody updates the cells to match.
- [x] `ORG_ADMIN` cannot delete the organization. Changing its owner is **not testable**: there is no owner-change operation, because nothing writes `manager_roles` (`PG-31`).
- [x] Every endpoint routes through the one resolution function, verified by review and by an architecture test that reads the source — the only way to check a negative — and which was proven by planting a bespoke check and watching it get caught.
- [ ] **The last `ORG_OWNER` cannot be removed. Blocked: there is no removal.** `PG-31` — no endpoint anywhere writes `manager_roles`, in any phase file. Left unticked rather than reasoned away, because the guard is real work that has not been done.

**Also not delivered, and why**
- Step 6 (confirmation and audit for granting `INSTANCE_OWNER`/`ORG_OWNER`) has nothing to attach to for the same reason. Both land with `PG-31`'s endpoint.

**Abuse cases to test**
- Horizontal escalation: an `ORG_ADMIN` in org A acting in org B (`docs/SECURITY/02` §3).
- Vertical escalation: an `ORG_ADMIN` granting themselves `ORG_OWNER`.
- A `PROJECT_OWNER` on project X acting on project Y.

---

## P2-06 — `POST /v1/authz/check` — RBAC Decisions

| | |
|---|---|
| **Status** | DONE (one item needs the VM) — [record](../MEMORY/records/2026-09-12-P2-06-authz-check.md), [spec](../MEMORY/specs/P2-06-authz-check.md). Not yet on staging |
| **Depends on** | P2-03 |
| **Plan refs** | `docs/PLAN/05-API-CONTRACT.md` § Authorization Check Endpoint, `docs/PLAN/08-AUTHORIZATION.md` Part A, `docs/PLAN/12-PERFORMANCE.md`, `docs/PLAN/13-OBSERVABILITY.md` |
| **Spec required** | Yes — authorization core |
| **Surface** | backend |

**Goal** — Real-time authorization decisions for consumer services, with a request and response shape designed now to accommodate Phase 4b's ABAC extension without a breaking change.

**Steps**
1. Implement the documented contract from `docs/PLAN/05`: `subject`, `action`, `resource`, `context` in; `allowed`, `matched_policy`, `reasons` out.
2. In Phase 2, `matched_policy` reflects the RBAC rule that decided the outcome; `reasons` explains it in the same terms. The field exists from day one so the ABAC extension changes content, not shape.
3. Decide against live grant data, not against the caller's token claims — the whole point of a real-time endpoint is that a revocation is honored before token expiry.
4. Authenticate the caller and verify it may ask about that subject. An unauthenticated or over-broad decision endpoint is an authorization oracle.
5. **Never log the raw `resource.attributes`** — `docs/PLAN/13` and `CLAUDE.md` both name this explicitly, and the attributes may contain the consumer application's business data.
6. Fail closed: a check that cannot complete because a dependency is down denies (`docs/PLAN/13` § Graceful Degradation — "availability of a decision is never a reason to weaken security posture").
7. Meet `docs/PLAN/12`'s targets: p50 < 20ms, p95 < 80ms, p99 < 150ms for RBAC-only checks.
8. Instrument decision latency, allow/deny ratio, and error rate, feeding `docs/PLAN/13`'s alert on sustained latency breach.

**Definition of Done**
- [x] Request and response match `docs/PLAN/05`'s documented example exactly.
- [x] Decisions read live grant data, and a revocation is reflected on the next check. The caller's token is minted **once**, before the revocation, and reused — so the only way the second answer can differ is if the decision read live data. This also closes `P2-03`'s open item.
- [x] Raw resource attributes never appear in any log, verified by capturing everything the handler logs during a request carrying a canary — and failing if the log is **empty** as well as if it contains the canary, so silence cannot satisfy it.
- [x] A dependency failure produces a deny, not an allow, verified by fault injection. It answers **503**, not a 200 claiming a decision was reached, which a caller may cache and which makes an outage look like a policy change on every dashboard watching the allow ratio.
- [ ] **`docs/PLAN/12`'s RBAC latency targets are met under load. Not measured:** `scripts/loadtest` must run on the VM, which was unreachable this session. The tightest targets in the document (p50 < 20ms, p95 < 80ms, p99 < 150ms), on the endpoint expected to carry the most traffic — so this is not a formality.
- [x] `reasons` is populated and useful for support and audit — and identical for every denial, because an endpoint whose reasons distinguished "no such user" from "no such role" would answer "does this user exist?" to anybody with a token.

**Added along the way**
- `Member`, the requirement meaning "holds a valid token for this organization". The policy table could previously say "an administrator" or nothing at all, and requiring `ORG_ADMIN` here would mean every service that checks a permission holds an administrative role over the organization.
- `UNAVAILABLE` as a fault class, distinct from `INTERNAL`: a bug here versus a dependency that did not answer.

**Abuse cases to test**
- Querying a decision about a subject the caller has no business asking about (`docs/SECURITY/02` §2).
- Using the endpoint as an enumeration oracle to map another organization's users or resources (`docs/SECURITY/02` §12).
- Attribute injection through `resource.attributes` reaching a log sink or a query.

---

## P2-07 — Authorization Decision Caching

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-12-P2-07-decision-caching.md), [ADR-022](../MEMORY/DECISIONS.md). Not yet on staging |
| **Depends on** | P2-06 |
| **Plan refs** | `docs/PLAN/12-PERFORMANCE.md` § Design Decisions Made for Performance, `docs/PLAN/08-AUTHORIZATION.md` Part C § Full Permission Check Flow |
| **Spec required** | Yes — security/performance trade-off |
| **Surface** | backend |

**Goal** — Meet the latency target without letting a revocation linger. `docs/PLAN/08` and `docs/PLAN/12` both prescribe short-TTL caching specifically so a DB round-trip isn't needed per check while revocations are still caught quickly.

**Steps**
1. Cache grant lookups in Redis with a deliberately short TTL — long enough to help, short enough that the revocation window is defensible. State the chosen value and its reasoning explicitly.
2. Invalidate proactively on grant change, so the TTL is a safety net rather than the primary mechanism.
3. Do not cache the decision itself where the resource is dynamic; cache the *inputs* (grants, role definitions) instead.
4. Document the revocation window in the public docs. A consumer team making a security decision deserves to know the real staleness bound, not an implied "instant."
5. Instrument cache hit rate and the observed staleness distribution.
6. On a cache backend failure, fall through to the database rather than failing the check — but never fall through to "allow."

**Definition of Done**
- [x] The TTL choice and its revocation-window implication are recorded in [ADR-022](../MEMORY/DECISIONS.md). 30 seconds, and the number matters less than what makes it defensible: invalidation is the mechanism, so the TTL covers only the cases invalidation cannot — a cache that was unreachable at the moment of the change, and a grant changed outside the API.
- [x] Proactive invalidation on grant change is tested end-to-end, through the real HTTP path. The tests **do not wait**: the TTL is 30 seconds and they take milliseconds, so the only thing that can make them pass is the invalidation landing.
- [x] Cache unavailability degrades to a database read, never to an allow — and the test asserts **both halves**, because a cache outage that denied everything would satisfy "never allow" while being an outage of its own. This found a real bug: without a per-operation timeout the request **timed out** instead of falling through, so a cache outage was a service outage.
- [x] The revocation window is documented publicly, with the reason attached: immediate normally, at most 30 seconds if an invalidation was lost, and if a step cannot be undone that is the window being accepted.
- [x] Hit rate and staleness are observable. Staleness is the age of an entry **when it was used**, which is what the fleet actually served rather than an upper bound anybody can read off a constant.

---

## P2-08 — Multi-Organization Activation

| | |
|---|---|
| **Status** | DONE (one item needs the VM) — [record](../MEMORY/records/2026-09-12-P2-08-multi-org.md). Not yet on staging |
| **Depends on** | P0-08, P1-16 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part B, `docs/PLAN/02-REQUIREMENTS.md` FR-11, `docs/PLAN/01-PRODUCT-SCOPE.md` § Out of Scope |
| **Spec required** | Yes — tenant isolation |
| **Surface** | backend |

**Goal** — Turn on the multi-tenancy the schema has carried since Phase 0, without the migration `docs/PLAN/02`'s assumption promised would not be needed.

**Steps**
1. Verify the promise: a second organization can be created and used with **no schema migration** (`docs/PLAN/02` § Assumptions). If a migration turns out to be needed, that is a plan deviation requiring an ADR.
2. Verify RLS holds under real multi-org data, not just in the synthetic test from `P0-08`.
3. Ensure every query path carries tenant context; add an architecture test that fails if a repository method can execute without it.
4. Ensure cross-org references are impossible: a project in org A can never be assigned to a user in org B (Phase 4's Project Grants are the only sanctioned path, and they are explicitly not this).
5. Keep the deferral from `docs/PLAN/01` intact: one user belonging to multiple organizations simultaneously is **out of scope**. Do not build workspace switching for a single identity; `P2-13`'s switcher is for an admin who holds manager roles in more than one org, which is a different thing.
6. Verify the audit log is org-scoped on read and that no filter combination crosses tenants.
7. Load-test with multiple organizations present to confirm RLS does not degrade query plans (`docs/PLAN/08` Part B mentions per-`org_id` indexing and partitioning as the growth strategy).

**Definition of Done**
- [x] A second organization is created and fully functional with no migration. A complete second tenant — organization, project, application, role, user, grant — with every row asserted to land. `docs/PLAN/02`'s assumption holds.
- [x] Cross-org data access is impossible at both the RLS and application layers. **This found a real hole**: `applications` and `project_grants` had no rule that their organization owns their project, so a row could be filed under the wrong tenant and then hidden by RLS from the organization that owns the project. Proven by dropping the new trigger and watching the test report the state the table was in this morning.
- [x] An architecture test fails if any query path can run without tenant context — and in **both directions**, because an exception listed for a file that no longer bypasses anything is as much rot as an unchecked bypass. It cannot forbid the bypass outright: six reads resolve the tenant itself, so requiring one first would be circular. It makes each deliberate instead.
- [x] Audit log reads are org-scoped under every filter combination, generated rather than listed — and the test asserts the caller can still read their **own** history, since every isolation assertion is otherwise satisfied by an endpoint returning nothing.
- [ ] **Query performance with multiple organizations is measured. Not run:** `scripts/loadtest` needs the VM, which was unreachable this session. The question `docs/PLAN/08` Part B raises is whether RLS degrades query plans once several tenants share a table.

**Abuse cases to test**
- Cross-tenant IDOR on every resource type (`docs/SECURITY/02` §2, §14).
- Tenant context confusion: switching context mid-request or mid-session.
- Enumerating another organization's existence through error differences (`docs/SECURITY/02` §12).

---

## P2-09 — Tenant Resolution Strategy

| | |
|---|---|
| **Status** | DONE — [record](../MEMORY/records/2026-09-12-P2-09-tenant-resolution.md), [ADR-023](../MEMORY/DECISIONS.md). Not yet on staging |
| **Depends on** | P2-08 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part B § Tenant Resolution |
| **Spec required** | Yes — authentication routing |
| **Surface** | backend |

**Goal** — Decide and implement how a request's organization is determined, from the combinable options `docs/PLAN/08` lists: subdomain, path, email domain at login, or a single default organization.

**Steps**
1. Choose the strategy and record it as an ADR. Each has a real cost: subdomains need wildcard TLS and DNS; path-based is simplest but leaks the org name into URLs; email-domain routing fails for users whose email domain is shared or personal.
2. Implement resolution as a single middleware producing the tenant context that RLS consumes.
3. Handle the unresolvable case explicitly — an unknown subdomain or unrecognized email domain must produce a clear, non-enumerable error rather than silently falling back to a default organization, which would be a cross-tenant leak.
4. Ensure resolution cannot be overridden by a client-supplied header or parameter.
5. Support the single-default-organization mode for purely internal deployments (`docs/PLAN/08` Part B), since that is what Phase 1 shipped with.

**Definition of Done**
- [x] The strategy is recorded as an ADR with its trade-offs — [ADR-023](../MEMORY/DECISIONS.md). Writing it down revealed the decision had **already been made**: the service resolves the tenant from the OIDC client, which is none of the four options Part B lists. Logged as `PG-33`.
- [x] Resolution happens in exactly one place: the client lookup, before anything else in the request.
- [x] An unresolvable tenant produces a clear error and never a default fallback. An unknown `client_id` is `P1-06`'s 400 that never redirects, and a search confirms there is no default-organization fallback anywhere to be tricked into.
- [x] A client-supplied header cannot change the resolved tenant. Tested by **reading the source**, because the claim is about every header and a behavioural test can only send the ones somebody thought of. Proven by planting an `X-Org-Id` read.
- [x] Single-org deployments still work unchanged — with one organization owning every client, every request resolves to it with no special case. That is what makes this strategy cover the MVP mode rather than replace it.

---

## P2-10 — Per-Organization Policy Enforcement

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-08, P1-02 |
| **Plan refs** | `docs/PLAN/08-AUTHORIZATION.md` Part B § Policies per Organization, `docs/PLAN/02-REQUIREMENTS.md` FR-12, `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 2 |
| **Spec required** | Yes — security policy |
| **Surface** | backend |

**Goal** — Organization settings that are **enforced at login time, not merely stored** — the precise wording of `docs/PLAN/17`'s Phase 2 criterion.

**Steps**
1. Enforce `password_policy` on every password set or change, reading per-org settings through `P1-02`'s evaluator.
2. Enforce `session_lifetime_hours` when creating sessions, per organization.
3. Enforce `allowed_login_methods`: a method not on the list is refused even if it is implemented.
4. Store `mfa_required` and validate it, but note that enforcement lands in Phase 3 when MFA exists. Do not advertise it as enforced before then (`CLAUDE.md`'s no-unshipped-claims rule applies to the console too).
5. Validate settings on write against a schema, rejecting unknown keys.
6. Handle the transition case: tightening a password policy does not invalidate existing passwords, but `max_age_days` should trigger a change on next login. Design this deliberately rather than leaving it emergent.
7. Audit every policy change with before and after values.

**Definition of Done**
- [ ] Password policy and session lifetime are enforced per organization at login, verified by test with two organizations holding different settings.
- [ ] A login method excluded by policy is refused.
- [ ] `mfa_required` is stored and validated, with enforcement explicitly deferred to Phase 3 and not claimed anywhere in the UI.
- [ ] Policy changes are audited with before/after values.
- [ ] Tightening a policy does not lock out existing users unexpectedly.

---

## P2-11 — Console: Roles Tab

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-02 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Roles tab), `docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, `docs/UI-UX/15-FORM-UX.md` |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — Create and manage project roles and their permission keys, using the shared table and form patterns.

**Steps**
1. Run the full `docs/UI-UX/19` implementation chain.
2. Table of roles with key, display name, permission count, and grant count.
3. Create and edit forms with client-side permission key validation matching the server's rule exactly — a client rule that is merely similar produces confusing rejections.
4. Deletion confirmation showing how many grants would be affected, with `color-danger` reserved for this destructive action.
5. Built-in roles render as non-editable with a clear explanation, not as a disabled control with no reason given.
6. Empty state distinguishes "no roles defined yet" (with a create affordance) from "no roles match the filter."

**Definition of Done**
- [ ] The implementation chain table is committed with the code.
- [ ] Client and server validation rules are identical, sharing one source of truth.
- [ ] Deletion warns with an accurate affected-grant count.
- [ ] Built-in roles are visibly and explicably non-editable.
- [ ] Accessibility and responsive requirements are met.

---

## P2-12 — Console: Authorizations Tab

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-03 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Authorizations tab), `docs/UI-UX/04-USER-FLOWS.md`, `docs/UI-UX/06-VISUAL-LANGUAGE.md` (role-source badge) |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — Search for a user and assign or revoke project roles, with the role-source badge in place from the start — `docs/UI-UX/08` makes that badge mandatory on every screen showing roles, not optional per screen.

**Steps**
1. Run the full `docs/UI-UX/19` chain.
2. User search with debounced server-side lookup and a loading state that does not shift layout.
3. Role assignment interface showing the project's available roles with their permission keys visible, so an admin can see what they are granting.
4. Implement the role-source badge now, even though every role in Phase 2 is "direct" — Phase 4 introduces "delegated," and building the badge later means auditing every screen again.
5. Revocation with confirmation, stating plainly that it takes effect immediately.
6. Reflect the least-privilege default visibly: a user with no grants shows an explicit "no access" state, not an ambiguous blank.
7. Mirror the User detail Grants tab from the same components.

**Definition of Done**
- [ ] The implementation chain table is committed.
- [ ] The role-source badge is present and renders "direct" correctly, ready for "delegated" in Phase 4.
- [ ] Assignment and revocation are covered by an E2E test.
- [ ] A user with no grants shows an unambiguous no-access state.
- [ ] Permission keys are visible at assignment time.

---

## P2-13 — Console: Organization Switcher

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-08 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Organization switcher), `docs/PLAN/06-FRONTEND-ARCHITECTURE.md` § Information Architecture, `docs/PLAN/01-PRODUCT-SCOPE.md` § Out of Scope |
| **Spec required** | No |
| **Surface** | console |

**Goal** — Let an admin holding manager roles in more than one organization switch context — while respecting `docs/PLAN/01`'s explicit deferral of one user belonging to multiple organizations as an end-user feature.

**Steps**
1. Show the switcher only when the signed-in admin actually administers more than one organization (`docs/UI-UX/08`: relevant only once multi-org is active).
2. Search input plus list, per `docs/UI-UX/08`'s component composition.
3. Switching context re-scopes every subsequent API call and clears cached server state for the previous organization — stale cross-org data in the UI is a leak even when the API was correct.
4. Make the active organization unmistakable in the chrome at all times. An admin performing a destructive action in the wrong organization is a realistic and severe failure mode.
5. Reflect the context in the URL, so a link or a refresh lands in the same place.
6. Verify server-side that the caller may act in the selected organization — the switcher is UI, not a control (`docs/UI-UX/08` § Cross-Screen Requirements).

**Definition of Done**
- [ ] The switcher lists exactly the organizations the caller administers, per server-side truth.
- [ ] Switching clears the previous organization's cached data, verified by test.
- [ ] The active organization is visible on every screen.
- [ ] Selecting an unauthorized organization is refused server-side even if forced client-side.
- [ ] The context survives refresh via the URL.

---

## P2-14 — Console: Policies (Access) Screen

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-10 |
| **Plan refs** | `docs/UI-UX/08-PAGE-SPECIFICATIONS.md` (Policies — Access tab), `docs/UI-UX/15-FORM-UX.md`, `docs/PLAN/08-AUTHORIZATION.md` Part B |
| **Spec required** | No — implementation chain mandatory |
| **Surface** | console |

**Goal** — Edit the organization's password policy, session lifetime, and login methods, mapping directly onto `organizations.settings`.

**Steps**
1. Run the full `docs/UI-UX/19` chain.
2. Form fields for `min_length`, `require_uppercase`, `max_age_days`, `session_lifetime_hours`, and `allowed_login_methods`.
3. Show the `mfa_required` toggle only if it can honestly be described. Since enforcement is Phase 3, either omit it or label it explicitly as taking effect when MFA ships — never present it as active.
4. Warn clearly about the blast radius: shortening session lifetime logs people out; restricting login methods can lock out users who only have that method.
5. Confirmation for changes that could reduce access for existing users.
6. Show the current effective values alongside the editable fields, so an admin knows what they are changing from.

**Definition of Done**
- [ ] Settings map exactly to `docs/PLAN/08` Part B's documented JSON shape.
- [ ] Nothing on the screen claims enforcement that does not exist in the current phase.
- [ ] Consequential changes warn before applying.
- [ ] Validation errors render inline per `docs/UI-UX/15`.
- [ ] Accessibility and responsive requirements are met.

---

## P2-15 — Docs: RBAC and Multi-Tenancy Guides

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-06, P2-08 |
| **Plan refs** | `docs/PLAN/20-PUBLIC-SITE-ARCHITECTURE.md` § Site Structure, `docs/UI-UX/21-CONTENT-AND-COPY-STRATEGY.md` |
| **Spec required** | No |
| **Surface** | docs |

**Goal** — Document the authorization model well enough that a consumer team integrates correctly without reading the source.

**Steps**
1. Concepts page for roles, permission keys, grants, and the manager role hierarchy, derived from `docs/PLAN/08` Parts A and B at a product-explainer level.
2. Guide: "Define roles for your application and assign them."
3. Guide: "Validate role claims in your service" — including the exact claim format from `P2-04`, with the reason for the nested `org_id`.
4. Guide: "Use `/v1/authz/check` for real-time decisions" — including the snapshot-versus-live distinction and `P2-07`'s documented revocation window. Getting this wrong in a consumer app is a real security bug, so the docs must be explicit rather than reassuring.
5. Regenerate the API reference for the new endpoints (automatic via `P0-16`).
6. Changelog entry for the Phase 2 release.
7. Audit every page for unshipped claims — Project Grants and ABAC are still Phase 4 and 4b.

**Definition of Done**
- [ ] The claim format in the docs matches the emitted tokens byte for byte.
- [ ] The revocation window and snapshot semantics are stated plainly.
- [ ] The API reference covers every new endpoint.
- [ ] No page mentions Project Grants or ABAC as available.

---

## P2-16 — Phase 2 Test Suite

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | all Phase 2 implementation tasks |
| **Plan refs** | `docs/PLAN/11-TESTING.md`, `docs/PLAN/10-THREAT-MODEL.md`, `docs/SECURITY/02` §2, §3 |
| **Spec required** | No |
| **Surface** | backend, console |

**Goal** — Prove the authorization model, not just exercise it.

**Steps**
1. **Unit**: RBAC decision logic with no database, exhaustively table-driven; manager role hierarchy across every (role, scope, action) triple; permission key validation.
2. **Integration**: the full flow from `docs/PLAN/11` — organization → project → application → user → role assignment → login → verify token claims are correct and correctly scoped.
3. **E2E**: console role creation and assignment; organization switching; a user with no grant being denied.
4. **Security**: cross-org access attempts on every resource; privilege escalation both horizontally and vertically; RLS enforcement independent of application filtering; `/v1/authz/check` as an enumeration oracle; claim tampering.
5. Add a multi-org performance test confirming RLS does not degrade plans at scale.
6. Wire everything into CI with the security tests as a distinct, visible suite.

**Definition of Done**
- [ ] Every abuse case named in Phase 2 tasks has a passing test.
- [ ] The manager role hierarchy has exhaustive combinatorial coverage.
- [ ] Cross-tenant isolation is verified at both layers, independently.
- [ ] The full suite is green in CI.

---

## P2-17 — Phase 2 Acceptance Validation

| | |
|---|---|
| **Status** | TODO |
| **Depends on** | P2-16 |
| **Plan refs** | `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 2, `docs/PLAN/09-SECURITY.md` § Secure Development Practices |
| **Spec required** | No |
| **Surface** | all |

**Goal** — Verify `docs/PLAN/17`'s four Phase 2 criteria with evidence, and threat-model Phase 3 before starting it.

**Steps**
1. Verify each criterion with recorded evidence:
   - A role assigned in Project A appears in the token claim scoped to Project A only.
   - `/v1/authz/check` returns correct allow/deny for a role-based query.
   - Per-organization password policy and MFA-required settings are enforced at login, not merely stored (noting the documented Phase 3 boundary for MFA).
   - A user with no grant has zero access — least privilege verified, not assumed.
2. Load-test `/v1/authz/check` against `docs/PLAN/12`'s RBAC targets.
3. Run the Phase 3 threat-model review (`docs/PLAN/09`).
4. Write the phase summary in `MEMORY/`.
5. Update `PROGRESS.md`; tag the release; publish the changelog.

**Definition of Done**
- [ ] All four `docs/PLAN/17` Phase 2 criteria verified with evidence.
- [ ] `/v1/authz/check` meets its latency targets under load.
- [ ] The Phase 3 threat-model review is complete.
- [ ] A phase summary exists in `MEMORY/`.

---

## Phase 2 Exit Checklist

From `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 2:

- [ ] A role assigned to a user in Project A is correctly reflected in that user's access token claim, scoped to Project A only.
- [ ] `/v1/authz/check` returns a correct allow/deny decision for a role-based query.
- [ ] Per-organization password policy and MFA-required settings are enforced at login time, not just stored.
- [ ] A user with no grant has zero access by default.

Plus, from this phase's own scope:

- [ ] Multi-organization support is active with verified cross-tenant isolation.
- [ ] The token claim format matches `docs/PLAN/08` exactly, including the nested `org_id`.
- [ ] The console exposes Roles, Authorizations, Policies, and the organization switcher.
- [ ] Every capability in the console is also reachable through the REST API (FR-14).
