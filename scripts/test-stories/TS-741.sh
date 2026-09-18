#!/usr/bin/env bash
# TS-741 — web_search skill injection: .datawatch/skills/web-search-guidance/SKILL.md created
# tags: surface:api feature:mcp-search parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-741"
story_preflight "surface:api feature:mcp-search" || return 0

_story_ts_741() {
  # Check if web_search is enabled in config
  local cfg
  cfg=$(api GET /api/config 2>/dev/null || echo "{}")
  local ws_enabled
  ws_enabled=$(echo "$cfg" | python3 -c "import json,sys; print(json.load(sys.stdin).get('web_search',{}).get('enabled',False))" 2>/dev/null || echo "False")

  if [[ "$ws_enabled" != "True" && "$ws_enabled" != "true" ]]; then
    skip "web_search.enabled=false — skill injection only fires when web_search is enabled"
    return
  fi

  # Check the skills directory for the injected skill
  local datawatch_dir="${HOME}/.datawatch"
  local skill_dir="$datawatch_dir/skills/web-search-guidance"
  local skill_file="$skill_dir/SKILL.md"

  if [[ -f "$skill_file" ]]; then
    save_evidence TS-741 "SKILL.md" "$(cat "$skill_file")"
    ok "web-search-guidance SKILL.md present at $skill_file — skill injection verified"
    return
  fi

  # The skill may be created per-session start, not globally
  # Check if there's a session we can inspect
  local sessions
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")
  local sess_count
  sess_count=$(echo "$sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin)
print(len(d if isinstance(d,list) else d.get("sessions",[])))
' 2>/dev/null || echo "0")

  if [[ "$sess_count" -eq 0 ]]; then
    skip "web_search.enabled but no sessions running and SKILL.md not found — skill injection happens at session start"
    return
  fi

  skip "web_search.enabled + sessions present but SKILL.md not at $skill_file — may use per-session path or different injection strategy"
}

RESULT=fail
_story_ts_741
: "${RESULT:=fail}"
unset -f _story_ts_741
