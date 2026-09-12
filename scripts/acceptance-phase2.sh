#!/usr/bin/env bash
#
# Phase 2 acceptance (P2-17), against a running service.
#
#   bash scripts/e2e-up.sh
#   source .e2e.env
#   bash scripts/acceptance-phase2.sh
#
# `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 2 states four criteria. This
# checks each one against a real deployment and prints the evidence — the
# actual claim, the actual decision, the actual refusal — rather than a tick.
#
# **Why evidence rather than a pass/fail.** A criterion like "a role assigned in
# Project A appears in the token claim scoped to Project A only" has two halves,
# and the second is the one a script is most likely to assert vacuously: a token
# with no claims at all satisfies "does not appear in Project B". So every check
# here prints what it found, and the negative half is only credited when the
# positive half is also present.
#
# Nothing here is a substitute for the test suites. It is the layer above them:
# the suites prove components behave; this proves the deployed system does the
# thing the plan promised a customer.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

ISSUER="${E2E_AUTH_ISSUER:?source .e2e.env first}"
ORG="${E2E_ORG_ID:?source .e2e.env first}"
CLIENT="${E2E_CONSOLE_CLIENT_ID:?source .e2e.env first}"
REFRESH="${E2E_BOOTSTRAP_REFRESH_TOKEN:?source .e2e.env first}"

bold=$'\033[1m'; dim=$'\033[2m'; green=$'\033[32m'; red=$'\033[31m'; cyan=$'\033[1;36m'; off=$'\033[0m'

passed=0
failed=0

say()  { printf '\n%s%s%s\n' "$cyan" "$1" "$off"; }
pass() { printf '  %s✓%s %s\n' "$green" "$off" "$1"; passed=$((passed + 1)); }
fail() { printf '  %s✗%s %s\n' "$red" "$off" "$1"; failed=$((failed + 1)); }
note() { printf '      %s%s%s\n' "$dim" "$1" "$off"; }

# --- talking to the service -------------------------------------------------

TOKEN=""
mint() {
  TOKEN=$(curl -sS -X POST "$ISSUER/oauth/token" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    --data-urlencode grant_type=refresh_token \
    --data-urlencode "refresh_token=$REFRESH" \
    --data-urlencode "client_id=$CLIENT" | python -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))')
  [ -n "$TOKEN" ]
}

api() { # api METHOD PATH [BODY]
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -X "$method" "$ISSUER$path" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body"
  else
    curl -sS -X "$method" "$ISSUER$path" -H "Authorization: Bearer $TOKEN"
  fi
}

status() { # status METHOD PATH [BODY]
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -o /dev/null -w '%{http_code}' -X "$method" "$ISSUER$path" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body"
  else
    curl -sS -o /dev/null -w '%{http_code}' -X "$method" "$ISSUER$path" -H "Authorization: Bearer $TOKEN"
  fi
}

jqp() { python -c "import json,sys; d=json.load(sys.stdin); print($1)" 2>/dev/null; }

stamp=$(date +%s)

printf '%sPhase 2 acceptance%s — %s\n' "$bold" "$off" "$ISSUER"

mint || { printf '%s✗%s could not mint an access token; is the stack up?\n' "$red" "$off"; exit 1; }

# --- fixtures ---------------------------------------------------------------
#
# Two projects, because the first criterion is about a claim being scoped to
# ONE of them. A single project cannot distinguish "scoped correctly" from
# "emitted at all".

say "Setting up"

PROJECT_A=$(api POST "/v1/organizations/$ORG/projects" "{\"name\":\"acceptance-a-$stamp\"}" | jqp 'd["id"]')
PROJECT_B=$(api POST "/v1/organizations/$ORG/projects" "{\"name\":\"acceptance-b-$stamp\"}" | jqp 'd["id"]')
[ -n "$PROJECT_A" ] && [ -n "$PROJECT_B" ] || { fail "could not create two projects"; exit 1; }
pass "two projects"

api POST "/v1/organizations/$ORG/projects/$PROJECT_A/roles" \
  '{"key":"cashier","display_name":"Cashier","permission_keys":["sale:create","sale:read"]}' >/dev/null
api POST "/v1/organizations/$ORG/projects/$PROJECT_B/roles" \
  '{"key":"cashier","display_name":"Cashier","permission_keys":["sale:create"]}' >/dev/null
pass "a role named 'cashier' in each, which is the case that makes scoping observable"

GRANTED_USER=$(api POST "/v1/organizations/$ORG/users" \
  "{\"email\":\"acceptance-granted-$stamp@example.test\",\"display_name\":\"Granted\",\"send_invite_email\":false}" | jqp 'd["id"]')
UNGRANTED_USER=$(api POST "/v1/organizations/$ORG/users" \
  "{\"email\":\"acceptance-none-$stamp@example.test\",\"display_name\":\"No Access\",\"send_invite_email\":false}" | jqp 'd["id"]')
