#!/usr/bin/env bash
# Model/effort tiering eval driver (Claude only).
#
# Fixtures: testdata/eval/model-tiering/  (see its README.md)
# State:    .datawatch/eval/model-tiering/<run-id>/state.json  (gitignored)
# Results:  docs/testing/runs/<YYYY-MM-DD>-model-tiering/results.{json,md}
#
# JSON / transcript / scoring / report logic lives in
# scripts/eval_model_tiering.py (stdlib only, run with `python3 -I`). This
# wrapper owns argument dispatch, timestamped phase lines and, for the
# verification touchpoint, the capture/restore trap around the global
# autonomous config — the ONLY place this driver changes daemon global config.
#
# Daemon: DW_BASE (default https://127.0.0.1:8443, the local daemon),
# DW_BASE_TOKEN (optional Bearer), DW_BASE_INSECURE (default 1 = accept the
# local self-signed cert, same as scripts/release-smoke.sh external mode).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PY_MOD="$REPO/scripts/eval_model_tiering.py"
SELF="scripts/eval-model-tiering.sh"
DEFAULT_VERIFY_TIERS="default,haiku-low,opus-high"

ts() { date '+%H:%M:%S'; }
phase() { echo "[$(ts)] $*"; }
py() { python3 -I "$PY_MOD" "$@"; }

# _dw_ppid prints the parent pid of a live pid (robust to spaces in comm).
_dw_ppid() {
  local s rest
  s=$(</proc/$1/stat 2>/dev/null) || return 1
  rest="${s##*) }"
  set -- $rest
  # fields after ")": state ppid ...
  printf '%s\n' "${2:-}"
}

# _dw_is_descendant <pid> <ancestor> — true iff <pid> is <ancestor> or a
# descendant of it (walks /proc/<pid>/stat upward, bounded).
_dw_is_descendant() {
  local h="$1" anc="$2" np i=0
  while [[ "$h" -gt 1 ]] 2>/dev/null && ((i < 64)); do
    [[ "$h" == "$anc" ]] && return 0
    np=$(_dw_ppid "$h") || return 1
    [[ -n "$np" ]] || return 1
    h="$np"
    i=$((i + 1))
  done
  return 1
}

usage() {
  cat <<EOF
Usage: $SELF <subcommand> [options]

Common
  preflight            --run-id ID   Read-only: daemon reachable, claude-code available, every execution/
                                     decompose tier valid against the daemon's claude models/efforts; records
                                     the live default and the current verification_backend/model/effort as
                                     *_at_start in state.json.
  report               --run-id ID | --results PATH   Render results.md next to results.json.
  cleanup              --run-id ID [--offline]   Remove cells/; confirm no scratch PRDs or global-config drift.
  self-test                          Fully offline: transcript totals/cost, report rendering, decompose
                                     scoring, verify-tier capture/restore trap.

Execution touchpoint (no global config writes)
  setup                --run-id ID [--tiers a,b]   Fresh git-archive snapshot per cell (+ git init, baseline commit).
  create-prd           --run-id ID [--dry-run] [--tiers a,b]   One scratch Automaton '[eval] model-tiering ID',
                                     one task per cell, per-task LLM pinned; left at needs_review.
  run                  --run-id ID --prd PRD [--timeout S] [--poll S]   Approve + run ONLY that scratch PRD.
  collect              --run-id ID   Verify each cell, price transcripts, write results.json.

Decompose touchpoint (no global config writes)
  decompose-run        --run-id ID [--tiers a,b] [--timeout S]   One scratch decompose-only Automaton per tier,
                                     scored against decompose/ground-truth-structure.json, then cancelled.
  decompose-self-test                Offline scoring test.

Verification touchpoint (the ONLY global-config writer, capture/restore guarded)
  verify-tier-run      --run-id ID [--tiers ${DEFAULT_VERIFY_TIERS}] [--task-type analysis]
                       [--exec-tier sonnet-medium] [--timeout S]
                                     Sequentially, per tier: capture -> set -> one task -> restore -> verify.
                                     'default' runs the task with config untouched. Requires preflight.
  verify-tier-restore  --run-id ID   Manual recovery: restore verification_* to the captured value.
  verify-tier-self-test              Offline: mocked config get/set, asserts the trap restores on success,
                                     task error, unexpected error and SIGTERM, and fails loudly when a
                                     restore does not stick.

Every subcommand accepts --help.
EOF
}

