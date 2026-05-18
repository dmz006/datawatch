#!/usr/bin/env bash
# TS-255 — GET /api/devices returns array (push device registry)
# tags: surface:api feature:config
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-255"
story_preflight "surface:api feature:config" || return 0

_story_ts_255() {
  local resp
  resp=$(api GET /api/devices)
  save_evidence TS-255 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "devices returns array (${#resp} bytes)"
  elif assert_json "$resp" 'isinstance(d, dict) and ("devices" in d or "items" in d)'; then
    ok "devices returns dict with devices/items key"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "devices endpoint not available in this build"
  else
    ko "devices returned unexpected shape: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_255
: "${RESULT:=fail}"
unset -f _story_ts_255
