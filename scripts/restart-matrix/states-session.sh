#!/usr/bin/env bash
# scripts/restart-matrix/states-session.sh — restart matrix, in-flight work states.
#
# Each scenario puts one unit of autonomous work into a named state on a sandbox
# daemon, restarts that daemon (TERM to its recorded pid), and checks what became
# of the work. No real model is involved: planner, worker, verifier and guardrail
# are one fake shell backend (see rm_write_fake_backend in lib.sh).
#
#   planning          decompose or planning is running
#   worker-running    a short real worker session is writing a counter file
#   verify-session    the worker finished; a live verifier session is judging it
#   verify-script     the worker finished; the daemon is running a verification script
#   verify-script-mid the same, with the script already half done (start marker
#                     written, now sleeping)
#   restart-during-launch  the first worker launch is held in the fake backend's
#                     launch step: the pane holds only a bare shell, no agent.
#                     The restart must end that shell at boot, not adopt it.
#
# Usage:
#   scripts/restart-matrix/states-session.sh [--strict] [--only NAME[,NAME]] [--list] [--keep]
#
# Measure mode (default) prints PASS or FAIL per criterion and exits 0 even when a
# state fails, so a run is a measurement. --strict exits 1 when any state failed.
# A harness fault (missing tool, no binary, refused production target) exits 2.
#
# The criteria describe the behaviour restart-proofing is meant to deliver: work
# in flight survives the restart, is not done twice, and loses no output.

set -uo pipefail
unset TMUX TMUX_TMPDIR # an ambient multiplexer never leaks into the sandbox

SC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SC_DIR/lib.sh"

SC_ALL_STATES=(planning worker-running verify-session verify-script verify-script-mid restart-during-launch)

sc_usage() {
  sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed -E 's/^# ?//'
}

# ------------------------------------------------------------------ helpers ----

sc_prd_status_in() { # PRD STATUS...
  local id="$1" cur
  shift
  cur="$(rm_prd_status "$id")"
  local s
  for s in "$@"; do [[ "$cur" == "$s" ]] && return 0; done
  return 1
}

sc_task_status_in() { # PRD STATUS...
  local id="$1" cur
  shift
  cur="$(rm_task_field "$id" status)"
  local s
  for s in "$@"; do [[ "$cur" == "$s" ]] && return 0; done
  return 1
}

sc_task_terminal() { sc_task_status_in "$1" completed failed cancelled blocked; }

# sc_wait_settled PRD MARKER_FILE TIMEOUT — waits for the work to be really over:
# the role's done marker exists (when a marker is named) and the task has stayed
# in a terminal status for a few seconds. A task can report completed while its
# verification is still running, so status alone ends the wait too early.
sc_wait_settled() {
  local prd="$1" marker="$2" timeout="$3" deadline=$((SECONDS + $3)) steady=0
  while ((SECONDS < deadline)); do
    if sc_task_terminal "$prd" && { [[ -z "$marker" ]] || [[ -s "$RM_CTRL/$marker" ]]; }; then
      steady=$((steady + 1))
      ((steady >= 6)) && return 0
    else
      steady=0
    fi
    sleep 1
  done
  return 1
}
sc_ctrl_at_least() { (($(rm_ctrl_count "$1") >= $2)); }

# sc_boot NAME — fresh sandbox plus a running daemon for one scenario.
sc_boot() {
  rm_sandbox_new "$1"
  rm_daemon_start
}

# sc_prd_new [SCRIPT_REL] — writes the plan the fake planner will return and
# creates the PRD. Sets SC_PRD.
sc_prd_new() {
  rm_write_plan "${1:-}"
  SC_PRD="$(rm_prd_create)"
  [[ -n "$SC_PRD" ]]
}

# sc_plan_start — asks the daemon to plan the PRD (returns at once).
sc_plan_start() { rm_api POST "/api/autonomous/prds/$SC_PRD/plan" '{}' >/dev/null; }