# Pull --run-id out of an arg list (for messages); python validates it.
arg_value() {
  local want=$1; shift
  while [[ $# -gt 0 ]]; do
    if [[ $1 == "$want" ]]; then echo "${2:-}"; return 0; fi
    shift
  done
  echo ""
}

# --------------------------------------------------------------------------
# Verification touchpoint: guarded capture/restore of global config
# --------------------------------------------------------------------------
# State for the per-tier subshell (globals so the traps can see them).
VT_RUN_ID=""
VT_RESTORE_DONE=0
VT_ARMED=0

verify_tier_restore_now() {
  # Restore + read-back assertion (python exits 3 with a loud message naming
  # the exact manual fix if the read-back does not equal restore_to).
  local rc=0
  if [[ -n ${EVAL_MOCK_CONFIG:-} && ${EVAL_SELFTEST_DROP_RESTORE:-0} == 1 ]]; then
    EVAL_MOCK_PUT_DROP=1 py vt-restore --run-id "$VT_RUN_ID" || rc=$?
  else
    py vt-restore --run-id "$VT_RUN_ID" || rc=$?
  fi
  if [[ $rc -eq 0 ]]; then
    VT_RESTORE_DONE=1
    phase "verify-tier: global config restored and verified"
    return 0
  fi
  echo "[$(ts)] verify-tier: RESTORE FAILED (rc=$rc) — see the message above; global config may be changed" >&2
  return 3
}

verify_tier_on_exit() {
  local rc=$?
  trap - EXIT
  if [[ $VT_ARMED == 1 && $VT_RESTORE_DONE != 1 ]]; then
    echo "[$(ts)] verify-tier: exit (rc=$rc) before restore — restoring global config now" >&2
    verify_tier_restore_now || rc=3
    [[ $rc -eq 0 ]] && rc=1   # an unplanned exit is never success
  fi
  exit "$rc"
}

verify_tier_on_signal() {
  echo "[$(ts)] verify-tier: caught SIG$1 — restoring global config before exit" >&2
  exit 130   # EXIT trap performs the restore
}

verify_tier_on_err() {
  echo "[$(ts)] verify-tier: unexpected error (line $1, rc=$2) — EXIT trap will restore global config" >&2
}

# One non-default tier. Runs as its own fresh process (`$SELF __vt-one ...`),
# never inside a `|| rc=$?` context: bash ignores `set -e` for everything run
# under `||`/`&&`/`if`, which would stop an unexpected error from reaching the
# EXIT trap. A fresh process gets a clean `set -Eeuo pipefail`.
verify_tier_one() {
  set -Eeuo pipefail
  VT_RUN_ID=$1
  local tier=$2 task_type=$3 exec_tier=$4 timeout=$5
  VT_RESTORE_DONE=0
  VT_ARMED=0
  phase "verify-tier[$tier]: capture current global verification config"
  # Capture first, before any trap or change: if it fails nothing was changed.
  py vt-capture --run-id "$VT_RUN_ID" --tier "$tier"
  trap verify_tier_on_exit EXIT
  trap 'verify_tier_on_signal INT' INT
  trap 'verify_tier_on_signal TERM' TERM
  trap 'verify_tier_on_err "$LINENO" "$?"' ERR
  VT_ARMED=1
  phase "verify-tier[$tier]: set global verification config to tier"
  py vt-set --run-id "$VT_RUN_ID" --tier "$tier"
  if [[ -n ${EVAL_MOCK_CONFIG:-} && ${EVAL_SELFTEST_CRASH_AFTER_SET:-0} == 1 ]]; then
    phase "verify-tier[$tier]: self-test simulated crash after set"
    false
  fi
  local task_rc=0
  phase "verify-tier[$tier]: run one '$task_type' task (execution tier $exec_tier)"
  py vt-task --run-id "$VT_RUN_ID" --tier "$tier" --task-type "$task_type" --exec-tier "$exec_tier" \
    --timeout "$timeout" || task_rc=$?
  phase "verify-tier[$tier]: task finished (rc=$task_rc); restoring global config"
  verify_tier_restore_now || exit 3
  trap - EXIT INT TERM ERR
  VT_ARMED=0
  return "$task_rc"
}

verify_tier_run() {
  local tiers="$DEFAULT_VERIFY_TIERS" task_type=analysis exec_tier=sonnet-medium timeout=7200
  VT_RUN_ID=""
  while [[ $# -gt 0 ]]; do
    case $1 in
      --run-id) VT_RUN_ID=$2; shift 2 ;;
      --tiers) tiers=$2; shift 2 ;;
      --task-type) task_type=$2; shift 2 ;;
      --exec-tier) exec_tier=$2; shift 2 ;;
      --timeout) timeout=$2; shift 2 ;;
      -h|--help) usage; return 0 ;;
      *) echo "verify-tier-run: unknown option $1" >&2; return 2 ;;
    esac
  done
  [[ -n $VT_RUN_ID ]] || { echo "verify-tier-run: --run-id required" >&2; return 2; }
  # Pre-check: preflight start values exist and live config has not drifted.
  phase "verify-tier-run: pre-check global config equals preflight start"
  py vt-final-check --run-id "$VT_RUN_ID"
  local overall=0 tier rc
  IFS=',' read -r -a tier_list <<<"$tiers"
  for tier in "${tier_list[@]}"; do   # SEQUENTIAL: one global config at a time
    tier=${tier// /}
    [[ -n $tier ]] || continue
    if [[ $tier == default ]]; then
      phase "verify-tier[default]: no config change; run one '$task_type' task"
      rc=0
      py vt-task --run-id "$VT_RUN_ID" --tier default --task-type "$task_type" --exec-tier "$exec_tier" \
        --timeout "$timeout" || rc=$?
    else
      rc=0
      bash "$REPO/scripts/eval-model-tiering.sh" __vt-one "$VT_RUN_ID" "$tier" "$task_type" "$exec_tier" "$timeout" || rc=$?
    fi
    if [[ $rc -eq 3 ]]; then
      echo "[$(ts)] verify-tier-run: aborting remaining tiers — global config restore failed for $tier" >&2
      overall=3
      break
    fi
    [[ $rc -ne 0 && $overall -eq 0 ]] && overall=$rc
    phase "verify-tier[$tier]: done (rc=$rc)"
  done
  # Last-line safety net, independent of the per-tier restores.
  phase "verify-tier-run: final check global config equals preflight start"
  py vt-final-check --run-id "$VT_RUN_ID" || return 3
  return "$overall"
}

