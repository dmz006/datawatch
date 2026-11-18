#!/usr/bin/env bash
# Verification: the rollup strings exist in all five locale files with the same key set, and the locale guard passes.
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
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|CHANGELOG.md|AGENT.md|CLAUDE.md|docs/plans/*) ;;
    internal/server/web/app.js|internal/server/web/style.css|internal/server/web/locales/*.json|internal/server/v5280_locales_test.go|internal/server/web/app-session-detail-rollup.test.js|docs/howto/sessions-deep-dive.md|internal/server/web/docs/howto/sessions-deep-dive.md) ;;
    *) bad "file outside the allowed surfaces: $f" ;;
  esac
done < "$CH"

python3 - <<'PY' || bad "locale key sets differ or the rollup keys are missing"
import json,sys,glob
files=sorted(glob.glob('internal/server/web/locales/*.json'))
sets={}
for f in files:
    d=json.load(open(f)); sets[f]={k for k in d if k.startswith('session_detail_')}
base=sets[files[0]]
assert any(k.startswith('session_detail_tags') for k in base), 'no session_detail_tags key'
for f,s in sets.items():
    assert s==base, f
for f in files:
    d=json.load(open(f)); assert d.get('session_detail_tags_summary'), f+' lacks session_detail_tags_summary'
PY
go test ./internal/server/ -count=1 -run 'TestLocales_CommonNavKeysPresent' || bad "locale guard test"

[ "$fail" -eq 0 ] || { echo "session-detail-rollup-locales.sh: FAIL"; exit 1; }
echo "session-detail-rollup-locales.sh: PASS"
