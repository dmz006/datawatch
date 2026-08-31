#!/usr/bin/env bash
# TS-660 — config_set MCP tool vision.model=moondream updates config
# tags: surface:mcp feature:vision group:vision conflict:selfconfig
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-660"
story_preflight "surface:mcp feature:vision group:vision conflict:selfconfig" || return 0

_story_ts_660() {
  local resp
  resp=$(api_mcp config_set '{"key":"vision.model","value":"moondream"}')
  save_evidence TS-660 "mcp_set.json" "$resp"

  if echo "$resp" | grep -qi "allow_self_config\|not allowed\|permission"; then
    skip "config_set requires allow_self_config; not set in test daemon"
    return
  fi

  local get_resp
  get_resp=$(api GET /api/config)
  if ! echo "$get_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['vision']['model']=='moondream'" 2>/dev/null; then
    ko "vision.model was not updated to moondream via config_set MCP"
    return
  fi

  # Restore
  api PUT /api/config '{"vision.model":"Gemma3:12b"}' >/dev/null 2>&1 || true
  ok "config_set MCP tool vision.model=moondream updated config"
}

RESULT=fail
_story_ts_660
: "${RESULT:=fail}"
unset -f _story_ts_660
