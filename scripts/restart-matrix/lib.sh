#!/usr/bin/env bash
# scripts/restart-matrix/lib.sh — shared helpers for the daemon-restart matrix.
#
# The matrix restarts a SANDBOX daemon while work is in flight and records what
# happened to that work. This library owns everything that touches a daemon:
#
#   - a throwaway work root (mktemp -d) holding the sandbox data dir, its own
#     multiplexer socket dir (an explicit throwaway TMUX_TMPDIR), a control dir
#     for the fake backend, a git project dir, logs and the JSON records
#   - a generated config: its own data_dir, free ports, TLS off, fake backends
#   - start / restart of that one daemon, by recorded pid only
#   - REST helpers against the sandbox, state snapshots and per-state records
#
# Safety contract (reused from internal/session/restart_safety_live_test.go and
# scripts/check-no-production-service-control.sh):
#
#   - nothing here ever calls a service manager, the daemon's own stop/restart
#     commands, or kills a process by name; a restart is TERM sent to the pid
#     this library recorded when it launched the sandbox daemon, after checking
#     that pid is alive and that its command line carries THIS sandbox config
#   - every tmux call runs under `env -u TMUX` with TMUX_TMPDIR pointing at the
#     throwaway socket dir; no server-wide tmux kill is ever issued
#   - the run refuses to start when the sandbox port or data dir matches the
#     production daemon (default ports, the default data dir, and whatever the
#     production config file declares)
#
# This file is sourced; it sets no shell options of its own.

RM_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RM_REPO_DIR="$(cd "$RM_LIB_DIR/../.." && pwd)"

# ---------------------------------------------------------------- logging ----

rm_ts() { date '+%H:%M:%S'; }
rm_log() { printf '[%s] %s\n' "$(rm_ts)" "$*" >&2; }
rm_die() {
  printf '[%s] ERROR: %s\n' "$(rm_ts)" "$*" >&2
  exit 2
}

# ----------------------------------------------------------- preconditions ----

rm_require_tools() {
  local t missing=0
  for t in jq curl python3 tmux git; do
    command -v "$t" >/dev/null 2>&1 || {
      rm_log "missing required tool: $t"
      missing=1
    }
  done
  [[ $missing -eq 0 ]] || rm_die "install the missing tools and retry"
}

rm_free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); p=s.getsockname()[1]; s.close(); print(p)'
}

# ------------------------------------------------------- production guard ----
#
# Production is identified three ways, any one of which blocks the run:
#   1. the well-known default ports (override list: RM_PROD_PORTS)
#   2. the default data dir (override: RM_PROD_DATA_DIR)
#   3. ports and data_dir declared in the production config file
#      (override path: RM_PROD_CONFIG; default $HOME/.datawatch/config.yaml)

rm_prod_config_file() { printf '%s' "${RM_PROD_CONFIG:-$HOME/.datawatch/config.yaml}"; }

rm_prod_ports() {
  local f
  f="$(rm_prod_config_file)"
  printf '%s\n' ${RM_PROD_PORTS:-8080 8443}
  if [[ -r "$f" ]]; then
    grep -E '^[[:space:]]+(port|tls_port):[[:space:]]*[0-9]+' "$f" 2>/dev/null |
      sed -E 's/.*:[[:space:]]*([0-9]+).*/\1/'
  fi
}

rm_prod_data_dirs() {
  local f
  f="$(rm_prod_config_file)"
  printf '%s\n' "${RM_PROD_DATA_DIR:-$HOME/.datawatch}"
  if [[ -r "$f" ]]; then
    grep -E '^data_dir:' "$f" 2>/dev/null | sed -E 's/^data_dir:[[:space:]]*//; s/^["'"'"']//; s/["'"'"']$//'
  fi
}

