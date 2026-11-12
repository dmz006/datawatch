#!/usr/bin/env bash
# Sandbox live check for the supervisor watch: the corrected set-up.
#
# Builds this tree, starts a throwaway daemon the way scripts/run-tests.sh and
# the release-smoke scripts do, and shows the watch raising a finding live.
# The first live check of the watch (docs/plans/reports/
# supervisor-watch-shadow-live-check.md) started its daemon with two set-up
# faults; this script is the corrected start the next run uses:
#
#   1. The daemon is started and recorded through scripts/test-daemon-lib.sh,
#      and td_install_traps runs before any other side effect, so the daemon
#      (and its live sessions) is ended on every exit path: normal, failure,
#      INT, TERM, HUP. The first run recorded a daemon with no traps
#      installed.
#   2. The bootstrap config sets server.proxy_sandbox_port to 0 (listener
#      disabled). Left at its default the sandbox daemon tries to bind 8444,
#      the production proxy's port. The check fails if the daemon log shows a
#      bind attempt on that port.
#
# What it checks, in order:
#   - the daemon is healthy, its boot log shows the proxy listener disabled
#     and no bind attempt, and the traps are installed in this shell
#   - GET /api/autonomous/supervisor-watch answers 200 in the default mode
#     (shadow) with the stored findings list
#   - a small git repository with a modified tracked file, set as
#     supervisor_watch.main_checkout, raises a main_checkout_dirty finding
#     within three coordinator polls; the watch stays in shadow, so the finding
#     never alerts
#   - the config keys the check set read back to their prior values after the
#     restore
#
# Everything lives under one scratch directory (the daemon's HOME, data
# directory, config, log, the fixture repository and the daemon-library state
# directory), removed on exit unless DATAWATCH_SVW_KEEP_WORK=1. TMUX and
# TMUX_TMPDIR are unset, and the daemon gets the scratch HOME, so it cannot
# adopt the production daemon's dedicated tmux socket. Nothing is sent to any
# daemon but the one this script starts.
#
# Environment:
#   DATAWATCH_SVW_WORK         scratch directory to use (default: a fresh mktemp -d)
#   DATAWATCH_SVW_KEEP_WORK=1  keep the scratch directory for inspection
#   DATAWATCH_SVW_WAIT         seconds to wait for the finding (default 240)
#
# Exit code: 0 = pass, 1 = failure.

if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOKEN="dw-test-token-12345"
WORK="${DATAWATCH_SVW_WORK:-$(mktemp -d)}"
mkdir -p "$WORK"
export TD_STATE_DIR="$WORK/tdstate"

# Shared daemon lifecycle: recorded at start, ended on every exit path, and
# only then is the scratch directory removed (TD_EXTRA_CLEANUP runs after
# td_stop_all). Installed before anything else happens.
source "$REPO_ROOT/scripts/test-daemon-lib.sh"
TD_EXTRA_CLEANUP=cleanup
td_install_traps "$@"

BASE=""
SANDBOX_PID=""
HTTP_CODE=""
API_BODY=""

die() {
  echo "FAIL: $*"
  exit 1
}
step() { echo "--- $*"; }
pass() { echo "PASS: $*"; }

# api METHOD PATH [BODY] — sets HTTP_CODE and API_BODY.
api() {
  local method="$1" path="$2" body="${3:-}" out
  local args=(-sk --max-time 30 -X "$method" -H "Authorization: Bearer $TOKEN" -w $'\n%{http_code}')
  if [[ -n "$body" ]]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi
  out=$(curl "${args[@]}" "$BASE$path") || out=$'\n000'
  HTTP_CODE="${out##*$'\n'}"
  API_BODY="${out%$'\n'*}"
}

# free_ports N — N distinct free loopback TCP ports, one per line.
free_ports() {
  python3 -I -c '
import socket, sys
socks = []
for _ in range(int(sys.argv[1])):
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    socks.append(s)
for s in socks:
    print(s.getsockname()[1])
for s in socks:
    s.close()
' "$1"
}

