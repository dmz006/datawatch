#!/usr/bin/env bash
# scripts/restart-matrix/states-other.sh — restart matrix, capacity, git, schedule and client states.
#
# Same protocol as states-session.sh: each scenario boots its own sandbox daemon,
# drives work into a named state, restarts that daemon (TERM to its recorded pid)
# and records what became of the work. No real model is involved; the fake shell
# backend in lib.sh plays every role.
#
#   waiting-capacity      a task waits for admission while another task holds the one-slot host pool
#   waiting-external      a worker parks on an external condition; its re-admission waits behind
#                         another task that holds the one-slot pool
#   final-gate            a final-gate task's verification script is running (start marker written)
#   git-operation         the daemon's task-start snapshot commit is inside a slow pre-commit hook
#   automation-branches   an Automation runs an entry PRD, forks into two active PRD branches and joins them
#   scheduled-due         a scheduled spawn comes due while the daemon is down
#   websocket-clients     two WebSocket clients ride out the restart (scripts/restart-matrix/ws-client.py)
#   worker-node-placement two pi-prompt workers each run on a different compute node; the node pools,
#                         the leases' nodes and the sessions are the same after the restart
#
# Usage:
#   scripts/restart-matrix/states-other.sh [--strict] [--only NAME[,NAME]] [--list] [--keep]
#
# Run one state alone through the entry point:
#   scripts/restart-matrix.sh --only waiting-capacity
#
# Measure mode (default) prints PASS or FAIL per criterion and exits 0 even when a
# state fails. --strict exits 1 when any state failed. A harness fault exits 2.
# The criteria and the before/after record format are the same as states-session.sh.

set -uo pipefail
unset TMUX TMUX_TMPDIR # an ambient multiplexer never leaks into the sandbox

OT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$OT_DIR/lib.sh"
# shellcheck source=states-session.sh
. "$OT_DIR/states-session.sh" # sc_* helpers: restart with snapshots, common criteria, finish

OT_ALL_STATES=(waiting-capacity waiting-external final-gate git-operation automation-branches scheduled-due websocket-clients worker-node-placement)

ot_usage() {
  sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed -E 's/^# ?//'
}

# ------------------------------------------------------------------ helpers ----

ot_git() { env -u TMUX -u TMUX_TMPDIR git "$@"; }

# ot_boot NAME [MAX_SESSIONS] — fresh sandbox and daemon. MAX_SESSIONS sizes the
# host pool (max_sessions minus reserved_interactive); unset keeps the default.
ot_boot() {
  if [[ -n "${2:-}" ]]; then export RM_MAX_SESSIONS="$2"; else unset RM_MAX_SESSIONS; fi
  sc_boot "$1"
}

# ot_project_new DIR — a throwaway git repo for one more PRD's project.
ot_project_new() {
  mkdir -p "$1"
  (
    cd "$1" || exit 1
    ot_git init -q -b main . &&
      ot_git config user.email matrix@example.invalid &&
      ot_git config user.name restart-matrix &&
      printf '%s\n' '# restart matrix project' >README.md &&
      ot_git add -A && ot_git commit -q -m initial
  ) || rm_die "could not initialise project repo $1"
}

# ot_prd_create DIR — creates a PRD on the shell backend; prints its id.
ot_prd_create() {
  local body
  body="$(jq -n --arg dir "$1" '{spec: "Restart matrix probe: write a result file.", project_dir: $dir, backend: "shell"}')"
  rm_api POST /api/autonomous/prds "$body" | jq -r '.id // .prd.id // empty'
}

# ot_plan PRD — asks the daemon to plan PRD and waits until the plan is reviewable.
ot_plan() {
  rm_api POST "/api/autonomous/prds/$1/plan" '{}' >/dev/null
  rm_wait_until 180 sc_prd_status_in "$1" needs_review approved
}

# ot_run PRD — approves a planned PRD when needed and starts its executor.
ot_run() {
  ot_approve "$1"
  rm_api POST "/api/autonomous/prds/$1/run" '{}' >/dev/null
}

# ot_approve PRD — approves a planned PRD and leaves it unstarted.
ot_approve() {
  if sc_prd_status_in "$1" needs_review; then
    rm_api POST "/api/autonomous/prds/$1/approve" '{"actor":"restart-matrix","note":"sandbox run"}' >/dev/null
  fi
}

ot_task_wait_reason() { rm_task_field "$1" wait_reason; }

# ot_parked_on_external E H — E waits for capacity behind the external condition
# while H, which queued behind E's lease, has taken the slot E released.
ot_parked_on_external() {
  local h_status
  h_status="$(rm_task_field "$2" status)"
  rm_task_status_is "$1" waiting_capacity &&
    [[ "$(ot_task_wait_reason "$1")" == await_external* ]] &&
    [[ -n "$h_status" && "$h_status" != waiting_capacity ]]
}

# ot_second_start_after_first_done — the second worker started only after the
# first worker finished, so the one-slot pool was held until then.
ot_second_start_after_first_done() {
  local done_at start2
  done_at="$(sed -n 1p "$RM_CTRL/worker.done" 2>/dev/null | awk '{print $1}')"
  start2="$(sed -n 2p "$RM_CTRL/worker.starts" 2>/dev/null | awk '{print $1}')"
  [[ -n "$done_at" && -n "$start2" ]] && ((start2 >= done_at))
}

ot_automation_status() { rm_api GET "/api/automations/$1" | jq -r '.status // empty'; }
ot_automation_is() { [[ "$(ot_automation_status "$1")" == "$2" ]]; }
ot_automation_cursors() { rm_api GET "/api/automations/$1" | jq -r '[.active_cursors[]?] | length'; }
ot_automation_last_block() { rm_api GET "/api/automations/$1" | jq -r '[.decisions[]? | "\(.kind): \(.note)"] | .[-3:] | join(" | ") | if . == "" then "none" else . end'; }

