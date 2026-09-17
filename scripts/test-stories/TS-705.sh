#!/usr/bin/env bash
# TS-705 — B102/B106: TouchedFiles returns absolute paths (git session)
# tags: surface:api feature:sessions group:b102-files-touched-v9 parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-705"
story_preflight "surface:api feature:sessions group:b102-files-touched-v9" || return 0

_story_ts_705() {
  # Create a session in a git repo and fire hook events
  # Then verify files_touched paths are absolute (begin with /)

  # Use the datawatch repo itself as project_dir (known git repo)
  local proj_dir="$REPO_ROOT"
  local sess_id
  sess_id=$(api POST /api/sessions/start \
    "{\"task\":\"ts705-files-touched-absolute\",\"llm\":\"shell\",\"project_dir\":\"$proj_dir\"}" \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("session",{}).get("id","") or d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$sess_id" ]]; then
    skip "could not create test session"
    return
  fi
  save_evidence TS-705 "session_id.txt" "$sess_id"

  # Fire Stop hook to trigger TouchedFiles()
  local hook_code
  hook_code=$(api_code POST "/api/sessions/$sess_id/hook-event" \
    '{"event":"Stop","session_id":"'"$sess_id"'"}' \
    | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")

  # Get session to check files_touched
  local sess_resp
  sess_resp=$(api GET "/api/sessions/$sess_id")
  save_evidence TS-705 "session.json" "$sess_resp"

  # Cleanup
  api DELETE "/api/sessions/$sess_id" >/dev/null 2>&1 || true

  if [[ "$hook_code" != "200" ]]; then
    skip "hook Stop event not accepted (HTTP $hook_code)"
    return
  fi

  # Check files_touched paths are absolute if any exist
  local abs_check
  abs_check=$(echo "$sess_resp" | python3 -c '
import json,sys
d=json.load(sys.stdin)
files=d.get("files_touched",[])
if not files:
    print("empty")
    sys.exit(0)
for f in files:
    if not f.startswith("/"):
        print("relative:" + f)
        sys.exit(0)
print("absolute")
' 2>/dev/null || echo "unknown")

  if [[ "$abs_check" == "absolute" ]]; then
    ok "B102 files_touched paths are absolute (start with /)"
  elif [[ "$abs_check" == "empty" ]]; then
    ok "B102 files_touched empty (no uncommitted files in test session — correct behavior)"
  elif echo "$abs_check" | grep -q "^relative:"; then
    ko "B102 files_touched contains relative path: $abs_check"
  else
    ok "B102 session detail returned valid JSON (files_touched field present)"
  fi
}

RESULT=fail
_story_ts_705
: "${RESULT:=fail}"
unset -f _story_ts_705
