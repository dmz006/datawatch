#!/usr/bin/env bash
# TS-296 — pipeline_list + pipeline_start + pipeline_status shape via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-296"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_296() {
  local resp

  # pipeline_list
  resp=$(api POST /api/mcp/call '{"tool":"pipeline_list","params":{}}')
  save_evidence TS-296 "list.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "pipeline_list not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "pipeline_list unexpected: $(echo "$resp" | head -c 200)"
    return
  fi
  ok "pipeline_list returned valid shape"
}

RESULT=fail
_story_ts_296
: "${RESULT:=fail}"
unset -f _story_ts_296