# cfg_value KEY — the effective value of a supervisor_watch key on GET /api/config.
cfg_value() {
  api GET /api/config
  [[ "$HTTP_CODE" == 200 ]] || die "GET /api/config returned $HTTP_CODE"
  printf '%s' "$API_BODY" | jq -r ".autonomous.automation.supervisor_watch.$1 // \"\""
}

cleanup() {
  # Runs from the library's TD_EXTRA_CLEANUP hook, after the daemon is ended:
  # it must not `exit` (the shared handler owns the status).
  if [[ "${DATAWATCH_SVW_KEEP_WORK:-0}" == 1 ]]; then
    echo "kept scratch directory: $WORK"
  else
    rm -rf "$WORK"
  fi
}

start_sandbox() {
  local sbx="$WORK/sandbox" bin="$WORK/datawatch" tpl="$REPO_ROOT/testdata/datawatch.yaml"
  [[ -f "$tpl" ]] || die "sandbox config template missing: testdata/datawatch.yaml"
  step "build the daemon from this tree"
  (cd "$REPO_ROOT" && go build -o "$bin" ./cmd/datawatch/) > "$WORK/build.log" 2>&1 \
    || { cat "$WORK/build.log"; die "go build of this tree failed"; }
  pass "built $bin"

  local ports p_http p_tls p_mcp p_dns p_hook p_ntfy
  ports=$(free_ports 6) || die "could not pick free ports"
  { read -r p_http; read -r p_tls; read -r p_mcp; read -r p_dns; read -r p_hook; read -r p_ntfy; } <<<"$ports"
  mkdir -p "$sbx/data/projects" "$sbx/home"
  # proxy_sandbox_port: 0 disables the remote-PWA proxy listener; unset, it
  # defaults to 8444, the production proxy's port.
  sed \
    -e "s|data_dir: /data|data_dir: $sbx/data|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $sbx/data/projects|g" \
    -e "s|port: 8080|port: $p_http|g" \
    -e "s|^  tls_port: 8443|  tls_port: $p_tls\n  proxy_sandbox_port: 0|" \
    -e "s|sse_port: 9090|sse_port: $p_mcp|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$p_dns\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$p_hook|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$p_ntfy|g" \
    "$tpl" > "$sbx/config.yaml"
  cp "$sbx/config.yaml" "$sbx/data/config.yaml"
  grep -q '^  proxy_sandbox_port: 0$' "$sbx/config.yaml" \
    || die "the bootstrap config does not disable the proxy sandbox listener"
  # The daemon must read this file: a config that does not parse would leave it
  # on its defaults (production ports), so refuse to start unless it parses.
  python3 -I -c '
import sys
try:
    import yaml
except ImportError:
    sys.exit(0)
yaml.safe_load(open(sys.argv[1]))
' "$sbx/config.yaml" 2>/dev/null || die "the bootstrap config is not valid YAML"

  HOME="$sbx/home" env -u TMUX -u TMUX_TMPDIR "$bin" start --foreground --config "$sbx/config.yaml" \
    > "$sbx/daemon.log" 2>&1 &
  SANDBOX_PID=$!
  td_track_pid "$SANDBOX_PID" "$sbx/config.yaml" \
    || die "could not record sandbox daemon pid $SANDBOX_PID for cleanup"
  BASE="https://127.0.0.1:$p_tls"
  td_set_api "$SANDBOX_PID" "$BASE" "$TOKEN" 2>/dev/null \
    || echo "WARN: could not record the sandbox daemon's API base" >&2
  local i
  for i in $(seq 1 120); do
    curl -skf --max-time 2 "$BASE/api/health" >/dev/null 2>&1 && break
    kill -0 "$SANDBOX_PID" 2>/dev/null || { tail -30 "$sbx/daemon.log"; die "sandbox daemon exited during start"; }
    sleep 0.5
  done
  curl -skf --max-time 2 "$BASE/api/health" >/dev/null 2>&1 \
    || { tail -30 "$sbx/daemon.log"; die "sandbox daemon did not become healthy within 60s"; }
  pass "sandbox daemon pid $SANDBOX_PID healthy at $BASE"
}

