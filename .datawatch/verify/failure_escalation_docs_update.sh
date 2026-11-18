#!/usr/bin/env bash
# Verification: the failure escalation docs describe the sonnet default and the back-off skip, links resolve, docs rules hold.
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
    internal/autonomous/*.go|internal/config/*.go|internal/server/*.go|internal/server/web/docs/*|internal/server/web/docs/*/*|docs/*|docs/*/*|scripts/check-*.sh) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
for s in scripts/check-docs-sync.sh scripts/check-curated-howtos.sh scripts/check-howto-coverage.sh; do
  [ -f "$s" ] || continue
  bash "$s" >/dev/null 2>&1 || bad "$s"
done
git diff "$BASE" -- docs | grep -E '^\+' | grep -qi 'sonnet' || bad "no added docs line names the sonnet default"
if git diff "$BASE" -- docs | grep -E '^\+' | grep -Eq '\bPRDs?\b'; then bad "added docs lines use the banned plan acronym (say Automaton)"; fi

[ "$fail" -eq 0 ] || { echo "failure_escalation_docs_update.sh: FAIL"; exit 1; }
echo "failure_escalation_docs_update.sh: PASS"
