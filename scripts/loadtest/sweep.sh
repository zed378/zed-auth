#!/usr/bin/env bash
#
# Concurrency sweep for one endpoint (P1-28, docs/PLAN/12 § Capacity Planning).
#
#   > Baseline capacity planning on: expected concurrent active users, expected
#   > token issuance rate (logins + refreshes) […]
#
# A pass/fail at one concurrency cannot tell a slow endpoint from a saturated
# one. The first run of `run.sh` met every isolated target and missed two in the
# mixed workload, with a shape — p50 46ms at 355 rps with 20 requests in flight
# — that is queue time, not per-request cost. This walks the concurrency up
# until throughput stops rising, which is the number capacity planning needs.
#
# Container CPU is sampled throughout, so the ceiling is attributed rather than
# guessed at.
#
#   PHASE=token bash ~/auth/scripts/loadtest/sweep.sh
#   PHASE=authorize|userinfo|management, default token
set -uo pipefail

FIXTURE_NAME="loadtest-sweep"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/_fixture.sh"

fixture_up
trap fixture_down EXIT

PHASE="${PHASE:-token}"
STEPS="${STEPS:-2 4 8 16 24 32}"
SECONDS_PER_STEP="${LOAD_SECONDS:-20}"

cd "$HERE" || exit 2
go build -o /tmp/zedauth-loadgen . || exit 2

echo "sweep       $PHASE, ${SECONDS_PER_STEP}s per step, on $(nproc) cores shared with $(docker ps -q | wc -l) containers"
echo
printf '%8s %9s %9s %9s %9s %9s %18s\n' workers rps p50 p95 p99 max 'cpu% svc/pg/redis'

for W in $STEPS; do
  SAMPLE="/tmp/zedauth-sweep-cpu.$W"
  ( while :; do
      docker stats --no-stream --format '{{.Name}} {{.CPUPerc}}' \
        zedauth-authservice-1 zedauth-postgres-1 zedauth-redis-1 2>/dev/null
      sleep 2
    done ) > "$SAMPLE" 2>/dev/null &
  SAMPLER=$!

  OUT=$(LOAD_CLIENT_ID="$APP" LOAD_EMAIL="$EMAIL" LOAD_PASSWORD="$PASSWORD" LOAD_ORG="$ORG" \
        LOAD_ONLY="$PHASE" LOAD_WORKERS="$W" LOAD_SECONDS="$SECONDS_PER_STEP" \
        /tmp/zedauth-loadgen 2>&1)

  kill "$SAMPLER" 2>/dev/null
  wait "$SAMPLER" 2>/dev/null

  CPU=$(awk '{ split($2, a, "%"); s[$1] += a[1]; n[$1]++ }
             END { printf "%.0f/%.0f/%.0f",
                   s["zedauth-authservice-1"] / (n["zedauth-authservice-1"] ? n["zedauth-authservice-1"] : 1),
                   s["zedauth-postgres-1"]    / (n["zedauth-postgres-1"]    ? n["zedauth-postgres-1"]    : 1),
                   s["zedauth-redis-1"]       / (n["zedauth-redis-1"]       ? n["zedauth-redis-1"]       : 1) }' "$SAMPLE")
  rm -f "$SAMPLE"

  # The generator prints one machine-readable line per phase. Finding the
  # columns of the human table by counting whitespace breaks the first time a
  # phase name contains a space, and "/v1 management (CRUD read)" already does.
  LINE=$(printf '%s' "$OUT" | grep '^CSV,' | head -1)
  if [ -z "$LINE" ]; then
    printf '%8s   no samples: %s\n' "$W" "$(printf '%s' "$OUT" | tr '\n' '|' | tail -c 300)"
    continue
  fi

  printf '%s,%s\n' "$LINE" "$CPU" | awk -F, -v w="$W" \
    '{ printf "%8s %9s %9s %9s %9s %9s %18s\n", w, $8, $4"ms", $5"ms", $6"ms", $7"ms", $10 }'

  FAILED=$(printf '%s' "$LINE" | awk -F, '{print $9}')
  [ "${FAILED:-0}" = "0" ] || printf '%8s   %s failed requests\n' "" "$FAILED"
done
