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
#   bash scripts/run-tests.sh --stories=TS-042,TS-056  # comma-separated set
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
# External-daemon mode (2026-10-13) — run stories against a daemon you
# already have running (a sandbox, a CI-deployed kind/k8s instance, a
# persistent dev daemon) instead of starting a fresh one. Mirrors
# scripts/release-smoke.sh's identical mechanism — same three
# variables, same meaning. See docs/howto/testing-against-a-running-daemon.md.
#   DW_BASE=https://127.0.0.1:18443 DW_BASE_TOKEN=your-token \
#     bash scripts/run-tests.sh --story=TS-042
# NOT the same as plain --no-daemon: that only reuses a daemon this
# SAME harness already started on TEST_PORT/TEST_TLS_PORT (for
# resuming mid-run); it has no way to target arbitrary ports.
#
# Parallel workers: stories tagged "parallel:ok" run concurrently; others run
# serially. Worker count adapts to CPU/memory load. Override with --workers=N
# or force --serial to disable all concurrency.

set -euo pipefail

# Ensure user-local binaries (kind, kubectl, rtk, etc.) are in PATH.
export PATH="${HOME}/.local/bin:${PATH}"

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
REPO_PARENT=$(cd "$REPO_DIR/.." && pwd)

# --- working directory -------------------------------------------------------
# Each run gets a unique 6-char hex ID so parallel runs on the same filesystem
# don't collide. Set DATAWATCH_TEST_ID to reuse a specific prior run's dir
# (e.g. to resume after a failure).
RUN_ID="${DATAWATCH_TEST_ID:-$(openssl rand -hex 3)}"
# Exported before the lifecycle library below: td_install_traps may re-run
# this exact command line once (when SIGINT was inherited as ignored), and
# the re-exec must land in the same run dir as the original start.
export DATAWATCH_TEST_ID="$RUN_ID"
TEST_DIR="$REPO_PARENT/datawatch-${RUN_ID}"
mkdir -p "$TEST_DIR"

# --- shared daemon lifecycle (test-daemon-lib) -------------------------------
# Every daemon this harness starts must end -- the daemon itself and every
# session it started -- on every exit path: normal exit, failure and
# interrupt. TD_STATE_DIR lives inside this run's directory so stories
# (even ones running in worker subshells) record their daemons in the same
# files the runner's traps read back, and a later run can find a stale run's
# daemon through those same records instead of searching by name.
export TD_STATE_DIR="$TEST_DIR/.daemon-state"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/test-daemon-lib.sh"
# Run from the library's EXIT/INT/TERM/HUP handlers after td_stop_all.
TD_EXTRA_CLEANUP=cleanup
# A set -e death must still count as a failure for cleanup()'s keep-dir
# decision (the library's handler preserves the original exit status).
trap 'FAILED=1' ERR
td_install_traps "$@"

# --- leftover-daemon guard (start-of-run baseline) ---------------------------
# Snapshot the live test daemons before the tests start anything else, so
# daemons already alive when the run began form the baseline. At the end
# of the run -- on every exit path -- cleanup() runs the guard's `check`
# against this file, after every recorded daemon has been stopped and
# before the run dir goes away, and a live test daemon that is not in the
# baseline means this job started it and left it behind: the run fails
# with the guard's report, all stories having passed or not.
GUARD_SH="$SCRIPT_DIR/check-no-leftover-test-daemons.sh"
LEFTOVER_BASELINE="$TEST_DIR/leftover-daemon-baseline"
# A re-used (resume) run dir must not carry this marker over from the
# earlier run -- a stale one would flag, or suppress the check of, this run.
rm -f "$LEFTOVER_BASELINE" "$TEST_DIR/leftover-daemons-FAIL" "$TEST_DIR/.leftover-check-done" 2>/dev/null || true
bash "$GUARD_SH" snapshot "$LEFTOVER_BASELINE" || \
  echo "  [cleanup] warning: could not take the leftover-daemon snapshot -- the end-of-run check will report every live test daemon" >&2

FAILED=0
DAEMON_PID=""