check_setup() {
  local log="$WORK/sandbox/daemon.log"
  step "set-up checks"
  trap -p EXIT | grep -q . || die "no EXIT trap is installed in the starting shell"
  td_is_recorded "$SANDBOX_PID" || die "the daemon is not recorded with the daemon library"
  pass "traps installed and the daemon recorded for cleanup"
  if grep -q -E 'could not bind .*:8444|:8444' "$log"; then
    grep -E ':8444' "$log" | head -3
    die "the sandbox daemon touched the production proxy port 8444"
  fi
  grep -q 'proxy sandbox listener disabled' "$log" \
    || die "the boot log does not show the proxy sandbox listener disabled"
  pass "proxy sandbox listener disabled; no bind attempt on 8444"
}

make_fixture() {
  local repo="$WORK/maincheckout"
  mkdir -p "$repo"
  git -C "$repo" init -q -b main
  git -C "$repo" config user.email svw@example.invalid
  git -C "$repo" config user.name "svw check"
  printf 'one\n' > "$repo/tracked.txt"
  git -C "$repo" add tracked.txt
  git -C "$repo" commit -q -m "fixture"
  printf 'two\n' >> "$repo/tracked.txt"
}

run_check() {
  local repo="$WORK/maincheckout" wait_s="${DATAWATCH_SVW_WAIT:-240}" prior_mode prior_main i
  step "the watch endpoint in the default mode"
  api GET /api/autonomous/supervisor-watch
  [[ "$HTTP_CODE" == 200 ]] || die "GET /api/autonomous/supervisor-watch returned $HTTP_CODE: $API_BODY"
  [[ "$(printf '%s' "$API_BODY" | jq -r .mode)" == shadow ]] || die "default mode is not shadow: $API_BODY"
  pass "GET /api/autonomous/supervisor-watch is 200 in shadow mode"

  prior_mode="$(cfg_value mode)"
  prior_main="$(cfg_value main_checkout)"
  step "set main_checkout to a repository with a modified tracked file"
  api PUT /api/config "$(jq -n --arg p "$repo" '{"autonomous.automation.supervisor_watch.main_checkout": $p}')"
  [[ "$HTTP_CODE" == 200 ]] || die "PUT /api/config returned $HTTP_CODE: $API_BODY"

  step "wait up to ${wait_s}s for a main_checkout_dirty finding"
  for ((i = 0; i < wait_s; i += 5)); do
    api GET /api/autonomous/supervisor-watch
    if [[ "$HTTP_CODE" == 200 ]] && printf '%s' "$API_BODY" \
      | jq -e '[.findings[] | select(.kind == "main_checkout_dirty" and (.resolved | not))] | length > 0' >/dev/null; then
      break
    fi
    sleep 5
  done
  printf '%s' "$API_BODY" | jq -e '[.findings[] | select(.kind == "main_checkout_dirty" and (.resolved | not))] | length > 0' >/dev/null \
    || die "no open main_checkout_dirty finding within ${wait_s}s: $API_BODY"
  pass "the watch raised main_checkout_dirty live"
  printf '%s' "$API_BODY" | jq -e '[.findings[] | select(.alerted_at != null)] | length == 0' >/dev/null \
    || die "shadow mode alerted: $API_BODY"
  pass "shadow mode raised no alert"

  step "restore the config the check changed"
  api PUT /api/config "$(jq -n --arg p "$prior_main" '{"autonomous.automation.supervisor_watch.main_checkout": $p}')"
  [[ "$HTTP_CODE" == 200 ]] || die "restore PUT /api/config returned $HTTP_CODE: $API_BODY"
  [[ "$(cfg_value main_checkout)" == "$prior_main" ]] || die "main_checkout did not read back to its prior value"
  [[ "$(cfg_value mode)" == "$prior_mode" ]] || die "mode changed during the check"
  pass "supervisor_watch config read back to its prior values"
}

make_fixture
start_sandbox
check_setup
run_check
echo "ALL PASS"
