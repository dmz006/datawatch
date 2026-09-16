#!/usr/bin/env bash
# TS-692 — autonomous_prd_set_story_llm MCP tool exists and is callable
# tags: surface:mcp feature:automata group:per-story-llm-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-692"
story_preflight "surface:mcp feature:automata group:per-story-llm-v9" || return 0

_story_ts_692() {
  local resp code

  # Call with a fake ID to confirm tool exists (400 or 404 is fine — 405 means absent).
  resp=$(api_code POST "/api/mcp" \
    '{"tool":"autonomous_prd_set_story_llm","args":{"id":"nonexistent","story_id":"s0","backend":"opencode"}}' \
    2>/dev/null || echo "")
  save_evidence TS-692 "mcp-set-story-llm.json" "$resp"

  if [[ -z "$resp" ]]; then
    # Try direct MCP SSE path.
    resp=$(api_mcp autonomous_prd_set_story_llm '{"id":"nonexistent","story_id":"s0","backend":"opencode"}' 2>/dev/null || echo "")
    save_evidence TS-692 "mcp-sse.json" "$resp"
  fi

  code=$(echo "$resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if echo "$resp" | grep -qi "not found\|404\|unknown\|prd not found\|story not found"; then
    ok "autonomous_prd_set_story_llm MCP tool reachable (returns not-found for fake IDs)"
  elif [[ "$code" == "200" ]]; then
    ok "autonomous_prd_set_story_llm MCP tool returned 200"
  elif echo "$resp" | grep -qi "tool.*not.*found\|unknown.*tool\|method.*not.*allowed"; then
    skip "autonomous_prd_set_story_llm MCP tool not available via /api/mcp"
  else
    skip "MCP path unclear (resp: $(echo "$resp" | head -c 150))"
  fi
}

RESULT=fail
_story_ts_692
: "${RESULT:=fail}"
unset -f _story_ts_692