# Prune stale artifacts left by runs that were SIGKILL'd or stories run
# standalone. A SIGKILL'd run's EXIT trap (cleanup(), below) never fires, so
# its daemon process is orphaned — killing the daemon and removing its
# directory only (without the other) either leaks a process forever or
# yanks the directory out from under a still-running daemon. Do both,
# daemon first. Anything older than 1 day (3 for the workspace TEST_DIR
# copies — cleanup() deliberately keeps those on failure for --resume-from,
# so give a real resume window before auto-reaping them) is fair game;
# active runs are always recent.
# _kill_orphan_daemon_for <dir> -- end the daemon a stale run left behind.
# Only a PID that a run recorded for itself may be signalled: read the run's
# own daemon.pid (written by start_test_daemon) and the shared state files
# in its TD_STATE_DIR (.daemon-state/*.rec). Before signalling, the live
# /proc/<pid>/cmdline must still carry that run's recorded config path -- a
# recycled PID never is. With no record at all nothing is touched; an
# unrecorded process is left running, with a warning. No process-name search
# of any kind; this never touches a multiplexer server.
_kill_orphan_for_pid() {
  local pid="$1" marker="$2" where="$3" cmd
  [[ "$pid" =~ ^[0-9]+$ && -n "$marker" ]] || return 0
  cmd=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)
  if [[ -n "$cmd" && "$cmd" == *"$marker"* ]]; then
    kill "$pid" 2>/dev/null || true
  else
    echo "  [cleanup] $where records pid $pid with marker '$marker' but its live cmdline no longer carries it; not signalling it" >&2
  fi
}
_kill_orphan_daemon_for() {
  local dir="$1" pidfile pid marker f
  pidfile="${dir}/daemon.pid"
  if [[ -f "$pidfile" ]]; then
    pid=$(sed -n '1p' "$pidfile" 2>/dev/null || true)
    marker=$(sed -n '2p' "$pidfile" 2>/dev/null || true)
    _kill_orphan_for_pid "$pid" "$marker" "$pidfile"
  elif [[ -d "$dir" ]] && [[ ! -e "${dir}/.daemon-state" ]]; then
    echo "  [cleanup] $dir: no daemon record (no daemon.pid, no shared state dir); any daemon it ran is not recorded here and is left alone" >&2
  fi
  # Shared state files recorded through the lifecycle library (covers second
  # and peer daemons that stories record against the run's state dir; this
  # run's own story daemons live under the per-story owner subdirs set by
  # the parallel workers).
  for f in "$dir"/.daemon-state/*.rec "$dir"/.daemon-state/*/*.rec; do
    [[ -f "$f" ]] || continue
    pid=$(sed -n 's/^pid=//p' "$f" | sed -n 1p)
    marker=$(sed -n 's/^marker=//p' "$f" | sed -n 1p)
    _kill_orphan_for_pid "$pid" "$marker" "$f"
    rm -f "$f" 2>/dev/null || true
  done
  return 0
}
for d in /tmp/dw-docker-sim-* /tmp/dw-test-[0-9]* /tmp/dw-test-sandbox-*; do
  [[ -e "$d" ]] || continue
  find "$d" -maxdepth 0 -mtime +1 2>/dev/null | grep -q . || continue
  _kill_orphan_daemon_for "$d"
  rm -rf "$d" 2>/dev/null || true
done
for d in "$REPO_PARENT"/datawatch-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]; do
  [[ -e "$d" ]] || continue
  find "$d" -maxdepth 0 -mtime +3 2>/dev/null | grep -q . || continue
  _kill_orphan_daemon_for "$d"
  rm -rf "$d" 2>/dev/null || true
done

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
    -e "s|default_project_dir: /data/projects|default_project_dir: $TEST_DATA/projects|g" \
    -e "s|port: 8080|port: $TEST_PORT|g" \
    -e "s|tls_port: 8443|tls_port: $TEST_TLS_PORT|g" \
    -e "s|sse_port: 9090|sse_port: $TEST_MCP_PORT|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"${TEST_TOKEN:-}\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:${TEST_DNS_PORT}\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:${TEST_WEBHOOK_PORT}|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:${TEST_NTFY_PORT}|g" \
    "$tmpl" > "$test_cfg"
  # Also write to TEST_DATA/config.yaml so cli_test (--config $TEST_DATA/config.yaml) works
  cp "$test_cfg" "$TEST_DATA/config.yaml"

  # Pre-seed a test skill so TS-065 (skill_load) can run without a git registry.
  # The skills store is loaded from disk at daemon startup; writing before first start
  # ensures the in-memory index includes the skill.
  local skill_dir="$TEST_DATA/skills/local/dw-test-skill"
  mkdir -p "$skill_dir"
  cat > "$skill_dir/SKILL.md" <<'SKILLMD'
# dw-test-skill

Minimal E2E test skill used to validate skill discovery and the `skill_load` MCP tool.
Do not use in production.

## Usage

```
skill_load name=dw-test-skill
```
SKILLMD
  local now
  now=$(python3 -c 'import datetime; print(datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"))' 2>/dev/null || echo "2026-01-01T00:00:00Z")
  python3 - "$TEST_DATA/skills.json" "$skill_dir" "$now" <<'PY'
import json, sys, os
path, skill_path, now = sys.argv[1], sys.argv[2], sys.argv[3]
idx = {"registries": [], "synced": [], "available_cache": {}, "updated_at": now}
if os.path.exists(path):
    try: idx = json.load(open(path))
    except Exception: pass
registries = idx.get("registries", [])
if not any(r.get("name") == "local" for r in registries):
    registries.append({"name": "local", "kind": "git", "url": "file:///dev/null",
                        "enabled": True, "description": "local test registry",
                        "created_at": now, "updated_at": now})
idx["registries"] = registries
synced = [s for s in idx.get("synced", [])
          if not (s.get("registry") == "local" and s.get("name") == "dw-test-skill")]
synced.append({"registry": "local", "name": "dw-test-skill", "path": skill_path,
                "synced_at": now, "version": "1.0.0"})
idx["synced"] = synced
idx["updated_at"] = now
json.dump(idx, open(path, "w"), indent=2)
PY

  # Seed the community registry entry + .skills-cache from the production data dir
  # when available (native local run with internet-synced production daemon).
  # Skipped in CI / docker where the production data dir is absent.
  local prod_skills_cache="${HOME}/.datawatch/.skills-cache/community"
  if [[ -d "$prod_skills_cache/.git" ]]; then
    local test_cache_dir="$TEST_DATA/.skills-cache"
    mkdir -p "$test_cache_dir"
    if [[ ! -d "$test_cache_dir/community/.git" ]]; then
      cp -a "$prod_skills_cache" "$test_cache_dir/community"
    fi
    # Add community registry + available_cache to skills.json via the production API cache.
    local prod_skills_json="${HOME}/.datawatch/skills.json"
    python3 - "$TEST_DATA/skills.json" "$prod_skills_json" "$now" <<'PY2'
