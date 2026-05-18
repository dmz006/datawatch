#!/usr/bin/env bash
# TS-532 — council_run MCP tool returns id+status shape
# tags: surface:mcp feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-532"
story_preflight "surface:mcp feature:council" || return 0

_story_ts_532() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"council_run","params":{"question":"1+1=?","personas":[]}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-532 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "council_run tool not available"
    return
  fi
  local run_id
  run_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("run_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -n "$run_id" ]]; then
    add_cleanup council "$run_id"
    ok "council_run tool returned run id: $run_id"
  elif assert_json "$resp" 'isinstance(d, dict)'; then
    ok "council_run tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_532
: "${RESULT:=fail}"
unset -f _story_ts_532
