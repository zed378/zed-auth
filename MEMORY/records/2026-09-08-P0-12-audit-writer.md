# P0-12 — Audit Event Writer

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Task** | `TASKS/PHASE-0-FOUNDATION.md` P0-12 |
| **Phase** | Phase 0 — Foundation |
| **Surface** | backend |
| **Author** | Claude Code |
| **Branch** | `feat/P0-12-audit-writer` |
| **Status** | Completed |

---

## What Changed

The single entry point for recording that something identity- or permission-changing happened, plus the fix for a scheduled outage. Also: six known vulnerabilities closed, and a local gate runner that found four bugs in itself.

## Why

`docs/PLAN/09` § Audit requires every identity- or permission-changing event to be recorded, and `docs/PLAN/04` makes the table append-only at the database level. One entry point exists so no feature invents its own format — an investigator's ability to reason about the log uniformly matters more than any individual event's shape, and six variants of "a role was assigned" destroys that long before one of them is actually wrong.

## The Decision This Task Existed To Make

`P0-12` asked for a documented choice on write semantics: does the audit write live inside the business transaction, or after it?

**Inside.** `Write` takes an existing `*postgres.Tx` rather than opening its own, so the event commits with the action that caused it.

The alternative loses either way. A role assignment that succeeds without its audit record leaves a permission change nothing recorded. A record for an assignment that rolled back describes something that never happened. Both produce a log that cannot be trusted, and an untrusted log is worse than no log, because decisions get made from it anyway.

The cost is real: **if the audit write fails, the action fails.** For an identity provider that is the correct trade — `docs/PLAN/09` asks for every permission-changing event to be captured, and "captured unless the insert happened to fail" is not that.

Forwarding to a SIEM takes the opposite trade, deliberately. The database copy is the record of truth and must commit with the action; the forwarded copy is a convenience for an external system, and an unreachable SIEM must not be able to stop logins from working. The failure is logged rather than swallowed.

## Redaction Lives in the Writer

Not at call sites. The table is append-only, so a credential written into it cannot be deleted by anyone, ever — the guarantee that protects the log also makes a mistake permanent. A call site that forgets is a credential sitting in a table with a 24-month retention.

Same key list as the logger (`P0-09`), applied recursively through nested payloads, and tested against a payload that nests a refresh token two levels deep.

## The Partition Time Bomb

I flagged this in the `P0-01…P0-13` record:

> "`events` partitions are seeded two months out and nothing renews them… This becomes a hard outage — every audited action failing — roughly two months from now. This is the single most likely way the work landed today breaks in production."

Migration 000006 seeded two months and relied on something calling the function again. Nothing did. When the last partition's range ends, every `INSERT` into `events` fails — and because every security-sensitive action writes an audit event, every such action fails with it. At midnight on the first of a month, with no deploy to correlate against.

The service now maintains three months of runway at startup and daily. Three rather than one because a service that is down for a while, or a tick that fails quietly, still has room before the failure becomes an outage; an unused empty partition costs nothing.

Two details that would have been bugs:

- **Concurrency.** Several instances start at once and all call this. The original `IF NOT EXISTS` check was a race between the existence test and the `CREATE`. Now an advisory lock keyed on the partition name.
- **Inherited privileges.** A new partition must inherit the append-only revocation. Without it, a partition created next month would accept `UPDATE` and `DELETE` — and the audit log would stop being append-only for exactly the recent events an attacker would want to alter. Tested.

## `SECURITY DEFINER` Rather Than Granting `CREATE`

`auth_app` has no `CREATE` on schema public, so partition maintenance failed with `permission denied`. The one-line fix is `GRANT CREATE ON SCHEMA public TO auth_app`.

That would let the runtime role create arbitrary tables, permanently, to solve a narrow scheduled maintenance need — undoing much of what the two-role split exists for (`docs/PLAN/08` Part B: compromising the service should yield as little as possible).

`SECURITY DEFINER` is the mechanism for exactly this: expose one narrowly-scoped privileged operation to a less-privileged role. `auth_app` gains the ability to create an events partition and nothing else.

`SECURITY DEFINER` has its own hazard, and it is closed here. Such a function runs privileged code with the *caller's* `search_path` unless told otherwise, so a caller who controls `search_path` can point an unqualified name at their own object and have the owner execute it. `SET search_path` on the function pins it at definition time. `EXECUTE` is also revoked from `PUBLIC` — a `SECURITY DEFINER` function is executable by everyone by default, which would have been a worse hole than the one being fixed.

## Instance-Level Events

`events.org_id` becomes nullable. `P0-08` added an explicit cross-tenant database path whose whole purpose is to span organizations, and `docs/PLAN/08` Part B requires that path to be auditable — an event recording its use cannot carry an org, because the point is that no single organization owns the action. Signing key rotation (`P1-03`) is next.

The policy branch makes these visible only from the instance-scoped path, and deliberately does **not** let that path read every organization's events. A cross-organization audit view is a separate capability needing deliberate design (`docs/UI-UX/08`'s Instance audit log, `P1-20`); granting it accidentally here would be precisely the "normal path with the filter omitted" `docs/PLAN/08` warns against.

## Six Vulnerabilities Found and Fixed