import json, sys, os
test_path, prod_path, now = sys.argv[1], sys.argv[2], sys.argv[3]
test_idx = {"registries": [], "synced": [], "available_cache": {}, "updated_at": now}
if os.path.exists(test_path):
    try: test_idx = json.load(open(test_path))
    except Exception: pass
prod_idx = {}
if os.path.exists(prod_path):
    try: prod_idx = json.load(open(prod_path))
    except Exception: pass
# Copy community registry entry from production.
regs = test_idx.get("registries", [])
for r in prod_idx.get("registries", []):
    if r.get("name") == "community" and not any(x.get("name") == "community" for x in regs):
        regs.append(r)
test_idx["registries"] = regs
# Copy community available_cache from production.
prod_cache = prod_idx.get("available_cache", {})
if "community" in prod_cache and "community" not in test_idx.get("available_cache", {}):
    if test_idx.get("available_cache") is None:
        test_idx["available_cache"] = {}
    test_idx["available_cache"]["community"] = prod_cache["community"]
test_idx["updated_at"] = now
json.dump(test_idx, open(test_path, "w"), indent=2)
PY2
  fi

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

  start_ntfy_server

  echo "Starting test daemon ($binary)..."
  mkdir -p "$TEST_DATA"
  # td_start records the daemon (PID + config-path marker) in TD_STATE_DIR;
  # the shared EXIT/INT/TERM/HUP traps end it -- sessions first, via the API
  # recorded below -- on every exit path (normal, failure, interrupt).
  td_start "$binary" "$test_cfg" "$TEST_DATA/daemon.log"
  DAEMON_PID=$TD_LAST_PID
  td_set_api "$DAEMON_PID" "${TEST_BASE:-http://127.0.0.1:$TEST_PORT}" "${TEST_TOKEN:-}"
  # PID + config path (the reaper's marker): a later run finds this daemon
  # through the record alone.
  printf '%s\n%s\n' "$DAEMON_PID" "$test_cfg" > "$TEST_DIR/daemon.pid"

  # Poll /api/health up to 30s (use HTTPS since HTTP port redirects when TLS enabled)
  local deadline=$(( $(date +%s) + 30 ))
  local healthy=0
  while [[ $(date +%s) -lt $deadline ]]; do
    if curl -skf "https://127.0.0.1:$TEST_TLS_PORT/api/health" > /dev/null 2>&1 || \
       curl -sf "http://127.0.0.1:$TEST_PORT/api/health" > /dev/null 2>&1; then
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
    tail -20 "$TEST_DATA/daemon.log" >&2 || true
    return 1
  fi

  echo "Daemon started (PID $DAEMON_PID) at http://127.0.0.1:$TEST_PORT"

  # Pre-warm the ollama model so LLM-dependent tests (TS-023 etc.) don't hit cold-start timeouts.
  local ollama_host
  ollama_host=$(python3 -c "import yaml,sys; c=yaml.safe_load(open('$test_cfg')); print(c.get('ollama',{}).get('host',''))" 2>/dev/null || echo "")
  local ollama_model
  ollama_model=$(python3 -c "import yaml,sys; c=yaml.safe_load(open('$test_cfg')); print(c.get('ollama',{}).get('model',''))" 2>/dev/null || echo "")
  if [[ -n "$ollama_host" && -n "$ollama_model" ]]; then
    echo "Pre-warming ollama model $ollama_model at $ollama_host ..."
    curl -sf --max-time 60 -X POST "$ollama_host/api/generate" \
      -H "Content-Type: application/json" \
      -d "{\"model\":\"$ollama_model\",\"prompt\":\"hi\",\"stream\":false}" \
      >/dev/null 2>&1 && echo "  ollama warmup done" || echo "  ollama warmup skipped (not reachable or timed out)"
  fi
  # Pre-warm vision model (moondream) so TS-667/668/670 don't hit cold-start crashes.
  local vision_host
  vision_host=$(python3 -c "import yaml,sys; c=yaml.safe_load(open('$test_cfg')); print(c.get('vision',{}).get('endpoint',''))" 2>/dev/null || echo "")
  local vision_model
  vision_model=$(python3 -c "import yaml,sys; c=yaml.safe_load(open('$test_cfg')); print(c.get('vision',{}).get('model',''))" 2>/dev/null || echo "")
  if [[ -n "$vision_host" && -n "$vision_model" ]]; then
    echo "Pre-warming vision model $vision_model at $vision_host ..."
    # Include a minimal 1x1 PNG as base64 so moondream (vision model) loads its vision
    # pipeline during warmup; text-only warmup can cause it to crash on the first real image.
    local _tiny_png
    _tiny_png="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI6QAAAABJRU5ErkJggg=="
    curl -sf --max-time 120 -X POST "$vision_host/api/generate" \
      -H "Content-Type: application/json" \
      -d "{\"model\":\"$vision_model\",\"prompt\":\"describe\",\"images\":[\"$_tiny_png\"],\"stream\":false}" \
      >/dev/null 2>&1 && echo "  vision warmup done" || echo "  vision warmup skipped (not reachable or timed out)"
  fi
}

