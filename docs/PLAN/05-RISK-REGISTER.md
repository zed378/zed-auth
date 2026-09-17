# 05 - Risk Register

> Category: **PLAN** (`docs/PLAN/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Maintain an inventory of architectural, security, and operational risks along with severity ratings and mitigations.

## Category Mandate

Ensures proactive risk management across technical debt, threat vectors, and multi-tenant isolation.

## Key Topics To Specify

- Risk 01: Multi-tenant data leakage (Mitigation: PostgreSQL RLS + DB session variables).
- Risk 02: Cross-org role escalation via Project Grants (Mitigation: `granted_role_keys` subset validation).
- Risk 03: Token secret compromise (Mitigation: Automated JWKS key rotation).

## Reference Architecture & Specification

Risk Mitigation Table:
| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| R-01 Data Leak | Low | Critical | PostgreSQL RLS + Integration Tests |

## Acceptance Criteria

- [x] Top architectural & security risks identified.
- [x] Mitigations mapped to implementation specs.

## Open Questions

Review annual penetration testing schedule for Risk 02.

## Related Documents

- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
- `docs/AUTHORIZATION/02-PROJECT-GRANTS-DELEGATION.md`
