#!/usr/bin/env bash
# release-smoke-daemon-cleanup.sh — proves the lifecycle helper
# (scripts/test-daemon-lib.sh) ends a REAL datawatch daemon on interrupt.
#
# Background: three daemons were once found still running long after their
# jobs finished — a sandbox daemon from a one-shot live-test config in a
# temp folder (holding a live session for days), and an end-to-end test
# daemon from a mktemp folder (orphaned, holding five listening ports).
# Every job that starts a datawatch daemon must end that daemon, and every
# session the daemon started, on every exit path: normal, failure,
# interrupt.
#
# What this smoke does, twice (once per signal, SIGINT then SIGTERM):
#   1. Build a scratch dir under the repository's own (gitignored)
#      .datawatch/ area, generate a config from testdata/datawatch.yaml
#      with the same sed substitutions as start_test_daemon in
#      scripts/run-tests.sh (free ports probed like free_port in
#      scripts/e2e-pwa.sh; TLS switched off so /api/health is plain HTTP).
#   2. Run a child driver script that sources scripts/test-daemon-lib.sh,
#      installs the shared traps, starts the daemon with td_start, waits
#      for /api/health, writes its own and the daemon's PIDs, then waits.
#   3. Send the driver SIGINT (round 1) / SIGTERM (round 2) — only the
#      PID this script recorded, and only while /proc/<pid>/cmdline still
#      carries the recorded marker (the driver script path).
#   4. Assert: the driver exited, the daemon PID is gone, the HTTP port is
#      no longer listening (td_wait_port_closed), and
#      scripts/check-no-leftover-test-daemons.sh reports nothing new
#      against the snapshot taken before the round started.
#
# Skips cleanly (exit 0 with a SKIP line) when no datawatch binary can be
# resolved (TEST_BINARY, bin/datawatch, PATH) and no `go` build is
# possible, or when prerequisites (curl, python3) are missing.
#
# Usage:
#   env -u TMUX -u TMUX_TMPDIR bash scripts/release-smoke-daemon-cleanup.sh
#
# Exits 0 on full pass (or clean skip), 1 on any FAIL. This script itself
# records the driver pid (marker: the driver script path) and the daemon
# pid (marker: its config path) in its own state dir with td_track_pid,
# so its OWN EXIT/INT/TERM/HUP traps (td_install_traps, the same library
# under test) end exactly those two before TD_EXTRA_CLEANUP removes the
# scratch dirs — even when only the smoke itself receives the signal.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"

PASS=0
FAIL=0
SKIP=0
ok()   { echo "  PASS  $*"; PASS=$((PASS+1)); }
ko()   { echo "  FAIL  $*"; FAIL=$((FAIL+1)); }
skip() { echo "  SKIP  $*"; SKIP=$((SKIP+1)); }
H()    { echo ""; echo "== $* =="; }

# --- Resolve the daemon binary, same order as scripts/run-tests.sh -------
BINARY=""
if [[ -n "${TEST_BINARY:-}" && -x "${TEST_BINARY:-}" ]]; then
  BINARY="$TEST_BINARY"
elif [[ -x "$ROOT/bin/datawatch" ]]; then
  BINARY="$ROOT/bin/datawatch"
else
  BINARY="$(command -v datawatch 2>/dev/null || true)"
fi
if [[ -z "${BINARY:-}" ]] && command -v go >/dev/null 2>&1; then
  ( cd "$ROOT" && go build -o bin/datawatch ./cmd/datawatch/ ) >/dev/null 2>&1 || true
  if [[ -x "$ROOT/bin/datawatch" ]]; then
    BINARY="$ROOT/bin/datawatch"
  fi
fi
if [[ -z "${BINARY:-}" ]]; then
  H "Daemon cleanup smoke (lifecycle helper vs a real daemon)"
  skip "no datawatch binary (TEST_BINARY, bin/datawatch, PATH all missing) and no go build possible"
  echo ""
  echo "Summary: $PASS pass / $FAIL fail / $SKIP skip"
  exit 0
