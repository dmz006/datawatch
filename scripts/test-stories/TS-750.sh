#!/usr/bin/env bash
# TS-750 — opencode session: .mcp.json contains web_search entry when web_search.enabled
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-750"

story_preflight "surface:api feature:mcp-search" || exit 0

# Check web_search is enabled in sandbox
ws_enabled=$(api GET /api/stats | python3 -c 'import json,sys; d=json.load(sys.stdin); print("yes" if d.get("web_search_enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$ws_enabled" != "yes" ]]; then
  skip "web_search.enabled=false in sandbox config — skipping injection test"
  exit 0
fi

# Create a unique projectDir for this test
proj_dir="/tmp/ts-750-opencode-$$"
mkdir -p "$proj_dir"

# Start an opencode session (enabled:false in config but API still creates it)
resp=$(api POST /api/sessions/start \
  "{\"task\":\"ts-750 opencode mcp inject\",\"backend\":\"opencode\",\"project_dir\":\"${proj_dir}\",\"actor\":\"ts-750\"}")
sid=$(echo "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id",""))' 2>/dev/null)

if [[ -z "$sid" ]]; then
  ko "could not create opencode session: $resp"
  exit 0
fi

add_cleanup sess "$sid"

# Wait briefly for the session setup to write .mcp.json
sleep 2

# Check .mcp.json in projectDir
mcp_json="$proj_dir/.mcp.json"
if [[ ! -f "$mcp_json" ]]; then
  ko ".mcp.json not written at $mcp_json after opencode session start"
  exit 0
fi

servers=$(python3 -c "import json; d=json.load(open('$mcp_json')); print(','.join(d.get('mcpServers',{}).keys()))" 2>/dev/null)
if ! echo "$servers" | grep -q "web_search"; then
  ko ".mcp.json missing web_search entry: servers=$servers"
  exit 0
fi

# Verify web_search entry has DATAWATCH_WEB_SEARCH_URL set
ws_url=$(python3 -c "
import json
d=json.load(open('$mcp_json'))
ws=d.get('mcpServers',{}).get('web_search',{})
print(ws.get('env',{}).get('DATAWATCH_WEB_SEARCH_URL',''))
" 2>/dev/null)

if [[ -z "$ws_url" ]]; then
  ko "web_search entry missing DATAWATCH_WEB_SEARCH_URL env var"
  exit 0
fi

ok "opencode session $sid: .mcp.json has web_search entry (servers=$servers, url=$ws_url)"
save_evidence "$CURRENT_STORY" "mcp_json.json" "$(cat "$mcp_json")"
rm -rf "$proj_dir"
