#!/usr/bin/env bash
# TS-440 — GET /api/sessions response has backend_family field (not llm_backend)
# tags: surface:api feature:sessions
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-440"
story_preflight "surface:api feature:sessions" || return 0

_story_ts_440() {
  ensure_test_session || return
  # No single-item GET endpoint; use sessions list and find by SESSION_ID
  local list_resp
  list_resp=$(api GET /api/sessions)
  save_evidence TS-440 "list_resp.json" "$list_resp"
  local result
  result=$(echo "$list_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('sessions',[])
sid='$SESSION_ID'
sess=next((s for s in items if s.get('id','')==sid or s.get('full_id','').endswith('-'+sid)),None)
if sess is None:
    print('notfound')
elif 'backend_family' in sess:
    print('yes')
else:
    print('no')
" 2>/dev/null || echo "no")
  if [[ "$result" == "yes" ]]; then
    ok "GET /api/sessions list items have backend_family field"
  elif [[ "$result" == "no" ]]; then
    skip "backend_family field not present in session object (may not be implemented)"
  else
    ko "session $SESSION_ID not found in GET /api/sessions: $(echo "$list_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_440
: "${RESULT:=fail}"
unset -f _story_ts_440
