# P0-14, P0-20 — Secrets Conventions and VM Deployment

| | |
|---|---|
| **Date** | 2026-09-08 |
| **Tasks** | `TASKS/PHASE-0-FOUNDATION.md` P0-14, P0-20 |
| **Phase** | Phase 0 — Foundation |
| **Surface** | backend, infra |
| **Author** | Claude Code |
| **Branch** | `feat/P0-14-secrets-and-vm-deployment` |
| **Status** | Completed, with one open deviation (DV-01) |

---

## What Changed

`OQ-03` was answered — self-managed VM now, Kubernetes later — which unblocked both remaining infrastructure tasks. This adds a secret-reference abstraction with a permission-checking file resolver, the full secret inventory and rotation runbooks, a pre-commit secret scanner, and a complete VM deployment: TLS-terminating reverse proxy, systemd unit, deploy script with migrate-then-rollout-then-smoke-test ordering, and a backup script that verifies itself by restoring.

## Why

Both tasks were blocked on knowing where this runs. `P0-14`'s secret storage and `P0-20`'s entire content depend on it, and `PLAN/07` only said "Vault / cloud secret manager" without naming an environment.

## The Deviation I Have to Flag

`PLAN/14` § Environment Strategy and `PLAN/15` § High Availability both specify **Multi-AZ minimum for production**. A single VM is not that.

This is not a technicality. `PLAN/09` opens by noting that a compromise here compromises every dependent application, and the same reasoning applies to an outage: a VM reboot takes authentication down platform-wide. Two Phase 5 tasks are also affected — `P5-06` requires horizontal scaling "verified in practice… by actually running a scale-out test", which one host cannot demonstrate, and `P5-07`'s DR drill wants an isolated environment to restore into.

Recorded as **DV-01** in `TASKS/BACKLOG.md` with a new Open Deviations section, and in [ADR-011](../DECISIONS.md). It is reasonable now — the consumer applications that would define an SLA do not exist yet (`OQ-08` is open), and `PLAN/00`'s incremental principle argues against building HA before a single tenant is live. It stops being reasonable the moment something depends on it in production.

## How

### The `SecretRef` indirection

The design decision that makes the Kubernetes migration cheap: the service reads **references**, not values. `file:/etc/zed-auth/secrets/jwt-signing.pem` today; `vault:` or `awssm:` later. The scheme changes and nothing else does.

It also matters for `signing_keys.private_key_ref`, which stores one of these (`PLAN/04`) and has a CHECK constraint refusing anything resembling key material.

Three behaviors are worth naming because each prevents a specific real failure:

- **Broad file permissions are refused.** The usual way a secret leaks on a self-managed host is not an attacker reading `/etc` — it is a file created with the shell's default umask and then read by an unrelated service running as another user.
- **Empty secrets are an error, not an empty value.** A service that starts with an empty signing key is worse than one that refuses to start.
- **Planned-but-unimplemented schemes fail loudly and by name.** A stub returning empty bytes would let a misconfigured deployment start with no signing key, and "unknown scheme" versus "not implemented yet" is the difference between a reader thinking they made a typo and knowing they hit a gap.

Trailing newlines are trimmed. An editor adds one, the value stops matching, and it surfaces as an authentication failure a long way from its cause.

### Deploy ordering

`deploy.sh` enforces `PLAN/14` § Release Process as executable steps rather than documentation: refuse a tag (digest only), back up, migrate separately as the owner while the previous version still serves, roll out, smoke test **through the public TLS endpoint**, and on failure roll back the **application only**.

That last point is the one most likely to be "corrected" by someone later. The schema stays forward on rollback. That is safe precisely because migrations are expand/contract — the previous version tolerates the new schema — and rolling the database back would be the more dangerous action, not the safer one.

The smoke test goes through Caddy over TLS rather than hitting the container, because a broken certificate and a misrouted proxy are the two most likely single-VM failures, and neither is visible from inside.

### Backups verify themselves