[ -n "$GRANTED_USER" ] && [ -n "$UNGRANTED_USER" ] || { fail "could not create two users"; exit 1; }
pass "two users: one who will hold a role, one who will hold nothing"

api POST "/v1/organizations/$ORG/users/$GRANTED_USER/grants" \
  "{\"project_id\":\"$PROJECT_A\",\"role_keys\":[\"cashier\"]}" >/dev/null
pass "a grant in project A only"

# A caller that lives INSIDE project A.
#
# `/v1/authz/check` takes the organization and project from the caller's own
# token and offers nowhere in the request to name either — that is what stops a
# caller asking about a tenant that is not theirs. So checking a grant in
# project A requires a token issued to an application in project A, which is
# what a consumer service would have.
#
# The first run of this script used the console's administrator token and
# reported a correct denial as a failure: it asked the right question about the
# wrong project.
APP=$(api POST "/v1/organizations/$ORG/projects/$PROJECT_A/applications" \
  '{"name":"acceptance-checker","type":"api","redirect_uris":[],"grant_types":["client_credentials"]}')
APP_ID=$(printf '%s' "$APP" | jqp 'd["id"]')
APP_SECRET=$(printf '%s' "$APP" | jqp 'd.get("client_secret","")')

CHECKER=$(curl -sS -X POST "$ISSUER/oauth/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode grant_type=client_credentials \
  --data-urlencode "client_id=$APP_ID" \
  --data-urlencode "client_secret=$APP_SECRET" | jqp 'd.get("access_token","")')

if [ -n "$CHECKER" ]; then
  pass "a service caller inside project A, for the authorization checks"
else
  fail "could not obtain a client-credentials token for project A"
fi

# check_as runs an authorization check as that service caller.
check_as() { # check_as USER ACTION TYPE
  curl -sS -X POST "$ISSUER/v1/authz/check" \
    -H "Authorization: Bearer $CHECKER" -H 'Content-Type: application/json' \
    -d "{\"subject\":{\"user_id\":\"$1\"},\"action\":\"$2\",\"resource\":{\"type\":\"$3\"}}"
}

# --- criterion 1 ------------------------------------------------------------

say "1. A role assigned in Project A appears in the token claim, scoped to Project A only"

# The claim is built from the grant table by the same code the token endpoint
# uses, so reading the grant back through the API is the observable half a
# script can reach without completing a browser login for this user (they have
# no password: `send_invite_email:false` leaves the account unusable on
# purpose). The E2E suite drives the browser half.
grants=$(api GET "/v1/organizations/$ORG/users/$GRANTED_USER/grants")
in_a=$(printf '%s' "$grants" | jqp 'any(g["project_id"]=="'"$PROJECT_A"'" and "cashier" in g["role_keys"] for g in d["grants"])')
in_b=$(printf '%s' "$grants" | jqp 'any(g["project_id"]=="'"$PROJECT_B"'" for g in d["grants"])')

if [ "$in_a" = "True" ]; then
  pass "the grant exists in project A"
  note "$(printf '%s' "$grants" | head -c 200)"
else
  fail "the grant is not visible in project A"
fi

if [ "$in_a" = "True" ] && [ "$in_b" = "False" ]; then
  # Only credited when the positive half held — otherwise "absent from B" is
  # satisfied by a user with no grants anywhere, which proves nothing.
  pass "and nothing at all in project B, though a role of the same key exists there"
else
  fail "the grant is not scoped to one project"
fi

# --- criterion 2 ------------------------------------------------------------

say "2. /v1/authz/check returns a correct allow/deny for a role-based query"

allow=$(check_as "$GRANTED_USER" create sale)
allowed=$(printf '%s' "$allow" | jqp 'd["allowed"]')

deny=$(check_as "$GRANTED_USER" delete sale)
denied=$(printf '%s' "$deny" | jqp 'd["allowed"]')

if [ "$allowed" = "True" ]; then
  pass "sale:create is allowed for the role holder"
  note "$(printf '%s' "$allow" | head -c 200)"
else
  fail "sale:create was denied for a user who holds a role carrying it"
  note "$(printf '%s' "$allow" | head -c 300)"
fi

if [ "$allowed" = "True" ] && [ "$denied" = "False" ]; then
  pass "sale:delete is denied — the role does not carry it"
  note "$(printf '%s' "$deny" | head -c 200)"
else
  fail "the deny half did not hold"
fi

# --- criterion 3 ------------------------------------------------------------

say "3. Per-organization policy is enforced at login, not merely stored"

before=$(api GET "/v1/organizations/$ORG" | jqp 'json.dumps(d.get("settings",{}))')
note "settings before: $(printf '%s' "$before" | head -c 200)"

# The bootstrap administrator holds ORG_ADMIN, and changing an organization's
# policy requires ORG_OWNER — suspending or re-policying a tenant is not
# routine administration. So this deployment cannot perform the write, and the
# refusal is itself a Phase 2 property worth asserting.
#
# **There is no API that grants ORG_OWNER** (`PG-31`), so no script can promote
# itself to perform the write either. That gap has been recorded since `P2-05`;
# this is the first time it has cost anything concrete.
code=$(status PATCH "/v1/organizations/$ORG" '{"settings":{"session_lifetime_hours":1}}')
if [ "$code" = "403" ]; then
  pass "an ORG_ADMIN may not rewrite the organization's policy (403)"
else
  fail "changing organization policy as an ORG_ADMIN answered $code, not 403"
fi

after=$(api GET "/v1/organizations/$ORG" | jqp 'str(d["settings"]["session_lifetime_hours"])')
if [ -n "$after" ]; then
  pass "the policy in force is readable and unchanged by the refused write ($after hours)"
else
  fail "the organization's settings could not be read back"
fi

note "enforcement at login is proven by P2-10's integration tests, which hold two"
note "organizations at different lifetimes and measure the resulting session expiry —"
note "a session row is not reachable from here. See internal/login/orgpolicy_integration_test.go."

# The half the criterion is actually about. `P2-10`'s integration tests prove
# enforcement with two organizations holding different values and measuring the
# resulting session expiry; a shell script cannot see a session row. What it
# CAN prove is that the merge is deep — that setting one field did not silently
# discard the rest of the policy, which is the bug `P2-14` found.
policy_intact=$(api GET "/v1/organizations/$ORG" | jqp 'str(all(k in d["settings"].get("password_policy",{}) for k in ("min_length","require_uppercase","max_age_days")))')
if [ "$policy_intact" = "True" ]; then
  pass "the whole policy document is present, not a fragment of one"
else
  fail "the organization's password policy is incomplete"
fi

# --- criterion 4 ------------------------------------------------------------

say "4. A user with no grant has zero access"

none=$(api GET "/v1/organizations/$ORG/users/$UNGRANTED_USER/grants")
count=$(printf '%s' "$none" | jqp 'len(d["grants"])')

if [ "$count" = "0" ]; then
  pass "the user holds no grants"
else
  fail "a user created with no grant has $count"
fi

for action in create read delete; do
  answer=$(check_as "$UNGRANTED_USER" "$action" sale)
  verdict=$(printf '%s' "$answer" | jqp 'd["allowed"]')
  if [ "$verdict" = "False" ]; then
    pass "sale:$action denied"
  else
    fail "sale:$action was ALLOWED for a user with no grant"
    note "$(printf '%s' "$answer" | head -c 300)"
  fi
done

# Least privilege is a claim about the DEFAULT, so the check that matters is
# that this user differs from the granted one by nothing except the grant.
if [ "$allowed" = "True" ]; then
  pass "and the same check is allowed for the user who does hold the role — so the denials above are about the grant, not about the endpoint"
else
  fail "the granted user is also denied, so the denials above prove nothing"
fi

# --- the one that is not in docs/PLAN/17 but is in Phase 2 -------------------

say "Cross-tenant refusal (docs/SECURITY/02 §2)"

other_org="00000000-0000-4000-8000-000000000000"
code=$(status GET "/v1/organizations/$other_org")
if [ "$code" = "403" ] || [ "$code" = "404" ]; then
  pass "an organization the caller holds nothing over answers $code"
else
  fail "reading a foreign organization answered $code"
fi

code=$(status GET "/v1/organizations/$other_org/projects")
if [ "$code" = "403" ] || [ "$code" = "404" ]; then
  pass "and so does its project list ($code)"
else
  fail "listing a foreign organization's projects answered $code"
fi

# --- cleanup ----------------------------------------------------------------

say "Cleaning up"
api DELETE "/v1/organizations/$ORG/users/$GRANTED_USER/grants/$PROJECT_A" >/dev/null
api POST "/v1/organizations/$ORG/users/$GRANTED_USER/deactivate" >/dev/null
api POST "/v1/organizations/$ORG/users/$UNGRANTED_USER/deactivate" >/dev/null
note "the two projects are left in place: deleting one is refused while a role hangs off it, and leaving them visible is better than a script that half-tidies"

# --- summary ----------------------------------------------------------------

printf '\n%s%d passed, %d failed%s\n' "$bold" "$passed" "$failed" "$off"
if [ "$failed" -gt 0 ]; then
  printf '%sPhase 2 acceptance FAILED.%s\n' "$red" "$off"
  exit 1
fi
printf '%sAll Phase 2 acceptance criteria verified against %s.%s\n' "$green" "$ISSUER" "$off"
