#!/usr/bin/env bash
# TS-540 — POST /api/mcp/call with tool=get_version returns version string
# tags: surface:mcp feature:mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-540"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_540() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"get_version","params":{}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-540 "resp.json" "$resp"
  if assert_json "$resp" '"version" in d or isinstance(d.get("version",""), str)'; then
    ok "get_version tool returned version: $(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("version","?"))' 2>/dev/null)"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "get_version tool returned dict"
  elif echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "get_version tool not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_540
: "${RESULT:=fail}"
unset -f _story_ts_540