`PLAN/15` says a backup that has never been tested is not a backup you can rely on. So `backup.sh` does not finish at `pg_dump`: it restores into a throwaway database, checks all 14 tables arrived, and reports the `events` row count — the audit log being the thing most needed after an incident and most easily lost to a partition that was not dumped.

It warns loudly when `AUTH_BACKUP_REMOTE` is unset, because a backup on the same disk as the database protects against `DROP TABLE` and nothing else, while providing the *feeling* of safety.

## Two Real Problems Found While Building

**A credential-isolation bug in my own compose file.** I had written `env_file: /etc/zed-auth/env` on the `authservice` container. That passes the **whole file** in, including `AUTH_POSTGRES_OWNER_PASSWORD` and `AUTH_MIGRATE_DSN` — the schema owner's credentials, which bypass row-level security and can alter the audit log. The entire point of the two-role split (`P0-05`, `P0-07`) is that compromising the service does not yield owner access, and `env_file` quietly handed it over.

Fixed by naming every variable explicitly, so a container receives only what it needs. Added a CI assertion that parses the rendered compose config and fails if the service's environment contains either owner variable — a code review would not reliably catch this reappearing.

Found only because CI's compose-validation step failed for an unrelated reason (`/etc/zed-auth/env` does not exist in CI) and made me look at the file again.

**CRLF in the pre-commit hook.** shellcheck caught `SC1017: literal carriage return`. On Linux that script fails with `bad interpreter: /bin/sh^M`. `.gitattributes` would have normalized it on commit, but the working copy was broken, and this is a security control: a hook that fails to execute protects nothing while still appearing to be there.

Added a CI job that refuses CRLF in anything a shell executes, runs shellcheck, verifies the hooks are executable, and actually *runs* the commit-msg hook against a bad subject to confirm it rejects. A committed non-executable hook is worse than no hook.

## Files Touched

| Path | Change |
|---|---|
| `backend/internal/config/secrets.go` + test | `SecretRef`, resolver, `IsSecretMaterial` |
| `deploy/SECRETS.md` | Inventory of 10 secrets, per-environment separation, rotation runbooks |
| `scripts/hooks/pre-commit` | Secret scanner |
| `deploy/vm/` | compose, Caddyfile, systemd unit, `env.example`, `deploy.sh`, `backup.sh`, README |
| `.github/workflows/ci.yml` | New `shell` job; owner-credential isolation assertion |
| `TASKS/BACKLOG.md` | OQ-03 answered; Open Deviations section; DV-01 |
| `MEMORY/DECISIONS.md` | ADR-011 |

## Tests Added

| Layer | Coverage |
|---|---|
| Unit | 11 secret-resolver cases: permissions across five broad modes and two safe ones, empty secrets, trailing newlines, malformed and unimplemented schemes, reference-vs-material detection, and that `SecretRef.String()` discloses neither path nor variable name |
| Integration | Existing schema tests still pass; the `signing_keys` PEM-refusal fixture now assembles its marker at runtime |
| Security | The permission and disclosure tests are the security suite here |

The permission tests skip on Windows, where POSIX mode bits are not meaningful. Since that is the most security-relevant behavior in the resolver, I ran them in a Linux container rather than shipping them unverified on the developer machine. CI runs on Ubuntu, so they gate every PR.

## Abuse Cases Covered

| Abuse case | Source | Test |
|---|---|---|
| Secret readable by another service on the host | `SECURITY/02` §16 | `TestSecretResolver_RefusesBroadFilePermissions` |
| Key material stored where a reference belongs | `PLAN/02` § Constraints | `TestIsSecretMaterial`, plus the DB constraint |
| Secret path disclosed in logs | `SECURITY/02` §12 | `TestSecretRef_StringDoesNotDiscloseTheReference` |
| Credential committed to the repository | `SECURITY/02` §16 | pre-commit hook, verified against six cases; gitleaks in CI |
| Service reaching owner credentials | `PLAN/08` Part B, `SECURITY/02` §19 | CI assertion over the rendered compose config |
| Rate-limit bypass via forged `X-Forwarded-For` | `SECURITY/02` §10 | Caddy overwrites rather than appends |
| Correlation-ID collision by a caller | `SECURITY/02` §10 | Caddy strips inbound `X-Request-Id` |
| Silent code substitution via a mutable tag | `SECURITY/02` §15 | `deploy.sh` refuses a non-digest image |
| Staging token valid in production | `PLAN/13`, `PLAN/14` | `deploy.sh` fingerprints signing keys across environments |

