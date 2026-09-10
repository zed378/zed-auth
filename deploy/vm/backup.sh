#!/usr/bin/env bash
#
# Backs up the Auth Service database, and verifies the backup by restoring it.
#
# docs/PLAN/15-DISASTER-RECOVERY.md § Restore Testing: "a backup that's never been
# tested isn't a backup you can rely on." This script therefore does not finish
# when pg_dump exits — it restores into a throwaway database and checks the
# schema and row counts survived. An unverified backup is a hypothesis, and the
# moment you discover it was wrong is the worst possible moment.
#
# docs/PLAN/15 also requires backups in a separate failure domain from the primary.
# On a single VM (ADR-011, DV-01) that means shipping offsite: a backup on the
# same disk as the database protects against `DROP TABLE`, and against nothing
# else.
#
# Usage:
#   ./backup.sh                 scheduled backup + verify + ship offsite
#   ./backup.sh --pre-deploy    fast pre-deployment restore point
#   ./backup.sh --verify-only   restore the newest backup and check it

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

  # Restoring without error is not the same as restoring correctly.
  #
  # This compares the restored database against the SOURCE rather than against
  # a list written here. The list version passed and reported "all 14 tables
  # restored", which read like completeness and meant "all 14 I was told to
  # look for" — a table added by a future migration would not have been
  # checked, and the event partitions were not checked at all, in a script
  # whose own comment says a partition that was not dumped is the likeliest
  # way to lose the audit log.
  tables_in() {
    "${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
      postgres psql -U auth_owner -d "$1" -tA -c \
      "SELECT table_name FROM information_schema.tables
        WHERE table_schema = 'public' ORDER BY table_name" \
      | tr -d '\r' | grep -v '^$' | sort
  }

  source_tables=$(tables_in "${AUTH_POSTGRES_DB:-auth}")
  restored_tables=$(tables_in "$verify_db")

  # The floor. Set comparison alone would pass if BOTH were empty — the
  # vacuous pass this project keeps meeting. A source with fewer tables than
  # the schema's core means something is wrong before the backup is even in
  # question.
  core_tables=(instances organizations users projects applications roles
               user_grants project_grants manager_roles sessions
               refresh_tokens signing_keys user_tokens events)

  for table in "${core_tables[@]}"; do
    grep -qx "$table" <<<"$source_tables" \
      || { cleanup_verify; die "the SOURCE database is missing ${table} — the backup is not the problem"; }
  done

  missing=$(comm -23 <(echo "$source_tables") <(echo "$restored_tables"))
  if [[ -n "$missing" ]]; then
    warn "tables present in the source but NOT in the restore:"
    echo "$missing" | sed 's/^/      /' >&2
    cleanup_verify
    die "this backup is incomplete"
  fi

  table_count=$(wc -l <<<"$source_tables" | tr -d '[:space:]')

  # Row counts per table, not just presence. A restored table that arrived
  # empty is a restore that looks fine and loses everything.
  mismatches=0
  while read -r table; do
    [[ -z "$table" ]] && continue
    src=$("${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
      postgres psql -U auth_owner -d "${AUTH_POSTGRES_DB:-auth}" -tA \
      -c "SELECT count(*) FROM \"$table\"" | tr -d '[:space:]')
    dst=$("${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
      postgres psql -U auth_owner -d "$verify_db" -tA \
      -c "SELECT count(*) FROM \"$table\"" | tr -d '[:space:]')

    if [[ "$src" != "$dst" ]]; then
      warn "row count differs for ${table}: source ${src}, restored ${dst}"
      mismatches=$((mismatches + 1))
    fi
  done <<<"$source_tables"

  if (( mismatches > 0 )); then
    cleanup_verify
    die "${mismatches} table(s) restored with the wrong number of rows"
  fi

  event_count=$("${COMPOSE[@]}" exec -T -e PGPASSWORD="${AUTH_POSTGRES_OWNER_PASSWORD}" \
    postgres psql -U auth_owner -d "$verify_db" -tA -c "SELECT count(*) FROM events" \
    | tr -d '[:space:]')

  echo "    ${table_count} tables restored, row counts match the source; events rows: ${event_count}"

  cleanup_verify
  trap 'die "aborted at line $LINENO"' ERR
  log "Backup verified"
fi

# --- Ship offsite -----------------------------------------------------------
#
# docs/PLAN/15: backups live in a separate failure domain from the primary. A backup
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
    warn "docs/PLAN/15 requires a separate failure domain. Losing this VM loses"
    warn "the database and every backup of it together."
  fi

  log "Pruning backups older than ${RETENTION} days"
  find "$DEST" -name "${AUTH_ENV}-*.dump" -mtime "+${RETENTION}" -delete -print
fi

log "Done"
