#!/usr/bin/env sh
# Coverage floors for the packages where a gap is a security problem.
#
# `P0-15` step 6 is specific about this: "Set a coverage floor for
# internal/authn, internal/authz, and internal/oidc specifically, rather than a
# meaningless repo-wide average."
#
# The reasoning is worth keeping. A repo-wide percentage is satisfied by
# testing whatever is easiest, and the easiest code to test is rarely the code
# where a bug matters. Eighty per cent overall can mean full coverage of
# configuration parsing and none of the token verification path. Floors on
# named packages say which code the number is about.
#
# The three packages do not exist yet — they arrive in Phase 1. That is why
# this script distinguishes "absent" from "below floor": an absent package is
# reported and skipped, so the floor starts applying the moment the package
# appears rather than on the day somebody remembers to add it here.
set -eu

cd "$(dirname "$0")/.."

FLOOR=80

# Package path : floor : the task that creates it.
#
# The floors are equal for now. They are listed separately because the right
# number for token verification is not obviously the right number for
# configuration, and a single constant invites nobody to think about it.
PACKAGES="
internal/authn:$FLOOR:P1-01
internal/signing:$FLOOR:P1-03
internal/authz:$FLOOR:P2-06
internal/oidc:$FLOOR:P1-06
internal/oauth/client:$FLOOR:P1-05
internal/session:$FLOOR:P1-11
internal/oauth/authorize:$FLOOR:P1-06
"

profile=$(mktemp)
trap 'rm -f "$profile"' EXIT

echo "Coverage floors (P0-15):"

missing=0
failed=0
checked=0

for entry in $PACKAGES; do
  [ -z "$entry" ] && continue

  pkg=$(echo "$entry" | cut -d: -f1)
  floor=$(echo "$entry" | cut -d: -f2)
  task=$(echo "$entry" | cut -d: -f3)

  # Go source, not just a directory. P0-02 scaffolded empty directories for
  # these packages, so a `-d` test reports them as present, `go test` fails
  # with "no Go files", and this script called that a coverage failure. A
  # floor that fails before the code exists is a floor everyone learns to
  # ignore, which is the opposite of what a floor is for.
  if [ -z "$(find "backend/$pkg" -name '*.go' -print -quit 2>/dev/null)" ]; then
    printf '  -  %-20s not built yet (%s)\n' "$pkg" "$task"
    missing=$((missing + 1))
    continue
  fi

  checked=$((checked + 1))

  # -tags=integration, because that is how these packages are actually tested.
  #
  # internal/signing measures 44% from unit tests alone and 83% with its
  # integration tests, and the second number is the real one — the store, the
  # rotation state machine and the database constraints are the parts most
  # worth a floor. Measuring a subset of the suite is measuring the wrong
  # thing, the same mistake as testing a compiler's input instead of its
  # output.
  if ! (cd backend && go test -tags=integration -coverprofile="$profile" -covermode=atomic "./$pkg/..." >/dev/null 2>&1); then
    printf '  ✗  %-20s tests failed\n' "$pkg"
    failed=$((failed + 1))
    continue
  fi

  pct=$(cd backend && go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')

  # Integer comparison: shells cannot compare decimals portably, and a floor
  # accurate to one per cent is accurate enough for a floor.
  whole=${pct%%.*}

  if [ "$whole" -lt "$floor" ]; then
    printf '  ✗  %-20s %s%% < %s%%\n' "$pkg" "$pct" "$floor"
    failed=$((failed + 1))
  else
    printf '  ✓  %-20s %s%% ≥ %s%%\n' "$pkg" "$pct" "$floor"
  fi
done

echo ""

if [ "$failed" -gt 0 ]; then
  echo "$failed package(s) below their coverage floor."
  echo ""
  echo "These floors are on the packages where an untested path is a security"
  echo "problem rather than an inconvenience. Raising the floor by deleting a"
  echo "package from this list is not a fix."
  exit 1
fi

if [ "$checked" -eq 0 ]; then
  echo "No floored packages exist yet ($missing pending). This is expected in Phase 0."
  echo "Each floor starts applying the moment its package appears."
  exit 0
fi

echo "$checked package(s) meet their floor; $missing not built yet."
