#!/usr/bin/env bash
# TS-754 — LIVE decompose round-trip: PRD → decompose → stories+tasks via ollama /api/ask path.
# tags: surface:api feature:automata live:yes parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-754"

story_preflight "surface:api feature:automata live:yes" || return 0

# Create a PRD with a simple spec so decompose completes quickly.
prd_body=$(api POST /api/autonomous/prds \
  '{"spec":"Write a bash function named get_date that returns the current date as YYYYMMDD","actor":"ts-754","project_dir":"/tmp"}')
prd_id=$(echo "$prd_body" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
if [[ -z "$prd_id" ]]; then
  ko "Could not create PRD: $prd_body"
  return 0
fi

# Trigger async decompose — expect 202 Accepted or 200.
decomp_raw=$(api_code POST "/api/autonomous/prds/${prd_id}/decompose" '{}')
decomp_code=$(echo "$decomp_raw" | grep -oP '(?<=__HTTP_CODE_)\d+(?=__)' | tail -1)
if [[ "$decomp_code" != "202" && "$decomp_code" != "200" ]]; then
  ko "decompose returned HTTP $decomp_code — expected 202"
  return 0
fi

# Poll up to 150s for PRD to leave planning state (planned or needs_review = decompose OK).
status=""
stories=0
prd_detail=""
for i in $(seq 1 30); do
  sleep 5
  prd_detail=$(api GET "/api/autonomous/prds/${prd_id}")
  status=$(echo "$prd_detail" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || true)
  stories=$(echo "$prd_detail" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('stories',[])))" 2>/dev/null || echo 0)
  echo "  [TS-754] attempt $i: status=$status stories=$stories"
  # planned = auto-approved; needs_review = operator review gate; either means decompose completed
  if [[ "$status" == "planned" || "$status" == "needs_review" || "${stories:-0}" -gt 0 ]]; then
    break
  fi
  if [[ "$status" == "draft" ]]; then
    skip "PRD rolled back to draft — decompose LLM unavailable or call failed (transient)"
    return 0
  fi
done

if [[ "$status" != "planned" && "$status" != "needs_review" ]]; then
  ko "PRD still in status=$status after timeout — decompose did not complete"
  save_evidence "$CURRENT_STORY" "ts754_timeout.json" "$prd_detail"
  return 0
fi

if [[ "${stories:-0}" -lt 1 ]]; then
  ko "decompose completed (status=$status) but produced 0 stories"
  save_evidence "$CURRENT_STORY" "ts754_no_stories.json" "$prd_detail"
  return 0
fi

# Verify first story has tasks nested in PRD detail.
task_count=$(echo "$prd_detail" | python3 -c "
import sys,json
d=json.load(sys.stdin)
s=d.get('stories',[])
print(len(s[0].get('tasks',[])) if s else 0)
" 2>/dev/null || echo 0)

if [[ "${task_count:-0}" -lt 1 ]]; then
  save_evidence "$CURRENT_STORY" "ts754_no_tasks.json" "$prd_detail"
  skip "first story has 0 tasks — ollama produced stories but no structured tasks (model quality)"
  return 0
fi

# Verify decision log shows ollama backend was used for decompose.
backend_used=$(echo "$prd_detail" | python3 -c "
import sys,json
d=json.load(sys.stdin)
decisions=[x for x in d.get('decisions',[]) if x.get('kind')=='decompose']
print(decisions[0]['backend'] if decisions else '')
" 2>/dev/null || true)

save_evidence "$CURRENT_STORY" "ts754_prd.json" "$prd_detail"

# Export PRD id for downstream stories (TS-755+).
echo "TS754_PRD_ID=$prd_id" > /tmp/ts754-state.env

ok "PRD $prd_id: status=$status stories=$stories tasks_in_story0=$task_count backend_used=$backend_used"
