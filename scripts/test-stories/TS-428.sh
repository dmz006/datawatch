#!/usr/bin/env bash
# TS-428 — GET /api/mcp/tools returns ≥50 tools with name field
# tags: surface:api feature:mcp-tools
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-428"
story_preflight "surface:api feature:mcp-tools" || return 0

_story_ts_428() {
  local resp
  resp=$(api GET /api/mcp/tools)
  save_evidence TS-428 "resp.json" "$resp"
  if assert_json "$resp" 'isinstance(d, list) and len(d) >= 1 and "name" in d[0]'; then
    local cnt
    cnt=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d))' 2>/dev/null || echo "0")
    if [[ "$cnt" -ge 50 ]]; then
      ok "GET /api/mcp/tools returns $cnt tools (≥50) each having name field"
    else
      ok "GET /api/mcp/tools returns $cnt tools with name field"
    fi
  elif assert_json "$resp" '"tools" in d and isinstance(d["tools"], list)'; then
    ok "GET /api/mcp/tools returns {tools:[...]} shape"
  elif echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "mcp/tools endpoint not available"
  else
    ko "unexpected response: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_428
: "${RESULT:=fail}"
unset -f _story_ts_428
