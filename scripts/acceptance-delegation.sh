#!/usr/bin/env bash
#
# Seeds a delegation between three throwaway organizations, runs the acceptance
# checks against the running deployment, and removes everything afterwards
# (P4-01 … P4-06).
#
# Runs ON the staging VM. Two reasons it seeds by SQL rather than through the
# Management API, and both are the system working as intended:
#
#   Creating an organization needs INSTANCE_OWNER, which no fixture account
#   holds — a suite running with the most powerful role in the system cannot
#   notice a missing permission check.
#
#   The receiving side of a grant cannot be created by the receiving side. That
#   is the whole point of delegation, so there is no call the partner's
#   administrator could make to give itself one.
#
# Everything created here is named with one prefix and deleted on exit, whether
# the run passed or not. The counts are printed rather than assumed.
set -Eeuo pipefail

STAMP="$(date +%s)"
PREFIX="deleg-${STAMP}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

[ -f "$HOME/auth-state/.env" ] || {
  echo "no $HOME/auth-state/.env — this runs on the staging VM, against its own stack" >&2
  exit 2
}
set -a; . "$HOME/auth-state/.env"; set +a

COMPOSE=(docker compose -f "$REPO/deploy/vm/docker-compose.tunnel.yml")
q() { "${COMPOSE[@]}" exec -T postgres psql -U auth_owner -d auth -tAqc "$1" 2>&1 | tr -d '\r'; }

cleanup() {
  echo
  echo "Cleanup"
  # Order matters: children before parents, and the grants before the projects
  # whose roles they name.
  q "DELETE FROM user_grants    WHERE project_grant_id IN (SELECT id FROM project_grants WHERE project_id IN (SELECT id FROM projects WHERE name LIKE '${PREFIX}%'))" >/dev/null
  q "DELETE FROM project_grants WHERE project_id IN (SELECT id FROM projects WHERE name LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM sessions       WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM manager_roles  WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM events         WHERE actor_user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM user_grants    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM users          WHERE email LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM applications   WHERE name LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM roles          WHERE project_id IN (SELECT id FROM projects WHERE name LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM projects       WHERE name LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM organizations  WHERE name LIKE '${PREFIX}%'" >/dev/null
  echo "  organizations left: $(q "SELECT count(*) FROM organizations WHERE name LIKE '${PREFIX}%'")"
  echo "  users left:         $(q "SELECT count(*) FROM users         WHERE email LIKE '${PREFIX}%'")"
  echo "  grants left:        $(q "SELECT count(*) FROM project_grants WHERE project_id IN (SELECT id FROM projects WHERE name LIKE '${PREFIX}%')")"
}
trap cleanup EXIT

echo "Seeding ${PREFIX}"

INSTANCE=$(q "SELECT id FROM instances ORDER BY created_at LIMIT 1")
[ -n "$INSTANCE" ] || { echo "no instance row" >&2; exit 2; }

REDIRECT="http://localhost:9998/callback"
PASSWORD="Deleg-$(head -c 18 /dev/urandom | base64 | tr -d '=+/')"
# Hashed by cmd/passwordhash, which reads the password from the environment
# rather than argv: argv is visible in `ps` to every user on the host.
HASH=$(cd "$REPO/backend" && PASSWORD="$PASSWORD" go run ./cmd/passwordhash)
[ -n "$HASH" ] || { echo "passwordhash produced nothing" >&2; exit 2; }

org() { q "INSERT INTO organizations (instance_id, name) VALUES ('$INSTANCE', '$1') RETURNING id"; }
VENDOR=$(org "${PREFIX}-vendor")
PARTNER=$(org "${PREFIX}-partner")
BYSTANDER=$(org "${PREFIX}-bystander")

project() { q "INSERT INTO projects (org_id, name) VALUES ('$1', '$2') RETURNING id"; }
VENDOR_PRJ=$(project "$VENDOR" "${PREFIX}-till")
PARTNER_PRJ=$(project "$PARTNER" "${PREFIX}-own")
BYSTANDER_PRJ=$(project "$BYSTANDER" "${PREFIX}-theirs")

SHARED="${PREFIX}-cashier"
WITHHELD="${PREFIX}-manager"
for key in "$SHARED" "$WITHHELD"; do
  q "INSERT INTO roles (org_id, project_id, key, display_name) VALUES ('$VENDOR', '$VENDOR_PRJ', '$key', '$key')" >/dev/null
done
q "INSERT INTO roles (org_id, project_id, key, display_name) VALUES ('$PARTNER', '$PARTNER_PRJ', '${PREFIX}-clerk', 'clerk')" >/dev/null

