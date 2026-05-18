#!/usr/bin/env bash
# run-tests.sh — E2E test runner.
#
# Automatically creates a working directory outside the repo for each run so
# test artifacts (isolated daemon data, evidence, run logs) never touch the
# source tree. The directory is deleted on success; kept on failure so you can
# inspect evidence and resume.
#
# Usage:
#   bash scripts/run-tests.sh                       # full run
#   bash scripts/run-tests.sh --surface=api         # filter by surface
#   bash scripts/run-tests.sh --feature=sessions    # filter by feature
#   bash scripts/run-tests.sh --story=TS-042        # single story
#   bash scripts/run-tests.sh --resume-from=TS-042  # resume after a blocker
#   bash scripts/run-tests.sh --workers=4           # override max parallel workers
#   bash scripts/run-tests.sh --serial              # force serial execution
#   bash scripts/run-tests.sh --no-daemon           # skip auto-starting a daemon
#
# Resuming a failed run (reuses its working dir so evidence is preserved):
#   DATAWATCH_TEST_ID=abc123 bash scripts/run-tests.sh --resume-from=TS-042
#
# Keep working dir even on success (for debugging):
#   KEEP_TEST_DIR=1 bash scripts/run-tests.sh
#
# Parallel workers: stories tagged "parallel:ok" run concurrently; others run
# serially. Worker count adapts to CPU/memory load. Override with --workers=N
# or force --serial to disable all concurrency.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
REPO_PARENT=$(cd "$REPO_DIR/.." && pwd)

# --- working directory -------------------------------------------------------
# Each run gets a unique 6-char hex ID so parallel runs on the same filesystem
# don't collide. Set DATAWATCH_TEST_ID to reuse a specific prior run's dir
# (e.g. to resume after a failure).
RUN_ID="${DATAWATCH_TEST_ID:-$(openssl rand -hex 3)}"
TEST_DIR="$REPO_PARENT/datawatch-${RUN_ID}"
mkdir -p "$TEST_DIR"

FAILED=0
DAEMON_PID=""

# --- daemon lifecycle --------------------------------------------------------
start_test_daemon() {
  local test_cfg="$TEST_DIR/config.yaml"
  local tmpl="$REPO_DIR/testdata/datawatch.yaml"

  if [[ ! -f "$tmpl" ]]; then
    echo "Warning: testdata/datawatch.yaml not found; skipping config generation" >&2
    return 0
  fi

  # Generate isolated config from template
  mkdir -p "$TEST_DATA"
  sed \
    -e "s|data_dir: /data|data_dir: $TEST_DATA|g" \
    -e "s|port: 8080|port: $TEST_PORT|g" \
    -e "s|sse_port: 9090|sse_port: $TEST_MCP_PORT|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    "$tmpl" > "$test_cfg"

  # Find the binary
  local binary=""
  if [[ -n "${TEST_BINARY:-}" && -x "$TEST_BINARY" ]]; then
    binary="$TEST_BINARY"
  elif [[ -x "$REPO_DIR/bin/datawatch" ]]; then
    binary="$REPO_DIR/bin/datawatch"
  else
    binary="$(command -v datawatch 2>/dev/null || true)"
  fi

  if [[ -z "$binary" ]]; then
    echo "Error: datawatch binary not found. Set TEST_BINARY or build first." >&2
    return 1
  fi

  echo "Starting test daemon ($binary)..."
  "$binary" start --foreground --config "$test_cfg" >> "$TEST_DIR/daemon.log" 2>&1 &
  DAEMON_PID=$!

  # Poll /api/health up to 30s
  local deadline=$(( $(date +%s) + 30 ))
  local healthy=0
  while [[ $(date +%s) -lt $deadline ]]; do
    if curl -sf "http://127.0.0.1:$TEST_PORT/api/health" > /dev/null 2>&1; then
      healthy=1
      break
    fi
    printf '.'
    sleep 0.5
  done
  echo ""

  if [[ $healthy -eq 0 ]]; then
    echo "Error: daemon did not become healthy within 30s" >&2
    echo "--- last 20 lines of daemon.log ---" >&2
    tail -20 "$TEST_DIR/daemon.log" >&2 || true
    return 1
  fi

  echo "Daemon started (PID $DAEMON_PID) at http://127.0.0.1:$TEST_PORT"
}

stop_test_daemon() {
  if [[ -n "$DAEMON_PID" ]]; then
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
    DAEMON_PID=""
  fi
}

cleanup() {
  stop_test_daemon
  destroy_pool
  if [[ $FAILED -ne 0 || -n "${KEEP_TEST_DIR:-}" ]]; then
    echo ""
    echo "Working dir kept: $TEST_DIR"
    echo "  Resume: DATAWATCH_TEST_ID=$RUN_ID bash scripts/run-tests.sh --resume-from=<story>"
  else
    rm -rf "$TEST_DIR"
  fi
}
trap 'FAILED=$?; cleanup' EXIT

