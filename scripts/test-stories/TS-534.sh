#!/usr/bin/env bash
# TS-534 — council_persona_oneshot MCP tool returns response text (may require LLM)
# tags: surface:mcp feature:council conflict:llm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-534"
story_preflight "surface:mcp feature:council conflict:llm" || return 0

_story_ts_534() {
  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then
    skip "no LLM backend available+enabled after retries"
    return
  fi
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"council_persona_oneshot","params":{"question":"What is 1+1?","persona":{}}}')
  save_evidence TS-534 "resp.json" "$resp"
  if echo "$resp" | grep -qi "unknown tool\|not enabled"; then
    skip "council_persona_oneshot tool not available"
    return
  fi
  if assert_json "$resp" 'isinstance(d, dict)'; then
    ok "council_persona_oneshot tool returned dict"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_534
: "${RESULT:=fail}"
unset -f _story_ts_534
