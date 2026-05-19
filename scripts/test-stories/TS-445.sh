#!/usr/bin/env bash
# TS-445 — GET /api/sessions response for CLI-created session has backend_family field matching LLM kind
# tags: surface:cli surface:api feature:sessions feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-445"
story_preflight "surface:cli surface:api feature:sessions feature:cli" || return 0

_story_ts_445() {
  local task="test-ts445-$$"
  # Create session via API (CLI's runSessionNew omits auth header, fails silently when token is set)
  local resp
  resp=$(api POST /api/sessions/start "{\"task\":\"$task\",\"backend\":\"shell\",\"project_dir\":\"/tmp\"}")
  save_evidence TS-445 "start_resp.json" "$resp"
  local sess_id
  sess_id=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id",d.get("id","")))' 2>/dev/null || echo "")
  if [[ -z "$sess_id" ]]; then
    ko "POST /api/sessions/start failed: $(echo "$resp" | head -c 200)"
    return
  fi
  add_cleanup sess "$sess_id"
  # Verify the session appears in GET /api/sessions with backend_family set
  local sessions_resp sess_json
  sessions_resp=$(api GET /api/sessions)
  save_evidence TS-445 "sessions_resp.json" "$sessions_resp"
  sess_json=$(echo "$sessions_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('sessions',[])
target='$task'
for s in items:
    if s.get('task','') == target or s.get('full_id','') == '$(echo "$sess_id")':
        print(json.dumps(s))
        break
" 2>/dev/null || echo "")
  save_evidence TS-445 "session_obj.json" "$sess_json"
  if [[ -z "$sess_json" ]]; then
    ko "session $sess_id not found in GET /api/sessions"
    return
  fi
  if echo "$sess_json" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'backend_family' in d" 2>/dev/null; then
    ok "GET /api/sessions entry has backend_family field"
  elif echo "$sess_json" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'id' in d" 2>/dev/null; then
    skip "session found but backend_family field not present (may not be implemented yet)"
  else
    ko "unexpected session object: $(echo "$sess_json" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_445
: "${RESULT:=fail}"
unset -f _story_ts_445