NTFY_PID=""

start_ntfy_server() {
  if ! command -v ntfy &>/dev/null; then return; fi
  ntfy serve \
    --listen-http ":${TEST_NTFY_PORT}" \
    --base-url "http://127.0.0.1:${TEST_NTFY_PORT}" \
    > "$TEST_DIR/ntfy.log" 2>&1 &
  NTFY_PID=$!
  # Wait up to 5s for ntfy to be ready
  local tries=0
  while [[ $tries -lt 10 ]]; do
    curl -sf "http://127.0.0.1:${TEST_NTFY_PORT}/v1/health" >/dev/null 2>&1 && break
    sleep 0.5; tries=$((tries+1))
  done
}

prepare_k8s_image() {
  local image="harbor.dmzs.com/library/datawatch-e2e:latest"
  local cluster="testing"

  if ! command -v kind &>/dev/null; then return; fi
  if ! kind get clusters 2>/dev/null | grep -qx "$cluster"; then return; fi

  # Build image if not present in docker
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo "  Building $image from Dockerfile.dev..."
    if ! docker build -f "$REPO_DIR/Dockerfile.dev" -t "$image" "$REPO_DIR" 2>&1 | tail -5; then
      echo "  WARNING: datawatch-e2e image build failed — K8s tests will skip"
      return
    fi
    echo "  Build complete."
  fi

  # Load into kind cluster
  echo "  Loading $image into kind cluster '$cluster'..."
  if kind load docker-image "$image" --name "$cluster" >/dev/null 2>&1; then
    echo "  K8s image ready."
  else
    echo "  WARNING: kind load failed — K8s tests may skip"
  fi
}

# wait_or_kill sends SIGTERM to pid, polls for up to timeout_s seconds for it
# to exit, then SIGKILLs it if it's still alive, and finally reaps it with a
# plain `wait` (returns immediately either way once the process is dead).
#
# Found live 2026-10-05: stop_test_daemon used a bare `kill "$PID"; wait
# "$PID"` with no timeout. When the daemon didn't exit promptly from
# SIGTERM (observed after a real autonomous-PRD failure path put it under
# load), `wait` blocked indefinitely — the whole suite hung for 90+ minutes
# with zero CPU use and no self-recovery, requiring a manual SIGKILL from
# outside the script to unblock it.
wait_or_kill() {
  local pid="$1" timeout_s="${2:-15}"
  kill "$pid" 2>/dev/null || true
  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if [[ $waited -ge $timeout_s ]]; then
      echo "  [cleanup] pid $pid did not exit within ${timeout_s}s of SIGTERM — sending SIGKILL" >&2
      kill -9 "$pid" 2>/dev/null || true
      break
    fi
    sleep 1
    waited=$(( waited + 1 ))
  done
  wait "$pid" 2>/dev/null || true
}

stop_test_daemon() {
  if [[ -n "$DAEMON_PID" ]]; then
    # td_stop_one ends the daemon's sessions through its own API first, then
    # signals only the recorded PID while /proc/<pid>/cmdline still carries
    # the recorded config path. Idempotent: the shared EXIT/INT/TERM/HUP
    # traps may already have stopped it. The daemon's own terminal service
    # ends with the daemon itself -- this script never lists or kills session
    # names and never touches a multiplexer server.
    td_stop_one "$DAEMON_PID"
    DAEMON_PID=""
    # Free-port allocation guaranteed five distinct ports above; confirm the
    # two daemon-facing ones actually came back after the stop.
    td_wait_port_closed "$TEST_PORT" 15 || echo "  [cleanup] warning: HTTP port $TEST_PORT still held after daemon stop" >&2
    td_wait_port_closed "$TEST_TLS_PORT" 15 || echo "  [cleanup] warning: TLS port $TEST_TLS_PORT still held after daemon stop" >&2
  fi
  if [[ -n "$NTFY_PID" ]]; then
    # This script's own child, recorded by PID: safe to signal directly.
    kill "$NTFY_PID" 2>/dev/null || true
    wait "$NTFY_PID" 2>/dev/null || true
    NTFY_PID=""
  fi
}

stop_second_test_daemon() {
  # TS-785 starts a real second daemon (federation E2E). A story that records
  # it through the shared library (td_track_pid) is already covered by the
  # state files in TD_STATE_DIR: td_stop_all ends it, sessions first via
  # its API, on the same exit paths as the primary daemon. Otherwise the
  # story writes $TEST_DIR/fed-peer-daemon.pid; recover from that record and
  # re-verify the live cmdline against the recorded config path before
  # signalling: never signal an unrelated or recycled pid.
  local pidfile="$TEST_DIR/fed-peer-daemon.pid"
  local pid
  if [[ -f "$pidfile" ]]; then
    pid=$(sed -n '1p' "$pidfile" 2>/dev/null || true)
    if [[ "$pid" =~ ^[0-9]+$ ]]; then
      if [[ -f "$TD_STATE_DIR/$pid.rec" ]]; then
        # Route through the shared state the same way a library-recording
        # story is: td_stop_one ends its sessions via the API, then signals
        # the recorded PID (no-op if the traps already stopped it).
        td_stop_one "$pid"
      elif kill -0 "$pid" 2>/dev/null; then
        local marker="${FED_PEER_DATA:-$(dirname "$pidfile")}/config.yaml"
        local cmd
        cmd=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)
        case "$cmd" in
          *"$marker"*) wait_or_kill "$pid" 10;;
          *) echo "  [cleanup] $pidfile points at pid $pid but its live cmdline does not carry '$marker'; not signalling it" >&2
        esac
      fi
    fi
  fi
  if [[ -n "${FED_PEER_DATA:-}" ]]; then
    rm -rf "$FED_PEER_DATA" 2>/dev/null || true
  fi
}

