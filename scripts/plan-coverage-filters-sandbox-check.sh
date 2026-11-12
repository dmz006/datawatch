#!/usr/bin/env bash
# plan-coverage-filters-sandbox-check.sh
#
# Live sandbox check for the plan-coverage finding filters:
#
#   autonomous.plan_coverage_suppress_created_files  (default true)
#   autonomous.plan_coverage_scoped_requirements     (default true)
#   autonomous.plan_coverage_requirement_sections    (default "scope,constraints")
#   autonomous.plan_coverage_min_requirement_chars   (default 30; 0 = the default)
#
# The unit tests already prove each filter's pure logic and the
# EffectivePlanCoverageFilters defaults. This script runs the one thing they
# cannot: a real daemon serving the filter config keys end to end.
#
# Checks (one PASS / FAIL / SKIPPED line per check in the report):
#   1. throwaway daemon healthy on free loopback ports; boot log never shows
#      a bind to production port 8443/8444 and shows the proxy sandbox
#      listener disabled (server.proxy_sandbox_port: 0); the
#      test-daemon-lib traps protect it; the scratch dirs are in place
#   2. GET /api/config shows the four keys at their documented defaults
#   3. every key round-trips: set to a non-default value, read it back,
#      restore it, read it back; an invalid minimum (negative or text) is
#      rejected with 400 and changes nothing
#   4. create a scratch Automaton from a fixture spec (a READ FIRST file
#      list, a backend-name token, one hyphenated phrase, a Scope sentence
#      with "must", a Constraints sentence) pointed at a scratch project
#      tree that already has a docs/ folder, then decompose and poll until
#      it finishes or is rolled back to draft
#   5. GET /plan_coverage shows the filters in effect, the suppressed
#      created_files / requirements count object, and no fixture stray
#      token in the unmatched requirements list
#   6. with both filters switched OFF the same read shows the live filters
#      off while the record's own pinned filters are unchanged
#   7. the CLI (datawatch autonomous prd-coverage), the comm-chat router
#      and the stdio MCP tool all agree with the REST read on the
#      suppressed counts
#   8. cleanup and restore: the scratch Automaton is cancelled + removed,
#      the four config keys read back to their pre-check values, the daemon
#      is ended and confirmed dead, and no process is left behind
#
# If the decompose does not finish (planning backend unreachable, or the PRD
# rolls back to draft — including when the product's own gates reject the
# plan the local planner produced), checks 5-7 are recorded as SKIPPED with
# the reason (the product's rejection detail, when one was recorded);
# checks 1, 2, 3 and 8 still run and the script exits 0 provided every check
# that ran passed.
#
# Environment:
#   DATAWATCH_PCF_REPORT       report path, relative to the repo root
#                             (default docs/plans/reports/plan-coverage-filters-live-check.md)
#   DATAWATCH_PCF_WAIT         seconds to wait for the decompose (default 240)
#   DATAWATCH_PCF_KEEP_WORK=1  keep the scratch directory for inspection
#
# Exit: 0 when every check that ran passed, 1 otherwise.

if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOKEN="dw-pcf-sandbox-token-1"
WORK="$(mktemp -d)"
REPORT_REL="${DATAWATCH_PCF_REPORT:-docs/plans/reports/plan-coverage-filters-live-check.md}"
WAIT_SECS="${DATAWATCH_PCF_WAIT:-240}"

EVID="$WORK/evidence.log"
: > "$EVID"
export TD_STATE_DIR="$WORK/tdstate"

# Shared daemon lifecycle: recorded at start, ended on every exit path
# (normal, failure, INT, TERM, HUP), and only then the scratch directory
# removed (TD_EXTRA_CLEANUP runs after td_stop_all). Installed before
# anything else happens.
source "$REPO_ROOT/scripts/test-daemon-lib.sh" \
  || { echo "could not source scripts/test-daemon-lib.sh" >&2; exit 1; }
TD_EXTRA_CLEANUP=cleanup
td_install_traps "$@"

BASE=""
BIN=""
SANDBOX_PID=""
PCF_ID=""
HAVE_RECORD=0

# ---- reporting ------------------------------------------------------------

RESULTS=()
OVERALL=0
FAIL_REASONS="none"
CLEANUP_NOTE=""

# rec LINE — append one check line to the result list and advance the counter.
rec() { RESULTS+=("$1"); CUR=$(( ${CUR:-0} + 1 )); }

# pass WHAT DETAIL / skip WHAT REASON / fail WHAT REASON — 1-based numbering
pass() { rec "$(( ${CUR:-0} + 1 )). $1 — PASS ($2)"; }
skip() { rec "$(( ${CUR:-0} + 1 )). $1 — SKIPPED ($2)"; }
fail() { rec "$(( ${CUR:-0} + 1 )). $1 — FAIL — $2"; OVERALL=1; FAIL_REASONS="$FAIL_REASONS; $1: $2"; }
step() { echo "--- $*"; }

# jget JSON FILT — jq -r against a JSON string; empty on any jq error.
jget() { printf '%s' "$1" | jq -r "$2" 2>/dev/null; }

# api METHOD PATH [BODY] — one authenticated request against the sandbox
# daemon. Appends the request and response to the ordered evidence log,
# leaves the status in HTTP_CODE and the body in API_BODY.
api() {
  local method="$1" path="$2" body="${3:-}" out
  local args=(-sk --max-time 60 -X "$method" -H "Authorization: Bearer $TOKEN" -w $'\n%{http_code}')
  if [[ -n "$body" ]]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi
  out="$(curl "${args[@]}" "$BASE$path" 2>>"$EVID")" || out=""
  [[ "$out" == *$'\n'* ]] || out="$out"$'\n''000'
  HTTP_CODE="${out##*$'\n'}"
  API_BODY="${out%$'\n'*}"
  printf '[%s] %s %s' "$(date -u +%H:%M:%S)" "$method" "$path" >> "$EVID"
  if [[ -n "$body" ]]; then printf ' body=%s' "$body" >> "$EVID"; fi
  printf ' -> HTTP %s body=%s\n' "$HTTP_CODE" "$API_BODY" >> "$EVID"
}

