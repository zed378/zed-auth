#!/bin/sh
# Points git at the repository's tracked hooks directory.
# Run once after cloning. Tracked hooks beat .git/hooks because they are reviewable.
set -e
git config core.hooksPath scripts/hooks
chmod +x scripts/hooks/* 2>/dev/null || true
echo "Hooks installed: core.hooksPath -> scripts/hooks"
echo "Active hooks:"
ls -1 scripts/hooks