stop_docker_sim() {
  # Recover names persisted by TS-160 in case env vars were lost (SIGKILL on a prior run).
  if [[ -z "${DOCKER_SIM_CONTAINER:-}" && -n "${DOCKER_SIM_DATA:-}" && -f "$DOCKER_SIM_DATA/container.name" ]]; then
    DOCKER_SIM_CONTAINER=$(cat "$DOCKER_SIM_DATA/container.name" 2>/dev/null || true)
  fi
  if [[ -z "${DOCKER_SIM_IMAGE:-}" && -n "${DOCKER_SIM_DATA:-}" && -f "$DOCKER_SIM_DATA/image.name" ]]; then
    DOCKER_SIM_IMAGE=$(cat "$DOCKER_SIM_DATA/image.name" 2>/dev/null || true)
  fi
  if [[ -n "${DOCKER_SIM_CONTAINER:-}" ]]; then
    docker stop "$DOCKER_SIM_CONTAINER" 2>/dev/null || true
    docker rm   "$DOCKER_SIM_CONTAINER" 2>/dev/null || true
    DOCKER_SIM_CONTAINER=""
  fi
  if [[ -n "${DOCKER_SIM_IMAGE:-}" ]]; then
    docker rmi "$DOCKER_SIM_IMAGE" 2>/dev/null || true
    DOCKER_SIM_IMAGE=""
  fi
  [[ -n "${DOCKER_SIM_DATA:-}" ]] && rm -rf "$DOCKER_SIM_DATA" 2>/dev/null || true
}

# dw_runner_cleanup_hooks — runner-shell cleanup for anything a story shell
# tracked but only the story itself can name: the story hook registered in
# DW_STORY_CLEANUP (TS-820: its sessions, LLM entries, Ollama stand-ins and
# scratch) and the channel-server helpers when lib.sh's own cleanup is
# visible (serial stories run in this shell). No-ops when nothing is
# registered; every step is guarded, so it is safe to run again after the
# shared td_stop_all of the traps has already ended the recorded daemons.
dw_runner_cleanup_hooks() {
  local hook="${DW_STORY_CLEANUP:-}"
  if [[ -n "$hook" ]] && declare -F "$hook" >/dev/null 2>&1; then
    "$hook" || true
  fi
  if declare -F _dw_channel_cleanup >/dev/null 2>&1; then
    _dw_channel_cleanup || true
  fi
  return 0
}

cleanup() {
  stop_test_daemon
  stop_second_test_daemon
  dw_runner_cleanup_hooks
  stop_docker_sim
  destroy_pool
  # Leftover-daemon guard: every daemon this run started has been stopped
  # by now (the trap path runs td_stop_all before this hook; the normal
  # path calls it just before this function). Report, and fail the run, if
  # a live test daemon is around that was not in the start-of-run baseline
  # -- i.e. this job started it and left it behind -- and keep the run dir
  # so the leftover can be inspected and ended.
  if [[ -f "$LEFTOVER_BASELINE" && ! -f "$TEST_DIR/.leftover-check-done" ]]; then
    if bash "$GUARD_SH" check "$LEFTOVER_BASELINE"; then
      :
    else
      echo ""
      echo "  [cleanup] FAIL: test daemon(s) still alive after this run (guard report above)."
      : > "$TEST_DIR/leftover-daemons-FAIL" 2>/dev/null || true
      FAILED=1
    fi
    touch "$TEST_DIR/.leftover-check-done" 2>/dev/null || true
  fi
  if [[ $FAILED -ne 0 || -n "${KEEP_TEST_DIR:-}" || -f "$TEST_DIR/leftover-daemons-FAIL" ]]; then
    echo ""
    echo "Working dir kept: $TEST_DIR"
    echo "  Resume: DATAWATCH_TEST_ID=$RUN_ID bash scripts/run-tests.sh --resume-from=<story>"
  else
    rm -rf "$TEST_DIR"
  fi
}
# The EXIT/INT/TERM/HUP traps are installed near the top via td_install_traps:
# they end every recorded daemon (sessions through its API first), preserve
# this script's original exit status, keep set -e deaths countable as failure
# (ERR trap) and then run cleanup() -- working dir, ntfy, docker-sim, worker
# pool -- via TD_EXTRA_CLEANUP.

