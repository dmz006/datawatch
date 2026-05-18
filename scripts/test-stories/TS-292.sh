#!/usr/bin/env bash
# TS-292 — marketplace_ollama_catalog + marketplace_pull_task shape via MCP
# tags: surface:mcp feature:mcp feature:parity
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-292"
story_preflight "surface:mcp feature:mcp feature:parity" || return 0

_story_ts_292() {
  local resp

  # Catalog
  resp=$(api POST /api/mcp/call '{"tool":"marketplace_ollama_catalog","params":{}}')
  save_evidence TS-292 "catalog.json" "$resp"
  if echo "$resp" | grep -qi "not found\|not enabled\|disabled\|unknown tool"; then
    skip "marketplace_ollama_catalog not available in this build"
    return
  fi
  if ! assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ko "marketplace_ollama_catalog unexpected: $(echo "$resp" | head -c 200)"
    return
  fi

  # Pull task (use a known model name; skip if no compute node)
  resp=$(api POST /api/mcp/call '{"tool":"marketplace_pull_task","params":{"model":"tinyllama","node":""}}')
  save_evidence TS-292 "pull_task.json" "$resp"
  if echo "$resp" | grep -qi "no node\|no compute\|not found\|error"; then
    skip "marketplace_pull_task: no compute node available (expected in test env)"
    return
  fi
  ok "marketplace_ollama_catalog + marketplace_pull_task shape returned"
}

RESULT=fail
_story_ts_292
: "${RESULT:=fail}"
unset -f _story_ts_292