`govulncheck` had been skipping — not installed. Installing it surfaced six standard-library vulnerabilities at go1.26.5, all fixed in 1.26.6. One is directly relevant: **`ReadHeaderTimeout` was not applied during the unencrypted HTTP/2 check**, and that timeout exists precisely to bound slow-header attacks. Also an infinite-loop bug in `golang.org/x/text` on invalid input.

Pinned the toolchain to 1.26.6, bumped the Dockerfile and CI, upgraded `x/text` to v0.39.0. Now clean.

Worth noting how close this came to being missed: the gate reported `skip` rather than `fail` when the tool was absent, and a skipped gate reads as fine at a glance.

## Files Touched

| Path | Change |
|---|---|
| `backend/internal/audit/audit.go` + test | Writer, 30 event constants, redaction, keyset pagination, partition maintenance |
| `backend/migrations/…000008…` | Nullable `org_id`, policy branch, partition runway |
| `backend/migrations/…000009…` | `SECURITY DEFINER` with pinned `search_path` |
| `backend/cmd/authservice/main.go` | Writer wired in; maintenance goroutine |
| `backend/go.mod`, `Dockerfile`, `.github/workflows/ci.yml` | Toolchain and dependency patches |
| `scripts/check.sh`, `Makefile` | Local gate runner |

## Tests Added

15 integration tests: write and read back, redaction including nested payloads, rollback with the transaction, cross-tenant claim rejection, instance-level events and their isolation from tenants, filtering, keyset pagination stability across an interleaved write, limit clamping, partition runway, concurrency safety, partition privilege inheritance, forwarder success and failure, empty-type rejection.

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Credential written into an undeletable log | `docs/PLAN/13`, `docs/SECURITY/02` §16 | `TestPayloadIsRedactedBeforeStorage` |
| Audit record for an action that did not happen | `docs/SECURITY/02` §19 | `TestEventRollsBackWithItsTransaction` |
| Event attributed to another tenant | `docs/SECURITY/02` §2 | `TestEventCannotClaimAnotherTenant` |
| Tenant reading instance-level events | `docs/PLAN/08` Part B | `TestInstanceLevelEvent` |
| Audit tampering via a new partition | `docs/SECURITY/02` §19 | `TestNewPartitionsAreAppendOnly` |
| Unbounded query against an unbounded table | `docs/SECURITY/02` §10 | limit clamping |
| Vulnerable dependencies | `docs/SECURITY/02` §15 | `govulncheck` in CI and `check.sh` |

## Definition of Done Verification

- [x] Every event-type constant documented with its payload shape
- [x] A payload containing a token or password is redacted before storage
- [x] Writes are append-only in practice
- [x] The write-semantics decision is recorded — here and in the commit
- [x] Query helper with pagination for the console screen
- [x] SIEM forwarding seam exists as a no-op
- [x] Deployed and verified on the VM

## What Did Not Work

**`scripts/check.sh` found four bugs in itself on its first run**, which is roughly what a first run should do. Three were minor: `-race` needs cgo (unavailable on this machine, works in CI), a `cd` without a guard, a stale message.

The fourth was serious. The tidiness check compared `go mod tidy`'s output against **git HEAD** and then ran `git checkout` to undo the change. With uncommitted dependency work in the tree that reported a false failure *and silently discarded the work* — it threw away the `x/text` security upgrade and the toolchain pin, twice, and I only noticed because `govulncheck` started failing again for no apparent reason.

Tidiness means "running tidy changes nothing", which is a comparison against the current files, not against HEAD. It now snapshots to a temp file and never touches git. **A check script that destroys uncommitted work is worse than no check script**, because it is trusted.

That is also the fourth verification-shaped bug in three days: two in the VM deployment, one in the RLS test, and this. The pattern is consistent — checks that look correct while measuring the wrong thing, and they are harder to notice than ordinary bugs because a green result invites no scrutiny.

**Also worth recording**: the `permission denied` on partition creation was a moment where the fast fix (`GRANT CREATE`) and the right fix (`SECURITY DEFINER`) differed by about twenty minutes of work and a permanent privilege expansion.

## Follow-Ups

- Instance-scoped access does not yet write its audit event. The writer exists now, but `postgres.WithInstanceScope` would need to depend on `audit`, and `audit` already depends on `postgres`. Breaking the cycle means a callback or an interface — small, but a design choice rather than a mechanical change. Tracked for `P0-11`.
- The SIEM forwarder is a no-op interface until `P5-08`.
- `events` retention (24 months, `OQ-09`) is still unconfirmed, and partitioning now makes acting on it cheap.

## What to Watch

**Partition maintenance is a goroutine with no supervision.** If it panics or its context is cancelled early, nothing notices, and the failure appears months later at a month boundary. The startup call logs its result, so a missing "events partitions ensured" line at startup is the signal — but nothing alerts on its absence. `P0-11` should add a metric.

**`SECURITY DEFINER` is now in the codebase**, and it is the kind of construct that gets copied. The pinned `search_path` and the `REVOKE ... FROM PUBLIC` are what make it safe; a future function that copies the pattern without both is a privilege escalation.

**The in-transaction write means a failing audit table takes the service down.** That is the intended trade, but it should be a conscious one: if `events` becomes unwritable — disk full, a missing partition, a lock — logins stop. The partition maintenance above removes the most likely cause; disk monitoring should cover the next.
