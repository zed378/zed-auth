# 04 - Security Detection & Alerting Rules

> Category: **SECURITY** (`docs/SECURITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify SIEM detection rules, threshold alerts, and anomaly detection logic.

## Category Mandate

Enables real-time detection of active security attacks.

## Key Topics To Specify

- Alert 01: >10 failed login attempts for a single account within 1 minute (Brute-force alert).
- Alert 02: Presentation of a revoked refresh token (Token theft alert).
- Alert 03: Any SQL syntax/execution error from DB driver (Possible SQLi probing alert).

## Reference Architecture & Specification

Alert Severity Matrix:
- Critical (P1): JWKS key access anomaly, Token theft detected -> PagerDuty alert.
- High (P2): High rate of failed logins -> Slack security channel alert.

## Acceptance Criteria

- [x] SIEM alert thresholds defined.
- [x] Notification routing specified.

## Open Questions

None.

## Related Documents

- `docs/OBSERVABILITY/01-AUDIT-LOGGING-SPECIFICATION.md`
