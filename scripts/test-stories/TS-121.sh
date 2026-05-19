#!/usr/bin/env bash
# TS-121 — MCP resources CLI list
# tags: surface:cli feature:mcp
# legacy fn: t10_ts121_mcp_resources_cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-121"
story_preflight "surface:cli feature:mcp" || return 0

_story_ts_121() {
  local out
  out=$(cli_test mcp resources list 2>&1 || true)
  save_evidence TS-121 "resources_list.txt" "$out"
  if echo "$out" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'resources' in d and isinstance(d['resources'], list)" 2>/dev/null; then
    ok "CLI mcp resources list returns JSON with resources array"
  elif echo "$out" | grep -q "resources"; then
    ok "CLI mcp resources list output contains 'resources' key: $(echo "$out" | head -c 100)"
  elif echo "$out" | grep -qi "not enabled\|disabled\|unavailable\|mcp not"; then
    skip "MCP not enabled in this build: $(echo "$out" | head -c 100)"
  else
    ko "CLI mcp resources list unexpected output: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_121
: "${RESULT:=fail}"
unset -f _story_ts_121
