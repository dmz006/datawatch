#!/usr/bin/env bash
# scripts/sec-assess/sandbox.sh — a throwaway, isolated daemon sandbox for
# live security/behavioral review (sec-assess harness, and human-driven
# probing).
#
# Every invocation gets its own data dir, a fresh token, its own ports
# (18080/18443 by default) and a fake external hostname, so nothing in here
# can touch the real daemon, the real ports, or the real network.
#
# Daemon cleanup — this script never leaves a daemon behind. Every exit path
# (normal, failure, interrupt) ends it:
#   - `start` and `run` record the daemon pid and its config path in the
#     folder's `pid` file, and install traps for EXIT, INT, TERM and HUP
#     (sb_install_traps) that end the daemon's sessions via the API and then
#     the daemon by its recorded pid — its cmdline is verified to still carry
#     this config path before any signal is sent, and processes are never
#     matched by name.
#   - The daemon is launched under `timeout <max lifetime>`
#     (SANDBOX_MAX_AGE, default 4h), so it ends on its own even if a
#     trap never runs. `start` reports the lifetime; `wipe` ends it earlier.
#   - `wipe` is the manual path: it ends the daemon's sessions via the API
#     using the token recorded in the sandbox config, verifies the recorded
#     pid's cmdline still carries that config path, ends the daemon, and only
#     then removes the folder.
#
# Usage:
#   sandbox.sh start [--base-url U] [--host H] [--port P] [--tls-port T]
#                    [--data-dir D] [--binary B]
#             run the sandbox daemon in the foreground. It ends by itself at
#             the max lifetime (SANDBOX_MAX_AGE); `wipe` or Ctrl-C ends it
#             earlier and keeps the folder for inspection.
#   sandbox.sh run [start flags] -- <command...>
#             run <command> against a fresh sandbox. The daemon is ended and
#             the sandbox folder removed on every exit path, and the command
#             exit status is returned.
#   sandbox.sh url
#             print the sandbox base URL / TLS URL (no daemon needed)
#   sandbox.sh wipe
#             end a still-running sandbox (sessions via the API, then the
#             daemon by its recorded pid) and remove the sandbox folder

set -u

# Isolated default: sandbox state lives under TMPDIR, never in the user's primary
# datawatch home — a cleanup that removes this folder must not be able to touch
# anything a real daemon owns.
SANDBOX_DIR="${SANDBOX_DIR:-${TMPDIR:-/tmp}/dw-sbx}"
LIB="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/test-daemon-lib.sh"
[[ -f "$LIB" ]] || { echo "sandbox.sh: missing shared daemon lifecycle helper: $LIB" >&2; exit 2; }
# shellcheck source=test-daemon-lib.sh
. "$LIB"

STATE="$SANDBOX_DIR"
CFG="$STATE/config.yaml"
LOGF="$STATE/daemon.log"
PIDF="$STATE/pid"
SB_CLEANED=0
SB_RUN_FOLDER=""   # set only by `run`: the folder always goes with the command
SB_DAEMON_PID=""

usage() {
  cat <<'EOF'
usage:
  sandbox.sh start [--base-url U] [--host H] [--port P] [--tls-port T]
                   [--data-dir D] [--binary B]
             run the sandbox daemon in the foreground. Ends by itself at the
             max lifetime (SANDBOX_MAX_AGE, default 4h); `wipe` or Ctrl-C
             ends it earlier (folder kept for inspection)
  sandbox.sh run [start flags] -- <command...>
             run <command> against a fresh sandbox; daemon ended and folder
             removed on every exit path; returns the command exit status
  sandbox.sh url
             print the sandbox base URL / TLS URL
  sandbox.sh wipe
             end a still-running sandbox and remove the sandbox folder
EOF
}

sb_log()   { echo "sandbox.sh: $*" >&2; }
sb_die()   { echo "sandbox.sh: $*" >&2; exit 1; }
sb_usage() { usage; exit "${1:-2}"; }

sb_die_port() { sb_die "refusing to start: a local daemon already answers on that port ($1). Stop it first, or use a different --port/--tls-port."; }

# sb_guard_ports — only refuse when *we* (this folder) are not the owner of
# an answer. A live daemon in another data dir means someone else's
# datawatch is on those local ports — never touch it, never start a
# second one on top.
sb_guard_ports() {
  local p1="${1:-}" p2="${2:-}" c1 c2
  c1="$(curl -so /dev/null -w '%{http_code}' --max-time 1 "http://127.0.0.1:$p1/" 2>/dev/null || true)"
  c2="$(curl -sko /dev/null -w '%{http_code}' --max-time 1 "https://127.0.0.1:$p2/" 2>/dev/null || true)"
  [[ -n "$c1" && "$c1" != "000" ]] && sb_die_port "$p1"
  [[ -n "$c2" && "$c2" != "000" ]] && sb_die_port "$p2"
}

