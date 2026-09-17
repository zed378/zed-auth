# 04 - Secret Management Specification

> Category: **DEVOPS** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Final specification &nbsp;|&nbsp; Owner: Platform Security & Engineering

## Purpose

Specify secret storage, injection, and rotation using HashiCorp Vault or AWS Secrets Manager.

## Category Mandate

Guarantees that database credentials, signing keys, and API secrets are never committed to version control.

## Key Topics To Specify

- Secrets injected as Kubernetes Secrets via External Secrets Operator.
- Secrets encrypted at rest using envelope encryption (KMS).
- Zero hardcoded secrets in repository.

## Reference Architecture & Specification

Secret Injection Rule: Hardcoded secrets in code or repository configuration trigger instant CI build termination.

## Acceptance Criteria

- [x] Secret storage engine specified.
- [x] Injection mechanism documented.

## Open Questions

None.

## Related Documents

- `docs/SECURITY/03-SECURITY-CONTROLS-BASELINE.md`