# rm_guard_not_production PORT TLS_PORT DATA_DIR — exits 2 on a match.
rm_guard_not_production() {
  local port="$1" tls_port="$2" data_dir="$3" p d real_data real_prod
  while IFS= read -r p; do
    [[ -z "$p" ]] && continue
    if [[ "$port" == "$p" || "$tls_port" == "$p" ]]; then
      rm_die "refusing to run: sandbox port $port/$tls_port matches production port $p"
    fi
  done < <(rm_prod_ports)
  real_data="$(realpath -m "$data_dir")"
  while IFS= read -r d; do
    [[ -z "$d" ]] && continue
    real_prod="$(realpath -m "${d/#\~/$HOME}")"
    if [[ "$real_data" == "$real_prod" ]]; then
      rm_die "refusing to run: sandbox data dir $real_data is the production data dir"
    fi
  done < <(rm_prod_data_dirs)
  # A listener already answering on the chosen port is somebody else's daemon.
  if curl -s --max-time 2 "http://127.0.0.1:$port/api/health" >/dev/null 2>&1; then
    rm_die "refusing to run: something already answers on 127.0.0.1:$port"
  fi
}

# ------------------------------------------------------------- work root ----

# rm_init_root — creates the throwaway work root once per run. The root path is
# kept short because the multiplexer socket path has a ~108 byte limit.
rm_init_root() {
  [[ -n "${RM_ROOT:-}" ]] && return 0
  local base="${RM_TMP_BASE:-${TMPDIR:-/tmp}}"
  mkdir -p "$base"
  RM_ROOT="$(mktemp -d "$base/rm.XXXXXX")"
  RM_REC="$RM_ROOT/records"
  RM_SNAP="$RM_REC/snapshots"
  mkdir -p "$RM_REC" "$RM_SNAP"
  export RM_ROOT RM_REC RM_SNAP
}

# rm_sandbox_new NAME — lays out a fresh sandbox for one scenario under the
# work root and exports its variables. Does not start anything.
rm_sandbox_new() {
  local name="$1"
  rm_init_root
  RM_SB="$RM_ROOT/$name"
  RM_DATA="$RM_SB/data"
  RM_TMUX="$RM_SB/t"
  RM_CTRL="$RM_SB/ctrl"
  RM_PROJECT="$RM_SB/project"
  RM_CONFIG="$RM_SB/config.yaml"
  RM_DAEMON_LOG="$RM_SB/daemon.log"
  RM_PIDFILE="$RM_SB/daemon.pid"
  mkdir -p "$RM_DATA" "$RM_TMUX" "$RM_CTRL" "$RM_PROJECT"
  RM_PORT="$(rm_free_port)"
  RM_TLS_PORT="$(rm_free_port)"
  while [[ "$RM_TLS_PORT" == "$RM_PORT" ]]; do RM_TLS_PORT="$(rm_free_port)"; done
  RM_BASE="http://127.0.0.1:$RM_PORT"
  RM_DAEMON_PID=""
  RM_DAEMON_ENV=()    # extra KEY=VALUE pairs for the daemon; only rm_pi_nodes_setup fills it
  RM_NODE_STUB_PID="" # the compute-node stub, only started by rm_pi_nodes_setup
  # tmux appends /tmux-<uid>/default to TMUX_TMPDIR.
  local sock="$RM_TMUX/tmux-$(id -u)/default"
  if ((${#sock} > 100)); then
    rm_die "multiplexer socket path too long (${#sock} bytes): set RM_TMP_BASE to a shorter directory"
  fi
  rm_guard_not_production "$RM_PORT" "$RM_TLS_PORT" "$RM_DATA"
  export RM_SB RM_DATA RM_TMUX RM_CTRL RM_PROJECT RM_CONFIG RM_DAEMON_LOG RM_PIDFILE RM_PORT RM_TLS_PORT RM_BASE
  rm_write_fake_backend
  rm_write_config
  rm_init_project
}

rm_find_binary() {
  if [[ -n "${DATAWATCH_BIN:-}" ]]; then
    [[ -x "$DATAWATCH_BIN" ]] || rm_die "DATAWATCH_BIN=$DATAWATCH_BIN is not executable"
    printf '%s' "$DATAWATCH_BIN"
    return
  fi
  if [[ -x "$RM_REPO_DIR/datawatch" ]]; then
    printf '%s' "$RM_REPO_DIR/datawatch"
    return
  fi
  rm_die "no daemon binary: build with 'go build -o datawatch ./cmd/datawatch/' or set DATAWATCH_BIN"
}

# ----------------------------------------------------------- fake backends ----
#
# One script plays every LLM role. The daemon's shell backend runs it as
# `script <task text> <project dir>` inside a multiplexer session, so planning,
# verification and guardrail judging all work without a real model: those roles
# are one-shot sessions told to write a JSON verdict to a named output file.
#
# Behaviour is steered by files in the control dir, read when the script starts:
#   planner_delay / verifier_delay / worker_ticks   seconds or tick counts
#   launch_delay                                    seconds the FIRST worker launch
#                                                   holds before its agent starts
#                                                   (a bare shell); launch.release
#                                                   ends the hold early
#   plan.json                                       the plan the planner returns
# Every role appends a line "<epoch> pid=<pid>" to <role>.starts so a scenario
# can count how many times a role ran and probe whether that process survived.

rm_write_fake_backend() {
  cat >"$RM_CTRL/fake-backend.sh" <<EOF
#!/usr/bin/env bash
task="\${1:-}"
dir="\${2:-.}"
ctrl="$RM_CTRL"
num() { local v; v="\$(cat "\$ctrl/\$1" 2>/dev/null)"; printf '%s' "\${v:-\$2}"; }
out="\$(printf '%s' "\$task" | grep -oE '[^[:space:]]*\.(decompose|verify|guardrail)-output-[0-9a-f]+\.json' | tail -1)"
case "\$task" in
  *decompose-output*) role=planner ;;
  *verify-output*)    role=verifier ;;
  *guardrail-output*) role=guardrail ;;
  *)                  role=worker ;;
esac
if [[ "\$role" == worker ]]; then
  # launch-delay: the first worker launch holds, before its agent starts, until
  # launch.release exists or launch_delay seconds pass. The wait uses bash
  # builtins only: a sleep child on the pane's tty would make AgentStarted see
  # an agent in a session that is really a bare shell.
  echo "pid=\$\$" >>"\$ctrl/worker.launches"
  delay="\$(num launch_delay 0)"
  if ((delay > 0)) && ((\$(grep -c '' "\$ctrl/worker.launches") == 1)); then
    echo "pid=\$\$" >>"\$ctrl/worker.held"
    hold_until=\$((SECONDS + delay))
    while ((SECONDS < hold_until)) && [[ ! -e "\$ctrl/launch.release" ]]; do
      read -r -t 1 -u 9 _ 9<>"\$ctrl/launch-hold.fifo" || true
    done
  fi
fi
echo "\$(date +%s) pid=\$\$" >>"\$ctrl/\$role.starts"
case "\$role" in
  planner)
    sleep "\$(num planner_delay 0)"
    [[ -n "\$out" ]] && cp "\$ctrl/plan.json" "\$out"
    ;;
  verifier)
    sleep "\$(num verifier_delay 0)"
    [[ -n "\$out" ]] && printf '%s\n' '{"ok":true,"severity":"info","summary":"fake verifier accepts"}' >"\$out"
    echo "\$(date +%s) done" >>"\$ctrl/verifier.done"
    ;;
  guardrail)
    [[ -n "\$out" ]] && printf '%s\n' '{"guardrail":"rules","outcome":"pass","summary":"fake guardrail accepts"}' >"\$out"
    ;;
  worker)
    ticks="\$(num worker_ticks 6)"
    # park_first_workers: the first N worker starts park on an external condition
    # after park_after_ticks ticks, until the scenario creates await.release.
    parks=0
    ((\$(grep -c '' "\$ctrl/worker.starts") <= \$(num park_first_workers 0))) && parks=1
    park_at="\$(num park_after_ticks 0)"
    : >"\$dir/worker-counter.txt"
    for i in \$(seq 1 "\$ticks"); do
      echo "tick \$i" >>"\$dir/worker-counter.txt"
      echo "worker tick \$i"
      if ((parks && i == park_at)); then
        echo "\$(date +%s) pid=\$\$" >>"\$ctrl/worker.parked"
        echo "DATAWATCH_AWAIT_EXTERNAL: matrix external condition"
        while [[ ! -e "\$ctrl/await.release" ]]; do sleep 1; done
        parks=0
      fi
      sleep 1
    done
    echo "work for this task" >"\$dir/worker-result.txt"
    echo "\$(date +%s) done" >>"\$ctrl/worker.done"
    # a real worker ends on the completion marker; the daemon's resume path reads it
    # from the recorded output to tell a finished worker from an unfinished one
    echo "DATAWATCH_COMPLETE: matrix worker finished"
    ;;
