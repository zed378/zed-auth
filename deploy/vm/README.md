# VM Deployment Runbook

Deploying the Auth Service to a self-managed VM. Covers first-time setup, routine deploys, backup and restore, and the move to Kubernetes later.

**Context**: [ADR-011](../../MEMORY/DECISIONS.md). **Secrets**: [../SECRETS.md](../SECRETS.md). **Governing plan**: `docs/PLAN/14-DEPLOYMENT.md`, `docs/PLAN/15-DISASTER-RECOVERY.md`.

---

## Read This First

> **This deployment does not meet `docs/PLAN/14`'s Multi-AZ requirement for production.** A single VM is a single point of failure for every consumer application's authentication: a reboot, a kernel panic, or a failed deploy takes login down platform-wide.
>
> Tracked as **DV-01** in `TASKS/BACKLOG.md`. It is accepted as interim, and it must be closed by the Kubernetes migration — or formally risk-accepted in `docs/PLAN/18-RISK-REGISTER.md` with an owner and a date — before any consumer application depends on this in production.

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

**The checkout is disposable; runtime state is not.** Keep them apart:

```
/home/infra/auth/          the git clone — `git pull` and re-clone are safe
/home/infra/auth-state/    mode 700, never touched by git
    .env                   mode 600
    secrets/               owned by uid 65532
    backups/
    artifacts/console      built elsewhere, copied here
    artifacts/public-site
```

This is not tidiness. Secrets, backups and build artifacts once lived inside the checkout, and deleting the directory to re-clone destroyed the signing key, the metrics token and every backup at once. The service kept running on already-open mounts and would not have survived a restart — invisible until the moment it mattered.

"Do not delete that directory" is not a control. The person deleting it is doing something ordinary.

Every path is read from a variable in `.env`, so nothing in the compose files needs to change:

```bash
AUTH_SECRETS_DIR=/home/infra/auth-state/secrets
AUTH_BACKUP_DEST=/home/infra/auth-state/backups
AUTH_CONSOLE_DIST=/home/infra/auth-state/artifacts/console
AUTH_SITE_DIST=/home/infra/auth-state/artifacts/public-site
```

Note `AUTH_BACKUP_DEST`, not `AUTH_BACKUP_DIR`. Guessing that name once sent every dump back inside the checkout while the script reported success.

### 2a. Cloning

```bash
sudo git clone https://github.com/zed378/zed-auth.git /opt/zed-auth
sudo /opt/zed-auth/deploy/vm/secrets.sh fix
```

`secrets.sh fix` creates `/etc/zed-auth/secrets` owned by **uid 65532**, not by you. That ownership is load-bearing, and getting it wrong is not a subtle failure — the service will not boot.

The runtime image is distroless `:nonroot`, so the process runs as uid 65532. The secrets directory is a bind mount, so the host's ownership is what the container sees. A directory that is mode 700 and owned by the operator cannot even be *traversed* by the service, and the failure reads as `stat /etc/zed-auth/secrets/<file>: permission denied` on a file that plainly exists and plainly has the right mode.

The repair that first comes to mind — make the file group-readable — is refused by the secret resolver (`P0-14`), correctly. That leaves exactly one shape: directory and contents owned by the service's uid, `0700` and `0400`. You are not the service; read these files with `sudo /opt/zed-auth/deploy/vm/secrets.sh show <name>`.

Run `secrets.sh fix` again after adding any file by hand. It is idempotent.

### 3. Signing keys

Generate them **on this machine**. `docs/PLAN/02` § Constraints: no third party holds the private signing key. A key generated on a laptop and copied over has been on a laptop.

```bash
sudo openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 \
  -out /etc/zed-auth/secrets/jwt-signing-current.pem
sudo /opt/zed-auth/deploy/vm/secrets.sh fix
```

The resolver refuses a key file that is group- or world-readable (`P0-14`), so a wrong mode here fails at startup rather than silently. `secrets.sh fix` sets both the mode and the ownership; `chmod` alone leaves the file unreadable to the service.

**Staging and production must have different keys.** `deploy.sh` fingerprints them and refuses to start if two environments share a key — a shared key means a staging token is valid in production.

### 4. Environment file

```bash
sudo install -m 0600 -o root -g root \
  /opt/zed-auth/deploy/vm/env.example /etc/zed-auth/env
sudo "$EDITOR" /etc/zed-auth/env      # replace every REPLACE_ME
```

Generate passwords with `openssl rand -base64 32`. Do not reuse one between `auth_owner` and `auth_app`: the whole point of the split is that compromising the service does not yield owner access.

### 4b. Metrics scrape token

Required whenever `AUTH_ADMIN_ADDR` is not loopback — which includes this compose deployment, because the bind is `0.0.0.0` *inside* the container. The service cannot see what the host does or does not publish, and correctly declines to assume anything about it.

The metrics port is **not published to the host**. The endpoint is reachable inside the compose network, so a Prometheus container joined to that network scrapes `authservice:9090` with nothing further to configure. See `docker-compose.metrics-port.yml` if you need it on the host.

```bash
sudo /opt/zed-auth/deploy/vm/secrets.sh metrics-token
echo 'AUTH_ADMIN_TOKEN_REF=file:/etc/zed-auth/secrets/metrics-token' \
  | sudo tee -a /etc/zed-auth/env
```

Prometheus sends it as a bearer token. A request without it gets a bare `404` rather than a `401`: the endpoint does not confirm it exists to anyone probing for it.

The token is required even though the port is unpublished, and that is not belt-and-braces for its own sake. Publishing a port is one line in a file that someone will one day add for a good reason; the token is what makes that line safe to add.

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

**Set `AUTH_BACKUP_REMOTE`.** Without it, backups sit on the same disk as the database they protect — which guards against `DROP TABLE` and against nothing else. `docs/PLAN/15` requires a separate failure domain, and `backup.sh` warns loudly when this is unset because a local-only backup gives the *feeling* of safety without it.

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

**Signing keys are backed up separately from the database** (`docs/PLAN/15`), and restoring the two out of sync is the subtle failure `docs/PLAN/15` § Restore Testing warns about: tokens issued before the restore fail verification, and the error points at neither.

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
