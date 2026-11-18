#!/usr/bin/env bash
# Verification: the unexecutable-script lint states which case applies and the how-to documents the behavior.
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

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
go test -count=1 ./internal/autonomous/ -run 'Unexecutable|DecomposeLint' || bad "lint tests"
[ -f docs/howto/automaton-verification-scripts.md ] || bad "docs/howto/automaton-verification-scripts.md is missing"
for f in docs/howto/automaton-verification-scripts.md internal/server/web/docs/howto/automaton-verification-scripts.md; do
  [ -f "$f" ] && { grep -qi 'verbatim' "$f" || bad "$f lacks the verbatim-block case"; }
done
diff -q docs/howto/automaton-verification-scripts.md internal/server/web/docs/howto/automaton-verification-scripts.md >/dev/null 2>&1 || bad "embedded copy differs"
[ -f scripts/check-howto-coverage.sh ] && { bash scripts/check-howto-coverage.sh >/dev/null 2>&1 || bad "scripts/check-howto-coverage.sh"; }
[ -f scripts/check-docs-sync.sh ] && { bash scripts/check-docs-sync.sh >/dev/null 2>&1 || bad "scripts/check-docs-sync.sh"; }
gofmt_touched

[ "$fail" -eq 0 ] || { echo "prestart-lint-docs.sh: FAIL"; exit 1; }
echo "prestart-lint-docs.sh: PASS"
