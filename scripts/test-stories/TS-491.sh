#!/usr/bin/env bash
# TS-491 — 5 locale bundles contain llm_field_enabled_models key
# tags: surface:locale feature:llm-registry
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-491"
story_preflight "surface:locale feature:llm-registry" || return 0

_story_ts_491() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    python3 -c "import json; d=json.load(open('$f')); assert 'llm_field_enabled_models' in d" 2>/dev/null \
      || { ko "$lang.json missing key: llm_field_enabled_models"; missing=1; }
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles have llm_field_enabled_models"
}

RESULT=fail
_story_ts_491
: "${RESULT:=fail}"
unset -f _story_ts_491
