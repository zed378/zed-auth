#!/usr/bin/env bash
#
# Phase 3 acceptance (P3-15), on the staging VM against its own stack.
#
#   ssh infra@… 'bash ~/auth/scripts/acceptance-phase3.sh'
#
# `docs/PLAN/17-ACCEPTANCE-CRITERIA.md` § Phase 3 states three criteria. The Go
# program beside this script walks each one against the running service —
# enrolling a real TOTP factor, signing in with it, losing the device, rotating
# refresh tokens, revoking a session — and prints what it found.
#
# The fixture is the load test's: a throwaway account and application, created
# directly in the database because no API grants a manager role (PG-31), and
# deleted on exit whatever happened. The account's factor and recovery codes go
# with it (ON DELETE CASCADE).
set -uo pipefail

FIXTURE_NAME="acceptance-phase3"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/loadtest/_fixture.sh"

fixture_up
trap fixture_down EXIT
fixture_banner

cd "$HERE/acceptance/phase3" || exit 2
ACCEPT_CLIENT_ID="$APP" \
ACCEPT_EMAIL="$EMAIL" \
ACCEPT_PASSWORD="$PASSWORD" \
  go run .
