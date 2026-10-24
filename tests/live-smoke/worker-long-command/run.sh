#!/usr/bin/env bash
# Sandbox repro: does a claude-code automaton worker survive a long silent shell wait?
#
#   run.sh <background|subagent|both> [--keep]   run one or both scenarios
#   run.sh clean                                 stop the sandbox daemon, remove the sandbox
#
# Env overrides: DURATION (seconds the worker waits, default 420), POLL_SECONDS (5),
# START_TIMEOUT (seconds to wait for a task session to appear, default 300),
# TASK_GRACE (seconds past DURATION before the harness gives up, default 900),
# SKIP_BUILD=1 (reuse the sandbox binary).
#
# Everything the harness creates lives under .sec-sandbox/worker-long-command/
# (git-ignored): a throwaway data dir, a rendered config, a private tmux socket
# directory, a copy of the binary, a nested git repo as the project dir, and
# results/<scenario>/. The production daemon is never touched.
set -uo pipefail
unset TMUX TMUX_TMPDIR

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../.." && pwd)"
SB="$REPO/.sec-sandbox/worker-long-command"
TEMPLATE="$REPO/testdata/datawatch.yaml"
PRD_IDS_FILE="$SB/state/prd-ids.txt"
DURATION="${DURATION:-420}"
POLL_SECONDS="${POLL_SECONDS:-5}"
START_TIMEOUT="${START_TIMEOUT:-300}"
TASK_GRACE="${TASK_GRACE:-900}"
TOKEN="dw-wlc-sandbox-token"
TASK_TITLE="wait for the long command"
KEEP=0
BASE=""

for t in go curl jq python3 git tmux claude; do
  command -v "$t" >/dev/null 2>&1 || { echo "missing tool on PATH: $t" >&2; exit 2; }
done

log() { printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
die() { log "ERROR: $*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
usage: run.sh <background|subagent|both> [--keep]
       run.sh clean
  background  claude-code worker starts a silent background sleep and waits on it
  subagent    claude-code worker delegates the same silent wait to a subagent
  both        background, then subagent
  --keep      leave the sandbox daemon and sessions running for inspection
  clean       stop the sandbox daemon (recorded pid only) and remove the sandbox
USAGE
}

# api METHOD PATH [JSON] — bearer-authenticated call to the sandbox daemon only.
api() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-sk -X "$method" -H "Authorization: Bearer $TOKEN")
  [[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' --data "$body")
  curl "${args[@]}" "$BASE$path" </dev/null
}

is_terminal() { [[ "$1" =~ ^(complete|completed|failed|killed)$ ]]; }

# Two free loopback ports, never the production daemon's ports.
pick_ports() {
  python3 - <<'PY'
import random
import socket

taken = {8080, 8443, 9090}
picked = []
rng = random.Random()
while len(picked) < 2:
    port = rng.randint(19600, 19999)
    if port in taken or port in picked:
        continue
    with socket.socket() as s:
        try:
            s.bind(("127.0.0.1", port))
        except OSError:
            continue
    picked.append(port)
print(*picked)
PY
}

daemon_pid() { [[ -f "$SB/state/daemon.pid" ]] && cat "$SB/state/daemon.pid"; }

daemon_alive() {
  local pid
  pid="$(daemon_pid)"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

# stop_daemon signals only the pid this harness recorded, and only after the
# live process's cmdline shows it is this sandbox's binary and config.
stop_daemon() {
  local pid cmd i
  pid="$(daemon_pid)"
  [[ -n "$pid" ]] || return 0
  if ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$SB/state/daemon.pid"
    return 0
  fi
  cmd=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)
  if [[ "$cmd" != *"$SB/bin/datawatch"* || "$cmd" != *"$SB/config.yaml"* ]]; then
    log "refusing to signal pid $pid: its cmdline is not this sandbox's daemon ($cmd)"
    return 1
  fi
  log "stopping sandbox daemon pid $pid (SIGTERM)"
  kill -TERM "$pid" 2>/dev/null
  for i in $(seq 1 30); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
  if kill -0 "$pid" 2>/dev/null; then
    log "pid $pid still alive after 30s; SIGKILL to the same recorded pid"
    kill -KILL "$pid" 2>/dev/null
  fi
  rm -f "$SB/state/daemon.pid"
}

prepare() {
  stop_daemon || die "a sandbox daemon from an earlier run is still running and could not be stopped"
  mkdir -p "$SB/bin" "$SB/data" "$SB/state" "$SB/results" "$SB/tmux"
  chmod 700 "$SB/tmux"
  if [[ ! -d "$SB/project/.git" ]]; then
    mkdir -p "$SB/project"
    git -C "$SB/project" init -q || die "git init of the sandbox project failed"
    git -C "$SB/project" -c user.name=wlc-sandbox -c user.email=wlc-sandbox@invalid \
      commit -q --allow-empty -m "sandbox project base" || die "base commit in the sandbox project failed"
  fi
  if [[ "${SKIP_BUILD:-0}" != 1 || ! -x "$SB/bin/datawatch" ]]; then
    log "building sandbox binary -> $SB/bin/datawatch"
    (cd "$REPO" && go build -o "$SB/bin/datawatch" ./cmd/datawatch/) || die "go build failed"
  fi
  local http_port tls_port
  read -r http_port tls_port < <(pick_ports)
  printf 'HTTP_PORT=%s\nTLS_PORT=%s\n' "$http_port" "$tls_port" > "$SB/state/ports.env"
  BASE="https://127.0.0.1:$tls_port"
  python3 "$HERE/lib/render_config.py" "$TEMPLATE" "$SB/config.yaml" "$SB/data" "$SB/project" \
    "$http_port" "$tls_port" "$TOKEN" || die "config render failed"
  : > "$PRD_IDS_FILE"
}

start_daemon() {
  env -u TMUX TMUX_TMPDIR="$SB/tmux" "$SB/bin/datawatch" start --foreground --config "$SB/config.yaml" \
    >> "$SB/daemon.log" 2>&1 &
  echo "$!" > "$SB/state/daemon.pid"
  log "sandbox daemon pid $! at $BASE (private tmux dir $SB/tmux)"
  local i
  for i in $(seq 1 60); do
    if curl -skf "$BASE/api/health" >/dev/null 2>&1; then
      log "sandbox daemon healthy"
      return 0
    fi
    daemon_alive || break
    sleep 1
  done
  tail -n 30 "$SB/daemon.log" >&2
  die "sandbox daemon did not become healthy"
}

cleanup_scenario() {
  local prd_id="$1" sid="$2" sstate=""
  api POST "/api/autonomous/prds/$prd_id/cancel" '{}' >/dev/null
  if [[ -n "$sid" && -f "$SB/data/sessions/$sid/session.json" ]]; then
    sstate=$(jq -r '.state // empty' "$SB/data/sessions/$sid/session.json" 2>/dev/null)
  fi
  if [[ -n "$sid" ]] && ! is_terminal "$sstate"; then
    api POST /api/sessions/kill "$(jq -n --arg id "$sid" '{id:$id}')" >/dev/null
    log "ended sandbox session $sid through the sandbox API"
  fi
}

# run_scenario NAME — drives one repro end to end and captures the evidence.
run_scenario() {
  local name="$1"
  local res="$SB/results/$name" proj="$SB/project" marker=".wlc-marker-$name"
  local offset prd_id story_id sid="" delivered="" end_epoch="" marker_epoch=""
  local timed_out=0 start_epoch deadline now prd_json tstatus="" sstate="" sdir
  rm -rf "$res"
  mkdir -p "$res"
  rm -f "$proj/$marker"
  sed -e "s/{{DURATION}}/$DURATION/g" -e "s|{{MARKER}}|$marker|g" "$HERE/specs/$name.txt" > "$res/task-spec.txt"
  offset=$(wc -c < "$SB/daemon.log")

  log "[$name] creating automaton: claude-code, haiku, bypassPermissions, DURATION=${DURATION}s"
  api POST /api/autonomous/prds "$(jq -n --arg spec "worker-long-command repro: $name" --arg dir "$proj" \
    '{spec:$spec, project_dir:$dir, backend:"claude-code", model:"haiku"}')" > "$res/01-prd-create.json"
  prd_id=$(jq -r '.id // .prd.id // empty' "$res/01-prd-create.json" 2>/dev/null)
  [[ -n "$prd_id" ]] || { log "[$name] create failed: $(head -c 300 "$res/01-prd-create.json")"; return 1; }
  echo "$prd_id" >> "$PRD_IDS_FILE"

  api POST "/api/autonomous/prds/$prd_id/set_permission_mode" \
    '{"permission_mode":"bypassPermissions","actor":"wlc-harness"}' > "$res/02-permission.json"
  api POST "/api/autonomous/prds/$prd_id/add_story" \
    '{"title":"long silent wait","actor":"wlc-harness"}' > "$res/03-add-story.json"
  story_id=$(api GET "/api/autonomous/prds/$prd_id" | jq -r '.stories[0].id // empty' 2>/dev/null)
  [[ -n "$story_id" ]] || { log "[$name] no story id on the automaton"; return 1; }
  api POST "/api/autonomous/prds/$prd_id/add_task" "$(jq -n --arg s "$story_id" --arg t "$TASK_TITLE" \
    --rawfile spec "$res/task-spec.txt" '{story_id:$s, title:$t, spec:$spec, actor:"wlc-harness"}')" > "$res/04-add-task.json"
  api POST "/api/autonomous/prds/$prd_id/approve" '{"actor":"wlc-harness"}' > "$res/05-approve.json"
  api POST "/api/autonomous/prds/$prd_id/run" '{}' > "$res/06-run.json"
  log "[$name] automaton $prd_id approved and running; waiting for the task session"

  start_epoch=$(date +%s)
  deadline=$((start_epoch + DURATION + TASK_GRACE))
  : > "$res/state-trace.log"
  while :; do
    now=$(date +%s)
    if [[ -z "$sid" ]] && (( now - start_epoch >= START_TIMEOUT )); then
      log "[$name] no task session appeared within ${START_TIMEOUT}s"
      break
    fi
    if (( now >= deadline )); then
      timed_out=1
      log "[$name] gave up after DURATION + TASK_GRACE"
      break
    fi
    prd_json=$(api GET "/api/autonomous/prds/$prd_id")
    if [[ -z "$sid" ]]; then
      sid=$(jq -r --arg t "$TASK_TITLE" \
        '[.. | objects | select(.title? == $t and ((.session_id? // "") != "")) | .session_id][0] // empty' \
        <<<"$prd_json" 2>/dev/null)
      if [[ -n "$sid" ]]; then
        delivered=$now
        log "[$name] task session $sid started; spec delivered"
      fi
    fi
    tstatus=$(jq -r --arg t "$TASK_TITLE" '[.. | objects | select(.title? == $t) | .status? // empty][0] // empty' \
      <<<"$prd_json" 2>/dev/null)
    sstate=""
    sdir="$SB/data/sessions/$sid"
    if [[ -n "$sid" && -f "$sdir/session.json" ]]; then
      sstate=$(jq -r '.state // empty' "$sdir/session.json" 2>/dev/null)
    fi
    printf '%s epoch=%s task=%s session=%s marker=%s\n' "$(date -u +%H:%M:%SZ)" "$now" "${tstatus:--}" \
      "${sstate:--}" "$([[ -f $proj/$marker ]] && echo yes || echo no)" >> "$res/state-trace.log"
    if is_terminal "$sstate"; then
      end_epoch=$now
      log "[$name] session reached terminal state '$sstate'"
      break
    fi
    if [[ "$tstatus" =~ ^(completed|failed|cancelled)$ ]]; then
      end_epoch=$now
      log "[$name] task reached '$tstatus'"
      break
    fi
    sleep "$POLL_SECONDS"
  done

  sdir="$SB/data/sessions/$sid"
  if [[ -n "$sid" && -d "$sdir" ]]; then
    cp "$sdir/session.json" "$res/session.json" 2>/dev/null
    cp "$sdir/timeline.md" "$res/timeline.md" 2>/dev/null
    cp "$sdir/output.log" "$res/output.log" 2>/dev/null
    tail -n 60 "$sdir/output.log" > "$res/output-tail.log" 2>/dev/null
    api GET "/api/sessions/$sid/status" > "$res/hook-status.json"
  fi
  tail -c "+$((offset + 1))" "$SB/daemon.log" > "$res/daemon.log" 2>/dev/null
  [[ -n "$sid" ]] && grep -F -- "$sid" "$res/daemon.log" > "$res/journal-session.log"
  api GET "/api/autonomous/prds/$prd_id" > "$res/prd-final.json"
  if [[ -f "$proj/$marker" ]]; then
    cp "$proj/$marker" "$res/marker.txt"
    marker_epoch=$(stat -c %Y "$proj/$marker")
  fi

  python3 "$HERE/lib/summarize.py" "$res" "$name" "$DURATION" "$sid" "$delivered" "$end_epoch" \
    "$marker_epoch" "$timed_out" "$sstate" || log "[$name] summary helper failed"

  if (( KEEP == 0 )); then
    cleanup_scenario "$prd_id" "$sid"
  else
    log "[$name] --keep: automaton $prd_id and session ${sid:-none} left running"
  fi
}

on_exit() {
  local rc=$?
  trap - EXIT
  if (( KEEP )); then
    log "--keep: sandbox daemon left running at $BASE; inspect, then run: run.sh clean"
  else
    if daemon_alive && [[ -s "$PRD_IDS_FILE" ]]; then
      while read -r id; do
        api POST "/api/autonomous/prds/$id/cancel" '{}' >/dev/null
      done < "$PRD_IDS_FILE"
    fi
    stop_daemon
  fi
  exit "$rc"
}

clean_sandbox() {
  stop_daemon || die "left the sandbox in place: its daemon could not be stopped"
  if [[ -d "$SB/tmux" ]] && env -u TMUX TMUX_TMPDIR="$SB/tmux" tmux list-sessions >/dev/null 2>&1; then
    log "note: the sandbox tmux server still has sessions; it exits when its last session ends"
  fi
  case "$SB" in
    */.sec-sandbox/worker-long-command) rm -rf "$SB" && log "removed $SB" ;;
    *) die "refusing to remove unexpected path $SB" ;;
  esac
}

main() {
  local cmd="${1:-}" s
  local scenarios=()
  [[ "${2:-}" == "--keep" || "${3:-}" == "--keep" ]] && KEEP=1
  case "$cmd" in
    background|subagent) scenarios=("$cmd") ;;
    both) scenarios=(background subagent) ;;
    clean) clean_sandbox; exit $? ;;
    *) usage >&2; exit 2 ;;
  esac
  prepare
  trap on_exit EXIT
  trap 'exit 130' INT TERM
  start_daemon
  for s in "${scenarios[@]}"; do
    run_scenario "$s" || log "[$s] scenario did not complete; see $SB/results/$s"
  done
}

main "$@"
