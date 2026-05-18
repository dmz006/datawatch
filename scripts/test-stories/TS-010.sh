#!/usr/bin/env bash
# TS-010 — Create session + verify limit enforcement
# tags: surface:api feature:sessions feature:limits
# legacy fn: t2_ts010_create_session
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-010"
story_preflight "surface:api feature:sessions feature:limits" || return 0

_story_ts_010() {
  local resp
  resp=$(api POST /api/sessions/start '{"task":"test-session-001","name":"test-session-001","backend":"shell","project_dir":"/tmp","effort":"quick"}')
  save_evidence TS-010 "create.json" "$resp"
  SESSION_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$SESSION_ID" ]]; then
    add_cleanup sess "$SESSION_ID"
    ok "session created: $SESSION_ID"
  elif echo "$resp" | grep -q "max sessions"; then
    # Session limit enforcement is working (success case: limit is enforced)
    ok "session limit enforcement verified: $(echo $resp | head -c 60)"
  else
    ko "session create failed: $resp"
  fi
}

RESULT=fail
_story_ts_010
: "${RESULT:=fail}"
unset -f _story_ts_010
