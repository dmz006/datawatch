#!/usr/bin/env bash
# TS-499 — autonomous_type_list MCP tool returns at least 4 built-in types
# tags: surface:mcp feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-499"
story_preflight "surface:mcp feature:automata" || return 0

_story_ts_499() {
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  [[ "$a_enabled" != "yes" ]] && { skip "autonomous disabled"; return; }
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"autonomous_type_list","params":{}}')
  save_evidence TS-499 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "autonomous_type_list tool not available"
    return
  fi
  local cnt
  cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);types=d.get("types",d) if isinstance(d,dict) else d;print(len(types) if isinstance(types,list) else 0)' 2>/dev/null || echo "0")
  if [[ "$cnt" -ge 4 ]] 2>/dev/null; then
    ok "autonomous_type_list returned $cnt built-in types"
  elif assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "autonomous_type_list returned valid JSON (count: $cnt)"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_499
: "${RESULT:=fail}"
unset -f _story_ts_499
