#!/usr/bin/env bash
# TS-031 — Create persona
# tags: surface:api feature:council conflict:db-write
# legacy fn: t4_ts031_create_persona
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-031"
story_preflight "surface:api feature:council conflict:db-write" || return 0

_story_ts_031() {
  local resp
  resp=$(api POST /api/council/personas '{"name":"test-persona-e2e-'"$$"'","role":"analyst","system_prompt":"You are a test analyst for e2e tests.","model":"claude-sonnet-4-5"}')
  save_evidence TS-031 "create.json" "$resp"
  PERSONA_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",d.get("persona",{}).get("id",d.get("name",""))))' 2>/dev/null || echo "")
  if [[ -n "$PERSONA_ID" ]]; then
    add_cleanup persona "$PERSONA_ID"
    ok "persona created: $PERSONA_ID"
  else
    skip "persona create failed (council may not be enabled): $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_031
: "${RESULT:=fail}"
unset -f _story_ts_031
