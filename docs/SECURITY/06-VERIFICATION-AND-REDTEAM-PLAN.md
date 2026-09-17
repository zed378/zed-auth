# 06 - Red-Team Verification Plan

> Category: **SECURITY** (`docs/SECURITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Detail static code analysis (SAST), dynamic vulnerability scanning (DAST), and periodic penetration testing.

## Category Mandate

Validates security posture through continuous automated and manual security testing.

## Key Topics To Specify

- SAST: `gosec ./...` run on every CI build.
- Dependency Audit: `govulncheck` & `npm audit` in CI.
- Penetration Testing: Annual third-party black-box and grey-box pentest.

## Reference Architecture & Specification

CI Security Gate: CI build fails if `gosec` flags any High severity security issue.

## Acceptance Criteria

- [x] SAST/DAST tooling specified.
- [x] Third-party audit schedule established.

## Open Questions

None.

## Related Documents

- `docs/TESTING/04-ABUSE-CASE-SECURITY-TESTING.md`
