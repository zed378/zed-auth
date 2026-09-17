# 02 - Attack Surface & Abuse Case Catalog

> Category: **SECURITY** (`docs/SECURITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Maintain an exhaustive catalog of potential abuse cases, attack vectors, and automated test requirements.

## Category Mandate

Acts as the mandatory security testing specification for all engineering tasks.

## Key Topics To Specify

- Scenario 01: Cross-Tenant Data Access (IDOR / RLS Bypass).
- Scenario 02: Project Grant Privilege Escalation (Requesting unauthorized role keys).
- Scenario 03: Refresh Token Replay Attack.
- Scenario 04: Password Brute-Force & Credential Stuffing.
- Scenario 05: JWT Signature Stripping / Algorithm None Attack.

## Reference Architecture & Specification

Mandatory Test Rule: Every security-sensitive feature must ship with a matching abuse-case test in `backend/test/abuse/` verifying rejection of these scenarios.

## Acceptance Criteria

- [x] All 5 major attack scenarios detailed.
- [x] Abuse-case testing requirement enforced.

## Open Questions

None.

## Related Documents

- `docs/PLAN/09-SECURITY.md`
- `docs/TESTING/04-ABUSE-CASE-SECURITY-TESTING.md`
