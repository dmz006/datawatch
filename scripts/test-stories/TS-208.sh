#!/usr/bin/env bash
# TS-208 — mcp-tools: full tool call chain
# tags: surface:mcp feature:mcp
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-208"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_208() {
    echo ""; echo "  >> TS-208: mcp-tools: full tool call chain"
    resp=$(api GET /api/mcp/tools)
    save_evidence "TS-208" "mcp_tools.json" "$resp"
    count=$(echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); l=d if isinstance(d,list) else d.get('tools',[]); print(len(l))" 2>/dev/null || echo "0")
    if [[ "$count" -gt 0 ]]; then
      ok "MCP tools list returns $count tools"
    else
      ko "MCP tool list empty"
    fi

}

RESULT=fail
_story_ts_208
: "${RESULT:=fail}"
unset -f _story_ts_208
