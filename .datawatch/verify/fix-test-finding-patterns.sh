#!/usr/bin/env bash
# Verify the scanner-flagged pattern rewrites in
# internal/server/web/app-task-routing.test.js (js/bad-tag-filter) and
# internal/server/web/app-plan-records.test.js (js/bad-code-sanitization).
# Checks only the files of this unit; proves the scanner barrier with
# scripts/codeql-local.sh --languages javascript-typescript (documented
# scanner interface).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source .datawatch/verify/_scanner-common.sh

files="internal/server/web/app-task-routing.test.js internal/server/web/app-plan-records.test.js"

# 1. JS parse checks on this unit's files only.
for f in $files; do
  [ -f "$f" ] || { echo "FAIL: $f not found"; exit 1; }
  node --input-type=module --check < "$f" || { echo "FAIL: node parse check of $f"; exit 1; }
  echo "OK: node parses $f"
done

# 2. the two test files must stay green.
for f in $files; do
  node --test "$f" || { echo "FAIL: node --test $f"; exit 1; }
  echo "OK: node --test $f"
done

# 3. cumulative changed set vs merge base (presence check; artefact dirs and
#    CHANGELOG.md excluded by dw_changed_set, per artefact_files_allowed).
changed="$(dw_changed_set)"
for f in $files; do
  printf '%s\n' "$changed" | grep -qx -- "$f" || echo "NOTE: $f not present in the cumulative changed set yet"
done

# 4. local CodeQL proof.
scan="$(dw_run_scan javascript-typescript)"
rc=0
dw_assert_pair "$scan" "js/bad-tag-filter" "internal/server/web/app-task-routing.test.js" || rc=1
dw_assert_pair "$scan" "js/bad-code-sanitization" "internal/server/web/app-plan-records.test.js" || rc=1
[ "$rc" -eq 0 ] || exit 1

echo "PASS: fix-test-finding-patterns"
