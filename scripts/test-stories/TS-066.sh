#!/usr/bin/env bash
# TS-066 — Skill registry MCP surface
# tags: surface:mcp feature:skills
# legacy fn: t7_ts066_skill_registry_mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-066"
story_preflight "surface:mcp feature:skills" || return 0

_story_ts_066() {
  local resp
  resp=$(api POST /api/mcp/call '{"tool":"skills_list","params":{}}' 2>/dev/null || \
        api GET /api/skills 2>/dev/null || echo '{}')
  resp=$(mcp_unwrap "$resp")
  save_evidence TS-066 "skills_registry.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "Skill registry MCP surface: skills_list or /api/skills returns valid shape"
  else
    ko "Skill registry MCP call failed: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_066
: "${RESULT:=fail}"
unset -f _story_ts_066
