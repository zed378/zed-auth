# 01 - Threat Actor Profiles

> Category: **SECURITY** (`docs/SECURITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Define threat actor capabilities, motivations, and potential attack vectors.

## Category Mandate

Ensures security controls are engineered against realistic threat adversaries.

## Key Topics To Specify

- Actor 1: External Credential Stuffer (Automated botnets targeting `/v1/auth/login`).
- Actor 2: Malicious Multi-Tenant User (Attempting IDOR / cross-tenant RLS bypass).
- Actor 3: Compromised Service Account (Attempting privilege escalation via Project Grants).

## Reference Architecture & Specification

Threat Matrix:
| Actor | Motivation | Skill Level | Primary Vector |
|---|---|---|---|
| External Bot | Account Takeover | Low-Medium | Credential Stuffing |
| Malicious Tenant | Data Theft | High | BOLA / IDOR / RLS Bypass |

## Acceptance Criteria

- [x] Threat actor profiles documented.
- [x] Attack vectors mapped to security mitigations.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md`