# sc_approve_and_run — approve a planned PRD and start its executor.
sc_approve_and_run() {
  rm_wait_until 180 sc_prd_status_in "$SC_PRD" needs_review approved || return 1
  if sc_prd_status_in "$SC_PRD" needs_review; then
    rm_api POST "/api/autonomous/prds/$SC_PRD/approve" '{"actor":"restart-matrix","note":"sandbox run"}' >/dev/null
  fi
  rm_api POST "/api/autonomous/prds/$SC_PRD/run" '{}' >/dev/null
}

# sc_restart_with_snapshots LABEL — before snapshot, restart, immediate-after
# snapshot. Files land in the record dir as <label>.before.json etc.
sc_restart_with_snapshots() {
  SC_BEFORE="$RM_SNAP/$1.before.json"
  SC_AFTER_RESTART="$RM_SNAP/$1.after-restart.json"
  rm_snapshot "$1-before" "$SC_PRD" "$SC_BEFORE"
  rm_daemon_restart || return 1
  sleep 3 # let the new daemon's resume pass run
  rm_snapshot "$1-after-restart" "$SC_PRD" "$SC_AFTER_RESTART"
}

# sc_finish LABEL — final snapshot once the work settles (or the wait runs out).
sc_finish() {
  SC_FINAL="$RM_SNAP/$1.final.json"
  rm_snapshot "$1-final" "$SC_PRD" "$SC_FINAL"
}

# sc_abort_state NAME REASON — the trigger state was never reached, so there is
# nothing to restart. Recorded as a failed state rather than aborting the run.
sc_abort_state() {
  rm_check "state_reached" 1 "$2"
  local snap="$RM_SNAP/$1.before.json"
  [[ -n "${SC_PRD:-}" ]] && rm_snapshot "$1-before" "$SC_PRD" "$snap"
  rm_state_end "$snap" "$snap" "$2"
}

# --------------------------------------------------------- shared criteria ----

# sc_json_get FILE JQ_EXPR
sc_json_get() { jq -r "$2" "$1"; }

# sc_common_criteria BEFORE AFTER_RESTART FINAL — criteria every state shares.
# Also fills the derived fields rm_state_end records.
sc_common_criteria() {
  local before="$1" after_restart="$2" final="$3" lost dup dup_after
  rm_check "restart_exited_on_term" "$([[ "${RM_RESTART_TERM_CLEAN:-}" == true ]] && echo 0 || echo 1)" "daemon pid ${RM_RESTART_OLD_PID:-?} down in ${RM_RESTART_DOWN_SECONDS:-?}s"
  rm_check "daemon_replaced" "$([[ -n "${RM_RESTART_NEW_PID:-}" && "${RM_RESTART_NEW_PID:-}" != "${RM_RESTART_OLD_PID:-}" ]] && echo 0 || echo 1)" "pid ${RM_RESTART_OLD_PID:-?} -> ${RM_RESTART_NEW_PID:-?}"

  dup="$(rm_duplicate_sessions "$final")"
  dup_after="$(rm_duplicate_sessions "$after_restart")"
  ((dup_after > dup)) && dup="$dup_after"
  RM_DERIVED_DUPLICATES="$dup"
  rm_check "no_duplicate_sessions" "$((dup == 0 ? 0 : 1))" "$dup extra live session(s) beyond one per role"

  lost="$(rm_total_lost_bytes "$before" "$final")"
  RM_DERIVED_LOST_BYTES="$lost"
  rm_check "no_lost_output" "$((lost == 0 ? 0 : 1))" "$lost byte(s) of earlier output missing afterwards"

  local rb ra
  rb="$(sc_json_get "$before" '.task.retry_count')"
  ra="$(sc_json_get "$final" '.task.retry_count')"
  rm_check "retry_count_unchanged" "$([[ "$rb" == "$ra" ]] && echo 0 || echo 1)" "retries $rb -> $ra"
}

