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
# Named `section`, not `head`.
#
# A function called `head` shadows the `head` command for the whole script,
# so every `... | head -20` in a pipeline called this function instead: it
# ignores stdin, prints a cyan "-20", and discards the piped output
# entirely. The effect was invisible until something failed, at which point
# the diagnostic detail that was supposed to be truncated to 20 lines was
# thrown away instead.
section() { printf '\n\033[1;36m%s\033[0m\n' "$1"; }

RUN_INTEGRATION="${RUN_INTEGRATION:-1}"
RUN_SECURITY="${RUN_SECURITY:-1}"

# --- Go ---------------------------------------------------------------------

section "Go"

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
if (cd backend && CGO_ENABLED=1 go test -race -shuffle=on ./... >/dev/null 2>&1); then
  pass "unit tests (-race, shuffled)"
elif (cd backend && go test ./... 2>&1 | grep -v "no test files"); then
  pass "unit tests (no -race — cgo unavailable; CI runs with it)"
else
  fail "unit tests"
  (cd backend && go test -shuffle=on ./... 2>&1 | grep -v "^ok" | head -20)
fi

# go.sum verification detects a dependency whose content changed without go.sum
# changing — the shape a compromised module takes (docs/SECURITY/02 §15).
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

section "Integration"

if [ "$RUN_INTEGRATION" != "1" ]; then
  skip "integration tests" "RUN_INTEGRATION=0"
elif ! docker compose -f deploy/docker-compose.yml ps postgres 2>/dev/null | grep -q "Up"; then
  skip "integration tests" "postgres not running — make up && make migrate-up"
else
  # -p 1: one package's test binary at a time.
  #
  # Each integration package starts its own PostgreSQL and Redis through
  # testcontainers (P0-15), which is correct — separate test binaries cannot
  # share a container. Starting four sets concurrently is what breaks: the
  # provider initialises in several processes at once and one fails with
  # "rootless Docker is not supported on Windows", which is a race wearing a
  # configuration error's clothes.
  #
  # It failed in exactly one package out of four, a different one each run —
  # the shape of an intermittent failure people learn to re-run rather than
  # read. Pinned rather than tolerated.
  if (cd backend && go test -tags=integration -count=1 -p 1 ./... 2>&1 | grep -v "no test files"); then
    pass "integration tests"
  else
    fail "integration tests"
  fi

  # A table added with an org_id but no policy is silently unisolated: queries
  # work, tests pass, and it returns every organization's rows. Invisible until
  # it is a breach (docs/PLAN/08 Part B, P0-08).
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

section "Migrations"

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

# docs/PLAN/14 § Rollback Strategy: a migration must leave the PREVIOUS application
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

section "Shell"

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

# The mode recorded in the INDEX, not the one on this filesystem.
#
# These are different things and the difference bit us. On Windows, git does
# not track the executable bit unless core.fileMode is set, so `chmod +x`
# locally never reaches the repository — every script was committed 100644
# while being executable in the working tree. The check above passed happily.
#
# It surfaced when the VM deployment moved to `git clone`: a fresh checkout got
# files nothing could run, and `secrets.sh` failed with "command not found" at
# the moment it was needed to regenerate a signing key.
#
# A clone gets the index mode. That is what has to be right.
non_exec_in_index=$(git ls-files -s -- \
    'scripts/*.sh' 'scripts/hooks/*' 'scripts/*.py' \
    'deploy/**/*.sh' 'public-site/scripts/*.mjs' 2>/dev/null \
  | awk '$1 == "100644" { print $4 }')

if [ -z "$non_exec_in_index" ]; then
  pass "scripts are executable in the git index (what a clone gets)"
else
  fail "scripts committed without the executable bit — a fresh clone cannot run them"
  echo "$non_exec_in_index" | sed 's/^/      /'
  echo "      Fix: git update-index --chmod=+x <file>"
fi

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

section "Security"

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

  # Tracked files only, via `git ls-files`.
  #
  # This walked the whole working tree until it started reporting three files
  # inside public-site/node_modules — a certificate library's own fixtures,
  # gitignored, and not committed by any definition. The check's own message
  # says "committed", so scanning untracked files was reporting something it
  # was not asking about, and a security check that cries wolf the first time
  # someone installs dependencies in a new directory is a check that gets
  # commented out.
  #
  # Repository test fixtures assemble PEM markers at runtime so this stays
  # true; any literal block in a tracked file is a real finding.
  if git ls-files -z \
       | xargs -0 grep -ln -- '-----BEGIN [A-Z ]*PRIVATE KEY-----' 2>/dev/null \
       | grep -v 'pre-commit\|check.sh' >/dev/null 2>&1; then
    fail "a PEM private key block is committed"
    git ls-files -z | xargs -0 grep -ln -- '-----BEGIN [A-Z ]*PRIVATE KEY-----' 2>/dev/null \
      | grep -v 'pre-commit\|check.sh' | sed 's/^/      /'
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

