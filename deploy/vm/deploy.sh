#!/usr/bin/env bash
#
# Deploys a new image to this VM.
#
# The ordering is the point. PLAN/14 § Release Process requires migrations to
# run as a separate, reviewable step BEFORE the application rolls out, and
# PLAN/14 § Rollback Strategy requires migrations to be backward-compatible for
# at least one release so that rolling the application back never requires
# rolling the database back.
#
#   1. Verify the image exists and is pinned by digest
#   2. Back up the database
#   3. Run migrations (separately, as the owner role)
#   4. Roll out the application (as the app role)
#   5. Smoke test
#   6. Roll back the APPLICATION only, if the smoke test fails
#
# Usage:
#   sudo ./deploy.sh ghcr.io/zed378/zed-auth@sha256:abc123...

set -Eeuo pipefail

# Overridable because the environment file is not always at the same path: a
# host-managed deployment keeps it in /etc, while a deployment confined to a
# user's home directory keeps it alongside the project. Hard-coding /etc would
# make this script unusable in the second case without editing it.
readonly ENV_FILE="${AUTH_ENV_FILE:-/etc/zed-auth/env}"
COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly COMPOSE_DIR
# Overridable so the tunnel variant (docker-compose.tunnel.yml) can be used
# without a second copy of this script.
readonly COMPOSE_FILE="${AUTH_COMPOSE_FILE:-docker-compose.yml}"
readonly COMPOSE=(docker compose -f "${COMPOSE_DIR}/${COMPOSE_FILE}")

log()  { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m    %s\033[0m\n' "$*" >&2; }
die()  { printf '\n\033[1;31mDEPLOY FAILED: %s\033[0m\n' "$*" >&2; exit 1; }

trap 'die "aborted at line $LINENO"' ERR

# --- 1. Preconditions -------------------------------------------------------

new_image="${1:-}"
[[ -n "$new_image" ]] || die "usage: $0 <image@sha256:digest>"

# A tag is mutable: whoever can push can change what "v1.2.3" points at, so a
# rollback to "the same tag" can silently deploy different code
# (SECURITY/02 §15 Supply-Chain).
[[ "$new_image" == *"@sha256:"* ]] \
  || die "image must be pinned by digest, not by tag: $new_image"

[[ -f "$ENV_FILE" ]] || die "$ENV_FILE not found — see deploy/vm/env.example"

perms=$(stat -c '%a' "$ENV_FILE")
[[ "$perms" == "600" ]] \
  || die "$ENV_FILE is mode $perms, must be 600 — it holds every credential"

set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a

: "${AUTH_ENV:?AUTH_ENV must be set in $ENV_FILE}"
: "${AUTH_DOMAIN:?AUTH_DOMAIN must be set in $ENV_FILE}"

log "Deploying to ${AUTH_ENV} (${AUTH_DOMAIN})"
echo "    image: ${new_image}"

# PLAN/13 and PLAN/14: production keys and data are never shared with staging.
# The check is cheap and the failure mode — a staging token being valid in
# production — is catastrophic and silent.
if [[ -d /etc/zed-auth/secrets ]]; then
  fingerprint_file=/etc/zed-auth/.key-fingerprints
  current_fp=$(find /etc/zed-auth/secrets -name '*.pem' -type f -exec sha256sum {} \; \
               | awk '{print $1}' | sort | sha256sum | awk '{print $1}')
  if [[ -f "$fingerprint_file" ]]; then
    while IFS='=' read -r env fp; do
      if [[ "$env" != "$AUTH_ENV" && "$fp" == "$current_fp" ]]; then
        die "signing keys are identical to the ${env} environment. Every environment must have its own keys (PLAN/13, PLAN/14) — a shared key means a ${env} token is valid here."
      fi
    done < "$fingerprint_file"
  fi
  grep -v "^${AUTH_ENV}=" "$fingerprint_file" 2>/dev/null > "${fingerprint_file}.tmp" || true
  echo "${AUTH_ENV}=${current_fp}" >> "${fingerprint_file}.tmp"
  mv "${fingerprint_file}.tmp" "$fingerprint_file"
  chmod 600 "$fingerprint_file"
fi

log "Pulling image"
docker pull "$new_image" >/dev/null || die "cannot pull $new_image"

previous_image="${AUTH_IMAGE:-}"
echo "    previous: ${previous_image:-<none>}"

# --- 2. Back up before touching anything ------------------------------------

log "Backing up the database before migrating"
if ! "${COMPOSE_DIR}/backup.sh" --pre-deploy; then
  die "backup failed — refusing to migrate without a restore point"
fi

# --- 3. Migrations, as a separate step --------------------------------------
#
# Run as the OWNER role in a one-off container, while the previous application
# version is still serving. This is safe only because migrations are
# expand/contract: the running version must tolerate the new schema
# (PLAN/14 § Rollback Strategy, enforced by the migration-safety CI job).

log "Applying migrations"
"${COMPOSE[@]}" run --rm --no-deps \
  -e AUTH_MIGRATE_DSN="${AUTH_MIGRATE_DSN}" \
  --entrypoint /migrate \
  authservice up \
  || die "migrations failed — the application has NOT been rolled out, so the previous version is still serving against the old schema"

# --- 4. Roll out ------------------------------------------------------------

log "Rolling out the application"
sed -i "s|^AUTH_IMAGE=.*|AUTH_IMAGE=${new_image}|" "$ENV_FILE"
export AUTH_IMAGE="$new_image"

"${COMPOSE[@]}" up -d --remove-orphans authservice

# --- 5. Smoke test ----------------------------------------------------------

log "Waiting for readiness"
ready=false
for _ in $(seq 1 30); do
  if "${COMPOSE[@]}" exec -T authservice /authservice -healthcheck >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 2
done

if [[ "$ready" == true ]]; then
  log "Smoke testing through the public endpoint"
  # Through Caddy, over TLS, exactly as a consumer application reaches it —
  # testing the container directly would not catch a broken certificate or a
  # misrouted proxy, which are the two most likely single-VM failures.
  if ! curl --fail --silent --show-error --max-time 10 \
       "https://${AUTH_DOMAIN}/healthz" >/dev/null; then
    ready=false
    warn "the service is healthy internally but not reachable over TLS"
  fi
fi

# --- 6. Roll back the application only --------------------------------------

if [[ "$ready" != true ]]; then
  warn "smoke test failed"

  if [[ -n "$previous_image" ]]; then
    log "Rolling back to ${previous_image}"
    # The APPLICATION only. The schema stays forward: migrations are
    # expand/contract, so the previous version runs against it. Rolling the
    # database back here would be the more dangerous action, not the safer one.
    sed -i "s|^AUTH_IMAGE=.*|AUTH_IMAGE=${previous_image}|" "$ENV_FILE"
    export AUTH_IMAGE="$previous_image"
    "${COMPOSE[@]}" up -d authservice
    warn "rolled back. The schema was NOT rolled back — that is deliberate."
  else
    warn "no previous image recorded; cannot roll back automatically"
  fi

  "${COMPOSE[@]}" logs --tail 50 authservice >&2 || true
  die "deployment rolled back"
fi

log "Deployed successfully"
"${COMPOSE[@]}" ps