# --- port allocation --------------------------------------------------------
# Ask the OS for a free port on 127.0.0.1. Each call returns a different port
# so parallel runs never collide. Override via env vars if you need fixed ports.
free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); p=s.getsockname()[1]; s.close(); print(p)'
}

# --- exports for story implementations --------------------------------------
export DATAWATCH_TEST_ID="$RUN_ID"
export DATAWATCH_TEST_DIR="$TEST_DIR"
export DATAWATCH_REPO_DIR="$REPO_DIR"
export DATAWATCH_COOKBOOK="$REPO_DIR/docs/testing/master-cookbook.md"

export TEST_RUN_HASH="$$"
export DATAWATCH_TEST_DATA="$TEST_DIR/.datawatch-test-${TEST_RUN_HASH}"
export TEST_PORT="${TEST_PORT:-$(free_port)}"
export TEST_TLS_PORT="${TEST_TLS_PORT:-$(free_port)}"
export TEST_MCP_PORT="${TEST_MCP_PORT:-$(free_port)}"
export TEST_DATA="${TEST_DATA:-${DATAWATCH_TEST_DATA}}"
export TEST_BASE="http://127.0.0.1:$TEST_PORT"

echo "Run ID  : $RUN_ID"
echo "Work dir: $TEST_DIR"
echo "Ports   : http=$TEST_PORT tls=$TEST_TLS_PORT mcp=$TEST_MCP_PORT"
echo ""

# --- argument parsing -------------------------------------------------------
FILTER_SURFACE=""
FILTER_FEATURE=""
FILTER_STORY=""
RESUME_FROM=""
FAIL_FAST=0
SERIAL_MODE=0
NO_DAEMON=0
WORKER_FLAG=""

for arg in "$@"; do
  case "$arg" in
    --surface=*)          FILTER_SURFACE="${arg#*=}" ;;
    --feature=*)          FILTER_FEATURE="${arg#*=}" ;;
    --story=*)            FILTER_STORY="${arg#*=}" ;;
    --resume-from=*)      RESUME_FROM="${arg#*=}" ;;
    --fail-fast*)         FAIL_FAST=1 ;;
    --workers=*)          WORKER_FLAG="${arg#*=}" ;;
    --serial|--no-parallel) SERIAL_MODE=1 ;;
    --no-daemon)          NO_DAEMON=1 ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

export FILTER_SURFACE FILTER_FEATURE FILTER_STORY RESUME_FROM FAIL_FAST

# --- resource monitoring (Linux /proc) --------------------------------------
# Returns CPU usage % (0-100) based on 200ms /proc/stat sample
cpu_pct() {
  if [[ ! -r /proc/stat ]]; then echo 50; return; fi
  local l1 l2
  read -ra l1 < /proc/stat
  sleep 0.2
  read -ra l2 < /proc/stat
  local u=$(( l2[1]+l2[3] - l1[1]-l1[3] ))
  local t=0
  local i
  for i in 1 2 3 4 5 6 7 8; do
    t=$(( t + l2[i] - l1[i] )) 2>/dev/null || true
  done
  [[ $t -le 0 ]] && echo 0 && return
  echo $(( u*100/t ))
}

# Returns free memory % (0-100)
mem_free_pct() {
  if [[ ! -r /proc/meminfo ]]; then echo 50; return; fi
  awk '/^MemAvailable:/{a=$2}/^MemTotal:/{t=$2}END{printf "%d",(t>0?(a*100/t):50)}' /proc/meminfo
}

# Returns suggested worker count based on current resource usage
suggest_workers() {
  local cpu mem ncpu max_w="${MAX_WORKERS:-0}"
  ncpu=$(nproc 2>/dev/null || echo 4)
  [[ $max_w -le 0 ]] && max_w=$(( ncpu < 8 ? ncpu : 8 ))

  cpu=$(cpu_pct)
  mem=$(mem_free_pct)

  if   [[ $cpu -gt 75 || $mem -lt 15 ]]; then echo 1
  elif [[ $cpu -lt 40 && $mem -gt 30 ]]; then echo $max_w
  else echo $(( max_w / 2 > 2 ? max_w / 2 : 2 ))
  fi
}

# --- worker pool (named-pipe semaphore) -------------------------------------
POOL_FIFO=""
POOL_FD=9

init_pool() {
  local n=$1
  POOL_FIFO=$(mktemp -u --suffix=.pool)
  mkfifo "$POOL_FIFO"
  eval "exec $POOL_FD<>\"$POOL_FIFO\""
  local _i
  for _i in $(seq "$n"); do printf 'x' >&$POOL_FD; done
}

