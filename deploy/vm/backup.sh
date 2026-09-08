#!/usr/bin/env bash
#
# Backs up the Auth Service database, and verifies the backup by restoring it.
#
# PLAN/15-DISASTER-RECOVERY.md § Restore Testing: "a backup that's never been
# tested isn't a backup you can rely on." This script therefore does not finish
# when pg_dump exits — it restores into a throwaway database and checks the
# schema and row counts survived. An unverified backup is a hypothesis, and the
# moment you discover it was wrong is the worst possible moment.
#
# PLAN/15 also requires backups in a separate failure domain from the primary.
# On a single VM (ADR-011, DV-01) that means shipping offsite: a backup on the
# same disk as the database protects against `DROP TABLE`, and against nothing
# else.
#
# Usage:
#   ./backup.sh                 scheduled backup + verify + ship offsite
#   ./backup.sh --pre-deploy    fast pre-deployment restore point
#   ./backup.sh --verify-only   restore the newest backup and check it

set -Eeuo pipefail

readonly ENV_FILE=/etc/zed-auth/env
COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly COMPOSE_DIR
readonly COMPOSE=(docker compose -f "${COMPOSE_DIR}/docker-compose.yml")

log()  { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m    %s\033[0m\n' "$*" >&2; }
die()  { printf '\n\033[1;31mBACKUP FAILED: %s\033[0m\n' "$*" >&2; exit 1; }

trap 'die "aborted at line $LINENO"' ERR

mode="${1:-scheduled}"

[[ -f "$ENV_FILE" ]] || die "$ENV_FILE not found"
set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a

: "${AUTH_ENV:?}"
: "${AUTH_POSTGRES_OWNER_PASSWORD:?}"
readonly DEST="${AUTH_BACKUP_DEST:-/var/backups/zed-auth}"
readonly RETENTION="${AUTH_BACKUP_RETENTION_DAYS:-30}"

mkdir -p "$DEST"
chmod 700 "$DEST"

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
readonly ARCHIVE="${DEST}/${AUTH_ENV}-${timestamp}.dump"

# --- Take the backup --------------------------------------------------------

if [[ "$mode" != "--verify-only" ]]; then
  log "Dumping ${AUTH_ENV} database"

  # Custom format: compressed, and restorable selectively, which matters during
  # an incident when you may want the audit log back before anything else.
  "${COMPOSE[@]}" exec -T \
    -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
    postgres pg_dump -U auth_owner -d auth --format=custom --no-owner \
    > "$ARCHIVE" \
    || die "pg_dump failed"

  chmod 600 "$ARCHIVE"

  size=$(stat -c '%s' "$ARCHIVE")
  # A dump smaller than a few KB is a dump of nothing. Catching that here beats
  # discovering it during a restore.
  (( size > 4096 )) || die "dump is only ${size} bytes — that is not a database"
  echo "    ${ARCHIVE} (${size} bytes)"
fi

newest="${ARCHIVE}"
[[ "$mode" == "--verify-only" ]] && newest=$(ls -1t "${DEST}"/*.dump 2>/dev/null | head -1)
[[ -f "$newest" ]] || die "no backup to verify"

# --- Verify by restoring ----------------------------------------------------
#
# Skipped for --pre-deploy, which runs inside a deployment window where the
# extra minutes matter and a scheduled verified backup already exists.

if [[ "$mode" != "--pre-deploy" ]]; then
  log "Verifying the backup by restoring it"

  verify_db="verify_$(date -u +%s)"

  cleanup_verify() {
    "${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
      postgres dropdb -U auth_owner --if-exists "$verify_db" >/dev/null 2>&1 || true
  }
  trap 'cleanup_verify; die "verification aborted at line $LINENO"' ERR

  "${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
    postgres createdb -U auth_owner "$verify_db" \
    || die "cannot create the verification database"

  "${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
    postgres pg_restore -U auth_owner -d "$verify_db" --no-owner < "$newest" \
    || { cleanup_verify; die "pg_restore failed — this backup is NOT usable"; }

  # Restoring without error is not the same as restoring correctly. Check that
  # the tables the plan requires actually arrived.
  expected_tables=(instances organizations users projects applications roles
                   user_grants project_grants manager_roles sessions
                   refresh_tokens signing_keys user_tokens events)

  for table in "${expected_tables[@]}"; do
    exists=$("${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
      postgres psql -U auth_owner -d "$verify_db" -tA \
      -c "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='${table}')" \
      | tr -d '[:space:]')
    [[ "$exists" == "t" ]] || { cleanup_verify; die "restored database is missing table ${table}"; }
  done

  # The audit log is the thing you most need after an incident and the thing
  # most easily lost to a partition that was not dumped.
  event_count=$("${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
    postgres psql -U auth_owner -d "$verify_db" -tA -c "SELECT count(*) FROM events" \
    | tr -d '[:space:]')

  echo "    all ${#expected_tables[@]} tables restored; events rows: ${event_count}"

  cleanup_verify
  trap 'die "aborted at line $LINENO"' ERR
  log "Backup verified"
fi

# --- Ship offsite -----------------------------------------------------------
#
# PLAN/15: backups live in a separate failure domain from the primary. A backup
# on the same disk as the database protects against DROP TABLE and nothing
# else — not disk failure, not the VM being lost, not ransomware.

if [[ "$mode" == "scheduled" ]]; then
  if [[ -n "${AUTH_BACKUP_REMOTE:-}" ]]; then
    log "Shipping offsite"
    case "$AUTH_BACKUP_REMOTE" in
      s3://*) aws s3 cp "$newest" "${AUTH_BACKUP_REMOTE}/" --only-show-errors ;;
      *:*)    scp -q "$newest" "$AUTH_BACKUP_REMOTE" ;;
      *)      die "AUTH_BACKUP_REMOTE format not recognised: $AUTH_BACKUP_REMOTE" ;;
    esac
    echo "    -> ${AUTH_BACKUP_REMOTE}"
  else
    # Loud, because a local-only backup gives the feeling of safety without it.
    warn "AUTH_BACKUP_REMOTE is not set — backups are LOCAL ONLY."
    warn "PLAN/15 requires a separate failure domain. Losing this VM loses"
    warn "the database and every backup of it together."
  fi

  log "Pruning backups older than ${RETENTION} days"
  find "$DEST" -name "${AUTH_ENV}-*.dump" -mtime "+${RETENTION}" -delete -print
fi

log "Done"
