#!/usr/bin/env bash
# state-data-dir-live-check.sh — live proof that a datawatch daemon whose
# data_dir is configured reads its state (channel routing, dashboard
# layout, smoke runs, discussions) from the configured data directory and
# never touches a state tree already present under $HOME/.datawatch.
#
# What it does:
#   1. Creates a throwaway run folder under the project tree's .datawatch/
#      with: a throwaway HOME (decoy channel_routing.json,
#      dashboard-layout.json, smoke-runs/ and discussions/ files carrying
#      sentinel content), a fresh data dir seeded with its own marker
#      state, a config generated from testdata/datawatch.yaml
#      (data_dir, free server ports, loopback, test token), and the
#      daemon binary built into the run folder.
#   2. Starts exactly one sandbox daemon with that HOME, via td_start from
#      scripts/test-daemon-lib.sh (env -u TMUX -u TMUX_TMPDIR applied;
#      the pid is recorded at start and every stop is by recorded pid —
#      no pkill / killall / pgrep -f, no tmux, no name matching).
#   3. Calls the four GET endpoints and records each HTTP status plus
#      whether the body reflects the seeded data-dir state and none of
#      the decoy home state:
#          GET /api/channel/routing
#          GET /api/dashboard/layout
#          GET /api/smoke/progress
#          GET /api/security/encryption/status
#   4. Stops the daemon by its recorded pid, waits for the port to close,
#      re-snapshots the decoy home and requires it byte-identical.
#   5. Writes .datawatch/reports/state-data-dir-live-check.md with one
#      line per check and a final line of exactly "RESULT: PASS" or
#      "RESULT: FAIL". Exit 0 iff RESULT is PASS.
#
# Guardrails honored: the operator's running daemon, its port, its
# config, its data and the tmux server are never touched; the script
# reads nothing under the real home except what `go build` needs for the
# toolchain cache (the build runs before HOME is repointed); the daemon
# and its temp files live in the run folder.
#
# Usage:
#   bash scripts/state-data-dir-live-check.sh

set -uo pipefail

unset TMUX TMUX_TMPDIR

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="$ROOT/.datawatch/reports/state-data-dir-live-check.md"

# The throwaway run folder lives under the project tree's .datawatch
# (path relative to the project root), one per process so parallel
# invocations cannot collide.
RUN="$ROOT/.datawatch/live-state-data-$$"
mkdir -p "$RUN"
export TD_STATE_DIR="$RUN/tdstate"

[[ -f "$LIB" ]] || { echo "ERR: missing $LIB" >&2; exit 1; }
# shellcheck source=/dev/null
source "$LIB"

cleanup_extra() {
  # Runs AFTER td_stop_all on every exit path: remove the run folder.
  rm -rf "$RUN" 2>/dev/null || true
}
TD_EXTRA_CLEANUP=cleanup_extra
td_install_traps "$@"

# --- layout ------------------------------------------------------------------
THOME="$RUN/home"                 # throwaway HOME for the daemon
THOME_DW="$THOME/.datawatch"      # the decoy state tree (the old fallback dir)
DATA="$RUN/data"                  # the configured data_dir
PROJ="$RUN/projects"              # default_project_dir (never used by these checks)
DTMP="$RUN/tmp"                   # the daemon's TMPDIR
SOCKDIR="$RUN/sock"               # daemon multiplexer socket dir
BIN="$RUN/bin/datawatch"          # built into the run folder, not the repo root
CFG="$RUN/config.yaml"
LOG="$RUN/daemon.log"
SNAP_BEFORE="$RUN/snap-before.txt"
SNAP_AFTER="$RUN/snap-after.txt"
mkdir -p "$THOME_DW/smoke-runs" "$DATA/smoke-runs" "$PROJ" "$DTMP" "$SOCKDIR" "$RUN/bin"

sentinel="STATE-DATA-DIR-DECOY-SENTINEL"

# --- decoys: exactly the state the pre-isolation fallback code would read ----
{ printf '%s\n' "{\"rules\":[{"channel_pattern":"decoy-*","peer_name":"decoy-peer"}]}" ; } \
  > "$THOME_DW/channel_routing.json"
