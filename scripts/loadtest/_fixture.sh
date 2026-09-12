#!/usr/bin/env bash
#
# Shared fixture for the load tests (P1-28, docs/PLAN/12).
#
# Sourced, never run. Creates a throwaway application and account, and deletes
# both on exit whether the run succeeded or not — a load test that leaves an
# account behind has changed the thing it was measuring.
#
# The password is generated here, lives only in this process's environment, and
# is hashed by `backend/cmd/passwordhash`, which reads it from the environment
# rather than argv: argv is visible in `ps` to every user on the host and ends
# up in shell history. It is only a fixture, and the habit is still worth more
# than the exception.

fixture_require() {
  : "${FIXTURE_NAME:?FIXTURE_NAME must be set before sourcing}"
  if [ ! -f "$HOME/auth-state/.env" ]; then
    echo "no $HOME/auth-state/.env — this runs on the staging VM, against its own stack" >&2
    exit 2
  fi
}

fixture_up() {
  fixture_require
  REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  EMAIL="${FIXTURE_NAME}@example.test"
  PASSWORD="Load-$(head -c 18 /dev/urandom | base64 | tr -d '=+/')"

  set -a; . "$HOME/auth-state/.env"; set +a
  COMPOSE=(docker compose -f "$REPO/deploy/vm/docker-compose.tunnel.yml")

  q "SELECT 1" >/dev/null || { echo "cannot reach postgres" >&2; exit 2; }

  ORG=$(q "SELECT id FROM organizations WHERE deleted_at IS NULL ORDER BY created_at LIMIT 1")
  PRJ=$(q "SELECT id FROM projects WHERE org_id = '$ORG' ORDER BY created_at LIMIT 1")
  [ -n "$ORG" ] && [ -n "$PRJ" ] || { echo "no organization or project to attach to" >&2; exit 2; }

  HASH=$(cd "$REPO/backend" && PASSWORD="$PASSWORD" go run ./cmd/passwordhash)
  [ -n "$HASH" ] || { echo "passwordhash produced nothing" >&2; exit 2; }

  USER_ID=$(q "INSERT INTO users (org_id, email, password_hash, status, display_name)
               VALUES ('$ORG', '$EMAIL', '$HASH', 'active', '$FIXTURE_NAME') RETURNING id")
  q "INSERT INTO manager_roles (user_id, role, scope_id)
     VALUES ('$USER_ID', 'ORG_ADMIN', '$ORG')" >/dev/null

  APP=$(q "INSERT INTO applications (project_id, org_id, name, type, redirect_uris, grant_types)
           VALUES ('$PRJ', '$ORG', '$FIXTURE_NAME', 'spa',
                   ARRAY['http://localhost:9998/callback'],
                   ARRAY['authorization_code','refresh_token']) RETURNING id")
  [ -n "$APP" ] || { echo "could not register the fixture application" >&2; exit 2; }
}

q() {
  "${COMPOSE[@]}" exec -T postgres psql -U auth_owner -d auth -tAqc "$1" 2>&1 | tr -d '\r'
}

fixture_down() {
  echo
  echo "Cleanup"
  q "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email = '$EMAIL')" >/dev/null
  q "DELETE FROM sessions       WHERE user_id IN (SELECT id FROM users WHERE email = '$EMAIL')" >/dev/null
  q "DELETE FROM manager_roles  WHERE user_id IN (SELECT id FROM users WHERE email = '$EMAIL')" >/dev/null
  q "DELETE FROM events         WHERE actor_user_id IN (SELECT id FROM users WHERE email = '$EMAIL')" >/dev/null
  q "DELETE FROM users          WHERE email = '$EMAIL'" >/dev/null
  q "DELETE FROM applications   WHERE name = '$FIXTURE_NAME'" >/dev/null
  # Printed rather than assumed. The counts are the proof.
  echo "  fixture accounts left:     $(q "SELECT count(*) FROM users WHERE email = '$EMAIL'")"
  echo "  fixture applications left: $(q "SELECT count(*) FROM applications WHERE name = '$FIXTURE_NAME'")"
}

fixture_banner() {
  echo "host        $(nproc) cores, $(free -g | awk '/^Mem:/{print $2}') GB, shared with $(docker ps -q | wc -l) containers"
  echo "service     $("${COMPOSE[@]}" images authservice --format '{{.Tag}}' 2>/dev/null | tail -1)"
  echo "fixture     application $APP, account $USER_ID"
  echo
}