# A → B, sharing exactly one of the vendor's two roles.
GRANT=$(q "INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
           VALUES ('$VENDOR_PRJ', '$VENDOR', '$PARTNER', ARRAY['$SHARED']) RETURNING id")
# B → C, so the partner can SEE a grant it made. Check 2 is about the filter
# leaving this out, which is only meaningful if the row exists and is visible.
OWN_GRANT=$(q "INSERT INTO project_grants (project_id, granting_org_id, granted_org_id, granted_role_keys)
               VALUES ('$PARTNER_PRJ', '$PARTNER', '$BYSTANDER', ARRAY['${PREFIX}-clerk']) RETURNING id")
[ -n "$GRANT" ] && [ -n "$OWN_GRANT" ] || { echo "could not seed the grants" >&2; exit 2; }

admin() { # org, label -> user id, with ORG_ADMIN over that organization
  local id
  id=$(q "INSERT INTO users (org_id, email, password_hash, status, display_name)
          VALUES ('$1', '${PREFIX}-$2@example.test', '$HASH', 'active', '${PREFIX}-$2') RETURNING id")
  q "INSERT INTO manager_roles (user_id, role, scope_id) VALUES ('$id', 'ORG_ADMIN', '$1')" >/dev/null
  printf '%s' "$id"
}
VENDOR_ADMIN=$(admin "$VENDOR" vendor-admin)
PARTNER_ADMIN=$(admin "$PARTNER" partner-admin)
BYSTANDER_ADMIN=$(admin "$BYSTANDER" bystander-admin)

# An ordinary member of the partner, to receive the delegated role. Not the
# administrator: P4-02 refuses a caller assigning to themselves.
MEMBER=$(q "INSERT INTO users (org_id, email, password_hash, status, display_name)
            VALUES ('$PARTNER', '${PREFIX}-member@example.test', '$HASH', 'active', '${PREFIX}-member') RETURNING id")

app() { q "INSERT INTO applications (project_id, org_id, name, type, redirect_uris, grant_types)
           VALUES ('$1', '$2', '${PREFIX}-$3', 'spa', ARRAY['$REDIRECT'], ARRAY['authorization_code','refresh_token']) RETURNING id"; }
VENDOR_CLIENT=$(app "$VENDOR_PRJ" "$VENDOR" vendor-app)
PARTNER_CLIENT=$(app "$PARTNER_PRJ" "$PARTNER" partner-app)
BYSTANDER_CLIENT=$(app "$BYSTANDER_PRJ" "$BYSTANDER" bystander-app)

echo "  vendor    $VENDOR  grant $GRANT"
echo "  partner   $PARTNER  own grant $OWN_GRANT"
echo "  bystander $BYSTANDER"
echo

# The vendor's own token, for the revocation in check 5. Obtained the same way
# the console would, through the hosted flow, by the acceptance program itself.
export ACCEPT_BASE="${ACCEPT_BASE:-http://127.0.0.1:10800}"
export ACCEPT_HOST="${ACCEPT_HOST:-auth.zedth.my.id}"

export DELEG_VENDOR_ORG="$VENDOR" DELEG_PARTNER_ORG="$PARTNER" DELEG_BYSTANDER_ORG="$BYSTANDER"
export DELEG_GRANT_ID="$GRANT" DELEG_OWN_GRANT_ID="$OWN_GRANT"
export DELEG_SHARED_ROLE="$SHARED" DELEG_WITHHELD_ROLE="$WITHHELD"
export DELEG_PARTNER_MEMBER="$MEMBER"
export DELEG_PARTNER_CLIENT="$PARTNER_CLIENT" DELEG_PARTNER_EMAIL="${PREFIX}-partner-admin@example.test" DELEG_PARTNER_PASSWORD="$PASSWORD"
export DELEG_BYSTANDER_CLIENT="$BYSTANDER_CLIENT" DELEG_BYSTANDER_EMAIL="${PREFIX}-bystander-admin@example.test" DELEG_BYSTANDER_PASSWORD="$PASSWORD"
export DELEG_VENDOR_CLIENT="$VENDOR_CLIENT" DELEG_VENDOR_EMAIL="${PREFIX}-vendor-admin@example.test" DELEG_VENDOR_PASSWORD="$PASSWORD"
export DELEG_VENDOR_ADMIN="$VENDOR_ADMIN" DELEG_PARTNER_ADMIN="$PARTNER_ADMIN" DELEG_BYSTANDER_ADMIN="$BYSTANDER_ADMIN"

cd "$REPO/scripts/acceptance/delegation" && go run .
