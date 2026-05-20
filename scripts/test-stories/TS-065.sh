#!/usr/bin/env bash
# TS-065 — Skill invoke via MCP
# tags: surface:mcp feature:skills
# legacy fn: t7_ts065_skill_invoke
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-065"
story_preflight "surface:mcp feature:skills" || return 0

_story_ts_065() {
  local resp
  resp=$(api GET /api/skills)
  save_evidence TS-065 "skills.json" "$resp"
  local first_skill
  first_skill=$(echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('skills',[])
print(arr[0].get('name','') if arr else '')
" 2>/dev/null || echo "")
  if [[ -z "$first_skill" ]]; then
    skip "No skills synced — invoke test skipped (pre-seeded skill not found; daemon may have started before seeding)"
    return
  fi
  # Load the skill (non-destructive read)
  local load_resp
  load_resp=$(api POST /api/mcp/call "{\"tool\":\"skill_load\",\"params\":{\"name\":\"$first_skill\"}}" 2>/dev/null || echo '{}')
  load_resp=$(mcp_unwrap "$load_resp")
  save_evidence TS-065 "skill_load.json" "$load_resp"
  if assert_json "$load_resp" 'isinstance(d, (dict, str)) and bool(d)'; then
    ok "Skill $first_skill: skill_load MCP call returned content"
  else
    ko "Skill invoke failed: $(echo "$load_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_065
: "${RESULT:=fail}"
unset -f _story_ts_065
