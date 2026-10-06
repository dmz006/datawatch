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

# Verify the web_search entry launches the current `mcp-search` subcommand
# and carries DATAWATCH_SESSION_ID. BL391 (2026-10-02, commit 5d6f1907)
# made `mcp-search` self-load the full multi-provider list (and resolve
# secret: refs) from config.yaml directly — per-field env vars like
# DATAWATCH_WEB_SEARCH_URL were deliberately removed from the injected
# entry (cmd/datawatch/main.go's own comment: "no per-field env vars
# needed now that there can be more than one provider"). This test
# predates that change (written 2026-09-18) and was never updated,
# so it was asserting removed behavior, not a real regression — found
# live via a full E2E run failure.
ws_args=$(python3 -c "
import json
d=json.load(open('$mcp_json'))
ws=d.get('mcpServers',{}).get('web_search',{})
print(','.join(ws.get('args',[])))
" 2>/dev/null)
ws_sid=$(python3 -c "
import json
d=json.load(open('$mcp_json'))
ws=d.get('mcpServers',{}).get('web_search',{})
print(ws.get('env',{}).get('DATAWATCH_SESSION_ID',''))
" 2>/dev/null)

if ! echo "$ws_args" | grep -q "mcp-search"; then
  ko "web_search entry does not launch the mcp-search subcommand: args=$ws_args"
  exit 0
fi
if [[ "$ws_sid" != "$sid" ]]; then
  ko "web_search entry's DATAWATCH_SESSION_ID ($ws_sid) does not match the session ($sid)"
  exit 0
fi

ok "opencode session $sid: .mcp.json has web_search entry (servers=$servers, args=$ws_args, session_id=$ws_sid)"
save_evidence "$CURRENT_STORY" "mcp_json.json" "$(cat "$mcp_json")"
rm -rf "$proj_dir"
