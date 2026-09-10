# P1-20 — Management API: Audit Log Read

**Date**: 2026-09-10
**Branch**: `feat/P1-19-users` (landed with `P1-19`)
**Spec**: none — the card is `Spec required: No`. It adds one read over a table `P0-07` and `P0-12` already own.

---

## What this is

One endpoint:

```
GET /v1/organizations/{org_id}/events
```

Filters for event type (repeatable), actor, and a time range, on top of `P1-15`'s pagination envelope. No migration: `events` has been partitioned, indexed and append-only since `P0-07`.

## One operation, and the test is against the router

The card's third DoD item is that no mutating operation exists. **"We did not write a delete handler" is a statement about intent**; what a caller meets is what the router registers, and that is generated from the contract.

So the assertion walks the mux and counts operations whose route mentions `/events`. Exactly one, and it must be a `GET`. A contract that grew a write here fails the test rather than quietly becoming a capability. It lives in `internal/httpserver`, where the mux is reachable — the first draft put it in `internal/auditlog` and could not see the field.

Two more layers underneath, both asserted:

- **Every other method over the wire** answers `405` or `404`, with the read as a control so the refusals are about the method rather than a missing route — and nothing forged appears in the table afterwards.
- **The application role cannot rewrite history even directly.** `UPDATE` and `DELETE` against `events` both fail as `auth_app`. That is `P0-07`'s guarantee rather than this task's — and on staging it was not in force; see below — and it is what makes the absence of a write endpoint meaningful rather than decorative: without it, "there is no endpoint" would be a promise instead of a privilege.

## Newest first, alone in this API

Every other list orders `created_at ASC`. This one is `DESC`, and `events_org_created_at_idx` is `(org_id, created_at DESC)` because `P0-07` expected exactly this access pattern.

An audit log is read from the end. The question is almost always "what just happened", and paging from the beginning of a table that grows without bound answers it slowly and last.

The cursor carries the id as well as the timestamp, because `events` is partitioned on `created_at` and its primary key is the pair. A test writes six events at **one instant** and walks them in pages of two: without the id in the comparison they would repeat or vanish.

For the same reason the API's `id` is `<id>:<timestamp>` rather than a bare number. A bare id is not unique across partitions, and a consumer treating it as one would eventually deduplicate two real events into one — which for an audit log is precisely the failure that matters.

## The tenant boundary, across combinations nobody thought of

The card's step 3 says no filter combination can reach across tenants. That is a claim about combinations, so the test enumerates eleven of them — including the ones designed to be worst: the neighbour's log is seeded with **the same event type, the same actor id and the same instant** as ours, so nothing but the tenant separates them.

No `org_id` predicate appears anywhere in the query. The transaction is scoped and RLS supplies it, which is what makes the claim true for every combination rather than for the ones somebody remembered.

And the control: the unfiltered read returns our own event, so the eleven absences are not an endpoint that returns nothing.

## Small decisions

- **An empty time range is a `400`, not an empty page.** `to` equal to or before `from` is almost always clock arithmetic gone wrong, and answering it with an empty page reads as "nothing happened" — the one answer an audit log must not give by accident.
- **`event_type` is bounded at twenty per query.** A filter is a convenience; an unbounded `IN` list is a way to make the planner do arbitrary work on a table with no ceiling.
- **An event with no actor is normal**, not an error: a failed login against an address that does not exist has none, and neither does a scheduled job. The field is omitted rather than null, and a test asserts such an event is readable without the filter and matches no value of it.
- **The payload's shape is not constrained by the contract.** Constraining it would mean a schema change for every new event type, and the console renders it as data rather than parsing it.

## Found while building it

**A payload that is not an object cannot exist.** The test tried to store `"not an object"` to prove the reader tolerates a malformed payload, and the database refused it — `events_payload_object` is a `CHECK`. That is a better answer than the one the test expected.

`decodePayload` still returns nil rather than failing, and that stays: defence in depth for a state the schema prevents costs nothing, and an investigator reading a page should not lose it to one row's detail. What the test asserts now is the guarantee that makes the fallback unreachable, plus the fallback itself as a unit.

**`events` has no partitions in the past.** The latency test backdated five thousand rows by thirty days and got "no partition of relation events found": `P0-12`'s maintenance creates partitions three months **ahead**, not behind. The bulk insert moved inside the current month.

## The budget, measured

`docs/PLAN/12` gives the Management API p50 < 100ms. The test writes five thousand events, warms the plan, then times five filtered reads.

The bound in the assertion is 300ms rather than 100ms, deliberately: this runs in a container on a developer machine next to whatever else is running, and a tight bound would be a flaky test rather than a stricter one. What it catches is a sequential scan, which over five thousand rows is orders of magnitude away rather than a few milliseconds. The measured figure is logged so a regression is visible even when it passes.

## Deployed, and the append-only guarantee was not holding

Rolled out to staging as part of `687a490`. The smoke test asked
`has_table_privilege('auth_app', 'events', 'UPDATE')` and got **true** — along with `DELETE`, on the parent and on all four live partitions. The audit log this task exposes was fully writable by the service role.

Not a bug in a migration. Both migrations that revoke those privileges read correctly, and a partition created today gets exactly `SELECT` and `INSERT`. The cause is that **golang-migrate never runs an applied version again**, so a database migrated before the `REVOKE` was written keeps what `ALTER DEFAULT PRIVILEGES` granted at `CREATE TABLE`. The repository described one thing and the deployment was another, silently.

`20260910000019` repairs it and refuses to finish if it did not. `check.sh` now asserts it on every run, and that gate is proven to fail.

**This is the sentence in this record that needed correcting.** "Append-only at the database level" is not quite the claim: it is append-only **for the application role**, by `REVOKE UPDATE, DELETE ON events FROM auth_app`. The owner retains everything, necessarily — it runs the migrations. So the guarantee is real and it is precisely scoped, and the scope matters: anyone holding the owner DSN can rewrite history, and the deploy path uses it.

## Verification

- **Integration, 14 tests**: every write method refused with the read as a control and nothing forged in the table; the application role unable to `UPDATE` or `DELETE`; eleven filter combinations against a deliberately identical neighbour, with the control; filtering by type, by two types, by actor, and by a half-open time range; the empty range refused; descending order; a full walk seeing each entry once; six events at one instant paging without repeating; a non-admin and a `PROJECT_OWNER` refused with the control; the payload returned as data; the `CHECK` that makes a malformed payload impossible plus the fallback as a unit; and the latency measurement.
- **Unit** (`internal/httpserver`): the router serves exactly one operation under `/events`, and it is a `GET`.

## What this task did not build

- **The console's Audit Log screen.** `P1-24` owns it.
- **Export.** A CSV or NDJSON download of a filtered range is a reasonable next thing to want and is not in the card.
- **Instance-level reads.** `events` carries rows with no `org_id` — signing key rotation, for one — and there is no endpoint for them. `/v1/instances/{instance_id}` has no Phase 1 task, which is the same gap `P1-16` noted.
