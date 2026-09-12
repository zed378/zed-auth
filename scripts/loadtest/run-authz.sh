#!/usr/bin/env bash
#
# Load-test `/v1/authz/check` against `docs/PLAN/12`'s RBAC targets (P2-17).
#
#   p50 < 20ms   p95 < 80ms   p99 < 150ms
#
# **Seeded entirely through the Management API**, unlike `run.sh`, which reaches
# into the database because it needs a password hash and a manager role and
# neither has an endpoint. Everything this phase needs — a project, a role, a
# user, a grant, and a service client — does have one.
#
# That is not tidiness. `run.sh` only works on the staging VM, and this session
# spent a week unable to measure anything because the VM was unreachable. A
# harness that runs against any deployment it can get a token for is one that
# still runs when the usual one is down.
#
#   source .e2e.env            # a local stack, from scripts/e2e-up.sh
#   bash scripts/loadtest/run-authz.sh
#
#   LOAD_WORKERS   concurrent workers (default 20)
#   LOAD_SECONDS   seconds (default 30)
#   LOAD_STRICT    divide every target by N — the harness's own mutation test
#
# Exits non-zero when the phase misses a target or sees failures.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

ISSUER="${E2E_AUTH_ISSUER:?source the stack environment first}"
ORG="${E2E_ORG_ID:?source the stack environment first}"
CLIENT="${E2E_CONSOLE_CLIENT_ID:?source the stack environment first}"
REFRESH="${E2E_BOOTSTRAP_REFRESH_TOKEN:?source the stack environment first}"

stamp=$(date +%s)
NAME="authz-load-$stamp"

jqp() { python -c "import json,sys; d=json.load(sys.stdin); print($1)" 2>/dev/null; }

TOKEN=$(curl -sS -X POST "$ISSUER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode grant_type=refresh_token \
  --data-urlencode "refresh_token=$REFRESH" \
  --data-urlencode "client_id=$CLIENT" | jqp 'd.get("access_token","")')
[ -n "$TOKEN" ] || { echo "could not mint an administrator token; is the stack up?" >&2; exit 2; }

api() {
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -X "$method" "$ISSUER$path" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body"
  else
    curl -sS -X "$method" "$ISSUER$path" -H "Authorization: Bearer $TOKEN"
  fi
}

echo "Seeding through the Management API"

PRJ=$(api POST "/v1/organizations/$ORG/projects" "{\"name\":\"$NAME\"}" | jqp 'd["id"]')
[ -n "$PRJ" ] || { echo "could not create a project" >&2; exit 2; }

api POST "/v1/organizations/$ORG/projects/$PRJ/roles" \
  '{"key":"cashier","display_name":"Cashier","permission_keys":["sale:create","sale:read"]}' >/dev/null

USER_ID=$(api POST "/v1/organizations/$ORG/users" \
  "{\"email\":\"$NAME@example.test\",\"display_name\":\"$NAME\",\"send_invite_email\":false}" | jqp 'd["id"]')
[ -n "$USER_ID" ] || { echo "could not create a user" >&2; exit 2; }

api POST "/v1/organizations/$ORG/users/$USER_ID/grants" \
  "{\"project_id\":\"$PRJ\",\"role_keys\":[\"cashier\"]}" >/dev/null

APP=$(api POST "/v1/organizations/$ORG/projects/$PRJ/applications" \
  "{\"name\":\"$NAME\",\"type\":\"api\",\"redirect_uris\":[],\"grant_types\":[\"client_credentials\"]}")
APP_ID=$(printf '%s' "$APP" | jqp 'd["id"]')
APP_SECRET=$(printf '%s' "$APP" | jqp 'd.get("client_secret","")')
[ -n "$APP_ID" ] && [ -n "$APP_SECRET" ] || { echo "could not register a service client" >&2; exit 2; }

CHECKER=$(curl -sS -X POST "$ISSUER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode grant_type=client_credentials \
  --data-urlencode "client_id=$APP_ID" \
  --data-urlencode "client_secret=$APP_SECRET" | jqp 'd.get("access_token","")')
[ -n "$CHECKER" ] || { echo "could not obtain a client-credentials token" >&2; exit 2; }

cleanup() {
  echo
  echo "Cleanup"
  curl -sS -X DELETE "$ISSUER/v1/organizations/$ORG/users/$USER_ID/grants/$PRJ" \
    -H "Authorization: Bearer $TOKEN" >/dev/null
  curl -sS -X POST "$ISSUER/v1/organizations/$ORG/users/$USER_ID/deactivate" \
    -H "Authorization: Bearer $TOKEN" >/dev/null
  curl -sS -X DELETE "$ISSUER/v1/organizations/$ORG/projects/$PRJ/applications/$APP_ID" \
    -H "Authorization: Bearer $TOKEN" >/dev/null
  # The project and its role are left: deleting a role is refused while a grant
  # references it, and a half-tidying script is worse than one that says what
  # it left.
  echo "  left in place: project $PRJ and its 'cashier' role"
}
trap cleanup EXIT

# A smoke check before measuring 20 workers' worth of it. A phase that measures
# the denial path is measuring the wrong path, and finding that out from a
# latency number rather than from a sentence wastes a run.
probe=$(curl -sS -X POST "$ISSUER/v1/authz/check" \
  -H "Authorization: Bearer $CHECKER" -H 'Content-Type: application/json' \
  -d "{\"subject\":{\"user_id\":\"$USER_ID\"},\"action\":\"create\",\"resource\":{\"type\":\"sale\"}}")
case "$probe" in
  *'"allowed":true'*) echo "  the fixture decides ALLOW, which is the path being sized" ;;
  *) echo "the fixture does not decide allow, so the run would measure the wrong path:" >&2
     echo "  $probe" >&2
     exit 2 ;;
esac

echo "  project $PRJ, subject $USER_ID, service client $APP_ID"
echo

# The pace comes from the service's own constant, not from a number typed
# here. A load test that drives an endpoint past its documented bound is
# measuring the rate limiter — which is what the first run of this phase did,
# reporting percentiles blended from 14,470 served requests and 2,471 fast
# 429 rejections.
QUOTA_GO=backend/internal/ratelimit/quota.go
AUTHZ_LIMIT=$(sed -n 's/^var PerClientAuthz = Quota{Limit: \([0-9]*\), Window: time.Minute}.*/\1/p' "$QUOTA_GO")
if [ -z "$AUTHZ_LIMIT" ]; then
  echo "could not read PerClientAuthz out of $QUOTA_GO — the pace would be a guess" >&2
  exit 2
fi
AUTHZ_RPS=$((AUTHZ_LIMIT / 60))
echo "  bound       $AUTHZ_LIMIT/minute (${AUTHZ_RPS}/s), read from $QUOTA_GO"
echo

cd scripts/loadtest || exit 2
LOAD_BASE="$ISSUER" \
LOAD_HOST="$(printf '%s' "$ISSUER" | sed -E 's#^https?://##; s#/.*##; s#:.*##')" \
LOAD_CLIENT_ID="$CLIENT" \
LOAD_EMAIL="unused-for-this-phase" \
LOAD_ORG="$ORG" \
LOAD_ONLY="authz" \
LOAD_AUTHZ_TOKEN="$CHECKER" \
LOAD_AUTHZ_SUBJECT="$USER_ID" \
LOAD_AUTHZ_RPS="$AUTHZ_RPS" \
LOAD_WORKERS="${LOAD_WORKERS:-20}" \
LOAD_SECONDS="${LOAD_SECONDS:-30}" \
LOAD_STRICT="${LOAD_STRICT:-1}" \
  go run .
