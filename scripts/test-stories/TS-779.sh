#!/usr/bin/env bash
# TS-779 — Full Automaton/PRD lifecycle: decompose → approve → run → verify → complete,
# with a live check that a task never has two live sessions at once (regression guard
# for the 2026-09-22 retry-session-leak fix in executor.go/manager.go — see
# docs/flow/task-session-reconcile-flow.md).
# tags: surface:api feature:automata feature:journey conflict:llm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-779"
story_preflight "surface:api feature:automata feature:journey conflict:llm" || return 0

_story_ts_779() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local avail
  avail=$(wait_for_llm_backend 3 15)
  if [[ -z "$avail" ]]; then skip "no LLM backend available+enabled after retries"; return; fi

  local ts prd_id resp status
  ts=$(date +%s)

  # A deliberately tiny, unambiguous spec so a small/fast model decomposes it
  # to ~1 story / 1 task instead of sprawling — keeps this test's runtime
  # bounded and its verification deterministic (file either exists or not).
  local marker="/tmp/e2e-ts779-marker-$ts.txt"
  local spec="Create a file at exactly the path $marker containing the single line: ok. Do nothing else."
  resp=$(api POST /api/autonomous/prds "{\"spec\":\"$spec\",\"project_dir\":\"/tmp\",\"effort\":\"low\"}")
  save_evidence "TS-779" "0_create_prd.json" "$resp"
  prd_id=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD: $(echo "$resp" | head -c 150)"
    return
  fi
  add_cleanup "automaton" "$prd_id"

  # --- decompose --------------------------------------------------------
  resp=$(api POST "/api/autonomous/prds/$prd_id/decompose" '{}')
  save_evidence "TS-779" "1_decompose.json" "$resp"

  local deadline=$(( $(date +%s) + 90 ))
  status=""
  while [[ $(date +%s) -lt $deadline ]]; do
    status=$(api GET "/api/autonomous/prds/$prd_id" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
    [[ "$status" != "planning" && -n "$status" ]] && break
    sleep 3
  done
  if [[ "$status" != "needs_review" ]]; then
    skip "decompose did not reach needs_review within 90s (status=$status) — LLM backend likely cold/slow"
    return
  fi
  ok "decompose reached needs_review"

  # --- approve ------------------------------------------------------------
  resp=$(api POST "/api/autonomous/prds/$prd_id/approve" '{"actor":"e2e-ts779","note":"full lifecycle test"}')
  save_evidence "TS-779" "2_approve.json" "$resp"
  status=$(echo "$resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")
  if [[ "$status" != "approved" ]]; then
    ko "approve did not return status=approved (got: $status)"
    return
  fi
  ok "approved"

  # --- run, polling for completion while watching session tracking -------
  resp=$(api POST "/api/autonomous/prds/$prd_id/run" '{}')
  save_evidence "TS-779" "3_run.json" "$resp"

  # Track every SessionID any task in this PRD has ever held, and whether
  # each was still "live" (per /api/sessions/{id}) the moment we observed a
  # *different* SessionID on the same task — that's the exact shape of the
  # retry-leak bug: task moves to session B while session A (never told to
  # stop) is still alive in the session manager.
  declare -A seen_session_for_task
  local leak_detected="" final_status=""
  deadline=$(( $(date +%s) + 240 ))
  while [[ $(date +%s) -lt $deadline ]]; do
    local prd_json
    prd_json=$(api GET "/api/autonomous/prds/$prd_id" 2>/dev/null)
    final_status=$(echo "$prd_json" | python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "")

    # Walk task_id -> session_id pairs currently on the PRD.
    while IFS=$'\t' read -r task_id session_id; do
      [[ -z "$task_id" || -z "$session_id" ]] && continue
      local prev="${seen_session_for_task[$task_id]:-}"
      if [[ -n "$prev" && "$prev" != "$session_id" ]]; then
        # Session rotated for this task — the old one must no longer be
        # live. A dead/absent session is fine; a still-live one is the leak.
        local old_state
        old_state=$(api GET "/api/sessions/$prev" 2>/dev/null | python3 -c "import json,sys
try:
    d=json.load(sys.stdin)
    print(d.get('state',''))
except Exception:
    print('')" 2>/dev/null || echo "")
        if [[ "$old_state" != "" && "$old_state" != "complete" && "$old_state" != "failed" && "$old_state" != "killed" ]]; then
          leak_detected="task=$task_id old_session=$prev (state=$old_state) new_session=$session_id"
        fi
      fi
      seen_session_for_task[$task_id]="$session_id"
    done < <(echo "$prd_json" | python3 -c "
import json, sys
d = json.load(sys.stdin)
for s in d.get('stories', []):
    for t in s.get('tasks', []):
        sid = t.get('session_id', '')
        if sid:
            print(f\"{t.get('id','')}\t{sid}\")
" 2>/dev/null)

    [[ -n "$leak_detected" ]] && break
    if [[ "$final_status" == "completed" || "$final_status" == "failed" || "$final_status" == "blocked" ]]; then
      break
    fi
    sleep 5
  done
  save_evidence "TS-779" "4_final_prd.json" "$(api GET "/api/autonomous/prds/$prd_id" 2>/dev/null)"

  if [[ -n "$leak_detected" ]]; then
    ko "duplicate live session detected across a retry — session-leak regression: $leak_detected"
    return
  fi

  if [[ "$final_status" != "completed" && "$final_status" != "failed" && "$final_status" != "blocked" ]]; then
    skip "PRD did not reach a terminal status within 240s (status=$final_status) — backend likely too slow for e2e window"
    return
  fi
  ok "no duplicate live session observed across any retry"

  if [[ "$final_status" == "completed" ]]; then
    if [[ -f "$marker" ]]; then
      ok "full lifecycle completed and verifier-confirmed marker file exists: $marker"
      rm -f "$marker"
    else
      ko "PRD reported completed but marker file $marker was never created — verifier false-positive?"
    fi
  else
    # A model that couldn't produce the file after AutoFixRetries is a
    # backend/LLM quality issue, not an executor bug — the thing this test
    # exists to guard (no duplicate sessions) already passed above.
    skip "PRD reached terminal status=$final_status (not completed) — likely LLM output quality, not an executor bug; session tracking was still clean"
  fi
}

RESULT=fail
_story_ts_779
: "${RESULT:=fail}"
unset -f _story_ts_779
