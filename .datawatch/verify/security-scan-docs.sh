#!/usr/bin/env bash
# Verification: the security scan docs carry the new setting, the warn-only note and the task field.
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
has() { grep -Eq -- "$2" "$1" 2>/dev/null || bad "$1 lacks: $2"; }
BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"; trap 'rm -f "$CH"' EXIT
{ git diff --name-only "$BASE"; git ls-files --others --exclude-standard; } | sort -u > "$CH"
allowed="docs/config-reference.yaml
docs/api/autonomous.md
internal/server/web/docs/config-reference.yaml
internal/server/web/docs/api/autonomous.md
.datawatch/verify/security-scan-docs.sh"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|docs/plans/drafts/*|docs/plans/parity-issues/*|docs/plans/reports/*) ;;
    *) grep -Fxq -- "$f" <<< "$allowed" || bad "file outside the allowed set: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

make sync-docs >/dev/null 2>&1 || bad "make sync-docs"
has docs/config-reference.yaml 'security_scan_extensions'
has docs/config-reference.yaml '[Ww]arn-only'
has docs/api/autonomous.md 'security_scan_extensions'
has docs/api/autonomous.md 'security_finding_count'
has internal/server/web/docs/config-reference.yaml 'security_scan_extensions'
has internal/server/web/docs/api/autonomous.md 'security_scan_extensions'
if git diff "$BASE" -- docs/config-reference.yaml docs/api/autonomous.md | grep -E '^\+' | grep -Eq '\bPRD\b'; then bad "added lines use the banned plan acronym (say Automaton)"; fi

[ "$fail" -eq 0 ] || { echo "security-scan-docs.sh: FAIL"; exit 1; }
echo "security-scan-docs.sh: PASS"
