# P2-01 — Role Model and Permission Keys

**Task**: `TASKS/PHASE-2-RBAC-MULTITENANCY.md` § P2-01
**Depends on**: `P1-17`
**Plan refs**: `docs/PLAN/08-AUTHORIZATION.md` Part A, `docs/PLAN/04-DATA-MODEL.md` § `roles`, `docs/PLAN/11-TESTING.md` § Unit Testing

---

## 1. Business Objective

Phase 1 proved who a user is. This is the first half of proving what they may do.

A role is a named bundle of permission keys, scoped to one project. The scoping is the entire point: `docs/PLAN/08` Part A opens with it, and the failure it prevents is the one nobody notices until it is an incident — an `admin` role created for the internal tools project quietly granting `admin` in the billing project, because both are called `admin`.

Nothing consumes roles yet. `P2-03` grants them to users, `P2-04` puts them in the token, `P2-06` answers questions about them. This task builds the vocabulary those three depend on, which is why it is worth getting the shape right while there is no data to migrate.

## 2. Actors

| Actor | What they do here |
|---|---|
| `ORG_OWNER`, `ORG_ADMIN` | Create and edit roles in any project in their organization |
| `PROJECT_OWNER` | Create and edit roles in their own project |
| The service itself | Marks a role `is_builtin`, which nobody can then rename or delete |
| A consumer application | Reads permission keys out of a token and decides for itself (`P2-04`) |

`P2-02` puts the API in front of this; authorization at the endpoint is its task. This one owns the store and the rules the store enforces regardless of who is calling.

## 3. Functional Requirements

- **FR-1** A role has `project_id`, `key`, `display_name`, `permission_keys text[]`, `is_builtin`.
- **FR-2** `key` is unique within a project and may repeat freely across projects.
- **FR-3** A permission key matches a documented format, validated by one function.
- **FR-4** A role marked `is_builtin` cannot be renamed, re-keyed, or deleted.
- **FR-5** A role key may not collide with a reserved name.
- **FR-6** Deleting a role that grants reference is **refused**, not cascaded.
- **FR-7** Create, update and delete each write an audit event.
- **FR-8** A role in Project A grants nothing in Project B, enforced by the schema rather than by the caller remembering.

## 4. Non-Functional Requirements

- **NFR-1** `roles` carries `org_id` and a row-level security policy, like every other tenant-scoped table (`docs/PLAN/08` Part B). `check.sh` gates this.
- **NFR-2** Permission key validation is a pure function with no I/O, so the console and the API cannot drift — see §9.
- **NFR-3** Listing a project's roles is one query.

## 5. Dependencies

| Needs | Provided by |
|---|---|
| `projects` and its tenancy | `P1-17` |
| Audit writer | `P0-12` |
| `WithTenant` and the runtime role | `P0-08` |
| Migration tooling | `P0-05` |

## 6. Database Changes

**The table already exists.** `P0-07` created the full Phase 0/1 schema, and `roles` has been in it since 2026-09-08 with `project_id`, `org_id`, `key`, `display_name`, `permission_keys text[]`, `is_builtin`, a `UNIQUE (project_id, key)` index, an `updated_at` trigger, and row-level security on `org_id`. Discovering that before writing a migration is the difference between this task and a second, conflicting definition of a role.

So the schema work here is the part Phase 0 could not know: the rules, not the shape. One additive migration adds three.

**The permission key format, as a constraint.** Nothing validates the array today — a role can carry `{"' OR 1=1"}` or an empty string. An `IMMUTABLE` function over the array plus a `CHECK`, because a `CHECK` cannot contain a subquery and cannot call a volatile function (`P1-29` learned both).

**The org/project agreement, as a trigger.** `roles` carries both `org_id` and `project_id` and nothing makes them agree. A caller with a valid `org_id` and another organization's `project_id` writes a role into the wrong tenant — and RLS then *hides* it from the organization that actually owns the project, which is worse than a visible error. The invariant spans two tables, so it cannot be a `CHECK`.

**Built-in immutability, as a trigger.** `docs/PLAN/04` says built-in roles cannot be renamed or deleted. Enforced in the database rather than only in the store, because the rule protects against a future writer that the store does not mediate — and `P1-20` already learned that "the application does not do that" is not a constraint.

**What the existing key shape is, and why it stays.** `roles_key_shape` is `^[a-z0-9][a-z0-9_-]{0,62}$` — it permits hyphens and a leading digit, where this spec's first draft proposed neither. The deployed constraint wins: its comment gives a real reason (role keys appear inside JWT claim keys, so the character set bounds what a role name can do to a token's shape), `read-only` is a legitimate role name, and no data exists that a tightening would clean up. A schema already reasoned about is not re-litigated to match a draft written later.

