#!/usr/bin/env bash
# TS-158 — Agent lifecycle
# tags: surface:api feature:agents
# legacy fn: t12_ts158_agent_lifecycle
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-158"
story_preflight "surface:api feature:agents" || return 0

_story_ts_158() {
  local resp
  resp=$(api GET /api/agents)
  save_evidence TS-158 "list.json" "$resp"
  if assert_json "$resp" '"agents" in d and isinstance(d["agents"], list)'; then
    ok "GET /api/agents returns {agents:[]} shape"
  else
    ko "agents list unexpected: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_158
: "${RESULT:=fail}"
unset -f _story_ts_158
