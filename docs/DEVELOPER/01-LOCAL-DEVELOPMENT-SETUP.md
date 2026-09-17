# 01 - Local Development Setup

> Category: **DEVELOPER** (`docs/DEVELOPER/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify step-by-step setup for local development using Docker Compose, Go, and Node.js.

## Category Mandate

Delivers a zero-friction local development environment setup.

## Key Topics To Specify

- Prerequisites: Go 1.22+, Node.js 20+, Docker & Docker Compose.
- `docker-compose up -d` (spins up local PostgreSQL and Redis).
- Database migration execution (`go run ./cmd/migrate up`).

## Reference Architecture & Specification

Local Setup Workflow:
```bash
docker-compose up -d
cd backend && go run ./cmd/server
cd console && npm run dev
```

## Acceptance Criteria

- [x] Docker Compose setup specified.
- [x] Local backend & frontend execution steps documented.

## Open Questions

None.

## Related Documents

- `docs/DEVELOPER/00-GETTING-STARTED.md`