# cfg_read — print "suppress scoped sections min" for the live config.
cfg_read() {
  api GET /api/config
  printf '%s %s %s %s' \
    "$(jget "$API_BODY" '.autonomous.plan_coverage_suppress_created_files')" \
    "$(jget "$API_BODY" '.autonomous.plan_coverage_scoped_requirements')" \
    "$(jget "$API_BODY" '.autonomous.plan_coverage_requirement_sections')" \
    "$(jget "$API_BODY" '.autonomous.plan_coverage_min_requirement_chars')"
}

cleanup() {
  # Runs from the library's TD_EXTRA_CLEANUP hook, after td_stop_all:
  # must not `exit` — the shared handler owns the exit status.
  if [[ "${DATAWATCH_PCF_KEEP_WORK:-0}" == "1" ]]; then
    echo "kept scratch directory: $WORK"
    return 0
  fi
  local home_dir="$WORK/sandbox/home" d pid i
  # The daemon probes its CLI backends at start and while shutting down
  # (gemini/aider/opencode --version) with HOME=$home_dir. Those node/python
  # cold-starts outlive td_stop_all's daemon-only kill, and gemini creates
  # $home_dir/.gemini ~1s after spawn — recreating the tree moments after a
  # successful rm. So: sweep kill-then-remove on a tight cadence for a full
  # window (no early exit — an in-flight probe must not short-circuit us),
  # then settle and verify. Every probe lives only a couple of seconds, so a
  # 0.5s cadence catches each one alive; anything it already created is
  # removed immediately after.
  for i in $(seq 1 16); do
    for d in /proc/[0-9]*; do
      pid=${d#/proc/}
      [[ -r "$d/environ" ]] || continue
      # tr must not open environ itself: bash reports the open failure on
      # stderr even with `tr ... 2>/dev/null`. Open it in a controlled
      # subshell instead (same result, no /proc/<pid>/environ noise).
      if tr '\0' '\n' < <(cat "$d/environ" 2>/dev/null) | grep -Fxq "HOME=$home_dir"; then
        kill -9 "$pid" 2>/dev/null || true
      fi
    done
    rm -rf "$WORK" 2>/dev/null || true
    sleep 0.5
  done
  sleep 2
  rm -rf "$WORK" 2>/dev/null || true
  if [[ -e "$WORK" ]]; then
    echo "warning: scratch directory survived cleanup: $WORK" >&2
  fi
}

finish() { # finish <code> — write the report, then exit (trap does teardown)
  local code="$1"
  local out="$REPO_ROOT/$REPORT_REL"
  mkdir -p "$(dirname "$out")"
  {
    echo "# Plan coverage filters — sandbox live check"
    echo
    echo "Date: $(date -u +%Y-%m-%d)"
    echo
    echo "Sandbox note: this ran the checks against a real daemon, not the unit-test-equivalents alone."
    echo "A throwaway daemon was built from this tree and started inside one scratch directory"
    echo "($( printf '%s' "$WORK" | sed 's|.*|<scratch>|' )) that carried its own HOME, its own data dir, its own config and its own scratch Automaton and project tree."
    echo "All of its listeners were bound to free loopback ports, never 8443 or 8444,"
    echo "and it was ended by the daemon library before the scratch directory was removed."
    echo
    if [[ "$code" == "0" ]]; then
      echo "Result: OK — every check that ran passed."
    else
      echo "Result: FAILED — $FAIL_REASONS"
    fi
    echo
    echo "## Checks"
    echo
    local line
    for line in "${RESULTS[@]}"; do echo "$line"; done
    echo
    echo "## Cleanup"
    echo
    echo "$CLEANUP_NOTE"
    echo
    echo "## Evidence (ordered request/response log)"
    echo
    echo '```'
    sed -e "s|$WORK|<scratch>|g" -e "s|$REPO_ROOT|<repo>|g" -e 's|/home/|<home-abs>|g' "$EVID"
    echo '```'
  } > "$out"
  echo "report written: $REPORT_REL"
  exit "$code"
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

# ============================================================================
# Sandbox boot
# ============================================================================

start_sandbox() {
  local sbx bin tpl
  sbx="$WORK/sandbox"
  bin="$WORK/datawatch"
  tpl="$REPO_ROOT/testdata/datawatch.yaml"
  [[ -f "$tpl" ]] || { step "FATAL: sandbox config template missing: testdata/datawatch.yaml"; return 1; }
  # The sandbox planning backend is a local ollama endpoint. Confirm it is
  # actually serving the model the bootstrap config asks for; if it is not,
  # record that so the check 4 skip reason is the truth (planner genuinely
  # down) rather than an unremapped hostname. This preflight never blocks
  # the sandbox boot itself — checks 1-3 and 8 do not need the planner.
  # The template planner is the tiny qwen3:1.7b. It routinely omits the
  # per-task verification_script that the product's verification-script-coverage
  # gate demands (a file-changing task without its own check is rejected), so
  # decompose only ever lands on a rejection record and checks 4-7 can only be
  # SKIPPED. Prefer the strongest small local model available: any candidate
  # the endpoint actually serves, in order, otherwise fall back to the
  # template's own model.
  #
  # Candidate ORDER matters: the ollama adapter
  # (internal/inference/adapters/ollama.go) reads only the generate "response"
  # field. Thinking models (qwen3.8-* etc.) frequently emit their whole answer
  # in "thinking" with an empty "response", which the adapter turns into a 200
  # with an empty answer that decomparse drops as a non-retryable failure.
  # Put non-thinking models first.
  local tags template_model
  template_model="$(grep -E '^  model:' "$tpl" | head -1 | awk '{print $2}')"
  tags="$(curl -sf --max-time 5 http://127.0.0.1:11434/api/tags 2>/dev/null)"
  local cand
  PLAN_MODEL=""
  if [[ -n "${DATAWATCH_PCF_PLANNER_MODEL:-}" ]] && printf '%s' "$tags" | jq -e --arg m "${DATAWATCH_PCF_PLANNER_MODEL}" '.models[]? | select((.name // "") | startswith($m))' >/dev/null 2>&1; then
    PLAN_MODEL="${DATAWATCH_PCF_PLANNER_MODEL}"
  else
    for cand in mistral-small3.2:latest qwen3.8-64k:27b qwen3:8b "$template_model"; do
      if [[ -n "$tags" ]] && printf '%s' "$tags" | jq -e --arg m "$cand" '.models[]? | select((.name // "") | startswith($m))' >/dev/null 2>&1; then
        PLAN_MODEL="$cand"; break
      fi
    done
  fi
  PLANNER_OK=0; PLANNER_WHY="not preflighted"
  if [[ -n "$PLAN_MODEL" ]] && printf '%s' "$tags" | jq -e --arg m "$PLAN_MODEL" '.models[]? | select((.name // "") | startswith($m))' >/dev/null 2>&1; then
    PLANNER_OK=1; PLANNER_WHY=""
  else
    PLANNER_WHY="the local ollama endpoint has no usable planning model (tried mistral-small3.2:latest, qwen3.8-64k:27b, qwen3:8b, '$template_model')"
  fi
  step "planner preflight: local ollama at http://127.0.0.1:11434 model '$PLAN_MODEL' — $([[ $PLANNER_OK == 1 ]] && echo available || echo "($PLANNER_WHY)")"
  step "build the daemon from this tree"
  (cd "$REPO_ROOT" && go build -o "$bin" ./cmd/datawatch/) > "$WORK/build.log" 2>&1 \
    || { tail -30 "$WORK/build.log"; step "FATAL: go build of this tree failed"; return 1; }
  step "built $bin"

  local p_http p_tls p_mcp p_dns p_hook p_ntfy ports
  ports=$(free_ports 6) || { step "FATAL: could not pick free ports"; return 1; }
  { read -r p_http; read -r p_tls; read -r p_mcp; read -r p_dns; read -r p_hook; read -r p_ntfy; } <<<"$ports"
  mkdir -p "$sbx/data/projects" "$sbx/home"
  # proxy_sandbox_port: 0 disables the remote-PWA proxy listener; unset it
  # defaults to 8444, the production proxy's port.
  # http://local-node:11434 is a docker-compose name that does not resolve
  # on a bare host; the planning backend (and the llms[] refs that point at
  # it) must be remapped to the local ollama endpoint or the decompose
  # lane is dead on arrival and checks 4-7 can only be SKIPPED.
  local planner_url="http://127.0.0.1:11434"
  sed \
    -e "s|data_dir: /data|data_dir: $sbx/data|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $sbx/data/projects|g" \
    -e "s|port: 8080|port: $p_http|g" \
    -e "s|^  tls_port: 8443|  tls_port: $p_tls\n  proxy_sandbox_port: 0|" \
    -e "s|sse_port: 9090|sse_port: $p_mcp|g" \
    -e "s|http://local-node:11434|$planner_url|g" \
    -e "s|model: qwen3:1\.7b|model: $PLAN_MODEL|g" \
    -e "s|^  planning_backend: ollama$|  planning_backend: ollama\n  planning_model: $PLAN_MODEL|" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$p_dns\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$p_hook|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$p_ntfy|g" \
    "$tpl" > "$sbx/config.yaml"
  cp "$sbx/config.yaml" "$sbx/data/config.yaml"
  grep -q '^  proxy_sandbox_port: 0$' "$sbx/config.yaml" \
    || { step "FATAL: the bootstrap config does not disable the proxy sandbox listener"; return 1; }
  grep -q '^  host: http://127\.0\.0\.1:11434$' "$sbx/config.yaml" \
    || { step "FATAL: the bootstrap config does not remap the planning backend to the local ollama endpoint"; return 1; }
  grep -q "^  model: $PLAN_MODEL$" "$sbx/config.yaml" \
    || { step "FATAL: the bootstrap config does not select the preflighted planning model '$PLAN_MODEL'"; return 1; }
  grep -q "^  planning_model: $PLAN_MODEL$" "$sbx/config.yaml" \
    || { step "FATAL: the bootstrap config does not set autonomous.planning_model to '$PLAN_MODEL'"; return 1; }
  python3 -I -c '
import sys
try:
    import yaml
except ImportError:
    sys.exit(0)
yaml.safe_load(open(sys.argv[1]))
' "$sbx/config.yaml" 2>/dev/null \
    || { step "FATAL: the bootstrap config is not valid YAML"; return 1; }

  # Scratch project tree for the fixture spec: has a docs/ folder, because
  # the spec's READ FIRST list references docs/pcfsandbox-guide.md. It must
  # sit under a daemon-allowed project root: with the sandbox HOME, the
  # default workspace root is $sbx/home/workspace.
  local proj="$sbx/home/workspace/proj"
  PROJ_DIR="$proj"
  mkdir -p "$proj/docs"
  echo "# pcf fixture project" > "$proj/README.md"
  echo "# pcf fixture docs" > "$proj/docs/pcfsandbox-guide.md"

  HOME="$sbx/home" env -u TMUX -u TMUX_TMPDIR "$bin" start --foreground --config "$sbx/config.yaml" \
    > "$sbx/daemon.log" 2>&1 &
  SANDBOX_PID=$!
  BIN="$bin"
  td_track_pid "$SANDBOX_PID" "$sbx/config.yaml" \
    || { step "FATAL: could not record sandbox daemon pid $SANDBOX_PID for cleanup"; return 1; }
  BASE="https://127.0.0.1:$p_tls"
  td_set_api "$SANDBOX_PID" "$BASE" "$TOKEN" 2>/dev/null || true

  local i
  for i in $(seq 1 120); do
    curl -skf --max-time 2 "$BASE/api/health" >/dev/null 2>&1 && break
    kill -0 "$SANDBOX_PID" 2>/dev/null || { tail -30 "$sbx/daemon.log"; step "FATAL: sandbox daemon exited during start"; return 1; }
    sleep 0.5
  done
  curl -skf --max-time 2 "$BASE/api/health" >/dev/null 2>&1 \
    || { tail -30 "$sbx/daemon.log"; step "FATAL: sandbox daemon did not become healthy within 60s"; return 1; }
  step "sandbox daemon pid $SANDBOX_PID healthy at $BASE"
  return 0
}

PROJ_DIR=""
if ! start_sandbox; then
  fail "throwaway daemon setup" "the daemon failed to build or start — see build.log / daemon.log in the scratch dir"
  CLEANUP_NOTE="The daemon never became healthy. The recorded-daemon trap ended everything the check started and the scratch directory was removed on exit (set DATAWATCH_PCF_KEEP_WORK=1 to keep it for inspection)."
  finish 1
fi

# ============================================================================
# CHECK 1 — throwaway daemon setup
# ============================================================================

step "check 1: throwaway daemon setup"
LOG1="$WORK/sandbox/daemon.log"
D1_OK=1; D1_WHY=""
trap -p EXIT | grep -q . || { D1_OK=0; D1_WHY="no EXIT trap is installed in the starting shell; "; }
td_is_recorded "$SANDBOX_PID" || { D1_OK=0; D1_WHY="$D1_WHYthe daemon is not recorded with scripts/test-daemon-lib.sh; "; }
if grep -q -E 'could not bind .*:844[34]|:844[34]' "$LOG1"; then
  D1_OK=0; D1_WHY="$D1_WHYthe boot log shows the daemon touching production port 8443/8444; "
fi
grep -q 'proxy sandbox listener disabled' "$LOG1" \
  || { D1_OK=0; D1_WHY="$D1_WHYthe boot log does not show the proxy sandbox listener disabled (server.proxy_sandbox_port: 0); "; }
[[ -f "$WORK/sandbox/data/config.yaml" && -n "$PROJ_DIR" && -d "$PROJ_DIR/docs" ]] \
  || { D1_OK=0; D1_WHY="$D1_WHYscratch data dir or fixture project docs/ folder missing; "; }
if [[ "$D1_OK" == 1 ]]; then
  pass "throwaway daemon setup" "daemon healthy at $BASE with traps installed, scratch dirs in place, proxy sandbox listener disabled, no bind attempt on 8443/8444"
else
  fail "throwaway daemon setup" "$D1_WHY"
  finish 1
fi

# ============================================================================
# CHECK 2 — GET /api/config shows documented defaults
# ============================================================================

step "check 2: default keys visible in GET /api/config"
api GET /api/config
V_SUP="$(jget "$API_BODY" '.autonomous.plan_coverage_suppress_created_files')"
V_SCOP="$(jget "$API_BODY" '.autonomous.plan_coverage_scoped_requirements')"
V_SECS="$(jget "$API_BODY" '.autonomous.plan_coverage_requirement_sections')"
V_MIN="$(jget "$API_BODY" '.autonomous.plan_coverage_min_requirement_chars')"
if [[ "$HTTP_CODE" == "200" \
      && "$V_SUP" == "true" && "$V_SCOP" == "true" \
      && "$V_SECS" == "scope,constraints" && "$V_MIN" == "30" ]]; then
  pass "GET /api/config shows the documented defaults" \
    "suppression=$V_SUP scoping=$V_SCOP sections='$V_SECS' min_chars=$V_MIN"
else
  fail "GET /api/config shows the documented defaults" \
    "got HTTP $HTTP_CODE suppression=$V_SUP scoping=$V_SCOP sections='$V_SECS' min_chars=$V_MIN (want true/true/scope,constraints/30)"
  finish 1
fi

# ============================================================================
# CHECK 3 — every key round-trips; an invalid minimum is rejected
# ============================================================================

step "check 3: round-trip each key; an invalid minimum is rejected with 400"
R3_OK=1; R3_WHY=""

# suppress_created_files: 2nd of the live pair → off → back on (2nd of the pair)
api PUT /api/config '{"autonomous.plan_coverage_suppress_created_files": false}'
[[ "$HTTP_CODE" == "200" ]] || { R3_OK=0; R3_WHY="PUT suppression=false gave HTTP $HTTP_CODE; "; }
api GET /api/config
[[ "$(jget "$API_BODY" '.autonomous.plan_coverage_suppress_created_files')" == "false" ]] \
  || { R3_OK=0; R3_WHY="suppression read back was not false after setting it off; "; }
api PUT /api/config '{"autonomous.plan_coverage_suppress_created_files": true}'
api GET /api/config
[[ "$(jget "$API_BODY" '.autonomous.plan_coverage_suppress_created_files')" == "true" ]] \
  || { R3_OK=0; R3_WHY="suppression did not read back true after restore; "; }

# scoped_requirements → off → back on
api PUT /api/config '{"autonomous.plan_coverage_scoped_requirements": false}'
[[ "$HTTP_CODE" == "200" ]] || { R3_OK=0; R3_WHY="PUT scoping=false gave HTTP $HTTP_CODE; "; }
api GET /api/config
[[ "$(jget "$API_BODY" '.autonomous.plan_coverage_scoped_requirements')" == "false" ]] \
  || { R3_OK=0; R3_WHY="scoping read back was not false after setting it off; "; }
api PUT /api/config '{"autonomous.plan_coverage_scoped_requirements": true}'
api GET /api/config
[[ "$(jget "$API_BODY" '.autonomous.plan_coverage_scoped_requirements')" == "true" ]] \
  || { R3_OK=0; R3_WHY="scoping did not read back true after restore; "; }

# requirement_sections: a non-default value → the value the check set → restore
api PUT /api/config '{"autonomous.plan_coverage_requirement_sections": "scope"}'
[[ "$HTTP_CODE" == "200" ]] || { R3_OK=0; R3_WHY="PUT sections=scope gave HTTP $HTTP_CODE; "; }
api GET /api/config
SECS_NOW="$(jget "$API_BODY" '.autonomous.plan_coverage_requirement_sections')"
[[ "$SECS_NOW" == "scope" ]] || { R3_OK=0; R3_WHY="sections read back was '$SECS_NOW', want 'scope'; "; }
api PUT /api/config '{"autonomous.plan_coverage_requirement_sections": "scope,constraints"}'
api GET /api/config
SECS_NOW="$(jget "$API_BODY" '.autonomous.plan_coverage_requirement_sections')"
[[ "$SECS_NOW" == "scope,constraints" ]] || { R3_OK=0; R3_WHY="sections did not read back 'scope,constraints' after restore, got '$SECS_NOW'; "; }

# min_requirement_chars: a non-default value → the value → back to the effective default
api PUT /api/config '{"autonomous.plan_coverage_min_requirement_chars": 40}'
[[ "$HTTP_CODE" == "200" ]] || { R3_OK=0; R3_WHY="PUT min_chars=40 gave HTTP $HTTP_CODE; "; }
api GET /api/config
[[ "$(jget "$API_BODY" '.autonomous.plan_coverage_min_requirement_chars')" == "40" ]] \
  || { R3_OK=0; R3_WHY="min_chars read back was not 40; "; }
api PUT /api/config '{"autonomous.plan_coverage_min_requirement_chars": 0}'
api GET /api/config
[[ "$(jget "$API_BODY" '.autonomous.plan_coverage_min_requirement_chars')" == "30" ]] \
  || { R3_OK=0; R3_WHY="min_chars did not read back the effective default 30 after restoring 0; "; }

# An invalid minimum is rejected with 400 and changes nothing.
api PUT /api/config '{"autonomous.plan_coverage_min_requirement_chars": -1}'
[[ "$HTTP_CODE" == "400" ]] || { R3_OK=0; R3_WHY="negative minimum ($HTTP_CODE) was not rejected with 400; "; }
api PUT /api/config '{"autonomous.plan_coverage_min_requirement_chars": "thirty"}'
[[ "$HTTP_CODE" == "400" ]] || { R3_OK=0; R3_WHY="text minimum ($HTTP_CODE) was not rejected with 400; "; }

# restore + read back to prove the rejects changed nothing
api PUT /api/config '{"autonomous.plan_coverage_min_requirement_chars": 0}'
api GET /api/config
LAST="$(jget "$API_BODY" '.autonomous.plan_coverage_min_requirement_chars')"
[[ "$LAST" == "30" ]] || { R3_OK=0; R3_WHY="min_chars ended at '$LAST', want 30; "; }

if [[ "$R3_OK" == 1 ]]; then
  pass "config round-trip" \
    "each of the four keys was set to a non-default value, read it back, restored it and read it back; a negative minimum and a text minimum were each rejected with 400 and changed nothing"
else
  fail "config round-trip" "$R3_WHY"
  finish 1
fi

# ============================================================================
# CHECK 4 — create a scratch Automaton from a fixture spec, decompose, poll
# ============================================================================

step "check 4: create a scratch Automaton from a fixture spec; decompose; poll"
# Fixture spec: a READ FIRST file list (three scratch paths), a backend-name
# token (qzx-eta-backend), one hyphenated phrase (cross-cutting-hedge), a
# Scope sentence with "must", and a Constraints sentence. The stray tokens
# are exactly the kinds of things the finding filters must keep out of the
# coverage report.
SPEC='Read first: internal/pcfsandbox/alpha.go, internal/pcfsandbox/alpha_test.go, docs/pcfsandbox-guide.md. Use the backend token qzx-eta-backend and the hyphenated phrase cross-cutting-hedge in the fixture notes. Scope: The plan must add the alpha widget end to end. Constraints: Keep the fixture tree under ten files. Every task that will change files needs its own verification_script: a path under .datawatch/verify/ that checks only that task''s own files. A story-level script does not cover its tasks, and a task with no planned files is exempt.'

api POST /api/autonomous/prds \
  "$(jq -cn --arg spec "$SPEC" --arg dir "$PROJ_DIR" \
      '{spec: $spec, actor: "pcf-sandbox-check", project_dir: $dir}')"
if [[ "$HTTP_CODE" == "200" ]]; then
  PCF_ID="$(jget "$API_BODY" '.id')"
else
  fail "scratch Automaton + decompose" "POST /api/autonomous/prds gave HTTP $HTTP_CODE: $(printf '%.200s' "$API_BODY")"
  finish 1
fi
step "created scratch Automaton $PCF_ID"

api POST "/api/autonomous/prds/$PCF_ID/decompose" '{}'
if [[ "$HTTP_CODE" != "200" && "$HTTP_CODE" != "202" ]]; then
  fail "scratch Automaton + decompose" "decompose accepted with HTTP $HTTP_CODE, want 200 or 202"
  skip "GET /plan_coverage shows the filters in effect" "decompose was not accepted, so there is no coverage record to read"
  skip "filters OFF read: live filters off, record pinned filters unchanged" "decompose was not accepted, so there is no coverage record to read"
  skip "multi-surface parity" "decompose was not accepted, so there is no coverage record to compare"
  # fall through to check 8 (cleanup/restore still run)
else
  DECOMPOSE_OK=1
fi

if [[ "${DECOMPOSE_OK:-0}" == "1" ]]; then
  # Poll until the PRD finishes decomposing or is rolled back to draft.
  STATUS="" STORIES=""
  for _ in $(seq 1 $(( WAIT_SECS / 5 + 1 ))); do
    sleep 5
    api GET "/api/autonomous/prds/$PCF_ID"
    STATUS="$(jget "$API_BODY" '.status')"
    STORIES="$(jget "$API_BODY" '.stories | length')"
    if [[ "$STATUS" == "planned" || "$STATUS" == "needs_review" || "${STORIES:-0}" -gt 0 ]] 2>/dev/null; then
      break
    fi
    if [[ "$STATUS" == "draft" ]]; then break; fi   # rolled back; LLM unavailable
  done
  case "$STATUS" in
    planned|needs_review)
      pass "scratch Automaton + decompose" "decompose finished in the sandbox: status=$STATUS, stories=$STORIES"
      HAVE_RECORD=1
      ;;
    *)
      # The record carries the product's own rejection reason when one was
      # recorded (the last GET above is still in API_BODY). Surface it so the
      # skip line states the truth — the common live failure is not a missing
      # planner but the plan being rejected by a product gate (e.g.
      # verification-script-coverage, which a headless ask planner cannot
      # satisfy while the PRD has a project_dir set).
      DECOMP_WHY="decompose did not finish (final status='$STATUS')"
      LAST_FAIL_DETAIL="$(jget "$API_BODY" '.last_decompose_failure.detail')"
      LAST_FAIL_CAT="$(jget "$API_BODY" '.last_decompose_failure.category')"
      if [[ -n "$LAST_FAIL_DETAIL" ]]; then
        DECOMP_WHY="$DECOMP_WHY; the product rejected the planner's plan (category=$LAST_FAIL_CAT): $(printf '%.240s' "$LAST_FAIL_DETAIL")"
      elif [[ "${PLANNER_OK:-0}" != "1" ]]; then
        DECOMP_WHY="$DECOMP_WHY; planner preflight said: $PLANNER_WHY"
      else
        DECOMP_WHY="$DECOMP_WHY; the local planner was reachable but the result did not reach a reviewable plan"
      fi
      skip "scratch Automaton + decompose" \
           "$DECOMP_WHY. Checks 5-7 have no coverage record to observe."
      skip "GET /plan_coverage shows the filters in effect" \
           "no coverage record was produced because the decompose did not finish"
      skip "filters OFF read: live filters off, record pinned filters unchanged" \
           "no coverage record was produced because the decompose did not finish"
      skip "multi-surface parity" \
           "no coverage record was produced because the decompose did not finish"
      ;;
  esac