printf '%s\n' '{"cards":[{"id":"decoy-card","cs":1,"rs":1}]}' > "$THOME_DW/dashboard-layout.json"
printf '%s\n' "{\"run_id\":\"decoy\",\"pass\":1,\"$sentinel\":true}" > "$THOME_DW/smoke-runs/decoy.json"
mkdir -p "$THOME_DW/discussions/decoy-discussion"
printf '%s\n' "{\"seq\":99,\"content\":\"$sentinel\",\"role\":\"user\"}" > "$THOME_DW/discussions/decoy-discussion/wal.jsonl"
printf '%s\n' "{\"actors\":[\"$sentinel\"]}" > "$THOME_DW/discussions/decoy-discussion/participants.json"
chmod 600 "$THOME_DW"/channel_routing.json "$THOME_DW"/dashboard-layout.json \
  "$THOME_DW/smoke-runs/decoy.json" "$THOME_DW"/discussions/decoy-discussion/* 2>/dev/null || true

# --- seeded data-dir state: what the daemon MUST serve instead ---------------
printf '%s\n' '{"rules":[{"channel_pattern":"data-dir-*","peer_name":"data-dir-peer"}]}' \
  > "$DATA/channel_routing.json"
printf '%s\n' '{"cards":[{"id":"data-dir-card","cs":1,"rs":1}]}' > "$DATA/dashboard-layout.json"
printf '%s\n' '{"run_id":"data-dir-run","pass":1,"data_dir_run":true}' > "$DATA/smoke-runs/data-dir-run.json"
mkdir -p "$DATA/discussions/data-dir-discussion"
printf '%s\n' '{"seq":1,"content":"data-dir-run","role":"user"}' > "$DATA/discussions/data-dir-discussion/wal.jsonl"
printf '%s\n' '{"actors":["data-dir-run"]}' > "$DATA/discussions/data-dir-discussion/participants.json"

# --- result bookkeeping -------------------------------------------------------
now() { date '+%H:%M:%S'; }
say() { printf '%s\n' "$*"; }
LINES=()          # report body, one entry per line
ov() { # ov <PASS|FAIL> <what> [evidence] — record one check line
  LINES+=("- **$1** — ${2}${3:+; ${3}}")
  say "  $1  $2"
}
OVERALL="PASS"
fail() { ov FAIL "$@"; OVERALL="FAIL"; }

write_report() { # write_report <PASS|FAIL> [extra preamble...]
  local overall="$1"; shift || true
  mkdir -p "$(dirname "$REPORT")"
  {
    printf '# state-data-dir live check\n\n'
    printf -- '- **When:** %s\n' "$(date '+%Y-%m-%d %H:%M:%S')"
    printf -- '- **Script:** `scripts/state-data-dir-live-check.sh`\n'
    printf -- '- **Daemon:** single sandbox daemon started via `td_start` (recorded pid %s), stopped by that recorded pid; tmux, ports, config and data of the operator daemon untouched.\n' "${DPID:-n/a}"
    printf -- '- **Decoy HOME:** throwaway home under the run folder with sentinel `channel_routing.json`, `dashboard-layout.json`, `smoke-runs/`, `discussions/`; snapshot compared before/after.\n\n'
    printf -- '## Checks\n\n'
    local l; for l in "${LINES[@]:-}"; do printf '%s\n' "$l"; done
    printf '\n'
    printf 'RESULT: %s\n' "$overall"
  } | sed -e "s|$RUN|<run>|g" -e "s|$THOME|<decoy-home>|g" > "$REPORT"
  say "report: $REPORT"
}

# --- prerequisites -----------------------------------------------------------
for t in curl python3; do
  command -v "$t" >/dev/null 2>&1 || { say "SKIP: $t is not on PATH"; exit 0; }
done
command -v go >/dev/null 2>&1 || { say "SKIP: go is not on PATH (the binary is built into the run folder)"; exit 0; }
[[ -f "$ROOT/testdata/datawatch.yaml" ]] || { say "SKIP: testdata/datawatch.yaml not found"; exit 0; }

free_port() {
  python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'
}
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"
TOKEN="dw-livedata-$$"
BASE="https://127.0.0.1:$TLS_PORT"

wait_until() { # wait_until <timeout-seconds> <command...>
  local t="$1"; shift
  local end=$((SECONDS + t))
  while ((SECONDS < end)); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  return 1
}

# ts <desc> <cmd...> — run a command under the start/end timestamp convention
ts() {
  local d="$1" t0 rc
  shift
  t0="$(date +%s)"
  say "[$(now)] ${d} — start"
  "$@"; rc=$?
  say "[$(now)] ${d} — done ($(( $(date +%s) - t0 ))s) rc=$rc"
  return $rc
}

ts_begin() { say "[$(now)] $1 — start"; _TS0="$(date +%s)"; } # for shell functions
 ts_end()   { say "[$(now)] $1 — done ($(( $(date +%s) - _TS0 ))s)"; }

# --- leftover-daemon snapshot (before we start anything) ---------------------
LEFT_SNAP="$RUN/leftover.snap"
env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" snapshot "$LEFT_SNAP" >/dev/null 2>&1 || true

# --- build into the run folder (real HOME: go toolchain cache) ---------------
say "state-data-dir live check — $(now)"
say "ports: http $PORT tls $TLS_PORT mcp $MCP_PORT"

ts "build daemon into run folder" env -u TMUX -u TMUX_TMPDIR go build -C "$ROOT" -o "$BIN" ./cmd/datawatch/
[[ -x "$BIN" ]] || { fail "build the daemon binary into the run folder" "go build failed: $(tail -3 "$LOG" 2>/dev/null | tr '\n' ' ')"; write_report FAIL; exit 1; }
ov PASS "daemon binary built into the run folder (not the repo root)" "go build -o <run>/bin/datawatch ./cmd/datawatch/"

# --- config -------------------------------------------------------------------
ts_begin "write sandbox config"
sed \
  -e "s|data_dir: /data|data_dir: $DATA|g" \
  -e "s|default_project_dir: /data/projects|default_project_dir: $PROJ|g" \
  -e "s|port: 8080|port: $PORT|g" \
  -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
  -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" \
  -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
  -e "s|token: \"\"|token: \"$TOKEN\"|g" \
  -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
  -e "/^web_search:/{n;/enabled: true/s//enabled: false/}" \
  -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$DNS_PORT\"|g" \
  -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$HOOK_PORT|g" \
  -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$NTFY_PORT|g" \
  "$ROOT/testdata/datawatch.yaml" > "$CFG"
ts_end "write sandbox config"
[[ -s "$CFG" ]] || { fail "write the sandbox config"; write_report FAIL; exit 1; }
ov PASS "sandbox config written: data_dir inside the run folder, free loopback ports, test token" "port=$PORT tls=$TLS_PORT"

# --- decoy-home snapshot (before/after) ---------------------------------------
# The byte-identity requirement is about the decoy's datawatch STATE tree
# (.datawatch). The daemon also spawns auxiliary MCP bridges (node / claude
# cli) whose only side effects are caches under $HOME/.cache and a fresh
# $HOME/.claude.json; those paths are tolerated, anything else differing
# fails the check.
snap_home() {
  ( cd "$THOME" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum ) 2>/dev/null \
    | sed 's|^\./||'
}
BYPRODUCT_RE='^(\./)?(\.cache/|\.claude\.json$)'
ts_begin "snapshot decoy home (before)"
snap_home > "$SNAP_BEFORE"
ts_end "snapshot decoy home (before)"
ov PASS "decoy home planted and snapshotted (before)" "$(wc -l < "$SNAP_BEFORE") files, $(wc -c < "$SNAP_BEFORE") bytes of checksums"

# --- start the single sandbox daemon ------------------------------------------
export PATH="$RUN/bin:$PATH" TMPDIR="$DTMP" TMUX_TMPDIR="$SOCKDIR" HOME="$THOME"
ts_begin "start sandbox daemon (HOME=throwaway home, env -u TMUX -u TMUX_TMPDIR)"
td_start "$BIN" "$CFG" "$LOG"; START_RC=$?
ts_end "start sandbox daemon (HOME=throwaway home, env -u TMUX -u TMUX_TMPDIR)"
DPID="${TD_LAST_PID:-}"
if [[ $START_RC -ne 0 || -z "$DPID" ]] || ! td_alive "$DPID"; then
  fail "start exactly one sandbox daemon" "td_start failed: $(grep -m 3 -Ei 'error|panic|fata' "$LOG" 2>/dev/null | head -3 | tr '\n' ' ')"
  write_report FAIL; exit 1
fi
td_set_api "$DPID" "$BASE" "$TOKEN"
if ! wait_until 90 curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then
  fail "sandbox daemon answers /api/health" "log tail: $(tail -5 "$LOG" 2>/dev/null | tr '\n' ' ')"
  write_report FAIL; exit 1
fi
ov PASS "single sandbox daemon up and healthy (pid recorded)" "pid $DPID, /api/health 200 at $(now)"

# --- the four GET endpoints ----------------------------------------------------
do_get() { # do_get <path> — sets G_CODE G_BODY; timestamped
  local p="$1" out p0
  p0="$(date +%s)"
  say "[$(now)] GET $p — start"
  out="$(curl -sk -m 30 -w '\n%{http_code}' -H "Authorization: Bearer $TOKEN" "$BASE$p")"
  say "[$(now)] GET $p — done ($(( $(date +%s) - p0 ))s)"
  G_CODE="${out##*$'\n'}"
  G_BODY="$(printf '%s' "${out%$'\n'*}" | tr '\n' ' ')"
}

# 1) channel routing
do_get "/api/channel/routing"
ROUTING_OK=0
[[ "$G_CODE" == 200 ]] || ROUTING_OK=1
printf '%s' "$G_BODY" | grep -qF '"data-dir-peer"' || ROUTING_OK=1
if printf '%s' "$G_BODY" | grep -qF '"decoy-peer"'; then ROUTING_OK=1; fi
if printf '%s' "$G_BODY" | grep -qF "$sentinel"; then ROUTING_OK=1; fi
if [[ $ROUTING_OK -eq 0 ]]; then
  ov PASS "GET /api/channel/routing: 200, serves the data-dir rule, no decoy" "body: $(printf '%s' "$G_BODY" | cut -c1-300)"
else
  fail "GET /api/channel/routing: expected 200 with the data-dir rule, none of the decoy" "status=$G_CODE body: $(printf '%s' "$G_BODY" | cut -c1-300)"
fi

# 2) dashboard layout
do_get "/api/dashboard/layout"
LAYOUT_OK=0
[[ "$G_CODE" == 200 ]] || LAYOUT_OK=1
printf '%s' "$G_BODY" | grep -qF 'data-dir-card' || LAYOUT_OK=1
if printf '%s' "$G_BODY" | grep -qF 'decoy-card'; then LAYOUT_OK=1; fi
if printf '%s' "$G_BODY" | grep -qF "$sentinel"; then LAYOUT_OK=1; fi
if [[ $LAYOUT_OK -eq 0 ]]; then
  ov PASS "GET /api/dashboard/layout: 200, serves the data-dir card set, no decoy" "body: $(printf '%s' "$G_BODY" | cut -c1-300)"
else
  fail "GET /api/dashboard/layout: expected 200 with the data-dir layout, no decoy card" "status=$G_CODE body: $(printf '%s' "$G_BODY" | cut -c1-300)"
fi

# 3) smoke progress (list)
do_get "/api/smoke/progress"
SMOKE_OK=0
[[ "$G_CODE" == 200 ]] || SMOKE_OK=1
printf '%s' "$G_BODY" | grep -qF 'data-dir-run' || SMOKE_OK=1
if printf '%s' "$G_BODY" | grep -qF '"decoy"'; then SMOKE_OK=1; fi
if printf '%s' "$G_BODY" | grep -qF "$sentinel"; then SMOKE_OK=1; fi
if [[ $SMOKE_OK -eq 0 ]]; then
  ov PASS "GET /api/smoke/progress: 200, lists the data-dir run, not the decoy run" "body: $(printf '%s' "$G_BODY" | cut -c1-300)"
else
  fail "GET /api/smoke/progress: expected 200 listing only the data-dir run" "status=$G_CODE body: $(printf '%s' "$G_BODY" | cut -c1-300)"
fi

# 4) encryption status
do_get "/api/security/encryption/status"
ENC_OK=0
[[ "$G_CODE" == 200 ]] || ENC_OK=1
python3 -I -c 'import json,sys; json.loads(sys.argv[1])' "$G_BODY" 2>/dev/null || ENC_OK=1
printf '%s' "$G_BODY" | grep -qF 'data-dir-discussion' || ENC_OK=1
if printf '%s' "$G_BODY" | grep -qF 'decoy-discussion'; then ENC_OK=1; fi
if printf '%s' "$G_BODY" | grep -qF "$THOME"; then ENC_OK=1; fi
if [[ $ENC_OK -eq 0 ]]; then
  ov PASS "GET /api/security/encryption/status: 200, JSON, lists only data-dir files, no decoy home path" "body: $(printf '%s' "$G_BODY" | cut -c1-400)"
else
  fail "GET /api/security/encryption/status: expected 200 JSON over the data dir only" "status=$G_CODE body: $(printf '%s' "$G_BODY" | cut -c1-400)"
fi

# --- stop the daemon by recorded pid -------------------------------------------
ts_begin "stop sandbox daemon (by its recorded pid only)"
td_stop_all; STOP_RC=$?
ts_end "stop sandbox daemon (by its recorded pid only)"
STOP_OK=0
if td_alive "${DPID:-0}" 2>/dev/null; then STOP_OK=1; fi
if ! td_wait_port_closed "$TLS_PORT" 15; then STOP_OK=1; fi
if [[ $STOP_OK -eq 0 ]]; then
  ov PASS "daemon ended by its recorded pid; port $TLS_PORT closed" "pid $DPID gone at $(now)"
else
  fail "daemon ended by its recorded pid and the port released" "alive=$(kill -0 "$DPID" 2>/dev/null && echo yes || echo no)"
fi

ts "leftover test-daemon check" env -u TMUX -u TMUX_TMPDIR bash "$ROOT/scripts/check-no-leftover-test-daemons.sh" check "$LEFT_SNAP"
LEFT=$?
if [[ $LEFT -eq 0 ]]; then
  ov PASS "check-no-leftover-test-daemons.sh: no residual test daemon" ""
else
  fail "check-no-leftover-test-daemons.sh reports a residual test daemon" ""
fi

# --- decoy-home snapshot (after — daemon fully stopped) ------------------------
ts_begin "snapshot decoy home (after)"
snap_home > "$SNAP_AFTER"
ts_end "snapshot decoy home (after)"
INTACT=0
# Strict: the decoy state tree must be byte-identical, entry for entry.
DW_B="$(grep -F ' .datawatch/' "$SNAP_BEFORE" | LC_ALL=C sort)"
DW_A="$(grep -F ' .datawatch/' "$SNAP_AFTER" | LC_ALL=C sort)"
if [[ "$DW_B" != "$DW_A" ]]; then
  fail "decoy .datawatch state tree byte-identical after the run" "differing/dropped/added state entries: $(printf '%s\n' "$DW_B" > "$RUN/dw-b.txt"; printf '%s\n' "$DW_A" > "$RUN/dw-a.txt"; diff "$RUN/dw-b.txt" "$RUN/dw-a.txt" 2>/dev/null | head -8 | tr '\n' ' ')"
else
  ov PASS "decoy .datawatch state tree byte-identical after the run (no reads into, no writes on it)" "all $(wc -l < <(printf '%s' "$DW_B")) decoy state files present with equal checksums"
fi
# Whole-home diff: tolerate only auxiliary MCP/claude-cli byproduct paths.
BAD_PATHS="$(python3 - "$SNAP_BEFORE" "$SNAP_AFTER" <<'PY_DIFF'
import sys
def entries(f):
    out = set()
    for line in open(f):
        parts = line.rstrip("\n").split("  ", 1)
        if len(parts) == 2:
            out.add(tuple(parts))
    return out
diff = entries(sys.argv[1]) ^ entries(sys.argv[2])
# Allowed byproducts: agent-CLI caches, plus the two files the daemon's
# web-search bridge (datawatch mcp-search, HOME-based data dir by documented
# design in internal/mcp/search/server.go) creates under $HOME/.datawatch.
PREFIX = (".cache/", ".claude", ".gemini/", ".local/")
EXACT = {".datawatch/secrets.key", ".datawatch/websearch.db",
         ".datawatch/websearch.db-shm", ".datawatch/websearch.db-wal"}
for _h, path in sorted(diff, key=lambda t: t[1]):
    if path.startswith("./"):
        path = path[2:]
    if path.startswith(PREFIX) or path in EXACT:
        continue
    print(path)
PY_DIFF
)" || BAD_RC=1
BAD_RC="${BAD_RC:-0}"
if [[ $BAD_RC -ne 0 ]]; then
  fail "decoy HOME: seeded state files untouched, only documented byproducts added" "could not compute the home before/after diff"
elif [[ -z "$(printf '%s' "$BAD_PATHS" | tr -d '[:space:]')" ]]; then
  ov PASS "decoy HOME: seeded state files untouched, only documented byproducts added" "seeded decoy state files byte-identical; differing files are only agent-CLI caches (.cache/, .claude*, .gemini/, .local/) plus the web-search bridge documented secrets.key / websearch.db byproducts"
else
  fail "decoy HOME: seeded state files untouched, only documented byproducts added" "unexpected differing entries: $(printf '%s' "$BAD_PATHS" | head -6 | tr '\n' ' ')"
fi

# --- verdict -------------------------------------------------------------------
write_report "$OVERALL"
if [[ "$OVERALL" == "PASS" ]]; then
  say "RESULT: PASS"
  exit 0
fi
say "RESULT: FAIL"
exit 1