`created_at`/`updated_at` default to `now()` — the database's clock — and the store does not pass its own. That is correct *here*, and it is worth naming why, given `P1-28`: nothing on this table compares the two columns, so there is no CHECK whose meaning depends on them sharing a clock. The rule from that incident is "one row, one clock **when a constraint compares them**", not "always override the default".

## 7. API Contract

None. `P2-02` adds `/v1/organizations/{org_id}/projects/{project_id}/roles`, and the OpenAPI spec changes there — this task ships no endpoint, so the spec is untouched and the generated router does not move.

The **permission key pattern does** go into the spec now, as a reusable schema, because it is the one thing two surfaces must agree on. See §9.

## 8. Frontend Changes

None yet. `P2-11` builds the Roles tab. What this task owes the console is the pattern its form validation will use, published where the console's generator can see it.

## 9. Backend Changes

**`internal/role`**, following the shape `internal/project` and `internal/application` already use: a `Role` domain type, a `Store` that takes no `org_id` argument because `WithTenant` supplies it, and pure validation with no database.

**Permission key format.** `resource:action`, from `docs/PLAN/08`'s own examples (`user:read`, `billing:write`). Formally:

```
^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*:[a-z][a-z0-9_]*$
```

A dotted resource (`billing.invoice:read`) is allowed because consumer applications will want a namespace and would otherwise invent one with a character we did not anticipate. `*` is **not** allowed: a wildcard in a permission key is an authorization decision hiding in a string, and if wildcards are wanted they belong in the decision engine (`P2-06`) where they can be reasoned about, not in stored data where every consumer must reimplement the match.

**The pattern has one definition.** `docs/PLAN/11` names role key format as a unit-test target and `P2-01` step 2 says the regex is "documented and shared with the console's form validation". Shared, for a regex, usually means copied — so:

1. `openapi/openapi.yaml` gains a `PermissionKey` schema carrying the pattern. The spec is already the contract's source of truth (ADR-013).
2. Go reads it from a generated constant, not a hand-typed literal.
3. The console reads it from the generated client.
4. `check.sh` asserts the three are byte-identical, and the gate is mutation-tested — because a gate comparing a value against itself is the failure mode this project has already shipped twice.

**Reserved keys.** `admin` is *not* reserved: it is the most natural role name a consumer application will want, and reserving it to prevent confusion with manager roles would trade a real need for a theoretical one. Reserved instead are the five `manager_roles` values in lower case (`instance_owner`, `org_owner`, `org_admin`, `project_owner`, `project_grant_owner`), because a project role named `org_admin` would appear in a token beside a manager role of the same name meaning something entirely different.

**No built-in roles are seeded.** `docs/PLAN/08` Part A names `org_owner` and `org_admin` as the built-in examples, and both are `manager_roles` — a different table, a different scope, already implemented. Recorded as **`PG-30`**; the mechanism ships, the two specific roles do not.

## 10. Authorization Rules

Enforced here (the store, regardless of caller): tenancy via RLS, the org/project agreement trigger, and the built-in immutability rule.

Enforced by `P2-02` (the endpoint): `PROJECT_OWNER` or higher. The store does not check manager roles, for the same reason `P1-17`'s store does not — the store is a shape, and a permission check that lives in two places is a permission check that disagrees with itself eventually.

## 11. Validation

| Field | Rule | On failure |
|---|---|---|
| `key` | matches the shipped `roles_key_shape`, `^[a-z0-9][a-z0-9_-]{0,62}$`, and is not reserved | field error naming `key` |
| `display_name` | 1–128 characters after trimming | field error |
| `permission_keys` | each matches the permission pattern; ≤ 256 entries; no duplicates | field error naming the **index** and the offending value |
| `project_id` | exists, and belongs to the caller's organization | `404`, not `403` — see §12 |

## 12. Error Handling

A project in another organization is **not found**, never forbidden. `403` tells the caller the id is real, which is the enumeration oracle `docs/SECURITY/02` §12 is about and which `P1-16` already settled for organizations.

Deleting a role with grants returns a conflict naming the count, not the grant holders — the count is what the operator needs, the identities are somebody else's tenant data in the Project Grant world `P4-01` builds.

## 13. Edge Cases

- **The same key in two projects** — permitted, and the test asserts they carry different permissions, because "unique per project" is easy to implement as "unique" by accident.
- **An empty `permission_keys`** — permitted. A role with no permissions is a label, and labels are useful before the permissions exist.
- **Duplicate keys within one role** — rejected rather than de-duplicated. Silently changing what the caller sent means the response does not match the request.
- **A 63rd character in a key** — the bound exists so the key fits a Postgres identifier's length if anyone ever generates one from it.
- **Deleting the project** — cascades, by FK. The roles are meaningless without it.
- **Renaming `display_name` on a built-in role** — permitted. `docs/PLAN/04` says built-ins cannot be *renamed or deleted*, and the name that matters for that is `key`, which is what grants reference; `display_name` is presentation.

