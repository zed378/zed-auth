#!/usr/bin/env bash
#
# Brings up everything the end-to-end suite needs, and prints the environment
# it needs to run against.
#
#   bash scripts/e2e-up.sh          # start and seed
#   source .e2e.env                 # take the environment it produced
#   cd console && npm run e2e       # run the suite
#   bash scripts/e2e-down.sh        # stop everything
#
# `docs/PLAN/11` § End-to-End asks for a successful login, SSO between the two
# demo applications, logout genuinely ending a session, and the console's
# invite flow. None of those can be faked: each needs a real service, a real
# browser and two genuinely separate consumer applications. This script is what
# makes that a command rather than an afternoon.
#
# **Nothing here reaches into the database except the bootstrap.** The
# organization, the first administrator and the console's own client have to be
# inserted, because no published endpoint can create them yet (`PG-26`).
# Everything after that — the demo applications, the per-test projects, users
# and invitations — goes through the Management API, using the same endpoints
# an operator would. A suite seeded past its own API is a suite that proves
# nothing about the API.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1
ROOT="$PWD"

COMPOSE="deploy/docker-compose.yml"
ISSUER="${E2E_AUTH_ISSUER:-http://localhost:8080}"
MAILPIT="${E2E_MAILPIT_URL:-http://localhost:8025}"
# `localhost`, not `127.0.0.1`. They are different sites to a browser, and the
# console's silent renewal is an iframe against the issuer that needs the SSO
# cookie — which a third-party iframe does not get. See playwright.config.ts.
CONSOLE_URL="${E2E_BASE_URL:-http://localhost:4173}"
DEMO_A_URL="${E2E_DEMO_A_URL:-http://localhost:8090}"
DEMO_B_URL="${E2E_DEMO_B_URL:-http://localhost:8091}"

ADMIN_EMAIL="e2e-admin@example.test"
ADMIN_PASSWORD="Correct-Horse-Battery-Staple-E2E-Admin"
ENV_FILE="$ROOT/.e2e.env"
PID_FILE="$ROOT/.e2e.pids"

say()  { printf '\n\033[1;36m%s\033[0m\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
die()  { printf '  \033[31m✗\033[0m %s\n' "$1"; exit 1; }

psql_() {
  docker compose -f "$COMPOSE" exec -T postgres \
    psql -U auth_owner -d auth -tAqc "$1" 2>/dev/null | tr -d '\r'
}

# --- the stack ---------------------------------------------------------------

say "Starting Postgres, Redis, Mailpit and the service"

docker compose -f "$COMPOSE" up -d --build >/dev/null 2>&1 || die "docker compose up failed"

# Readiness, not liveness. `/readyz` consults the database and Redis, which is
# the question being asked here; `/healthz` answers yes from a process that
# cannot reach either.
for _ in $(seq 1 60); do
  if [ "$(curl -sS -o /dev/null -w '%{http_code}' "$ISSUER/readyz" 2>/dev/null)" = "200" ]; then
    break
  fi
  sleep 2
done
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ISSUER/readyz")" = "200" ] \
  || die "the service never became ready at $ISSUER/readyz"
ok "the service is ready at $ISSUER"

# Migrations, as a deliberate step — after readiness, because that is how we
# know Postgres is accepting connections.
#
# The service does NOT run them at startup (`docs/PLAN/14` § Release Process),
# which is right for a deployment and surprising locally: `docker compose up`
# against an existing volume gives a service that starts happily on a schema of
# any age. This stack was found ten versions behind, and the only symptom was
# an authorize request answering "this application is not registered" — because
# the function that resolves a client_id did not exist yet. Nothing said so,
# and `/readyz` was green throughout.
#
# Run as the OWNER: the application role cannot create anything, which is the
# entire point of the two-role split.
# MSYS_NO_PATHCONV on this one command, not on the script.
#
# Git Bash rewrites anything argument-shaped like a Unix path into a Windows
# one, so `--entrypoint /migrate` reached Docker as
# `--entrypoint "C:/Program Files/Git/migrate"` and it answered with a
# container-init failure naming a path this script never mentions. Exporting
# the variable for the whole script fixes that and breaks every `-o /dev/null`
# below, which then stops becoming NUL and fails with a write error. One
# command, one variable. Both are ignored on Linux.
MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
docker compose -f "$COMPOSE" run --rm --no-deps \
  -e AUTH_MIGRATE_DSN="postgres://auth_owner:local_dev_only@postgres:5432/auth?sslmode=disable" \
  --entrypoint /migrate authservice up >/dev/null 2>&1 \
  || die "migrations failed — run them by hand to see why"
