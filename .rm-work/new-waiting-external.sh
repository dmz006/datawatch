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