# flush_story_cleanup — delete resources registered via add_cleanup() after
# each serial story runs, so sessions don't accumulate and hit max_sessions.
flush_story_cleanup() {
  local log="${CLEANUP_LOG:-$TEST_DIR/cleanup.log}"
  [[ -f "$log" ]] || return 0
  # Use HTTPS to avoid HTTP→HTTPS redirect stripping the DELETE method
  local base="https://127.0.0.1:${TEST_TLS_PORT:-18443}"
  local tok="${TEST_TOKEN:-dw-test-token-12345}"
  local curl_del=(-sk --max-time 10 -X DELETE -H "Authorization: Bearer $tok")
  local curl_post=(-sk --max-time 10 -X POST -H "Authorization: Bearer $tok" -H "Content-Type: application/json")
  while IFS=' ' read -r kind id; do
    [[ -z "$kind" || -z "$id" ]] && continue
    case "$kind" in
      # Sessions use POST /api/sessions/delete (no REST DELETE on /{id})
      sess|session)    curl "${curl_post[@]}" -d "{\"id\":\"$id\"}" "$base/api/sessions/delete" >/dev/null 2>&1 ;;
      automaton)       curl "${curl_del[@]}" "$base/api/autonomous/prds/$id" >/dev/null 2>&1 ;;
      persona)         curl "${curl_del[@]}" "$base/api/council/personas/$id" >/dev/null 2>&1 ;;
      compute_node)    curl "${curl_del[@]}" "$base/api/compute/nodes/$id" >/dev/null 2>&1 ;;
      observer_peer)   curl "${curl_del[@]}" "$base/api/observer/peers/$id" >/dev/null 2>&1 ;;
      filter)          curl "${curl_del[@]}" "$base/api/filters?id=$id" >/dev/null 2>&1 ;;
      mem)             curl "${curl_del[@]}" "$base/api/memory/entries/$id" >/dev/null 2>&1 ;;
      sched)           curl "${curl_del[@]}" "$base/api/schedules?id=$id" >/dev/null 2>&1 ;;
      secret)          curl "${curl_del[@]}" "$base/api/secrets/$id" >/dev/null 2>&1 ;;
      server)          curl "${curl_del[@]}" "$base/api/mcp/servers/$id" >/dev/null 2>&1 ;;
      profile-proj)    curl "${curl_del[@]}" "$base/api/profiles?name=$id" >/dev/null 2>&1 ;;
      llm)             curl "${curl_del[@]}" "$base/api/llms/$id" >/dev/null 2>&1 ;;
    esac
  done < "$log"
  : > "$log"
}

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

if [[ -n "${DW_BASE:-}" ]]; then
  # External-daemon mode (2026-10-13) — same mechanism and variable
  # names as scripts/release-smoke.sh's own DW_BASE (see
  # docs/howto/testing-against-a-running-daemon.md). Found live:
  # TEST_PORT/TEST_TLS_PORT alone do NOT support this — _fresh_port
  # below only honors a pre-set port when it's currently FREE, which
  # an already-running daemon's port never is, so a naive
  # TEST_TLS_PORT=<port-in-use> silently got overridden to a random
  # free port instead and the run failed against nothing. DW_BASE
  # bypasses port allocation for TEST_BASE/TEST_BASE_HTTP entirely
  # instead of fighting that logic.
  export TEST_BASE="$DW_BASE"
  export TEST_BASE_HTTP="${DW_BASE_HTTP:-$DW_BASE}"
  export TEST_TOKEN="${DW_BASE_TOKEN:-dw-test-token-12345}"
  export DW_BASE_INSECURE="${DW_BASE_INSECURE:-1}" # see release-smoke.sh's identical default + rationale
  NO_DAEMON=1
  echo "External-daemon mode: TEST_BASE=$TEST_BASE"
fi

# Pick free ports — if an env var is set but the port is already occupied by
# another process (e.g. leftover from a previous run), pick a fresh one.
# Skipped entirely in DW_BASE mode above (TEST_BASE/TEST_BASE_HTTP are
# already set and nothing here should overwrite them), but every OTHER
# fixture port (MCP, DNS, docker-sim, ntfy, webhook) still needs a real
# value for story code that reads it, so only guard the two daemon-facing
# ports.
_port_free() {
  ! ss -tlnH "sport = :$1" 2>/dev/null | grep -q . 2>/dev/null
}
_fresh_port() {
  local p="${1:-}"
  if [[ -n "$p" ]] && _port_free "$p"; then echo "$p"; else free_port; fi
}
if [[ -z "${DW_BASE:-}" ]]; then
  export TEST_PORT="$(_fresh_port "${TEST_PORT:-}")"
  export TEST_TLS_PORT="$(_fresh_port "${TEST_TLS_PORT:-}")"
fi
export TEST_MCP_PORT="$(_fresh_port "${TEST_MCP_PORT:-}")"
export TEST_DNS_PORT="$(_fresh_port "${TEST_DNS_PORT:-}")"
export DOCKER_SIM_HTTP="$(_fresh_port "${DOCKER_SIM_HTTP:-}")"
export DOCKER_SIM_TLS="$(_fresh_port "${DOCKER_SIM_TLS:-}")"
export DOCKER_SIM_MCP="$(_fresh_port "${DOCKER_SIM_MCP:-}")"
export DOCKER_SIM_CHAN="$(_fresh_port "${DOCKER_SIM_CHAN:-}")"
export TEST_NTFY_PORT="$(_fresh_port "${TEST_NTFY_PORT:-}")"
export TEST_NTFY_TOPIC="dw-e2e-${RUN_ID}"
# Stable path keyed by RUN_ID so the EXIT trap can always find and remove it,
# even after a resume. lib.sh falls back to /tmp/dw-docker-sim-$$ only when
# this var is unset (standalone story invocation).
export DOCKER_SIM_DATA="/tmp/dw-docker-sim-${RUN_ID}"
export TEST_WEBHOOK_PORT="$(_fresh_port "${TEST_WEBHOOK_PORT:-}")"
export TEST_DATA="${TEST_DATA:-${DATAWATCH_TEST_DATA}}"
if [[ -z "${DW_BASE:-}" ]]; then
  export TEST_BASE="https://127.0.0.1:$TEST_TLS_PORT"
  export TEST_BASE_HTTP="http://127.0.0.1:$TEST_PORT"
  export TEST_TOKEN="${TEST_TOKEN:-dw-test-token-12345}"
