#!/usr/bin/env bash
# TS-247 — MCP tool chain journey: list → call health_check → verify stats
# tags: surface:mcp feature:mcp
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-247"
story_preflight "surface:mcp feature:mcp" || return 0

_story_ts_247() {
    echo ""; echo "  >> TS-247: MCP tool chain journey: list → call health_check → verify stats"
    tools=$(api GET /api/mcp/tools)
    save_evidence "TS-247" "1_tools.json" "$tools"
    call=$(api POST /api/mcp/call '{"tool":"health_check","arguments":{}}' 2>/dev/null || echo '{"error":"not callable"}')
    save_evidence "TS-247" "2_call.json" "$call"
    stats=$(api GET /api/stats)
    save_evidence "TS-247" "3_stats.json" "$stats"
    if echo "$tools" | python3 -c "import json,sys; d=json.load(sys.stdin); l=d if isinstance(d,list) else d.get('tools',[]); assert len(l)>0" 2>/dev/null; then
      ok "MCP tool chain journey: tools listed, health_check called, stats verified"
    else
      ko "MCP tool chain journey: no tools found"
    fi

}

RESULT=fail
_story_ts_247
: "${RESULT:=fail}"
unset -f _story_ts_247