# sc_redone_flag BEFORE FINAL ROLE... — redone is true when a LLM or worker role
# started more times after the restart than before.
sc_redone_flag() {
  local before="$1" final="$2" role cb ca redone=false
  shift 2
  for role in "$@"; do
    cb="$(sc_json_get "$before" ".counts.${role}_starts")"
    ca="$(sc_json_get "$final" ".counts.${role}_starts")"
    ((cb > 0 && ca > cb)) && redone=true # a role that had not started before the restart is not a redo
  done
  RM_DERIVED_REDONE="$redone"
}

# sc_task_session_stable BEFORE FINAL — the task kept the one session it had
# before the restart (a finished session may leave the live list, so the task's
# own session id is the thing compared).
sc_task_session_stable() {
  local b a
  b="$(sc_json_get "$1" '.task.session_id')"
  a="$(sc_json_get "$2" '.task.session_id')"
  [[ -n "$b" && "$b" == "$a" ]]
}

# ---------------------------------------------------------------- scenarios ----

state_planning() {
  local name=planning
  rm_state_begin "$name" "decompose or planning is running"
  sc_boot "$name"
  echo 45 >"$RM_CTRL/planner_delay"
  sc_prd_new verify/quick.sh || { sc_abort_state "$name" "could not create the PRD"; return; }
  sc_plan_start
  if ! rm_wait_until 90 sc_ctrl_at_least planner.starts 1; then
    sc_abort_state "$name" "the planner session never started (PRD status: $(rm_prd_status "$SC_PRD"))"
    return
  fi
  local planner_pid
  planner_pid="$(rm_last_pid "$RM_CTRL/planner.starts")"
  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  local survived=1
  rm_pid_alive "$planner_pid" && survived=0
  # The fake planner takes 45s; give the daemon a long window to finish planning.
  rm_wait_until 150 sc_prd_status_in "$SC_PRD" needs_review approved
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  sc_redone_flag "$SC_BEFORE" "$SC_FINAL" planner
  rm_check "planner_process_survived" "$survived" "planner pid $planner_pid alive right after the restart: $([[ $survived == 0 ]] && echo yes || echo no)"
  rm_check "planner_not_redone" "$([[ "$RM_DERIVED_REDONE" == false ]] && echo 0 || echo 1)" "planner starts $(sc_json_get "$SC_BEFORE" .counts.planner_starts) -> $(sc_json_get "$SC_FINAL" .counts.planner_starts)"
  rm_check "plan_completes_unaided" "$(sc_prd_status_in "$SC_PRD" needs_review approved && echo 0 || echo 1)" "PRD status after the wait: $(rm_prd_status "$SC_PRD")"
  local tasks
  tasks="$(rm_prd_json "$SC_PRD" | jq -r '[(.prd // .).stories[]?.tasks[]?] | length' 2>/dev/null)"
  rm_check "plan_has_its_task" "$([[ "${tasks:-0}" == 1 ]] && echo 0 || echo 1)" "tasks in the finished plan: ${tasks:-0}"
  rm_state_end "$SC_BEFORE" "$SC_FINAL"
}

state_worker_running() {
  local name=worker-running ticks=45
  rm_state_begin "$name" "worker running: a short real session writing a counter file"
  sc_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo "$ticks" >"$RM_CTRL/worker_ticks"
  sc_prd_new verify/quick.sh || { sc_abort_state "$name" "could not create the PRD"; return; }
  sc_plan_start
  sc_approve_and_run || { sc_abort_state "$name" "plan never became runnable (PRD status: $(rm_prd_status "$SC_PRD"))"; return; }
  if ! rm_wait_until 120 sc_counter_at_least 4; then
    sc_abort_state "$name" "the worker never reached tick 4 (task status: $(rm_task_field "$SC_PRD" status))"
    return
  fi
  local worker_pid
  worker_pid="$(rm_last_pid "$RM_CTRL/worker.starts")"
  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  local survived=1
  rm_pid_alive "$worker_pid" && survived=0
  sc_wait_settled "$SC_PRD" worker.done 240
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  sc_redone_flag "$SC_BEFORE" "$SC_FINAL" worker
  rm_check "worker_process_survived" "$survived" "worker pid $worker_pid alive right after the restart: $([[ $survived == 0 ]] && echo yes || echo no)"
  rm_check "worker_not_redone" "$([[ "$RM_DERIVED_REDONE" == false ]] && echo 0 || echo 1)" "worker starts $(sc_json_get "$SC_BEFORE" .counts.worker_starts) -> $(sc_json_get "$SC_FINAL" .counts.worker_starts)"
  rm_check "session_id_stable" "$(sc_task_session_stable "$SC_BEFORE" "$SC_FINAL" && echo 0 || echo 1)" "task session $(sc_json_get "$SC_BEFORE" .task.session_id) -> $(sc_json_get "$SC_FINAL" .task.session_id)"
  local lines
  lines="$(rm_count_lines "$RM_PROJECT/worker-counter.txt")"
  rm_check "counter_file_complete" "$([[ "$lines" == "$ticks" ]] && echo 0 || echo 1)" "counter lines $lines of $ticks"
  rm_check "task_completes" "$(sc_task_status_in "$SC_PRD" completed && echo 0 || echo 1)" "task status: $(rm_task_field "$SC_PRD" status)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL"
}