sb_cmdline() { # pid — space-joined cmdline, empty when unreadable
  [[ -r "/proc/$1/cmdline" ]] && tr '\0' ' ' < "/proc/$1/cmdline" || printf ''
}

# sb_safe_rm — the only door to a folder removal. Refuses anything that is
# not an absolute path, the user's home tree (or anything above/in it), and any
# folder without the .sandbox marker this script itself writes at start.
sb_safe_rm() { # dir
  local dir="$1" h="$HOME" par
  [[ -n "$dir" && "$dir" == /* ]] || { sb_log "refusing to remove '$dir': an absolute path is required"; return 1; }
  par="${h%/*}"
  case "${dir%/}" in
    ""|"/"|"$h"|"$par") sb_log "refusing to remove '$dir': it is (above) the user's home tree"; return 1 ;;
  esac
  if [[ ! -f "$dir/.sandbox" ]]; then
    sb_log "refusing to remove '$dir': no sandbox marker (.sandbox) — not a folder this script created"
    return 1
  fi
  rm -rf -- "$dir" || { sb_log "could not remove $dir"; return 1; }
  return 0
}

sb_cfg_field() { # cfg key — first `key: value` under the daemon config
  local cfg="$1" key="$2"
  sed -nE "s/^[[:space:]]*${key}:[[:space:]]*\"?([A-Za-z0-9._:-]+)\"?.*/\1/p" "$cfg" 2>/dev/null | head -n1
}

# sb_install_traps — the safety net. A signal that the shell does not trap
# kills the shell *without* running its EXIT trap, so all four paths are
# trapped explicitly. Every path funnels into sb_on_exit, which ends the
# recorded daemon the only way this script may end it: sessions via the API
# first (td_set_api), then the recorded pid after td_stop_one re-verified the
# config-path marker in its cmdline. No name-based process matching, ever.
sb_install_traps() {
  trap sb_on_exit EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP
}

sb_on_exit() {
  [[ "$SB_CLEANED" == 1 ]] && return 0
  SB_CLEANED=1
  # td_stop_all covers every daemon recorded this session (start: one; run:
  # one). It is a no-op when the daemon already ended (e.g. `run` finished
  # via the timeout backstop or `wipe` ended it manually).
  td_stop_all 2>/dev/null || true
  if [[ -n "$SB_RUN_FOLDER" ]]; then
    # `run` mode: the folder is scratch space; it goes on success and on
    # failure alike — sb_safe_rm still enforces the marker/absolute/home
    # guards even on this script's own recorded path.
    if sb_safe_rm "$SB_RUN_FOLDER"; then
      sb_log "sandbox folder removed: $SB_RUN_FOLDER"
    else
      sb_log "sandbox folder kept (removal refused or failed): $SB_RUN_FOLDER"
    fi
  else
    sb_log "daemon ended (folder kept for inspection: $STATE; end it manually with 'wipe')"
  fi
  return 0
}

# sb_start_daemon — start the sandbox daemon once, record it, wait for
# health. Shared by `start` and `run`.
sb_start_daemon() {
  local base_url="${SB_BASE_URL:-http://127.0.0.1:18080}"
  local host="${SB_HOST:-example.invalid}"
  local port="${SB_PORT:-18080}"
  local tls_port="${SB_TLS_PORT:-18443}"
  local datadir="${SB_DATA_DIR:-}"
  local bin="${SB_BIN:-}" tbin="" pid="" token=""

  if [[ -f "$PIDF" ]] && kill -0 "$(sed -n 1p "$PIDF" 2>/dev/null)" 2>/dev/null; then
    sb_die "a sandbox is already running (pid $(sed -n 1p "$PIDF")). End it with 'wipe' first."
  fi
  bin="${bin:-$(command -v datawatch 2>/dev/null || true)}"
  [[ -n "$bin" ]] || sb_die "no datawatch binary on PATH; pass --binary <path>"
  [[ -z "$datadir" ]] && datadir="$STATE/data"

  local max_age="${SANDBOX_MAX_AGE:-4h}"
  tbin="$(command -v timeout 2>/dev/null || true)"
  if [[ -z "$tbin" ]]; then
    sb_log "WARNING: timeout(1) not found; the daemon has no max-lifetime backstop (would have been $max_age) and lives only until you run 'wipe'"
  fi

  mkdir -p -- "$STATE"
  # Marker: the only proof that a folder is safe to remove later.
  echo "sandbox folder — created by sandbox.sh; daemon bin=$bin; pidfile=$PIDF; $(date)" > "$STATE/.sandbox"
  token="sbx-$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-24)"
  cat > "$CFG" <<EOF
data_dir: $datadir
hostname: $host

server:
  enabled: true
  host: 127.0.0.1
  port: $port
  tls_enabled: false
  tls_port: $tls_port
  token: "$token"
  proxy_sandbox_port: 0
EOF
  sb_guard_ports "$port" "$tls_port"

  if [[ -n "$tbin" ]]; then
    # daemon-cleanup-ok: long-running daemon line. It ends by itself: `timeout
    # $max_age` is the backstop (default 4h). The pid of the wrapper and the
    # config path are recorded below and in $PIDF, the traps installed by
    # sb_install_traps fire on EXIT/INT/TERM/HUP, and td_stop_one ends this
    # exact recorded pid only after its cmdline still carries $CFG. `wipe`
    # is the manual equivalent.
    "$tbin" "$max_age" "$bin" start --foreground --config "$CFG" </dev/null > "$LOGF" 2>&1 &
  else
    # daemon-cleanup-ok: long-running daemon line. No timeout backstop on
    # this host, so the recorded pid + config path ($PIDF) and the
    # EXIT/INT/TERM/HUP traps are the only ways out, plus 'wipe'.
    "$bin" start --foreground --config "$CFG" </dev/null > "$LOGF" 2>&1 &
  fi
  pid=$!
  SB_DAEMON_PID="$pid"

  # cmdline marker: this sandbox's own config path — unique to this folder.
  td_track_pid "$pid" "$CFG"
  # Record the daemon for the traps AND as the single documented line in the
  # pid file (pid on line 1, config path on line 2) that `wipe` consumes.
  printf '%s\n%s\n' "$pid" "$CFG" > "$PIDF"
  # API (sessions) credentials: base URL + the token from the config we just
  # wrote, so any exit path ends the sandbox's sessions before the daemon.
  td_set_api "$pid" "http://127.0.0.1:$port" "$token"

  local i code ok=0
  for i in $(seq 1 90); do
    code="$(curl -so /dev/null -w '%{http_code}' --max-time 1 "http://127.0.0.1:$port/" 2>/dev/null || true)"
    if [[ -n "$code" && "$code" != "000" ]]; then ok=1; break; fi
    code="$(curl -sko /dev/null -w '%{http_code}' --max-time 1 "https://[$host]:$tls_port/" 2>/dev/null || true)"
    if [[ -n "$code" && "$code" != "000" ]]; then ok=1; break; fi
    sleep 1
  done
  [[ "$ok" == 1 ]] || { echo "--- daemon log (tail) ---" >&2; tail -n 40 "$LOGF" >&2 || true; sb_die "daemon did not come up (see $LOGF)"; }

  local base
  base="$(curl -s --retry 10 --retry-connrefused --retry-delay 1 --max-time 3 -o /dev/null -w '%{url_effective}' "http://127.0.0.1:$port/" 2>/dev/null || echo "http://127.0.0.1:$port")"
  echo "sbx base         $base"
  echo "sbx tls          https://[$host]:$tls_port"
  echo "sbx folder       $STATE"
  echo "sbx daemon       pid $pid — ends on its own at the max lifetime ($max_age); end it earlier with 'sandbox.sh wipe' or Ctrl-C"
}

sb_parse_start_flags() { # consumes its own args via global shift-through
  while (($#)); do
    case "$1" in
      --base-url) SB_BASE_URL="${2:?}"; shift 2 ;;
      --host)     SB_HOST="${2:?}";     shift 2 ;;
      --port)     SB_PORT="${2:?}";     shift 2 ;;
      --tls-port) SB_TLS_PORT="${2:?}"; shift 2 ;;
      --data-dir) SB_DATA_DIR="${2:?}"; shift 2 ;;
      --binary)   SB_BIN="${2:?}";      shift 2 ;;
      -h|--help)  sb_usage 0 ;;
      *) sb_die "unknown flag: $1 (try: sandbox.sh --help)" ;;
    esac
  done
}

