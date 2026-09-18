# 05 - Backup and Disaster Recovery

> Category: **DevOps** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Partially implemented &nbsp;|&nbsp; Tasks: P0-20, P1-02 (incident), P3-14 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe what is backed up, how a backup is proven to be restorable, and what recovery looks like today.

## Scope

Operational backup and restore. The requirements are `docs/PLAN/15-DISASTER-RECOVERY.md`.

## As Built

### The backup itself (`deploy/vm/backup.sh`)

- Dumps the database, then **restores the dump into a throwaway database and checks the schema and row counts survived**. The script deliberately does not finish at `pg_dump`: `docs/PLAN/15` says a backup that has never been tested is not a backup, and the moment you discover otherwise is the worst possible moment.
- Modes: scheduled (`backup.sh`), a fast pre-deployment restore point (`--pre-deploy`), and verification of the newest backup (`--verify-only`).
- Prunes backups older than 30 days.
- Ships offsite, because `docs/PLAN/15` requires a separate failure domain: on a single VM, a backup on the same disk protects against `DROP TABLE` and nothing else.

### Scheduling

A systemd timer runs the service unit nightly (`deploy/vm/zed-auth-backup.timer`, `zed-auth-backup.service`). The destination is under the runtime state directory, outside the checkout, and the unit creates it with restrictive ownership before running.

### Before every deployment

A backup is taken first, and its journal and file are checked, before any migration runs. If the backup fails, the deployment stops.

### The failure this design now guards against

Backups stopped silently twice — the unit pointed at paths that no longer existed after a re-clone, and nothing alerted because nothing was failing loudly. The lesson recorded was to alert on the **absence of a recent success** rather than on failure counts, and the destination was moved outside the checkout (`MEMORY/records/` entries for `P0-20` and `P3-14`; the lesson is also recorded in the project memory as a recurring pattern).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Schedule | Nightly | `zed-auth-backup.timer` |
| Verification | Every backup is restored into a scratch database and checked | `deploy/vm/backup.sh` |
| Retention | 30 days | `deploy/vm/backup.sh` |
| Destination | Runtime state directory, outside the git checkout | unit `Environment=` and `ReadWritePaths=` |
| Deployment order | Backup first; stop on failure | `deploy/vm/deploy.sh`, `docs/DEVOPS/00-DEVOPS-OVERVIEW.md` |
| Alerting | **None today** — see Not Yet Built | `deploy/observability/alerts.yml` has 23 rules, none about backups |

## Interfaces

- `sudo systemctl start zed-auth-backup.service` — run now; `journalctl -u zed-auth-backup.service` — confirm.
- `deploy/vm/backup.sh --verify-only` — prove the newest backup restores.

## Security Considerations

- A dump contains every tenant's data: it inherits the database's confidentiality requirements, and its destination directory is created with restrictive ownership rather than inheriting whatever existed.
- Offsite copies must not weaken that; the transport and destination are operational configuration, not repository content.

## Verification

- The restore check inside every backup run.
- Staging deployments in `MEMORY/records/` cite the dump file created immediately before each migration.

## Not Yet Built / Open Questions

- **No backup alerting.** The unit writes to the journal and the script verifies its own dump, but nothing exports a success metric and `deploy/observability/alerts.yml` carries no backup rule — so a silent stop would again be noticed only by looking. This is the unfinished half of the lesson above.
- **No scheduled restore drill** into a full environment: the per-backup restore proves the dump is loadable, not that the service comes up against it.
- RPO and RTO are stated in `docs/PLAN/15-DISASTER-RECOVERY.md` but have not been measured end to end.
- Point-in-time recovery (WAL archiving) is not configured; recovery granularity is the nightly dump plus any pre-deployment restore point.

## Related Documents

- `docs/PLAN/15-DISASTER-RECOVERY.md`; `deploy/vm/backup.sh`; `deploy/vm/README.md`.
- `docs/OBSERVABILITY/` for the alerting side.
