# 04 - Database Migrations Strategy

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify database migration tooling (`golang-migrate`), versioning conventions, and zero-downtime deployment rules.

## Category Mandate

Ensures safe schema changes without locking tables or interrupting active traffic.

## Key Topics To Specify

- Sequential numbered migration files (`000001_create_orgs.up.sql`, `000001_create_orgs.down.sql`).
- Non-blocking schema change rules (ADD COLUMN nullable first, populate, then set NOT NULL).
- Automated migration execution during CI/CD deploy.

## Reference Architecture & Specification

Migration Rule: Destructive drop column/table operations are deferred until code dependent on them is completely decommissioned.

## Acceptance Criteria

- [x] Migration tool & naming convention specified.
- [x] Zero-downtime migration guidelines documented.

## Open Questions

None.

## Related Documents

- `docs/DEVOPS/03-CICD-PIPELINE-SPECIFICATION.md`
