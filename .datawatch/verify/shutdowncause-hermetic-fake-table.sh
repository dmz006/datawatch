#!/usr/bin/env bash
# Verification: the fake process table regression test exists and passes, including 20 runs under the race detector.
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"; trap 'rm -f "$CH"' EXIT
{ git diff --name-only "$BASE"; git ls-files --others --exclude-standard; } | sort -u > "$CH"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|docs/plans/*) ;;
    internal/shutdowncause/*.go|docs/howto/testing-against-a-running-daemon.md|internal/server/web/docs/howto/testing-against-a-running-daemon.md) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

[ -f internal/shutdowncause/proctable_linux_test.go ] || bad "internal/shutdowncause/proctable_linux_test.go is missing"
go vet ./internal/shutdowncause/ || bad "go vet"
go test -count=1 ./internal/shutdowncause/ || bad "go test"
go test -race -parallel 1 -count=20 -run TestProcScanFakeTable ./internal/shutdowncause/ || bad "race runs of TestProcScanFakeTable"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "shutdowncause-hermetic-fake-table.sh: FAIL"; exit 1; }
echo "shutdowncause-hermetic-fake-table.sh: PASS"
