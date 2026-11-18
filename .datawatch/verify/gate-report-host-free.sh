#!/usr/bin/env bash
# Verification: the generated gate report carries no host name, and the host-free test exists and passes.
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
    internal/genartifacts/*|internal/genartifacts/*/*|scripts/check-generated-reports-leaks.sh|scripts/check-generated-reports-leaks_test.sh|Makefile|docs/*.md|docs/*/*.md|internal/server/web/docs/*|internal/server/web/docs/*/*) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

[ -f internal/genartifacts/host_free_test.go ] || bad "internal/genartifacts/host_free_test.go is missing"
go build ./internal/genartifacts/ || bad "go build"
go vet ./internal/genartifacts/ || bad "go vet"
go test -count=1 ./internal/genartifacts/ || bad "go test internal/genartifacts"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "gate-report-host-free.sh: FAIL"; exit 1; }
echo "gate-report-host-free.sh: PASS"
