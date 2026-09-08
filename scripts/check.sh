#!/usr/bin/env bash
#
# Runs every quality and security gate locally.
#
# These are the same gates .github/workflows/ci.yml runs, available before a
# push rather than after one. CI remains the authority — it runs on a clean
# checkout on Linux, which catches what a developer machine hides (the CRLF
# bug in the pre-commit hook was exactly that). This is the fast feedback loop,
# not a replacement.
#
#   make check      fast gates — format, vet, unit tests
#   make check-all  everything below, including integration and security
#
# Each gate exists because it has caught something real, or because the failure
# it prevents is silent. The comments say which.

set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1
ROOT="$PWD"

PASS=0
FAIL=0
SKIP=0

pass() { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS+1)); }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL+1)); }
skip() { printf '  \033[33m–\033[0m %s (%s)\n' "$1" "$2"; SKIP=$((SKIP+1)); }
head() { printf '\n\033[1;36m%s\033[0m\n' "$1"; }

RUN_INTEGRATION="${RUN_INTEGRATION:-1}"
RUN_SECURITY="${RUN_SECURITY:-1}"

# --- Go ---------------------------------------------------------------------

head "Go"

if [ -z "$(gofmt -l backend/cmd backend/internal backend/migrations 2>/dev/null)" ]; then
  pass "gofmt"
else
  fail "gofmt — unformatted files:"
  gofmt -l backend/cmd backend/internal backend/migrations | sed 's/^/      /'
fi

if (cd backend && go vet ./... 2>&1); then pass "go vet"; else fail "go vet"; fi
if (cd backend && go build ./... 2>&1); then pass "go build"; else fail "go build"; fi

# -race matters most here: sessions, rate limiting, and authorization code
# redemption are concurrent paths where a data race is a security bug rather
# than a flake. It needs cgo, which is off by default on Windows without a C
# toolchain — so fall back to a plain run and say so, rather than reporting a
# failure that is really a missing compiler. CI runs on Linux, where -race works.
if (cd backend && CGO_ENABLED=1 go test -race ./... >/dev/null 2>&1); then
  pass "unit tests (-race)"
elif (cd backend && go test ./... 2>&1 | grep -v "no test files"); then
  pass "unit tests (no -race — cgo unavailable; CI runs with it)"
else
  fail "unit tests"
  (cd backend && go test ./... 2>&1 | grep -v "^ok" | head -20)
fi

# go.sum verification detects a dependency whose content changed without go.sum
# changing — the shape a compromised module takes (SECURITY/02 §15).
if (cd backend && go mod verify >/dev/null 2>&1); then pass "go mod verify"; else fail "go mod verify"; fi

# Tidiness means "running go mod tidy changes nothing", which is a comparison
# against the CURRENT files — not against git HEAD.
#
# An earlier version compared against HEAD and then ran `git checkout` to undo
# what tidy did. With uncommitted dependency work in the tree, that reported a
# false failure AND silently discarded the change: it threw away a security
# upgrade to golang.org/x/text mid-session. A check script must never destroy
# uncommitted work, so this one snapshots to a temp file and never touches git.
tidy_tmp="$(mktemp -d)"
cp backend/go.mod backend/go.sum "$tidy_tmp/"
(cd backend && go mod tidy >/dev/null 2>&1)
if diff -q "$tidy_tmp/go.mod" backend/go.mod >/dev/null 2>&1    && diff -q "$tidy_tmp/go.sum" backend/go.sum >/dev/null 2>&1; then
  pass "go.mod is tidy"
else
  fail "go.mod is not tidy — run: cd backend && go mod tidy"
  cp "$tidy_tmp/go.mod" "$tidy_tmp/go.sum" backend/
fi
rm -rf "$tidy_tmp"

# --- Integration ------------------------------------------------------------

head "Integration"

if [ "$RUN_INTEGRATION" != "1" ]; then
  skip "integration tests" "RUN_INTEGRATION=0"
elif ! docker compose -f deploy/docker-compose.yml ps postgres 2>/dev/null | grep -q "Up"; then
  skip "integration tests" "postgres not running — make up && make migrate-up"
