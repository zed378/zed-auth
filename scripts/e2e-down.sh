#!/usr/bin/env bash
#
# Stops everything scripts/e2e-up.sh started.
#
#   bash scripts/e2e-down.sh          # stop the demos and the stack
#   KEEP_DATA=1 bash scripts/e2e-down.sh   # keep the database volume
#
# The volumes go by default. A seeded database that survives between runs is
# the thing that makes a suite pass on a machine where it should fail: the
# organization, the administrator and the console's client are all still there
# from yesterday, so a broken bootstrap goes unnoticed until CI, which has no
# yesterday.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1
ROOT="$PWD"

PID_FILE="$ROOT/.e2e.pids"

if [ -f "$PID_FILE" ]; then
  while read -r pid; do
    [ -n "$pid" ] || continue
    # The recorded pid is `go run`, which spawns the compiled binary as a
    # child. Killing only the parent leaves the server holding its port, and
    # the next run fails to bind with an error that names neither cause.
    pkill -P "$pid" 2>/dev/null
    kill "$pid" 2>/dev/null
  done < "$PID_FILE"
  rm -f "$PID_FILE"
  echo "stopped the demo applications"
fi

# Anything still holding the demo ports, including a binary orphaned by an
# earlier interrupted run.
for port in 8090 8091; do
  pid=$(lsof -ti ":$port" 2>/dev/null)
  [ -n "$pid" ] && kill $pid 2>/dev/null && echo "freed port $port"
done

if [ "${KEEP_DATA:-0}" = "1" ]; then
  docker compose -f deploy/docker-compose.yml down 2>&1 | tail -2
  echo "the database volume was kept (KEEP_DATA=1)"
else
  docker compose -f deploy/docker-compose.yml down -v 2>&1 | tail -2
fi

rm -f "$ROOT/.e2e.env" "$ROOT/.e2e-demo-a.log" "$ROOT/.e2e-demo-b.log" "$ROOT/.e2e-console-build.log"
echo "done"