fi
H "Daemon cleanup smoke (lifecycle helper vs a real daemon)"
echo "  using binary: $BINARY"
if ! command -v curl >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
  skip "curl or python3 not on PATH (health probe and free-port probing need them)"
  echo ""
  echo "Summary: $PASS pass / $FAIL fail / $SKIP skip"
  exit 0
fi

# --- Scratch area inside the repository (gitignored), created if missing -
SCRATCH_ROOT="$ROOT/.datawatch"
mkdir -p "$SCRATCH_ROOT"
# Keep the library's shared record dir inside the repository too, so every
# write of this smoke (both its own and the driver's records) stays local.
export TD_STATE_DIR="$SCRATCH_ROOT/release-smoke-daemon-cleanup-state"

# Same API-token substitution as start_test_daemon in scripts/run-tests.sh
# (its default token, unless the caller already set one). The driver records
# it with td_set_api; the smoke records it on its own daemon record too,
# so the smoke's own td_stop_all ends the daemon's sessions first.
TEST_TOKEN="${TEST_TOKEN:-dw-test-token-12345}"
export TEST_TOKEN

source "$LIB"

# --- Its own cleanup: remove the scratch dirs it made, on EVERY exit
# --- path (normal, failure, INT, TERM, HUP). The shared trap handler
# --- ends any recorded daemon first; this hook runs afterwards.
SCRATCHES=()
smoke_extra_cleanup() {
  local d
  for d in "${SCRATCHES[@]:-}"; do
    if [[ -n "$d" && -d "$d" ]]; then
      rm -rf "$d" 2>/dev/null || true
    fi
  done
}
TD_EXTRA_CLEANUP=smoke_extra_cleanup
td_install_traps "$@"

# free_port — same probing as free_port in scripts/e2e-pwa.sh (the fallback
# only fires when python3 is missing, which the script already skips on;
# it is numeric, not hex like the uuid fallback used for)
free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null \
    || printf '%s\n' "$((20000 + RANDOM % 20000))"
}

# send_signal_recorded <pid> <sig> <marker> — signal only a PID this
# script itself recorded, and only while its live /proc/<pid>/cmdline
# still carries the marker recorded with it. Returns 0 = sent, 1 = marker
# no longer present (nothing signalled), 2 = process already gone.
send_signal_recorded() {
  local pid="${1:-}" sig="${2:-}" marker="${3:-}" cmd
  [[ -n "$pid" && "$pid" =~ ^[0-9]+$ && -n "$marker" ]] || return 1
  cmd="$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
  if [[ -z "$cmd" ]]; then
    return 2
  fi
  case "$cmd" in
    *"$marker"*) kill "-$sig" "$pid" 2>/dev/null && return 0 ;;
    *) return 1 ;;
  esac
  return 1
}

# write_driver <driver.sh> — the child driver: install the lifecycle
# traps, start the daemon through the helper, wait for /api/health,
# record both PIDs, then wait for the signal to arrive.
write_driver() {
  cat > "$1" <<'DRIVER'
#!/usr/bin/env bash
# Child driver for release-smoke-daemon-cleanup.sh (runtime-generated).
set -uo pipefail
: "${B:?}" "${C:?}" "${S:?}" "${P:?}" "${T:?}" "${LIB:?}"
export TD_STATE_DIR="$S/td-state"
source "$LIB"
td_install_traps
if ! td_start "$B" "$C" "$S/daemon.log"; then
  echo "driver: daemon start failed; last daemon log lines:" >&2
  tail -n 10 "$S/daemon.log" 2>/dev/null >&2
  exit 1
fi
DP="$TD_LAST_PID"
# Record the API (with the config's token) so stopping ends this daemon's
# live sessions through its own API first.
td_set_api "$DP" "http://127.0.0.1:${P}" "${T}"
up=0
for i in $(seq 1 120); do
  if curl -s --max-time 2 "http://127.0.0.1:${P}/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
    up=1
    break
  fi
  sleep 0.5
done
if [[ "$up" != "1" ]]; then
  echo "driver: daemon never reached /api/health" >&2
  tail -n 10 "$S/daemon.log" 2>/dev/null >&2
  exit 1
fi
printf '%s\n%s\n' "$$" "$DP" > "$S/pids"
echo "driver: ready (driver=$$ daemon=$DP)"
# Now wait: the shared EXIT/INT/TERM/HUP traps installed above are what
# end this daemon on whichever path this driver exits through.
wait
DRIVER
}

