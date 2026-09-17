# 01 - Organization & Project Hierarchy Specification

> Category: **MULTI-TENANCY** (`docs/MULTI-TENANCY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail Organization and Project entity attributes, ownership rules, and member scoping.

## Category Mandate

Defines operational boundaries for tenants, projects, and member privileges.

## Key Topics To Specify

- Organization creation & ownership assignment.
- Project scoping (Applications and Roles defined per Project).
- Member organization membership (`users.org_id`).

## Reference Architecture & Specification

Rule: A Role named `admin` in Project A is entirely distinct from a Role named `admin` in Project B.

## Acceptance Criteria

- [x] Entity attributes documented.
- [x] Scoping boundaries enforced.

## Open Questions

None.

## Related Documents

- `docs/DATABASE/01-SCHEMA-DEFINITIONS.md`
