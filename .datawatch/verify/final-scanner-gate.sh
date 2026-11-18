#!/usr/bin/env bash
# Final end-to-end gate: all eight scanner findings gone, builds and tests
# green, docs pass present.
# Only asserts PRESENCE of this change set's required files (plus the
# scanner/build/test/proof gates); it never asserts a finite allow-list of
# the whole changed set. Generated artefacts (docs/plans/drafts/,
# docs/plans/parity-issues/, docs/plans/reports/, CHANGELOG.md) are excluded
# from the changed set by dw_changed_set, per final_gate_artefact_allowed.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
source .datawatch/verify/_scanner-common.sh

go_files="internal/autonomous/file_drift.go internal/autonomous/uncommitted_work.go internal/autonomous/cleanup_status.go"
js_files="internal/server/web/app.js internal/server/web/app-task-routing.test.js internal/server/web/app-plan-records.test.js"

# 1. docs sync first.
dw_sync_docs

# 2. presence check: this change set's files must be in the cumulative
#    changed set vs merge base (artefact dirs excluded by dw_changed_set).
changed="$(dw_changed_set)"
for f in $go_files $js_files; do
  if ! printf '%s\n' "$changed" | grep -qx -- "$f"; then
    echo "FAIL: $f is not in the cumulative changed set"
    exit 1
  fi
  echo "OK: $f in the cumulative changed set"
done
docs="$(printf '%s\n' "$changed" | grep -E -- '^docs/' || true)"
if [ -z "$docs" ]; then
  echo "FAIL: no docs/ file in the cumulative changed set for the documentation pass"
  exit 1
fi
echo "OK: docs pass files present: $(printf '%s' "$docs" | tr '\n' ' ')"

# 3. gofmt on this change set's Go files only (never over a package or directory).
for f in $go_files; do
  bad="$(gofmt -l "$f" || true)"
  if [ -n "$bad" ]; then echo "FAIL: gofmt -l reports: $bad"; exit 1; fi
done
echo "OK: gofmt clean on the Go files"

# 4. JS parse checks on this change set's web files.
for f in $js_files; do
  node --input-type=module --check < "$f" || { echo "FAIL: node parse check of $f"; exit 1; }
done
echo "OK: node parses the web files"

# 5. build + Go tests for the touched package.
go build ./internal/autonomous/ || { echo "FAIL: go build ./internal/autonomous/"; exit 1; }
echo "OK: go build ./internal/autonomous/"
go test ./internal/autonomous/ || { echo "FAIL: go test ./internal/autonomous/"; exit 1; }
echo "OK: go test ./internal/autonomous/"

# 6. the two web test files must stay green.
for f in internal/server/web/app-task-routing.test.js internal/server/web/app-plan-records.test.js; do
  [ -f "$f" ] || { echo "FAIL: $f not found"; exit 1; }
  node --test "$f" || { echo "FAIL: node --test $f"; exit 1; }
done
echo "OK: node --test passes for the two test files"

# 7. local CodeQL proof for all eight findings (rule, file pairs).
goscan="$(dw_run_scan go)"
rc=0
dw_assert_pair "$goscan" "go/command-injection" "internal/autonomous/file_drift.go" || rc=1
for f in $go_files; do
  dw_assert_pair "$goscan" "go/path-injection" "$f" || rc=1
done
[ "$rc" -eq 0 ] || exit 1

jsscan="$(dw_run_scan javascript-typescript)"
rc=0
dw_assert_pair "$jsscan" "js/xss-through-dom" "internal/server/web/app.js" || rc=1
dw_assert_pair "$jsscan" "js/bad-tag-filter" "internal/server/web/app-task-routing.test.js" || rc=1
dw_assert_pair "$jsscan" "js/bad-code-sanitization" "internal/server/web/app-plan-records.test.js" || rc=1
[ "$rc" -eq 0 ] || exit 1

echo "PASS: final-scanner-gate"
