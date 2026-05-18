#!/usr/bin/env bash
# TS-448 — 5 locale bundles contain new_session_v7_llm_label and new_session_v7_compute_label keys
# tags: surface:locale feature:sessions
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-448"
story_preflight "surface:locale feature:sessions" || return 0

_story_ts_448() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0
  # Check which llm label key exists in en.json
  local llm_key="new_session_v7_llm_label"
  python3 -c "import json; d=json.load(open('$locale_dir/en.json')); assert 'new_session_v7_llm_label' in d" 2>/dev/null \
    || llm_key="new_session_llm_label"
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    for key in "$llm_key" "new_session_v7_compute_label"; do
      python3 -c "import json; d=json.load(open('$f')); assert '$key' in d" 2>/dev/null \
        || { ko "$lang.json missing key: $key"; missing=1; }
    done
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles have $llm_key and new_session_v7_compute_label"
}

RESULT=fail
_story_ts_448
: "${RESULT:=fail}"
unset -f _story_ts_448
