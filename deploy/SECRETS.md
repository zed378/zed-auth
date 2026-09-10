# Secrets and Configuration

How secrets are stored, reached, separated, and rotated.

**Governing documents**: `docs/PLAN/07-BACKEND-ARCHITECTURE.md` § Infrastructure, `docs/PLAN/09-SECURITY.md`, `docs/PLAN/02-REQUIREMENTS.md` § Constraints, `docs/SECURITY/02-ATTACK-SURFACE-AND-SCENARIOS.md` §16. Deployment context: [ADR-011](../MEMORY/DECISIONS.md).

---

## The One Constraint That Cannot Be Traded Away

> `docs/PLAN/02-REQUIREMENTS.md` § Constraints: **No third-party dependency may hold the private signing key outside of Auth Service's own infrastructure/secret manager.**

Everything below is arrangeable. That is not. A self-managed VM satisfies it trivially — the key never leaves the owner's machine — and any future move to a managed secret store must keep satisfying it.

---

## Configuration Precedence

| Kind of value | Where it lives | Why |
|---|---|---|
| Deployment-specific, non-secret (issuer URL, timeouts, pool sizes) | Environment variables | Visible in `docker inspect` and process listings, which is fine for non-secrets and useful for debugging |
| Secrets (keys, passwords, client secrets) | A **file**, referenced by URI | Environment variables leak: into `docker inspect`, into a crash dump, into a child process, and into any library that logs its environment on startup |
| Local development | `.env`, git-ignored | Never contains a real credential |

The service reads **references**, not values — `file:/etc/zed-auth/secrets/metrics-token` rather than the token itself. Signing keys are the same in kind but no longer configured by an environment variable: since `P1-03` each key's reference lives in a `signing_keys` row, so a rollback of the binary cannot change which key signs. `internal/config/secrets.go` resolves them, and the scheme is what changes when the deployment target does — `file:` on the VM today, `vault:` or `awssm:` after the Kubernetes migration (ADR-011).

The resolver refuses:

- a secret file that is group- or world-readable, which is how a secret usually leaks on a self-managed host — not an attacker reading `/etc`, but a file created with the shell's default umask and then read by an unrelated service running as another user;
- an empty secret, because a service that starts with an empty signing key is worse than one that refuses to start;
- a scheme that is planned but unimplemented, loudly and by name, so a reader can tell a gap from a typo.

It also trims trailing newlines. An editor adds one, the value stops matching, and it surfaces as an authentication failure a long way from its cause.

---

## Secret Inventory

Every secret this system holds. A secret that is not on this list is not managed.

| Secret | Holds | Storage | Rotation | Blast radius if leaked |
|---|---|---|---|---|
| **JWT signing private key** (`oidc`) | Signs every access and ID token | `file:` under `/etc/zed-auth/secrets/`, `0400`, owned by the service user. Referenced from `signing_keys.private_key_ref` | 90 days, with an overlap window (`docs/PLAN/09`) | **Total.** An attacker can mint a valid token for any user in any organization. This is the highest-value secret in the system |
| **SAML signing private key** (Phase 4) | Signs SAML assertions | Same, distinct key set from OIDC | 90 days | Impersonation of any user to any SAML service provider |
| **PostgreSQL `auth_app` password** | Service runtime database access | Env var in the compose file's env file, `0600` | 180 days, or immediately on suspicion | Read and write of all tenant data, subject to RLS. Cannot alter the audit log — `auth_app` has no UPDATE or DELETE on `events` |
| **PostgreSQL `auth_owner` password** | Schema owner; migrations only | Same file, but only ever loaded by the migration step | 180 days | **Higher than `auth_app`.** The owner bypasses row-level security and can alter the audit log |
| **Redis password** | Session cache, rate-limit counters, authorization codes | Same file | 180 days | Session hijacking via cached session lookup; rate-limit bypass |
| **OIDC client secrets** | Confidential client authentication | Hashed at rest in `applications.client_secret_hash`. The plaintext exists only in the consumer application | Per-application, on demand, with an overlap window (`P1-05`) | Impersonation of that one application |
| **SMTP credentials** | Invitations, password resets, anomaly notices | Env file, `0600` | Per provider policy | Sending mail as the platform — a phishing capability, since password reset emails are the obvious lure |
| **Webhook signing secrets** (Phase 4) | Per-endpoint payload signatures | Hashed at rest in `webhook_endpoints.secret_hash` | Per-endpoint, on demand | Forged webhook deliveries to one consumer |
| **Social IdP client secrets** (Phase 4) | Google, Microsoft, GitHub federation | `file:` under `/etc/zed-auth/secrets/` | Per provider | Federated login impersonation for that provider |
| **Backup encryption key** | Encrypts offsite database backups | **Stored off the VM.** A backup key kept on the machine the backup protects is not a backup key | Annually | Offsite backups become readable — which is the entire database, including every hash |