verify_tier_restore() {
  VT_RUN_ID=$(arg_value --run-id "$@")
  [[ ${1:-} == -h || ${1:-} == --help ]] && { usage; return 0; }
  [[ -n $VT_RUN_ID ]] || { echo "verify-tier-restore: --run-id required" >&2; return 2; }
  verify_tier_restore_now
}

# --------------------------------------------------------------------------
# verify-tier-self-test: offline, mocked config get/set
# --------------------------------------------------------------------------
verify_tier_self_test() {
  [[ ${1:-} == -h || ${1:-} == --help ]] && { usage; return 0; }
  local run_id="selftest-vt-$$-$RANDOM"
  local rdir="$REPO/.datawatch/eval/model-tiering/$run_id"
  local tmp
  tmp=$(mktemp -d)
  export EVAL_MOCK_CONFIG="$tmp/config.json"
  local orig='{"verification_backend":"claude-code","verification_model":"qwen3.8:27b","verification_effort":"","enabled":true}'
  mkdir -p "$rdir"
  echo "$orig" >"$EVAL_MOCK_CONFIG"
  python3 -I -c 'import json,sys; json.dump({"run_id":sys.argv[2],"verification_backend_at_start":"claude-code",
"verification_model_at_start":"qwen3.8:27b","verification_effort_at_start":""},open(sys.argv[1],"w"))' \
    "$rdir/state.json" "$run_id"
  local fails=0
  check_restored() {  # $1 label
    if python3 -I -c 'import json,sys; a=json.load(open(sys.argv[1])); b=json.loads(sys.argv[2]); sys.exit(0 if a==b else 1)' \
      "$EVAL_MOCK_CONFIG" "$orig"; then
      phase "  PASS $1: config restored exactly"
    else
      phase "  FAIL $1: config is $(cat "$EVAL_MOCK_CONFIG")"; fails=$((fails + 1))
    fi
  }
  expect_rc() {  # $1 label $2 want(op) $3 got
    if [[ $2 == nonzero && $3 -ne 0 ]] || [[ $2 != nonzero && $3 -eq $2 ]]; then
      phase "  PASS $1: rc=$3"
    else
      phase "  FAIL $1: rc=$3 want $2"; fails=$((fails + 1))
    fi
  }
  local rc out

  phase "vt-self-test 1: task succeeds"
  rc=0; EVAL_MOCK_TASK=ok verify_tier_run --run-id "$run_id" --tiers haiku-low >/dev/null 2>&1 || rc=$?
  expect_rc "task ok" 0 $rc; check_restored "task ok"

  phase "vt-self-test 2: task throws partway (several tiers, sequential)"
  rc=0; EVAL_MOCK_TASK=fail verify_tier_run --run-id "$run_id" --tiers default,haiku-low,opus-high >/dev/null 2>&1 || rc=$?
  expect_rc "task error" nonzero $rc; check_restored "task error"

  phase "vt-self-test 3: unexpected error right after the config change (ERR/EXIT trap)"
  rc=0; EVAL_SELFTEST_CRASH_AFTER_SET=1 EVAL_MOCK_TASK=ok verify_tier_run --run-id "$run_id" --tiers opus-high >/dev/null 2>&1 || rc=$?
  expect_rc "crash after set" nonzero $rc; check_restored "crash after set"

  phase "vt-self-test 4: SIGTERM while the task is running"
  rc=0
  EVAL_MOCK_TASK=sleep EVAL_MOCK_PIDFILE="$tmp/task.pid" verify_tier_run --run-id "$run_id" --tiers sonnet-high >"$tmp/sig.log" 2>&1 &
  local bgpid=$! i
  for i in $(seq 1 100); do [[ -s $tmp/task.pid ]] && break; sleep 0.1; done
  if python3 -I -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["verification_model"]=="sonnet" else 1)' "$EVAL_MOCK_CONFIG"; then
    phase "  PASS signal: tier config was applied while the task ran"
  else
    phase "  FAIL signal: tier config not applied before the task"; fails=$((fails + 1))
  fi
  # TERM the per-tier process (child of the bg job) and the mock task itself.
  local sub
  for sub in $(ps -o pid= --ppid "$bgpid" 2>/dev/null); do kill -TERM "$sub" 2>/dev/null || true; done
  # task.pid was written by our own bg job above; before signaling, verify
  # the pid on disk is still alive and still a descendant of that job —
  # never kill a bare pid from a file without an ownership check.
  local taskpid
  taskpid=$(cat "$tmp/task.pid" 2>/dev/null || true)
  if [[ -n "$taskpid" ]] && kill -0 "$taskpid" 2>/dev/null && _dw_is_descendant "$taskpid" "$bgpid"; then
    kill -TERM "$taskpid" 2>/dev/null || true
  elif [[ -n "$taskpid" ]]; then
    echo "[selftest] task.pid pid ${taskpid:-?} is not a descendant of our bg job $bgpid; not killing" >&2
  fi
  wait "$bgpid" || rc=$?
  expect_rc "SIGTERM" nonzero $rc; check_restored "SIGTERM"
  grep -q 'caught SIGTERM' "$tmp/sig.log" && phase "  PASS signal: TERM trap fired" \
    || { phase "  FAIL signal: TERM trap did not fire"; sed 's/^/    /' "$tmp/sig.log"; fails=$((fails + 1)); }

  phase "vt-self-test 5: restore that does not stick fails loudly with exit 3"
  rc=0; out=$(EVAL_SELFTEST_DROP_RESTORE=1 EVAL_MOCK_TASK=ok verify_tier_run --run-id "$run_id" --tiers fable-low 2>&1) || rc=$?
  expect_rc "restore failure" 3 $rc
  if grep -q 'GLOBAL CONFIG RESTORE FAILED' <<<"$out" && grep -q "verify-tier-restore --run-id $run_id" <<<"$out"; then
    phase "  PASS restore failure: loud message names the manual fix command"
  else
    phase "  FAIL restore failure: message missing"; fails=$((fails + 1))
  fi
  phase "vt-self-test 6: a fresh run refuses to change anything while drift is present"
  rc=0; EVAL_MOCK_TASK=ok verify_tier_run --run-id "$run_id" --tiers haiku-low >/dev/null 2>&1 || rc=$?
  expect_rc "refuse on drift" nonzero $rc
  if python3 -I -c 'import json,sys; sys.exit(0 if json.load(open(sys.argv[1]))["verification_model"]=="fable" else 1)' "$EVAL_MOCK_CONFIG"; then
    phase "  PASS refuse on drift: config left untouched (still the drifted value)"
  else
    phase "  FAIL refuse on drift: config was changed"; fails=$((fails + 1))
  fi
  phase "vt-self-test 7: manual verify-tier-restore recovers"
  rc=0; verify_tier_restore --run-id "$run_id" >/dev/null 2>&1 || rc=$?
  expect_rc "manual restore" 0 $rc; check_restored "manual restore"
  rc=0; py vt-final-check --run-id "$run_id" >/dev/null 2>&1 || rc=$?
  expect_rc "final check after recovery" 0 $rc

  unset EVAL_MOCK_CONFIG
  rm -rf "$tmp" "$rdir"
  if [[ $fails -ne 0 ]]; then
    phase "VERIFY-TIER-SELF-TEST FAILED ($fails)"
    return 1
  fi
  phase "VERIFY-TIER-SELF-TEST OK"
}

