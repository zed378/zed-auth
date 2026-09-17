# 00 - Project Context

> Category: **PLAN** (`docs/PLAN/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define the overarching vision, market context, and architectural motivation for building a Centralized Auth Service.

## Category Mandate

Establishes product identity as a unified identity platform (SSO, OIDC/OAuth 2.1, multi-tenant RBAC, Project Grants delegation).

## Key Topics To Specify

- Market landscape & identity platform requirements.
- Unified SSO and organization management goals.
- Developer experience and REST Management API design philosophy.

## Reference Architecture & Specification

```
Consumer Applications -> Centralized Auth Service -> PostgreSQL (RLS)
```
Core principle: Decoupled identity, centralized authorization, developer-first APIs.

## Acceptance Criteria

- [x] High-level vision documented.
- [x] Target personas identified.
- [x] Key value propositions stated explicitly.

## Open Questions

None currently open.

## Related Documents

- `docs/README.md` (master index)
- `docs/PLAN/01-PRODUCT-SCOPE.md`
- `docs/ARCHITECTURE/00-SYSTEM-ARCHITECTURE.md`