esac
EOF
  chmod +x "$RM_CTRL/fake-backend.sh"
  # the launch hold waits on this FIFO with a builtin read timeout; nothing is written to it
  rm -f "$RM_CTRL/launch-hold.fifo"
  mkfifo -m 600 "$RM_CTRL/launch-hold.fifo" || rm_die "could not create the launch-hold fifo"
}

# rm_write_config — sandbox config: own data_dir, free ports, TLS off, fake backends.
rm_write_config() {
  cat >"$RM_CONFIG" <<EOF
data_dir: $RM_DATA
hostname: restart-matrix
server:
  enabled: true
  host: 127.0.0.1
  port: $RM_PORT
  tls_enabled: false
  tls_port: $RM_TLS_PORT
  token: ""
  proxy_sandbox_port: 0 # the sandbox never binds the default proxy port (8444)
memory:
  enabled: false
session:
  llm_backend: shell
  max_sessions: ${RM_MAX_SESSIONS:-20}
shell_backend:
  enabled: true
  script_path: $RM_CTRL/fake-backend.sh
autonomous:
  enabled: true
  planning_backend: shell
  verification_backend: shell
  execution_backend: shell
  poll_interval_seconds: 1
  refuse_dirty_tree_start: false
EOF
  # Opt-in only: states that do not set RM_PI_NODES keep the config above, byte for byte.
  [[ "${RM_PI_NODES:-0}" == 1 ]] && rm_write_config_pi
  return 0
}

# rm_write_config_pi — extends the sandbox config when RM_PI_NODES=1: the pi and
# pi-prompt backends run the fake pi that rm_pi_nodes_setup writes into the sandbox.
rm_write_config_pi() {
  # A PRD's project dir must lie under an allowed project root; the sandbox dir is not under
  # the default roots, so name it (inside the server block, after its last key).
  sed -i "/^  proxy_sandbox_port:/a\\  allowed_project_roots: [\"$RM_SB\"]" "$RM_CONFIG"
  cat >>"$RM_CONFIG" <<EOF
pi:
  enabled: true
  binary: $RM_SB/bin/pi
pi_prompt:
  enabled: true
  binary: $RM_SB/bin/pi
EOF
}

# rm_init_project — a tiny git repo with the verification scripts the plans name.
# The scripts take the control dir from the environment baked in at write time.
rm_init_project() {
  mkdir -p "$RM_PROJECT/verify"
  # quick: passes at once
  cat >"$RM_PROJECT/verify/quick.sh" <<EOF
#!/usr/bin/env bash
echo "\$(date +%s) pid=\$\$" >>"$RM_CTRL/script.starts"
test -s worker-result.txt
EOF
  # gated: records that it launched, waits for the gate file, then records the
  # start marker, sleeps, and finishes. The gate lets a scenario pick whether
  # the restart lands before the script's real work begins or in the middle.
  # The script does nothing about its parent going away: whether it survives a
  # restart is decided by the restart kill mode (RM_KILL_MODE), as in a unit.
  cat >"$RM_PROJECT/verify/gated.sh" <<EOF
#!/usr/bin/env bash
echo "\$(date +%s) pid=\$\$" >>"$RM_CTRL/script.launched"
while [[ ! -e "$RM_CTRL/script.gate" ]]; do sleep 0.5; done
echo "\$(date +%s) pid=\$\$" >>"$RM_CTRL/script.starts"
sleep "\$(cat "$RM_CTRL/script_sleep" 2>/dev/null || echo 4)"
echo "\$(date +%s) done" >>"$RM_CTRL/script.finished"
test -s worker-result.txt
EOF
  chmod +x "$RM_PROJECT/verify/"*.sh
  (
    cd "$RM_PROJECT" || exit 1
    env -u TMUX git init -q -b main . &&
      git config user.email matrix@example.invalid &&
      git config user.name restart-matrix &&
      printf '%s\n' '# restart matrix project' >README.md &&
      git add -A && git commit -q -m "initial"
  ) || rm_die "could not initialise sandbox project repo"
}

# ------------------------------------------------------------ daemon control ----

# Every tmux call against the sandbox goes through this wrapper.
rm_tmux() { env -u TMUX TMUX_TMPDIR="$RM_TMUX" tmux "$@"; }

rm_api() { # METHOD PATH [JSON]
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -s --max-time 20 -X "$method" -H 'Content-Type: application/json' -d "$body" "$RM_BASE$path"
  else
    curl -s --max-time 20 -X "$method" "$RM_BASE$path"
  fi
}

rm_healthy() { curl -s --max-time 2 "$RM_BASE/api/health" 2>/dev/null | jq -e . >/dev/null 2>&1; }

rm_uptime() { rm_api GET /api/health | jq -r '.uptime_seconds // empty' 2>/dev/null; }

# rm_wait_until TIMEOUT_SECONDS COMMAND... — polls COMMAND every half second.
rm_wait_until() {
  local timeout="$1"
  shift
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  return 1
}

# rm_launch_daemon — starts the sandbox daemon from the generated config and
# records its pid. env -u TMUX keeps any ambient multiplexer out; TMUX_TMPDIR is
# the throwaway socket dir.
rm_launch_daemon() {
  local bin
  bin="$(rm_find_binary)"
  env -u TMUX TMUX_TMPDIR="$RM_TMUX" ${RM_DAEMON_ENV[@]+"${RM_DAEMON_ENV[@]}"} "$bin" --config "$RM_CONFIG" start --foreground \
    >>"$RM_DAEMON_LOG" 2>&1 &
  RM_DAEMON_PID=$!
  echo "$RM_DAEMON_PID" >"$RM_PIDFILE"
  # Record this sandbox's daemon in the shared lifecycle state (config path
  # as its marker) so the shell that sourced this lib can end it later by
  # recorded PID with a cmdline check — never by name, even on interrupt.
  if declare -F td_track_pid >/dev/null 2>&1; then
    td_track_pid "$RM_DAEMON_PID" "$RM_CONFIG" 2>/dev/null || true
  fi
  if ! rm_wait_until 45 rm_healthy; then
    rm_log "sandbox daemon did not answer health; log tail:"
    tail -20 "$RM_DAEMON_LOG" >&2
    return 1
  fi
  return 0
}

