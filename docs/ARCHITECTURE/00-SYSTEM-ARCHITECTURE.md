# 00 - System Architecture

> Category: **ARCHITECTURE** (`docs/ARCHITECTURE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail top-level system architecture, service topology, and client-server communication channels.

## Category Mandate

Defines the system as an enterprise identity platform with clean separation between REST APIs, OIDC Provider, Authorization Engine, and Storage.

## Key Topics To Specify

- High-level topology diagram.
- Public vs Internal API gateways.
- Auth Engine, OIDC Provider, and Management API subsystems.
- Storage interfaces (PostgreSQL, Redis).

## Reference Architecture & Specification

```
[ Client App / SPA ] -> [ API Gateway / Chi Router ]
                             |
       +---------------------+---------------------+
       v                     v                     v
[ OIDC Engine ]      [ Management API ]   [ Authorization Engine ]
       |                     |                     |
       +---------------------+---------------------+
                             v
              [ PostgreSQL (RLS) ]  [ Redis ]
```

## Acceptance Criteria

- [x] Service components clearly illustrated.
- [x] Primary data paths documented.

## Open Questions

Evaluate Envoy API Gateway integration for enterprise edge deployments.

## Related Documents

- `docs/README.md`
- `docs/ARCHITECTURE/01-SERVICE-BOUNDARIES.md`
- `docs/ARCHITECTURE/02-BACKEND-ARCHITECTURE.md`