sc_counter_at_least() { (($(rm_count_lines "$RM_PROJECT/worker-counter.txt") >= $1)); }

state_verify_session() {
  local name=verify-session
  rm_state_begin "$name" "worker finished; the task is verifying through a live verifier session"
  sc_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 2 >"$RM_CTRL/worker_ticks"
  echo 45 >"$RM_CTRL/verifier_delay"
  sc_prd_new || { sc_abort_state "$name" "could not create the PRD"; return; }
  sc_plan_start
  sc_approve_and_run || { sc_abort_state "$name" "plan never became runnable (PRD status: $(rm_prd_status "$SC_PRD"))"; return; }
  if ! rm_wait_until 180 sc_ctrl_at_least verifier.starts 1; then
    sc_abort_state "$name" "the verifier session never started (task status: $(rm_task_field "$SC_PRD" status))"
    return
  fi
  local verifier_pid
  verifier_pid="$(rm_last_pid "$RM_CTRL/verifier.starts")"
  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  local survived=1
  rm_pid_alive "$verifier_pid" && survived=0
  sc_wait_settled "$SC_PRD" verifier.done 240
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  sc_redone_flag "$SC_BEFORE" "$SC_FINAL" worker verifier
  rm_check "verifier_process_survived" "$survived" "verifier pid $verifier_pid alive right after the restart: $([[ $survived == 0 ]] && echo yes || echo no)"
  rm_check "verifier_not_redone" "$([[ "$(sc_json_get "$SC_FINAL" .counts.verifier_starts)" == "$(sc_json_get "$SC_BEFORE" .counts.verifier_starts)" ]] && echo 0 || echo 1)" "verifier starts $(sc_json_get "$SC_BEFORE" .counts.verifier_starts) -> $(sc_json_get "$SC_FINAL" .counts.verifier_starts)"
  rm_check "worker_not_redone" "$([[ "$(sc_json_get "$SC_FINAL" .counts.worker_starts)" == "$(sc_json_get "$SC_BEFORE" .counts.worker_starts)" ]] && echo 0 || echo 1)" "worker starts $(sc_json_get "$SC_BEFORE" .counts.worker_starts) -> $(sc_json_get "$SC_FINAL" .counts.worker_starts)"
  rm_check "task_completes" "$(sc_task_status_in "$SC_PRD" completed && echo 0 || echo 1)" "task status: $(rm_task_field "$SC_PRD" status)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL"
}

