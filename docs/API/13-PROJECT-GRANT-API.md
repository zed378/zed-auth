# 13 - Project Grant API

> Category: **API** (`docs/API/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify REST endpoints for cross-organization Project Grants (`/v1/project-grants`).

## Category Mandate

Enables secure cross-tenant role delegation between granting and granted organizations.

## Key Topics To Specify

- `POST /v1/project-grants`: Delegate project permissions to external org.
- `GET /v1/project-grants`: List active project delegations.
- `DELETE /v1/project-grants/{id}`: Revoke cross-org grant.

## Reference Architecture & Specification

Project Grant Payload:
```json
{
  "project_id": "proj_123",
  "granted_org_id": "org_456",
  "granted_role_keys": ["viewer", "editor"]
}
```

## Acceptance Criteria

- [x] Project grant delegation API documented.
- [x] Validation against granted role subset enforced.

## Open Questions

None.

## Related Documents

- `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`