sb_wipe() {
  if [[ -f "$PIDF" ]]; then
    local pid cfg cmd token api_base
    pid="$(sed -n 1p "$PIDF" 2>/dev/null || true)"
    cfg="$(sed -n 2p "$PIDF" 2>/dev/null || true)"
    [[ -n "$cfg" && -f "$cfg" ]] || cfg="$CFG"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      # The pid file is a trust record, not a proof. Before any signal, the
      # recorded pid must still be the sandbox daemon: its cmdline still has
      # to carry this sandbox's config path. Otherwise refuse — a replaced
      # pid could be anything.
      cmd="$(sb_cmdline "$pid")"
      if [[ "$cmd" != *"$cfg"* ]]; then
        sb_die "recorded pid $pid no longer carries this config ($cfg) in its cmdline ('$cmd'); refusing to signal it. Re-run the sandbox and wipe again." >&2
      fi
      # Sessions first, via the API, with the token from the recorded config.
      token="$(sb_cfg_field "$cfg" token)"
      local port
      port="$(sb_cfg_field "$cfg" port)"
      if [[ -n "$token" && -n "$port" ]]; then
        td_track_pid "$pid" "$cfg"
        td_set_api "$pid" "http://127.0.0.1:$port" "$token"
      else
        sb_log "warning: no token/port recorded in $cfg; ending the daemon without the API session pass"
        td_track_pid "$pid" "$cfg"
      fi
      td_stop_all 2>/dev/null || true
      if kill -0 "$pid" 2>/dev/null; then
        sb_die "daemon pid $pid is still alive after the end attempt; refusing to remove $STATE. Check it and wipe again."
      fi
      sb_log "daemon pid $pid ended (sessions via the API first)"
    else
      sb_log "no live daemon recorded in $PIDF; removing the folder"
    fi
  else
    sb_log "no sandbox recorded in $PIDF; removing the folder"
  fi
  if [[ -d "$STATE" ]]; then
    sb_safe_rm "$STATE" || sb_die "folder kept (removal refused or failed): $STATE"
    sb_log "removed $STATE"
  else
    sb_log "folder already gone: $STATE"
  fi
}