# Shared body of the two script states. GATE_OPEN=1 lets the script past its
# gate before the restart, so the restart lands after its start marker.
sc_script_state() { # NAME DESCRIPTION GATE_OPEN
  local name="$1" desc="$2" gate_open="$3"
  rm_state_begin "$name" "$desc"
  sc_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo 2 >"$RM_CTRL/worker_ticks"
  echo 20 >"$RM_CTRL/script_sleep"
  [[ "$gate_open" == 1 ]] && : >"$RM_CTRL/script.gate"
  sc_prd_new verify/gated.sh || { sc_abort_state "$name" "could not create the PRD"; return; }
  rm_prd_script_guardrails "$SC_PRD"
  sc_plan_start
  sc_approve_and_run || { sc_abort_state "$name" "plan never became runnable (PRD status: $(rm_prd_status "$SC_PRD"))"; return; }
  local marker=script.launched
  [[ "$gate_open" == 1 ]] && marker=script.starts
  if ! rm_wait_until 180 sc_ctrl_at_least "$marker" 1; then
    sc_abort_state "$name" "the verification script never reached $marker (task status: $(rm_task_field "$SC_PRD" status))"
    return
  fi
  local status_before
  status_before="$(rm_task_field "$SC_PRD" status)"
  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  local status_after
  status_after="$(sc_json_get "$SC_AFTER_RESTART" .task.status)"
  : >"$RM_CTRL/script.gate" # the re-run (or the original) may proceed
  sc_wait_settled "$SC_PRD" script.finished 240
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  sc_redone_flag "$SC_BEFORE" "$SC_FINAL" worker
  rm_check "worker_not_redone" "$([[ "$RM_DERIVED_REDONE" == false ]] && echo 0 || echo 1)" "worker starts $(sc_json_get "$SC_BEFORE" .counts.worker_starts) -> $(sc_json_get "$SC_FINAL" .counts.worker_starts)"
  case "$status_after" in
    verifying | in_progress | running_tests | completed) rm_check "task_not_reset_by_restart" 0 "task status $status_before -> $status_after" ;;
    *) rm_check "task_not_reset_by_restart" 1 "task status $status_before -> $status_after" ;;
  esac
  local finished
  finished="$(sc_json_get "$SC_FINAL" .counts.script_finished)"
  rm_check "script_completes_once" "$([[ "$finished" == 1 ]] && echo 0 || echo 1)" "script finished $finished time(s); launched $(sc_json_get "$SC_FINAL" .counts.script_launched), started $(sc_json_get "$SC_FINAL" .counts.script_starts)"
  rm_check "task_completes" "$(sc_task_status_in "$SC_PRD" completed && echo 0 || echo 1)" "task status: $(rm_task_field "$SC_PRD" status)"
  rm_state_end "$SC_BEFORE" "$SC_FINAL"
}

state_verify_script() {
  sc_script_state verify-script "worker finished; the task is verifying through a daemon-run script that has launched" 0
}

state_verify_script_mid() {
  sc_script_state verify-script-mid "verifying through a daemon-run script that is half done: start marker written, now sleeping" 1
}

