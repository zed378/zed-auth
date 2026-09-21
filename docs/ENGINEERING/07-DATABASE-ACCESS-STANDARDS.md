# 07 - Database Access Standards

> Category: **Engineering Practice** (`docs/ENGINEERING/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-04, P2-01…P2-03, P4-01…P4-06 &nbsp;|&nbsp; Verified against: `38863ee`

## Purpose

How code reaches PostgreSQL: which role, inside what transaction, with what SQL, and the
rules migrations follow. The schema itself is [`../DATABASE/`](../DATABASE/); the tenancy
model is [`../MULTI-TENANCY/`](../MULTI-TENANCY/).

## Scope

`backend/internal/storage/postgres/`, every package's `store.go`, `backend/migrations/`.

## As Built

### Two roles, and the service never holds the owner's

| Role | Used by | Rights |
|---|---|---|
| `auth_owner` | `backend/cmd/migrate` | DDL, policy creation, ownership |
| `auth_app` | the service | DML only, subject to row-level security |

`scripts/check.sh` asserts the service container receives no owner credentials. The
separation is what makes RLS meaningful: a role that can `ALTER TABLE … DISABLE ROW LEVEL
SECURITY` is not constrained by it.

### Every tenant-scoped read or write happens inside `WithTenant`

```go
h.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error { ... })
```

It opens a transaction and sets the tenant before anything else runs:

```go
tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', $1, true)`, orgID)
```

`set_config` with a **parameter**, not `SET LOCAL` with interpolation, even though the
value comes from trusted code: `SET LOCAL` is not parameterisable, so building it by hand
would be the one place in the package where a value reaches a statement as text.

Instance-scoped work passes the empty string, which makes `current_org_id()` return NULL,
which every policy evaluates as false. Instance work therefore cannot read a
policy-protected table by accident — it has to query something not under RLS, or be the
owner. That is a deliberate second line of defence behind the naming.

### The query filters on the tenant column *as well as* RLS

RLS bounds what is **visible**. The query decides what is **relevant**. For a table both
parties can see — `project_grants` has a two-sided policy — the difference is the whole
control:

- The granting side's list filters on `granting_org_id`. A mutation removing it let the
  receiving organization revoke a grant it could see through a project of its own (`P4-01`).
- The receiving side's list filters on `granted_org_id`. A mutation widening it to either
  side made the grants this organization *made* appear in its *received* list (`P4-06`).

**Visibility is not authority.** It is the single most repeated lesson in this codebase.

### Parameterised SQL, always

No string-concatenated queries anywhere, including for identifiers. `AGENTS.md` states it,
`docs/SECURITY/02` §8 is the source, `gosec` and review enforce it.

Array parameters go through `pq.Array`. Batched lookups take `$1::uuid[]` and a single
array rather than a loop of queries.

### Secondary data is batched, never fetched per row

`ListReceived` fetches a page, then resolves names with one call and holder counts with
one more, both over the page's ids. A per-row lookup is an N+1 that only shows up under a
partner with many grants.

### Rules enforced in more than one place

A rule that matters is enforced at the store **and** in the database:

| Rule | Store | Database |
|---|---|---|
| A delegated assignment is a subset of the grant | checked with the grant read `FOR SHARE` | trigger, firing on every column the rule reads |
| A grant only narrows | checked on update | trigger refusing widening or reactivation |
| A tenant sees only its own rows | query filter | RLS policy |

The trigger is not redundancy for its own sake: a future writer that bypasses the store
still meets it. `P4-02` found the first version of its trigger fired on `project_grant_id`
only, so a delegated row could have been widened by the direct `PATCH`.

### Policy shape affects the query plan

A permissive RLS policy is OR-ed with the others. A **correlated `EXISTS`** inside a policy
defeats indexing and produces a sequential scan at scale; an **uncorrelated `IN (SELECT …)`**
lets the planner bitmap-OR two indexes. `P4-04` rewrote a policy for exactly this, and
`backend/tests/security/rls_plans_test.go` asserts the plan at scale — which required
seeding delegation, because an empty table plans differently.

### Migrations

- **Paired.** Every `up` has a `down`; the gate fails otherwise.
- **Additive by default** (`docs/PLAN/14`'s expand/contract). A migration must not break a
  running previous version mid-rollout.
- **Destructive migrations carry an `EXPAND/CONTRACT` note** explaining the rollout order.
  The gate greps for `DROP COLUMN`, `DROP TABLE`, `RENAME` and `ALTER COLUMN … TYPE` and
  fails if the note is absent.
- **`SECURITY DEFINER` functions set `search_path` and are bounded in their own `WHERE`.**
  RLS does not apply to them, so their predicate is the whole control. `REVOKE ALL … FROM
  PUBLIC` then `GRANT EXECUTE … TO auth_app`.

### One clock per row

A `CHECK` comparing two timestamps only means what it says if one clock wrote both.
`idempotency_records` took `expires_at` from the caller and `created_at` from the column
default, so `expires_at > created_at` was really asserting "the caller's clock is less than
a day behind the database's". It held for three days, and then the integration suite —
whose clock is fixed — could not insert anything at all. Nothing had changed; the tests had
simply expired.

The gate now finds every table with such a `CHECK` and requires shipped code to write
`created_at` explicitly. Its first version derived the table name from the constraint and
looked for a table that does not exist, found nothing, and passed while the bug was in the
tree — so **a table it cannot find an `INSERT` for is now a failure, not a silence.**

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Service role | `auth_app`, never the owner | `scripts/check.sh` |
| Tenant scope | `WithTenant`, parameterised `set_config` | `backend/internal/storage/postgres/postgres.go` |
| Tenant filter in the query | required, in addition to RLS | review, mutation testing |
| SQL | parameterised only | `AGENTS.md`, `gosec` |
| Every tenant-scoped table | has RLS | `scripts/check.sh` |
| Audit relations | append-only for `auth_app` | `scripts/check.sh` |
| Migrations | paired; destructive ones justified | `scripts/check.sh` |
| `SECURITY DEFINER` | fixed `search_path`, bounded predicate, no PUBLIC execute | migration review |
| Timestamps in a `CHECK` | written by one clock | `scripts/check.sh` |

## Verification

- `backend/internal/storage/postgres/rls_integration_test.go` — policies hold.
- `backend/tests/security/isolation_test.go` — cross-tenant isolation, with a coverage map
  a renamed test fails.
- `backend/tests/security/rls_plans_test.go` — RLS does not force sequential scans at scale.
- `scripts/check.sh` — RLS presence, audit append-only, migration pairing, clock checks.

## Related Documents

- [`05-HANDLER-AND-STORE-TEMPLATES.md`](./05-HANDLER-AND-STORE-TEMPLATES.md)
- [`../DATABASE/`](../DATABASE/)
- [`../MULTI-TENANCY/`](../MULTI-TENANCY/)