## Definition of Done Verification

- [x] Every secret inventoried with owner, storage, and rotation cadence
- [x] Local, staging, production use distinct values — enforced by the fingerprint check, not just documented
- [x] No secret readable from the image or the repository
- [x] Pre-commit hook documented and verified against six cases
- [x] Staging reachable over TLS only — the service port is never published
- [x] Migrations run as a separate step before rollout
- [x] Backups automated with PITR (WAL archiving) and **restore-verified**
- [x] shellcheck clean; compose files validate
- [ ] **A merge to `main` deploys to staging automatically** — not done. `deploy.sh` runs on the VM, but nothing triggers it from CI yet: that needs a runner with SSH access to the VM and a deploy key, which is a credential decision for the owner. Tracked as `OQ-10`.
- [ ] **A backup restore executed against the real staging VM** — the script self-verifies and its logic is sound, but no VM exists yet. This must be run once during real provisioning; until then `P0-20` is done in code and unproven in practice.

Both unmet items are stated rather than quietly ticked.

## What Did Not Work

**A pre-commit hook that blocked its own repository.** My first version had a generic `scheme://user:password@host` pattern, which fires on this repository's own `local_dev_only` placeholder — present in `docker-compose.yml`, `.env.example`, the Makefile, CI, and the integration tests. I had written a comment in that same file warning that a noisy hook gets bypassed within a week, and then wrote a noisy hook. Removed the generic pattern; the placeholder-aware check in section 4 already covered connection strings properly.

**Test fixtures that tripped the scanner.** The PEM-detection tests legitimately contained PEM markers. Rather than allowlisting the files, the fixtures now assemble markers at runtime, so the repository contains **zero** literal PEM blocks and any future occurrence is a genuine finding. Enforced by a CI check.

**Two escaping failures again**: a Python patch put a literal newline inside a Go string, and one replacement silently did not match while still appending its helper. Both caught by the compiler. The pattern is now unmistakable — patching source through Python string replacement is error-prone enough that Edit is the right tool unless the change is genuinely mechanical.

**shellcheck directive placement.** `# shellcheck source=/dev/null` above `set -a; source "$F"; set +a` does not attach, because `source` is mid-compound-command. Splitting the line fixed it.

## Follow-Ups

- **OQ-10 (new)**: automated staging deploy from CI needs a deploy credential — an SSH key or a self-hosted runner. A credential decision for the owner, not one I should make.
- **DV-01** stays open until the Kubernetes migration or a formal risk acceptance.
- `OQ-05` (RPO/RTO) is still unanswered, so `P5-07`'s drill can be executed but not judged.
- `AUTH_BACKUP_REMOTE` must be set during real provisioning.

## What to Watch

**`AUTH_BACKUP_REMOTE` left unset is the single most likely way this deployment loses data permanently.** The script warns, but a warning in a nightly timer's output is a warning nobody reads. Losing the VM would lose the database and every backup of it together.

**The `env_file` bug will want to come back.** `env_file` is shorter, more idiomatic, and looks obviously correct. The CI assertion is the only thing standing between convenience and handing the service RLS-bypassing credentials — if someone adds a variable and the assertion starts failing, the fix is to name the variable, not to relax the check.

**`deploy.sh` has never run.** Its logic is sound and shellcheck is clean, but sound-and-unrun is exactly the state `PLAN/15` warns about for backups, and the same applies here. The first real deploy is the test.

**Certificate renewal is now a silent dependency.** Caddy renews automatically, which is why it was chosen — but nothing alerts if renewal fails. On a single VM an expired certificate is a total outage, and `P0-11` (metrics) or `P5-08` (monitoring) should add a certificate-expiry alert before this carries real traffic.