### Separation by Environment

`docs/PLAN/13-OBSERVABILITY.md` and `docs/PLAN/14-DEPLOYMENT.md` both state it: **every environment has separate signing keys and databases. Production keys and data are never copied into staging.**

This is not bureaucratic. Staging is where debugging happens, where access is looser, and where a developer will reasonably dump a token to a terminal. A shared signing key means a staging token is a production token.

| | local | staging | production |
|---|---|---|---|
| Signing keys | Generated on first run, disposable | Generated for staging, never reused | Generated for production, never leaves the VM |
| Database | Container, disposable | Own instance | Own instance |
| Backups | None | Optional | Required, offsite, encrypted, restore-verified |

`P0-20`'s deployment script refuses to start if it detects a signing key fingerprint shared between two environments.

---

## Filesystem Layout on the VM

```
/etc/zed-auth/
  env                          0600  root:root       non-secret + secret env vars
  secrets/                     0700  zedauth:zedauth
    jwt-signing-current.pem    0400  zedauth:zedauth
    jwt-signing-next.pem       0400  zedauth:zedauth
    jwt-signing-previous.pem   0400  zedauth:zedauth
```

The service runs as an unprivileged `zedauth` user, not root. `/etc/zed-auth/env` stays root-owned and is read by the container runtime rather than by the service, so a compromise of the service process does not yield the database password from disk — only from its own environment, which it needs anyway.

Rotation keeps three keys because `docs/PLAN/09` requires an overlap window: `next` is published in JWKS before it signs anything, `current` signs, `previous` still verifies. A token issued a second before rotation must still verify after it.

---

## Rotation Runbooks

Each of these must be **executed once against staging** before it counts as written. `P0-14`'s Definition of Done requires it, and a runbook that has never been run is a hypothesis.

### JWT signing key (every 90 days)

The property that must hold throughout: **no token that was valid before the rotation becomes invalid during it.**

1. Generate the new key on the VM. It must never transit a network or a developer machine.
2. Write it to `secrets/jwt-signing-next.pem`, `0400`, owned by `zedauth`.
3. Insert the row with `status = 'next'`. It is now published in JWKS but signs nothing — consumers begin caching it before any token depends on it.
4. **Wait at least one JWKS cache TTL.** Skipping this is the way to break every consumer at once: promote a key before consumers have fetched it and every new token fails verification.
5. Promote: `next` → `current`, old `current` → `previous`. Exactly one `current` per purpose is enforced by a partial unique index (`P0-07`).
6. Verify a freshly issued token carries the new `kid` and verifies; verify a token issued before step 5 still verifies.
7. After the longest access-token lifetime has passed, retire `previous` and delete the file.
8. Audit the rotation.

Rollback: if step 6 fails, demote the new key back to `next` and restore the previous `current`. Because both remain in JWKS throughout, no consumer sees an interruption.

### Database passwords (every 180 days)

PostgreSQL has no dual-password mechanism, so this needs a brief coordinated window.

1. `ALTER ROLE auth_app WITH PASSWORD '<new>'`.
2. Update `/etc/zed-auth/env`.
3. Restart the service. In-flight requests drain first (`P0-04`), so this is a pause rather than an error.
4. Confirm `/readyz` returns 200.

Rotate `auth_owner` separately and less often; it is only used by the migration step, so nothing long-running holds a connection with it.

### Redis password (every 180 days)

Same shape. Losing the Redis cache is recoverable — PostgreSQL is authoritative for sessions (ADR-003) — so a brief restart costs cached lookups, not sessions.

### Emergency rotation (suspected compromise)

Do not follow the 90-day procedure. `docs/SECURITY/04-INCIDENT-RESPONSE-PLAYBOOKS.md` governs; the shape is:

1. Generate and promote a new signing key **immediately**, skipping the overlap window. This invalidates every outstanding token, which is the point.
2. Revoke every session and every refresh token.
3. Rotate every database and Redis credential.
4. Rotate every OIDC client secret and notify each consumer application.
5. Preserve the audit log before anything else — it is the investigation.

Consumers will see mass logout. That is the correct outcome and should be communicated, not avoided.

---

## What Must Never Happen

- A secret in the repository. Enforced by `.gitignore`, a pre-commit hook, and gitleaks in CI (`P0-13`).
- Key material in `signing_keys.private_key_ref`. Enforced by a database CHECK constraint (`P0-07`) and by `IsSecretMaterial` at startup.
- A secret in a log. Enforced by the logger's redaction layer (`P0-09`) and a CI grep.
- The same signing key in two environments.
- A backup encryption key stored on the machine the backup protects.
- A secret pasted into a chat, an issue, or a commit message. If it happens, rotate — do not delete the message and hope. Deleting it does not un-see it, and most chat systems keep an audit trail of the deletion but not of who read it first.
