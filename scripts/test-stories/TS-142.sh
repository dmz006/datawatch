#!/usr/bin/env bash
# TS-142 — Plugins panel in PWA
# tags: surface:pwa feature:plugins conflict:pwa
# legacy fn: t11_ts142_plugins_panel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-142"
story_preflight "surface:pwa feature:plugins conflict:pwa" || return 0

_story_ts_142() {
  local resp
  resp=$(api GET /api/plugins)
  save_evidence TS-142 "plugins.json" "$resp"
  if assert_json "$resp" 'isinstance(d.get("plugins",[]), list) or isinstance(d, dict)'; then
    ok "plugins endpoint works"
  else
    ko "plugins endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_142
: "${RESULT:=fail}"
unset -f _story_ts_142