acquire_worker() {
  read -r -N1 -u $POOL_FD _tok
}

release_worker() {
  printf 'x' >&$POOL_FD
}

destroy_pool() {
  eval "exec $POOL_FD>&-" 2>/dev/null || true
  [[ -n "$POOL_FIFO" ]] && rm -f "$POOL_FIFO" 2>/dev/null || true
  POOL_FIFO=""
}

# --- parallel job tracking --------------------------------------------------
declare -A PAR_STORY=()  # pid -> story_id (bash 5.3: must init with =() for set -u compat)
declare -a PAR_PIDS=()   # active pids
RESULT_DIR="$TEST_DIR/par-results"
mkdir -p "$RESULT_DIR"
CURRENT_WORKERS=2
MAX_WORKERS=0

# Run a story as a background parallel job
launch_parallel() {
  local story_id="$1" script="$2"

  # Adapt worker count based on current resource pressure
  local suggested
  suggested=$(suggest_workers)
  if [[ $suggested -ne $CURRENT_WORKERS ]]; then
    echo "  [workers] cpu=$(cpu_pct)% mem=$(mem_free_pct)% free → adjusting $CURRENT_WORKERS→$suggested"
    if [[ $suggested -gt $CURRENT_WORKERS ]]; then
      # Add extra tokens to the semaphore for ramp-up
      local add=$(( suggested - CURRENT_WORKERS ))
      local _j
      for _j in $(seq "$add"); do printf 'x' >&$POOL_FD; done
    fi
    CURRENT_WORKERS=$suggested
  fi

  acquire_worker  # blocks until slot available

  local result_file="$RESULT_DIR/${story_id}.result"
  (
    set +e
    RESULT=""
    CURRENT_STORY="$story_id"
    mkdir -p "$EVIDENCE_DIR/$story_id"
    # shellcheck source=/dev/null
    source "$script"
    printf '%s' "${RESULT:-fail}" > "$result_file"
    release_worker
  ) &
  local pid=$!
  PAR_PIDS+=("$pid")
  PAR_STORY[$pid]="$story_id"
}

# Wait for all parallel jobs and collect results
drain_parallel() {
  [[ ${#PAR_PIDS[@]} -eq 0 ]] && return
  local pid sid rf r
  for pid in "${PAR_PIDS[@]}"; do
    wait "$pid" 2>/dev/null || true
    sid="${PAR_STORY[$pid]:-}"
    [[ -z "$sid" ]] && continue
    rf="$RESULT_DIR/${sid}.result"
    r="fail"
    [[ -f "$rf" ]] && r=$(cat "$rf")
    case "$r" in
      pass) ((PASS++)); echo "✓ $sid" ;;
      skip) ((SKIP++)); echo "- $sid (skip)" ;;
      *)    ((FAIL++)); echo "✗ $sid"
            if [[ $FAIL_FAST -eq 1 ]]; then
              FAILED=1
            fi
            ;;
    esac
  done
  PAR_PIDS=()
  unset PAR_STORY; declare -gA PAR_STORY=()
}

# --- tag helpers ------------------------------------------------------------
story_tags() {
  grep -m1 "^# tags:" "$1" 2>/dev/null | sed 's/^# tags: *//' || true
}

is_parallel_ok() {
  echo "$1" | grep -q "parallel:ok"
}

# --- story runner -----------------------------------------------------------
# Story implementations live in scripts/test-stories/ as individual scripts
# named TS-NNN.sh, sourced in order. Each script sets RESULT=pass|fail|skip
# and writes evidence to $DATAWATCH_TEST_DIR/evidence/TS-NNN/.

STORIES_DIR="$SCRIPT_DIR/test-stories"
PASS=0; FAIL=0; SKIP=0
EVIDENCE_DIR="$TEST_DIR/evidence"
mkdir -p "$EVIDENCE_DIR"

if [[ ! -d "$STORIES_DIR" ]]; then
  echo "No story implementations yet. Add scripts to scripts/test-stories/TS-NNN.sh"
  echo "Stories are defined in: $DATAWATCH_COOKBOOK"
  exit 0
fi

