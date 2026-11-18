#!/usr/bin/env bash
# Verification: a script the task creates itself is accepted at start and still refused when the spec does not mention creating it.
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
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|AGENT.md|CLAUDE.md|docs/plans/*) ;;
    internal/autonomous/*.go|docs/*.md|docs/*/*.md|internal/server/web/docs/*|internal/server/web/docs/*/*) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

go build ./internal/autonomous/ || bad "go build"
go test -count=1 ./internal/autonomous/ -run 'Prestart' || bad "prestart tests"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "prestart-task-created.sh: FAIL"; exit 1; }
echo "prestart-task-created.sh: PASS"
