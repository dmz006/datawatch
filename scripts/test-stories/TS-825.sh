#!/usr/bin/env bash
# TS-825 — judge size rule, proven end-to-end on a real run. With
# autonomous.judge_size_threshold_lines set to 1, an Automaton whose task
# changes more than one line on its branch is judged at completion, and the
# judge_policy decision it records carries the size rule text and the changed
# line count. With the threshold put back to 400, a second judge on the same
# small change records the default order with the count.
#
# How it reaches a runnable state is the TS-796 approach: add_story/add_task
# are rejected while an Automaton is "draft", so it decomposes first (planner
# on ollama, a file-free placeholder spec), then replaces the planned story with
# its own story + task. The execution backend is switched to claude-code
# afterwards (ollama has no file tools). The completion judge (rules-review)
# is switched on for this Automaton only, through its own guardrails override,
# so no other configuration entry changes. The one daemon setting the story
# touches, autonomous.judge_size_threshold_lines, is read first and put back by
# an EXIT trap, so it is restored even when the story fails. The second judge is
# triggered with rejudge when the Automaton ended blocked, else with plan-review.
# Skips (never passes silently) when autonomous is disabled, claude-code is not
# available, decompose does not reach review, the run does not reach its judge
# step, or the second judge cannot be triggered on this daemon.
# tags: surface:api feature:automata conflict:llm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-825"
story_preflight "surface:api feature:automata conflict:llm" || return 0

# Threshold to put back; empty until the story has read it.
_TS825_ORIG_THRESHOLD=""

_ts825_restore() {
  [[ -n "$_TS825_ORIG_THRESHOLD" ]] || return 0
  api PUT /api/config "{\"autonomous.judge_size_threshold_lines\":$_TS825_ORIG_THRESHOLD}" >/dev/null 2>&1 || true
  _TS825_ORIG_THRESHOLD=""
}

# Prints the judge_policy decision notes of an Automaton snapshot, one per line.
_ts825_judge_notes() {
  python3 -c '
import json,sys
d=json.load(sys.stdin)
for x in d.get("decisions") or []:
    if x.get("kind")=="judge_policy":
        print((x.get("note") or "").replace("\n"," "))
' 2>/dev/null
}