# rm_verify_sandbox_pid PID — the sandbox pid contract: alive, a datawatch
# process, and its command line carries this sandbox's config path.
rm_verify_sandbox_pid() {
  local pid="$1" cmd
  [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 1 ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  [[ -r "/proc/$pid/cmdline" ]] || return 1
  cmd="$(tr '\0' ' ' <"/proc/$pid/cmdline")"
  [[ "$cmd" == *datawatch* && "$cmd" == *"$RM_CONFIG"* ]]
}

rm_daemon_start() {
  rm_launch_daemon || return 1
  rm_verify_sandbox_pid "$RM_DAEMON_PID" || rm_die "started daemon pid $RM_DAEMON_PID does not verify as this sandbox's daemon"
  rm_log "sandbox daemon up: pid $RM_DAEMON_PID, $RM_BASE"
}

# rm_direct_children PID — pids whose parent is PID, one per line.
rm_direct_children() {
  local d stat ppid
  for d in /proc/[0-9]*; do
    stat="$(cat "$d/stat" 2>/dev/null)" || continue
    # field 4 is the parent pid; the command name (field 2) may hold spaces, so
    # cut at the closing parenthesis first
    ppid="$(sed -E 's/^[0-9]+ \(.*\) [A-Za-z] ([0-9]+) .*/\1/' <<<"$stat")"
    [[ "$ppid" == "$1" ]] && printf '%s\n' "${d#/proc/}"
  done
}

# RM_KILL_MODE chooses what a restart ends, mirroring the unit's KillMode:
#   control-group (default)  TERM the daemon pid and, by pid, each of its direct
#                            children, as a service manager with KillMode
#                            control-group does for every process in the unit
#   process                  TERM the daemon pid only, as KillMode process does
# Limitation: this is a simulation by pid. Only direct children are ended, not
# grandchildren, and a multiplexer server the daemon started is not in the daemon's
# process tree (it detaches), so it is never ended here; under a real control group
# it would be, unless it runs in its own unit as the production setup does.
RM_KILL_MODE="${RM_KILL_MODE:-control-group}"

# rm_daemon_restart — TERM to the recorded pid (plus its direct children in
# control-group mode), wait for the daemon to exit, then relaunch from the same
# config. Sets RM_RESTART_* for the record.
rm_daemon_restart() {
  local pid="$RM_DAEMON_PID" t0=$SECONDS child kids=()
  rm_verify_sandbox_pid "$pid" || rm_die "recorded pid '$pid' is not this sandbox's live daemon; refusing to signal it"
  RM_RESTART_OLD_PID="$pid"
  RM_RESTART_UPTIME_BEFORE="$(rm_uptime)"
  case "$RM_KILL_MODE" in
    control-group)
      while IFS= read -r child; do [[ -n "$child" ]] && kids+=("$child"); done < <(rm_direct_children "$pid")
      ;;
    process) ;;
    *) rm_die "RM_KILL_MODE must be control-group or process (got '$RM_KILL_MODE')" ;;
  esac
  RM_RESTART_CHILDREN="${kids[*]:-}"
  kill -TERM "$pid"
  # the pids were read as this daemon's direct children an instant ago
  for child in "${kids[@]}"; do kill -TERM "$child" 2>/dev/null || true; done
  RM_RESTART_TERM_CLEAN=true
  if ! rm_wait_until 90 bash -c "! kill -0 $pid 2>/dev/null"; then
    # Still alive after a long grace: this pid is the sandbox daemon recorded
    # above (verified a moment ago), so ending it is within scope.
    RM_RESTART_TERM_CLEAN=false
    kill -KILL "$pid" 2>/dev/null || true
    rm_wait_until 10 bash -c "! kill -0 $pid 2>/dev/null" || rm_die "sandbox daemon pid $pid will not exit"
  fi
  wait "$pid" 2>/dev/null || true
  # RM_DOWNTIME keeps the daemon down that many seconds before relaunch, so a
  # scenario can put a scheduled job due while the daemon is down.
  [[ -n "${RM_DOWNTIME:-}" ]] && sleep "$RM_DOWNTIME"
  RM_RESTART_DOWN_SECONDS=$((SECONDS - t0))
  rm_launch_daemon || return 1
  RM_RESTART_NEW_PID="$RM_DAEMON_PID"
  RM_RESTART_UPTIME_AFTER="$(rm_uptime)"
  rm_log "sandbox daemon restarted: pid $RM_RESTART_OLD_PID -> $RM_RESTART_NEW_PID (term_clean=$RM_RESTART_TERM_CLEAN)"
}

# rm_sandbox_down — stops the sandbox daemon by its recorded pid, ends any
# leftover sandbox sessions one by one (never the whole multiplexer server), and
# removes the data dir unless RM_KEEP is set.
rm_sandbox_down() {
  local pid="${RM_DAEMON_PID:-}" s
  if [[ -n "$pid" ]] && rm_verify_sandbox_pid "$pid"; then
    kill -TERM "$pid" 2>/dev/null || true
    rm_wait_until 30 bash -c "! kill -0 $pid 2>/dev/null" || kill -KILL "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [[ -n "${RM_TMUX:-}" && -d "$RM_TMUX" ]]; then
    while IFS= read -r s; do
      [[ -n "$s" ]] && rm_tmux kill-session -t "$s" 2>/dev/null || true
    done < <(rm_tmux list-sessions -F '#{session_name}' 2>/dev/null)
  fi
  rm_node_stub_stop
  RM_DAEMON_PID=""
}