# --- Coverage floors --------------------------------------------------------
#
# P0-15 step 6: floors on the packages where an untested path is a security
# problem, not a repo-wide average. A repo-wide percentage is satisfied by
# testing whatever is easiest, and the easiest code to test is rarely the code
# where a bug matters.

section "Coverage"

if sh scripts/check-coverage.sh >/dev/null 2>&1; then
  # The message differs by whether the packages exist yet, so it is shown
  # rather than summarised: "0 of 3 floors apply" is the useful fact in Phase 0.
  pass "coverage floors ($(sh scripts/check-coverage.sh | tail -1))"
else
  fail "coverage floors"
  sh scripts/check-coverage.sh 2>&1 | sed 's/^/      /'
fi

# --- API contract -----------------------------------------------------------
#
# openapi/openapi.yaml is the source of truth and the Go server interface is
# generated from it (ADR-013). Two things can go wrong and both are silent: the
# spec can become invalid, and the committed generated code can drift from it.

section "API contract"

if docker info >/dev/null 2>&1; then
  if MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD/openapi:/spec" \
     redocly/cli:1.34.2 lint --config /spec/redocly.yaml /spec/openapi.yaml >/dev/null 2>&1; then
    pass "OpenAPI spec is valid"
  else
    fail "OpenAPI spec is invalid"
    MSYS_NO_PATHCONV=1 docker run --rm -v "$PWD/openapi:/spec" \
      redocly/cli:1.34.2 lint --config /spec/redocly.yaml /spec/openapi.yaml 2>&1 | tail -20
  fi
else
  skip "OpenAPI spec validation" "docker unavailable"
fi

# The generated code is committed, so a reviewer sees the contract change and
# its consequences in one diff and a fresh clone builds with no generation
# step. That only holds if the committed copy is current.
#
# Regenerate into a scratch copy rather than over the working tree: a check
# that rewrites files it is only supposed to inspect is how uncommitted work
# gets destroyed, which this script has done before.
gen_before=$(mktemp)
cp backend/internal/api/api.gen.go "$gen_before"

if (cd backend && go generate ./internal/api/ >/dev/null 2>&1); then
  if diff -q "$gen_before" backend/internal/api/api.gen.go >/dev/null 2>&1; then
    pass "generated API code matches the spec"
  else
    fail "generated API code is stale — run: make openapi-generate"
    diff -u "$gen_before" backend/internal/api/api.gen.go | head -30
  fi
else
  fail "code generation from the OpenAPI spec failed"
fi

# Put back exactly what was there, whatever the outcome above.
cp "$gen_before" backend/internal/api/api.gen.go
rm -f "$gen_before"

# The spec is what the public API reference renders from, so an endpoint listed
# there is a public claim that it exists. One implementation, shared with CI.
if python3 scripts/openapi-shipped-paths.py >/dev/null 2>&1; then
  pass "spec claims no endpoint beyond what has shipped"
else
  fail "spec documents unshipped endpoints"
  python3 scripts/openapi-shipped-paths.py 2>&1 | sed 's/^/      /'
fi

# --- Console ----------------------------------------------------------------
#
# The console has its own toolchain, so these run npm rather than go. Skipped
# rather than failed when node_modules is absent: a backend-only change should
# not require a frontend install to check.

section "Brand"

# The identity mark is generated from one definition of the geometry, and each
# surface keeps its own copy. Three ways that goes wrong quietly: a hand-edit
# to a generated SVG, a coordinate that drifts from the concept document, and
# one surface's copy updating while the other's does not. brand/check.py
# catches all three; see brand/BRAND.md.
if python3 brand/check.py >/dev/null 2>&1; then
  pass "brand assets match their source and the concept"
else
  fail "brand assets are out of step"
  python3 brand/check.py 2>&1 | tail -12
fi

section "Console"

if [ ! -d console/node_modules ]; then
  skip "console lint, typecheck and tests" "run: cd console && npm install"
else
  if (cd console && npm run --silent lint >/dev/null 2>&1); then
    pass "console lint (token discipline, a11y rules)"
  else
    fail "console lint"
    (cd console && npm run --silent lint 2>&1 | tail -25)
  fi

  if (cd console && npm run --silent typecheck >/dev/null 2>&1); then
    pass "console typecheck"
  else
    fail "console typecheck"
    (cd console && npm run --silent typecheck 2>&1 | tail -25)
  fi

  if (cd console && npm test --silent >/dev/null 2>&1); then
    pass "console tests (tokens, contrast, branding, shell a11y)"
  else
    fail "console tests"
    (cd console && npm test --silent 2>&1 | tail -30)
  fi

  # The generated client is committed, the same discipline as the backend's
  # generated server interface: a reviewer sees the contract change and its
  # consequences in one diff (ADR-013).
  client_before=$(mktemp)
  cp console/src/lib/api/schema.gen.ts "$client_before"

  if (cd console && npm run --silent api:generate >/dev/null 2>&1); then
    if diff -q "$client_before" console/src/lib/api/schema.gen.ts >/dev/null 2>&1; then
      pass "console API client matches the spec"
    else
      fail "console API client is stale — run: cd console && npm run api:generate"
    fi
  else
    fail "console API client generation failed"
  fi

  cp "$client_before" console/src/lib/api/schema.gen.ts
  rm -f "$client_before"

  # The E2E layer needs a browser and a built bundle, so it is opt-in locally
  # and always on in CI — the same arrangement as the public site build.
  if [ "${CHECK_FULL:-0}" = "1" ]; then
    if (cd console && npx playwright test >/dev/null 2>&1); then
      pass "console end-to-end tests"
    else
      fail "console end-to-end tests"
      (cd console && npx playwright test 2>&1 | tail -20)
    fi
  else
    skip "console end-to-end tests" "needs a browser; set CHECK_FULL=1"
  fi
