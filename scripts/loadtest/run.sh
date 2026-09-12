#!/usr/bin/env bash
#
# The load test docs/PLAN/12 asks for before each major release:
#
#   > Load-test /oauth/token and /oauth/authorize under realistic concurrent-user
#   > simulation before each major release, not just before the initial launch.
#   > Include a mixed workload test […] since production will never see just one
#   > traffic type in isolation.
#
# Run it on the staging VM. It drives 127.0.0.1:10800 — the service's own port —
# because going through the Cloudflare tunnel from a laptop measures Cloudflare,
# the internet and a home uplink, and then reports all three as the service's
# latency.
#
#   ssh infra@… 'bash ~/auth/scripts/loadtest/run.sh'
#
#   LOAD_WORKERS   concurrent workers per phase (default 20)
#   LOAD_SECONDS   seconds per phase (default 30)
#   LOAD_STRICT    divide every target by N — the harness's own mutation test:
#                  a run that has only ever printed "within targets" has not
#                  been shown capable of printing anything else
#
# Exits non-zero when a phase misses a docs/PLAN/12 target or sees failures.
set -uo pipefail

FIXTURE_NAME="loadtest-run"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/_fixture.sh"

fixture_up
trap fixture_down EXIT
fixture_banner

cd "$HERE" || exit 2
LOAD_CLIENT_ID="$APP" \
LOAD_EMAIL="$EMAIL" \
LOAD_PASSWORD="$PASSWORD" \
LOAD_ORG="${LOAD_ORG_OVERRIDE:-$ORG}" \
LOAD_WORKERS="${LOAD_WORKERS:-20}" \
LOAD_SECONDS="${LOAD_SECONDS:-30}" \
LOAD_STRICT="${LOAD_STRICT:-1}" \
  go run .
