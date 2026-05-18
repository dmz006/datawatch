#!/usr/bin/env bash
# TS-137 — Settings panel config round-trip
# tags: surface:pwa feature:config conflict:pwa
# legacy fn: t11_ts137_settings_panel
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-137"
story_preflight "surface:pwa feature:config conflict:pwa" || return 0

_story_ts_137() {
  local resp
  resp=$(api GET /api/config)
  save_evidence TS-137 "config.json" "$resp"
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "config endpoint returns data"
  else
    ko "config endpoint failed: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_137
: "${RESULT:=fail}"
unset -f _story_ts_137
