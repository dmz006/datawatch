#!/usr/bin/env bash
# TS-515 — 5 locale bundles contain push_topic_alerts key
# tags: surface:locale feature:push
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-515"
story_preflight "surface:locale feature:push" || return 0

_story_ts_515() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    python3 -c "import json; d=json.load(open('$f')); assert 'push_topic_alerts' in d" 2>/dev/null \
      || { ko "$lang.json missing key: push_topic_alerts"; missing=1; }
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles have push_topic_alerts"
}

RESULT=fail
_story_ts_515
: "${RESULT:=fail}"
unset -f _story_ts_515