fi

echo "Run ID  : $RUN_ID"
echo "Work dir: $TEST_DIR"
echo "Ports   : http=$TEST_PORT tls=$TEST_TLS_PORT mcp=$TEST_MCP_PORT dns=$TEST_DNS_PORT"
echo ""

# --- argument parsing -------------------------------------------------------
FILTER_SURFACE=""
FILTER_FEATURE=""
FILTER_STORY=""
FILTER_STORIES=""
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
    --stories=*)          FILTER_STORIES="${arg#*=}" ;;
    --resume-from=*)      RESUME_FROM="${arg#*=}" ;;
    --fail-fast*)         FAIL_FAST=1 ;;
    --workers=*)          WORKER_FLAG="${arg#*=}" ;;
    --serial|--no-parallel) SERIAL_MODE=1 ;;
    --no-daemon)          NO_DAEMON=1 ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

export FILTER_SURFACE FILTER_FEATURE FILTER_STORY FILTER_STORIES RESUME_FROM FAIL_FAST

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
  local par_cleanup="$TEST_DIR/cleanup-${story_id}.log"
  (
    set +e
    # Each parallel worker gets its own cleanup log so concurrent sourcing
    # of lib.sh (which truncates the shared log) doesn't lose cleanup entries.
    export CLEANUP_LOG="$par_cleanup"
    RESULT=""
    CURRENT_STORY="$story_id"
    # Scope the shared daemon-library state to this story: daemons this
    # subshell starts are recorded under its owner and its EXIT trap ends
    # them with td_stop_self — a parallel worker never leaves its daemons
    # to the runner's traps, and never signals a sibling's or the
    # runner's recorded daemons.
    export TD_OWNER="$story_id"
    mkdir -p "$EVIDENCE_DIR/$story_id"
    # Trap ensures result file and semaphore release run even when the story
    # calls `exit 0` directly instead of `return` (TS-750/751/752 pattern).
    # Order: the story's own registered cleanup, then this owner's recorded
    # daemons (td_stop_self), then the result file and the semaphore.
    trap 'flush_story_cleanup; dw_runner_worker_cleanup; { if declare -F td_stop_self >/dev/null 2>&1; then td_stop_self; fi; printf "%s" "${RESULT:-fail}" > "$result_file"; release_worker; }' EXIT
    # shellcheck source=/dev/null
    source "$script"
  ) &
  local pid=$!
  PAR_PIDS+=("$pid")
  PAR_STORY[$pid]="$story_id"
}