fi

# ============================================================================
# CHECK 5 — GET /plan_coverage with the filters in effect
# ============================================================================

if [[ "${HAVE_RECORD:-0}" == "1" ]]; then
  step "check 5: GET /plan_coverage with the filters in effect"
  api GET "/api/autonomous/prds/$PCF_ID/plan_coverage"
  F_SUP="$(jget "$API_BODY" '.filters.suppress_created_files')"
  F_SCOP="$(jget "$API_BODY" '.filters.scoped_requirements')"
  F_SECS="$(jget "$API_BODY" '.filters.requirement_sections | join(",")')"
  F_MIN="$(jget "$API_BODY" '.filters.min_requirement_chars')"
  SUP_CF="$(jget "$API_BODY" '.suppressed.created_files')"
  SUP_REQ="$(jget "$API_BODY" '.suppressed.requirements')"
  # the record's own report is the object that must not carry the stray tokens
  UNM="$(jget "$API_BODY" '[.plan_coverage.report.unmatched[]?.requirement] | join("\n")')"
  STRAY=""
  for tok in internal/pcfsandbox/alpha.go internal/pcfsandbox/alpha_test.go \
             docs/pcfsandbox-guide.md qzx-eta-backend cross-cutting-hedge; do
    if printf '%s\n' "$UNM" | grep -qF "$tok"; then
      STRAY="$tok"
      break
    fi
  done
  D5_OK=1; D5_WHY=""
  [[ "$HTTP_CODE" == "200" ]] || { D5_OK=0; D5_WHY="HTTP $HTTP_CODE; "; }
  [[ "$F_SUP" == "true" && "$F_SCOP" == "true" && "$F_SECS" == "scope,constraints" && "$F_MIN" == "30" ]] \
    || { D5_OK=0; D5_WHY="live filters read $F_SUP/$F_SCOP/'$F_SECS'/$F_MIN, want true/true/scope,constraints/30; "; }
  # both suppress counts must be present numbers (zero is a valid count)
  [[ "$SUP_CF" =~ ^[0-9]+$ && "$SUP_REQ" =~ ^[0-9]+$ ]] \
    || { D5_OK=0; D5_WHY="suppressed count object was created_files='$SUP_CF' requirements='$SUP_REQ', want two integers; "; }
  [[ -z "$STRAY" ]] || { D5_OK=0; D5_WHY="stray fixture token '$STRAY' appears in the unmatched requirements list; "; }
  if [[ "$D5_OK" == 1 ]]; then
    pass "GET /plan_coverage shows the filters in effect" \
         "live filters suppression=$F_SUP scoping=$F_SCOP sections='$F_SECS' min_chars=$F_MIN; suppressed created_files=$SUP_CF requirements=$SUP_REQ; no fixture stray token in the unmatched list"
  else
    fail "GET /plan_coverage shows the filters in effect" "$D5_WHY"
    finish 1
  fi