_story_ts_825() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local exec_backend="claude-code" exec_ok
  exec_ok=$(api GET /api/backends | python3 -c '
import json,sys
d=json.load(sys.stdin)
print("yes" if any(b.get("name")==sys.argv[1] and b.get("enabled") and b.get("available") for b in d.get("llm",[])) else "no")
' "$exec_backend" 2>/dev/null || echo "no")
  if [[ "$exec_ok" != "yes" ]]; then skip "$exec_backend not available on this daemon (needed to execute a file-writing task)"; return; fi

  # ---- remember the threshold; restore it on any exit ----
  local orig
  orig=$(api GET /api/autonomous/config | python3 -c '
import json,sys
v=json.load(sys.stdin).get("judge_size_threshold_lines")
print(v if isinstance(v,int) and not isinstance(v,bool) else "")
' 2>/dev/null || echo "")
  if [[ -z "$orig" ]]; then skip "GET /api/autonomous/config has no judge_size_threshold_lines on this daemon"; return; fi
  local prev_exit_trap
  prev_exit_trap=$(trap -p EXIT)
  _TS825_ORIG_THRESHOLD="$orig"
  trap '_ts825_restore' EXIT
  save_evidence TS-825 "0_threshold_before.txt" "judge_size_threshold_lines=$orig"

  _ts825_run "$exec_backend"

  _ts825_restore
  eval "${prev_exit_trap:-trap - EXIT}"
  local after
  after=$(api GET /api/autonomous/config | python3 -c 'import json,sys;print(json.load(sys.stdin).get("judge_size_threshold_lines"))' 2>/dev/null || echo "?")
  save_evidence TS-825 "9_threshold_after.txt" "judge_size_threshold_lines=$after (was $orig)"
  if [[ "$after" != "$orig" ]]; then
    echo "  [TS-825] threshold not restored: was $orig, now $after" >&2
    ko "judge_size_threshold_lines was $orig before the story and is $after after it"
  fi
}

_ts825_run() {
  local exec_backend="$1" code

  code=$(api_code PUT /api/config '{"autonomous.judge_size_threshold_lines":1}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then ko "PUT /api/config judge_size_threshold_lines=1 returned $code"; return; fi

  # ---- fresh temp git repo with a base commit ----
  local repo
  repo=$(mktemp -d "${TMPDIR:-/tmp}/ts825-repo-XXXXXX")
  git -C "$repo" init -q -b main
  echo "# ts825 fixture" > "$repo/README.md"
  git -C "$repo" add README.md
  git -C "$repo" -c user.email=ts825@example.invalid -c user.name=ts825 commit -qm init

  # ---- Automaton: decompose a file-free placeholder to reach needs_review ----
  local spec="Placeholder plan used only to reach review. Produce exactly one story with one task titled Review that reads the README and writes nothing. Do not plan any files."
  local atm atm_id status="" attempt i
  atm=$(api POST /api/autonomous/prds "{\"spec\":\"$spec\",\"project_dir\":\"$repo\",\"backend\":\"ollama\",\"effort\":\"low\"}")
  atm_id=$(echo "$atm" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$atm_id" ]]; then rm -rf "$repo"; ko "could not create Automaton: $(echo "$atm" | head -c 200)"; return; fi
  add_cleanup automaton "$atm_id"
  for attempt in 1 2 3; do
    api POST "/api/autonomous/prds/$atm_id/decompose" '{}' >/dev/null
    for i in $(seq 1 100); do
      status=$(api GET "/api/autonomous/prds/$atm_id/decompose/status" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
      [[ "$status" == "pending" || "$status" == "in_progress" ]] || break
      sleep 3
    done
    status=$(api GET "/api/autonomous/prds/$atm_id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    [[ "$status" == "needs_review" ]] && break
  done
  if [[ "$status" != "needs_review" ]]; then
    rm -rf "$repo"
    skip "decompose did not reach needs_review after 3 attempts (status=$status): $(api GET "/api/autonomous/prds/$atm_id/decompose/status" | head -c 200)"
    return
  fi

  # ---- replace the planned story with one story + one task that changes 3 lines ----
  local story_resp story_id old_ids
  story_resp=$(api POST "/api/autonomous/prds/$atm_id/add_story" '{"title":"Notes","description":"Write a small notes file."}')
  story_id=$(echo "$story_resp" | python3 -c 'import json,sys;print(json.load(sys.stdin)["stories"][-1]["id"])' 2>/dev/null || echo "")
  old_ids=$(echo "$story_resp" | python3 -c 'import json,sys;print(" ".join(s["id"] for s in json.load(sys.stdin)["stories"][:-1]))' 2>/dev/null || echo "")
  if [[ -z "$story_id" ]]; then rm -rf "$repo"; ko "add_story returned no story id: $(echo "$story_resp" | head -c 200)"; return; fi
  for i in $old_ids; do api POST "/api/autonomous/prds/$atm_id/remove_story" "{\"story_id\":\"$i\"}" >/dev/null; done
  local t_resp task_id
  t_resp=$(api POST "/api/autonomous/prds/$atm_id/add_task" "{\"story_id\":\"$story_id\",\"title\":\"notes\",\"spec\":\"Create NOTES.md containing exactly three lines: alpha, beta and gamma, one per line. Do nothing else.\"}")
  task_id=$(echo "$t_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print([s for s in d["stories"] if s["id"]==sys.argv[1]][0]["tasks"][-1]["id"])' "$story_id" 2>/dev/null || echo "")
  if [[ -z "$task_id" ]]; then rm -rf "$repo"; ko "add_task returned no task id"; return; fi

  # ---- claude-code executes; the completion judge is on for this Automaton only ----
  api POST "/api/autonomous/prds/$atm_id/set_llm" "{\"backend\":\"$exec_backend\",\"effort\":\"low\"}" >/dev/null
  code=$(api_code PUT "/api/autonomous/prds/$atm_id/guardrails" '{"per_prd_guardrails":["rules-review"]}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then rm -rf "$repo"; ko "guardrails override returned $code"; return; fi
  code=$(api_code POST "/api/autonomous/prds/$atm_id/approve" '{}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then rm -rf "$repo"; ko "approve returned $code"; return; fi
  code=$(api_code POST "/api/autonomous/prds/$atm_id/run" '{}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" && "$code" != "202" ]]; then rm -rf "$repo"; ko "run returned $code"; return; fi

  # ---- poll until the first judge entry lands or the Automaton is terminal (max 1800s) ----
  local snap notes="" st=""
  for i in $(seq 1 360); do
    snap=$(api GET "/api/autonomous/prds/$atm_id")
    notes=$(echo "$snap" | _ts825_judge_notes)
    st=$(echo "$snap" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    [[ -n "$notes" ]] && break
    case "$st" in completed|failed|cancelled|blocked) break;; esac
    sleep 5
  done
  save_evidence TS-825 "1_first_judge_snapshot.json" "$snap"
  if [[ -z "$notes" ]]; then
    rm -rf "$repo"
    if [[ "$st" == "completed" || "$st" == "blocked" ]]; then
      ko "Automaton $atm_id reached $st with no judge_policy decision recorded"
    else
      skip "run did not reach its judge step (Automaton $atm_id status=$st)"
    fi
    return
  fi

  # ---- first judge: size rule text and a count above 1 ----
  local first_verdict
  first_verdict=$(printf '%s\n' "$notes" | python3 -c '
import re,sys
for line in sys.stdin:
    m=re.search(r"size rule: (\d+) changed lines is above the threshold of 1,", line)
    if m and int(m.group(1))>1:
        print("OK lines=%s" % m.group(1)); sys.exit(0)
print("NO")
')
  if [[ "$first_verdict" != OK* ]]; then
    rm -rf "$repo"
    save_evidence TS-825 "2_first_judge_notes.txt" "$notes"
    if printf '%s\n' "$notes" | grep -q "is not in the chain"; then
      skip "judge chain on this daemon does not include the size-route backend, so the size rule cannot fire: $(printf '%s\n' "$notes" | head -1 | head -c 240)"
    else
      ko "no judge_policy note carries the size rule with a count above 1; notes: $(printf '%s\n' "$notes" | head -3 | head -c 400)"
    fi
    return
  fi
  save_evidence TS-825 "2_first_judge_notes.txt" "$notes
$first_verdict"
  echo "  [TS-825] automaton=$atm_id first judge: $first_verdict"
  local first_count
  first_count=$(printf '%s\n' "$notes" | grep -c .)

  # ---- threshold back to 400, then a second judge on the same small change ----
  code=$(api_code PUT /api/config '{"autonomous.judge_size_threshold_lines":400}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then rm -rf "$repo"; ko "PUT /api/config judge_size_threshold_lines=400 returned $code"; return; fi

  # The first judge entry can land before the Automaton finishes; wait for it to
  # leave running so a rejudge or plan review is accepted.
  for i in $(seq 1 120); do
    st=$(api GET "/api/autonomous/prds/$atm_id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    case "$st" in completed|failed|cancelled|blocked) break;; esac
    sleep 5
  done
  local trigger="" tcode
  if [[ "$st" == "blocked" ]]; then
    tcode=$(api_code POST "/api/autonomous/prds/$atm_id/rejudge" '{}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
    trigger="rejudge:$tcode"
  fi
  if [[ "$tcode" != "200" && "$tcode" != "202" ]]; then
    tcode=$(api_code POST "/api/autonomous/prds/$atm_id/plan-review" '{}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
    trigger="$trigger plan-review:$tcode"
  fi

  local notes2="" n2=0
  for i in $(seq 1 120); do
    snap=$(api GET "/api/autonomous/prds/$atm_id")
    notes2=$(echo "$snap" | _ts825_judge_notes)
    n2=$(printf '%s\n' "$notes2" | grep -c .)
    [[ "$n2" -gt "$first_count" ]] && break
    sleep 5
  done
  save_evidence TS-825 "3_second_judge_snapshot.json" "$snap"
  rm -rf "$repo"
  if [[ "$n2" -le "$first_count" ]]; then
    skip "no second judge ran on this daemon (Automaton status=$st, trigger: ${trigger:-none}); first judge verified: $first_verdict"
    return
  fi
  local second_verdict
  second_verdict=$(printf '%s\n' "$notes2" | tail -n +"$((first_count + 1))" | python3 -c '
import re,sys
for line in sys.stdin:
    m=re.search(r"default order: (\d+) changed lines is at or below the threshold of 400", line)
    if m and int(m.group(1))>=1:
        print("OK lines=%s" % m.group(1)); sys.exit(0)
print("NO")
')
  save_evidence TS-825 "4_second_judge_notes.txt" "$(printf '%s\n' "$notes2" | tail -n +"$((first_count + 1))")
$second_verdict"
  echo "  [TS-825] second judge: $second_verdict (trigger: $trigger)"
  if [[ "$second_verdict" == OK* ]]; then
    ok "threshold 1: judge note carries the size rule with the count ($first_verdict); threshold 400: second judge records default order with the count ($second_verdict)"
  else
    ko "second judge note does not record default order with the count at threshold 400: $(printf '%s\n' "$notes2" | tail -n +"$((first_count + 1))" | head -2 | head -c 400)"
  fi
}

RESULT=fail
_story_ts_825
: "${RESULT:=fail}"
unset -f _story_ts_825 _ts825_run _ts825_judge_notes _ts825_restore
unset _TS825_ORIG_THRESHOLD