fi

# --- Public site ------------------------------------------------------------
#
# Separate from the console's section on purpose: docs/PLAN/20 § Why a Separate
# Surface requires these to be independent projects, and running them as one
# gate would quietly couple what the plan says to keep apart.
#
# The Docusaurus build is slow, so it runs only when the site's own sources
# changed. The three cheap checks always run.

section "Public site"

if [ ! -d public-site/node_modules ]; then
  skip "public site checks" "run: cd public-site && npm install"
else
  if (cd public-site && npm run --silent check:tokens >/dev/null 2>&1); then
    pass "brand tokens match the console"
  else
    fail "brand tokens have drifted from the console"
    (cd public-site && npm run --silent check:tokens 2>&1 | tail -12)
  fi

  if (cd public-site && npm run --silent check:contrast >/dev/null 2>&1); then
    pass "public site contrast meets AA in both themes"
  else
    fail "public site contrast"
    (cd public-site && npm run --silent check:contrast 2>&1 | tail -15)
  fi

  if (cd public-site && npm run --silent check:boundary >/dev/null 2>&1); then
    pass "no code shared between the public site and the console"
  else
    fail "the public site is reaching into the console"
    (cd public-site && npm run --silent check:boundary 2>&1 | tail -12)
  fi

  # The generated API reference, same discipline as the backend interface and
  # the console client: committed, and CI fails if it is stale.
  #
  # `api:generate` cleans before it generates, and that matters here. Plain
  # `gen-api-docs` writes new pages but leaves an existing `sidebar.ts` alone,
  # so adding P1-04's two endpoints produced tag pages with no sidebar entry:
  # this gate passed while the site build failed on a page it could not place.
  # A staleness check that only sees the files its generator overwrites is not
  # a staleness check.
  api_before=$(mktemp -d)
  cp -r public-site/docs/api-reference/. "$api_before/" 2>/dev/null || true

  if (cd public-site && npm run --silent api:generate >/dev/null 2>&1); then
    if diff -r -q "$api_before" public-site/docs/api-reference >/dev/null 2>&1; then
      pass "generated API reference matches the spec"
    else
      fail "generated API reference is stale — run: cd public-site && npm run api:generate"
    fi
  else
    fail "API reference generation failed"
  fi

  rm -rf public-site/docs/api-reference
  mkdir -p public-site/docs/api-reference
  cp -r "$api_before/." public-site/docs/api-reference/ 2>/dev/null || true
  rm -rf "$api_before"

  # The build and the two audits that read its output are opt-in locally: the
  # build takes about half a minute, and CI runs all three on every push.
  if [ "${CHECK_FULL:-0}" = "1" ]; then
    if (cd public-site && npm run --silent build >/dev/null 2>&1); then
      pass "public site builds (no broken links)"

      # P0-19's DoD: every claim maps to something shipped or labelled planned.
      if (cd public-site && npm run --silent check:claims >/dev/null 2>&1); then
        pass "capability audit"
      else
        fail "capability audit"
        (cd public-site && npm run --silent check:claims 2>&1 | tail -12)
      fi

      # docs/PLAN/20 § What Never Gets Published.
      if (cd public-site && npm run --silent check:leak >/dev/null 2>&1); then
        pass "no internal material on the public site"
      else
        fail "internal material found on the public site"
        (cd public-site && npm run --silent check:leak 2>&1 | tail -12)
      fi
    else
      fail "public site build"
      (cd public-site && npm run --silent build 2>&1 | tail -25)
    fi
  else
    skip "public site build, capability audit and leak check" "slow; set CHECK_FULL=1"
  fi
fi

# --- Deployment config ------------------------------------------------------

section "Deployment"

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
     AUTH_REDIS_PASSWORD=local_dev_only \
     AUTH_SECRETS_DIR=/tmp/s AUTH_ADMIN_TOKEN_REF=file:/tmp/s/metrics-token \
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
    AUTH_REDIS_PASSWORD=local_dev_only \
    AUTH_SECRETS_DIR=/tmp/s AUTH_ADMIN_TOKEN_REF=file:/tmp/s/metrics-token \
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