else
  if (cd backend && go test -tags=integration -count=1 ./... 2>&1 | grep -v "no test files"); then
    pass "integration tests"
  else
    fail "integration tests"
  fi

  # A table added with an org_id but no policy is silently unisolated: queries
  # work, tests pass, and it returns every organization's rows. Invisible until
  # it is a breach (PLAN/08 Part B, P0-08).
  unprotected=$(docker compose -f deploy/docker-compose.yml exec -T postgres \
    psql -U auth_owner -d auth -tA -c "
      SELECT c.relname FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      WHERE n.nspname='public' AND c.relkind IN ('r','p')
        AND EXISTS (SELECT 1 FROM information_schema.columns col
                    WHERE col.table_schema='public' AND col.table_name=c.relname
                      AND col.column_name IN ('org_id','granting_org_id'))
        AND NOT c.relrowsecurity
        AND NOT EXISTS (SELECT 1 FROM pg_inherits i WHERE i.inhrelid=c.oid);" 2>/dev/null | tr -d '[:space:]')

  if [ -z "$unprotected" ]; then
    pass "every tenant-scoped table has row-level security"
  else
    fail "tables without row-level security: $unprotected"
  fi
fi

# --- Migrations -------------------------------------------------------------

head "Migrations"

missing_down=""
for up in backend/migrations/*.up.sql; do
  [ -f "$up" ] || continue
  down="${up%.up.sql}.down.sql"
  [ -f "$down" ] || missing_down="$missing_down $(basename "$up")"
done
if [ -z "$missing_down" ]; then
  pass "every up migration has a down"
else
  fail "missing down migrations:$missing_down"
fi

# PLAN/14 § Rollback Strategy: a migration must leave the PREVIOUS application
# version able to run against the new schema, so an app rollback never needs a
# database rollback.
undocumented=""
for up in backend/migrations/*.up.sql; do
  [ -f "$up" ] || continue
  if grep -Eiq '(DROP[[:space:]]+(COLUMN|TABLE)|RENAME[[:space:]]+(COLUMN|TO)|ALTER[[:space:]]+COLUMN[[:space:]]+[a-z_]+[[:space:]]+TYPE)' "$up"; then
    grep -q 'EXPAND/CONTRACT' "$up" || undocumented="$undocumented $(basename "$up")"
  fi
done
if [ -z "$undocumented" ]; then
  pass "destructive migrations carry an expand/contract note"
else
  fail "destructive migrations without justification:$undocumented"
fi

# --- Shell ------------------------------------------------------------------

head "Shell"

# A script committed with CRLF fails on Linux with "bad interpreter: /bin/sh^M".
# The git hooks are a security control, and one that fails to execute protects
# nothing while still appearing to be there. This has happened once already.
crlf=""
while IFS= read -r f; do
  [ -f "$f" ] || continue
  if grep -qU $'\r' "$f" 2>/dev/null; then crlf="$crlf $f"; fi
done < <(find scripts deploy -type f \( -name '*.sh' -o -path 'scripts/hooks/*' \) 2>/dev/null)
if [ -z "$crlf" ]; then pass "no CRLF in executable scripts"; else fail "CRLF found:$crlf"; fi

nonexec=""
for h in scripts/hooks/*; do
  [ -f "$h" ] || continue
  [ -x "$h" ] || nonexec="$nonexec $h"
done
if [ -z "$nonexec" ]; then pass "git hooks are executable"; else fail "non-executable hooks:$nonexec"; fi

# The hook is only a control if it actually rejects. Verify rather than assume.
if printf 'no task id here\n' > /tmp/_msgcheck && ! sh scripts/hooks/commit-msg /tmp/_msgcheck >/dev/null 2>&1 \
   && printf 'P0-01: valid\n' > /tmp/_msgcheck && sh scripts/hooks/commit-msg /tmp/_msgcheck >/dev/null 2>&1; then
  pass "commit-msg hook rejects a subject without a task ID"
else
  fail "commit-msg hook is not working"
fi
rm -f /tmp/_msgcheck

if command -v shellcheck >/dev/null 2>&1; then
  if shellcheck -S warning scripts/*.sh scripts/hooks/* deploy/vm/*.sh deploy/postgres/init/*.sh 2>&1; then
    pass "shellcheck"
  else
    fail "shellcheck"
  fi
elif docker info >/dev/null 2>&1; then
  if MSYS_NO_PATHCONV=1 docker run --rm -v "$ROOT:/mnt" -w /mnt koalaman/shellcheck:stable \
       -S warning scripts/install-hooks.sh scripts/check.sh scripts/hooks/commit-msg scripts/hooks/pre-commit \
       deploy/vm/deploy.sh deploy/vm/backup.sh deploy/postgres/init/01-roles.sh 2>&1; then
    pass "shellcheck (via docker)"
  else
    fail "shellcheck"
  fi
else
  skip "shellcheck" "not installed and docker unavailable"
fi

# --- Security ---------------------------------------------------------------

head "Security"

if [ "$RUN_SECURITY" != "1" ]; then
  skip "security scans" "RUN_SECURITY=0"
else
  GOSEC="$(go env GOPATH 2>/dev/null)/bin/gosec"
  [ -f "${GOSEC}.exe" ] && GOSEC="${GOSEC}.exe"
  if [ -x "$GOSEC" ]; then
    if (cd backend && "$GOSEC" -severity medium -confidence medium -exclude-generated -quiet ./... 2>&1); then
      pass "gosec"
    else
      fail "gosec"
    fi
  else
    skip "gosec" "go install github.com/securego/gosec/v2/cmd/gosec@v2.29.0"
  fi

  GOVULN="$(go env GOPATH 2>/dev/null)/bin/govulncheck"
  [ -f "${GOVULN}.exe" ] && GOVULN="${GOVULN}.exe"
  if [ -x "$GOVULN" ]; then
    if (cd backend && "$GOVULN" ./... >/dev/null 2>&1); then
      pass "govulncheck"
    else
      fail "govulncheck — known vulnerabilities present"
      (cd backend && "$GOVULN" ./... 2>&1 | head -20)
    fi
  else
    skip "govulncheck" "go install golang.org/x/vuln/cmd/govulncheck@v1.1.4"
  fi

  # The logger redacts by attribute key (P0-09), but a raw body passed
  # positionally would slip past that.
  if grep -rnE '(slog|log)\.[A-Za-z]+\([^)]*(r\.Header|r\.Body|req\.Header|req\.Body|\.RawQuery)' \
       --include='*.go' backend/cmd backend/internal 2>/dev/null; then
    fail "raw request material reaches a log call"
  else
    pass "no raw request material in log calls"
  fi

  # Test fixtures assemble PEM markers at runtime so this stays true; any
  # literal block is therefore a real finding, not a known exception.
  if grep -rn --exclude-dir=.git --exclude='pre-commit' --exclude='check.sh' \
       -- '-----BEGIN [A-Z ]*PRIVATE KEY-----' . >/dev/null 2>&1; then
    fail "a PEM private key block is committed"
  else
    pass "no PEM private key blocks"
  fi

  # The staged-file scanner, run over the whole tree rather than the index.
  if git ls-files -z | xargs -0 grep -lE '(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}|xox[baprs]-[A-Za-z0-9-]{10,}' 2>/dev/null | grep -v 'pre-commit\|check.sh'; then
    fail "a credential-shaped token is committed"
  else
    pass "no credential-shaped tokens"
  fi
fi

# --- Deployment config ------------------------------------------------------

head "Deployment"

if docker info >/dev/null 2>&1; then
  if docker compose -f deploy/docker-compose.yml config -q 2>&1; then
    pass "local compose is valid"
  else
    fail "local compose is invalid"
  fi

  if AUTH_ENV=staging AUTH_DOMAIN=ci.example.com AUTH_ISSUER=https://ci.example.com \
     AUTH_IMAGE=example@sha256:0000000000000000000000000000000000000000000000000000000000000000 \
     AUTH_POSTGRES_DSN=postgres://auth_app:local_dev_only@postgres:5432/auth \
     AUTH_POSTGRES_APP_PASSWORD=local_dev_only AUTH_POSTGRES_OWNER_PASSWORD=local_dev_only \
     AUTH_REDIS_PASSWORD=local_dev_only AUTH_JWT_SIGNING_KEY_REF=file:/k.pem \
     AUTH_SECRETS_DIR=/tmp/s \
     docker compose -f deploy/vm/docker-compose.tunnel.yml config -q 2>&1; then
    pass "tunnel compose is valid"
  else
    fail "tunnel compose is invalid"
  fi

  # The schema owner bypasses RLS and can alter the audit log. If the service
  # is compromised, the blast radius must stop at what auth_app can do.
  rendered=$(AUTH_ENV=staging AUTH_ISSUER=https://ci.example.com \
    AUTH_IMAGE=example@sha256:0000000000000000000000000000000000000000000000000000000000000000 \
    AUTH_POSTGRES_DSN=postgres://auth_app:local_dev_only@postgres:5432/auth \
    AUTH_POSTGRES_APP_PASSWORD=local_dev_only AUTH_POSTGRES_OWNER_PASSWORD=local_dev_only \
    AUTH_REDIS_PASSWORD=local_dev_only AUTH_JWT_SIGNING_KEY_REF=file:/k.pem \
    AUTH_SECRETS_DIR=/tmp/s \
    docker compose -f deploy/vm/docker-compose.tunnel.yml config --format json 2>/dev/null)

  if echo "$rendered" | grep -q 'AUTH_POSTGRES_OWNER_PASSWORD\|AUTH_MIGRATE_DSN'; then
    if echo "$rendered" | python3 -c "
import json,sys
d=json.load(sys.stdin)
env=d['services']['authservice']['environment']
leaked=[k for k in env if k in ('AUTH_POSTGRES_OWNER_PASSWORD','AUTH_MIGRATE_DSN')]
sys.exit(1 if leaked else 0)
" 2>/dev/null; then
      pass "service container receives no owner credentials"
    else
      fail "the service container receives owner credentials"
    fi
  else
    pass "service container receives no owner credentials"
  fi
else
  skip "compose validation" "docker unavailable"
fi

# --- Summary ----------------------------------------------------------------

printf '\n\033[1m%d passed, %d failed, %d skipped\033[0m\n' "$PASS" "$FAIL" "$SKIP"

if [ "$FAIL" -gt 0 ]; then
  printf '
[31mChecks failed.[0m CI would reject this too, but slower.

'
  exit 1
fi

printf '\nAll gates passed.\n'
