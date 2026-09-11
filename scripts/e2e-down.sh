#!/usr/bin/env bash
#
# Stops everything scripts/e2e-up.sh started.
#
#   bash scripts/e2e-down.sh               # stop the demos and the stack
#   KEEP_DATA=1 bash scripts/e2e-down.sh   # keep the database volume
#
# The volumes go by default. A seeded database that survives between runs is
# what makes a suite pass on a machine where it should fail: the organization,
# the administrator and the console's client are all still there from
# yesterday, so a broken bootstrap goes unnoticed until CI, which has no
# yesterday.
#
# **It verifies.** An earlier version killed the recorded pids and reported
# success while both demo servers were still listening — the pids belonged to
# `go run` wrappers rather than to the servers themselves. The next run then
# started against a stale demo holding a client_id the wiped database no longer
# had, and the failure pointed at the authorize endpoint. A teardown that
# cannot confirm it tore anything down is worse than none.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1
ROOT="$PWD"

PID_FILE="$ROOT/.e2e.pids"
PORTS="8090 8091"

# still_listening reports whether anything answers on a port.
#
# `curl`, not `lsof` — Git Bash has no lsof, and the first version of this
# script used it and silently did nothing on Windows. Every machine that can
# run this suite has curl, because the suite is made of HTTP.
still_listening() {
  curl -sS -o /dev/null --max-time 2 "http://localhost:$1/healthz" 2>/dev/null
}

if [ -f "$PID_FILE" ]; then
  while read -r pid; do
    [ -n "$pid" ] || continue
    kill "$pid" 2>/dev/null
  done < "$PID_FILE"
  rm -f "$PID_FILE"
fi

# Give them a moment to exit, then say plainly if they did not.
for _ in 1 2 3 4 5; do
  leftover=""
  for port in $PORTS; do
    still_listening "$port" && leftover="$leftover $port"
  done
  [ -z "$leftover" ] && break
  sleep 1
done

if [ -n "${leftover:-}" ]; then
  printf 'still listening on%s — a later run will serve stale configuration.\n' "$leftover" >&2
  printf 'find and stop it:  netstat -ano | grep -E ":(%s) "\n' "$(echo "$PORTS" | tr ' ' '|')" >&2
  printf '                   (Linux/macOS: lsof -ti :%s | xargs kill)\n' "${PORTS%% *}" >&2
else
  echo "the demo applications are stopped"
fi

if [ "${KEEP_DATA:-0}" = "1" ]; then
  docker compose -f deploy/docker-compose.yml down 2>&1 | tail -2
  echo "the database volume was kept (KEEP_DATA=1)"
else
  docker compose -f deploy/docker-compose.yml down -v 2>&1 | tail -2
fi

rm -rf "$ROOT/.e2e-bin"
rm -f "$ROOT/.e2e.env" "$ROOT/.e2e-demo-a.log" "$ROOT/.e2e-demo-b.log" "$ROOT/.e2e-console-build.log"
echo "done"
