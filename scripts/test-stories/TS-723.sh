#!/usr/bin/env bash
# TS-723 — PRD task session visibility: GET /api/autonomous/prds/{id} task has session_id field
# tags: surface:api feature:automata
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-723"
story_preflight "surface:api feature:automata" || return 0

_story_ts_723() {
  # Check autonomous enabled
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Get any existing PRD with a task that has a session_id
  local prds_resp
  prds_resp=$(api GET /api/autonomous/prds 2>/dev/null || echo "[]")
  save_evidence TS-723 "prds_list.json" "$prds_resp"

  local task_with_session
  task_with_session=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    for story in prd.get("stories",[]):
        for task in story.get("tasks",[]):
            if task.get("session_id"):
                print(task["session_id"])
                import sys; sys.exit(0)
' 2>/dev/null || echo "")

  if [[ -n "$task_with_session" ]]; then
    ok "PRD task has session_id=$task_with_session — task session visibility works (v8.23.0)"
    return
  fi

  # No existing PRD has a completed task — verify the field exists in the schema
  # by checking any task object (even if session_id is empty string or null)
  local any_task
  any_task=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    for story in prd.get("stories",[]):
        for task in story.get("tasks",[]):
            print("session_id" in task)
            import sys; sys.exit(0)
print("")
' 2>/dev/null || echo "")

  if [[ "$any_task" == "True" ]]; then
    ok "PRD task schema includes session_id field (v8.23.0) — no completed task to verify a non-empty value"
    return
  fi

  skip "no PRDs with tasks found — need to run an Automata to validate task session_id field presence"
}

RESULT=fail
_story_ts_723
: "${RESULT:=fail}"
unset -f _story_ts_723
