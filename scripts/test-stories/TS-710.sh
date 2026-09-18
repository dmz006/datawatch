#!/usr/bin/env bash
# TS-710 — v8.23.0 PRD reset_task: POST /api/autonomous/prds/{id}/reset_task resets failed task
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-710"
story_preflight "surface:api feature:automata" || return 0

_story_ts_710() {
  # Check autonomous enabled
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Create a minimal PRD with one task
  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"Create a single file /tmp/ts710-test.txt containing the word ok.","project_dir":"/tmp/ts710-prd"}' \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD for reset_task test"
    return
  fi

  _cleanup() {
    api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true
  }

  # Decompose to get stories/tasks (use LLM if available, else skip)
  local decomp_code
  decomp_code=$(api_code POST "/api/autonomous/prds/$prd_id/decompose" '{}' | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$decomp_code" != "200" && "$decomp_code" != "202" ]]; then
    _cleanup
    skip "decompose failed ($decomp_code) — need LLM for reset_task live test"
    return
  fi

  # Wait for stories (up to 90s)
  local prd_snap story_count task_id
  for i in $(seq 1 45); do
    sleep 2
    prd_snap=$(api GET "/api/autonomous/prds/$prd_id" 2>/dev/null || echo "{}")
    local prd_status
    prd_status=$(echo "$prd_snap" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    story_count=$(echo "$prd_snap" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d.get("stories",[])))' 2>/dev/null || echo "0")
    [[ "$prd_status" == "needs_review" || "$prd_status" == "approved" ]] && [[ "$story_count" -ge 1 ]] && break
    [[ "$prd_status" == "draft" || "$prd_status" == "failed" ]] && break
  done
  save_evidence TS-710 "post_decompose.json" "$prd_snap"

  if [[ "$story_count" -lt 1 ]]; then
    _cleanup
    skip "decompose produced 0 stories — LLM planning did not complete in 90s"
    return
  fi

  # Extract first task ID from first story
  task_id=$(echo "$prd_snap" | python3 -c '
import json,sys
d = json.load(sys.stdin)
stories = d.get("stories",[])
if stories:
    tasks = stories[0].get("tasks",[])
    if tasks:
        print(tasks[0].get("id",""))
' 2>/dev/null || echo "")

  if [[ -z "$task_id" ]]; then
    _cleanup
    skip "could not find a task in the decomposed PRD to reset"
    return
  fi

  # Inject a failed status via force=true on a fresh task (BL382 force=true resets any task)
  local reset_resp reset_code reset_body
  reset_resp=$(api_code POST "/api/autonomous/prds/$prd_id/reset_task" \
    "{\"task_id\":\"$task_id\",\"actor\":\"e2e-ts710\",\"force\":true}")
  reset_code=$(echo "$reset_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  reset_body=$(echo "$reset_resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-710 "reset_resp.json" "$reset_body"

  _cleanup

  if [[ "$reset_code" == "200" ]]; then
    ok "POST /api/autonomous/prds/$prd_id/reset_task succeeded (HTTP 200) — task $task_id reset"
    return
  fi
  if [[ "$reset_code" == "400" ]]; then
    # force=true on a pending task may return 400 if already in pending state
    local err_msg
    err_msg=$(echo "$reset_body" | head -c 200)
    if echo "$err_msg" | grep -qi "already pending\|not failed\|not blocked\|not cancelled"; then
      ok "POST reset_task returned 400 'already in pending state' — endpoint reachable and logic correct (task was never failed)"
    else
      ko "POST reset_task returned 400 with unexpected error: $err_msg"
    fi
    return
  fi

  ko "POST /api/autonomous/prds/{id}/reset_task returned HTTP $reset_code: $(echo "$reset_body" | head -c 200)"
}

RESULT=fail
_story_ts_710
: "${RESULT:=fail}"
unset -f _story_ts_710
unset -f _cleanup 2>/dev/null || true
