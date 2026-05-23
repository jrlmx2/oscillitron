#!/usr/bin/env bash
# CLAUDE GENERATED
# Point this checkout's git at the tracked hooks directory.
# One-time per clone. Idempotent — safe to re-run.
#
# What it does: `git config core.hooksPath scripts/git-hooks`
# That's the whole thing. Git fires hooks from .git/hooks/ by
# default; this redirects to scripts/git-hooks/ so the hooks live
# in the repo and every dev gets the same checks.
set -euo pipefail

git config core.hooksPath scripts/git-hooks
chmod +x scripts/git-hooks/*

echo "Git hooks installed: core.hooksPath = scripts/git-hooks/"
echo "Hooks now active: $(ls scripts/git-hooks/ | tr '\n' ' ')"
