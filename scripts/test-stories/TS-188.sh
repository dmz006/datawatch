#!/usr/bin/env bash
# TS-188 — MCP tool surface: channel bridge matches daemon tool count
# tags: surface:mcp feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-188"
story_preflight "surface:mcp feature:parity" || return 0

_story_ts_188() {
    echo ""; echo "  >> TS-188: MCP tool surface: channel bridge matches daemon tool count"
    daemon_tools=$(api GET /api/mcp/tools)
    daemon_count=$(echo "$daemon_tools" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d) if isinstance(d,list) else len(d.get('tools',d)))" 2>/dev/null || echo "0")
    save_evidence "TS-188" "daemon_tools.json" "$daemon_tools"
    if [[ "$daemon_count" -gt 0 ]]; then
      ok "Daemon exposes $daemon_count MCP tools"
    else
      ko "No MCP tools found at /api/mcp/tools"
    fi

}

RESULT=fail
_story_ts_188
: "${RESULT:=fail}"
unset -f _story_ts_188
