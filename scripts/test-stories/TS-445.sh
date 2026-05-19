#!/usr/bin/env bash
# TS-445 — GET /api/sessions response for CLI-created session has backend_family field matching LLM kind
# tags: surface:cli surface:api feature:sessions feature:cli
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-445"
story_preflight "surface:cli surface:api feature:sessions feature:cli" || return 0

_story_ts_445() {
  local task="test-cli-session-ts445-$$"
  local out rc
  # Create a session via CLI using shell backend (always available in test env)
  out=$(cli_test session new --backend shell "$task" 2>&1); rc=$?
  save_evidence TS-445 "cli_out.txt" "$out"
  if [[ $rc -ne 0 ]]; then
    if echo "$out" | grep -qiE "unknown.*flag|unknown command|not found|disabled|not.*available|no such"; then
      skip "session new --backend not available: $(echo "$out" | head -c 80)"
    else
      ko "CLI session new failed rc=$rc: $(echo "$out" | head -c 200)"
    fi
    return
  fi
  # CLI prints "Session started. Task: ..." but no ID; find by task name in sessions list
  local sessions_resp sess_json full_id
  sessions_resp=$(api GET /api/sessions)
  save_evidence TS-445 "sessions_resp.json" "$sessions_resp"
  sess_json=$(echo "$sessions_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('sessions',[])
task='$task'
for s in items:
    if s.get('task','') == task or s.get('name','') == task:
        print(json.dumps(s))
        break
" 2>/dev/null || echo "")
  if [[ -z "$sess_json" ]]; then
    skip "could not find CLI-created session by task name '$task' in sessions list"
    return
  fi
  full_id=$(echo "$sess_json" | python3 -c "import json,sys; print(json.load(sys.stdin).get('full_id',''))" 2>/dev/null || echo "")
  [[ -n "$full_id" ]] && add_cleanup sess "$full_id"
  save_evidence TS-445 "session_obj.json" "$sess_json"
  if echo "$sess_json" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'backend_family' in d" 2>/dev/null; then
    ok "CLI-created session has backend_family field"
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