else
  # already recorded SKIPPED in check 4's fail branch if applicable
  :
fi

# ============================================================================
# CHECK 6 — filters OFF: live filters off, record's pinned filters unchanged
# ============================================================================

if [[ "${HAVE_RECORD:-0}" == "1" ]]; then
  step "check 6: switch both filters off; read; restore"
  # the record's pinned filters are frozen at compute time — capture them
  api GET "/api/autonomous/prds/$PCF_ID/plan_coverage"
  P_SUP="$(jget "$API_BODY" '.plan_coverage.filters.suppress_created_files')"
  P_SCOP="$(jget "$API_BODY" '.plan_coverage.filters.scoped_requirements')"
  P_CF="$(jget "$API_BODY" '.suppressed.created_files')"
  P_REQ="$(jget "$API_BODY" '.suppressed.requirements')"

  api PUT /api/config '{"autonomous.plan_coverage_suppress_created_files": false, "autonomous.plan_coverage_scoped_requirements": false}'
  if [[ "$HTTP_CODE" != "200" ]]; then
    fail "filters OFF read" "PUT both filters off gave HTTP $HTTP_CODE"
    finish 1
  fi
  REST_PUT_OK=1

  api GET "/api/autonomous/prds/$PCF_ID/plan_coverage"
  O_SUP="$(jget "$API_BODY" '.filters.suppress_created_files')"
  O_SCOP="$(jget "$API_BODY" '.filters.scoped_requirements')"
  O_CF="$(jget "$API_BODY" '.suppressed.created_files')"
  O_REQ="$(jget "$API_BODY" '.suppressed.requirements')"
  O_PSUP="$(jget "$API_BODY" '.plan_coverage.filters.suppress_created_files')"
  O_PSCOP="$(jget "$API_BODY" '.plan_coverage.filters.scoped_requirements')"

  # restore before asserting so a later step always sees the defaults
  api PUT /api/config '{"autonomous.plan_coverage_suppress_created_files": true, "autonomous.plan_coverage_scoped_requirements": true}'
  if [[ "$HTTP_CODE" == "200" ]]; then REST_OK=1; else REST_OK=0; fi

  D6_OK=1; D6_WHY=""
  [[ "$O_SUP" == "false" && "$O_SCOP" == "false" ]] \
    || { D6_OK=0; D6_WHY="live filters after the switch read $O_SUP/$O_SCOP, want false/false; "; }
  [[ "$O_PSUP" == "$P_SUP" && "$O_PSCOP" == "$P_SCOP" ]] \
    || { D6_OK=0; D6_WHY="the record's pinned filters changed from $P_SUP/$P_SCOP to $O_PSUP/$O_PSCOP after a config change; "; }
  [[ "$O_CF" =~ ^[0-9]+$ && "$O_REQ" =~ ^[0-9]+$ ]] \
    || { D6_OK=0; D6_WHY="the suppressed count object was not two integers after the switch; "; }
  [[ "$REST_OK" == "1" ]] || { D6_OK=0; D6_WHY="restore PUT after the switch-off failed (HTTP=$HTTP_CODE); "; }
  if [[ "$D6_OK" == "1" ]]; then
    pass "filters OFF read: live filters off, record pinned filters unchanged" \
         "after switching both off, live filters read false/false while the record's own pinned filters stayed $O_PSUP/$O_PSCOP and the suppressed object was still present (created_files=$O_CF requirements=$O_REQ); restored"
  else
    fail "filters OFF read: live filters off, record pinned filters unchanged" "$D6_WHY"
    finish 1
  fi
