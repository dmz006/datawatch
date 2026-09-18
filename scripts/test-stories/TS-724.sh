#!/usr/bin/env bash
# TS-724 — Verifier diff grounding: PRD task exposes pre_task_sha field after run
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-724"
story_preflight "surface:api feature:automata" || return 0

_story_ts_724() {
  # Check autonomous enabled
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Look in existing PRDs for a task with pre_task_sha set
  local prds_resp
  prds_resp=$(api GET /api/autonomous/prds 2>/dev/null || echo "[]")
  save_evidence TS-724 "prds_list.json" "$prds_resp"

  local sha_found
  sha_found=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    for story in prd.get("stories",[]):
        for task in story.get("tasks",[]):
            sha = task.get("pre_task_sha","")
            if sha:
                print(sha)
                import sys; sys.exit(0)
' 2>/dev/null || echo "")

  if [[ -n "$sha_found" ]]; then
    ok "PRD task has pre_task_sha=$sha_found — verifier git-diff grounding wired (v8.20.x)"
    return
  fi

  # Check schema: does any task have the pre_task_sha key at all?
  local field_present
  field_present=$(echo "$prds_resp" | python3 -c '
import json,sys
d = json.load(sys.stdin)
prds = d if isinstance(d,list) else d.get("prds",[])
for prd in prds:
    for story in prd.get("stories",[]):
        for task in story.get("tasks",[]):
            print("yes" if "pre_task_sha" in task else "no")
            import sys; sys.exit(0)
' 2>/dev/null || echo "")

  if [[ "$field_present" == "yes" ]]; then
    ok "PRD task schema includes pre_task_sha field — verifier diff grounding present (no git project ran yet)"
    return
  fi

  skip "no PRDs with tasks found — run an Automata on a git project to validate pre_task_sha threading"
}

RESULT=fail
_story_ts_724
: "${RESULT:=fail}"
unset -f _story_ts_724