# The first worker launch holds in the fake backend before its agent starts, so
# the daemon's pane is a bare shell with no agent (AgentStarted reports false).
# The restart must end that shell at boot: adopting it as live would leave the
# task waiting on a session that never ran, and a fresh launch would then be a
# duplicate. The relaunch runs unheld and the PRD completes unaided.
state_restart_during_launch() {
  local name=restart-during-launch ticks=3
  rm_state_begin "$name" "restart while the first worker launch is held in the fake backend's launch step: the pane holds only a bare shell, no agent"
  sc_boot "$name"
  echo 1 >"$RM_CTRL/planner_delay"
  echo "$ticks" >"$RM_CTRL/worker_ticks"
  echo 90 >"$RM_CTRL/launch_delay" # backstop: the hold lasts this long unless launch.release appears
  sc_prd_new verify/quick.sh || { sc_abort_state "$name" "could not create the PRD"; return; }
  sc_plan_start
  sc_approve_and_run || { sc_abort_state "$name" "plan never became runnable (PRD status: $(rm_prd_status "$SC_PRD"))"; return; }
  if ! rm_wait_until 120 sc_ctrl_at_least worker.held 1; then
    sc_abort_state "$name" "the first worker launch never reached its hold (task status: $(rm_task_field "$SC_PRD" status))"
    return
  fi
  local held_pid held_session held_tmux held_cmd bare=1
  held_pid="$(rm_last_pid "$RM_CTRL/worker.held")"
  held_session="$(rm_task_field "$SC_PRD" session_id)"
  held_tmux="$(rm_session_rows | jq -r --arg id "$held_session" '[.[]? | select(.id == $id or .full_id == $id) | .tmux_session][0] // empty')"
  # list-panes, not display-message: display-message prints nothing for a session target.
  held_cmd="$(rm_tmux list-panes -t "=$held_tmux" -F '#{pane_current_command}' 2>/dev/null | head -n 1)"
  case "$held_cmd" in bash | sh | zsh | dash | fish | ksh) bare=0 ;; esac
  rm_check "held_pane_is_bare_shell" "$bare" "pane ${held_tmux:-?} runs '${held_cmd:-nothing}' before the restart"

  sc_restart_with_snapshots "$name" || { sc_abort_state "$name" "sandbox daemon did not come back"; return; }
  rm_wait_until 30 bash -c "! kill -0 $held_pid 2>/dev/null" || true
  local ended=1
  if [[ -n "$held_tmux" ]] && ! rm_tmux has-session -t "=$held_tmux" 2>/dev/null && ! rm_pid_alive "$held_pid"; then ended=0; fi
  rm_check "bare_shell_ended_at_boot" "$ended" "launch pid ${held_pid:-?} alive: $(rm_pid_alive "$held_pid" && echo yes || echo no); pane ${held_tmux:-?} present: $(rm_tmux has-session -t "=$held_tmux" 2>/dev/null && echo yes || echo no)"

  sc_wait_settled "$SC_PRD" worker.done 240
  rm_wait_until 60 sc_prd_status_in "$SC_PRD" completed || true
  sc_finish "$name"

  sc_common_criteria "$SC_BEFORE" "$SC_AFTER_RESTART" "$SC_FINAL"
  local final_session
  final_session="$(sc_json_get "$SC_FINAL" '.task.session_id')"
  rm_check "bare_shell_not_adopted" "$([[ -n "$held_session" && "$final_session" != "$held_session" ]] && echo 0 || echo 1)" "task session $held_session -> $final_session"
  rm_check "task_not_left_in_progress" "$(sc_task_status_in "$SC_PRD" completed && echo 0 || echo 1)" "task status: $(rm_task_field "$SC_PRD" status)"
  rm_check "prd_completes_unaided" "$(sc_prd_status_in "$SC_PRD" completed && echo 0 || echo 1)" "PRD status: $(rm_prd_status "$SC_PRD")"
  rm_state_end "$SC_BEFORE" "$SC_FINAL"
}

# --------------------------------------------------------------------- main ----

sc_main() {
  local strict=0 only="" list=0 arg
  while (($#)); do
    arg="$1"
    shift
    case "$arg" in
      --strict) strict=1 ;;
      --keep) export RM_KEEP=1 ;;
      --list) list=1 ;;
      --only) only="${1:-}"; shift || true ;;
      --only=*) only="${arg#--only=}" ;;
      -h | --help) sc_usage; return 0 ;;
      *) rm_die "unknown argument: $arg (try --help)" ;;
    esac
  done
  if ((list)); then
    printf '%s\n' "${SC_ALL_STATES[@]}"
    return 0
  fi
  export RM_MODE=measure
  ((strict)) && RM_MODE=strict

  local states=() s known
  if [[ -n "$only" ]]; then
    IFS=, read -r -a states <<<"$only"
    for s in "${states[@]}"; do
      known=0
      for arg in "${SC_ALL_STATES[@]}"; do [[ "$s" == "$arg" ]] && known=1; done
      ((known)) || rm_die "unknown state: $s (known: ${SC_ALL_STATES[*]})"
    done
  else
    states=("${SC_ALL_STATES[@]}")
  fi

  rm_require_tools
  rm_init_root
  rm_install_traps # EXIT/INT/TERM/HUP — the sandbox is ended on every exit path
  printf 'restart matrix (%s mode), records: %s\n' "$RM_MODE" "$RM_REC"

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
  sc_main "$@"
  exit $?
fi