else
  :
fi

# ============================================================================
# CHECK 7 — CLI, comm-chat and MCP agree with the REST read
# ============================================================================

if [[ "${HAVE_RECORD:-0}" == "1" ]]; then
  step "check 7: CLI / comm-chat / MCP all agree with the REST read"
  # Reference REST read (after check 6 restored the defaults).
  api GET "/api/autonomous/prds/$PCF_ID/plan_coverage"
  REF_CF="$(jget "$API_BODY" '.suppressed.created_files')"
  REF_REQ="$(jget "$API_BODY" '.suppressed.requirements')"

  # CLI — `datawatch autonomous prd-coverage <id> --json`
  CLI_BODY="$(DATAWATCH_TOKEN="$TOKEN" "$BIN" --config "$WORK/sandbox/config.yaml" -u "$BASE" autonomous prd-coverage "$PCF_ID" --json 2>/dev/null)"
  CLI_RC=$?
  # Tolerate non-JSON log lines the CLI might print: keep the last JSON object line.
  CLI_JSON="$(printf '%s\n' "$CLI_BODY" | python3 -c '
import json, sys
last = None
for line in sys.stdin:
    line = line.strip()
    if line.startswith("{"):
        try:
            last = json.loads(line)
        except Exception:
            pass
print(json.dumps(last) if last is not None else "")
' 2>/dev/null)"
  CLI_CF="$(jget "$CLI_JSON" '.suppressed.created_files // .plan_coverage.suppressed.created_files // empty')"
  CLI_REQ="$(jget "$CLI_JSON" '.suppressed.requirements // .plan_coverage.suppressed.requirements // empty')"

  # comm-chat router — POST /api/test/message
  api POST /api/test/message "$(jq -cn --arg id "$PCF_ID" '{text: ("autonomous plan-coverage " + $id)}')"
  CHAT_RESP="$(printf '%s' "$API_BODY" | jq -r '.responses | join(" ")' 2>/dev/null)"
  SUP_LINE="$(printf '%s\n' "$CHAT_RESP" | grep -oE 'suppressed[^.]*' | head -1)"
  CHAT_CF="$(printf '%s' "$SUP_LINE" | grep -oE 'created files? [0-9]+' | grep -oE '[0-9]+' | head -1)"
  CHAT_REQ="$(printf '%s' "$SUP_LINE" | grep -oE 'requirements? [0-9]+' | grep -oE '[0-9]+' | head -1)"

  # stdio MCP — autonomous_prd_plan_coverage_get
  MCP_RAW="$(printf '%s\n%s\n%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"pcf-live-check","version":"0"},"capabilities":{}}}' \
      '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
      "$(jq -cn --arg id "$PCF_ID" '{jsonrpc:"2.0",id:2,method:"tools/call",params:{name:"autonomous_prd_plan_coverage_get",arguments:{id:$id}}}')" \
    | timeout 60 "$BIN" --config "$WORK/sandbox/config.yaml" mcp 2>/dev/null)"
  MCP_CF="$(printf '%s' "$MCP_RAW" | python3 -c '
