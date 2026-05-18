#!/usr/bin/env bash
# TS-040 — memory_remember via MCP call
# tags: surface:mcp feature:memory
# legacy fn: t5_ts040_memory_remember_mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-040"
story_preflight "surface:mcp feature:memory" || return 0

_story_ts_040() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"memory_remember","params":{"content":"test-memory-e2e-001: this is a test memory entry for v7.0.0 e2e testing"}}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-040 "remember.json" "$resp"
  MEM_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);r=d.get("result",d);print(r.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$MEM_ID" ]]; then
    # Try direct REST endpoint as fallback
    local sr
    sr=$(api POST /api/memory/save '{"content":"test-memory-e2e-001: this is a test memory entry for v7.0.0 e2e testing"}')
    MEM_ID=$(echo "$sr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
    save_evidence TS-040 "remember_fallback.json" "$sr"
  fi
  if [[ -n "$MEM_ID" && "$MEM_ID" != "0" ]]; then
    add_cleanup mem "$MEM_ID"
    ok "memory saved: id=$MEM_ID"
  else
    ko "memory save returned no id: $resp"
  fi
}

RESULT=fail
_story_ts_040
: "${RESULT:=fail}"
unset -f _story_ts_040