# --- daemon startup ---------------------------------------------------------
if [[ $NO_DAEMON -eq 0 ]]; then
  if curl -sf "http://127.0.0.1:$TEST_PORT/api/health" > /dev/null 2>&1; then
    echo "Using existing daemon at $TEST_BASE"
    # Discover actual MCP SSE port from the daemon's config so TS-624 and
    # other MCP stories hit the right port instead of the default TEST_PORT+1.
    _mcp_port=$(curl -s "http://127.0.0.1:$TEST_PORT/api/config" \
      -H "Authorization: Bearer ${TEST_TOKEN}" 2>/dev/null \
      | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("mcp",{}).get("sse_port",""))' \
      2>/dev/null || true)
    if [[ -n "$_mcp_port" && "$_mcp_port" =~ ^[0-9]+$ ]]; then
      export TEST_MCP_PORT="$_mcp_port"
      echo "  MCP SSE port: $TEST_MCP_PORT (from daemon config)"
    fi
  else
    start_test_daemon
  fi
else
  echo "Skipping daemon start (--no-daemon); assuming daemon at $TEST_BASE"
fi
echo ""

# --- worker pool setup ------------------------------------------------------
SERIAL=${SERIAL_MODE:-0}
[[ -n "$WORKER_FLAG" ]] && MAX_WORKERS=$WORKER_FLAG

if [[ $SERIAL -eq 0 ]]; then
  # Check if any story is tagged parallel:ok
  HAVE_PARALLEL=0
  for _s in "$STORIES_DIR"/TS-*.sh; do
    [[ -f "$_s" ]] || continue
    grep -qm1 "parallel:ok" "$_s" 2>/dev/null && HAVE_PARALLEL=1 && break
  done
  if [[ $HAVE_PARALLEL -eq 1 ]]; then
    CURRENT_WORKERS=$(suggest_workers)
    echo "Parallel mode: initial workers=$CURRENT_WORKERS (max=${MAX_WORKERS:-auto})"
    init_pool "$CURRENT_WORKERS"
  else
    SERIAL=1
  fi
fi

if [[ $SERIAL -eq 1 ]]; then
  echo "Serial mode: all stories run sequentially"
fi
echo ""

# --- main story loop --------------------------------------------------------
PAST_RESUME=0
[[ -z "$RESUME_FROM" ]] && PAST_RESUME=1

for story_script in "$STORIES_DIR"/TS-*.sh; do
  [[ -f "$story_script" ]] || continue
  story_id=$(basename "$story_script" .sh)

  # resume-from: skip stories before the resume point
  if [[ $PAST_RESUME -eq 0 ]]; then
    [[ "$story_id" == "$RESUME_FROM" ]] && PAST_RESUME=1 || continue
  fi

  # story filter
  [[ -n "$FILTER_STORY" && "$story_id" != "$FILTER_STORY" ]] && continue

  # surface/feature filter from tags
  tags=$(story_tags "$story_script")
  if [[ -n "$FILTER_SURFACE" ]] && ! echo "$tags" | grep -q "surface:${FILTER_SURFACE}"; then
    continue
  fi
  if [[ -n "$FILTER_FEATURE" ]] && ! echo "$tags" | grep -q "feature:${FILTER_FEATURE}"; then
    continue
  fi

  if [[ $SERIAL -eq 0 ]] && is_parallel_ok "$tags"; then
    launch_parallel "$story_id" "$story_script"
  else
    # Serial story — drain any outstanding parallel jobs first
    drain_parallel

    # Bail early if --fail-fast triggered in drain_parallel
    if [[ $FAIL_FAST -eq 1 && $FAILED -ne 0 ]]; then
      echo "Stopping at first failure (--fail-fast)."
      exit 1
    fi

    RESULT=""
    CURRENT_STORY="$story_id"
    mkdir -p "$EVIDENCE_DIR/$story_id"
    # shellcheck source=/dev/null
    source "$story_script"

    case "${RESULT:-fail}" in
      pass) ((PASS++)); echo "✓ $story_id" ;;
      skip) ((SKIP++)); echo "- $story_id (skip)" ;;
      fail|*)
        ((FAIL++))
        echo "✗ $story_id"
        if [[ $FAIL_FAST -eq 1 ]]; then
          echo "Stopping at first failure (--fail-fast)."
          drain_parallel
          FAILED=1
          exit 1
        fi
        ;;
    esac
  fi
done

drain_parallel
destroy_pool

# --- bail out if --fail-fast was triggered during drain_parallel ------------
if [[ $FAIL_FAST -eq 1 && $FAILED -ne 0 ]]; then
  echo ""
  echo "Stopped at first failure (--fail-fast)."
  echo "Results: $PASS passed  $FAIL failed  $SKIP skipped"
  exit 1
fi

# --- summary ----------------------------------------------------------------
echo ""
echo "Results: $PASS passed  $FAIL failed  $SKIP skipped"
if [[ $SERIAL -eq 0 ]]; then
  echo "Workers: adaptive (peak ~$(suggest_workers), cpu=$(cpu_pct)% mem=$(mem_free_pct)% free)"
fi

if [[ $FAIL -gt 0 ]]; then
  FAILED=1
  exit 1
fi