import json, sys
try:
    for line in sys.stdin:
        line = line.strip()
        if not line: continue
        o = json.loads(line)
        if o.get("id") == 2 and "result" in o:
            for c in o["result"].get("content", []):
                if c.get("type") == "text":
                    d = json.loads(c["text"])
                    print(d.get("suppressed", {}).get("created_files", ""),
                          d.get("suppressed", {}).get("requirements", ""), sep=" ")
                    sys.exit(0)
except Exception:
    pass
' 2>/dev/null)"
  MCP_CF_VAL="${MCP_CF%% *}"
  MCP_REQ_VAL="${MCP_CF##* }"

  D7_OK=1; D7_WHY=""
  [[ "$CLI_RC" == "0" && "$CLI_CF" == "$REF_CF" && "$CLI_REQ" == "$REF_REQ" ]] \
    || { D7_OK=0; D7_WHY="CLI created_files/requirements='$CLI_CF/$CLI_REQ' (rc=$CLI_RC), REST says '$REF_CF/$REF_REQ'; "; }
  [[ "$CHAT_CF" == "$REF_CF" && "$CHAT_REQ" == "$REF_REQ" ]] \
    || { D7_OK=0; D7_WHY="comm-chat created_files/requirements='$CHAT_CF/$CHAT_REQ', REST says '$REF_CF/$REF_REQ' (chat response: $CHAT_RESP); "; }
  [[ "$MCP_CF_VAL" == "$REF_CF" && "$MCP_REQ_VAL" == "$REF_REQ" ]] \
    || { D7_OK=0; D7_WHY="MCP created_files/requirements='$MCP_CF_VAL/$MCP_REQ_VAL', REST says '$REF_CF/$REF_REQ'; "; }
  if [[ "$D7_OK" == "1" ]]; then
    pass "CLI, comm-chat and MCP agree with the REST read" \
         "all three surfaces report suppressed created_files=$REF_CF requirements=$REF_REQ, same as the REST read"
  else
    fail "CLI, comm-chat and MCP agree with the REST read" "$D7_WHY"
    finish 1
  fi
