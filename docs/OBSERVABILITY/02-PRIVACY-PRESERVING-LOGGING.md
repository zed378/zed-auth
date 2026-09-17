# 02 - Privacy-Preserving Logging & Redaction

> Category: **OBSERVABILITY** (`docs/OBSERVABILITY/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Establish zero-leak privacy logging rules to prevent sensitive credentials or PII from entering log files.

## Category Mandate

Guarantees that tokens, passwords, and sensitive attributes are NEVER logged to stdout or log aggregators.

## Key Topics To Specify

- Blacklisted fields: `password`, `token`, `secret`, `authorization`, `cookie`, `attributes`.
- Automatic recursive JSON sanitizer middleware in logging pipeline.
- Replaces sensitive values with `[REDACTED]` string.

## Reference Architecture & Specification

Sanitization Rule: Never log raw JWT tokens, client secrets, passwords, or raw ABAC evaluation resource attributes.

## Acceptance Criteria

- [x] Blacklisted log fields enumerated.
- [x] Automatic redaction middleware specified.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/03-SECURITY-CONTROLS-BASELINE.md`
