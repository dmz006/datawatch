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
  mkdir -p "$RM_REC"
  export RM_ROOT RM_REC
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
    : >"\$dir/worker-counter.txt"
    for i in \$(seq 1 "\$ticks"); do
      echo "tick \$i" >>"\$dir/worker-counter.txt"
      echo "worker tick \$i"
      sleep 1
    done
    echo "work for this task" >"\$dir/worker-result.txt"
    echo "\$(date +%s) done" >>"\$ctrl/worker.done"
    ;;
esac
EOF
  chmod +x "$RM_CTRL/fake-backend.sh"
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
memory:
  enabled: false
session:
  llm_backend: shell
  max_sessions: 20
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
  per_task_guardrails: [rules]
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
  # A service manager ends a daemon's child processes when it restarts the
  # daemon; this harness signals only the daemon pid, so the script ends itself
  # when the daemon that launched it is gone, which has the same effect.
  cat >"$RM_PROJECT/verify/gated.sh" <<EOF
#!/usr/bin/env bash
parent=\$PPID
alive() { kill -0 "\$parent" 2>/dev/null; }
echo "\$(date +%s) pid=\$\$" >>"$RM_CTRL/script.launched"
while [[ ! -e "$RM_CTRL/script.gate" ]]; do alive || exit 143; sleep 0.5; done
echo "\$(date +%s) pid=\$\$" >>"$RM_CTRL/script.starts"
end=\$((SECONDS + \$(cat "$RM_CTRL/script_sleep" 2>/dev/null || echo 4)))
while ((SECONDS < end)); do alive || exit 143; sleep 0.5; done
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
  env -u TMUX TMUX_TMPDIR="$RM_TMUX" "$bin" --config "$RM_CONFIG" start --foreground \
    >>"$RM_DAEMON_LOG" 2>&1 &
  RM_DAEMON_PID=$!
  echo "$RM_DAEMON_PID" >"$RM_PIDFILE"
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

# rm_daemon_restart — TERM to the recorded pid only, wait for it to exit, then
# relaunch from the same config. Sets RM_RESTART_* for the record.
rm_daemon_restart() {
  local pid="$RM_DAEMON_PID" t0=$SECONDS
  rm_verify_sandbox_pid "$pid" || rm_die "recorded pid '$pid' is not this sandbox's live daemon; refusing to signal it"
  RM_RESTART_OLD_PID="$pid"
  RM_RESTART_UPTIME_BEFORE="$(rm_uptime)"
  kill -TERM "$pid"
  RM_RESTART_TERM_CLEAN=true
  if ! rm_wait_until 90 bash -c "! kill -0 $pid 2>/dev/null"; then
    # Still alive after a long grace: this pid is the sandbox daemon recorded
    # above (verified a moment ago), so ending it is within scope.
    RM_RESTART_TERM_CLEAN=false
    kill -KILL "$pid" 2>/dev/null || true
    rm_wait_until 10 bash -c "! kill -0 $pid 2>/dev/null" || rm_die "sandbox daemon pid $pid will not exit"
  fi
  wait "$pid" 2>/dev/null || true
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
  RM_DAEMON_PID=""
}

# --------------------------------------------------------- scenario plumbing ----

# rm_write_plan [SCRIPT_REL] — one story, one task. With SCRIPT_REL the task is
# verified by that daemon-run script; without it the task states why it has no
# script, so the verdict comes from a verifier session instead.
rm_write_plan() {
  local script="${1:-}"
  jq -n --arg script "$script" '
    def vfy: if $script != "" then {verification_script: $script}
             else {verification_script_reason: "judged by a verifier session; the restart matrix has no script for this state"} end;
    {
      title: "matrix plan",
      stories: [({
        title: "matrix story",
        description: "one story for the restart matrix",
        files: ["worker-result.txt"],
        depends_on: [],
        files_generated: ["worker-result.txt"],
        tasks: [({
          title: "matrix task",
          spec: "Write worker-result.txt in the project directory.",
          files: ["worker-result.txt"],
          depends_on: [],
          files_generated: ["worker-result.txt"]
        } + vfy)]
      } + vfy)]
    }' >"$RM_CTRL/plan.json"
}

rm_prd_create() { # prints the PRD id
  local body
  body="$(jq -n --arg dir "$RM_PROJECT" '{spec: "Restart matrix probe: write a result file.", project_dir: $dir, backend: "shell"}')"
  rm_api POST /api/autonomous/prds "$body" | jq -r '.id // .prd.id // empty'
}

rm_prd_json() { rm_api GET "/api/autonomous/prds/$1"; }
rm_prd_status() { rm_prd_json "$1" | jq -r '.status // .prd.status // empty'; }

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
  local lf
  while IFS= read -r lf; do
    [[ -n "$lf" && -f "$lf" ]] && cp "$lf" "$copydir/logs/$(basename "$lf")"
  done < <(printf '%s' "$sess_rows" | jq -r '.[]? | .log_file // empty')
  jq -n \
    --arg label "$label" \
    --arg at "$(date -u +%FT%TZ)" \
    --arg daemon_pid "${RM_DAEMON_PID:-}" \
    --arg uptime "$(rm_uptime)" \
    --arg prd_status "$(rm_prd_status "$prd")" \
    --argjson task "$task" \
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
      task: {
        id: ($task.id // ""), status: ($task.status // ""),
        retry_count: ($task.retry_count // 0),
        prior_attempt_count: ($task.prior_attempt_count // 0),
        session_id: ($task.session_id // ""), adopted_session: ($task.adopted_session // false)
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
  if msg="$(cmp "$before" "$after" 2>&1)"; then
    echo 0
    return
  fi
  case "$msg" in
    *"EOF on $before"*) echo 0 ;;
    *"EOF on $after"*) echo $((bs - $(stat -c%s "$after"))) ;;
    *)
      n="$(printf '%s' "$msg" | sed -nE 's/.*byte ([0-9]+).*/\1/p')"
      echo $((bs - ${n:-1} + 1))
      ;;
  esac
}

# rm_total_lost_bytes BEFORE_SNAPSHOT AFTER_SNAPSHOT — output lost across the
# worker counter file plus every session log that existed before the restart.
rm_total_lost_bytes() {
  local b="$1" a="$2" bc ac total=0 f lost
  bc="$(jq -r .copies "$b")"
  ac="$(jq -r .copies "$a")"
  lost="$(rm_lost_bytes "$bc/worker-counter.txt" "$ac/worker-counter.txt")"
  total=$((total + lost))
  if [[ -d "$bc/logs" ]]; then
    for f in "$bc"/logs/*; do
      [[ -e "$f" ]] || continue
      lost="$(rm_lost_bytes "$f" "$ac/logs/$(basename "$f")")"
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
    --argjson pass "$RM_STATE_PASS" --argjson fail "$RM_STATE_FAIL" \
    --arg lost "${RM_DERIVED_LOST_BYTES:-0}" --arg dup "${RM_DERIVED_DUPLICATES:-0}" \
    --arg redone "${RM_DERIVED_REDONE:-false}" \
    '{
      state: $state, description: $desc, result: $result, note: $note,
      restart: {old_pid: $old_pid, new_pid: $new_pid, term_clean: ($term_clean == "true"), down_seconds: ($down | tonumber? // null)},
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
  RM_RESTART_OLD_PID="" RM_RESTART_NEW_PID="" RM_RESTART_TERM_CLEAN="" RM_RESTART_DOWN_SECONDS=""
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
