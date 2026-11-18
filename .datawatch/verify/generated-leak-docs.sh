#!/usr/bin/env bash
# Verification: the leak-check how-to and operations page describe the generated-reports scan, and the embedded copies match.
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

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
grep -qi 'generated' docs/howto/local-env-leak-check.md || bad "the how-to does not describe the generated-reports scan"
grep -q 'check-generated-reports-leaks' docs/howto/local-env-leak-check.md || bad "the how-to does not name the make target"
for d in howto/local-env-leak-check.md operations.md; do
  diff -q "docs/$d" "internal/server/web/docs/$d" >/dev/null 2>&1 || bad "embedded copy differs: $d"
done
[ -f scripts/check-docs-sync.sh ] && { bash scripts/check-docs-sync.sh >/dev/null 2>&1 || bad "scripts/check-docs-sync.sh"; }

[ "$fail" -eq 0 ] || { echo "generated-leak-docs.sh: FAIL"; exit 1; }
echo "generated-leak-docs.sh: PASS"
