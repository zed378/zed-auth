# 03 - Attribute-Based Access Control (ABAC)

> Category: **AUTHORIZATION** (`docs/AUTHORIZATION/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail contextual ABAC rule evaluation (IP range, access window, resource sensitivity, user attributes).

## Category Mandate

Provides fine-grained dynamic policy evaluation beyond static roles.

## Key Topics To Specify

- Policy condition syntax (JSON-based expression rules).
- Contextual attributes: `request.ip`, `request.time`, `user.mfa_authenticated`, `resource.confidentiality`.
- DENY overrides ALLOW precedence.

## Reference Architecture & Specification

Policy Rule Example:
`ALLOW action IF user.has_role('editor') AND request.ip IN tenant.allowed_ips AND user.mfa_authenticated == true`

## Acceptance Criteria

- [x] ABAC attribute vocabulary specified.
- [x] Evaluation precedence rules defined.

## Open Questions

Evaluate CEL (Common Expression Language) engine integration in Phase 3.

## Related Documents

- `docs/AUTHORIZATION/00-AUTHORIZATION-ARCHITECTURE.md`
