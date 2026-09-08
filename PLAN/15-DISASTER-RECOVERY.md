# 15 — Disaster Recovery

Auth Service's data is the "source of truth" for the identity and access rights of every organization it serves — losing it is one of the most severe possible incidents for the whole platform, not just for this service.

## Backup Strategy

- **Automated PostgreSQL backups**: snapshots + WAL archiving for point-in-time recovery.
- Backups stored in a separate failure domain (different region/account) from the primary database.
- Signing keys backed up separately from the database, with restricted access (`09-SECURITY.md`).

## Recovery Objectives

| Metric | Target | Notes |
|---|---|---|
| **RPO** (Recovery Point Objective) | To be set based on WAL archiving frequency — aim for minutes, not hours, given this is identity data | Every lost transaction here means lost/incorrect access records |
| **RTO** (Recovery Time Objective) | To be set based on business tolerance for an all-consumer-apps authentication outage | Since every consumer app depends on this service, RTO here effectively becomes the platform's RTO |

Set concrete numbers once consumer-app SLAs are known; don't leave these as placeholders in the production runbook.

## Restore Testing

- **Regularly test restores** — a backup that's never been tested isn't a backup you can rely on.
- Restore drills should be run against an isolated environment (not staging or production) and validate: data integrity, correct point-in-time, and that signing keys restored alongside the DB are the matching set (a mismatched key/DB pair after a restore is a subtle and dangerous failure mode).

## High Availability

- **Multi-AZ** deployment minimum for production (`14-DEPLOYMENT.md`).
- Evaluate multi-region deployment only if a cross-region HA requirement is confirmed — don't build it speculatively.

## Incident Runbook (Outline)

1. Detect (via alerts in `13-OBSERVABILITY.md`) that the primary datastore is unavailable or corrupted.
2. Fail over to a standby/replica if available; otherwise begin restore from the latest verified backup.
3. Once restored, verify signing key consistency before resuming traffic (see §"Restore Testing").
4. Resume traffic gradually, monitoring the metrics in `13-OBSERVABILITY.md` for anomalies before declaring the incident resolved.
5. Write a post-incident review, feeding any newly identified gaps into `18-RISK-REGISTER.md`.

## Disaster Recovery Drill Cadence

- At minimum once before the Phase 5 hardening milestone (`16-IMPLEMENTATION-ROADMAP.md`), and periodically afterward (e.g. annually or after any major infrastructure change).

Continue to [16 — Implementation Roadmap](./16-IMPLEMENTATION-ROADMAP.md).
