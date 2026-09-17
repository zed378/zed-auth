# 01 - Product Scope & Non-Goals

> Category: **PLAN** (`docs/PLAN/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define explicit boundaries of what Centralized Auth Service does and does NOT do.

## Category Mandate

Ensures scope discipline by enumerating core capabilities and explicit non-goals (e.g. not a payment engine, not an e-commerce backend).

## Key Topics To Specify

- Core identity capabilities (OIDC/OAuth 2.1, RBAC, SAML, WebAuthn).
- Explicit non-goals (Billing calculation engine, CMS, raw user file storage).
- Multi-tenant boundary rules.

## Reference Architecture & Specification

In Scope: User AuthN, OIDC Provider, RBAC + ABAC + Project Grants, Management API, Console UI.
Out of Scope: Payment processing (handled via webhooks/external PSP), raw media hosting.

## Acceptance Criteria

- [x] In-scope feature set clearly listed.
- [x] Out-of-scope non-goals clearly listed.

## Open Questions

Validate SAML 2.0 IdP inclusion timeline in Phase 2.

## Related Documents

- `docs/PLAN/00-PROJECT-CONTEXT.md`
- `docs/PLAN/02-REQUIREMENTS.md`
