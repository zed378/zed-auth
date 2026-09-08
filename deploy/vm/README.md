# VM Deployment Runbook

Deploying the Auth Service to a self-managed VM. Covers first-time setup, routine deploys, backup and restore, and the move to Kubernetes later.

**Context**: [ADR-011](../../MEMORY/DECISIONS.md). **Secrets**: [../SECRETS.md](../SECRETS.md). **Governing plan**: `PLAN/14-DEPLOYMENT.md`, `PLAN/15-DISASTER-RECOVERY.md`.

---

## Read This First

> **This deployment does not meet `PLAN/14`'s Multi-AZ requirement for production.** A single VM is a single point of failure for every consumer application's authentication: a reboot, a kernel panic, or a failed deploy takes login down platform-wide.
>
> Tracked as **DV-01** in `TASKS/BACKLOG.md`. It is accepted as interim, and it must be closed by the Kubernetes migration — or formally risk-accepted in `PLAN/18-RISK-REGISTER.md` with an owner and a date — before any consumer application depends on this in production.

Everything below is the strongest posture a single host allows, so the gap stays **bounded and measured** rather than unknown.

---

## What Runs Here

| Container | Role | Exposed |
|---|---|---|
| `caddy` | TLS termination, automatic Let's Encrypt | 80, 443 — the only ports on the host |
| `authservice` | The service | Internal only |
| `postgres` | Authoritative store, WAL archiving on | Internal only |
| `redis` | Session cache, rate limits, authorization codes | Internal only |

Only Caddy is reachable from outside. There is no way to reach the service without TLS, because the port is never published.

---

## First-Time Setup

### 1. Host

```bash
# Docker, and nothing else the service needs
curl -fsSL https://get.docker.com | sh
sudo systemctl enable --now docker

# Unattended security updates. On a self-managed host, an unpatched kernel is
# the most likely way in that has nothing to do with this application.
sudo apt-get install -y unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades

# Only 22, 80 and 443. Postgres and Redis are never published.
sudo ufw default deny incoming
sudo ufw allow 22/tcp && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp
sudo ufw enable
```

### 2. Repository and directories

```bash
sudo git clone https://github.com/zed378/zed-auth.git /opt/zed-auth
sudo mkdir -p /etc/zed-auth/secrets
sudo chmod 700 /etc/zed-auth/secrets
```

### 3. Signing keys

Generate them **on this machine**. `PLAN/02` § Constraints: no third party holds the private signing key. A key generated on a laptop and copied over has been on a laptop.

```bash
sudo openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out /etc/zed-auth/secrets/jwt-signing-current.pem
sudo chmod 400 /etc/zed-auth/secrets/jwt-signing-current.pem
```

The resolver refuses a key file that is group- or world-readable (`P0-14`), so a wrong mode here fails at startup rather than silently.

**Staging and production must have different keys.** `deploy.sh` fingerprints them and refuses to start if two environments share a key — a shared key means a staging token is valid in production.

### 4. Environment file

```bash
sudo install -m 0600 -o root -g root \
  /opt/zed-auth/deploy/vm/env.example /etc/zed-auth/env
sudo "$EDITOR" /etc/zed-auth/env      # replace every REPLACE_ME
```

Generate passwords with `openssl rand -base64 32`. Do not reuse one between `auth_owner` and `auth_app`: the whole point of the split is that compromising the service does not yield owner access.

### 5. Start

```bash
sudo cp /opt/zed-auth/deploy/vm/zed-auth.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now zed-auth

cd /opt/zed-auth/deploy/vm
sudo docker compose run --rm --no-deps --entrypoint /migrate authservice up
curl -sf https://your-domain/healthz
```

### 6. Backups

```bash
sudo tee /etc/systemd/system/zed-auth-backup.service <<'EOF'
[Unit]
Description=Zed Auth database backup
[Service]
Type=oneshot
ExecStart=/opt/zed-auth/deploy/vm/backup.sh
EOF

sudo tee /etc/systemd/system/zed-auth-backup.timer <<'EOF'
[Unit]
Description=Nightly Zed Auth backup
[Timer]
OnCalendar=daily
RandomizedDelaySec=1h
Persistent=true
[Install]
WantedBy=timers.target
EOF

sudo systemctl enable --now zed-auth-backup.timer
sudo /opt/zed-auth/deploy/vm/backup.sh   # run once now, and read the output
```

