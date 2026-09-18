#!/usr/bin/env bash
# TS-752 — claude-code session: web_search in .mcp.json + SKILL.md injected
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-752"

story_preflight "surface:api feature:mcp-search" || exit 0

ws_enabled=$(api GET /api/stats | python3 -c 'import json,sys; d=json.load(sys.stdin); print("yes" if d.get("web_search_enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$ws_enabled" != "yes" ]]; then
  skip "web_search.enabled=false — skipping claude-code inject test"
  exit 0
fi

proj_dir="/tmp/ts-752-ccod-$$"
mkdir -p "$proj_dir"

resp=$(api POST /api/sessions/start \
  "{\"task\":\"ts-752 claude-code web-search inject\",\"backend\":\"claude-code\",\"project_dir\":\"${proj_dir}\",\"actor\":\"ts-752\"}")
sid=$(echo "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id",""))' 2>/dev/null)

if [[ -z "$sid" ]]; then
  ko "could not create claude-code session: $resp"
  exit 0
fi

add_cleanup sess "$sid"
sleep 2

# claude-code: .mcp.json has web_search (but NOT datawatch, unlike goose/opencode)
mcp_json="$proj_dir/.mcp.json"
if [[ ! -f "$mcp_json" ]]; then
  ko "claude-code session $sid: no .mcp.json at $mcp_json"
  exit 0
fi

servers=$(python3 -c "import json; d=json.load(open('$mcp_json')); print(','.join(d.get('mcpServers',{}).keys()))" 2>/dev/null)
if ! echo "$servers" | grep -q "web_search"; then
  ko "claude-code session $sid: .mcp.json missing web_search entry (servers=$servers)"
  exit 0
fi

# SKILL.md must also be injected
skill_file="$proj_dir/.datawatch/skills/web-search-guidance/SKILL.md"
if [[ ! -f "$skill_file" ]]; then
  ko "claude-code session $sid: web-search-guidance SKILL.md not written"
  exit 0
fi

skill_lines=$(wc -l < "$skill_file" 2>/dev/null || echo 0)
ok "claude-code session $sid: .mcp.json servers=$servers; SKILL.md injected (${skill_lines} lines)"
save_evidence "$CURRENT_STORY" "mcp_json.json" "$(cat "$mcp_json")"
save_evidence "$CURRENT_STORY" "skill_md.txt" "$(cat "$skill_file")"
rm -rf "$proj_dir"
