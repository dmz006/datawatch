#!/usr/bin/env bash
# Verify the XSS-through-DOM fix in internal/server/web/app.js.
# Checks only the file of this unit; proves the scanner barrier with
# scripts/codeql-local.sh --languages javascript-typescript (documented
# scanner interface).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source .datawatch/verify/_scanner-common.sh

target="internal/server/web/app.js"

# 1. JS parse check on this unit's file only.
[ -f "$target" ] || { echo "FAIL: $target not found"; exit 1; }
node --input-type=module --check < "$target" || { echo "FAIL: node parse check of $target"; exit 1; }
echo "OK: node parses $target"

# 2. cumulative changed set vs merge base (presence check; artefact dirs and
#    CHANGELOG.md excluded by dw_changed_set, per artefact_files_allowed).
changed="$(dw_changed_set)"
printf '%s\n' "$changed" | grep -qx -- "$target" || echo "NOTE: $target not present in the cumulative changed set yet"

# 3. local CodeQL proof.
scan="$(dw_run_scan javascript-typescript)"
dw_assert_pair "$scan" "js/xss-through-dom" "$target" || exit 1

echo "PASS: fix-quick-command-select"
