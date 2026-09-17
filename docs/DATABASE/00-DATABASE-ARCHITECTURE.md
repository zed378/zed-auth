# 00 - Database Architecture Overview

> Category: **DATABASE** (`docs/DATABASE/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify PostgreSQL database engine setup, connection pooling (`pgxpool`), and storage layout.

## Category Mandate

Delivers reliable, ACID-compliant persistence for all identity and authorization state.

## Key Topics To Specify

- PostgreSQL 15+ database engine.
- Connection pooling via `jackc/pgx/v5/pgxpool` (max connections, idle timeout).
- Parameterized query execution exclusively — zero concatenated SQL strings.

## Reference Architecture & Specification

Connection Pool Settings:
- Max Connections: 50 per replica
- Min Idle Connections: 10
- Max Conn Lifetime: 1 hour

## Acceptance Criteria

- [x] PostgreSQL engine version specified.
- [x] Connection pool parameters defined.

## Open Questions

None.

## Related Documents

- `docs/ARCHITECTURE/02-BACKEND-ARCHITECTURE.md`
