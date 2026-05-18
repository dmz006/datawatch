#!/usr/bin/env bash
# TS-060 — List plugins
# tags: surface:api feature:plugins
# legacy fn: t7_ts060_list_plugins
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-060"
story_preflight "surface:api feature:plugins" || return 0

_story_ts_060() {
  local resp
  resp=$(api GET /api/plugins)
  save_evidence TS-060 "plugins.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/plugins returns valid shape"
  else
    ko "plugins list unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_060
: "${RESULT:=fail}"
unset -f _story_ts_060