main() {
  local sub="${1:-}"
  [[ -n "$sub" ]] || { sb_usage 2; }
  shift
  case "$sub" in
    start)
      sb_parse_start_flags "$@"
      # Traps BEFORE the daemon exists: nothing can outlive this shell.
      sb_install_traps
      sb_start_daemon
      # Foreground: the lifetime ends with the daemon — the timeout backstop
      # firing, or an early end (Ctrl-C / failure / wipe elsewhere).
      local rc=0
      wait "$SB_DAEMON_PID" || rc=$?
      if [[ "$rc" == "124" ]]; then
        sb_log "daemon reached its max lifetime (${SANDBOX_MAX_AGE:-4h}) and ended on its own"
        return 0
      fi
      if [[ "$rc" == 0 ]]; then sb_log "daemon ended (rc 0)"; fi
      return 0
      ;;
    run)
      # flags ... -- command...
      local -a cmd=()
      while (($#)); do
        if [[ "$1" == "--" ]]; then shift; cmd=("$@"); break; fi
        case "$1" in
          --base-url|--host|--port|--tls-port|--data-dir|--binary)
            local k="$1" v="${2:?missing value for $1}"
            case "$k" in
              --base-url) SB_BASE_URL="$v" ;;
              --host)     SB_HOST="$v" ;;
              --port)     SB_PORT="$v" ;;
              --tls-port) SB_TLS_PORT="$v" ;;
              --data-dir) SB_DATA_DIR="$v" ;;
              --binary)   SB_BIN="$v" ;;
            esac
            shift 2 ;;
          -h|--help) sb_usage 0 ;;
          *) sb_die "unknown flag for run: $1 (try: sandbox.sh --help)" ;;
        esac
      done
      ((${#cmd[@]})) || sb_die "run: no command after '--' (usage: sandbox.sh run [flags] -- <command...>)"
      SB_RUN_FOLDER="$STATE"
      sb_install_traps
      sb_start_daemon
      # The command runs with the sandbox live; its exit status is returned
      # to the caller, and sb_on_exit (all traps) ends the daemon + folder.
      local rc=0
      "${cmd[@]}" || rc=$?
      exit "$rc"
      ;;
    url)
      local base="${SB_BASE_URL:-${BASE_URL:-http://127.0.0.1:18080}}"
      local port="${SB_PORT:-18080}" tls="${SB_TLS_PORT:-18443}"
      case "$base" in
        *"$port"*) : ;;
        *) base="http://127.0.0.1:$port" ;;
      esac
      echo "$base"
      echo "https://[${SB_HOST:-example.invalid}]:$tls"
      return 0
      ;;
    wipe)
      sb_wipe
      ;;
    -h|--help|help) sb_usage 0 ;;
    *) sb_usage 2 ;;
  esac
}

main "$@"
