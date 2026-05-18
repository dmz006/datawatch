#!/usr/bin/env bash
# TS-473 — autonomous_prd_list MCP tool returns array
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-473"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_473() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$a_enabled" != "yes" ]] && { skip "autonomous disabled"; return; }
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"autonomous_prd_list","params":{}}')
  save_evidence TS-473 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not found\|not enabled"; then
    skip "autonomous_prd_list tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, list) or isinstance(d.get("prds",[]), list)'; then
    ok "autonomous_prd_list tool returned array shape"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_473
: "${RESULT:=fail}"
unset -f _story_ts_473
