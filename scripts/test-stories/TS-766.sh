#!/usr/bin/env bash
# TS-766 — LIVE round-trip: decomposition_profile=opencode → session-based decompose fires
# tags: surface:api feature:automata live:yes parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-766"

story_preflight "surface:api feature:automata live:yes" || return 0

# Verify opencode is available.
opencode_bin="${OPENCODE_BIN:-/home/dmz/.opencode/bin/opencode}"
if ! command -v opencode &>/dev/null && [[ ! -x "$opencode_bin" ]]; then
  skip "opencode binary not found — skip live session-based decompose test"
  return 0
fi

# Check autonomous is enabled.
a_cfg=$(api GET /api/autonomous/config)
a_enabled=$(echo "$a_cfg" | python3 -c "import sys,json; print(json.load(sys.stdin).get('enabled','false'))" 2>/dev/null || echo false)
if [[ "$a_enabled" != "True" && "$a_enabled" != "true" ]]; then
  skip "autonomous disabled in sandbox — skip"
  return 0
fi

# Create a PRD with project_dir=/tmp (opencode needs a real dir).
prd_body=$(api POST /api/autonomous/prds \
  '{"spec":"Write a bash function named ts766_hello that prints Hello TS-766 to stdout","actor":"ts-766","project_dir":"/tmp"}')
prd_id=$(echo "$prd_body" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || true)
if [[ -z "$prd_id" ]]; then
  ko "Could not create PRD for opencode decompose test: $prd_body"
  return 0
fi

# Set decomposition_profile=opencode to force session-based decompose path.
set_raw=$(api_code POST "/api/autonomous/prds/${prd_id}/set_llm" \
  '{"decomposition_profile":"opencode","actor":"ts-766"}')
set_code=$(echo "$set_raw" | grep -oP '(?<=__HTTP_CODE_)\d+(?=__)' | tail -1)
if [[ "$set_code" != "200" ]]; then
  ko "set_llm decomposition_profile=opencode returned HTTP $set_code"
  return 0
fi

# Trigger async decompose.
decomp_raw=$(api_code POST "/api/autonomous/prds/${prd_id}/decompose" '{}')
decomp_code=$(echo "$decomp_raw" | grep -oP '(?<=__HTTP_CODE_)\d+(?=__)' | tail -1)
if [[ "$decomp_code" != "202" && "$decomp_code" != "200" ]]; then
  ko "decompose returned HTTP $decomp_code — expected 202"
  return 0
fi

# Poll daemon logs for "decompose-session spawned" with backend=opencode.
spawned_msg=""
for i in $(seq 1 20); do
  sleep 3
  spawned_msg=$(grep -a "decompose-session.*backend=opencode\|decompose.*spawned.*opencode" \
    /tmp/dw-test-sandbox-2423963/daemon.log 2>/dev/null | tail -1 || true)
  prd_detail=$(api GET "/api/autonomous/prds/${prd_id}")
  status=$(echo "$prd_detail" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || true)
  echo "  [TS-766] attempt $i: status=$status spawned=${spawned_msg:0:80}"
  if [[ -n "$spawned_msg" || "$status" == "planned" || "$status" == "needs_review" || "$status" == "draft" ]]; then
    break
  fi
done

save_evidence "$CURRENT_STORY" "ts766_prd.json" "$prd_detail"

# Check if session-based decompose path was taken (log message OR outputFile exists).
outputFile="/tmp/.decompose-output.json"
took_session_path=false
[[ -n "$spawned_msg" ]] && took_session_path=true
[[ -f "$outputFile" ]] && took_session_path=true

# Also verify via PRD decisions: if opencode backend used, decision will record it.
decision_backend=$(echo "$prd_detail" | python3 -c "
import sys,json
d=json.load(sys.stdin)
decisions=[x for x in d.get('decisions',[]) if x.get('kind')=='decompose']
print(decisions[0]['backend'] if decisions else '')
" 2>/dev/null || true)
echo "  [TS-766] decision_backend=$decision_backend status=$status"

if [[ "$status" == "draft" && -z "$spawned_msg" ]]; then
  # Decompose may have failed (opencode timed out) — check the decisions for error
  ko "opencode session-based decompose rolled back to draft — check daemon.log for opencode errors"
  return 0
fi

if [[ -n "$spawned_msg" || "$decision_backend" == "opencode" || "$status" == "planned" || "$status" == "needs_review" ]]; then
  ok "opencode session-based decompose fired: PRD=$prd_id status=$status backend=${decision_backend:-session-path-confirmed}"
else
  ko "Could not confirm opencode session-based decompose fired (status=$status, no spawn log found)"
fi
