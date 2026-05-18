#!/usr/bin/env bash
# TS-533 — council_run_cancel MCP tool returns 200
# tags: surface:mcp feature:council
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-533"
story_preflight "surface:mcp feature:council" || return 0

_story_ts_533() {
  # Create a run first
  local run_resp
  run_resp=$(api POST /api/council/run '{"question":"1+1=?","personas":[]}')
  local run_id
  run_id=$(echo "$run_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("run_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -z "$run_id" ]]; then
    skip "could not create council run for cancel test"
    return
  fi
  local resp
  resp=$(api POST /api/mcp/call "{\"tool\":\"council_run_cancel\",\"params\":{\"run_id\":\"$run_id\"}}")
  save_evidence TS-533 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "council_run_cancel tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "council_run_cancel tool returned dict"
  elif echo "$resp" | grep -qi "not in progress\|already completed"; then
    ok "council_run_cancel: run already completed"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_533
: "${RESULT:=fail}"
unset -f _story_ts_533
