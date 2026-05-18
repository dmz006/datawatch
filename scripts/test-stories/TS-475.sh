#!/usr/bin/env bash
# TS-475 — 5 locale bundles contain lifecycle_hint_plan key (or lifecycle_hint_approve)
# tags: surface:locale feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-475"
story_preflight "surface:locale feature:automata" || return 0

_story_ts_475() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0
  # lifecycle_hint_plan is required per spec; check for it
  local check_key="lifecycle_hint_plan"
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    python3 -c "import json; d=json.load(open('$f')); assert '$check_key' in d" 2>/dev/null \
      || { ko "$lang.json missing key: $check_key"; missing=1; }
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles have $check_key"
}

RESULT=fail
_story_ts_475
: "${RESULT:=fail}"
unset -f _story_ts_475