# dw_runner_worker_cleanup — exit path of a parallel story subshell, besides
# what the shared library traps already stopped by recorded PID: the story
# hook that the story shell registered in DW_STORY_CLEANUP (TS-820: its
# sessions, LLM entries, Ollama stand-ins and scratch) and the channel
# helpers recorded by lib.sh in this subshell. No-ops when nothing is
# registered; guarded steps, so safe to run twice.
dw_runner_worker_cleanup() {
  local hook="${DW_STORY_CLEANUP:-}"
  if [[ -n "$hook" ]] && declare -F "$hook" >/dev/null 2>&1; then
    "$hook" || true
  fi
  if declare -F _dw_channel_cleanup >/dev/null 2>&1; then
    _dw_channel_cleanup || true
  fi
  return 0
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
  # conflict:llm tests must not run concurrently — Ollama saturation causes transient skips
  echo "$1" | grep -q "conflict:llm" && return 1
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
RUN_START=$(date +%s)
declare -A FEAT_PASS FEAT_FAIL FEAT_SKIP FEAT_MS

if [[ ! -d "$STORIES_DIR" ]]; then
  echo "No story implementations yet. Add scripts to scripts/test-stories/TS-NNN.sh"
  echo "Stories are defined in: $DATAWATCH_COOKBOOK"
  exit 0
fi

# --- K8s image pre-build ----------------------------------------------------
prepare_k8s_image

# --- daemon startup ---------------------------------------------------------
# DW_BASE checked directly here (not just via NO_DAEMON) since the
# flag-parsing block above unconditionally sets NO_DAEMON=0 AFTER the
# DW_BASE branch already ran and set NO_DAEMON=1 -- relying on
# assignment order alone would silently clobber it.
if [[ $NO_DAEMON -eq 0 && -z "${DW_BASE:-}" ]]; then
  if curl -skf "https://127.0.0.1:$TEST_TLS_PORT/api/health" > /dev/null 2>&1 || \
     curl -sf "http://127.0.0.1:$TEST_PORT/api/health" > /dev/null 2>&1; then
    echo "Using existing daemon at $TEST_BASE"
    # Create TEST_DATA/config.yaml so cli_test (--config) uses the right port.
    if [[ -n "${TEST_DATA:-}" && -f "$REPO_DIR/testdata/datawatch.yaml" ]]; then
      mkdir -p "$TEST_DATA"
      sed \
        -e "s|data_dir: /data|data_dir: $TEST_DATA|g" \
        -e "s|default_project_dir: /data/projects|default_project_dir: $TEST_DATA/projects|g" \
        -e "s|port: 8080|port: $TEST_PORT|g" \
        -e "s|tls_port: 8443|tls_port: $TEST_TLS_PORT|g" \
        -e "s|sse_port: 9090|sse_port: $TEST_MCP_PORT|g" \
        -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
        -e "s|token: \"\"|token: \"${TEST_TOKEN:-}\"|g" \
        -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
        "$REPO_DIR/testdata/datawatch.yaml" > "$TEST_DATA/config.yaml"
    fi
    # Discover actual MCP SSE port from the daemon's config so TS-624 and
    # other MCP stories hit the right port instead of the default TEST_PORT+1.
    _mcp_port=$(curl -sk "https://127.0.0.1:$TEST_TLS_PORT/api/config" \
      -H "Authorization: Bearer ${TEST_TOKEN}" 2>/dev/null \
      | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("mcp",{}).get("sse_port",""))' \
      2>/dev/null || true)
    if [[ -n "$_mcp_port" && "$_mcp_port" =~ ^[0-9]+$ ]]; then
      # If config reports a low port (e.g. 9090 template default) but TEST_PORT is
      # high (e.g. 18080), the daemon binds SSE at the same offset from TEST_PORT.
      # Formula: actual = TEST_PORT - 8080 + config_sse_port
      if [[ $_mcp_port -lt $TEST_PORT ]]; then
        _mcp_port=$(( TEST_PORT - 8080 + _mcp_port ))
      fi
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

  # story filter — single (--story=) or comma-separated set (--stories=)
  [[ -n "$FILTER_STORY" && "$story_id" != "$FILTER_STORY" ]] && continue
  if [[ -n "$FILTER_STORIES" ]] && ! echo ",$FILTER_STORIES," | grep -q ",$story_id,"; then
    continue
  fi

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
    _story_start=$(date +%s%N)
    # shellcheck source=/dev/null
    source "$story_script"
    flush_story_cleanup
    _story_ms=$(( ($(date +%s%N) - _story_start) / 1000000 ))

    # accumulate per-feature timing
    _feat=$(grep -m1 '^# tags:' "$story_script" 2>/dev/null | grep -oP 'feature:\K[^ ]+' | head -1 || echo "other")
    FEAT_MS[$_feat]=$(( ${FEAT_MS[$_feat]:-0} + _story_ms ))

    case "${RESULT:-fail}" in
      pass) ((PASS++)); FEAT_PASS[$_feat]=$(( ${FEAT_PASS[$_feat]:-0} + 1 )); echo "✓ $story_id (${_story_ms}ms)" ;;
      skip) ((SKIP++)); FEAT_SKIP[$_feat]=$(( ${FEAT_SKIP[$_feat]:-0} + 1 )); echo "- $story_id (skip)" ;;
      fail|*)
        ((FAIL++))
        FEAT_FAIL[$_feat]=$(( ${FEAT_FAIL[$_feat]:-0} + 1 ))
        echo "✗ $story_id (${_story_ms}ms)"
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
RUN_ELAPSED=$(( $(date +%s) - RUN_START ))
echo ""
echo "Results: $PASS passed  $FAIL failed  $SKIP skipped  (${RUN_ELAPSED}s total)"
if [[ $SERIAL -eq 0 ]]; then
  echo "Workers: adaptive (peak ~$(suggest_workers), cpu=$(cpu_pct)% mem=$(mem_free_pct)% free)"
fi

# Per-feature breakdown table
if [[ ${#FEAT_MS[@]} -gt 0 ]]; then
  echo ""
  printf "%-22s %5s %5s %5s %7s\n" "Feature" "pass" "fail" "skip" "time(s)"
  printf "%-22s %5s %5s %5s %7s\n" "----------------------" "-----" "-----" "-----" "-------"
  for _f in $(echo "${!FEAT_MS[@]}" | tr ' ' '\n' | sort); do
    printf "%-22s %5d %5d %5d %7.1f\n" \
      "$_f" \
      "${FEAT_PASS[$_f]:-0}" \
      "${FEAT_FAIL[$_f]:-0}" \
      "${FEAT_SKIP[$_f]:-0}" \
      "$(echo "scale=1; ${FEAT_MS[$_f]:-0}/1000" | bc)"
  done
fi

if [[ $FAIL -gt 0 ]]; then
  FAILED=1
  exit 1
fi

# All stories passed. Run the end-of-run cleanup in the main flow now (the
# EXIT trap does the identical, idempotent work on the failure and interrupt
# paths): stop every daemon this run started, run the leftover-daemon guard
# against the start-of-run baseline, remove the run dir -- and fail the run
# with the guard's output if a test daemon this run started is still alive
# after all of it.
td_stop_all
cleanup
if [[ -f "$TEST_DIR/leftover-daemons-FAIL" ]]; then
  echo ""
  echo "FAIL: every story passed, but the run left test daemon(s) alive (guard report above)"
  exit 1
fi
exit 0
