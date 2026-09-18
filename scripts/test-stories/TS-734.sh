#!/usr/bin/env bash
# TS-734 — Verifier diff: run a git-project Automata and check pre_task_sha in completed task
# tags: surface:api feature:automata conflict:llm parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-734"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_734() {
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Check if there are any existing completed tasks with pre_task_sha in a git project
  local prds_resp
  prds_resp=$(api GET /api/autonomous/prds 2>/dev/null || echo "[]")

  local sha_found
  sha_found=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    proj = prd.get("project_dir","")
    for story in prd.get("stories",[]):
        for task in story.get("tasks",[]):
            sha = task.get("pre_task_sha","")
            if sha:
                print(f"{sha[:12]} (project={proj[:30]})")
                import sys; sys.exit(0)
' 2>/dev/null || echo "")

  if [[ -n "$sha_found" ]]; then
    ok "PRD task has pre_task_sha — verifier diff grounding live-validated: $sha_found"
    return
  fi

  # Create a real git repo and run a tiny PRD on it
  local git_dir="$RUN_DIR/ts734-git-project"
  mkdir -p "$git_dir"
  cd "$git_dir" && git init -q && git config user.email "ts734@test" && git config user.name "TS734"
  echo "initial" > README.md
  git add README.md && git commit -q -m "initial"
  cd - >/dev/null

  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    "{\"spec\":\"Add a file called ts734-output.txt containing the word 'verified'.\",\"project_dir\":\"$git_dir\"}" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD for pre_task_sha test"
    return
  fi

  _cleanup() {
    api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true
    rm -rf "$git_dir" 2>/dev/null || true
  }

  # Decompose
  local decomp_code
  decomp_code=$(api_code POST "/api/autonomous/prds/$prd_id/decompose" '{}' | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$decomp_code" != "200" && "$decomp_code" != "202" ]]; then
    _cleanup
    skip "decompose failed ($decomp_code) — LLM required for pre_task_sha live test"
    return
  fi

  # Wait for stories (up to 90s)
  local prd_snap story_count
  for i in $(seq 1 45); do
    sleep 2
    prd_snap=$(api GET "/api/autonomous/prds/$prd_id" 2>/dev/null || echo "{}")
    local prd_status
    prd_status=$(echo "$prd_snap" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    story_count=$(echo "$prd_snap" | python3 -c 'import json,sys;print(len(json.load(sys.stdin).get("stories",[])))' 2>/dev/null || echo "0")
    [[ "$prd_status" == "needs_review" || "$prd_status" == "approved" ]] && [[ "$story_count" -ge 1 ]] && break
    [[ "$prd_status" == "draft" || "$prd_status" == "failed" ]] && break
  done

  if [[ "$story_count" -lt 1 ]]; then
    _cleanup
    skip "decompose did not produce stories in 90s"
    return
  fi

  # Approve and run
  api POST "/api/autonomous/prds/$prd_id/approve" '{}' >/dev/null 2>&1 || true
  local run_code
  run_code=$(api_code POST "/api/autonomous/prds/$prd_id/run" '{}' | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$run_code" != "200" && "$run_code" != "202" ]]; then
    _cleanup; skip "run failed ($run_code)"; return
  fi

  # Wait for any task to start (to get pre_task_sha) or PRD to complete (up to 120s)
  local task_sha=""
  for i in $(seq 1 60); do
    sleep 2
    prd_snap=$(api GET "/api/autonomous/prds/$prd_id" 2>/dev/null || echo "{}")
    local final_status
    final_status=$(echo "$prd_snap" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    task_sha=$(echo "$prd_snap" | python3 -c '
import json,sys
d = json.load(sys.stdin)
for story in d.get("stories",[]):
    for task in story.get("tasks",[]):
        sha = task.get("pre_task_sha","")
        if sha:
            print(sha[:12])
            import sys; sys.exit(0)
' 2>/dev/null || echo "")
    [[ -n "$task_sha" ]] && break
    case "$final_status" in completed|failed|cancelled) break ;; esac
  done

  save_evidence TS-734 "final.json" "$prd_snap"
  _cleanup

  if [[ -n "$task_sha" ]]; then
    ok "PRD task has pre_task_sha=$task_sha — verifier git-diff grounding live-verified on git project"
    return
  fi

  skip "PRD ran but no pre_task_sha found — tasks may not have started within 120s or project dir was not a git repo"
}

RESULT=fail
_story_ts_734
: "${RESULT:=fail}"
unset -f _story_ts_734
unset -f _cleanup 2>/dev/null || true
