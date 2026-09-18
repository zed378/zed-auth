# 00 - DevOps Overview

> Category: **DevOps** (`docs/DEVOPS/`) &nbsp;|&nbsp; Status: Implemented &nbsp;|&nbsp; Tasks: P0-02, P0-13, P0-14, P0-20, P1-27, P3-15 &nbsp;|&nbsp; Verified against: `84eb9a2`

## Purpose

Describe how this system is built, checked, shipped and recovered today — as opposed to how `docs/PLAN/14-DEPLOYMENT.md` says it eventually should be.

## Scope

The operational picture and the order of a release. The detail is in the sibling documents: environments (`01`), images (`02`), CI (`03`), secrets (`04`), backup and recovery (`05`).

## As Built

- **One Go binary** and two static artefacts (console, public site). Everything runs under Docker Compose in every environment that exists today.
- **Local**: `deploy/docker-compose.yml` brings up Postgres, Redis, Mailpit and the service. `scripts/e2e-up.sh` additionally seeds a bootstrap instance, organization, administrator and console client, registers two demo applications, builds the console for that stack and prints the environment the E2E suite needs. `scripts/e2e-down.sh` tears it down.
- **CI** (`.github/workflows/ci.yml`) runs eleven jobs: commit convention, backend, integration, api-contract, console, public-site, migration-safety, security, shell, docker, and the aggregate gate. `scripts/check.sh` runs the same families locally in thirteen sections — Go, demo applications, integration, migrations, shell, security, coverage, API contract, brand, console, public site, deployment — so a developer sees the same failure CI would produce, faster.
- **Staging** is a single VM reached only from the owner's private network, running the compose file in `deploy/vm/` behind a Cloudflare tunnel. Every service binds a port above 10000. Runtime state — the environment file, secrets and backups — lives outside the checkout so a re-clone cannot destroy it.
- **Production does not exist yet.** Kubernetes is the intended target (`docs/PLAN/14`, ADR-011) and nothing has been deployed to it.

### Release order (staging, and the order any environment must follow)

1. **Back up first** — `systemctl start zed-auth-backup.service`, then confirm from the journal and the dump file. If the backup fails, stop.
2. **Pull the merged commit** in the checkout.
3. **Build the image** on the host, tagged for the task (`zed-auth:<tag>`).
4. **Migrate as the owner role**, using the migrate entrypoint with the owner DSN.
5. **Roll out**: point `AUTH_IMAGE` at the new tag and recreate the service container.
6. **Verify**: health, the served image tag, and a functional check against the running service — not just "the container started".

Steps 4 and 5 are separate because migrations are expand/contract: the previous version must keep working against the new schema (`docs/DATABASE/04-MIGRATIONS-STRATEGY.md`).

## Rules and Defaults

| Rule | Value | Enforced in |
|---|---|---|
| Configuration source | Environment variables prefixed `AUTH_` | `backend/internal/config/config.go` |
| Default HTTP listener | `:8080`, admin listener separate | `AUTH_HTTP_ADDR`, `backend/cmd/authservice` |
| Migrations | Run as the database owner, never as the application role | `backend/cmd/migrate`, `deploy/vm/deploy.sh` |
| Backups | Nightly timer, destination under the state directory, alert on absence of success | `deploy/vm/zed-auth-backup.{service,timer}`, `deploy/vm/backup.sh` |
| Staging ports | Above 10000, exposed only through the tunnel | `deploy/vm/docker-compose.tunnel.yml` |

## Interfaces

- `scripts/check.sh` — the local gate. `CHECK_FULL=1` additionally runs the public-site build, its audits, and the console E2E suite.
- `deploy/vm/deploy.sh` — the documented deployment order.
- Runbooks: `deploy/vm/RUNBOOK-key-rotation.md`, `deploy/RUNBOOK-mfa-recovery.md`.

## Security Considerations

- The application never receives the database owner credentials; only `migrate` does (asserted by a gate in `scripts/check.sh`).
- Secrets are referenced, never committed (`04-SECRET-MANAGEMENT.md`, `deploy/SECRETS.md`).
- A deploy script piped over SSH must redirect stdin for `docker compose run`, or the command consumes the rest of the script and the rollout silently stops after the migration (observed 2026-09-17, recorded in `MEMORY/records/2026-09-17-P4-02-delegated-user-grants.md`).

## Verification

- CI on every push; `scripts/check.sh` before every merge.
- Staging: each backend task is deployed and exercised against the running service, with the result recorded in `MEMORY/records/`.
- Acceptance scripts per phase (`scripts/acceptance-phase2.sh`, `scripts/acceptance-phase3.sh`).

## Not Yet Built / Open Questions

- Kubernetes manifests, a production environment, and any autoscaling story.
- Restore has a documented procedure but no scheduled restore drill (`05-BACKUP-AND-DISASTER-RECOVERY.md`).

## Related Documents

- `docs/PLAN/14-DEPLOYMENT.md`, `docs/PLAN/15-DISASTER-RECOVERY.md`.
- `docs/TESTING/` for what the gates actually run.
