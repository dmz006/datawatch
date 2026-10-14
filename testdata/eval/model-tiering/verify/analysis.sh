#!/usr/bin/env bash
# Eval cell verifier — analysis task (tracker output.log / safe gc fix).
# Usage: analysis.sh <cell-dir>. Exit 0 only if the cell satisfies specs/analysis.md.
# Adapted from .datawatch/verify/tracker-excludes-output-log.sh, but runs inside
# the cell and adds a hidden behavioural test the worker never sees.
set -euo pipefail
echo "[$(date '+%H:%M:%S')] start analysis verify"
CELL=${1:?usage: analysis.sh <cell-dir>}
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$CELL"
[ -f internal/session/tracker.go ] || { echo "FAIL: not a datawatch snapshot (no internal/session/tracker.go)"; exit 1; }
[ -f internal/session/tracker_test.go ] || { echo "FAIL: internal/session/tracker_test.go missing"; exit 1; }
# gofmt only the .go files the cell changed: the baseline tree already has
# unrelated files that predate the current gofmt alignment rules.
root=$(git rev-list --max-parents=0 HEAD | tail -n1)
touched=$( { git diff --name-only "$root"; git ls-files --others --exclude-standard; } | grep -E '\.go$' | sort -u || true)
[ -z "$touched" ] && touched="internal/session/tracker.go internal/session/tracker_test.go"
existing=()
for f in $touched; do [ -f "$f" ] && existing+=("$f"); done
unformatted=$(gofmt -l "${existing[@]}")
[ -z "$unformatted" ] || { echo "FAIL: gofmt needed: $unformatted"; exit 1; }
grep -q 'gc.auto' internal/session/tracker.go || { echo "FAIL: tracker.go does not set gc.auto"; exit 1; }
grep -lq 'gc.auto' internal/session/*_test.go || { echo "FAIL: no test asserting gc.auto"; exit 1; }
grep -lq 'output.log' internal/session/*tracker*_test.go || { echo "FAIL: no tracker test covering output.log"; exit 1; }
# The docs BM25 index is a generated, gitignored embed, absent from a
# `git archive` snapshot. Seed the same '{}' placeholder the Makefile uses.
mkdir -p internal/docsindex/assets
[ -f internal/docsindex/assets/docs-bm25-index.json ] || echo '{}' > internal/docsindex/assets/docs-bm25-index.json
echo "[$(date '+%H:%M:%S')] go build ./..."
go build ./...
go vet ./internal/session/
echo "[$(date '+%H:%M:%S')] go test ./internal/session/ (worker tests + package)"
go test -count=1 ./internal/session/
echo "[$(date '+%H:%M:%S')] hidden behavioural test"
[ ! -e internal/session/zz_eval_hidden_test.go ] || { echo "FAIL: zz_eval_hidden_test.go already exists in cell"; exit 1; }
cp "$HERE/hidden/tracker_eval_hidden_test.go" internal/session/zz_eval_hidden_test.go
# cwd is the cell dir for the rest of the script; literal relative path only.
trap 'rm -f internal/session/zz_eval_hidden_test.go' EXIT
go test -count=1 -run '^TestEvalHidden_' ./internal/session/
if [ "${EVAL_GOLANGCI:-0}" = 1 ] && command -v golangci-lint >/dev/null 2>&1; then
  rm -f internal/session/zz_eval_hidden_test.go
  golangci-lint run ./internal/session/...
fi
echo "RESULT: PASS"
echo "[$(date '+%H:%M:%S')] done analysis verify"
