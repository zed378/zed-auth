# 04 - Secret Management

> Category: **DevOps** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-14, P3-03, P3-15 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

State where secret material lives, how the service reaches it, and what must never happen to it. The operational detail is `deploy/SECRETS.md`; this document is the summary and the pointer.

## Scope

Secrets as an operational concern. The cryptography they protect is `docs/SESSION-MANAGEMENT/03-JWKS-KEY-ROTATION.md` and `docs/IDENTITY-PROTOCOL/`.

## As Built

### The constraint that shapes everything

`docs/PLAN/02-REQUIREMENTS.md` § Constraints: **no third-party dependency may hold the private signing key outside this service's own infrastructure or secret manager.** A self-managed host satisfies it trivially; any future managed store must keep satisfying it (`deploy/SECRETS.md`, ADR-011).

### What secrets exist

| Secret | Purpose | Where it lives |
|---|---|---|
| Signing keys | JWT signatures | Referenced by the `signing_keys` row; material outside the database, reachable only by the service |
| MFA seal key | Sealing TOTP secrets at rest | File reference in the environment |
| Database credentials | Application role and owner role | Environment; the owner role goes only to `migrate` |
| Client secrets | Confidential OIDC clients | Hashed in the database; the plaintext is returned exactly once at creation or rotation |
| Metrics token | Admin listener access | Environment |
| SMTP credentials | Mail delivery | Environment |

### How they are supplied

Configuration carries **references** (paths, names), not values, and the loader fails startup if a required reference is missing or unreadable. Nothing secret is committed: `deploy/vm/env.example` documents the shape with placeholders, and the real environment file lives in the runtime state directory outside the checkout so a re-clone cannot destroy or expose it.

### Rotation

- Signing keys rotate with overlap through the `next` → `current` → `previous` → `retired` lifecycle; `backend/cmd/keyctl` and `deploy/vm/RUNBOOK-key-rotation.md` document the procedure.
- Client secrets rotate through `POST …/applications/{id}/rotate-secret`, which returns the new value once.
- Database and SMTP credentials are rotated by hand; there is no automation.

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Secrets in the repository | None; placeholders only | `scripts/hooks/pre-commit`, `scripts/check.sh` § Security |
| Secret-shaped strings in logs | Refused | log-hygiene gates in `scripts/check.sh` |
| Private key material in the repository or images | Refused (PEM block scan) | pre-commit hook and gate |
| Owner database credentials in the service container | Never | gate in `scripts/check.sh` § Deployment |
| Client secret readback | Impossible; only the hash is stored | `backend/internal/application/` |

## Security Considerations

- The gates above exist because each of these is a mistake that looks harmless in a diff: a test fixture with a real token, a debug log line, a compose file that reuses the owner DSN "just for now".
- Losing the signing key is a recovery problem, not just a security one: the state directory that holds it is deliberately outside the checkout, after a re-clone once destroyed keys and backups together.

## Verification

- `scripts/check.sh` § Security — no PEM blocks, no credential-shaped tokens, no raw request material in log calls.
- `scripts/hooks/pre-commit` — the same checks before a commit is written.
- `backend/internal/config/*_test.go` — missing references fail startup.

## Not Yet Built / Open Questions

- No managed secret store (Vault, cloud KMS) and no automatic rotation for database or SMTP credentials.
- No secret-access audit beyond process-level logging.

## Related Documents

- `deploy/SECRETS.md`, `deploy/vm/secrets.sh`, `deploy/vm/RUNBOOK-key-rotation.md`.
- `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §16; `docs/PLAN/09-SECURITY.md`.
