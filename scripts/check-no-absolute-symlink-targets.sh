#!/usr/bin/env bash
# scripts/check-no-absolute-symlink-targets.sh — rejects tracked symlinks whose
# stored target is an absolute path (starts with "/").
#
# An absolute symlink target pins a working tree to one host (a machine-local
# directory, an operator home path), so a fresh clone elsewhere can never use
# the path the symlink was meant for. Tracked symlink targets must stay
# relative so the repo is portable.
#
# Report convention (scripts/check-no-local-leaks.sh): print the repo-relative
# path and a short class of problem. The offending target is an operator-local
# path and is never echoed into logs, output or reports — only the "class" is
# named, so this script deliberately does not print it even when it finds one.
#
# Usage: scripts/check-no-absolute-symlink-targets.sh
#   CHECK_REPO=...  optional override of the repo root (tests point it at a
#                   scratch fixture repo); defaults to the git top level.

set -euo pipefail

ROOT="${CHECK_REPO:-$(git rev-parse --show-toplevel)}"
cd "$ROOT"

echo "==> Tracked symlink audit (absolute targets)"

FAIL=0
COUNT=0
while read -r mode sha _ path; do
    [[ "$mode" == "120000" ]] || continue
    COUNT=$((COUNT + 1))
    target="$(git cat-file blob "$sha")"
    if [[ "$target" == /* ]]; then
        echo "  ✗ $path: absolute symlink target"
        FAIL=1
    fi
done < <(git ls-files -s)

if (( FAIL > 0 )); then
    echo "FAIL: tracked symlink(s) with absolute target(s) — use relative targets"
    exit 1
fi
echo "OK: no absolute symlink targets ($COUNT tracked symlink(s) checked)"
