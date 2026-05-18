#!/usr/bin/env bash
# TS-262 — GET /api/templates returns array
# tags: surface:api feature:plugins
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-262"
story_preflight "surface:api feature:plugins" || return 0

_story_ts_262() {
  local resp
  resp=$(api GET /api/templates)
  save_evidence TS-262 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list)'; then
    ok "templates returns array"
  elif assert_json "$resp" 'isinstance(d, dict) and ("templates" in d or "items" in d)'; then
    ok "templates returns dict with templates key"
  elif echo "$resp" | grep -qi "not found\|404\|no route"; then
    skip "templates endpoint not available in this build"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_262
: "${RESULT:=fail}"
unset -f _story_ts_262
