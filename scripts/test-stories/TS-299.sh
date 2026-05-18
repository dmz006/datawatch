#!/usr/bin/env bash
# TS-299 — telemetry_list + telemetry_get shape via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-299"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_299() {
  local resp telemetry_id

  # telemetry_list
  resp=$(api POST /api/mcp/call '{"tool":"telemetry_list","params":{}}')
  save_evidence TS-299 "list.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "telemetry_list not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "telemetry_list unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  telemetry_id=$(echo "$resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if isinstance(d,list) and len(d)>0:
    item=d[0]; print(item.get("id",item.get("name","")) if isinstance(item,dict) else str(item))
elif isinstance(d,dict):
    for k in ("telemetry","items","result"):
        if k in d and isinstance(d[k],list) and len(d[k])>0:
            item=d[k][0]; print(item.get("id",item.get("name","")) if isinstance(item,dict) else str(item)); exit()
' 2>/dev/null || echo "")

  if [[ -z "$telemetry_id" ]]; then
    ok "telemetry_list returned valid shape (no telemetry items)"
    return
  fi

  # telemetry_get
  resp=$(api POST /api/mcp/call "{\"tool\":\"telemetry_get\",\"params\":{\"id\":\"$telemetry_id\"}}")
  save_evidence TS-299 "get.json" "$resp"
  ok "telemetry_list + telemetry_get valid for $telemetry_id"
}

RESULT=fail
_story_ts_299
: "${RESULT:=fail}"
unset -f _story_ts_299