# --------------------------------------------------------------------------
main() {
  local cmd=${1:-}
  if [[ $cmd == __vt-one ]]; then   # internal: see verify_tier_one (deliberately not under ||)
    shift
    verify_tier_one "$@"
    exit $?
  fi
  [[ -n $cmd ]] || { usage; exit 2; }
  shift || true
  local T rc=0
  T=$(date +%s)
  phase "start $cmd"
  case $cmd in
    -h|--help|help) usage ;;
    preflight|setup|create-prd|run|collect|decompose-run|decompose-self-test|report|cleanup)
      py "$cmd" "$@" || rc=$? ;;
    verify-tier-run) verify_tier_run "$@" || rc=$? ;;
    verify-tier-restore) verify_tier_restore "$@" || rc=$? ;;
    verify-tier-self-test) verify_tier_self_test "$@" || rc=$? ;;
    self-test)
      if [[ ${1:-} == -h || ${1:-} == --help ]]; then usage
      else
        phase "self-test: python (transcript, report, decompose scoring)"
        py self-test || rc=$?
        phase "self-test: verify-tier capture/restore"
        verify_tier_self_test || rc=$?
        bash -n "$REPO/scripts/eval-model-tiering.sh" || rc=$?
        [[ $rc -eq 0 ]] && phase "SELF-TEST OK" || phase "SELF-TEST FAILED"
      fi ;;
    *) echo "unknown subcommand: $cmd" >&2; usage >&2; rc=2 ;;
  esac
  phase "done $cmd rc=$rc ($(($(date +%s) - T))s)"
  exit "$rc"
}

main "$@"
