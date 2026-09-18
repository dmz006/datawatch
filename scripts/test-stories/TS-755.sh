#!/usr/bin/env bash
# TS-755 — REST reset_task 400 for nonexistent task ID
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-755"

story_preflight "surface:api feature:automata" || exit 0

# Reuse PRD from TS-754 state if available; otherwise create a new one.
prd_id=""
if [[ -f /tmp/ts754-state.env ]]; then
  prd_id=$(grep TS754_PRD_ID /tmp/ts754-state.env | cut -d= -f2 || true)
fi

if [[ -z "$prd_id" ]]; then
  prd_body=$(api POST /api/autonomous/prds \
    '{"spec":"Create a hello world script","actor":"ts-755","project_dir":"/tmp"}')
  prd_id=$(echo "$prd_body" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
fi

if [[ -z "$prd_id" ]]; then
  ko "Could not get a PRD for reset_task test"
  exit 0
fi

# POST reset_task with a task_id that does not exist in this PRD.
reset_raw=$(api_code POST "/api/autonomous/prds/${prd_id}/reset_task" \
  '{"task_id":"nonexistent-task-ts755","actor":"ts-755"}')
reset_code=$(echo "$reset_raw" | grep -oP '(?<=__HTTP_CODE_)\d+(?=__)' | tail -1)
reset_body=$(echo "$reset_raw" | sed 's/__HTTP_CODE_[0-9]*__//')

save_evidence "$CURRENT_STORY" "ts755_reset.json" "$reset_body"

if [[ "$reset_code" != "400" ]]; then
  ko "Expected HTTP 400 for nonexistent task_id; got $reset_code"
  exit 0
fi

ok "POST reset_task with nonexistent task_id returned 400 as expected (PRD=$prd_id)"