ok "migrations are up to date"

# A signing key, if there is none.
#
# The service starts, reports healthy and answers /readyz with no key at all —
# it only fails when something asks it to sign, which is the token endpoint,
# which is the first thing any test does. The symptom is a 500 and
# `server_error`; the cause is four log lines earlier and says "no current
# signing key".
#
# This is why the documented local stack could not complete a login: there was
# no key, and nothing in `docker compose up` creates one. `keyctl` ships in the
# image as of `P1-27`, so the tool and the service agree on where the private
# half lives by construction rather than by matching mount flags.
KEYS=$(psql_ "SELECT count(*) FROM signing_keys WHERE status = 'current'")
if [ "$KEYS" != "1" ]; then
  keyctl() {
    MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
    docker compose -f "$COMPOSE" run --rm --no-deps \
      -e AUTH_MIGRATE_DSN="postgres://auth_owner:local_dev_only@postgres:5432/auth?sslmode=disable" \
      -e AUTH_SECRETS_DIR=/etc/zed-auth/secrets \
      --entrypoint /keyctl authservice "$@"
  }
  # Docker creates a named volume's mount point owned by root, and every
  # container in this stack runs unprivileged (uid 65532, the distroless
  # `nonroot` user). So `keyctl generate` fails with "permission denied"
  # writing a file into a directory it cannot write to — the same trap
  # deploy/vm/docker-compose.yml documents for the WAL archive, where a silent
  # version of it meant point-in-time recovery was configured and not working.
  #
  # Fixed with a throwaway container that HAS a shell, because the runtime
  # image deliberately does not.
  MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' \
  docker run --rm -v zed-auth_auth_secrets:/s alpine:3.21 \
    chown -R 65532:65532 /s >/dev/null 2>&1 \
    || die "could not take ownership of the signing-key volume"

  keyctl generate >/dev/null 2>&1 || die "keyctl generate failed"
  keyctl rotate   >/dev/null 2>&1 || die "keyctl rotate failed"

  # Restart: the key cache is read at startup, and a service that came up with
  # no key does not discover one on its own.
  docker compose -f "$COMPOSE" restart authservice >/dev/null 2>&1
  for _ in $(seq 1 30); do
    [ "$(curl -sS -o /dev/null -w '%{http_code}' "$ISSUER/readyz" 2>/dev/null)" = "200" ] && break
    sleep 1
  done
fi

# Asserted rather than assumed. A key set with no keys in it is exactly what
# this stack served for weeks while every probe stayed green.
curl -sS "$ISSUER/.well-known/jwks.json" | grep -q '"kid"' \
  || die "the key set is empty — the service cannot sign anything"
ok "a signing key is current and published"

# Restart afterwards. The service came up against the old schema, and a
# connection pool that has already prepared statements against it is a class of
# confusion that costs far more than the two seconds this takes.
docker compose -f "$COMPOSE" restart authservice >/dev/null 2>&1
for _ in $(seq 1 30); do
  [ "$(curl -sS -o /dev/null -w '%{http_code}' "$ISSUER/readyz" 2>/dev/null)" = "200" ] && break
  sleep 1
done
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ISSUER/readyz")" = "200" ] \
  || die "the service did not come back after the migration"

curl -sS "$MAILPIT/api/v1/info" >/dev/null 2>&1 \
  && ok "Mailpit is answering at $MAILPIT" \
  || die "Mailpit is not answering at $MAILPIT — invitations cannot be read"

# --- the bootstrap, and only the bootstrap -----------------------------------

say "Seeding the bootstrap (PG-26: no API can do this yet)"

