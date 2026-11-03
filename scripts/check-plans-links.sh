#!/usr/bin/env bash
#
# check-plans-links.sh — fail if any markdown link target still names a
# dated plan (YYYY-MM-DD-<slug>.md) that cannot be resolved against the
# docs/plans/ tree (top level, historical-plans/, or any subdirectory).
# Plain prose mentions are not flagged; remote http(s) targets are not
# checked (they point outside this repository).
#
# Scans the same reference surfaces that tidy-plans.sh rewrites: files
# under scripts/, the root rules files (AGENT.md, CLAUDE.md, CONTEXT.md,
# CONTRIBUTING.md, DATAWATCH-CONTEXT.md, README.md, SECURITY.md), and
# every file under docs/.
#
# Output is a single count (dangling plan references) — never a list of
# files.
#
# Usage:
#   scripts/check-plans-links.sh           exit 1 if any dangling reference
#   scripts/check-plans-links.sh --quiet   stay silent when the count is zero

set -euo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
exec python3 "$ROOT/scripts/check-plans-links.py" "$ROOT" "$@"
