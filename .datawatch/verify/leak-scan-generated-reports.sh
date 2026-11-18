#!/usr/bin/env bash
# Verification: the generated-reports leak scan and its test exist and pass.
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
    internal/genartifacts/*|internal/genartifacts/*/*|scripts/check-generated-reports-leaks.sh|scripts/check-generated-reports-leaks_test.sh|Makefile|docs/*.md|docs/*/*.md|internal/server/web/docs/*|internal/server/web/docs/*/*) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

[ -f scripts/check-generated-reports-leaks.sh ] || bad "scripts/check-generated-reports-leaks.sh is missing"
[ -f scripts/check-generated-reports-leaks_test.sh ] || bad "scripts/check-generated-reports-leaks_test.sh is missing"
bash -n scripts/check-generated-reports-leaks.sh || bad "bash -n check-generated-reports-leaks.sh"
bash scripts/check-generated-reports-leaks_test.sh || bad "check-generated-reports-leaks_test.sh"
grep -q 'check-generated-reports-leaks' Makefile || bad "Makefile does not wire check-generated-reports-leaks"

[ "$fail" -eq 0 ] || { echo "leak-scan-generated-reports.sh: FAIL"; exit 1; }
echo "leak-scan-generated-reports.sh: PASS"