# ot_branches_started AUTO — both branch workers started, or the Automation blocked
# (so a refused activation is reported at once, with its reason).
ot_branches_started() { sc_ctrl_at_least worker.starts 3 || ot_automation_is "$1" blocked; }

ot_schedule_json() { rm_api GET /api/schedules | jq -c --arg id "$1" '[.[] | select(.id == $id)][0] // {}'; }
ot_schedule_field() { ot_schedule_json "$1" | jq -r ".$2 // empty"; }
ot_fire_count() { ot_schedule_json "$1" | jq -r '.fire_count // 0'; }

# ot_ws_count FILE EVENT — how many events of that kind a client has logged.
ot_ws_count() {
  local n
  n="$(grep -c "\"ev\": \"$2\"" "$1" 2>/dev/null)"
  printf '%s' "${n:-0}"
}

# ot_ws_all_have EVENT N — both WebSocket clients have logged EVENT at least N times.
ot_ws_all_have() {
  (($(ot_ws_count "$RM_SB/ws-one.jsonl" "$1") >= $2 && $(ot_ws_count "$RM_SB/ws-two.jsonl" "$1") >= $2))
}

# ot_ws_more_messages ONE TWO — both clients have logged more messages than before.
ot_ws_more_messages() {
  (($(ot_ws_count "$RM_SB/ws-one.jsonl" message) > $1 && $(ot_ws_count "$RM_SB/ws-two.jsonl" message) > $2))
}

# ot_ws_summary FILE — per-client summary: messages before and after the
# reconnect, the reconnect time and whether the client gave up.
ot_ws_summary() {
  jq -c -s '
    reduce .[] as $e ({"restarted": false, "before": 0, "after": 0, "reconnect_seconds": null, "gave_up": false};
      if $e.ev == "message" then (if .restarted then .after += 1 else .before += 1 end)
      elif $e.ev == "reconnected" then (.restarted = true | .reconnect_seconds = $e.down_for)
      elif $e.ev == "gave_up" then .gave_up = true
      else . end)
    | del(.restarted)' "$1" 2>/dev/null || echo '{"before":0,"after":0,"reconnect_seconds":null,"gave_up":true}'
}

# ot_boot_pi NAME — fresh sandbox whose pi and pi-prompt backends run a fake pi, with
# two compute nodes (one worker slot each) and the LLM entries that list them.
ot_boot_pi() {
  unset RM_MAX_SESSIONS
  RM_PI_NODES=1
  rm_sandbox_new "$1"
  unset RM_PI_NODES
  rm_pi_nodes_setup
  rm_daemon_start
  rm_pi_nodes_register
}

# ot_prd_create_pi DIR — creates a PRD whose worker runs on pi-prompt with the pinned
# model; prints its id.
ot_prd_create_pi() {
  local body
  body="$(jq -n --arg dir "$1" --arg model "$RM_PI_MODEL" '{spec: "Restart matrix probe: write a result file.", project_dir: $dir, backend: "pi-prompt", model: $model}')"
  rm_api POST /api/autonomous/prds "$body" | jq -r '.id // .prd.id // empty'
}

# ot_node_held NODE — what GET /api/capacity says node:<NODE> holds.
ot_node_held() { rm_api GET /api/capacity | jq -r --arg p "node:$1" '[.pools[]? | select(.name == $p) | .held][0] // 0'; }

# ot_each_node_holds_one — both node pools hold exactly one lease.
ot_each_node_holds_one() { [[ "$(ot_node_held "$RM_NODE_A")" == 1 && "$(ot_node_held "$RM_NODE_B")" == 1 ]]; }

# ot_leases_of A B — the ledger has a lease for each of the two tasks.
ot_leases_of() {
  rm_api GET /api/capacity | jq -e --arg a "$1" --arg b "$2" '[.leases[]?.holder] | (index($a) != null) and (index($b) != null)' >/dev/null
}

