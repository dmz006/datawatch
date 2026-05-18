#!/usr/bin/env bash
# TS-447 — 5 locale bundles contain session_llm_ref_title and session_compute_ref_title keys
# tags: surface:locale feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-447"
story_preflight "surface:locale feature:sessions" || return 0

_story_ts_447() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    for key in "session_llm_ref_title" "session_compute_ref_title"; do
      python3 -c "import json; d=json.load(open('$f')); assert '$key' in d" 2>/dev/null \
        || { ko "$lang.json missing key: $key"; missing=1; }
    done
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles have session_llm_ref_title and session_compute_ref_title"
}

RESULT=fail
_story_ts_447
: "${RESULT:=fail}"
unset -f _story_ts_447
