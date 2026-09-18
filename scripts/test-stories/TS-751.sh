#!/usr/bin/env bash
# TS-751 — goose session: web-search-guidance SKILL.md injected into projectDir
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-751"

story_preflight "surface:api feature:mcp-search" || exit 0

# Check web_search is enabled in sandbox
ws_enabled=$(api GET /api/stats | python3 -c 'import json,sys; d=json.load(sys.stdin); print("yes" if d.get("web_search_enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$ws_enabled" != "yes" ]]; then
  skip "web_search.enabled=false — skipping goose inject test"
  exit 0
fi

proj_dir="/tmp/ts-751-goose-$$"
mkdir -p "$proj_dir"

# Create a goose session
resp=$(api POST /api/sessions/start \
  "{\"task\":\"ts-751 goose web-search inject\",\"backend\":\"goose\",\"project_dir\":\"${proj_dir}\",\"actor\":\"ts-751\"}")
sid=$(echo "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id",""))' 2>/dev/null)

if [[ -z "$sid" ]]; then
  ko "could not create goose session: $resp"
  exit 0
fi

add_cleanup sess "$sid"
sleep 2

# Check .mcp.json exists (goose uses .mcp.json too)
mcp_json="$proj_dir/.mcp.json"
servers=""
if [[ -f "$mcp_json" ]]; then
  servers=$(python3 -c "import json; d=json.load(open('$mcp_json')); print(','.join(d.get('mcpServers',{}).keys()))" 2>/dev/null)
fi

# Check web-search-guidance SKILL.md injection
skill_file="$proj_dir/.datawatch/skills/web-search-guidance/SKILL.md"
has_skill="no"
[[ -f "$skill_file" ]] && has_skill="yes"

if [[ "$has_skill" != "yes" && ! "$servers" =~ web_search ]]; then
  ko "goose session $sid: neither .mcp.json web_search nor web-search-guidance SKILL.md found"
  exit 0
fi

if [[ "$has_skill" == "yes" ]]; then
  skill_lines=$(wc -l < "$skill_file" 2>/dev/null || echo 0)
  ok "goose session $sid: web-search-guidance SKILL.md injected (${skill_lines} lines)"
  save_evidence "$CURRENT_STORY" "skill_md.txt" "$(cat "$skill_file")"
elif [[ "$servers" =~ web_search ]]; then
  ok "goose session $sid: .mcp.json has web_search entry (servers=$servers)"
  save_evidence "$CURRENT_STORY" "mcp_json.json" "$(cat "$mcp_json")"
fi

rm -rf "$proj_dir"