# ot_node_snapshot LABEL PRD_A PRD_B OUT — the usual snapshot of PRD A plus the placement
# the restart must leave alone: every node pool (name, limit, held, holders), every lease's
# node, the two workers' task and session ids, and the pi starts the fake pi recorded.
ot_node_snapshot() {
  local label="$1" a="$2" b="$3" out="$4" cap tasks
  rm_snapshot "$label" "$a" "$out"
  cap="$(rm_api GET /api/capacity | jq -c . 2>/dev/null || echo '{}')"
  tasks="$(jq -nc --argjson a "$(rm_task_json "$a")" --argjson b "$(rm_task_json "$b")" '[$a, $b] | map({id: (.id // ""), status: (.status // ""), session_id: (.session_id // ""), retry_count: (.retry_count // 0)})')"
  jq -c --argjson cap "$cap" --argjson tasks "$tasks" --argjson pi "$(rm_ctrl_count pi.starts)" '
    . + {
      node_pools: [$cap.pools[]? | select(.name | startswith("node:")) | {name, limit, held, holders: ((.holders // []) | sort)}] | sort_by(.name),
      leases: [$cap.leases[]? | {holder, node: (.node // ""), node_unknown: (.node_unknown // false), session_id: (.session_id // "")}] | sort_by(.holder),
      workers: $tasks,
      pi_starts: $pi
    }' "$out" >"$out.tmp" && mv "$out.tmp" "$out"
}

# ot_pi_pids — pid of every fake pi start recorded so far, one per line.
ot_pi_pids() { sed -nE 's/.*pid=([0-9]+).*/\1/p' "$RM_CTRL/pi.starts" 2>/dev/null; }

# ot_pi_all_alive — every recorded fake pi is still running.
ot_pi_all_alive() {
  local p n=0
  while IFS= read -r p; do
    [[ -n "$p" ]] || continue
    n=$((n + 1))
    rm_pid_alive "$p" || return 1
  done < <(ot_pi_pids)
  ((n > 0))
}

# ------------------------------------------------------------------ states ----

state_waiting_capacity() {
  local name=waiting-capacity a b status_after
  SC_PRD=""
  rm_state_begin "$name" "a task waits for admission while another task holds the one-slot host pool"
  ot_boot "$name" 1
  echo 1 >"$RM_CTRL/planner_delay"
  echo 30 >"$RM_CTRL/worker_ticks"

  rm_write_plan ""
  SC_PRD="$(ot_prd_create "$RM_PROJECT")"
  a="$SC_PRD"
  ot_plan "$a" || { sc_abort_state "$name" "PRD A never reached review (status: $(rm_prd_status "$a"))"; return; }
  ot_project_new "$RM_SB/project-b"
  rm_write_plan ""
  b="$(ot_prd_create "$RM_SB/project-b")"
  SC_PRD="$b"
  ot_plan "$b" || { sc_abort_state "$name" "PRD B never reached review (status: $(rm_prd_status "$b"))"; return; }

  ot_run "$a"
  if ! rm_wait_until 90 sc_ctrl_at_least worker.starts 1; then
    SC_PRD="$a"
    sc_abort_state "$name" "PRD A's worker never started (task status: $(rm_task_field "$a" status))"
    return
  fi
  ot_run "$b"
  SC_PRD="$b"
  if ! rm_wait_until 120 rm_task_status_is "$b" waiting_capacity; then
    sc_abort_state "$name" "PRD B's task never waited for capacity (task status: $(rm_task_field "$b" status))"
    return
  fi
  local reason_before
  reason_before="$(ot_task_wait_reason "$b")"

  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  status_after="$(sc_json_get "$SC_AFTER_RESTART" .task.status)"
  sc_wait_settled "$b" "" 400
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  rm_check "still_waiting_after_restart" "$([[ "$status_after" == waiting_capacity ]] && echo 0 || echo 1)" "task status after restart: $status_after (wait reason before: ${reason_before:-none})"
  local runs
  runs="$(rm_count_lines "$RM_CTRL/worker.starts")"
  rm_check "one_worker_per_task" "$([[ "$runs" == 2 ]] && echo 0 || echo 1)" "worker starts: $runs (task A and task B, once each)"
  rm_check "admitted_after_slot_freed" "$(ot_second_start_after_first_done && echo 0 || echo 1)" "task B started after task A's worker finished"
  rm_check "task_completes" "$(sc_task_status_in "$b" completed && echo 0 || echo 1)" "task B status: $(rm_task_field "$b" status)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "PRD A held the only host slot; PRD B queued behind it"
}

state_waiting_external() {
  local name=waiting-external e parked_pid survived status_after reason_after starts
  SC_PRD=""
  rm_state_begin "$name" "a worker parks on an external condition with its session alive; the daemon restarts while it waits"
  ot_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 25 >"$RM_CTRL/worker_ticks"
  echo 1 >"$RM_CTRL/park_first_workers"
  echo 8 >"$RM_CTRL/park_after_ticks"

  rm_write_plan ""
  SC_PRD="$(ot_prd_create "$RM_PROJECT")"
  e="$SC_PRD"
  ot_plan "$e" || { sc_abort_state "$name" "PRD never reached review (status: $(rm_prd_status "$e"))"; return; }
  ot_run "$e"
  if ! rm_wait_until 120 sc_ctrl_at_least worker.parked 1; then
    sc_abort_state "$name" "the worker never parked on the external condition (task status: $(rm_task_field "$e" status))"
    return
  fi
  parked_pid="$(rm_last_pid "$RM_CTRL/worker.starts")"

  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  status_after="$(sc_json_get "$SC_AFTER_RESTART" .task.status)"
  reason_after="$(ot_task_wait_reason "$e")"
  survived=1
  rm_pid_alive "$parked_pid" && survived=0
  : >"$RM_CTRL/await.release" # the external condition resolves after the restart
  sc_wait_settled "$e" "" 400
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  rm_check "parked_worker_survived" "$survived" "parked worker pid $parked_pid alive right after the restart: $([[ $survived == 0 ]] && echo yes || echo no)"
  case "$status_after" in
    failed | cancelled | "") rm_check "not_failed_by_restart" 1 "task status after restart: ${status_after:-none}" ;;
    *) rm_check "not_failed_by_restart" 0 "task status after restart: $status_after (wait reason: ${reason_after:-none})" ;;
  esac
  starts="$(rm_count_lines "$RM_CTRL/worker.starts")"
  rm_check "worker_not_restarted" "$([[ "$starts" == 1 ]] && echo 0 || echo 1)" "worker starts: $starts (the parked worker's single start)"
  rm_check "resumes_after_condition" "$(sc_task_status_in "$e" completed && echo 0 || echo 1)" "task status after the condition resolves: $(rm_task_field "$e" status)"
  rm_check "worker_completes_once" "$([[ "$(rm_count_lines "$RM_CTRL/worker.done")" == 1 ]] && echo 0 || echo 1)" "worker finished $(rm_count_lines "$RM_CTRL/worker.done") time(s)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "worker parked on an external condition; the restart came while its session waited"
}

state_final_gate() {
  local name=final-gate kind status_before status_after finished
  SC_PRD=""
  rm_state_begin "$name" "the final-gate task's verification script is running (start marker written, sleeping)"
  ot_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 2 >"$RM_CTRL/worker_ticks"
  echo 20 >"$RM_CTRL/script_sleep"
  : >"$RM_CTRL/script.gate"

  rm_write_plan verify/gated.sh
  jq '.stories[0].tasks[0].kind = "final_gate"' "$RM_CTRL/plan.json" >"$RM_CTRL/plan.tmp" && mv "$RM_CTRL/plan.tmp" "$RM_CTRL/plan.json"
  SC_PRD="$(ot_prd_create "$RM_PROJECT")"
  rm_prd_script_guardrails "$SC_PRD"
  ot_plan "$SC_PRD" || { sc_abort_state "$name" "PRD never reached review (status: $(rm_prd_status "$SC_PRD"))"; return; }
  ot_run "$SC_PRD"
  if ! rm_wait_until 180 sc_ctrl_at_least script.starts 1; then
    sc_abort_state "$name" "the final-gate script never started (task status: $(rm_task_field "$SC_PRD" status))"
    return
  fi
  kind="$(rm_task_field "$SC_PRD" kind)"
  status_before="$(rm_task_field "$SC_PRD" status)"

  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  status_after="$(sc_json_get "$SC_AFTER_RESTART" .task.status)"
  sc_wait_settled "$SC_PRD" script.finished 240
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  sc_redone_flag "$SC_BEFORE" "$SC_FINAL" worker
  rm_check "final_gate_kind_kept" "$([[ "$kind" == final_gate ]] && echo 0 || echo 1)" "task kind: ${kind:-none}"
  rm_check "worker_not_redone" "$([[ "$RM_DERIVED_REDONE" == false ]] && echo 0 || echo 1)" "worker starts $(sc_json_get "$SC_BEFORE" .counts.worker_starts) -> $(sc_json_get "$SC_FINAL" .counts.worker_starts)"
  case "$status_after" in
    verifying | in_progress | running_tests | completed) rm_check "task_not_reset_by_restart" 0 "task status $status_before -> $status_after" ;;
    *) rm_check "task_not_reset_by_restart" 1 "task status $status_before -> $status_after" ;;
  esac
  finished="$(sc_json_get "$SC_FINAL" .counts.script_finished)"
  rm_check "gate_completes_once" "$([[ "$finished" == 1 ]] && echo 0 || echo 1)" "gate script finished $finished time(s); launched $(sc_json_get "$SC_FINAL" .counts.script_launched), started $(sc_json_get "$SC_FINAL" .counts.script_starts)"
  rm_check "task_completes" "$(sc_task_status_in "$SC_PRD" completed && echo 0 || echo 1)" "task status: $(rm_task_field "$SC_PRD" status)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL"
}

state_git_operation() {
  local name=git-operation status_after lock head_before results dirty
  SC_PRD=""
  rm_state_begin "$name" "the daemon's task-start snapshot commit is inside a slow pre-commit hook in the project repo"
  ot_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 2 >"$RM_CTRL/worker_ticks"
  # The hook sleeps only once git-slow exists, so commits made while planning stay fast.
  # Limit: the commit that takes a worker's output did not run this hook in a trial that
  # armed it after the worker started, so the commit this state interrupts is the
  # task-start snapshot commit, before any worker runs.
  cat >"$RM_PROJECT/.git/hooks/pre-commit" <<EOF
#!/usr/bin/env bash
if [[ -e "$RM_CTRL/git-slow" ]]; then
  echo "\$(date +%s) pid=\$\$" >>"$RM_CTRL/git-hook.starts"
  sleep "\$(cat "$RM_CTRL/git-slow")"
  echo "\$(date +%s) done" >>"$RM_CTRL/git-hook.done"
fi
exit 0
EOF
  chmod +x "$RM_PROJECT/.git/hooks/pre-commit"

  rm_write_plan ""
  SC_PRD="$(ot_prd_create "$RM_PROJECT")"
  rm_prd_git_enable "$SC_PRD"
  # The daemon's own bookkeeping files (.mcp.json, .datawatch/tree.lock) are dirty
  # before every git-enabled task, with or without a restart; with the default
  # block they stop the task before the restart matters. Measure in warn mode.
  rm_api PUT "/api/autonomous/prds/$SC_PRD/guardrails" '{"block_on_file_drift":false}' >/dev/null
  ot_plan "$SC_PRD" || { sc_abort_state "$name" "PRD never reached review (status: $(rm_prd_status "$SC_PRD"))"; return; }
  echo 20 >"$RM_CTRL/git-slow"
  ot_run "$SC_PRD"
  if ! rm_wait_until 180 sc_ctrl_at_least git-hook.starts 1; then
    sc_abort_state "$name" "no git commit reached the pre-commit hook (task status: $(rm_task_field "$SC_PRD" status))"
    return
  fi
  head_before="$(ot_git -C "$RM_PROJECT" rev-list --count HEAD)"

  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  status_after="$(sc_json_get "$SC_AFTER_RESTART" .task.status)"
  rm_wait_until 90 sc_ctrl_at_least git-hook.done 1
  sc_wait_settled "$SC_PRD" worker.done 300
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  sc_redone_flag "$SC_BEFORE" "$SC_FINAL" worker
  rm_check "hook_completed" "$(sc_ctrl_at_least git-hook.done 1 && echo 0 || echo 1)" "pre-commit hook runs finished: $(rm_ctrl_count git-hook.done) (started: $(rm_ctrl_count git-hook.starts))"
  lock=0
  [[ -e "$RM_PROJECT/.git/index.lock" ]] && lock=1
  rm_check "no_stale_index_lock" "$lock" "index.lock present after settle: $([[ $lock == 1 ]] && echo yes || echo no)"
  results="$(ot_git -C "$RM_PROJECT" log --format=%H --diff-filter=A -- worker-result.txt | wc -l | tr -d ' ')"
  rm_check "result_committed_once" "$([[ "$results" == 1 ]] && echo 0 || echo 1)" "commits adding worker-result.txt: $results (HEAD count when the hook was reached: $head_before)"
  # The daemon rewrites its own .mcp.json and .datawatch/ bookkeeping after a commit, so those
  # paths are excluded; any other changed path is output the commit left behind.
  dirty="$(ot_git -C "$RM_PROJECT" status --porcelain -- . ':!.mcp.json' ':!.datawatch' ':!.gitignore')"
  rm_check "repo_clean_after_settle" "$([[ -z "$dirty" ]] && echo 0 || echo 1)" "uncommitted worker output after settle: $(printf '%s' "$dirty" | grep -c . | tr -d ' ') path(s) (daemon bookkeeping excluded)"
  rm_check "task_completes" "$(sc_task_status_in "$SC_PRD" completed && echo 0 || echo 1)" "task status: $(rm_task_field "$SC_PRD" status) (after restart: $status_after)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "task-start snapshot commit blocked in a 20s pre-commit hook when the daemon was restarted; the commit of a worker output was not interrupted"
}

state_automation_branches() {
  local name=automation-branches e l r p auto body status_after cursors_before cursors_after
  SC_PRD=""
  rm_state_begin "$name" "an Automation forks into two active PRD branches; the daemon restarts while both run"
  ot_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 30 >"$RM_CTRL/worker_ticks"

  # The entry member runs first and fires the fork (a fork cannot be the entry).
  rm_write_plan ""
  e="$(ot_prd_create "$RM_PROJECT")"
  SC_PRD="$e"
  ot_plan "$e" || { sc_abort_state "$name" "entry PRD never reached review (status: $(rm_prd_status "$e"))"; return; }
  ot_project_new "$RM_SB/project-l"
  rm_write_plan ""
  l="$(ot_prd_create "$RM_SB/project-l")"
  ot_plan "$l" || { SC_PRD="$l"; sc_abort_state "$name" "branch PRD never reached review (status: $(rm_prd_status "$l"))"; return; }
  ot_project_new "$RM_SB/project-r"
  rm_write_plan ""
  r="$(ot_prd_create "$RM_SB/project-r")"
  ot_plan "$r" || { SC_PRD="$r"; sc_abort_state "$name" "second branch PRD never reached review (status: $(rm_prd_status "$r"))"; return; }

  body="$(jq -n --arg e "$e" --arg l "$l" --arg r "$r" '{
    name: "restart matrix branches",
    actor: "restart-matrix",
    reason: "fork two PRD branches after an entry member and join when both finish",
    entry_member_id: "entry",
    members: [
      {id: "entry", prd_id: $e, on_complete: "fork1"},
      {id: "fork1", kind: "fork", fork_members: ["branch-a", "branch-b"]},
      {id: "branch-a", prd_id: $l},
      {id: "branch-b", prd_id: $r},
      {id: "join1", kind: "join", join_members: ["branch-a", "branch-b"], on_branch_failure: "all_must_succeed"}
    ]
  }')"
  # The Automation starts member PRDs; it will not start one still waiting for review.
  for p in "$e" "$l" "$r"; do ot_approve "$p"; done
  auto="$(rm_api POST /api/automations "$body" | jq -r '.id // empty')"
  if [[ -z "$auto" ]]; then sc_abort_state "$name" "the Automation was not created"; return; fi
  rm_api POST "/api/automations/$auto/activate" '{"actor":"restart-matrix","reason":"sandbox run"}' >/dev/null
  if ! rm_wait_until 180 ot_branches_started "$auto" || ! sc_ctrl_at_least worker.starts 2; then
    sc_abort_state "$name" "both branches never started (automation status: $(ot_automation_status "$auto"), last block: $(ot_automation_last_block "$auto"))"
    return
  fi
  cursors_before="$(ot_automation_cursors "$auto")"
  if [[ "$cursors_before" != 2 ]]; then
    sc_abort_state "$name" "the Automation did not show two active branches (cursors: $cursors_before)"
    return
  fi

  SC_PRD="$l"
  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  status_after="$(ot_automation_status "$auto")"
  cursors_after="$(ot_automation_cursors "$auto")"
  rm_wait_until 400 ot_automation_is "$auto" completed
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  rm_check "automation_active_after_restart" "$([[ "$status_after" == active ]] && echo 0 || echo 1)" "Automation status after restart: $status_after"
  rm_check "both_branches_survive" "$([[ "$cursors_after" == 2 ]] && echo 0 || echo 1)" "active branches: $cursors_before before, $cursors_after after restart"
  local starts
  starts="$(rm_count_lines "$RM_CTRL/worker.starts")"
  rm_check "branches_not_rerun" "$([[ "$starts" == 3 ]] && echo 0 || echo 1)" "worker starts: $starts (entry once, each branch once)"
  rm_check "automation_completes" "$(ot_automation_is "$auto" completed && echo 0 || echo 1)" "Automation status: $(ot_automation_status "$auto")"
  rm_check "both_branches_complete" "$([[ "$(rm_prd_status "$l")" == completed && "$(rm_prd_status "$r")" == completed ]] && echo 0 || echo 1)" "branch PRD statuses: $(rm_prd_status "$l") and $(rm_prd_status "$r")"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "automation $auto: branches $cursors_before -> $cursors_after across the restart"
}

state_scheduled_due() {
  local name=scheduled-due id due body state_before fires_before fires_after fires_final state_final last_result ran
  SC_PRD=""
  rm_state_begin "$name" "a scheduled spawn comes due while the daemon is down and must fire once after boot"
  ot_boot "$name"
  echo 2 >"$RM_CTRL/worker_ticks"

  due="$(date -u -d '+20 seconds' +%FT%TZ)"
  body="$(jq -n --arg due "$due" --arg dir "$RM_PROJECT" '{
    type: "spawn",
    command: "matrix scheduled task",
    run_at: $due,
    name: "matrix-scheduled",
    schedule_name: "matrix-due",
    project_dir: $dir,
    backend: "shell",
    one_shot: true
  }')"
  id="$(rm_api POST /api/schedules "$body" | jq -r '.id // empty')"
  if [[ -z "$id" ]]; then sc_abort_state "$name" "the schedule was not created"; return; fi
  state_before="$(ot_schedule_field "$id" state)"
  fires_before="$(ot_fire_count "$id")"

  SC_BEFORE="$RM_SNAP/$name.before.json"
  rm_snapshot "$name-before" "" "$SC_BEFORE"
  RM_DOWNTIME=30 rm_daemon_restart || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  sleep 3
  SC_AFTER_RESTART="$RM_SNAP/$name.after-restart.json"
  rm_snapshot "$name-after-restart" "" "$SC_AFTER_RESTART"

  local t=0
  while ((t < 90)) && [[ "$(ot_fire_count "$id")" == 0 ]]; do sleep 1; t=$((t + 1)); done
  sleep 25 # a second fire, if the scheduler had one, would land in this window
  SC_FINAL="$RM_SNAP/$name.final.json"
  rm_snapshot "$name-final" "" "$SC_FINAL"
  fires_final="$(ot_fire_count "$id")"
  state_final="$(ot_schedule_field "$id" state)"
  last_result="$(ot_schedule_field "$id" last_fire_result)"
  ran="$(rm_count_lines "$RM_CTRL/worker.starts")"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  rm_check "pending_before_restart" "$([[ "$state_before" == pending && "$fires_before" == 0 ]] && echo 0 || echo 1)" "schedule state $state_before, fires $fires_before before the restart"
  rm_check "down_across_due_time" "$([[ "${RM_RESTART_DOWN_SECONDS:-0}" -ge 25 ]] && echo 0 || echo 1)" "daemon down ${RM_RESTART_DOWN_SECONDS:-?}s; run_at was $due"
  rm_check "fired_after_boot" "$([[ "$fires_final" -ge 1 ]] && echo 0 || echo 1)" "fire count $fires_final; last result: ${last_result:-none}"
  rm_check "fires_exactly_once" "$([[ "$fires_final" == 1 ]] && echo 0 || echo 1)" "fire count after a 25s grace: $fires_final"
  rm_check "spawned_task_ran_once" "$([[ "$ran" == 1 ]] && echo 0 || echo 1)" "worker starts: $ran"
  rm_check "schedule_done" "$([[ "$state_final" == done ]] && echo 0 || echo 1)" "schedule state: $state_final"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "schedule $id came due during a 30s outage"
}

state_websocket_clients() {
  local name=websocket-clients c pid pids=() before_one before_two sum_one sum_two r_one r_two within
  SC_PRD=""
  rm_state_begin "$name" "two WebSocket clients ride out the restart; reconnect time and message delivery measured"
  ot_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 30 >"$RM_CTRL/worker_ticks"

  rm_write_plan ""
  SC_PRD="$(ot_prd_create "$RM_PROJECT")"
  ot_plan "$SC_PRD" || { sc_abort_state "$name" "PRD never reached review (status: $(rm_prd_status "$SC_PRD"))"; return; }
  ot_run "$SC_PRD"
  if ! rm_wait_until 90 sc_ctrl_at_least worker.starts 1; then
    sc_abort_state "$name" "the worker never started, so there is no event traffic to watch"
    return
  fi

  for c in one two; do
    python3 -I "$OT_DIR/ws-client.py" --url "ws://127.0.0.1:$RM_PORT/ws" --name "$c" \
      --out "$RM_SB/ws-$c.jsonl" --stop "$RM_SB/ws.stop" --give-up-after 90 \
      >>"$RM_SB/ws-$c.log" 2>&1 &
    pids+=($!)
  done
  if ! rm_wait_until 30 ot_ws_all_have connect 1; then
    sc_abort_state "$name" "the WebSocket clients never connected"
    for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
    return
  fi
  rm_wait_until 30 ot_ws_all_have message 1
  before_one="$(ot_ws_count "$RM_SB/ws-one.jsonl" message)"
  before_two="$(ot_ws_count "$RM_SB/ws-two.jsonl" message)"

  export RM_DOWNTIME=5
  sc_restart_with_snapshots "$name" || { unset RM_DOWNTIME; sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  unset RM_DOWNTIME
  rm_wait_until 90 ot_ws_all_have reconnected 1
  rm_wait_until 30 ot_ws_more_messages "$before_one" "$before_two"
  sc_finish "$name"

  : >"$RM_SB/ws.stop"
  for pid in "${pids[@]}"; do
    rm_wait_until 15 bash -c "! kill -0 $pid 2>/dev/null" || kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  sum_one="$(ot_ws_summary "$RM_SB/ws-one.jsonl")"
  sum_two="$(ot_ws_summary "$RM_SB/ws-two.jsonl")"
  r_one="$(jq -r '.reconnect_seconds // 999' <<<"$sum_one")"
  r_two="$(jq -r '.reconnect_seconds // 999' <<<"$sum_two")"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  rm_check "clients_connected_before_restart" "$([[ "$before_one" -ge 1 && "$before_two" -ge 1 ]] && echo 0 || echo 1)" "messages seen before restart: one=$before_one two=$before_two"
  rm_check "both_clients_reconnected" "$([[ "$r_one" != 999 && "$r_two" != 999 ]] && echo 0 || echo 1)" "reconnect seconds: one=$r_one two=$r_two"
  if awk -v a="$r_one" -v b="$r_two" 'BEGIN { exit !(a <= 60 && b <= 60) }'; then within=0; else within=1; fi
  rm_check "reconnect_within_60s" "$within" "drop-to-reconnect seconds: one=$r_one two=$r_two"
  rm_check "no_client_gave_up" "$([[ "$(jq -r .gave_up <<<"$sum_one")" == false && "$(jq -r .gave_up <<<"$sum_two")" == false ]] && echo 0 || echo 1)" "gave up: one=$(jq -r .gave_up <<<"$sum_one") two=$(jq -r .gave_up <<<"$sum_two")"
  rm_check "messages_flow_after_reconnect" "$([[ "$(jq -r .after <<<"$sum_one")" -ge 1 && "$(jq -r .after <<<"$sum_two")" -ge 1 ]] && echo 0 || echo 1)" "messages after reconnect: one=$(jq -r .after <<<"$sum_one") two=$(jq -r .after <<<"$sum_two")"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "messages before/after the restart: one $(jq -r .before <<<"$sum_one")/$(jq -r .after <<<"$sum_one"), two $(jq -r .before <<<"$sum_two")/$(jq -r .after <<<"$sum_two"); events during the outage are not replayed"
}

# Two pi-prompt workers, placed by the capacity ledger on two different compute nodes (one
# slot each), are running when the daemon restarts. The fake pi processes live in the
# multiplexer and survive; the new daemon adopts their sessions. Each node's pool must still
# hold exactly one worker, each lease must name the node it had, no lease may lose its node
# (node_unknown), and neither worker may be started again. The nodes' addresses are loopback
# stubs that only answer the model residency check (see rm_pi_nodes_setup).
state_worker_node_placement() {
  local name=worker-node-placement a b
  SC_PRD=""
  rm_state_begin "$name" "two pi-prompt workers, each on a different compute node, ride out a daemon restart"
  ot_boot_pi "$name" || { sc_abort_state "$name" "sandbox daemon, compute nodes or LLM entries did not come up"; return; }
  echo 1 >"$RM_CTRL/planner_delay"
  echo 300 >"$RM_CTRL/pi_seconds"

  rm_write_plan ""
  SC_PRD="$(ot_prd_create_pi "$RM_PROJECT")"
  a="$SC_PRD"
  ot_plan "$a" || { sc_abort_state "$name" "PRD A never reached review (status: $(rm_prd_status "$a"))"; return; }
  ot_project_new "$RM_SB/project-b"
  rm_write_plan ""
  b="$(ot_prd_create_pi "$RM_SB/project-b")"
  ot_plan "$b" || { sc_abort_state "$name" "PRD B never reached review (status: $(rm_prd_status "$b"))"; return; }

  ot_run "$a"
  if ! rm_wait_until 120 sc_ctrl_at_least pi.starts 1; then
    sc_abort_state "$name" "PRD A's pi worker never started (task status: $(rm_task_field "$a" status), wait reason: $(ot_task_wait_reason "$a"))"
    return
  fi
  ot_run "$b"
  if ! rm_wait_until 120 sc_ctrl_at_least pi.starts 2; then
    sc_abort_state "$name" "PRD B's pi worker never started (task status: $(rm_task_field "$b" status), wait reason: $(ot_task_wait_reason "$b"))"
    return
  fi
  if ! rm_wait_until 60 ot_each_node_holds_one; then
    sc_abort_state "$name" "the workers are not one per node: $RM_NODE_A holds $(ot_node_held "$RM_NODE_A"), $RM_NODE_B holds $(ot_node_held "$RM_NODE_B")"
    return
  fi
  local task_a task_b
  task_a="$(rm_task_field "$a" id)"
  task_b="$(rm_task_field "$b" id)"

  SC_BEFORE="$RM_SNAP/$name.before.json"
  ot_node_snapshot "$name-before" "$a" "$b" "$SC_BEFORE"
  local pids_before
  pids_before="$(ot_pi_pids | paste -sd, -)"

  rm_daemon_restart || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  # the new daemon rebuilds the ledger from the live sessions; wait for both leases, then
  # let any later correction settle before the snapshot
  rm_wait_until 90 ot_leases_of "$task_a" "$task_b" || true
  sleep 8
  SC_AFTER_RESTART="$RM_SNAP/$name.after-restart.json"
  ot_node_snapshot "$name-after-restart" "$a" "$b" "$SC_AFTER_RESTART"
  SC_FINAL="$SC_AFTER_RESTART"
  local alive=1
  ot_pi_all_alive && alive=0
  : >"$RM_CTRL/pi.release" # let the fake workers end before the sandbox is torn down

  local before="$SC_BEFORE" after="$SC_AFTER_RESTART" pools_b pools_a nodes_b nodes_a
  rm_check "restart_exited_on_term" "$([[ "${RM_RESTART_TERM_CLEAN:-}" == true ]] && echo 0 || echo 1)" "daemon pid ${RM_RESTART_OLD_PID:-?} down in ${RM_RESTART_DOWN_SECONDS:-?}s"
  rm_check "daemon_replaced" "$([[ -n "${RM_RESTART_NEW_PID:-}" && "${RM_RESTART_NEW_PID:-}" != "${RM_RESTART_OLD_PID:-}" ]] && echo 0 || echo 1)" "pid ${RM_RESTART_OLD_PID:-?} -> ${RM_RESTART_NEW_PID:-?}"

  pools_b="$(jq -c '[.node_pools[] | "\(.name) limit=\(.limit) held=\(.held) holders=\(.holders | join("+"))"]' "$before")"
  pools_a="$(jq -c '[.node_pools[] | "\(.name) limit=\(.limit) held=\(.held) holders=\(.holders | join("+"))"]' "$after")"
  rm_check "placed_one_per_node_before" "$(jq -e --arg a "node:$RM_NODE_A" --arg b "node:$RM_NODE_B" '[.node_pools[] | select(.name == $a or .name == $b) | select(.limit == 1 and .held == 1)] | length == 2' "$before" >/dev/null && echo 0 || echo 1)" "before: $pools_b"
  rm_check "node_pools_unchanged" "$([[ "$(jq -c .node_pools "$before")" == "$(jq -c .node_pools "$after")" ]] && echo 0 || echo 1)" "before: $pools_b; after: $pools_a"
  rm_check "each_node_still_holds_one" "$(jq -e --arg a "node:$RM_NODE_A" --arg b "node:$RM_NODE_B" '[.node_pools[] | select(.name == $a or .name == $b) | select(.held == 1)] | length == 2' "$after" >/dev/null && echo 0 || echo 1)" "after: $pools_a"

  nodes_b="$(jq -c '[.leases[] | "\(.holder)@\(.node)"]' "$before")"
  nodes_a="$(jq -c '[.leases[] | "\(.holder)@\(.node)"]' "$after")"
  rm_check "lease_nodes_unchanged" "$([[ "$(jq -c '[.leases[] | {holder, node}]' "$before")" == "$(jq -c '[.leases[] | {holder, node}]' "$after")" ]] && echo 0 || echo 1)" "before: $nodes_b; after: $nodes_a"
  rm_check "no_lease_node_unknown" "$(jq -e '[.leases[] | select(.node_unknown)] | length == 0' "$after" >/dev/null && echo 0 || echo 1)" "leases flagged node_unknown after: $(jq -r '[.leases[] | select(.node_unknown) | .holder] | join(",") | if . == "" then "none" else . end' "$after")"

  rm_check "worker_sessions_unchanged" "$([[ "$(jq -c '[.workers[] | .session_id]' "$before")" == "$(jq -c '[.workers[] | .session_id]' "$after")" && "$(jq -r '[.workers[] | select(.session_id == "")] | length' "$before")" == 0 ]] && echo 0 || echo 1)" "worker session ids before $(jq -c '[.workers[] | .session_id]' "$before"), after $(jq -c '[.workers[] | .session_id]' "$after")"
  rm_check "workers_not_started_twice" "$([[ "$(jq -r .pi_starts "$before")" == 2 && "$(jq -r .pi_starts "$after")" == 2 ]] && echo 0 || echo 1)" "fake pi starts: $(jq -r .pi_starts "$before") -> $(jq -r .pi_starts "$after")"
  rm_check "worker_processes_survived" "$alive" "fake pi pids $pids_before alive after the restart: $([[ $alive == 0 ]] && echo yes || echo no)"
  rm_check "retry_counts_unchanged" "$([[ "$(jq -c '[.workers[] | .retry_count]' "$before")" == "$(jq -c '[.workers[] | .retry_count]' "$after")" ]] && echo 0 || echo 1)" "retries $(jq -c '[.workers[] | .retry_count]' "$before") -> $(jq -c '[.workers[] | .retry_count]' "$after")"
  rm_state_end "$SC_BEFORE" "$SC_FINAL" "PRD A and PRD B each ran a pi-prompt worker on a different one-slot node"
}

# --------------------------------------------------------------------- main ----

ot_main() {
  local strict=0 only="" list=0 arg s known states=()
  while (($#)); do
    arg="$1"
    shift
    case "$arg" in
      --strict) strict=1 ;;
      --keep) export RM_KEEP=1 ;;
      --list) list=1 ;;
      --only) only="${1:-}"; shift || true ;;
      --only=*) only="${arg#--only=}" ;;
      -h | --help) ot_usage; return 0 ;;
      *) rm_die "unknown argument: $arg (try --help)" ;;
    esac
  done
  if ((list)); then
    printf '%s\n' "${OT_ALL_STATES[@]}"
    return 0
  fi
  export RM_MODE=measure
  ((strict)) && RM_MODE=strict

  if [[ -n "$only" ]]; then
    IFS=, read -r -a states <<<"$only"
    for s in "${states[@]}"; do
      known=0
      for arg in "${OT_ALL_STATES[@]}"; do [[ "$s" == "$arg" ]] && known=1; done
      ((known)) || rm_die "unknown state: $s (known: ${OT_ALL_STATES[*]})"
    done
  else
    states=("${OT_ALL_STATES[@]}")
  fi

  rm_require_tools
  rm_init_root
  rm_install_traps # EXIT/INT/TERM/HUP — the sandbox is ended on every exit path
  printf 'restart matrix, other states (%s mode), records: %s\n' "$RM_MODE" "$RM_REC"

  for s in "${states[@]}"; do
    "state_${s//-/_}"
    rm_sandbox_finish
  done
  rm_write_summary
  if ((strict)) && (($(rm_total_failures) > 0)); then
    return 1
  fi
  return 0
}

# Run only when executed; sourcing this file just defines the scenarios.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  ot_main "$@"
  exit $?
fi
