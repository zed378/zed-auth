# A Verified Restore, and a Verification That Was Weaker Than It Sounded

**Date**: 2026-09-08
**Task**: `P0-20` (partial — see Outstanding)
**Branch**: `feat/P0-20-staging-backups`

---

## The Operational Event

**A backup of the staging database was restored successfully and verified against the source.** `docs/PLAN/15` § Restore Testing puts it plainly: "a backup that's never been tested isn't a backup you can rely on", and `P0-20`'s Definition of Done asks for a restore executed at least once with the result recorded here.

| | |
|---|---|
| Backup | `staging-20260908T141756Z.dump`, 88,838 bytes |
| Restored into | a throwaway database, dropped afterwards; the live database was never touched |
| Tables | 19 — the full set, matching the source exactly |
| Row counts | every table matched the source |
| Result | success, exit 0 |

The restore is now also **automated**: `zed-auth-backup.timer` runs daily at 03:15 UTC with `Persistent=true`, so a VM that was off overnight backs up at boot rather than skipping the day. `P0-20` step 7 asks for automated backups, and a backup taken by hand is taken until the week somebody is busy.

---

## The Verification Was Weaker Than Its Own Message

The first run reported `all 14 tables restored`. The live database has 19.

That was not a missing-data bug — 19 is 14 base tables plus 4 event partitions plus `schema_migrations`, and the dump was complete. It was a **reporting** bug, and a consequential one: the script checked a hardcoded list of fourteen table names, so "all 14 tables restored" meant *"all fourteen I was told to look for"* while reading like completeness.

Two things followed from that:

**A table added by a future migration would not have been checked.** Phase 1 adds several. The backup could have silently omitted one and the verification would still have printed a confident line.

**The event partitions were not checked at all** — in a script whose own comment says the audit log is "the thing most easily lost to a partition that was not dumped". It checked `count(*) FROM events`, which reads through the partitions, so a missing partition would have shown up as a smaller number that nothing compared to anything.

It now enumerates the tables in the **source** and compares. Same for row counts, per table — a restored table that arrives empty is a restore that looks fine and has lost everything. The hardcoded list survives as a *floor*, checked against the source rather than the restore: without it, set comparison would pass trivially if both databases were empty, which is the vacuous pass this project has now met six times.

The new message is `19 tables restored, row counts match the source`, which is a claim about completeness rather than about a list.

**Verified by breaking it.** A table was created in the live database after the newest backup was taken; the verification failed with `this backup is incomplete` and named `backup_drift_probe`. The table was dropped afterwards.

---

## What `P0-20` Still Does Not Have

Three of the five Definition-of-Done items are done. The other two are not mine to close.

**Continuous deployment on merge to `main` is not set up.** It needs GitHub Actions to reach the VM, which means a deploy credential held by GitHub and a path into a machine that is currently reachable only through a tunnel. That is a decision about trust and exposure rather than a configuration task, and it is the owner's to make. Raised as `OQ-11`.

**`AUTH_BACKUP_REMOTE` is unset, so backups are local only.** The script warns about it on every run, and the warning is right: a backup on the same disk as the database protects against `DROP TABLE` and against nothing else. Losing the VM loses the database and every backup of it together — which is exactly the failure domain `docs/PLAN/15` requires backups to be outside of. Where they should go is a decision with a cost attached. Raised as `OQ-12`.

Two more items are **vacuously satisfied and should not be ticked as though they were achieved**: "staging signing keys and database are provably distinct from production" and "production promotion requires an explicit human action" are both trivially true because there is no production environment. They become real requirements when one exists, and marking them done now would mean nobody checks them then.

---

## Outstanding

- `OQ-11` — continuous deployment to staging: does GitHub Actions get a path to the VM, and on what terms?
- `OQ-12` — where do backups go? Until then, one disk holds the database and every copy of it.
- The standing item from `P0-14`: the VM password appeared in a chat transcript and should be rotated.