## 14. Abuse Cases

Cross-referenced against `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`:

| # | Scenario | Control | Test |
|---|---|---|---|
| A-1 | §3 Privilege escalation — create a role in another organization's project by supplying its `project_id` | The trigger refuses the mismatch; RLS hides the project in the first place | `TestARoleCannotBeWrittenIntoAnotherOrganizationsProject` |
| A-2 | §2 IDOR — read or edit a role by id from another tenant | RLS; the store takes no `org_id` to get wrong | `TestRolesAreInvisibleAcrossTenants` |
| A-3 | §3 — shadow a manager role by creating a project role named `org_admin` | Reserved key list | `TestManagerRoleNamesAreReserved` |
| A-4 | §11 Business logic — delete a role that grants depend on, silently removing access | Delete is refused while referenced | `TestDeletingAReferencedRoleIsRefused` (lands with `P2-03`, which is what creates a reference) |
| A-5 | §3 — a permission key like `*` or `user:*` granting more than it appears to | `*` is not in the pattern | `TestPermissionKeyRejects` |
| A-6 | §19 Audit integrity — change a role without leaving a trace | Every write audits, against a real database | `TestRoleChangesAreAudited` |

A-4's test is written now and marked to land with `P2-03`: the rule is implemented here, and the only way to create a reference is the grant table `P2-03` adds. Named rather than left implicit, because "we will test it when the dependency exists" is how `P1-13`'s lockout event went unwritten for two tasks.

## 15. Logging / Audit Requirements

`role.created`, `role.updated`, `role.deleted`, carrying `project_id`, `key`, and for updates the permission keys **added and removed** rather than the whole new array — a diff answers "who gave this role billing access" in one read, where a snapshot needs two rows and a comparison.

No permission key is secret, so nothing here is redacted. The role's `display_name` is user-controlled text and is bounded before it is written, so an audit payload cannot be used as storage.

## 16. Security Controls

- RLS on `roles`, asserted by the existing `check.sh` gate and by an isolation test using the **runtime** role.
- The org/project trigger, tested by attempting the write the trigger exists to stop.
- Reserved keys and the permission pattern as pure functions, fuzzed — `P1-27`'s `FuzzVerify` found nothing, and finding nothing is the result a fuzz target is for.

## 17. Testing Strategy

| Layer | What |
|---|---|
| Unit | The pattern and reserved-key functions: a table of accept/reject cases including every boundary the regex has, plus a fuzz target asserting no input panics and nothing outside the pattern is accepted |
| Integration | Uniqueness per project, repetition across projects, the trigger, RLS from the runtime role, built-in immutability, audit rows against a real database |
| Contract | The `PermissionKey` schema exists in the spec and matches the Go constant and the console's — mutation-tested |

## 18. Acceptance Criteria

1. A role key is unique within its project and repeats freely across projects.
2. Permission key validation is one pure, unit-tested function, used by both surfaces and gated against drift.
3. A built-in role cannot be deleted or re-keyed.
4. Deleting a referenced role is refused.
5. A role in Project A grants nothing in Project B, proved by a test that would pass if the scoping were absent — so the test asserts the *absence* of the Project B permission, not merely the presence of the Project A one.

## 19. Definition of Done

The card's five boxes, plus: the migration is reversible, `check.sh` is green, and the abuse-case table above has a test per row or a named task that will add it.

## 20. Implementation Sequence

1. Migration: table, constraints, trigger, RLS, index.
2. `internal/role`: types, validation, fuzz target.
3. `PermissionKey` in the OpenAPI spec; the drift gate; mutate the gate to prove it fails.
4. Store: CRUD, tenancy, built-in immutability, delete refusal.
5. Audit events, asserted against a real database rather than a fake.
6. Isolation tests as the runtime role.

## 21. Rollback Strategy

The down migration drops the table. Nothing reads it before `P2-03`, so a rollback during Phase 2 loses only roles created since the deploy — and `P2-03` will not ship until this is settled. After `P2-03`, dropping `roles` would orphan grants, which is why the FK direction has grants referencing roles and not the reverse.

## 22. Technical Risks

**The permission key format is a one-way door.** Consumer applications will store these strings and branch on them. Widening the pattern later is harmless; narrowing it breaks deployed code we cannot see. That asymmetry is the argument for excluding `*` now — it can be added, and it cannot be taken back.

**`is_builtin` with nothing built in** looks like dead code, and a future reader may delete the mechanism. The migration comment and `PG-30` say why it exists; the immutability test keeps it honest by exercising it on a row the test marks itself.