# The instance comes first, and a fresh database has none.
#
# `organizations.instance_id` is NOT NULL with no default, and nothing creates
# the first instance — not a migration, not an endpoint, not a startup path.
# Found by running this script against an empty database, which is exactly the
# case `PG-26` says nobody had tried.
INSTANCE_ID=$(psql_ "SELECT id FROM instances ORDER BY created_at LIMIT 1")
if [ -z "$INSTANCE_ID" ]; then
  INSTANCE_ID=$(psql_ "INSERT INTO instances (name) VALUES ('e2e') RETURNING id")
fi
[ -n "$INSTANCE_ID" ] || die "could not create the instance"
ok "instance $INSTANCE_ID"

ORG_ID=$(psql_ "SELECT id FROM organizations WHERE name = 'e2e' LIMIT 1")
if [ -z "$ORG_ID" ]; then
  ORG_ID=$(psql_ "INSERT INTO organizations (instance_id, name) VALUES ('$INSTANCE_ID', 'e2e') RETURNING id")
fi
[ -n "$ORG_ID" ] || die "could not create the organization"
ok "organization $ORG_ID"

ADMIN_ID=$(psql_ "SELECT id FROM users WHERE email = '$ADMIN_EMAIL' LIMIT 1")
if [ -z "$ADMIN_ID" ]; then
  HASH=$(cd backend && PASSWORD="$ADMIN_PASSWORD" go run ./cmd/passwordhash)
  [ -n "$HASH" ] || die "could not hash the administrator's password"
  ADMIN_ID=$(psql_ "INSERT INTO users (org_id, email, password_hash, status, display_name)
                    VALUES ('$ORG_ID', '$ADMIN_EMAIL', '$HASH', 'active', 'E2E Admin') RETURNING id")
  # ORG_ADMIN, not INSTANCE_OWNER. A suite running with the most powerful role
  # in the system is a suite that cannot notice a missing permission check —
  # and login.spec.ts asserts a role-gated route refuses.
  psql_ "INSERT INTO manager_roles (user_id, role, scope_id) VALUES ('$ADMIN_ID', 'ORG_ADMIN', '$ORG_ID')" >/dev/null
fi
ok "administrator $ADMIN_ID"

PROJECT_ID=$(psql_ "SELECT id FROM projects WHERE org_id = '$ORG_ID' AND name = 'e2e' LIMIT 1")
if [ -z "$PROJECT_ID" ]; then
  PROJECT_ID=$(psql_ "INSERT INTO projects (org_id, name) VALUES ('$ORG_ID', 'e2e') RETURNING id")
fi

# The console's own client. Seeded rather than created per test, because the
# console BUNDLE is built with its client id baked in — a test-time override
# would mean a test-only code path in the one file that decides where
# authorization codes are sent (`P1-21` removed exactly that).
CONSOLE_CLIENT=$(psql_ "SELECT id FROM applications WHERE project_id = '$PROJECT_ID' AND name = 'e2e-console' LIMIT 1")
if [ -z "$CONSOLE_CLIENT" ]; then
  # Two post-logout URIs, not one. `window.location.origin` carries no
  # trailing slash, and these are matched by exact string comparison like every
  # other URI here — registering only the slashed form produces a sign-out that
  # refuses with "an address it has not registered".
  #
  # `allowed_origins` is the console's own origin (P1-29). Without it every
  # Management API call fails in the browser with "CORS" and nothing else —
  # which is the state this whole stack was in until P1-27 pointed a browser
  # at it.
  CONSOLE_CLIENT=$(psql_ "INSERT INTO applications (project_id, org_id, name, type, redirect_uris, post_logout_redirect_uris, grant_types, allowed_origins)
    VALUES ('$PROJECT_ID', '$ORG_ID', 'e2e-console', 'spa',
            ARRAY['$CONSOLE_URL/auth/callback', '$CONSOLE_URL/auth/silent'],
            ARRAY['$CONSOLE_URL', '$CONSOLE_URL/'],
            ARRAY['authorization_code','refresh_token'],
            ARRAY['$CONSOLE_URL']) RETURNING id")
else
  # An existing row from before P1-29 has no origins. Kept idempotent rather
  # than requiring a wipe, because "it worked yesterday" is exactly when this
  # script gets run again.
  psql_ "UPDATE applications SET allowed_origins = ARRAY['$CONSOLE_URL']
          WHERE id = '$CONSOLE_CLIENT' AND NOT ('$CONSOLE_URL' = ANY (allowed_origins))" >/dev/null
fi
ok "console client $CONSOLE_CLIENT"

# --- a management token, through the real login ------------------------------

say "Signing in as the administrator, through the hosted page"

# `offline_access` is requested below, so the exchange also yields a refresh
# token.
#
# An access token lives ten minutes (`P1-07`) and a Playwright run does not
# reliably finish inside ten minutes. Handing the suite a token minted here
# would produce one that passes locally and fails in CI at whichever test
# happened to be running when the clock ran out — the shape of flake that gets
# a test quarantined instead of fixed.
VERIFIER=$(head -c 40 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')
CHALLENGE=$(printf '%s' "$VERIFIER" | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '=')
REDIRECT="$CONSOLE_URL/auth/callback"
ENC_REDIRECT=$(printf '%s' "$REDIRECT" | sed 's|:|%3A|g; s|/|%2F|g')
JAR=$(mktemp)

LOGIN_URL=$(curl -sS -c "$JAR" -o /dev/null -w '%{redirect_url}' \
  "$ISSUER/oauth/authorize?response_type=code&client_id=$CONSOLE_CLIENT&redirect_uri=$ENC_REDIRECT&scope=openid%20offline_access&state=seed&code_challenge=$CHALLENGE&code_challenge_method=S256")
case "$LOGIN_URL" in
  *"/login?request="*) ;;
  *) die "authorize did not reach the login page: ${LOGIN_URL:-nothing}" ;;
esac

FORM=$(curl -sS -b "$JAR" -c "$JAR" "$LOGIN_URL")
CSRF=$(printf '%s' "$FORM" | grep -o 'name="csrf_token" value="[^"]*"' | sed 's/.*value="//; s/"//')
REQ=$(printf '%s' "$FORM" | grep -o 'name="request" value="[^"]*"' | sed 's/.*value="//; s/"//')
CB=$(curl -sS -b "$JAR" -c "$JAR" -o /dev/null -w '%{redirect_url}' \
      --data-urlencode "csrf_token=$CSRF" --data-urlencode "request=$REQ" \
      --data-urlencode "email=$ADMIN_EMAIL" --data-urlencode "password=$ADMIN_PASSWORD" "$ISSUER/login")
CODE=$(printf '%s' "$CB" | sed -n 's/.*[?&]code=\([^&]*\).*/\1/p')
rm -f "$JAR"
[ -n "$CODE" ] || die "no authorization code came back: ${CB:-nothing}"

TOKENS=$(curl -sS -X POST "$ISSUER/oauth/token" \
  -d grant_type=authorization_code -d "code=$CODE" -d "client_id=$CONSOLE_CLIENT" \
  --data-urlencode "redirect_uri=$REDIRECT" -d "code_verifier=$VERIFIER")
TOKEN=$(printf '%s' "$TOKENS" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
REFRESH=$(printf '%s' "$TOKENS" | sed -n 's/.*"refresh_token":"\([^"]*\)".*/\1/p')
[ -n "$TOKEN" ] || die "no access token came back"
[ -n "$REFRESH" ] || die "no refresh token came back — was offline_access granted?"
ok "a management token was obtained through the same flow a user uses"

# --- the two demo applications, through the API ------------------------------

say "Registering the demo applications"

api() { curl -sS -X "$1" "$ISSUER$2" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${3:+-d "$3"}; }
APPS="/v1/organizations/$ORG_ID/projects/$PROJECT_ID/applications"

id_of() { python3 -c "
import json,sys
name = sys.argv[1]
for a in json.load(sys.stdin).get('applications', []):
    if a['name'] == name:
        print(a['id']); break
" "$1"; }

EXISTING=$(api GET "$APPS?page_size=100")
DEMO_A_ID=$(printf '%s' "$EXISTING" | id_of "e2e-demo-a")
DEMO_B_ID=$(printf '%s' "$EXISTING" | id_of "e2e-demo-b")

if [ -z "$DEMO_A_ID" ]; then
  CREATED=$(api POST "$APPS" "{\"name\":\"e2e-demo-a\",\"type\":\"web\",
    \"redirect_uris\":[\"$DEMO_A_URL/callback\"],
    \"post_logout_redirect_uris\":[\"$DEMO_A_URL/\"],
    \"grant_types\":[\"authorization_code\",\"refresh_token\"]}")
  DEMO_A_ID=$(printf '%s' "$CREATED" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  DEMO_A_SECRET=$(printf '%s' "$CREATED" | sed -n 's/.*"client_secret":"\([^"]*\)".*/\1/p')
else
  # The secret is returned exactly once, at creation. On a re-run there is no
  # way to recover it, so rotate: an overlap window means nothing else breaks.
  DEMO_A_SECRET=$(api POST "$APPS/$DEMO_A_ID/rotate-secret" |
    sed -n 's/.*"client_secret":"\([^"]*\)".*/\1/p')
fi
[ -n "$DEMO_A_ID" ] && [ -n "$DEMO_A_SECRET" ] || die "demo A was not registered"

if [ -z "$DEMO_B_ID" ]; then
  # `allowed_origins` is demo B's own origin. Its browser half exchanges the
  # authorization code at /oauth/token — which is in the public CORS set and
  # needs no registration — and then calls its OWN /api/me, which is
  # same-origin. The registration is here anyway because a consumer team
  # copying this SPA will call a resource server eventually, and a demo that
  # omits the field teaches that it is optional.
  DEMO_B_ID=$(api POST "$APPS" "{\"name\":\"e2e-demo-b\",\"type\":\"spa\",
    \"redirect_uris\":[\"$DEMO_B_URL/\"],
    \"post_logout_redirect_uris\":[\"$DEMO_B_URL/\"],
    \"allowed_origins\":[\"$DEMO_B_URL\"],
    \"grant_types\":[\"authorization_code\"]}" |
    sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
fi
[ -n "$DEMO_B_ID" ] || die "demo B was not registered"
ok "demo A (confidential) $DEMO_A_ID"
ok "demo B (public)       $DEMO_B_ID"

# --- run the demos on the host ----------------------------------------------

say "Starting the demo applications"

# On the host rather than in containers, deliberately. The issuer they are
# given has to be the one the BROWSER uses, and a container that resolved
# `localhost:8080` would reach itself. Two processes on the host avoid an
# entire class of container-networking confusion in a suite whose failures
# should all be about authentication.
#
# **Built first, then run — not `go run`.**
#
# `go run` compiles to a temporary binary and execs it as a CHILD, so the pid
# this script can record is the wrapper's. Killing that leaves the server
# holding its port, and the next run starts a demo that cannot bind, exits,
# and leaves the PREVIOUS one answering with a client_id that no longer
# exists. The symptom is an authorize request refusing an application that is
# right there in the database.
#
# `pkill -P` was the first fix and does not work here: on Windows the MSYS
# process tree does not reach the compiled binary. Building removes the
# wrapper instead of trying to see through it.
mkdir -p "$ROOT/.e2e-bin"
(cd demo && go build -o "$ROOT/.e2e-bin/webapp" ./webapp && go build -o "$ROOT/.e2e-bin/spa" ./spa) \
  || die "the demo applications did not build"

# Stop the demos this script started last time, if any.
#
# The script promises to be idempotent, and it was not: the guard below refused
# to start while the PREVIOUS run's demos were still up, so a second run —
# which is exactly what you do after changing the console — died before
# rebuilding anything and left the old bundle in place. Stopping our own first
# is what makes the promise true.
if [ -f "$PID_FILE" ]; then
  while read -r pid; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null
  done < "$PID_FILE"
  sleep 1
fi

# Anything STILL listening is not ours, and starting on top of it produces a
# stack that looks right and serves stale configuration.
for port in 8090 8091; do
  if curl -sS -o /dev/null --max-time 2 "http://localhost:$port/healthz" 2>/dev/null; then
    die "something not started by this script is listening on $port. Run: bash scripts/e2e-down.sh"
  fi
done

: > "$PID_FILE"

DEMO_ISSUER="$ISSUER" DEMO_CLIENT_ID="$DEMO_A_ID" DEMO_CLIENT_SECRET="$DEMO_A_SECRET" \
  DEMO_BASE_URL="$DEMO_A_URL" DEMO_ADDR=":8090" \
  "$ROOT/.e2e-bin/webapp" >"$ROOT/.e2e-demo-a.log" 2>&1 &
echo $! >> "$PID_FILE"

DEMO_ISSUER="$ISSUER" DEMO_CLIENT_ID="$DEMO_B_ID" \
  DEMO_BASE_URL="$DEMO_B_URL" DEMO_ADDR=":8091" \
  "$ROOT/.e2e-bin/spa" >"$ROOT/.e2e-demo-b.log" 2>&1 &
echo $! >> "$PID_FILE"

for url in "$DEMO_A_URL" "$DEMO_B_URL"; do
  for _ in $(seq 1 45); do
    [ "$(curl -sS -o /dev/null -w '%{http_code}' "$url/healthz" 2>/dev/null)" = "200" ] && break
    sleep 1
  done
  [ "$(curl -sS -o /dev/null -w '%{http_code}' "$url/healthz" 2>/dev/null)" = "200" ] \
    || die "$url never became healthy — see .e2e-demo-a.log and .e2e-demo-b.log"
  ok "$url is healthy"
done

# --- build the console against this stack ------------------------------------

say "Building the console for this stack"

(
  cd console
  VITE_AUTH_ISSUER="$ISSUER" VITE_AUTH_CLIENT_ID="$CONSOLE_CLIENT" VITE_API_BASE_URL="$ISSUER" \
    npm run build >"$ROOT/.e2e-console-build.log" 2>&1
) || die "the console build failed — see .e2e-console-build.log"
ok "console built with client $CONSOLE_CLIENT"

# --- the environment ---------------------------------------------------------

# An UNQUOTED heredoc, because it has to expand $ISSUER and the rest -- which
# means backticks inside it are command substitution, comment or not. The ones
# in the prose below are escaped for that reason; the names in those comments
# were being run as commands until P2-12 saw the errors in the output.
cat > "$ENV_FILE" <<EOF
# Written by scripts/e2e-up.sh. Source it, then run the suite.
#
# Everything here points at a local stack. There is no secret in this file that
# is not already a local-development placeholder.
export E2E_AUTH_ISSUER="$ISSUER"
export E2E_ORG_ID="$ORG_ID"
export E2E_CONSOLE_CLIENT_ID="$CONSOLE_CLIENT"
export E2E_BOOTSTRAP_REFRESH_TOKEN="$REFRESH"
export E2E_MAILPIT_URL="$MAILPIT"
export E2E_BASE_URL="$CONSOLE_URL"
# The bootstrap administrator, for the few tests that need a manager role.
#
# There is no API that assigns one — \`manager_roles\` is \`P2\`'s — so this
# account is seeded by SQL like the rest of the bootstrap (\`PG-26\`), and the
# suite is handed its credentials rather than a way to mint more.
export E2E_ADMIN_EMAIL="$ADMIN_EMAIL"
export E2E_ADMIN_PASSWORD="$ADMIN_PASSWORD"
export E2E_DEMO_A_URL="$DEMO_A_URL"
export E2E_DEMO_B_URL="$DEMO_B_URL"
EOF

say "Ready"
cat <<EOF
  source .e2e.env
  cd console && npm run e2e

  The suite mints its own access tokens from the refresh token in .e2e.env, so
  a long run does not run out of credential halfway through. Everything this
  script seeds is idempotent: a second run costs a few seconds and changes
  nothing but demo A's secret, which it rotates because a client secret is
  returned exactly once and cannot be read back.

  Stop everything with: bash scripts/e2e-down.sh
EOF