# ------------------------------------------------- pi workers on two nodes ----
#
# Opt-in plumbing for the worker-node-placement state, which needs pi-prompt
# workers placed on compute nodes. A state calls it as
#
#   RM_PI_NODES=1; rm_sandbox_new NAME; unset RM_PI_NODES   # config gets the pi blocks
#   rm_pi_nodes_setup                                       # fake pi, pi config dir, node stub
#   rm_daemon_start
#   rm_pi_nodes_register                                    # compute nodes and LLM entries
#
# Everything lives under the sandbox dir ($RM_SB), which rm_sandbox_finish removes.
#
#   - bin/pi          a fake pi: it records its start in <ctrl>/pi.starts (with the
#                     provider endpoint the daemon bound it to), then only sleeps until
#                     <ctrl>/pi.release exists or <ctrl>/pi_seconds pass, and prints a
#                     line. No network, no model.
#   - pi-agent/       the pi config directory the daemon's launches read
#                     (PI_CODING_AGENT_DIR): a provider table naming the "ollama" provider
#   - node-stub.py    a loopback listener on each node's address that answers the model
#                     residency check (GET /api/ps) the daemon makes before it starts a
#                     worker on a node of a multi-node entry. Without it the check fails
#                     and the worker fails over or fails. It serves nothing else.
# The daemon gets the fake pi directory first on its PATH and PI_CODING_AGENT_DIR through
# RM_DAEMON_ENV, so a real pi on the host can never be reached.

RM_PI_MODEL="ollama/qwen3.8:27b"
RM_NODE_A="matrix-node-a"
RM_NODE_B="matrix-node-b"

rm_pi_nodes_setup() {
  mkdir -p "$RM_SB/bin" "$RM_SB/pi-agent"
  cat >"$RM_SB/bin/pi" <<EOF
#!/usr/bin/env bash
ctrl="$RM_CTRL"
[[ "\${1:-}" == --version ]] && { echo "fake-pi 0.0.0"; exit 0; }
base="\$(sed -nE 's/.*"baseUrl"[^"]*"([^"]*)".*/\1/p' "\${PI_CODING_AGENT_DIR:-/nonexistent}/models.json" 2>/dev/null | head -n 1)"
echo "\$(date +%s) pid=\$\$ base=\${base:-none}" >>"\$ctrl/pi.starts"
echo "fake pi started"
end=\$((SECONDS + \$(cat "\$ctrl/pi_seconds" 2>/dev/null || echo 300)))
while [[ ! -e "\$ctrl/pi.release" ]] && ((SECONDS < end)); do sleep 1; done
echo "\$(date +%s) pid=\$\$" >>"\$ctrl/pi.done"
echo "fake pi done"
EOF
  chmod +x "$RM_SB/bin/pi"
  # the provider table a node-bound launch rewrites: only the provider's baseUrl changes
  cat >"$RM_SB/pi-agent/models.json" <<EOF
{"providers": {"ollama": {"baseUrl": "http://127.0.0.1:1/v1", "api": "openai-completions", "apiKey": "none", "models": [{"id": "qwen3.8:27b"}]}}}
EOF
  cat >"$RM_CTRL/node-stub.py" <<'EOF'
import json, sys, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

model = sys.argv[1]


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.rstrip("/") == "/api/ps":
            code, body = 200, {"models": [{"name": model, "model": model}]}
        else:
            code, body = 404, {}
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass


servers = [ThreadingHTTPServer(("127.0.0.1", int(p)), Handler) for p in sys.argv[2:]]
for srv in servers[1:]:
    threading.Thread(target=srv.serve_forever, daemon=True).start()
servers[0].serve_forever()
EOF
  RM_NODE_PORT_A="$(rm_free_port)"
  RM_NODE_PORT_B="$(rm_free_port)"
  while [[ "$RM_NODE_PORT_B" == "$RM_NODE_PORT_A" ]]; do RM_NODE_PORT_B="$(rm_free_port)"; done
  python3 -I "$RM_CTRL/node-stub.py" "${RM_PI_MODEL#*/}" "$RM_NODE_PORT_A" "$RM_NODE_PORT_B" >>"$RM_SB/node-stub.log" 2>&1 &
  RM_NODE_STUB_PID=$!
  RM_DAEMON_ENV=("PATH=$RM_SB/bin:$PATH" "PI_CODING_AGENT_DIR=$RM_SB/pi-agent")
  export RM_NODE_PORT_A RM_NODE_PORT_B
}

# rm_node_stub_stop — ends the node stub by the pid recorded when it was started, after
# checking that pid still runs THIS sandbox's stub script.
rm_node_stub_stop() {
  local pid="${RM_NODE_STUB_PID:-}"
  [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 1 ]] || return 0
  if [[ -r "/proc/$pid/cmdline" ]] && tr '\0' ' ' <"/proc/$pid/cmdline" | grep -qF "$RM_CTRL/node-stub.py"; then
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  RM_NODE_STUB_PID=""
}

# rm_pi_nodes_register — two compute nodes with distinct explicit addresses and one
# worker slot each, an ollama entry (the engine the model's provider resolves to) and a
# pi-prompt entry, both listing the two nodes. Returns 1 when the daemon refuses any.
rm_pi_nodes_register() {
  local n port models body rc=0
  for n in "$RM_NODE_A:$RM_NODE_PORT_A" "$RM_NODE_B:$RM_NODE_PORT_B"; do
    port="${n#*:}"
    rm_api POST /api/compute/nodes "$(jq -nc --arg name "${n%%:*}" --arg addr "http://127.0.0.1:$port" \
      '{name: $name, kind: "ollama", address: $addr, session_slots: 1, max_concurrent_sessions: 1}')" |
      jq -e '.name' >/dev/null 2>&1 || { rm_log "compute node ${n%%:*} was not accepted"; rc=1; }
  done
  models="$(jq -nc --arg a "$RM_NODE_A" --arg b "$RM_NODE_B" --arg m "${RM_PI_MODEL#*/}" '[{node: $a, model: $m}, {node: $b, model: $m}]')"
  for n in ollama pi-prompt; do
    body="$(jq -nc --arg name "$n" --arg a "$RM_NODE_A" --arg b "$RM_NODE_B" --argjson models "$models" \
      '{name: $name, kind: $name, compute_nodes: [$a, $b], models: $models, max_inflight: 4}')"
    # The daemon may have created the entry itself from the config (the pi-prompt binary), so
    # replace it in place; deleting it would only tombstone it. Create it when it is absent.
    { rm_api PUT "/api/llms/$n" "$body" | jq -e '.name' >/dev/null 2>&1 ||
      rm_api POST /api/llms "$body" | jq -e '.name' >/dev/null 2>&1; } || { rm_log "llm entry $n was not accepted"; rc=1; }
  done
  return $rc
}