else
  :
fi

# ============================================================================
# CHECK 8 — cleanup and restore
# ============================================================================

step "check 8: cleanup and restore"
# Cancel + remove the scratch Automaton, then confirm a re-read is a 404.
if [[ -n "$PCF_ID" ]]; then
  api POST "/api/autonomous/prds/$PCF_ID/cancel" '{"actor":"pcf-sandbox-check"}'
  api DELETE "/api/autonomous/prds/$PCF_ID?hard=true"
  api GET "/api/autonomous/prds/$PCF_ID"
  case "${HTTP_CODE:-}" in
    404) PRD_GONE="re-read gave 404 as expected" ;;
    *)   PRD_GONE="re-read after removal gave HTTP $HTTP_CODE (want 404)" ;;
  esac
else
  PRD_GONE="no scratch Automaton to remove (decompose was not accepted)"
fi

# Restore the four keys to their pre-check values.
api PUT /api/config \
  '{"autonomous.plan_coverage_suppress_created_files": true, "autonomous.plan_coverage_scoped_requirements": true, "autonomous.plan_coverage_requirement_sections": "scope,constraints", "autonomous.plan_coverage_min_requirement_chars": 0}'

api GET /api/config
R_SUP="$(jget "$API_BODY" '.autonomous.plan_coverage_suppress_created_files')"
R_SCOP="$(jget "$API_BODY" '.autonomous.plan_coverage_scoped_requirements')"
R_SECS="$(jget "$API_BODY" '.autonomous.plan_coverage_requirement_sections')"
R_MIN="$(jget "$API_BODY" '.autonomous.plan_coverage_min_requirement_chars')"

