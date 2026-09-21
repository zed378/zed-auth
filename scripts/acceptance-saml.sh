#!/usr/bin/env bash
#
# Registers a throwaway SAML service provider, runs the acceptance checks
# against the running deployment, and removes everything afterwards
# (P4-07, P4-08).
#
# Runs ON the staging VM. It seeds by SQL for a reason that is itself worth
# noticing: there is no API for registering a SAML service provider yet. The
# table, the flags and the enforcement all exist; the administrator does not.
# That is `P4-09`, and until it lands this script is the only way to create the
# row — which is exactly the argument for building it.
#
# Everything created here is named with one prefix and deleted on exit, whether
# the run passed or not. The counts are printed rather than assumed.
set -Eeuo pipefail

STAMP="$(date +%s)"
PREFIX="saml-${STAMP}"
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
  # The service provider first: assertion ids and name ids reference it, and
  # the sessions reference the users.
  q "DELETE FROM saml_name_ids        WHERE sp_id IN (SELECT id FROM saml_service_providers WHERE entity_id LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM saml_authn_requests  WHERE sp_id IN (SELECT id FROM saml_service_providers WHERE entity_id LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM saml_service_providers WHERE entity_id LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM saml_assertion_ids   WHERE org_id IN (SELECT id FROM organizations WHERE name LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM sessions        WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM refresh_tokens  WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM manager_roles   WHERE user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM events          WHERE actor_user_id IN (SELECT id FROM users WHERE email LIKE '${PREFIX}%')" >/dev/null
  q "DELETE FROM users           WHERE email LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM applications    WHERE name LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM projects        WHERE name LIKE '${PREFIX}%'" >/dev/null
  q "DELETE FROM organizations   WHERE name LIKE '${PREFIX}%'" >/dev/null
  echo "  service providers left: $(q "SELECT count(*) FROM saml_service_providers WHERE entity_id LIKE '${PREFIX}%'")"
  echo "  users left:             $(q "SELECT count(*) FROM users WHERE email LIKE '${PREFIX}%'")"
  echo "  organizations left:     $(q "SELECT count(*) FROM organizations WHERE name LIKE '${PREFIX}%'")"
}
trap cleanup EXIT

echo "Seeding ${PREFIX}"

INSTANCE=$(q "SELECT id FROM instances ORDER BY created_at LIMIT 1")
[ -n "$INSTANCE" ] || { echo "no instance row" >&2; exit 2; }

# The SAML signing key has to exist before any of this means anything. Checked
# here rather than left to the first check to discover, because "SAML is not
# configured on this instance" from every endpoint at once is a deployment
# step that was missed, not a test failure.
SAMLKEY=$(q "SELECT count(*) FROM signing_keys WHERE purpose = 'saml' AND state = 'current'")
[ "$SAMLKEY" = "1" ] || {
  echo "no current SAML signing key on this instance." >&2
  echo "  AUTH_ISSUER=... keyctl -purpose saml generate && keyctl -purpose saml rotate" >&2
  exit 2
}

ENTITY="${PREFIX}-sp.example.test"
ACS="https://${PREFIX}-sp.example.test/acs"

PASSWORD="Saml-$(head -c 18 /dev/urandom | base64 | tr -d '=+/')"
# Hashed by cmd/passwordhash, which reads the password from the environment
# rather than argv: argv is visible in `ps` to every user on the host.
HASH=$(cd "$REPO/backend" && PASSWORD="$PASSWORD" go run ./cmd/passwordhash)
[ -n "$HASH" ] || { echo "passwordhash produced nothing" >&2; exit 2; }

ORG=$(q "INSERT INTO organizations (instance_id, name) VALUES ('$INSTANCE', '${PREFIX}-org') RETURNING id")
PRJ=$(q "INSERT INTO projects (org_id, name) VALUES ('$ORG', '${PREFIX}-project') RETURNING id")

# type 'saml': the application row is the thing the console will manage, and
# the registration hangs off it. It takes no redirect URIs and no grant types,
# because a SAML service provider participates in neither.
APP=$(q "INSERT INTO applications (project_id, org_id, name, type, redirect_uris, grant_types)
         VALUES ('$PRJ', '$ORG', '${PREFIX}-app', 'saml', ARRAY[]::text[], ARRAY[]::text[]) RETURNING id")

SP=$(q "INSERT INTO saml_service_providers (application_id, org_id, entity_id, acs_url, attribute_release)
        VALUES ('$APP', '$ORG', '$ENTITY', '$ACS', ARRAY['email']) RETURNING id")
[ -n "$SP" ] || { echo "could not register the service provider" >&2; exit 2; }

# A SECOND registration, identical to the first in every respect except
# allow_idp_initiated.
#
# Two rows rather than one row toggled between passes. Toggling proves less
# than it looks: a refusal before the flip and a delivery after it is also what
# you would see if the flip had fixed something else, or if the first attempt
# had simply been too early. Two registrations differing in exactly one column,
# exercised in the same run against the same key and the same user, leave the
# flag as the only thing that can account for the difference.
IDP_ENTITY="${PREFIX}-idp-sp.example.test"
IDP_ACS="https://${PREFIX}-idp-sp.example.test/acs"
IDP_APP=$(q "INSERT INTO applications (project_id, org_id, name, type, redirect_uris, grant_types)
             VALUES ('$PRJ', '$ORG', '${PREFIX}-idp-app', 'saml', ARRAY[]::text[], ARRAY[]::text[]) RETURNING id")
IDP_SP=$(q "INSERT INTO saml_service_providers
              (application_id, org_id, entity_id, acs_url, attribute_release, allow_idp_initiated)
            VALUES ('$IDP_APP', '$ORG', '$IDP_ENTITY', '$IDP_ACS', ARRAY['email'], true) RETURNING id")
[ -n "$IDP_SP" ] || { echo "could not register the opted-in service provider" >&2; exit 2; }

USER_EMAIL="${PREFIX}-user@example.test"
USER_ID=$(q "INSERT INTO users (org_id, email, password_hash, status, display_name)
             VALUES ('$ORG', '$USER_EMAIL', '$HASH', 'active', '${PREFIX}-user') RETURNING id")
[ -n "$USER_ID" ] || { echo "could not seed the user" >&2; exit 2; }

echo "  organization     $ORG"
echo "  service provider $SP  ($ENTITY)"
echo "  opted in         $IDP_SP  ($IDP_ENTITY)"
echo

export ACCEPT_BASE="${ACCEPT_BASE:-http://127.0.0.1:10800}"
export ACCEPT_HOST="${ACCEPT_HOST:-auth.zedth.my.id}"
export ACCEPT_ISSUER="${AUTH_ISSUER:-https://${ACCEPT_HOST}}"

export SP_ENTITY_ID="$ENTITY" SP_ACS_URL="$ACS"
export SP_IDP_ENTITY_ID="$IDP_ENTITY" SP_IDP_ACS_URL="$IDP_ACS"
export SP_USER_EMAIL="$USER_EMAIL" SP_USER_PASSWORD="$PASSWORD"

cd "$REPO/scripts/acceptance/saml" && go run .