# --------------------------------------------------------- scenario plumbing ----

# rm_write_plan [SCRIPT_REL] — one story, one task. With SCRIPT_REL the task is
# verified by that daemon-run script. Without it the task plans no files and
# states why it has no script (the plan checker exempts such a task), so the
# verdict comes from a verifier session instead.
rm_write_plan() {
  local script="${1:-}"
  jq -n --arg script "$script" '
    def vfy: if $script != "" then {verification_script: $script}
             else {verification_script_reason: "judged by a verifier session; this restart-matrix state has no script"} end;
    def planned: if $script != "" then {files: ["worker-result.txt"], files_generated: ["worker-result.txt"]} else {} end;
    {
      title: "matrix plan",
      stories: [({
        title: "matrix story",
        description: "one story for the restart matrix",
        depends_on: [],
        tasks: [({
          title: "matrix task",
          spec: "Do the one unit of work the matrix probes.",
          depends_on: []
        } + planned + vfy)]
      } + planned + vfy)]
    }' >"$RM_CTRL/plan.json"
}

rm_prd_create() { # prints the PRD id
  local body
  body="$(jq -n --arg dir "$RM_PROJECT" '{spec: "Restart matrix probe: write a result file.", project_dir: $dir, backend: "shell"}')"
  rm_api POST /api/autonomous/prds "$body" | jq -r '.id // .prd.id // empty'
}

rm_prd_json() { rm_api GET "/api/autonomous/prds/$1"; }
rm_prd_status() { rm_prd_json "$1" | jq -r '.status // .prd.status // empty'; }

# rm_prd_git_enable PRD — gives the PRD git settings, so its task sessions run
# git-enabled and a finished task's work is committed by the daemon.
rm_prd_git_enable() {
  rm_api POST "/api/autonomous/prds/$1/set_git" '{"auto_pr":false,"base_branch":"main"}' >/dev/null
}

# rm_prd_script_guardrails PRD — a task's verification script runs in the
# per-task guardrail step, so a PRD that verifies through a script needs one
# configured. A clean script exit passes that guardrail without a judge call.
rm_prd_script_guardrails() {
  rm_api PUT "/api/autonomous/prds/$1/guardrails" '{"per_task_guardrails":["rules-review"]}' >/dev/null
}

# First task of the PRD, as JSON (the matrix PRDs have exactly one task).
rm_task_json() {
  rm_prd_json "$1" | jq -c '(.prd // .) as $p | [$p.stories[]?.tasks[]?][0] // {}'
}
rm_task_field() { rm_task_json "$1" | jq -r ".$2 // empty"; }

rm_prd_status_is() { [[ "$(rm_prd_status "$1")" == "$2" ]]; }
rm_task_status_is() { [[ "$(rm_task_field "$1" status)" == "$2" ]]; }

# rm_count_lines FILE — number of lines, 0 when absent.
rm_count_lines() { [[ -f "$1" ]] && wc -l <"$1" | tr -d ' ' || echo 0; }

rm_ctrl_count() { rm_count_lines "$RM_CTRL/$1"; }

# rm_last_pid FILE — pid recorded on the last line of a *.starts style file.
rm_last_pid() { [[ -f "$1" ]] && tail -1 "$1" | sed -nE 's/.*pid=([0-9]+).*/\1/p'; }

rm_pid_alive() { [[ -n "${1:-}" ]] && kill -0 "$1" 2>/dev/null; }

# ------------------------------------------------------------- snapshots ----

# rm_session_rows — one JSON array of the sandbox daemon's sessions.
rm_session_rows() {
  rm_api GET /api/sessions | jq -c 'if type=="array" then . else (.sessions // []) end' 2>/dev/null || echo '[]'
}

rm_pane_pids() {
  rm_tmux list-panes -a -F '#{session_name}:#{pane_pid}' 2>/dev/null | jq -R . | jq -sc .
}