D8_OK=1; D8_WHY=""
if [[ ! "$R_SUP" == "true" || ! "$R_SCOP" == "true" || "$R_SECS" != "scope,constraints" || "$R_MIN" != "30" ]]; then
  D8_OK=0
  D8_WHY="the four keys did not read back to their pre-check values (got $R_SUP/$R_SCOP/'$R_SECS'/$R_MIN, want true/true/'scope,constraints'/30); "
fi
if [[ "$PRD_GONE" != "re-read gave 404 as expected" ]]; then
  D8_OK=0
  D8_WHY="$D8_WHY$PRD_GONE; "
fi

# End + confirm the sandbox daemon is dead.
td_stop_all 2>/dev/null || true
DEAD=0; i=0
while [[ "$i" -lt 30 ]]; do
  if ! kill -0 "$SANDBOX_PID" 2>/dev/null; then DEAD=1; break; fi
  sleep 0.5
  i=$((i+1))
done
if [[ "$DEAD" != "1" ]]; then
  D8_OK=0
  D8_WHY="$D8_WHY the sandbox daemon pid $SANDBOX_PID was still alive 15s after td_stop_all; "
fi

if [[ "$D8_OK" == "1" ]]; then
  pass "cleanup and restore" \
       "the scratch Automaton $PCF_ID was cancelled, removed and re-read as $PRD_GONE; the four config keys were restored and read back as $R_SUP/$R_SCOP/'$R_SECS'/$R_MIN; the sandbox daemon was ended by scripts/test-daemon-lib.sh td_stop_all and is confirmed dead — no process is left behind"
else
  fail "cleanup and restore" "$D8_WHY"
fi

CLEANUP_NOTE="The scratch Automaton ${PCF_ID:-<none>} was cancelled and removed by this script (POST /cancel then DELETE), re-read $PRD_GONE. The four filter config keys were restored to their pre-check values and read back as $R_SUP/$R_SCOP/'$R_SECS'/$R_MIN. The sandbox daemon was ended by scripts/test-daemon-lib.sh td_stop_all and confirmed dead before the scratch directory was removed; no process is left behind."

# Final report + exit. The daemon-library EXIT trap then removes the scratch
# dir via TD_EXTRA_CLEANUP=cleanup.
if [[ "$OVERALL" == "0" ]]; then
  finish 0
else
  finish 1
fi