**Set `AUTH_BACKUP_REMOTE`.** Without it, backups sit on the same disk as the database they protect — which guards against `DROP TABLE` and against nothing else. `PLAN/15` requires a separate failure domain, and `backup.sh` warns loudly when this is unset because a local-only backup gives the *feeling* of safety without it.

---

## Routine Deploy

```bash
sudo /opt/zed-auth/deploy/vm/deploy.sh ghcr.io/zed378/zed-auth@sha256:<digest>
```

The ordering matters and is enforced by the script:

1. **Refuse a tag.** Only a digest. A tag is mutable, so rolling back to "the same tag" can silently deploy different code.
2. **Back up first.** No restore point, no migration.
3. **Migrate separately**, as the owner, while the previous version still serves. Safe only because migrations are expand/contract — CI refuses a destructive one without justification.
4. **Roll out**, as `auth_app`.
5. **Smoke test through the public TLS endpoint**, not the container. Testing the container directly would miss a broken certificate or a misrouted proxy, which are the two most likely single-VM failures.
6. **On failure, roll back the application only.** The schema stays forward. Rolling the database back would be the more dangerous action, not the safer one — that is the whole reason for expand/contract.

---

## Restore

`backup.sh` verifies every scheduled backup by restoring it into a throwaway database and checking all 14 tables arrived. A backup that has never been restored is a hypothesis.

```bash
# Restore the newest backup and verify it, without touching production
sudo ./backup.sh --verify-only

# Real restore
sudo systemctl stop zed-auth
sudo docker compose up -d postgres
sudo docker compose exec -T postgres dropdb -U auth_owner auth
sudo docker compose exec -T postgres createdb -U auth_owner auth
sudo docker compose exec -T postgres pg_restore -U auth_owner -d auth --no-owner \
  < /var/backups/zed-auth/<env>-<timestamp>.dump
sudo systemctl start zed-auth
```

**Signing keys are backed up separately from the database** (`PLAN/15`), and restoring the two out of sync is the subtle failure `PLAN/15` § Restore Testing warns about: tokens issued before the restore fail verification, and the error points at neither.

Point-in-time recovery is possible because WAL archiving is on. `P5-07` is where the full drill happens and where RTO and RPO get measured — they are still unset (`OQ-05`), which means a drill can be executed but not yet judged successful.

---

## Moving to Kubernetes

The architecture was kept deployment-agnostic so this is a manifest exercise, not a rewrite (ADR-011). What changes:

| | VM | Kubernetes |
|---|---|---|
| Orchestration | Compose + systemd | Deployment + HPA |
| Config | `${VAR}` from `/etc/zed-auth/env` | ConfigMap + Secret |
| Secrets | `file:` references | `vault:` or `awssm:` — the resolver already has the seam |
| TLS | Caddy | Ingress controller |
| Postgres | Container here | Managed service or operator |

What does **not** change: the service code. It reads configuration from the environment, keeps no state on local disk, and terminates TLS elsewhere.

The migration also closes DV-01, which is the real reason to do it.

---

## Operations

```bash
sudo systemctl status zed-auth
sudo docker compose -f /opt/zed-auth/deploy/vm/docker-compose.yml logs -f authservice
sudo docker compose -f /opt/zed-auth/deploy/vm/docker-compose.yml ps
```

Logs are structured JSON with a `request_id` on every line, and the logger redacts credentials by attribute key (`P0-09`). There is deliberately no shell in the runtime image (ADR-008), so diagnosis happens from logs and metrics rather than from inside the container.

### If the service will not start

Configuration errors are reported all at once, each naming its variable — read the first lines of the log rather than guessing. Common causes, in order of likelihood:

1. A signing key file that is not `0400` or `0600`. The resolver refuses it.
2. `AUTH_ISSUER` with a trailing slash, or `http://` outside local development. Both are refused at startup, because a mismatched issuer breaks every conforming client library.
3. `AUTH_LOG_LEVEL=debug` with `AUTH_ENV=production`. Refused deliberately: debug output across an identity provider is where sensitive values leak despite redaction.

### Certificate problems

Caddy renews automatically. `docker compose logs caddy` shows ACME activity. Port 80 must stay open — it is used for the challenge, never to serve the application.