# rm_snapshot LABEL PRD_ID OUTFILE — state of everything the matrix compares
# across a restart, as one JSON document. Also keeps copies of the worker's
# counter file and the live session logs so lost output can be measured.
rm_snapshot() {
  local label="$1" prd="$2" out="$3" task sess_rows pane_pids copydir
  task="$(rm_task_json "$prd")"
  sess_rows="$(rm_session_rows)"
  pane_pids="$(rm_pane_pids)"
  copydir="$RM_SB/snap-$label"
  rm -rf "$copydir"
  mkdir -p "$copydir/logs"
  [[ -f "$RM_PROJECT/worker-counter.txt" ]] && cp "$RM_PROJECT/worker-counter.txt" "$copydir/worker-counter.txt"
  # Copies are keyed by session id: every session's log is called output.log, so
  # the file name alone would collide.
  local sid lf
  while IFS=$'\t' read -r sid lf; do
    [[ -n "$lf" && -f "$lf" ]] && cp "$lf" "$copydir/logs/$sid.log"
  done < <(printf '%s' "$sess_rows" | jq -r '.[]? | [(.full_id // .id // "unknown"), (.log_file // "")] | @tsv')
  local prd_info
  prd_info="$(rm_prd_json "$prd" | jq -c '(.prd // .) | {blocked_reason, failure_reason, rejection_reason}' 2>/dev/null || echo '{}')"
  jq -n \
    --arg label "$label" \
    --arg at "$(date -u +%FT%TZ)" \
    --arg daemon_pid "${RM_DAEMON_PID:-}" \
    --arg uptime "$(rm_uptime)" \
    --arg prd_status "$(rm_prd_status "$prd")" \
    --argjson task "$task" \
    --argjson prd_info "$prd_info" \
    --argjson sessions "$sess_rows" \
    --argjson panes "$pane_pids" \
    --arg copydir "$copydir" \
    --argjson planner_starts "$(rm_ctrl_count planner.starts)" \
    --argjson worker_starts "$(rm_ctrl_count worker.starts)" \
    --argjson verifier_starts "$(rm_ctrl_count verifier.starts)" \
    --argjson guardrail_starts "$(rm_ctrl_count guardrail.starts)" \
    --argjson script_launched "$(rm_ctrl_count script.launched)" \
    --argjson script_starts "$(rm_ctrl_count script.starts)" \
    --argjson script_finished "$(rm_ctrl_count script.finished)" \
    --argjson counter_bytes "$( [[ -f "$RM_PROJECT/worker-counter.txt" ]] && stat -c%s "$RM_PROJECT/worker-counter.txt" || echo 0)" \
    '{
      label: $label, at: $at,
      daemon: {pid: $daemon_pid, uptime_seconds: ($uptime | tonumber? // null)},
      prd_status: $prd_status,
      prd_info: $prd_info,
      task: {
        id: ($task.id // ""), status: ($task.status // ""),
        retry_count: ($task.retry_count // 0),
        prior_attempt_count: ($task.prior_attempt_count // 0),
        session_id: ($task.session_id // ""), adopted_session: ($task.adopted_session // false),
        error: ($task.error // ""), wait_reason: ($task.wait_reason // "")
      },
      sessions: ($sessions | map({id: (.id // .full_id // ""), full_id: (.full_id // ""), name: (.name // ""), state: (.state // ""), log_file: (.log_file // ""), tmux_session: (.tmux_session // "")})),
      session_ids: ($sessions | map(.full_id // .id // "")),
      pane_pids: $panes,
      counts: {
        planner_starts: $planner_starts, worker_starts: $worker_starts,
        verifier_starts: $verifier_starts, guardrail_starts: $guardrail_starts,
        script_launched: $script_launched, script_starts: $script_starts, script_finished: $script_finished
      },
      worker_counter_bytes: $counter_bytes,
      copies: $copydir
    }' >"$out"
}

# rm_lost_bytes BEFORE_FILE AFTER_FILE — bytes of BEFORE that AFTER does not
# still start with. Missing or empty BEFORE loses nothing; a missing AFTER
# loses all of BEFORE.
rm_lost_bytes() {
  local before="$1" after="$2" bs msg n
  [[ -s "$before" ]] || { echo 0; return; }
  bs="$(stat -c%s "$before")"
  [[ -f "$after" ]] || { echo "$bs"; return; }
  if msg="$(LC_ALL=C cmp "$before" "$after" 2>&1)"; then
    echo 0
    return
  fi
  # cmp quotes file names in its message, so match on the path itself rather than
  # on a quoted form of it.
  case "$msg" in
    *"EOF on"*"$after"*) echo $((bs - $(stat -c%s "$after"))) ;;
    *"EOF on"*) echo 0 ;;
    *)
      n="$(printf '%s' "$msg" | sed -nE 's/.*byte ([0-9]+).*/\1/p')"
      echo $((bs - ${n:-1} + 1))
      ;;
  esac
}

# rm_total_lost_bytes BEFORE_SNAPSHOT AFTER_SNAPSHOT — output lost across the
# worker counter file plus every session log that existed before the restart.
rm_total_lost_bytes() {
  local b="$1" a="$2" bc ac total=0 f lost id path
  bc="$(jq -r .copies "$b")"
  ac="$(jq -r .copies "$a")"
  lost="$(rm_lost_bytes "$bc/worker-counter.txt" "$ac/worker-counter.txt")"
  total=$((total + lost))
  if [[ -d "$bc/logs" ]]; then
    for f in "$bc"/logs/*; do
      [[ -e "$f" ]] || continue
      id="$(basename "$f" .log)"
      if [[ -f "$ac/logs/$id.log" ]]; then
        lost="$(rm_lost_bytes "$f" "$ac/logs/$id.log")"
      else
        # The session left the live list, so it has no copy after the restart. Its
        # log file may still be on disk: compare with that, else it is all lost.
        path="$(jq -r --arg id "$id" '[.sessions[] | select((.full_id // .id) == $id) | .log_file][0] // empty' "$b")"
        lost="$(rm_lost_bytes "$f" "$path")"
      fi
      total=$((total + lost))
    done
  fi
  echo "$total"
}

# rm_duplicate_sessions SNAPSHOT — how many EXTRA live sessions exist beyond one
# per role. A role is the session name's kind: decompose, verify, guardrail or
# worker. Terminal sessions do not count.
rm_duplicate_sessions() {
  jq -r '
    [.sessions[]
      | select(.state | IN("complete","failed","killed") | not)
      | (if (.name | test("decompose|planning")) then "planner"
         elif (.name | test("verify")) then "verifier"
         elif (.name | test("guardrail")) then "guardrail"
         else "worker" end)]
    | group_by(.) | map(length - 1 | if . < 0 then 0 else . end) | add // 0' "$1"
}

# --------------------------------------------------- per-state record + checks ----

# Scenario protocol: rm_state_begin NAME DESCRIPTION; rm_check ... ; rm_state_end.

rm_state_begin() {
  RM_STATE="$1"
  RM_STATE_DESC="$2"
  RM_STATE_PASS=0
  RM_STATE_FAIL=0
  RM_CRITERIA_FILE="$RM_REC/$RM_STATE.criteria.jsonl"
  : >"$RM_CRITERIA_FILE"
  printf '\n== state: %s — %s\n' "$RM_STATE" "$RM_STATE_DESC"
}

# rm_check NAME RESULT DETAIL — RESULT is 0 for pass, non-zero for fail.
rm_check() {
  local name="$1" result="$2" detail="${3:-}" verdict
  if [[ "$result" == "0" ]]; then
    verdict=PASS
    RM_STATE_PASS=$((RM_STATE_PASS + 1))
    printf '  PASS  %s%s\n' "$name" "${detail:+ — $detail}"
  else
    verdict=FAIL
    RM_STATE_FAIL=$((RM_STATE_FAIL + 1))
    printf '  FAIL  %s%s\n' "$name" "${detail:+ — $detail}"
  fi
  jq -nc --arg n "$name" --arg v "$verdict" --arg d "$detail" '{criterion: $n, result: $v, detail: $d}' >>"$RM_CRITERIA_FILE"
}

# rm_check_cond NAME DETAIL COMMAND... — passes when COMMAND succeeds.
rm_check_cond() {
  local name="$1" detail="$2"
  shift 2
  if "$@" >/dev/null 2>&1; then rm_check "$name" 0 "$detail"; else rm_check "$name" 1 "$detail"; fi
}

# rm_state_end BEFORE_SNAPSHOT AFTER_SNAPSHOT [NOTE] — writes the state's JSON record.
rm_state_end() {
  local before="$1" after="$2" note="${3:-}" result=PASS
  [[ $RM_STATE_FAIL -gt 0 ]] && result=FAIL
  local before_json='null' after_json='null'
  [[ -f "$before" ]] && before_json="$(cat "$before")"
  [[ -f "$after" ]] && after_json="$(cat "$after")"
  jq -n \
    --arg state "$RM_STATE" --arg desc "$RM_STATE_DESC" --arg result "$result" --arg note "$note" \
    --argjson before "$before_json" --argjson after "$after_json" \
    --slurpfile criteria "$RM_CRITERIA_FILE" \
    --arg old_pid "${RM_RESTART_OLD_PID:-}" --arg new_pid "${RM_RESTART_NEW_PID:-}" \
    --arg term_clean "${RM_RESTART_TERM_CLEAN:-}" --arg down "${RM_RESTART_DOWN_SECONDS:-}" \
    --arg kill_mode "$RM_KILL_MODE" --arg children "${RM_RESTART_CHILDREN:-}" \
    --argjson pass "$RM_STATE_PASS" --argjson fail "$RM_STATE_FAIL" \
    --arg lost "${RM_DERIVED_LOST_BYTES:-0}" --arg dup "${RM_DERIVED_DUPLICATES:-0}" \
    --arg redone "${RM_DERIVED_REDONE:-false}" \
    '{
      state: $state, description: $desc, result: $result, note: $note,
      restart: {old_pid: $old_pid, new_pid: $new_pid, term_clean: ($term_clean == "true"), down_seconds: ($down | tonumber? // null), kill_mode: $kill_mode, children_terminated: ($children | split(" ") | map(select(. != "")))},
      derived: {
        task_status_before: ($before.task.status // null), task_status_after: ($after.task.status // null),
        session_ids_before: ($before.session_ids // []), session_ids_after: ($after.session_ids // []),
        pane_pids_before: ($before.pane_pids // []), pane_pids_after: ($after.pane_pids // []),
        retry_count_before: ($before.task.retry_count // null), retry_count_after: ($after.task.retry_count // null),
        duplicate_sessions: ($dup | tonumber? // 0),
        lost_output_bytes: ($lost | tonumber? // 0),
        redone: ($redone == "true")
      },
      criteria: $criteria, pass: $pass, fail: $fail,
      before: $before, after: $after
    }' >"$RM_REC/$RM_STATE.json"
  printf '  -> %s (%d pass, %d fail), record: %s\n' "$result" "$RM_STATE_PASS" "$RM_STATE_FAIL" "$RM_REC/$RM_STATE.json"
  unset RM_DERIVED_LOST_BYTES RM_DERIVED_DUPLICATES RM_DERIVED_REDONE
  RM_RESTART_OLD_PID="" RM_RESTART_NEW_PID="" RM_RESTART_TERM_CLEAN="" RM_RESTART_DOWN_SECONDS="" RM_RESTART_CHILDREN=""
}

# rm_write_summary — combined summary of every state record under the work
# root, as JSON and a table. Several state scripts may share one work root.
rm_write_summary() {
  local files=() f
  for f in "$RM_REC"/*.json; do
    [[ -f "$f" ]] || continue
    [[ "$(basename "$f")" == summary.json ]] && continue
    files+=("$f")
  done
  if ((${#files[@]} == 0)); then
    echo '{"states":[],"pass":0,"fail":0}' >"$RM_REC/summary.json"
    return
  fi
  jq -s '{
    generated_at: (now | todate),
    mode: (env.RM_MODE // "measure"),
    states: map({state, description, result, pass, fail, derived, failed_criteria: [.criteria[] | select(.result == "FAIL") | {criterion, detail}]}),
    pass: (map(select(.result == "PASS")) | length),
    fail: (map(select(.result == "FAIL")) | length)
  }' "${files[@]}" >"$RM_REC/summary.json"
  printf '\n== summary (%s)\n' "$RM_REC/summary.json"
  jq -r '.states[] | "  \(.result)  \(.state)  (\(.pass) pass, \(.fail) fail)" + (if (.failed_criteria | length) > 0 then "\n        failing: " + ([.failed_criteria[].criterion] | join(", ")) else "" end)' "$RM_REC/summary.json"
}

# rm_total_failures — number of failing states across the work root.
rm_total_failures() { jq -r '.fail // 0' "$RM_REC/summary.json" 2>/dev/null || echo 0; }

# rm_sandbox_finish — stops the scenario's daemon and sessions and, unless
# RM_KEEP=1, removes the scenario's sandbox dir. The JSON records stay.
rm_sandbox_finish() {
  rm_sandbox_down
  if [[ "${RM_KEEP:-0}" != 1 && -n "${RM_SB:-}" && -d "$RM_SB" && "$RM_SB" == "$RM_ROOT"/* ]]; then
    rm -rf "$RM_SB"
  fi
}

# rm_install_traps — the daemon-cleanup safety net for every exit path.
# EXIT covers normal exits, failures and early returns; INT, TERM and HUP must
# each be trapped in their own right, because a signal that is neither trapped
# nor ignored kills the shell without running the EXIT trap, leaving exactly
# one thing alive: the sandbox daemon. All four paths run rm_sandbox_finish —
# the same order as the happy path (end the recorded daemon pid, its sessions
# one by one; remove the sandbox dir unless RM_KEEP=1). It is idempotent, so
# the second run after a signal is a no-op.
rm_install_traps() {
  trap 'rm_sandbox_finish' EXIT
  trap 'rm_sandbox_finish; trap - EXIT INT TERM HUP; exit 130' INT
  trap 'rm_sandbox_finish; trap - EXIT INT TERM HUP; exit 143' TERM
  trap 'rm_sandbox_finish; trap - EXIT INT TERM HUP; exit 129' HUP
}