# run_round <signal-name> <expected-driver-exit-status>
run_round() {
  local sig="$1" expect="$2"
  local scr cfg drv snap
  local p1 p2 p3 p4 p5 p6
  local dpid ddaemon i rc rc2

  scr="$(mktemp -d "$SCRATCH_ROOT/release-smoke-daemon-cleanup.XXXXXX")" || { ko "$sig: could not make a scratch dir under $SCRATCH_ROOT"; return; }
  SCRATCHES+=("$scr")
  cfg="$scr/config.yaml"
  drv="$scr/driver.sh"
  snap="$scr/leftover-baseline.txt"

  if [[ ! -f "$ROOT/testdata/datawatch.yaml" ]]; then
    ko "$sig: testdata/datawatch.yaml not found"
    return
  fi

  # Same sed substitutions as start_test_daemon in scripts/run-tests.sh
  # (including its TEST_TOKEN substitution), with free probed ports; TLS
  # additionally switched off below so the /api/health probe and the port
  # assertion run on plain HTTP.
  p1=$(free_port); p2=$(free_port); p3=$(free_port)
  p4=$(free_port); p5=$(free_port); p6=$(free_port)
  sed \
    -e "s|data_dir: /data|data_dir: $scr/data|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $scr/data/projects|g" \
    -e "s|port: 8080|port: $p1|g" \
    -e "s|tls_port: 8443|tls_port: $p2|g" \
    -e "s|tls_enabled: true|tls_enabled: false|g" \
    -e "s|sse_port: 9090|sse_port: $p3|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"${TEST_TOKEN}\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$p4\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$p5|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$p6|g" \
    "$ROOT/testdata/datawatch.yaml" > "$cfg" || { ko "$sig: config generation failed"; return; }

  write_driver "$drv" || { ko "$sig: could not write the child driver"; return; }

  # Snapshot the live test daemons BEFORE this round starts, so the check
  # afterwards reports only what this round left behind.
  if ! bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$snap" >/dev/null 2>&1; then
    ko "$sig: leftover-daemon snapshot failed"
    return
  fi

  # Run the driver in the background; its own traps handle the signal.
  # Record the driver in THIS script's own state dir right after it
  # starts: its marker is the driver script path, which its live
  # /proc/<pid>/cmdline carries for its whole life (bash <path>/driver.sh).
  # td_install_traps' td_stop_all then ends it — and, through the daemon
  # record below, whatever it left — no matter which exit path of this
  # script runs, before TD_EXTRA_CLEANUP touches the scratch dir.
  B="$BINARY" C="$cfg" S="$scr" P="$p1" T="$TEST_TOKEN" LIB="$LIB" bash "$drv" >"$scr/driver.out" 2>&1 &
  dpid=$!
  td_track_pid "$dpid" "driver.sh" || { ko "$sig: could not record the driver pid for the shared traps"; wait "$dpid" 2>/dev/null || true; return; }

  # Wait for the driver to report ready (it only does so once the daemon
  # answered /api/health), bounded by its own startup deadline plus slack.
  local ready=""
  for i in $(seq 1 150); do
    if [[ -s "$scr/pids" ]]; then
      ready=1
      break
    fi
    if ! kill -0 "$dpid" 2>/dev/null; then
      break # driver quit before it was ready
    fi
    sleep 0.5
  done
  if [[ -z "${ready:-}" ]]; then
    ko "$sig: driver did not reach ready (see below)"
    echo "    $(tail -n 6 "$scr/driver.out" 2>/dev/null | sed 's/^/    /' | tr -s ' ')"
    wait "$dpid" 2>/dev/null || true
    return
  fi
  ddaemon="$(sed -n 2p "$scr/pids")"

  # Record the daemon in this script's own state dir too (marker: its
  # config path, which its cmdline carries as long as it runs, exactly as
  # td_start records it), with its API token so the smoke's own
  # td_stop_all ends the daemon's sessions first and then the daemon —
  # before smoke_extra_cleanup removes the scratch dir.
  td_track_pid "$ddaemon" "$cfg" || ko "$sig: could not record the daemon pid for the shared traps"
  td_set_api "$ddaemon" "http://127.0.0.1:$p1" "$TEST_TOKEN" || true

  if curl -s --max-time 3 "http://127.0.0.1:$p1/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
    ok "$sig: daemon healthy before the signal (driver=$dpid daemon=$ddaemon port=$p1)"
  else
    ko "$sig: daemon was not healthy before the signal"
  fi

  # Interrupt. Only the recorded driver PID, marker-checked (its cmdline
  # carries the driver script path for its whole life).
  local sent
  sent="no"
  send_signal_recorded "$dpid" "$sig" "driver.sh" && sent="yes" || rc=$?
  if [[ "$sent" != "yes" ]]; then
    ko "$sig: could not signal the recorded driver pid $dpid"
    return
  fi

  # Give the driver's own trap (and through it, the helper) a bounded
  # window to end the daemon and exit by itself.
  local gone=0
  for i in $(seq 1 50); do
    if ! kill -0 "$dpid" 2>/dev/null; then
      gone=1
      break
    fi
    sleep 0.5
  done
  if [[ "$gone" != "1" ]]; then
    ko "$sig: driver still alive 25s after $sig; escalating (same recorded pid, marker-checked)"
    send_signal_recorded "$dpid" "TERM" "driver.sh" || true
    for i in $(seq 1 20); do
      kill -0 "$dpid" 2>/dev/null || break
      sleep 0.5
    done
  fi
  wait "$dpid" 2>/dev/null; rc=$?
  if [[ "$gone" == "1" && "$rc" == "$expect" ]]; then
    ok "$sig: driver ended by its own trap with status $rc"
  else
    ko "$sig: driver end status $rc (expected $expect, exited-cleanly-with-signal=$gone)"
  fi

  # The daemon must be gone — the driver's traps (the library under test)
  # are what ended it.
  local dgone=0
  for i in $(seq 1 60); do
    if ! kill -0 "$ddaemon" 2>/dev/null; then
      dgone=1
      break
    fi
    sleep 0.5
  done
  if [[ "$dgone" == "1" ]]; then
    ok "$sig: daemon $ddaemon is gone"
  else
    ko "$sig: daemon $ddaemon survived the driver's cleanup; ending it now (its own recorded pid, config-path marker-checked)"
    # daemon-cleanup-ok: last-resort, signals the driver's own daemon pid only after the config marker is still in its live cmdline
    send_signal_recorded "$ddaemon" "KILL" "config.yaml" || true
    sleep 1
  fi

  if td_wait_port_closed "$p1" 20; then
    ok "$sig: HTTP port $p1 is no longer listening"
  else
    ko "$sig: HTTP port $p1 still accepting connections"
  fi

  local lout lrc
  lout="$(bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$snap" 2>&1)"
  lrc=$?
  if [[ "$lrc" -eq 0 ]]; then
    ok "$sig: leftover-daemon checker reports nothing new against the pre-run snapshot"
  else
    ko "$sig: leftover-daemon checker reported leftovers:"
    echo "    $(echo "$lout" | tail -n 4 | sed 's/^/    /')"
  fi
}

H "Round 1 — SIGINT"
run_round "INT" 130

H "Round 2 — SIGTERM"
run_round "TERM" 143

echo ""
echo "Summary: $PASS pass / $FAIL fail / $SKIP skip"
[[ "$FAIL" == "0" ]]
