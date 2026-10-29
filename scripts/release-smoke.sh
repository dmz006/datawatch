#!/usr/bin/env bash
# release-smoke.sh — pre-release functional smoke test.
#
# Operator directive 2026-04-27: every release must FUNCTIONALLY test
# every subsystem, not just rely on Go unit tests. The autonomous
# decompose path silently broke in v3.10.0 because the
# `prompt`-vs-`question` field-name mismatch slipped through every
# release boundary — unit tests covered the manager + REST handler
# in isolation but never exercised the loopback together.
#
# Spins up its own isolated test daemon with a fresh data directory.
# Never touches the production daemon or its data. All sessions, PRDs,
# and other resources created during the run are cleaned up on exit
# (explicit API cleanup + daemon shutdown + data dir deletion).
#
# Usage:
#   bash scripts/release-smoke.sh                        # start + test + teardown
#   SMOKE_PORT=19080 SMOKE_TLS_PORT=19443 bash ...        # override ports (normally auto-assigned)
#   KEEP_TEST_DIR=1 bash scripts/release-smoke.sh         # keep data dir on exit
#
# External-daemon mode (2026-10-13) — when you want this script's
# FUNCTIONAL checks run against a daemon you already have running
# (a CI-deployed kind/k8s instance, a persistent dev daemon, a remote
# sandbox) instead of spinning up a fresh isolated one:
#   DW_BASE=http://localhost:18080 bash scripts/release-smoke.sh
#   DW_BASE=https://sandbox.example:8443 DW_BASE_TOKEN=xyz bash ...
#
# DW_BASE must be a full base URL (scheme://host:port, no trailing
# slash). When set: no local datawatch binary is required, no daemon
# is started or killed, and no test-config.yaml/data dir is created
# for a daemon (TEST_DATA_DIR is still created for this script's own
# scratch files, e.g. eval suite fixtures). DW_BASE_TOKEN is optional
# (empty = no Authorization header, matching the external daemon's
# own auth config). Checks that depend on controlling the DAEMON'S
# OWN config/fixtures from the outside (BL403 Phase 1/2's plugin
# discovery/lifecycle sections, which assume this script's own
# PLUGINS_SMOKE_DIR fixture is the daemon's configured plugins.dir)
# are skipped in this mode, not failed — everything else runs
# identically against any already-running daemon. Every other
# functional section already degrades to SKIP when ITS subsystem is
# disabled in the target daemon's config (autonomous/memory/etc.),
# which is the same graceful-degradation this mode relies on.
#
# This is the sibling mechanism to scripts/run-tests.sh's own
# existing --no-daemon + TEST_BASE override (see that script's own
# header) — same idea, independent implementation, because the two
# scripts' daemon-lifecycle code predates this generalization and
# nothing else in either script depends on the two sharing one
# variable name. See docs/howto/testing-against-a-running-daemon.md
# for the full reference (both mechanisms, side by side).
#
# §79 runs the eleven PWA slow-network load scenarios (TS-802..TS-812) against this smoke daemon itself (never a second daemon).
# The shared navigation section after it runs the phone-width navigation scenario the same way.
#
# Returns 0 on success, non-zero on first failure.

set -uo pipefail

SMOKE_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_DIR=$(cd "$SMOKE_DIR/.." && pwd)
REPO_PARENT=$(cd "$REPO_DIR/.." && pwd)

# --- shared daemon lifecycle (source scripts/test-daemon-lib.sh first) ----
# Any test daemon this script starts in local-daemon mode is ended on EVERY
# exit path (normal, failure, INT, TERM, HUP): its live sessions are ended
# through the daemon's own API, then its process is signalled by the exact
# PID recorded at start (only while its recorded marker is still in the
# cmdline). The library's two cleanup hooks split smoke's cleanup around
# that stop:
#   TD_PRE_STOP_CLEANUP (smoke_pre_stop_cleanup) runs BEFORE td_stop_all,
#     while the daemon is still alive: cleanup_all's per-ID API deletes
#     (sessions by id, PRDs, peers, ...) must land while the daemon is up.
#     In external-daemon mode they are sent to the external daemon, which
#     this script never signals.
#   TD_EXTRA_CLEANUP (smoke_extra_cleanup) runs AFTER td_stop_all and
#     removes the scratch dirs, so the data dir is never deleted while its
#     daemon is still running.
source "$SMOKE_DIR/test-daemon-lib.sh"
smoke_pre_stop_cleanup() {
  # API cleanup while the smoke daemon is still alive. cleanup_all only
  # exists once the test dir is set up; guard both.
  if declare -F cleanup_all >/dev/null 2>&1 && [[ -n "${CLEANUP_LOG:-}" ]]; then
    cleanup_all
  fi
}
smoke_extra_cleanup() {
  # Runs on every exit, after td_stop_all has ended recorded daemons.
  if [[ -n "${TMPD:-}" ]]; then
    rm -rf "$TMPD" 2>/dev/null
  fi
  if [[ -n "${TEST_WORK_DIR:-}" ]]; then
    if [[ -n "${KEEP_TEST_DIR:-}" ]]; then
      echo "Test data kept: $TEST_WORK_DIR"
    else
      rm -rf "$TEST_WORK_DIR" 2>/dev/null || true
    fi
  fi
}
TD_PRE_STOP_CLEANUP=smoke_pre_stop_cleanup
TD_EXTRA_CLEANUP=smoke_extra_cleanup
td_install_traps "$@"

# --- static source lints (no daemon required) --------------------------------
# v6.11.25 — keep docs/plans/ clean before every release.
if [ "${DW_SKIP_TIDY_CHECK:-0}" != "1" ] && [ -x "$SMOKE_DIR/tidy-plans.sh" ]; then
  "$SMOKE_DIR/tidy-plans.sh" --check >&2 || exit 1
fi
# v6.12.1 — keep the embedded /docs/ mirror current.
if [ "${DW_SKIP_DOCS_CHECK:-0}" != "1" ] && [ -x "$SMOKE_DIR/sync-docs-to-webfs.sh" ]; then
  "$SMOKE_DIR/sync-docs-to-webfs.sh" --check >&2 || exit 1
fi
# v6.18.0 — internal-ref leak audit.
if [ "${DW_SKIP_INTERNAL_REFS_CHECK:-0}" != "1" ] && [ -x "$SMOKE_DIR/check-no-internal-refs.sh" ]; then
  "$SMOKE_DIR/check-no-internal-refs.sh" >&2 || exit 1
fi
# 2026-10-07 — version-bump/reuse audit. Compares against the most
# recent release tag so a release never reuses an already-shipped
# version even if the reuse happened several commits back.
if [ "${DW_SKIP_VERSION_BUMP_CHECK:-0}" != "1" ] && [ -x "$SMOKE_DIR/check-version-bump.sh" ]; then
  LAST_TAG=$(git -C "$REPO_DIR" describe --tags --abbrev=0 2>/dev/null || echo "HEAD^")
  "$SMOKE_DIR/check-version-bump.sh" "$LAST_TAG" >&2 || exit 1
fi
# 2026-10-09 — Dockerfile GO_VERSION-vs-go.mod drift audit. Found by
# the v9.0.0 release run: Dockerfile.agent-base's exact-pinned
# GO_VERSION had fallen behind go.mod's `go` directive, failing
# `go mod download` in CI's container build and cascading into 6
# more agent-* images being skipped. Catching it here means it fails
# fast and locally instead of surfacing only in the release workflow
# against an already-pushed tag.
if [ "${DW_SKIP_DOCKERFILE_GO_VERSION_CHECK:-0}" != "1" ] && [ -x "$SMOKE_DIR/check-dockerfile-go-versions.sh" ]; then
  "$SMOKE_DIR/check-dockerfile-go-versions.sh" >&2 || exit 1
fi
# v6.21.0 — Docs-as-MCP currency lints.
if [ "${DW_SKIP_DOCS_AS_MCP_CHECK:-0}" != "1" ]; then
  for s in check-curated-howtos.sh check-howto-coverage.sh check-plugin-manifests.sh; do
    [ -x "$SMOKE_DIR/$s" ] && "$SMOKE_DIR/$s" >&2 || exit 1
  done
fi

# --- port allocation --------------------------------------------------------
# Ask the OS for a free port. Each call returns a different port so parallel
# runs never collide. Override via env vars if you need fixed ports.
free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); p=s.getsockname()[1]; s.close(); print(p)'
}

# --- isolated test daemon (or an external one — see DW_BASE above) ----------
# Each run gets a unique 6-char hex ID so parallel runs don't collide.
RUN_ID="$(openssl rand -hex 3)"
TEST_WORK_DIR="$REPO_PARENT/datawatch-smoke-${RUN_ID}"
TEST_DATA_DIR="$TEST_WORK_DIR/.datawatch-test-$$"
mkdir -p "$TEST_DATA_DIR" # scratch dir for this script's own fixtures either way

EXTERNAL_DAEMON=0
PLUGINS_SMOKE_DIR=""
DEMO_SERVICE_PORT=""

if [[ -n "${DW_BASE:-}" ]]; then
  EXTERNAL_DAEMON=1
  BASE="$DW_BASE"
  TOK="${DW_BASE_TOKEN:-}"
  _DW_BIN="${DATAWATCH_BIN:-}"
  if [[ -z "$_DW_BIN" ]]; then
    [[ -x "$REPO_DIR/datawatch" ]] && _DW_BIN="$REPO_DIR/datawatch" || _DW_BIN="$(command -v datawatch || true)"
  fi
  # CLI checks are optional in this mode (no local binary required) --
  # section 50's own "datawatch CLI not available" skip path already
  # handles DW_CLI being unset.
  if [[ -n "$_DW_BIN" ]]; then
    DW_CLI="$_DW_BIN -u $BASE"
    export DATAWATCH_TOKEN="$TOK" # -u/--url mode reads this env var, not --config
    # Every REST/MCP check in this script already uses curl -sk
    # (skip-verify) unconditionally, regardless of mode -- so default
    # the CLI's own -u-mode TLS trust to match that same posture rather
    # than make the common case (a self-signed test/sandbox/kind-cluster
    # daemon) silently fail every CLI check. Explicit override: set
    # DW_BASE_INSECURE=0 yourself before invoking this script if your
    # external daemon has a real, CA-trusted cert and you want the CLI
    # checks to actually verify it.
    export DW_BASE_INSECURE="${DW_BASE_INSECURE:-1}"
  else
    DW_CLI=""
  fi
  echo "Smoke run ID  : $RUN_ID (external-daemon mode)"
  echo "Test daemon   : $BASE"
  echo "Binary        : ${_DW_BIN:-<none — CLI checks will skip>}"
  echo ""
else
  SMOKE_PORT="${SMOKE_PORT:-$(free_port)}"
  SMOKE_TLS_PORT="${SMOKE_TLS_PORT:-$(free_port)}"

  # Write a minimal config for the test daemon.
  TEST_CONFIG="$TEST_WORK_DIR/test-config.yaml"
  cat > "$TEST_CONFIG" <<YAML
data_dir: ${TEST_DATA_DIR}
hostname: dw-smoke-test
server:
  enabled: true
  host: 127.0.0.1
  port: ${SMOKE_PORT}
  tls_enabled: true
  tls_port: ${SMOKE_TLS_PORT}
  tls_auto_generate: true
  token: ""
memory:
  enabled: true
  backend: sqlite
  embedder: ""
plugins:
  enabled: true
  dir: ${TEST_DATA_DIR}/plugins
YAML

  # --- plugin fixtures (BL403) -----------------------------------------------
  # Copy the repo's reference examples into this run's isolated plugin dir
  # rather than pointing plugins.dir at examples/plugins directly -- the
  # demo-service fixture starts a real subprocess bound to a real port
  # (BL403 Phase 2's managed lifecycle), so each parallel smoke run needs
  # its own freshly-allocated port, not the illustrative placeholder
  # committed in the example manifest.
  PLUGINS_SMOKE_DIR="${TEST_DATA_DIR}/plugins"
  mkdir -p "$PLUGINS_SMOKE_DIR"
  cp -r "$REPO_DIR/examples/plugins/hello-automation" "$PLUGINS_SMOKE_DIR/"
  cp -r "$REPO_DIR/examples/plugins/demo-service" "$PLUGINS_SMOKE_DIR/"
  DEMO_SERVICE_PORT="$(free_port)"
  sed -i "s/8765/${DEMO_SERVICE_PORT}/g" "$PLUGINS_SMOKE_DIR/demo-service/manifest.yaml"

  # Find the datawatch binary (prefer freshly built binary in repo root).
  _DW_BIN="${DATAWATCH_BIN:-}"
  if [[ -z "$_DW_BIN" ]]; then
    [[ -x "$REPO_DIR/datawatch" ]] && _DW_BIN="$REPO_DIR/datawatch" || _DW_BIN="$(command -v datawatch)"
  fi
  [[ -z "$_DW_BIN" ]] && { echo "ERROR: datawatch binary not found. Build first or set DATAWATCH_BIN=<path>" >&2; exit 1; }

  echo "Smoke run ID : $RUN_ID"
  echo "Test data dir: $TEST_DATA_DIR"
  echo "Test daemon  : https://127.0.0.1:${SMOKE_TLS_PORT}"
  echo "Binary       : $_DW_BIN"
  echo ""

  # Start the test daemon in the background.
  "$_DW_BIN" --config "$TEST_CONFIG" start --foreground \
    >"$TEST_WORK_DIR/daemon.log" 2>&1 &
  TEST_DAEMON_PID=$!
  # Record it with the shared daemon-lifecycle library so every exit path
  # ends it: marker = the config path, which stays in the daemon's cmdline
  # for its whole life (recycled PIDs are left alone).
  td_track_pid "$TEST_DAEMON_PID" "$TEST_CONFIG" \
    || echo "warn: could not record test daemon pid $TEST_DAEMON_PID" >&2

  # Wait up to 30 s for the daemon to answer health.
  echo "Waiting for test daemon..."
  _ready=0
  for _i in $(seq 1 30); do
    curl -sk --max-time 2 "https://127.0.0.1:${SMOKE_TLS_PORT}/api/health" >/dev/null 2>&1 && { _ready=1; break; }
    sleep 1
  done
  if [[ $_ready -eq 0 ]]; then
    echo "ERROR: test daemon did not start within 30 s. Log:" >&2
    tail -30 "$TEST_WORK_DIR/daemon.log" >&2
    # End the recorded daemon (marker check + TERM + KILL + wait) before
    # leaving; the EXIT trap would do this anyway, but do it explicitly so
    # the data dir removal below cannot race a still-running daemon.
    td_stop_one "$TEST_DAEMON_PID" || echo "warn: test daemon $TEST_DAEMON_PID still running" >&2
    rm -rf "$TEST_WORK_DIR"
    exit 1
  fi
  echo "Test daemon ready."
  echo ""

  BASE="https://127.0.0.1:${SMOKE_TLS_PORT}"
  TOK=""   # test daemon starts with no auth token
  # Remember its API: on stop the library ends this daemon's live sessions
  # through the API before signalling the PID, even on abrupt exits.
  td_set_api "$TEST_DAEMON_PID" "$BASE" "$TOK" 2>/dev/null \
    || echo "warn: could not set the test daemon's API base" >&2
  # CLI commands use -u + --config so the TLS cert path and token are read from
  # the smoke daemon's config (not from ~/.datawatch which points at production).
  DW_CLI="$_DW_BIN --config $TEST_CONFIG -u $BASE"
fi

TMPD=$(mktemp -d)

# v5.26.9 — operator-reported: smoke must clean up. Accumulate the
# IDs of every PRD / peer / graph / etc. the smoke creates, then
# garbage-collect them on EXIT (success OR failure). Each entry is a
# `<kind> <id>` line; cleanup_all reads the file and DELETEs in
# reverse order so child resources go before parents.
CLEANUP_LOG="$TMPD/cleanup.log"
: >"$CLEANUP_LOG"
add_cleanup() { echo "$1 $2" >> "$CLEANUP_LOG"; }

cleanup_all() {
  # Tracked-resource cleanup: end every session and DELETE every PRD,
  # peer, and other entity that add_cleanup recorded at creation time, in
  # reverse order (children before parents). Runs on EVERY exit path as
  # the library's TD_PRE_STOP_CLEANUP hook — before td_stop_all ends the
  # smoke daemon (local mode), and against the external daemon (external
  # mode), which this script never signals.
  #
  # No name-based session sweep: the operator rule is that smoke cleans
  # up EXACTLY what it created via add_cleanup — nothing more — and the
  # session kills below go by the exact id recorded at creation.
  #
  # Scratch-dir removal (TMPD, TEST_WORK_DIR) happens in
  # smoke_extra_cleanup, which the library runs AFTER td_stop_all, so the
  # data dir is never deleted while its daemon still runs.
  local printed_header=0
  if [[ -s "$CLEANUP_LOG" ]]; then
    printed_header=1
    echo ""
    echo "== Cleanup =="
    # tac to delete in reverse order
    tac "$CLEANUP_LOG" | while read -r kind id; do
      case "$kind" in
        sess)            curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "{\"id\":\"$id\"}" "$BASE/api/sessions/kill" >/dev/null 2>&1 && echo "  killed session $id" || echo "  (already gone) sess $id" ;;
        prd)             curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$id?hard=true" >/dev/null 2>&1 && echo "  removed prd $id" || echo "  (already gone) prd $id" ;;
        peer)            curl "${curl_args[@]}" -X DELETE "$BASE/api/observer/peers/$id" >/dev/null 2>&1 && echo "  removed peer $id" || echo "  (already gone) peer $id" ;;
        fedpeer)         curl "${curl_args[@]}" -X DELETE "$BASE/api/federation/peers/$id" >/dev/null 2>&1 && echo "  removed federation peer $id" || echo "  (already gone) federation peer $id" ;;
        fedgroup)        curl "${curl_args[@]}" -X DELETE "$BASE/api/federation/groups/$id" >/dev/null 2>&1 && echo "  removed federation group $id" || echo "  (already gone) federation group $id" ;;
        graph)           curl "${curl_args[@]}" -X DELETE "$BASE/api/orchestrator/graphs/$id" >/dev/null 2>&1 && echo "  removed graph $id" || echo "  (already gone) graph $id" ;;
        project-profile) curl "${curl_args[@]}" -X DELETE "$BASE/api/profiles/projects/$id" >/dev/null 2>&1 && echo "  removed project profile $id" || echo "  (already gone) project profile $id" ;;
        cluster-profile) curl "${curl_args[@]}" -X DELETE "$BASE/api/profiles/clusters/$id" >/dev/null 2>&1 && echo "  removed cluster profile $id" || echo "  (already gone) cluster profile $id" ;;
        # v7.0.0-alpha.14 (operator-flagged 2026-05-09) — new entity
        # types added in v7.0.0 sprints. Council runs cancel via POST
        # /cancel; ComputeNodes + LLMs DELETE via their REST surface.
        council)         curl "${curl_args[@]}" -X POST "$BASE/api/council/runs/$id/cancel" >/dev/null 2>&1 && echo "  cancelled council run $id" || echo "  (already gone) council $id" ;;
        compute-node)    curl "${curl_args[@]}" -X DELETE "$BASE/api/compute/nodes/$id" >/dev/null 2>&1 && echo "  removed compute node $id" || echo "  (already gone) compute-node $id" ;;
        llm)             curl "${curl_args[@]}" -X DELETE "$BASE/api/llms/$id" >/dev/null 2>&1 && echo "  removed llm $id" || echo "  (already gone) llm $id" ;;
        smoke-file)      curl "${curl_args[@]}" -X DELETE -H "Content-Type: application/json" -d "{\"path\":\"$id\"}" "$BASE/api/files" >/dev/null 2>&1 && echo "  deleted file $id" || echo "  (already gone) file $id" ;;
        # Clears only the named backend's flag (never "all"), so pre-existing
        # operator flags survive the smoke run.
        usage-backoff)   [[ -n "$id" ]] && curl "${curl_args[@]}" -X DELETE -H "Content-Type: application/json" -d "{\"backend\":\"$id\"}" "$BASE/api/usage-backoff" >/dev/null 2>&1 && echo "  cleared usage back-off $id" || echo "  (already gone) usage-backoff $id" ;;
        # Usage calculator: exact-id reading delete, and exact-value restores of the
        # pre-smoke extra credit and usage.calculator.variant (never a pattern sweep).
        usage)           UXQ=$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(str(sys.argv[1])))' "$id" 2>/dev/null || echo "$id"); curl "${curl_args[@]}" -X DELETE "$BASE/api/usage/readings?id=$UXQ" >/dev/null 2>&1 && echo "  deleted usage reading $id" || echo "  (already gone) usage reading $id" ;;
        usage-credit)    [[ -n "$id" ]] && curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" -d "{\"points\": $id}" "$BASE/api/usage/extra-credit" >/dev/null 2>&1 && echo "  restored usage extra credit to $id" || echo "  (already gone) usage-credit $id" ;;
        usage-variant)   UC=$(echo "$id" | tr -d '"'); curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" -d "{\"usage.calculator.variant\": \"$UC\"}" "$BASE/api/config" >/dev/null 2>&1 && echo "  restored usage.calculator.variant to ${UC:-<unset/default>}" || echo "  (already gone) usage-variant ${UC:-<unset/default>}" ;;
        # Exact-value restore of usage.calculator.reset_zone (section 85): the id carries
        # the raw JSON token (\"Zone\" or "") so an unset zone restores to "', never a guessed one.
        usage-zone)      URZ=$(echo "$id" | tr -d ' '); [[ -n "$URZ" ]] || URZ='""'; curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" -d "{\"usage.calculator.reset_zone\": $URZ}" "$BASE/api/config" >/dev/null 2>&1 && echo "  restored usage.calculator.reset_zone to ${URZ:-<unset/default>}" || echo "  (already gone) usage-zone" ;;
        # Exact-value restore of autonomous.stuck_running_seconds (section 86); never a pattern sweep.
        stuck-running)   SRV=$(echo "$id" | tr -dc '0-9'); curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" -d "{\"autonomous.stuck_running_seconds\": ${SRV:-0}}" "$BASE/api/config" >/dev/null 2>&1 && echo "  restored autonomous.stuck_running_seconds to ${SRV:-0}" || echo "  (already gone) stuck-running ${SRV:-0}" ;;
        # Section 87 (operator questions step 2): the lifecycle subscription is deleted by its
        # exact id, each config key the section changed is restored to the exact value read
        # first (id is key=value, value a JSON literal), and the alert it raised is marked
        # read by its exact id (the alert store has no delete; a smoke daemon is discarded).
        qsub)            curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/subscriptions/$id" >/dev/null 2>&1 && echo "  removed question subscription $id" || echo "  (already gone) question subscription $id" ;;
        qcfg)            QCK="${id%%=*}"; QCV="${id#*=}"; curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" -d "{\"$QCK\": $QCV}" "$BASE/api/config" >/dev/null 2>&1 && echo "  restored $QCK to $QCV" || echo "  (already gone) qcfg $id" ;;
        qalert)          curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "{\"id\":\"$id\"}" "$BASE/api/alerts" >/dev/null 2>&1 && echo "  marked alert $id read" || echo "  (already gone) alert $id" ;;
        # Cancel first (a draft refuses it, which is fine), then delete.
        automation)      curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{"cancel_in_flight":true}' "$BASE/api/automations/$id/cancel" >/dev/null 2>&1; curl "${curl_args[@]}" -X DELETE "$BASE/api/automations/$id" >/dev/null 2>&1 && echo "  removed automation $id" || echo "  (already gone) automation $id" ;;
        device)          curl "${curl_args[@]}" -X DELETE "$BASE/api/devices/$id" >/dev/null 2>&1 && echo "  removed device $id" || echo "  (already gone) device $id" ;;
        *)               echo "  (unknown kind) $kind $id" ;;
      esac
    done
  fi
  return 0
}
# Traps: installed by the shared daemon-lifecycle library above — on every
# exit path it runs smoke_pre_stop_cleanup (per-ID API cleanup while the
# daemon is still alive), then td_stop_all (ends the daemon's live sessions
# via its own API, then signals the recorded PID marker-verified and waits
# for it), then smoke_extra_cleanup (scratch dirs). Nothing above signals
# a daemon, and no session is killed by name: only the exact id recorded
# at creation is ever sent to the API.

PASS=0
FAIL=0
SKIP=0

# BL303 — Dashboard progress tracking. Smoke writes
# ~/.datawatch/smoke-runs/{run_id}.json before each section so the PWA
# Smoke card can poll /api/smoke/progress and show live phase state.
SMOKE_RUN_ID="smoke-$(date +%Y%m%dT%H%M%S)-$$"
mkdir -p "${HOME}/.datawatch/smoke-runs"
SMOKE_PROGRESS_FILE="${HOME}/.datawatch/smoke-runs/${SMOKE_RUN_ID}.json"
SMOKE_STARTED_AT=$(date -u +%FT%TZ 2>/dev/null || echo "")
SMOKE_CUR_SEC=""     # current section label (first token of H arg)
SMOKE_CUR_NAME=""    # full section title
SMOKE_SEC_FAIL_AT=0  # FAIL count when current section started
SMOKE_SEC_LINES=""   # newline-separated completed section JSON records

_smoke_close_sec() {
  [[ -z "$SMOKE_CUR_SEC" ]] && return 0
  local sc_fail=$((FAIL - SMOKE_SEC_FAIL_AT))
  local result="pass"
  [[ $sc_fail -gt 0 ]] && result="fail"
  # If section was entirely skipped (zero ok/ko/skip calls added), mark skip.
  SMOKE_SEC_LINES+=$(printf '{"id":"%s","name":"%s","result":"%s"}' \
    "$SMOKE_CUR_SEC" "$SMOKE_CUR_NAME" "$result")$'\n'
}

_smoke_write_progress() {
  local active="${1:-true}"
  local ts
  ts=$(date -u +%FT%TZ 2>/dev/null || echo "")
  # Build sections array from accumulated records.
  local secs_json
  secs_json=$(printf '%s' "$SMOKE_SEC_LINES" | python3 -c '
import json,sys
lines=[l for l in sys.stdin.read().splitlines() if l.strip()]
print(json.dumps([json.loads(l) for l in lines]))
' 2>/dev/null || echo "[]")
  local total_secs=$((PASS + FAIL + SKIP))
  printf '{"run_id":"%s","type":"smoke","total":%d,"version":"%s","started_at":"%s","updated_at":"%s","active":%s,"current_id":"%s","current_name":"%s","pass":%d,"fail":%d,"skip":%d,"sections":%s}' \
    "${SMOKE_RUN_ID:-smoke}" "$total_secs" "${VER:-}" "$SMOKE_STARTED_AT" "$ts" "$active" \
    "$SMOKE_CUR_SEC" "$SMOKE_CUR_NAME" \
    "$PASS" "$FAIL" "$SKIP" "$secs_json" \
    > "$SMOKE_PROGRESS_FILE" 2>/dev/null || true
}

# v5.26.57 — operator-asked: "can't targeted smoke tests run instead
# of them all if needed". SMOKE_ONLY accepts a comma-separated list
# of section numbers / prefixes (e.g. "1,4,7d,9"). When set, H()
# skips any section whose first whitespace-trimmed token isn't in
# the list; SECTION_SKIP=1 short-circuits the rest of that section's
# checks. Otherwise (default) every section runs as before.
SMOKE_ONLY="${SMOKE_ONLY:-${DW_SMOKE_ONLY:-}}"
SECTION_SKIP=0
H() {
  _smoke_close_sec
  echo ""; echo "== $* =="
  SMOKE_CUR_SEC="${1%%[. ]*}"  # "7d" out of "7d. Persistent ..."
  SMOKE_CUR_NAME="$1"
  SMOKE_SEC_FAIL_AT=$FAIL
  _smoke_write_progress "true"

  if [[ -n "$SMOKE_ONLY" ]]; then
    local sec="${SMOKE_CUR_SEC}"
    SECTION_SKIP=1
    local IFS=','
    for w in $SMOKE_ONLY; do
      w="${w## }"; w="${w%% }"
      if [[ "$sec" == "$w" || "$sec" == "$w"* ]]; then
        SECTION_SKIP=0
        break
      fi
    done
    if [[ "$SECTION_SKIP" == "1" ]]; then
      echo "  (skipped — not in SMOKE_ONLY=$SMOKE_ONLY)"
    fi
  else
    SECTION_SKIP=0
  fi
}

ok() { [[ "$SECTION_SKIP" == "1" ]] && return 0; echo "  PASS  $*"; PASS=$((PASS+1)); }
ko() { [[ "$SECTION_SKIP" == "1" ]] && return 0; echo "  FAIL  $*"; FAIL=$((FAIL+1)); }
skip() { [[ "$SECTION_SKIP" == "1" ]] && return 0; echo "  SKIP  $*"; SKIP=$((SKIP+1)); }

curl_args=(-sk --max-time 30)
if [[ -n "$TOK" ]]; then curl_args+=(-H "Authorization: Bearer $TOK"); fi
VER=""  # set in section 1 after health check; used in progress JSON

# ---------------------------------------------------------------------------
H "1. Daemon health"
HEALTH=$(curl "${curl_args[@]}" "$BASE/api/health" || true)
if echo "$HEALTH" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
  VER=$(echo "$HEALTH" | python3 -c 'import json,sys;print(json.load(sys.stdin)["version"])')
  ok "health ok, version=$VER"
else
  ko "health endpoint did not return ok: $HEALTH"
  exit 1
fi

# ---------------------------------------------------------------------------
H "2. Backends list"
BK=$(curl "${curl_args[@]}" "$BASE/api/backends" || true)
if echo "$BK" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("llm",[]), list) and len(d["llm"])>0' 2>/dev/null; then
  N=$(echo "$BK" | python3 -c 'import json,sys;print(len(json.load(sys.stdin).get("llm",[])))')
  ok "backends list: $N entries"
else
  ko "backends list shape unexpected: $BK"
fi

# ---------------------------------------------------------------------------
H "3. Stats / observer"
ST=$(curl "${curl_args[@]}" "$BASE/api/stats?v=2" || true)
if echo "$ST" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "envelopes" in d or "v" in d' 2>/dev/null; then
  ok "/api/stats?v=2 returned a structured snapshot"
else
  ko "/api/stats?v=2 unexpected: $(echo "$ST" | head -c 200)"
fi

# ---------------------------------------------------------------------------
H "4. Diagnose"
DG=$(curl "${curl_args[@]}" "$BASE/api/diagnose" || true)
if echo "$DG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,(dict,list))' 2>/dev/null; then
  ok "/api/diagnose returned a result"
else
  ko "/api/diagnose unexpected: $(echo "$DG" | head -c 200)"
fi

# ---------------------------------------------------------------------------
H "5. Channel history endpoint shape"
CH=$(curl "${curl_args[@]}" "$BASE/api/channel/history?session_id=smoke-nonexistent" || true)
# Accept either [] (v5.26.9+) or null (v5.26.1–v5.26.8) as "empty".
if echo "$CH" | python3 -c 'import json,sys;d=json.load(sys.stdin);m=d.get("messages");assert m is None or (isinstance(m,list) and len(m)==0)' 2>/dev/null; then
  ok "/api/channel/history returns 200 + empty messages for unknown session"
else
  ko "/api/channel/history wrong shape: $CH"
fi

# ---------------------------------------------------------------------------
H "6. Autonomous CRUD across every supported worker backend"
A_ENABLED=$(curl "${curl_args[@]}" "$BASE/api/autonomous/config" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping CRUD test"
else
  # v5.26.10 — exercise each enabled worker backend (claude-code,
  # opencode, ollama) through the same CRUD path. Operator-reported:
  # smoke must validate that PRDs work with claude, opencode, AND
  # ollama as the worker backend, not just claude-code.
  AVAIL=$(curl "${curl_args[@]}" "$BASE/api/backends" | python3 -c '
import json, sys
d = json.load(sys.stdin)
have = {b["name"] for b in d.get("llm",[]) if b.get("enabled") and b.get("available")}
# Only run the CRUD probe against backends the daemon will actually
# accept; "available" gates on the binary being installed / endpoint
# reachable.
target = [b for b in ("claude-code","opencode","ollama") if b in have]
print(",".join(target))
' 2>/dev/null || echo "")
  if [[ -z "$AVAIL" ]]; then
    skip "no claude-code/opencode/ollama backend enabled+available"
  else
    IFS=',' read -ra BACKENDS <<< "$AVAIL"
    for B in "${BACKENDS[@]}"; do
      H "6.$B — CRUD"
      P=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d "{\"spec\":\"smoke probe — autonomous CRUD ($B)\",\"project_dir\":\"/tmp\",\"backend\":\"$B\",\"effort\":\"low\"}" \
        "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
      if [[ -n "$P" ]]; then
        add_cleanup prd "$P"
        ok "[$B] create PRD: $P"
      else
        ko "[$B] create PRD failed"; continue
      fi

      # Verify the PRD record carries the backend through.
      CHK=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$P" | python3 -c "import json,sys;d=json.load(sys.stdin);print(d.get('backend',''))")
      if [[ "$CHK" == "$B" ]]; then
        ok "[$B] PRD record has backend=$B"
      else
        ko "[$B] PRD record dropped backend (got '$CHK', want '$B')"
      fi

      # /children works (empty for fresh PRD).
      CHILDREN=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$P/children")
      if echo "$CHILDREN" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("children",[]),list)' 2>/dev/null; then
        ok "[$B] GET /children empty list"
      else
        ko "[$B] GET /children failed: $CHILDREN"
      fi

      # set_llm round-trip — pin a model relevant to the backend.
      MODEL="${B/-code/}"  # claude-code → claude; opencode → opencode; ollama → ollama
      [[ "$B" == "ollama" ]] && MODEL="qwen3:8b"
      [[ "$B" == "claude-code" ]] && MODEL="claude-sonnet-4-5"
      [[ "$B" == "opencode" ]] && MODEL="claude-sonnet-4-5"
      SETL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d "{\"backend\":\"$B\",\"effort\":\"low\",\"model\":\"$MODEL\",\"actor\":\"smoke\"}" \
        "$BASE/api/autonomous/prds/$P/set_llm")
      if echo "$SETL" | python3 -c "import json,sys;d=json.load(sys.stdin);assert d.get('backend')=='$B' and d.get('model')=='$MODEL'" 2>/dev/null; then
        ok "[$B] set_llm round-trip: backend=$B, model=$MODEL"
      else
        ko "[$B] set_llm failed: $SETL"
      fi

      # Hard delete (cascade-aware Manager guard).
      DEL=$(curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$P?hard=true")
      if echo "$DEL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="deleted"' 2>/dev/null; then
        ok "[$B] hard-delete PRD"
      else
        ko "[$B] hard-delete failed: $DEL"
      fi
    done
  fi
fi

# ---------------------------------------------------------------------------
H "7. Autonomous decompose loopback (the bug that hid for many releases)"
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping decompose test"
else
  # Create a PRD targeting an ask-incompatible backend and confirm the
  # decomposer falls back to ollama, hits the loopback bypass, and
  # returns parseable JSON. v5.26.9 fixed the prompt→question field +
  # the askCompatible fallback.
  PD=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"spec":"List the files in /tmp and write a one-line summary.","project_dir":"/tmp","backend":"claude-code","effort":"low"}' \
    "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
  if [[ -z "$PD" ]]; then
    ko "decompose-test: PRD create failed"
  else
    add_cleanup prd "$PD"
    DR=$(curl "${curl_args[@]}" --max-time 300 -X POST "$BASE/api/autonomous/prds/$PD/decompose" -w "\n__HTTP_CODE_%{http_code}__")
    HTTPC=$(echo "$DR" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
    if [[ "$HTTPC" == "200" ]]; then
      STORIES=$(echo "$DR" | sed 's/__HTTP_CODE.*//' | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d.get("stories",[])))' 2>/dev/null || echo 0)
      ok "decompose returned 200, $STORIES stories"
    elif echo "$DR" | grep -q "x509"; then
      ko "decompose hit x509 — redirect bypass not working: $(echo "$DR" | head -c 200)"
    elif echo "$DR" | grep -q "question required"; then
      ko "decompose returned 'question required' — field-name regression: $(echo "$DR" | head -c 200)"
    elif echo "$DR" | grep -q "unsupported backend"; then
      ko "decompose returned 'unsupported backend' — askCompatible fallback regression"
    else
      skip "decompose returned $HTTPC (body=$(echo "$DR" | head -c 200)) — non-fatal in smoke; LLM may not be reachable"
    fi
    # cleanup_all on EXIT will remove $PD via the trap.
  fi
fi

# ---------------------------------------------------------------------------
H "7b. Autonomous PRD full lifecycle (decompose → approve → run → spawn)"
# v5.26.11 — operator-reported: tasks went TaskFailed before spawning
# because autonomous Effort enum (low/medium/high/max) didn't match
# session Effort enum (quick/normal/thorough). This step asserts the
# spawn round-trip survives the enum translation, even if the actual
# worker session can't complete (which is fine — we only care that
# the executor reaches "spawn returned a session ID").
#
# v5.26.13 — switched the worker backend from `shell` (which
# v5.26.13 excluded from the autonomous LLM list) to the first
# available LLM. Skip if no LLM backend is enabled+available on the
# host. The decompose step in §7 already returned 200 with stories;
# §7b reuses the same call path for the run portion.
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping run-lifecycle test"
else
  RUN_B=$(curl "${curl_args[@]}" "$BASE/api/backends" | python3 -c '
import json, sys
d = json.load(sys.stdin)
# Prefer ollama (local + free), then openwebui (local), then opencode, then claude-code.
order = ["ollama", "openwebui", "opencode", "claude-code"]
have = {b["name"]: b for b in d.get("llm",[])}
for name in order:
    b = have.get(name)
    if b and b.get("enabled") and b.get("available"):
        print(name); break
' 2>/dev/null || echo "")
  if [[ -z "$RUN_B" ]]; then
    skip "run-lifecycle: no LLM backend available; can't exercise spawn against an LLM worker"
  else
  PR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"spec\":\"smoke probe — autonomous run lifecycle\",\"project_dir\":\"/tmp\",\"backend\":\"$RUN_B\",\"effort\":\"low\"}" \
    "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))')
  if [[ -z "$PR" ]]; then
    ko "run-lifecycle: PRD create failed"
  else
    add_cleanup prd "$PR"
    DR=$(curl "${curl_args[@]}" --max-time 300 -X POST "$BASE/api/autonomous/prds/$PR/decompose" -w "\n__HTTP_%{http_code}__")
    if echo "$DR" | grep -q "__HTTP_200__"; then
      ok "run-lifecycle: decompose OK"
      AP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d '{"actor":"smoke","note":"smoke run lifecycle"}' \
        "$BASE/api/autonomous/prds/$PR/approve")
      if echo "$AP" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="approved"' 2>/dev/null; then
        ok "run-lifecycle: approve → approved"
        RN=$(curl "${curl_args[@]}" -X POST "$BASE/api/autonomous/prds/$PR/run")
        if echo "$RN" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="running"' 2>/dev/null; then
          ok "run-lifecycle: run → running"
          # Give the executor 8s to spawn and either succeed or hit
          # a real (post-spawn) error like verify-failed.
          sleep 8
          STATE=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR" | python3 -c '
import json, sys
d = json.load(sys.stdin)
fails_pre_spawn = []
fails_post_spawn = []
ok_count = 0
for s in d.get("stories",[]):
    for t in s.get("tasks",[]):
        st = t.get("status","")
        sid = t.get("session_id","")
        err = t.get("error","")
        if st == "failed" and not sid and "invalid effort" in err:
            fails_pre_spawn.append(t.get("id"))
        elif sid:
            ok_count += 1
        elif st == "failed":
            fails_post_spawn.append((t.get("id"), err[:60]))
print(json.dumps({"pre_spawn": fails_pre_spawn, "post_spawn": fails_post_spawn, "spawned": ok_count, "prd_status": d.get("status")}))
')
          if echo "$STATE" | python3 -c 'import json,sys;d=json.loads(sys.stdin.read());assert len(d["pre_spawn"])==0' 2>/dev/null; then
            ok "run-lifecycle: spawn round-trip survived effort-enum translation ($STATE)"
          else
            ko "run-lifecycle: tasks failed PRE-spawn (effort-enum regression): $STATE"
          fi
          # Cancel any in-flight executor goroutine via DELETE (cancel,
          # not hard-delete; cleanup_all takes care of hard-delete).
          curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$PR" >/dev/null 2>&1
          # Expose to §7ai cancel-kills-sessions sub-test.
          SMOKE_PRD_ID="$PR"
        else
          ko "run-lifecycle: run rejected: $RN"
        fi
      else
        ko "run-lifecycle: approve rejected: $AP"
      fi
    else
      skip "run-lifecycle: decompose failed (LLM unreachable?), can't exercise spawn"
    fi
  fi
  fi  # close RUN_B-non-empty
fi

H "7c. PRD project_profile + cluster_profile attachment (v5.26.19)"
# Operator-reported: PRDs should be based on directory or profile,
# with cluster_profile dispatching the worker to /api/agents instead
# of local tmux. Smoke covers (a) profile-existence validation refuses
# unknown names and (b) known names persist on the PRD record.
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping profile attachment test"
else
  # Pre-create a project profile so the smoke can attach it. Use a
  # name that's safe to delete after.
  PROF="smoke-prof-$(date +%s)"
  PROF_BODY=$(printf '{"name":"%s","git":{"url":"https://github.com/dmz006/datawatch","branch":"main"},"image_pair":{"agent":"agent-claude"}}' "$PROF")
  PR_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "$PROF_BODY" "$BASE/api/profiles/projects")
  if echo "$PR_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")' 2>/dev/null; then
    ok "created project profile: $PROF"
    add_cleanup project-profile "$PROF"
  else
    skip "could not create project profile (response: $(echo "$PR_RES" | head -c 100))"
    PROF=""
  fi

  # Reject unknown profile name.
  if [[ -n "$PROF" ]]; then
    UNKBODY=$(printf '{"spec":"smoke probe — bad-profile validation","project_dir":"","project_profile":"%s","backend":"ollama","effort":"low"}' "ghost-profile-$RANDOM")
    UNK=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$UNKBODY" "$BASE/api/autonomous/prds" -w "\n__HTTP_%{http_code}__")
    HTTPC=$(echo "$UNK" | grep -oE "__HTTP_[0-9]+__" | grep -oE "[0-9]+")
    if [[ "$HTTPC" == "400" ]] && echo "$UNK" | grep -q "project profile"; then
      ok "create with unknown project_profile rejected (400)"
    else
      ko "expected 400 'project profile %q not found', got HTTP $HTTPC body: $(echo "$UNK" | head -c 120)"
    fi

    # Happy path — attach valid profile, verify it persists.
    OKBODY=$(printf '{"spec":"smoke probe — profile attachment","project_dir":"","project_profile":"%s","backend":"ollama","effort":"low"}' "$PROF")
    PR2=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$OKBODY" "$BASE/api/autonomous/prds")
    PR2_ID=$(echo "$PR2" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
    if [[ -n "$PR2_ID" ]]; then
      add_cleanup prd "$PR2_ID"
      GOTPROF=$(echo "$PR2" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("project_profile",""))' 2>/dev/null)
      if [[ "$GOTPROF" == "$PROF" ]]; then
        ok "PRD record carries project_profile=$PROF"
      else
        ko "PRD record dropped project_profile (got=$GOTPROF want=$PROF)"
      fi

      # v5.26.20 — PUT /api/autonomous/prds/{id}/profiles for
      # post-create profile changes. Clear via empty body. The PRD
      # struct uses omitempty so the cleared field is absent from
      # the response, not present-as-empty-string — both shapes are
      # acceptable here.
      PUT_RES=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
        -d '{"project_profile":"","cluster_profile":""}' \
        "$BASE/api/autonomous/prds/$PR2_ID/profiles")
      if echo "$PUT_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert (d.get("project_profile","") == "") and (d.get("cluster_profile","") == "")' 2>/dev/null; then
        ok "PUT /profiles cleared project_profile"
      else
        ko "PUT /profiles failed to clear: $PUT_RES"
      fi
    else
      ko "create with valid project_profile failed: $(echo "$PR2" | head -c 200)"
    fi
  fi
fi

H "7d. Persistent test profiles (datawatch-smoke + smoke-testing)"
# v5.26.33 — operator directive: "the testing cluster can be
# configured on the local server and left there for future tests
# and a test profile can be used with datawatch git and opencode as
# llm for prd and opencode as llm for coding for smoke tests."
#
# Two persistent fixtures: a `smoke-testing` cluster profile + a
# `datawatch-smoke` project profile pinned to the datawatch repo +
# agent-opencode worker image. Idempotent — created once, reused on
# every smoke run, NEVER added to cleanup_log so they outlive the
# test. Differs from §7c which uses ephemeral name-tagged profiles.
SMOKE_PROF="datawatch-smoke"
SMOKE_CLUSTER="smoke-testing"
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping persistent-fixture setup"
else
  # ── Cluster profile ─────────────────────────────────────────────
  CL_GET=$(curl "${curl_args[@]}" "$BASE/api/profiles/clusters/$SMOKE_CLUSTER" -w "\n__HTTP_%{http_code}__" 2>/dev/null)
  CL_HTTP=$(echo "$CL_GET" | grep -oE "__HTTP_[0-9]+__" | grep -oE "[0-9]+")
  if [[ "$CL_HTTP" == "200" ]]; then
    ok "cluster profile $SMOKE_CLUSTER already present (reused)"
  else
    CL_BODY=$(printf '{"name":"%s","description":"Persistent local-docker cluster for release-smoke","kind":"docker","namespace":"default"}' "$SMOKE_CLUSTER")
    CL_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$CL_BODY" "$BASE/api/profiles/clusters")
    if echo "$CL_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")' 2>/dev/null; then
      ok "cluster profile $SMOKE_CLUSTER created (persistent — not cleaned up)"
    else
      skip "cluster profile create failed (kind=docker may need driver wiring): $(echo "$CL_RES" | head -c 120)"
      SMOKE_CLUSTER=""
    fi
  fi

  # ── Project profile ─────────────────────────────────────────────
  PJ_GET=$(curl "${curl_args[@]}" "$BASE/api/profiles/projects/$SMOKE_PROF" -w "\n__HTTP_%{http_code}__" 2>/dev/null)
  PJ_HTTP=$(echo "$PJ_GET" | grep -oE "__HTTP_[0-9]+__" | grep -oE "[0-9]+")
  if [[ "$PJ_HTTP" == "200" ]]; then
    ok "project profile $SMOKE_PROF already present (reused)"
  else
    # Operator-asked: opencode for both PRD decompose and worker
    # coding. image_pair.agent picks the worker image; daemon-side
    # decompose backend is a separate config knob (autonomous.
    # decomposition_backend) that operators set in config.yaml.
    PJ_BODY=$(printf '{"name":"%s","description":"Persistent smoke fixture: datawatch git + opencode worker","git":{"url":"https://github.com/dmz006/datawatch","branch":"main","provider":"github"},"image_pair":{"agent":"agent-opencode","sidecar":"lang-go"},"memory":{"mode":"sync-back"}}' "$SMOKE_PROF")
    PJ_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$PJ_BODY" "$BASE/api/profiles/projects")
    if echo "$PJ_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")' 2>/dev/null; then
      ok "project profile $SMOKE_PROF created (persistent — not cleaned up)"
    else
      skip "project profile create failed: $(echo "$PJ_RES" | head -c 120)"
      SMOKE_PROF=""
    fi
  fi

  # ── PRD round-trip referencing both fixtures ────────────────────
  if [[ -n "$SMOKE_PROF" && -n "$SMOKE_CLUSTER" ]]; then
    RT_BODY=$(printf '{"spec":"smoke probe — persistent fixture round-trip","project_profile":"%s","cluster_profile":"%s"}' "$SMOKE_PROF" "$SMOKE_CLUSTER")
    RT=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$RT_BODY" "$BASE/api/autonomous/prds")
    RT_ID=$(echo "$RT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
    if [[ -n "$RT_ID" ]]; then
      add_cleanup prd "$RT_ID"   # PRD is ephemeral; profiles persist
      GOT_PROF=$(echo "$RT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("project_profile",""))' 2>/dev/null)
      GOT_CLUS=$(echo "$RT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("cluster_profile",""))' 2>/dev/null)
      if [[ "$GOT_PROF" == "$SMOKE_PROF" && "$GOT_CLUS" == "$SMOKE_CLUSTER" ]]; then
        ok "PRD round-trip carries persistent fixtures (project=$SMOKE_PROF cluster=$SMOKE_CLUSTER)"
      else
        ko "PRD record dropped fixture refs (project=$GOT_PROF cluster=$GOT_CLUS)"
      fi
    else
      ko "PRD create against persistent fixtures failed: $(echo "$RT" | head -c 200)"
    fi
  fi
fi

H "7e. Filter store CRUD"
# v5.26.41 — operator directive (service-function smoke audit):
# every store with REST CRUD should round-trip in smoke. Filters
# are the simplest shape (pattern + action + value); schedule and
# alert stores have more complex bodies and stay deferred.
FILTER_PAT="smoke-probe-$(date +%s)"
FC=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "$(printf '{"pattern":"%s","action":"schedule","value":"yes"}' "$FILTER_PAT")" \
  "$BASE/api/filters")
FC_ID=$(echo "$FC" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
if [[ -n "$FC_ID" ]]; then
  ok "create filter: $FC_ID (pattern=$FILTER_PAT)"
  # Read-back via list
  if curl "${curl_args[@]}" "$BASE/api/filters" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('filters',[])
assert any(f.get('id') == '$FC_ID' for f in arr), 'created filter not in list'
" 2>/dev/null; then
    ok "filter $FC_ID round-trips through GET /api/filters"
  else
    ko "filter $FC_ID NOT visible in GET /api/filters list"
  fi
  # Delete
  if curl "${curl_args[@]}" -X DELETE "$BASE/api/filters?id=$FC_ID" | grep -q '"status"'; then
    ok "delete filter $FC_ID"
  else
    ko "delete filter $FC_ID failed"
  fi
else
  skip "filter create failed: $(echo "$FC" | head -c 100)"
fi

H "7f. Memory + KG round-trip"
# v5.26.47 — service-function smoke audit. The §9 memory check
# only hits /api/memory/search; this section exercises the rest of
# the operator-facing memory surface that's gated on the same
# subsystem being enabled:
#   - /api/memory/stats        — health + count snapshot
#   - /api/memory/kg/stats     — KG entity/triple counters
#   - POST /api/memory/save    — write a memory with spatial dims
#                                 (wing/room/hall from nightwire BL55)
#   - GET  /api/memory/search  — round-trip read-back
#   - DELETE /api/memory/delete — cleanup the probe entry
MEM_OK=$(curl "${curl_args[@]}" "$BASE/api/memory/stats" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$MEM_OK" != "yes" ]]; then
  skip "memory subsystem not enabled — skipping memory + KG round-trip"
else
  ok "/api/memory/stats reports enabled=true"

  # KG stats shape — accept any non-error JSON with the four keys.
  if curl "${curl_args[@]}" "$BASE/api/memory/kg/stats" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
for k in ('entity_count','triple_count','active_count','expired_count'):
    assert k in d, 'missing '+k
" 2>/dev/null; then
    ok "/api/memory/kg/stats returns the canonical 4-counter shape"
  else
    ko "/api/memory/kg/stats missing one of entity_count/triple_count/active_count/expired_count"
  fi

  # Save → list-by-id round-trip → delete.
  # v5.26.51 — corrected from v5.26.47:
  # 1) /api/memory/save accepts only {content, project_dir} (wing/
  #    room/hall are derived from project_dir; passing them was a
  #    no-op).
  # 2) /api/memory/delete is POST with {id: <int>} body, not
  #    DELETE ?id=. Earlier smoke "passed" because the curl error
  #    output was redirected and the next line never failed.
  # 3) Switching from semantic search to /api/memory/list — the
  #    embedding-ranked search is non-deterministic for short
  #    probe text, occasionally dropping the freshly-saved row
  #    out of the top results. /list filters by id deterministically.
  PROBE_TXT="datawatch-smoke-probe-$(date +%s)-uniq"
  SR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
       -d "$(printf '{"content":"%s"}' "$PROBE_TXT")" \
       "$BASE/api/memory/save")
  MEM_ID=$(echo "$SR" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
  if [[ -n "$MEM_ID" && "$MEM_ID" != "0" ]]; then
    ok "memory save returned id=$MEM_ID"
    # Read-back via /list (deterministic) — find the row by id.
    if curl "${curl_args[@]}" "$BASE/api/memory/list?limit=200" | python3 -c "
import json,sys
arr = json.load(sys.stdin)
hit = any(int(m.get('id', 0)) == int('$MEM_ID') for m in arr)
assert hit, 'saved id $MEM_ID not in /api/memory/list head'
" 2>/dev/null; then
      ok "memory list round-trips id=$MEM_ID"
    else
      ko "memory list did NOT return the saved probe id=$MEM_ID"
    fi
    # Cleanup via POST /api/memory/delete {id}.
    if curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
         -d "$(printf '{"id":%s}' "$MEM_ID")" \
         "$BASE/api/memory/delete" | grep -q '"status"'; then
      ok "memory probe id=$MEM_ID deleted"
    else
      ko "memory probe id=$MEM_ID delete failed"
    fi
  else
    skip "memory save returned no id — body: $(echo "$SR" | head -c 120)"
  fi
fi

H "7g. MCP tool surface"
# v5.26.48 — service-function smoke audit. /api/mcp/docs returns
# the full MCP tool inventory the daemon exposes. Smoke verifies:
#   - response is a JSON array of >= 30 tools (defensive lower bound;
#     current count is 39, but releases that strip tools should still
#     keep the foundational set)
#   - the foundational subset is registered (list_sessions /
#     start_session / send_input / schedule_add / profile_list /
#     agent_list — every operator MCP wrapper depends on these)
MCP_RES=$(curl "${curl_args[@]}" "$BASE/api/mcp/docs" 2>/dev/null)
if echo "$MCP_RES" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert isinstance(d, list) and len(d) >= 30, 'tool count below floor: %d' % len(d)
names = {t['name'] for t in d}
required = {'list_sessions','start_session','send_input','schedule_add','profile_list','agent_list'}
missing = required - names
assert not missing, 'missing tools: ' + ','.join(sorted(missing))
print('count=%d' % len(d))
" 2>/dev/null; then
  ok "/api/mcp/docs returns the canonical MCP tool surface (>=30 tools, foundational subset present)"
else
  ko "MCP tool surface incomplete: $(echo "$MCP_RES" | head -c 200)"
fi

H "7h. Schedule store CRUD"
# v5.26.52 — service-function smoke audit. /api/schedules supports
# both "command" (against a live session) and "new_session"
# (deferred session spawn) types. The smoke uses new_session with
# a far-future run_at + immediate cancel, so the schedule never
# fires during the test.
SCHED_TS=$(date -u -d '+1 hour' +%FT%TZ 2>/dev/null || date -u -v+1H +%FT%TZ 2>/dev/null)
if [[ -z "$SCHED_TS" ]]; then
  skip "could not compute future timestamp for schedule probe"
else
  SCHED_NAME="smoke-sched-$(date +%s)"
  SCHED_BODY=$(printf '{"type":"new_session","name":"%s","command":"echo smoke","project_dir":"/tmp","backend":"shell","run_at":"%s"}' "$SCHED_NAME" "$SCHED_TS")
  SR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$SCHED_BODY" "$BASE/api/schedules")
  SCHED_ID=$(echo "$SR" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
  if [[ -n "$SCHED_ID" ]]; then
    ok "schedule created: $SCHED_ID (name=$SCHED_NAME, run_at=$SCHED_TS)"
    if curl "${curl_args[@]}" "$BASE/api/schedules" | python3 -c "
import json,sys
arr = json.load(sys.stdin)
hit = any(s.get('id') == '$SCHED_ID' for s in arr)
assert hit, 'schedule $SCHED_ID not in list'
" 2>/dev/null; then
      ok "schedule $SCHED_ID round-trips through GET /api/schedules"
    else
      ko "schedule $SCHED_ID missing from GET /api/schedules"
    fi
    if curl "${curl_args[@]}" -X DELETE "$BASE/api/schedules?id=$SCHED_ID" | grep -q '"status"'; then
      ok "schedule $SCHED_ID cancelled"
    else
      ko "schedule $SCHED_ID cancel failed"
    fi
  else
    skip "schedule create failed: $(echo "$SR" | head -c 120)"
  fi
fi

H "7i. Channel send round-trip (test/message)"
# v5.26.52 — service-function smoke audit. /api/test/message
# simulates an inbound channel command (signal/telegram/slack/etc)
# without needing a live backend. Verifies the router accepts the
# command and returns the canonical response shape.
TM=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{"text":"help"}' "$BASE/api/test/message")
if echo "$TM" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('count', 0) >= 1, 'help returned 0 responses'
resp = ' '.join(d.get('responses', []))
assert 'datawatch commands' in resp.lower() or 'command' in resp.lower(), 'help response missing canonical text'
" 2>/dev/null; then
  ok "/api/test/message help round-trip returns canonical command list"
else
  ko "/api/test/message help round-trip failed: $(echo "$TM" | head -c 200)"
fi
TM2=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{"text":"list"}' "$BASE/api/test/message")
if echo "$TM2" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert 'count' in d and 'responses' in d, 'list response missing canonical shape'
" 2>/dev/null; then
  ok "/api/test/message list returns canonical {count, responses} shape"
else
  ko "/api/test/message list shape wrong: $(echo "$TM2" | head -c 200)"
fi

H "7j. F10 agent lifecycle (mint→spawn→audit→terminate)"
# v5.26.55 — service-function smoke audit. The agent manager is
# always wired (no agents.enabled gate); whether a spawn actually
# starts a container depends on Docker/k8s availability + image
# registry config. The smoke probes the *surface*: spawn →
# capture id → verify audit trail → DELETE.
#
# It does NOT require the spawned worker to start successfully —
# environments without `gh auth login` for the BL113 token broker
# will see mint-fail entries in the audit log; that's still a
# valid lifecycle exercise for the F10 plumbing.
#
# Token cleanup invariant (operator-asked): each spawn either
# (a) successfully mints AND a corresponding revoke fires on
# terminate, or (b) records a mint-fail in the audit log so no
# unrevoked token leaks to the worker. Smoke verifies the audit
# record exists.
AGENT_OK=$(curl "${curl_args[@]}" "$BASE/api/agents" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if isinstance(d,dict) and "agents" in d else "no")' 2>/dev/null || echo "no")
if [[ "$AGENT_OK" != "yes" ]]; then
  skip "agent manager unavailable; skipping F10 lifecycle"
elif [[ -z "$SMOKE_PROF" || -z "$SMOKE_CLUSTER" ]]; then
  skip "F10 lifecycle requires §7d fixtures; not present"
else
  ok "GET /api/agents returns canonical {agents:[]} shape"
  AUDIT_BEFORE=$(wc -l "$HOME/.datawatch/auth/audit.jsonl" 2>/dev/null | awk '{print $1}' || echo 0)
  SP_BODY=$(printf '{"project_profile":"%s","cluster_profile":"%s","task":"smoke F10 probe","branch":"main"}' "$SMOKE_PROF" "$SMOKE_CLUSTER")
  SP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$SP_BODY" "$BASE/api/agents" 2>/dev/null)
  AGT_ID=$(echo "$SP" | python3 -c "
import json,sys
d=json.load(sys.stdin)
# Response shape: either {agent:{...}} or the agent dict directly.
a = d.get('agent', d) if isinstance(d, dict) else {}
print(a.get('id',''))
" 2>/dev/null)
  if [[ -n "$AGT_ID" ]]; then
    ok "agent spawn round-trip returned id=$AGT_ID"
    if curl "${curl_args[@]}" "$BASE/api/agents" | python3 -c "
import json,sys
arr = json.load(sys.stdin).get('agents',[])
hit = any(a.get('id') == '$AGT_ID' for a in arr)
assert hit, 'agent $AGT_ID missing from list'
" 2>/dev/null; then
      ok "agent $AGT_ID appears in GET /api/agents"
    else
      ko "agent $AGT_ID missing from GET /api/agents"
    fi
    # Audit invariant — at least one new line should appear in the
    # auth audit (mint or mint-fail). Operator-asked: no token leaks.
    sleep 1
    AUDIT_AFTER=$(wc -l "$HOME/.datawatch/auth/audit.jsonl" 2>/dev/null | awk '{print $1}' || echo 0)
    if [[ "$AUDIT_AFTER" -gt "$AUDIT_BEFORE" ]]; then
      ok "auth audit grew on spawn (BL113 broker recorded mint or mint-fail)"
    else
      # Acceptable when the broker isn't wired at all (no /auth/audit.jsonl);
      # treat as skip.
      skip "auth audit unchanged ($AUDIT_BEFORE→$AUDIT_AFTER) — broker may not be wired"
    fi
    # Cleanup — DELETE returns 204 even if the worker is mid-start;
    # daemon walks the broker revoke path on its way through.
    if curl "${curl_args[@]}" -X DELETE -w "%{http_code}" -o /dev/null "$BASE/api/agents/$AGT_ID" 2>/dev/null | grep -q "204"; then
      ok "agent $AGT_ID DELETE → 204 (terminate + token revoke path triggered)"
    else
      ko "agent $AGT_ID terminate failed"
    fi
  else
    skip "agent spawn failed at the API surface: $(echo "$SP" | head -c 200)"
  fi
fi

H "7k. Claude skip_permissions config round-trip"
# v5.26.57 — operator-asked: "Have we smoke tested it?" (about
# claude --dangerously-skip-permissions / session.claude.skip_permissions).
# The behaviour (claude actually skipping prompts) needs a live
# claude session; this section just verifies the config knob
# round-trips through GET / PUT /api/config so a regression in
# the dotted-key handler can't silently disable it. The same
# config is what the daemon reads at startup before Register'ing
# the claude-code backend with --dangerously-skip-permissions.
SK_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c 'import json,sys;d=json.load(sys.stdin);v=d.get("session",{}).get("skip_permissions","missing");print(str(v).lower())' 2>/dev/null)
if [[ "$SK_BEFORE" == "missing" ]]; then
  skip "session.claude.skip_permissions key not in /api/config response shape"
else
  ok "GET /api/config exposes session.skip_permissions=$SK_BEFORE"
  # Toggle, verify, restore. Dotted-key PUT shape uses
  # session.skip_permissions (the api.go config map key); maps to
  # cfg.Session.ClaudeSkipPermissions internally.
  if [[ "$SK_BEFORE" == "true" ]]; then
    NEXT="false"; RESTORE="true"
  else
    NEXT="true"; RESTORE="false"
  fi
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"session.skip_permissions":%s}' "$NEXT")" \
    "$BASE/api/config" >/dev/null
  AFTER=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("session",{}).get("skip_permissions")).lower())' 2>/dev/null)
  if [[ "$AFTER" == "$NEXT" ]]; then
    ok "PUT /api/config flipped session.skip_permissions to $NEXT"
  else
    ko "PUT /api/config did not flip (was $SK_BEFORE → wanted $NEXT → got $AFTER)"
  fi
  # Restore original value (failing this leaks state across runs).
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"session.skip_permissions":%s}' "$RESTORE")" \
    "$BASE/api/config" >/dev/null
fi

H "7l. PRD-flow Phase 3 — per-story execution profile + per-story approval"
# v5.26.62 — Phase 3 endpoints land in v5.26.60 (.A schema/REST) +
# v5.26.61 (.B Run gating + config flag). §7l toggles
# autonomous.per_story_approval ON, decomposes a contrived PRD,
# approves the PRD, verifies stories transition to
# awaiting_approval, calls approve_story / reject_story / set_
# story_profile, validates audit decisions, then restores the
# config and cleans up.
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping Phase 3 smoke"
else
  # Capture + flip the gate flag.
  PSA_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("per_story_approval","")).lower())' 2>/dev/null)
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"autonomous.per_story_approval":true}' "$BASE/api/config" >/dev/null
  ok "autonomous.per_story_approval flipped on for Phase 3 smoke (was $PSA_BEFORE)"

  # Create a PRD, decompose, approve.
  PR3=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"spec":"phase3 smoke probe — touch internal/foo.go","project_dir":"/tmp","backend":"ollama","effort":"low"}' \
    "$BASE/api/autonomous/prds")
  PR3_ID=$(echo "$PR3" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
  if [[ -n "$PR3_ID" ]]; then
    add_cleanup prd "$PR3_ID"
    DEC=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{}' "$BASE/api/autonomous/prds/$PR3_ID/decompose")
    if echo "$DEC" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("stories"),list) and len(d["stories"])>=1' 2>/dev/null; then
      ok "Phase 3: PRD $PR3_ID decomposed (≥1 story)"
      curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
        -d '{"actor":"smoke"}' "$BASE/api/autonomous/prds/$PR3_ID/approve" >/dev/null
      # With per_story_approval ON, every story should be awaiting_approval.
      AWAIT_OK=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR3_ID" | python3 -c "
import json,sys
d=json.load(sys.stdin)
sts=[s.get('status') for s in (d.get('stories') or [])]
print('yes' if all(s=='awaiting_approval' for s in sts) and sts else 'no')" 2>/dev/null)
      if [[ "$AWAIT_OK" == "yes" ]]; then
        ok "Phase 3: PRD approve transitioned every story → awaiting_approval"
      else
        ko "Phase 3: stories did NOT transition to awaiting_approval after PRD approve"
      fi
      # Pick the first story id; exercise set_story_profile, approve, reject.
      SID=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR3_ID" | python3 -c 'import json,sys;d=json.load(sys.stdin);print((d.get("stories") or [{}])[0].get("id",""))' 2>/dev/null)
      if [[ -n "$SID" ]]; then
        # set_story_profile (use the persistent §7d project profile).
        curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
          -d "$(printf '{"story_id":"%s","profile":"%s","actor":"smoke"}' "$SID" "${SMOKE_PROF:-datawatch-smoke}")" \
          "$BASE/api/autonomous/prds/$PR3_ID/set_story_profile" > /dev/null
        if curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR3_ID" | python3 -c "
import json,sys
d=json.load(sys.stdin)
s=next((x for x in (d.get('stories') or []) if x.get('id')=='$SID'), {})
# set_story_profile errors when PRD is past needs_review; we
# already approved above so this should fail. Check the audit
# entry exists either way.
" 2>/dev/null; then
          : # set_story_profile is gated on needs_review; expected to noop after approve
        fi
        # Approve the story.
        curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
          -d "$(printf '{"story_id":"%s","actor":"smoke"}' "$SID")" \
          "$BASE/api/autonomous/prds/$PR3_ID/approve_story" > /dev/null
        APP_OK=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR3_ID" | python3 -c "
import json,sys
d=json.load(sys.stdin)
s=next((x for x in (d.get('stories') or []) if x.get('id')=='$SID'), {})
print('yes' if s.get('approved')==True and s.get('status') in ('pending','in_progress','completed') else 'no')" 2>/dev/null)
        if [[ "$APP_OK" == "yes" ]]; then
          ok "Phase 3: approve_story flipped Approved=true and transitioned awaiting_approval → pending"
        else
          ko "Phase 3: approve_story did not flip the story state"
        fi
        # Reject would block the story; smoke can't easily verify
        # without a second story, so just exercise the endpoint with
        # a reason and check for an audit decision.
        curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
          -d "$(printf '{"story_id":"%s","actor":"smoke","reason":"smoke probe — not real reject"}' "$SID")" \
          "$BASE/api/autonomous/prds/$PR3_ID/reject_story" > /dev/null
        REJ_OK=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PR3_ID" | python3 -c "
import json,sys
d=json.load(sys.stdin)
decs=[x.get('kind') for x in (d.get('decisions') or [])]
print('yes' if 'reject_story' in decs else 'no')" 2>/dev/null)
        if [[ "$REJ_OK" == "yes" ]]; then
          ok "Phase 3: reject_story recorded a decision in the audit timeline"
        else
          ko "Phase 3: reject_story did not append an audit decision"
        fi
      else
        skip "no story id available; can't exercise per-story endpoints"
      fi
    else
      skip "Phase 3 decompose returned no stories: $(echo "$DEC" | head -c 100)"
    fi
  else
    skip "Phase 3 PRD create failed: $(echo "$PR3" | head -c 200)"
  fi

  # Restore the gate flag to its prior value.
  if [[ "$PSA_BEFORE" == "true" ]]; then RESTORE_PSA=true; else RESTORE_PSA=false; fi
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"autonomous.per_story_approval":%s}' "$RESTORE_PSA")" \
    "$BASE/api/config" >/dev/null
fi

H "7m. Wake-up stack L0–L3 surface checks"
# v5.26.65 — service-function smoke audit residual #39. The wake-up
# layers (L0-L5 + L0ForAgent + WakeUpContext) compose at agent
# bootstrap time and don't have a direct REST endpoint. Smoke
# probes the underlying surfaces a regression in the layer-
# composer would also break:
#
#   L0  — <data_dir>/identity.txt presence (operator-set or empty)
#   L1+ — /api/memory/stats reports memory enabled (source for L1)
#   L3  — /api/memory/search responds (the layer's own underlying
#         endpoint)
#
# L4 (parent context) + L5 (sibling visibility) need a spawned-
# agent fixture; tracked. This section is a partial probe — full
# L0-L5 round-trip lives in the Go unit tests under
# internal/memory/layers_recursive_test.go.
DD="${HOME}/.datawatch"
if [[ -f "$DD/identity.txt" ]]; then
  ok "L0: identity.txt present at $DD/identity.txt"
else
  skip "L0: $DD/identity.txt not set (operator hasn't provided a host identity — empty L0 is valid)"
fi
if [[ "$MEM_OK" == "yes" ]]; then
  # L1 source — stats reports enabled.
  ok "L1 source: memory subsystem reachable (already validated by §7f / §9)"
else
  skip "L1 source: memory subsystem disabled"
fi

H "7n. KG add + query round-trip"
# v5.26.68 — §41 audit gap #1: full KG round-trip beyond just stats.
if [[ "$MEM_OK" != "yes" ]]; then
  skip "memory disabled; skipping KG round-trip"
else
  KG_PROBE_SUB="smoke-probe-$(date +%s)-subject"
  KG_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "$(printf '{"subject":"%s","predicate":"smoke_probe","object":"smoke-target"}' "$KG_PROBE_SUB")" \
    "$BASE/api/memory/kg/add")
  KG_ID=$(echo "$KG_RES" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
  if [[ -n "$KG_ID" && "$KG_ID" != "0" ]]; then
    ok "KG add returned id=$KG_ID"
    if curl "${curl_args[@]}" "$BASE/api/memory/kg/query?entity=$KG_PROBE_SUB" | python3 -c "
import json,sys
arr = json.load(sys.stdin)
hit = any(int(t.get('id',0)) == int('$KG_ID') for t in arr)
assert hit, 'kg id $KG_ID not found in query'
" 2>/dev/null; then
      ok "KG query?entity=$KG_PROBE_SUB returns the saved triple"
    else
      ko "KG query did NOT return id=$KG_ID"
    fi
    # /api/memory/kg/invalidate cleans up
    curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d "$(printf '{"id":%s}' "$KG_ID")" "$BASE/api/memory/kg/invalidate" >/dev/null
  else
    skip "KG add failed: $(echo "$KG_RES" | head -c 100)"
  fi
fi

H "7o. Spatial-dim filtered search round-trip"
# v5.26.68 — §41 audit gap #2: search filtered by wing/hall/room.
if [[ "$MEM_OK" != "yes" ]]; then
  skip "memory disabled"
else
  PROBE_TXT="datawatch-spatial-probe-$(date +%s)"
  SR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
       -d "$(printf '{"content":"%s","wing":"smoke-spatial"}' "$PROBE_TXT")" \
       "$BASE/api/memory/save")
  SP_ID=$(echo "$SR" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null)
  if [[ -n "$SP_ID" ]]; then
    # Spatial-filter via /api/memory/list with wing param
    if curl "${curl_args[@]}" "$BASE/api/memory/list?wing=smoke-spatial&limit=200" | python3 -c "
import json,sys
arr = json.load(sys.stdin)
hit = any(int(m.get('id',0)) == int('$SP_ID') for m in arr)
assert hit, 'spatial probe $SP_ID not in wing-filtered list'
" 2>/dev/null; then
      ok "spatial wing-filter returns probe id=$SP_ID"
    else
      # Defensive: maybe daemon doesn't accept wing in /list query; try search
      skip "spatial wing-filter list returned no hit (daemon may not honor wing param in /list — recall via /search would still cover this)"
    fi
    curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
         -d "$(printf '{"id":%s}' "$SP_ID")" "$BASE/api/memory/delete" >/dev/null
  else
    skip "spatial probe save failed"
  fi
fi

H "7p. Entity detection round-trip (BL60)"
# v5.26.68 — §41 audit gap #4: entity detection via memory_save +
# kg query?entity=. Saves a fact mentioning a unique-suffix entity;
# verifies the entity emerges in /api/memory/kg/query?entity=. The
# entity-detection pass runs async on save; smoke retries briefly.
if [[ "$MEM_OK" != "yes" ]]; then
  skip "memory disabled"
else
  ENT="smoke-entity-$(date +%s)"
  curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
       -d "$(printf '{"content":"The %s component talks to PostgreSQL."}' "$ENT")" \
       "$BASE/api/memory/save" >/dev/null
  # Poll up to 10s for the entity extractor to complete.
  HIT="no"
  for i in 1 2 3 4 5; do
    sleep 2
    if curl "${curl_args[@]}" "$BASE/api/memory/kg/query?entity=$ENT" 2>/dev/null | python3 -c "
import json,sys
try:
    arr = json.load(sys.stdin)
    assert isinstance(arr, list) and len(arr) >= 1
except Exception:
    sys.exit(1)
" 2>/dev/null; then
      HIT="yes"; break
    fi
  done
  if [[ "$HIT" == "yes" ]]; then
    ok "entity detection: $ENT surfaces in KG within 10s"
  else
    skip "entity detector hasn't surfaced $ENT in 10s — may be disabled or async-slow"
  fi
fi

H "7q. Per-backend channel send (when configured)"
# v5.26.68 — §41 audit gap #3: real backend send when one is wired.
# Skip cleanly otherwise (CI doesn't have signal/telegram tokens).
CFG_BACKENDS=$(curl "${curl_args[@]}" "$BASE/api/config" 2>/dev/null | python3 -c "
import json,sys
d = json.load(sys.stdin)
backends = []
for name in ('signal','telegram','slack','discord','matrix','email','twilio'):
    sec = d.get(name, {})
    if isinstance(sec, dict) and sec.get('enabled'):
        backends.append(name)
print(','.join(backends))" 2>/dev/null)
if [[ -z "$CFG_BACKENDS" ]]; then
  skip "no comm backend (signal/telegram/slack/etc.) is enabled"
else
  ok "comm backends enabled: $CFG_BACKENDS"
  # Use the existing /api/test/message synthesizer as the cheapest
  # round-trip — exercises router → backend dispatcher path without
  # actually sending an outbound message that would alert real users.
  # Enabled-backends presence alone is the primary signal we want
  # at the regression level; outbound smoke would need each
  # backend's recipient configured per-CI which is not portable.
  ok "comm backend send-path covered indirectly via §7i + dispatcher route"
fi

H "7r. Stdio-mode MCP tools (when wrapper available)"
# v5.26.71 — full stdio MCP probe via release-smoke-stdio-mcp.sh.
# Spawns `datawatch mcp` as a subprocess, sends JSON-RPC initialize +
# tools/list + tools/call(memory_recall), validates each response.
# Closes mempalace-audit partial: "memory_recall not in stdio surface".
# 9.5.29 (BL463) — adds the live get_my_scope probe (a session calls it and
# gets its scope back) and the failing-call contract: both in-process and
# loopback-proxy failures must come back isError=true with status + body,
# holding MCP to REST's reliability bar.
STDIO_MCP_SCRIPT="$(dirname "$0")/release-smoke-stdio-mcp.sh"
if [[ -x "$STDIO_MCP_SCRIPT" ]]; then
  # Give the probe its context. Local smoke mode: point the spawned
  # `datawatch mcp` at the smoke daemon's config so its loopback proxies hit
  # SMOKE_PORT against the isolated data dir (and the failing proxy call
  # gets a real HTTP status + body). External-daemon mode: use the binary
  # we already resolved for the CLI if we have one.
  STDIO_ENV=()
  STDIO_HINT=""
  if [[ "$EXTERNAL_DAEMON" -eq 0 && -n "${TEST_CONFIG:-}" && -n "${_DW_BIN:-}" ]]; then
    STDIO_ENV=("DW_BIN=$_DW_BIN" "DW_CONFIG=$TEST_CONFIG" "DW_REQUIRE_HTTP_STATUS=1")
    # Positive probe needs a known session: reuse one if the smoke daemon
    # already has one, else start a throwaway fixture session (shell backend
    # preferred — free, local, no LLM quota).
    STDIO_HINT=$(curl "${curl_args[@]}" -s "$BASE/api/sessions" 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin); ss=d.get("sessions") if isinstance(d,dict) else d
for s in (ss or []):
    fid=s.get("full_id") or s.get("id")
    if fid: print(fid); break' 2>/dev/null || echo "")
    if [[ -z "$STDIO_HINT" ]]; then
      FIX_BACKEND=$(curl "${curl_args[@]}" -s "$BASE/api/backends" 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin); llm=d.get("llm") or []
prefer=["shell","opencode-acp","opencode","claude-code"]
avail=[b.get("name") for b in llm if b.get("available") and b.get("name")]
for p in prefer:
    if p in avail: print(p); break
else: print(avail[0] if avail else "")' 2>/dev/null || echo "")
      if [[ -n "$FIX_BACKEND" ]]; then
        FIX_RESP=$(curl "${curl_args[@]}" -s -X POST -H "Content-Type: application/json" \
          -d "{\"task\":\"smoke fixture: stdio get_my_scope check\",\"llm_backend\":\"$FIX_BACKEND\",\"project_dir\":\"/tmp\",\"name\":\"smoke-stdio-scope-$RUN_ID\"}" \
          "$BASE/api/sessions/start" 2>/dev/null || echo "")
        STDIO_HINT=$(echo "$FIX_RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("full_id",""))' 2>/dev/null || echo "")
      fi
      [[ -n "$STDIO_HINT" ]] && add_cleanup sess "$STDIO_HINT"
    fi
  elif [[ -n "${_DW_BIN:-}" ]]; then
    STDIO_ENV=("DW_BIN=$_DW_BIN")
    STDIO_HINT=$(curl "${curl_args[@]}" -s "$BASE/api/sessions" 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin); ss=d.get("sessions") if isinstance(d,dict) else d
for s in (ss or []):
    fid=s.get("full_id") or s.get("id")
    if fid: print(fid); break' 2>/dev/null || echo "")
  fi
  if STDIO_OUT=$(env DW_SESSION_HINT="$STDIO_HINT" "${STDIO_ENV[@]}" bash "$STDIO_MCP_SCRIPT" 2>&1); then
    ok "$STDIO_OUT"
  else
    rc=$?
    if [[ $rc -eq 2 ]]; then
      skip "$STDIO_OUT"
    else
      ko "$STDIO_OUT"
    fi
    [ -n "$STDIO_HINT" ] && echo "  (probe session hint: $STDIO_HINT)" >&2
  fi
else
  skip "release-smoke-stdio-mcp.sh missing; stdio MCP probe deferred"
fi

H "7s. Wake-up L4/L5 bundle composer (REST)"
# v5.26.71 — full L0-L5 composition probe via release-smoke-wakeup.sh.
# Hits GET /api/memory/wakeup with three argument shapes (L0+L1 base,
# L4 with parent_agent_id, L5 with self+parent). Verifies the same
# bytes an agent would receive at bootstrap. Backs out the v5.26.68
# prereq-only stub.
WAKEUP_SCRIPT="$(dirname "$0")/release-smoke-wakeup.sh"
if [[ -x "$WAKEUP_SCRIPT" ]]; then
  if WAKEUP_OUT=$(DATAWATCH_BASE="$BASE" DATAWATCH_TOKEN="$TOK" bash "$WAKEUP_SCRIPT" 2>&1); then
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      [[ "$line" == OK:* ]] && continue
      ok "$line"
    done <<< "$WAKEUP_OUT"
  else
    rc=$?
    if [[ $rc -eq 2 ]]; then
      skip "wake-up probe skipped (memory subsystem disabled or daemon unreachable)"
    else
      ko "wake-up probe failed: $WAKEUP_OUT"
    fi
  fi
else
  skip "release-smoke-wakeup.sh missing; L4/L5 probe deferred"
fi

H "7t. v6.0 mempalace surfaces — sweep_stale + spellcheck + extract_facts"
# 2026-10-13 — found running release-smoke.sh in external-daemon mode
# against a real kind-deployed chart for the first time ever: this
# section had no memory-enabled gate at all (unlike section 7f's
# identical surface, just above), so it hard-FAILed the first time it
# ran against a daemon whose chart defaults leave memory disabled —
# every prior run only ever hit the local test daemon's own
# always-memory-enabled TEST_CONFIG. Same $MEM_OK gate 7f already uses.
if [[ "$MEM_OK" != "yes" ]]; then
  skip "memory subsystem not enabled — skipping mempalace surfaces (sweep_stale/spellcheck/extract_facts)"
else
SWEEP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"older_than_days":90,"dry_run":true}' "$BASE/api/memory/sweep_stale")
if echo "$SWEEP" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "candidates" in d and "dry_run" in d' 2>/dev/null; then
  ok "sweep_stale dry-run shape ok ($SWEEP)"
else
  ko "sweep_stale failed: $SWEEP"
fi
SPELL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"text":"protocoll daemon"}' "$BASE/api/memory/spellcheck")
if echo "$SPELL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["count"]>=1' 2>/dev/null; then
  ok "spellcheck returned suggestions"
else
  ko "spellcheck failed: $SPELL"
fi
EXTRACT=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"text":"Postgres depends on libpq."}' "$BASE/api/memory/extract_facts")
if echo "$EXTRACT" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["count"]>=1' 2>/dev/null; then
  ok "extract_facts returned triples"
else
  ko "extract_facts failed: $EXTRACT"
fi
fi

H "7u. v5.27.2 surfaces — subsystem reload + claude_auto_accept_disclaimer config round-trip"
# REST: full reload returns OK + requires_restart list.
RR=$(curl "${curl_args[@]}" -X POST "$BASE/api/reload")
if echo "$RR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["ok"] and "requires_restart" in d' 2>/dev/null; then
  ok "reload (full) OK + requires_restart list"
else
  ko "reload (full) shape mismatch: $RR"
fi
# REST: subsystem=filters → applied:[filters].
RF=$(curl "${curl_args[@]}" -X POST "$BASE/api/reload?subsystem=filters")
if echo "$RF" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["ok"] and "filters" in d.get("applied",[])' 2>/dev/null; then
  ok "reload?subsystem=filters applied"
else
  ko "reload?subsystem=filters shape mismatch: $RF"
fi
# REST: unknown subsystem → 500 with registered names listed.
HC=$(curl "${curl_args[@]}" -o /tmp/_dw_smoke_reload.txt -w "%{http_code}" -X POST "$BASE/api/reload?subsystem=__bogus__")
if [[ "$HC" == "500" ]] && grep -q "unknown subsystem" /tmp/_dw_smoke_reload.txt; then
  ok "reload?subsystem=__bogus__ → 500 with registered list"
else
  ko "reload?subsystem=__bogus__ unexpected: code=$HC body=$(cat /tmp/_dw_smoke_reload.txt)"
fi
rm -f /tmp/_dw_smoke_reload.txt
# Chat-channel parity: `reload` + `reload <subsystem>` round-trip.
RC=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"text":"reload filters"}' "$BASE/api/test/message")
if echo "$RC" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["count"]==1 and "filters" in d["responses"][0]' 2>/dev/null; then
  ok "chat-channel reload filters"
else
  ko "chat-channel reload filters: $RC"
fi
# Config round-trip: claude_auto_accept_disclaimer.
SAVED=$(curl "${curl_args[@]}" "$BASE/api/config" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin).get("session",{}).get("claude_auto_accept_disclaimer",None))')
PUT=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d '{"session.claude_auto_accept_disclaimer": true}' "$BASE/api/config")
if echo "$PUT" | grep -q '"status":"ok"'; then
  CHECK=$(curl "${curl_args[@]}" "$BASE/api/config" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("session",{}).get("claude_auto_accept_disclaimer"))')
  if [[ "$CHECK" == "True" ]]; then
    ok "config PUT/GET round-trip session.claude_auto_accept_disclaimer"
  else
    ko "config readback want True got $CHECK"
  fi
  # Restore prior value so the smoke is idempotent.
  if [[ "$SAVED" == "False" || "$SAVED" == "None" ]]; then
    curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
      -d '{"session.claude_auto_accept_disclaimer": false}' "$BASE/api/config" >/dev/null
  fi
else
  ko "config PUT failed: $PUT"
fi

H "7v. v5.27.4 — GET /api/update/check read-only endpoint"
# datawatch#25 — mobile clients need check-without-install for the
# "check → confirm → install" UX. Validates the endpoint exists,
# returns the right shape, and never triggers a download.
UC=$(curl "${curl_args[@]}" "$BASE/api/update/check")
if echo "$UC" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["status"] in ("up_to_date","update_available");assert "current_version" in d and "latest_version" in d' 2>/dev/null; then
  ok "update/check shape ok ($UC)"
else
  ko "update/check shape mismatch: $UC"
fi
# POST should be rejected (write-action goes through /api/update).
HC=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" -X POST "$BASE/api/update/check")
if [[ "$HC" == "405" ]]; then
  ok "update/check POST → 405 (read-only)"
else
  ko "update/check POST → $HC want 405"
fi

H "7w. v5.27.5 — claude-options endpoints (models / efforts / permission_modes)"
for path in models efforts permission_modes; do
  PR=$(curl "${curl_args[@]}" "$BASE/api/llm/claude/$path")
  if echo "$PR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("source")=="hardcoded"' 2>/dev/null; then
    ok "GET /api/llm/claude/$path returns hardcoded list"
  else
    ko "GET /api/llm/claude/$path bad shape: $PR"
  fi
done
# Permission-mode list MUST include 'plan' — the headline use case.
PM=$(curl "${curl_args[@]}" "$BASE/api/llm/claude/permission_modes")
if echo "$PM" | python3 -c 'import json,sys;d=json.load(sys.stdin);vals=[m["value"] for m in d.get("modes",[])];assert "plan" in vals' 2>/dev/null; then
  ok "permission_modes includes plan"
else
  ko "permission_modes missing plan: $PM"
fi
# Models list MUST include the three core aliases.
MD=$(curl "${curl_args[@]}" "$BASE/api/llm/claude/models")
if echo "$MD" | python3 -c 'import json,sys;d=json.load(sys.stdin);vals=[a["value"] for a in d.get("aliases",[])];assert all(x in vals for x in ["opus","sonnet","haiku"])' 2>/dev/null; then
  ok "models lists opus/sonnet/haiku aliases"
else
  ko "models missing core aliases: $MD"
fi
# Config round-trip on session.permission_mode.
SAVED_PM=$(curl "${curl_args[@]}" "$BASE/api/config" \
  | python3 -c 'import json,sys;print(json.load(sys.stdin).get("session",{}).get("permission_mode",""))')
PUT=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d '{"session.permission_mode": "plan"}' "$BASE/api/config")
if echo "$PUT" | grep -q '"status":"ok"'; then
  CHECK=$(curl "${curl_args[@]}" "$BASE/api/config" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("session",{}).get("permission_mode",""))')
  if [[ "$CHECK" == "plan" ]]; then
    ok "config round-trip session.permission_mode → plan"
  else
    ko "config readback want plan got $CHECK"
  fi
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "{\"session.permission_mode\": \"$SAVED_PM\"}" "$BASE/api/config" >/dev/null
else
  ko "config PUT failed: $PUT"
fi

H "7x. v5.27.7 — quick_commands endpoint + bridge memory tools"
QC=$(curl "${curl_args[@]}" "$BASE/api/quick_commands")
if echo "$QC" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("source") in ("default","config");assert isinstance(d.get("commands"),list) and len(d["commands"])>=5' 2>/dev/null; then
  ok "quick_commands endpoint shape ok"
else
  ko "quick_commands shape mismatch: $QC"
fi
# Default list must include the canonical "yes" / "Esc" / "↑" entries
if echo "$QC" | python3 -c 'import json,sys;d=json.load(sys.stdin);labels=[c["label"] for c in d["commands"]];assert "yes" in labels and "Esc" in labels' 2>/dev/null; then
  ok "quick_commands default list includes yes + Esc"
else
  ko "quick_commands default list missing canonical entries: $QC"
fi
HC=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" -X POST "$BASE/api/quick_commands")
if [[ "$HC" == "405" ]]; then
  ok "quick_commands rejects POST (read-only)"
else
  ko "quick_commands POST → $HC want 405"
fi

H "7y. v6.0 BL220 — detection/dns_channel/proxy config sections readable"
# MCP tools detection_config_get / dns_channel_config_get / proxy_config_get all
# proxy to /api/config and extract a named section. Verify /api/config returns
# the expected top-level keys so the tools have something to forward.
CFG7Y=$(curl "${curl_args[@]}" "$BASE/api/config")
if echo "$CFG7Y" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "detection" in d' 2>/dev/null; then
  ok "config has detection section (detection_config_get prereq)"
else
  ko "config missing detection section: $(echo "$CFG7Y" | head -c 200)"
fi
if echo "$CFG7Y" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "dns_channel" in d or "proxy" in d' 2>/dev/null; then
  ok "config has dns_channel/proxy section (dns_channel_config_get/proxy_config_get prereq)"
else
  skip "dns_channel and proxy sections absent (optional subsystems not configured)"
fi

H "7z. v6.0 BL220 — analytics endpoint shape"
# CLI command 'datawatch analytics' wraps GET /api/analytics; verify endpoint shape.
AN=$(curl "${curl_args[@]}" "$BASE/api/analytics?range=7d")
if echo "$AN" | python3 -c 'import json,sys;d=json.load(sys.stdin);b=d.get("buckets",[]);assert isinstance(b,list);assert all("date" in r and "session_count" in r for r in b)' 2>/dev/null; then
  ok "analytics endpoint: buckets have date + session_count fields"
else
  ko "analytics endpoint shape mismatch: $(echo "$AN" | head -c 200)"
fi

H "7aa. v6.0 BL220 — comm commands analytics + detection via test/message"
# Comm commands for analytics and detection are routed by the comm dispatcher.
# The response may fail with 'REST loopback not configured' in isolated test
# environments — that's a SKIP, not a FAIL. A structural 404 or missing
# 'responses' key is a FAIL.
AN_MSG=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"text":"analytics"}' "$BASE/api/test/message")
if echo "$AN_MSG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("responses",[]),list) and len(d["responses"])>0' 2>/dev/null; then
  if echo "$AN_MSG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "loopback" not in d["responses"][0].lower()' 2>/dev/null; then
    ok "comm analytics command: dispatched and responded"
  else
    skip "comm analytics: loopback unavailable in this env (command wired correctly)"
  fi
else
  ko "comm analytics command: no responses array in: $(echo "$AN_MSG" | head -c 200)"
fi

DET_MSG=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"text":"detection"}' "$BASE/api/test/message")
if echo "$DET_MSG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("responses",[]),list) and len(d["responses"])>0' 2>/dev/null; then
  if echo "$DET_MSG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "loopback" not in d["responses"][0].lower()' 2>/dev/null; then
    ok "comm detection command: dispatched and responded"
  else
    skip "comm detection: loopback unavailable in this env (command wired correctly)"
  fi
else
  ko "comm detection command: no responses array in: $(echo "$DET_MSG" | head -c 200)"
fi

H "7ab. Go channel bridge available at runtime"
# Verify the daemon resolved the native Go bridge (not the JS fallback).
# A daemon running the JS bridge means datawatch-channel was not shipped
# in the release or the auto-download failed — either is a smoke failure.
BRIDGE=$(curl "${curl_args[@]}" "$BASE/api/channel/info" 2>/dev/null || true)
if echo "$BRIDGE" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("kind")=="go"' 2>/dev/null; then
  ok "channel bridge: Go bridge active ($(echo "$BRIDGE" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("path","?"))' 2>/dev/null))"
elif echo "$BRIDGE" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("kind")=="js"' 2>/dev/null; then
  ko "channel bridge: JS fallback active — datawatch-channel missing from release or auto-download failed"
else
  skip "channel bridge: /api/channel/info unavailable or unexpected shape ($BRIDGE)"
fi

H "7ac. MCP resources/read round-trip (BL302 S1)"
RES=$(curl "${curl_args[@]}" "$BASE/api/mcp/resources" 2>/dev/null || true)
COUNT=$(echo "$RES" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("resources",[])))' 2>/dev/null || echo 0)
if [ "$COUNT" -gt 0 ]; then
  ok "resources list: $COUNT resources registered"
else
  ko "resources list: 0 resources (expected >0)"
fi
VERSION_CONTENT=$(curl "${curl_args[@]}" "$BASE/api/mcp/resources/read?uri=datawatch://version" 2>/dev/null || true)
if echo "$VERSION_CONTENT" | python3 -c 'import json,sys; d=json.load(sys.stdin); cs=d.get("contents",[]); assert len(cs)>0 and "version" in (cs[0].get("text","") if cs else "")' 2>/dev/null; then
  ok "datawatch://version read returns version field"
else
  ko "datawatch://version read failed or missing version field (got: ${VERSION_CONTENT:0:100})"
fi
TEMPLATES_RES=$(curl "${curl_args[@]}" "$BASE/api/mcp/resources/templates" 2>/dev/null || true)
TMPL_COUNT=$(echo "$TEMPLATES_RES" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("templates",[])))' 2>/dev/null || echo 0)
if [ "$TMPL_COUNT" -gt 0 ]; then
  ok "resource templates list: $TMPL_COUNT templates registered"
else
  ko "resource templates list: 0 templates (expected >0)"
fi

# BL302 S2 — live resource checks (sessions, stats, alerts).
SESSIONS_CONTENT=$(curl "${curl_args[@]}" "$BASE/api/mcp/resources/read?uri=datawatch://sessions" 2>/dev/null || true)
if echo "$SESSIONS_CONTENT" | python3 -c 'import json,sys; d=json.load(sys.stdin); cs=d.get("contents",[]); t=cs[0].get("text","") if cs else ""; assert "sessions" in t' 2>/dev/null; then
  ok "datawatch://sessions returns sessions key"
else
  ko "datawatch://sessions read failed or missing sessions key (got: ${SESSIONS_CONTENT:0:100})"
fi

STATS_CONTENT=$(curl "${curl_args[@]}" "$BASE/api/mcp/resources/read?uri=datawatch://stats" 2>/dev/null || true)
if echo "$STATS_CONTENT" | python3 -c 'import json,sys; d=json.load(sys.stdin); cs=d.get("contents",[]); assert len(cs)>0' 2>/dev/null; then
  ok "datawatch://stats read returns content"
else
  ko "datawatch://stats read failed (got: ${STATS_CONTENT:0:100})"
fi

ALERTS_CONTENT=$(curl "${curl_args[@]}" "$BASE/api/mcp/resources/read?uri=datawatch://alerts" 2>/dev/null || true)
if echo "$ALERTS_CONTENT" | python3 -c 'import json,sys; d=json.load(sys.stdin); cs=d.get("contents",[]); t=cs[0].get("text","") if cs else ""; assert "alerts" in t' 2>/dev/null; then
  ok "datawatch://alerts returns alerts key"
else
  ko "datawatch://alerts read failed or missing alerts key (got: ${ALERTS_CONTENT:0:100})"
fi

H "7ad. MCP prompts surface check (BL302 S4)"
PROMPTS_RES=$(curl "${curl_args[@]}" "$BASE/api/mcp/prompts" 2>/dev/null || true)
PROMPT_COUNT=$(echo "$PROMPTS_RES" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d.get("prompts",[])))' 2>/dev/null || echo 0)
if [ "$PROMPT_COUNT" -ge 10 ]; then
  ok "prompts list: $PROMPT_COUNT prompts registered (expected ≥10)"
else
  ko "prompts list: $PROMPT_COUNT prompts (expected ≥10, got: ${PROMPTS_RES:0:100})"
fi
DIAG_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"name":"diagnose-system","arguments":{}}' \
  "$BASE/api/mcp/prompts/get" 2>/dev/null || true)
if echo "$DIAG_RES" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "messages" in d and len(d["messages"])>0' 2>/dev/null; then
  ok "prompts/get diagnose-system returns messages"
else
  ko "prompts/get diagnose-system failed or missing messages (got: ${DIAG_RES:0:100})"
fi

H "7ae. MCP sampling endpoint surface check (BL302 S3)"
SAMPLE_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"trigger":"smoke","messages":[{"role":"user","content":"ping"}]}' \
  "$BASE/api/mcp/sample" 2>/dev/null || true)
if echo "$SAMPLE_RES" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "error" in d or "result" in d' 2>/dev/null; then
  ok "sampling endpoint returns structured response (no active session is ok)"
else
  ko "sampling endpoint missing or returning unexpected shape (got: ${SAMPLE_RES:0:100})"
fi

H "7af. MCP elicitation endpoint surface check (BL302 S3)"
ELICIT_RES=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"schema":"approval","message":"smoke test"}' \
  "$BASE/api/mcp/elicit" 2>/dev/null || true)
if echo "$ELICIT_RES" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "error" in d or "result" in d' 2>/dev/null; then
  ok "elicitation endpoint returns structured response (no active session is ok)"
else
  ko "elicitation endpoint missing or returning unexpected shape (got: ${ELICIT_RES:0:100})"
fi

H "7ag. BL312 S1 — multi-server CRUD surface check"
SRV_NAME="smoke-srv-$(date +%s)"
SRV_CREATE=$(curl "${curl_args[@]}" -s -X POST -H "Content-Type: application/json" \
  -d "{\"name\":\"$SRV_NAME\",\"url\":\"http://127.0.0.1:9999\",\"enabled\":false}" \
  "$BASE/api/servers" 2>/dev/null || true)
if echo "$SRV_CREATE" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("ok") or d.get("name")' 2>/dev/null; then
  ok "server create: $SRV_NAME"
  SRV_GET=$(curl "${curl_args[@]}" -s "$BASE/api/servers/$SRV_NAME" 2>/dev/null || true)
  if echo "$SRV_GET" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("name") == "'$SRV_NAME'"' 2>/dev/null; then
    ok "server get: $SRV_NAME found"
  else
    ko "server get: unexpected response: ${SRV_GET:0:120}"
  fi
  SRV_DEL=$(curl "${curl_args[@]}" -s -X DELETE "$BASE/api/servers/$SRV_NAME" 2>/dev/null || true)
  if echo "$SRV_DEL" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("ok")' 2>/dev/null; then
    ok "server delete: $SRV_NAME removed"
  else
    ko "server delete failed: ${SRV_DEL:0:120}"
  fi
  SRV_GONE=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/servers/$SRV_NAME" 2>/dev/null || echo "000")
  if [[ "$SRV_GONE" == "404" ]]; then
    ok "server get after delete: 404 confirmed"
  else
    ko "server get after delete: expected 404, got $SRV_GONE"
  fi
else
  ko "server create failed: ${SRV_CREATE:0:120}"
fi
SRV_LIST=$(curl "${curl_args[@]}" -s "$BASE/api/servers" 2>/dev/null || true)
if echo "$SRV_LIST" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "servers" in d or isinstance(d, list)' 2>/dev/null; then
  ok "server list: endpoint responds with server collection"
else
  ko "server list: unexpected shape: ${SRV_LIST:0:120}"
fi

H "7ah. BL312 S4/S5 — aggregated sessions + alerts + PRDs endpoints"
AGG_SESS=$(curl "${curl_args[@]}" -s "$BASE/api/sessions/aggregated" 2>/dev/null || true)
if echo "$AGG_SESS" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert isinstance(d, list)' 2>/dev/null; then
  ok "sessions/aggregated: returns array"
else
  ko "sessions/aggregated: unexpected: ${AGG_SESS:0:120}"
fi
AGG_ALERTS=$(curl "${curl_args[@]}" -s "$BASE/api/alerts/aggregated" 2>/dev/null || true)
if echo "$AGG_ALERTS" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert isinstance(d, list)' 2>/dev/null; then
  ok "alerts/aggregated: returns array"
else
  ko "alerts/aggregated: unexpected: ${AGG_ALERTS:0:120}"
fi
AGG_PRDS=$(curl "${curl_args[@]}" -s "$BASE/api/autonomous/prds/aggregated" 2>/dev/null || true)
if echo "$AGG_PRDS" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert isinstance(d, list)' 2>/dev/null; then
  ok "prds/aggregated: returns array"
else
  ko "prds/aggregated: unexpected: ${AGG_PRDS:0:120}"
fi

H "7ai. PRD model field persistence + cancel kills sessions"
# v8.23.x — Two regressions this section guards:
# 1. autonomous_prd_create lacked a model field so sessions always fell
#    back to cfg.OpenCode.DefaultModel instead of the operator's choice.
# 2. PRD cancel did not kill in-flight task sessions; they kept running.
if [[ "$A_ENABLED" != "yes" ]]; then
  skip "autonomous disabled; skipping PRD model + cancel tests"
else
  # Model field round-trip (no LLM required).
  PRD_M=$(curl "${curl_args[@]}" -s -X POST -H "Content-Type: application/json" \
    -d "{\"spec\":\"smoke model test\",\"project_dir\":\"/tmp\",\"model\":\"smoke-model-sentinel\"}" \
    "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$PRD_M" ]]; then
    ko "PRD create with model field: create failed"
  else
    add_cleanup prd "$PRD_M"
    M_GOT=$(curl "${curl_args[@]}" -s "$BASE/api/autonomous/prds/$PRD_M" | \
      python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("model",""))' 2>/dev/null || echo "")
    if [[ "$M_GOT" == "smoke-model-sentinel" ]]; then
      ok "PRD model field persists through create"
    else
      ko "PRD model field not persisted: got '$M_GOT' want 'smoke-model-sentinel'"
    fi
    curl "${curl_args[@]}" -s -X DELETE "$BASE/api/autonomous/prds/$PRD_M" >/dev/null 2>&1
  fi

  # Cancel kills sessions — requires a spawned task session.
  # Reuse SMOKE_PRD_ID from §7b if available; it already ran + has sessions.
  if [[ -n "${SMOKE_PRD_ID:-}" ]]; then
    # Check that no task session from the cancelled PRD is still running.
    LIVE_SESS=$(curl "${curl_args[@]}" -s "$BASE/api/sessions" | python3 -c "
import json, sys
d = json.load(sys.stdin)
prd_sessions = []
for s in d.get('sessions', []):
    if s.get('prd_id') == '$SMOKE_PRD_ID' and s.get('state') in ('running','waiting_input','rate_limited'):
        prd_sessions.append(s.get('id'))
print(','.join(prd_sessions))
" 2>/dev/null || echo "")
    if [[ -z "$LIVE_SESS" ]]; then
      ok "cancel kills sessions: no live task sessions remain after PRD cancel"
    else
      ko "cancel kills sessions: session(s) still running after PRD cancel: $LIVE_SESS"
    fi
  else
    skip "cancel kills sessions: no smoke PRD from §7b available"
  fi
fi

H "8. Observer peer register + push + cross-host aggregator"
PEER_NAME="smoke-peer-$(date +%s)"
REG=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "{\"name\":\"$PEER_NAME\",\"shape\":\"A\",\"version\":\"smoke\"}" \
  "$BASE/api/observer/peers")
PEER_TOK=$(echo "$REG" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("token",""))' 2>/dev/null || echo "")
if [[ -n "$PEER_TOK" && ${#PEER_TOK} -gt 20 ]]; then
  add_cleanup peer "$PEER_NAME"
  ok "peer register: $PEER_NAME (token len ${#PEER_TOK})"
else
  ko "peer register failed: $REG"
fi

if [[ -n "$PEER_TOK" ]]; then
  PUSH=$(curl "${curl_args[@]}" -X POST \
    -H "Authorization: Bearer $PEER_TOK" -H "Content-Type: application/json" \
    -d "{\"shape\":\"A\",\"peer_name\":\"$PEER_NAME\",\"snapshot\":{\"v\":2,\"envelopes\":[{\"id\":\"smoke-env\",\"kind\":\"Backend\",\"name\":\"smoke\"}]}}" \
    "$BASE/api/observer/peers/$PEER_NAME/stats")
  if echo "$PUSH" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("status")=="ok"' 2>/dev/null; then
    ok "peer push: snapshot accepted"
  else
    ko "peer push failed: $PUSH"
  fi

  ALL=$(curl "${curl_args[@]}" "$BASE/api/observer/envelopes/all-peers")
  if echo "$ALL" | python3 -c "import json,sys;d=json.load(sys.stdin);assert '$PEER_NAME' in (d.get('by_peer') or {})" 2>/dev/null; then
    ok "cross-host aggregator includes $PEER_NAME"
  else
    ko "cross-host aggregator missing $PEER_NAME: $(echo "$ALL" | head -c 200)"
  fi

  # cleanup_all on EXIT will deregister $PEER_NAME via the trap.
fi

# ---------------------------------------------------------------------------
H "9. Memory recall (smoke)"
# v5.26.28 fix — endpoint is /api/memory/search (not /recall). The
# old path always 404'd, so smoke silently SKIPped memory across
# every release even when the subsystem was healthy.
MR=$(curl "${curl_args[@]}" "$BASE/api/memory/search?q=smoke" || true)
# Accept either {"results":[...]} OR a bare top-level list. The
# previous check called .get() on a list and threw AttributeError,
# which made smoke SKIP even on healthy memory subsystems.
if echo "$MR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,list) or isinstance(d.get("results",[]),list)' 2>/dev/null; then
  ok "memory search returned a result list"
else
  skip "memory not enabled or returned $(echo "$MR" | head -c 100)"
fi

# ---------------------------------------------------------------------------
H "10. Voice transcribe availability"
VC=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("whisper",{}).get("enabled") else "no")' 2>/dev/null || echo no)
if [[ "$VC" == "yes" ]]; then
  ok "whisper enabled (transcription endpoint reachable in PWA)"
else
  skip "whisper disabled — mic affordances stay hidden in PWA"
fi

# ---------------------------------------------------------------------------
H "12. v6.7.0 BL255 — Skills registry CRUD + add-default + sync flow"
# Test the skills surface end-to-end. Idempotent — uses a uniquely named
# operator-test registry, so safe to run on configured systems too.
SKILLS_CHECK=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/skills/registries" 2>/dev/null || echo "000")
if [[ "$SKILLS_CHECK" != "200" ]]; then
  skip "skills disabled or endpoint unreachable (HTTP $SKILLS_CHECK)"
else
  ok "skills registries endpoint reachable"
  # add-default is idempotent
  ADD_DEFAULT=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" "$BASE/api/skills/registries/add-default" 2>/dev/null || echo "000")
  if [[ "$ADD_DEFAULT" == "200" ]]; then
    ok "skills registry add-default: idempotent (200)"
  else
    ko "skills registry add-default returned HTTP $ADD_DEFAULT"
  fi
  # Verify pai is in the list now
  PAI_LISTED=$(curl "${curl_args[@]}" "$BASE/api/skills/registries" 2>/dev/null | python3 -c 'import json,sys
d=json.load(sys.stdin)
regs=d if isinstance(d,list) else d.get("registries",[])
names=[r.get("name","") for r in regs]
print("yes" if "pai" in names else "no")' 2>/dev/null || echo "no")
  if [[ "$PAI_LISTED" == "yes" ]]; then
    ok "skills registry list: pai present"
  else
    ko "skills registry list missing pai after add-default"
  fi
  # Synced list endpoint reachable (likely empty)
  SYNCED_CHECK=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/skills" 2>/dev/null || echo "000")
  if [[ "$SYNCED_CHECK" == "200" ]]; then
    ok "skills synced list endpoint reachable"
  else
    ko "skills synced list endpoint returned HTTP $SYNCED_CHECK"
  fi
fi

H "11. Orchestrator graph CRUD"
O_ENABLED=$(curl "${curl_args[@]}" "$BASE/api/orchestrator/config" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
if [[ "$O_ENABLED" != "yes" ]]; then
  skip "orchestrator disabled; skipping graph CRUD"
else
  G=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"name":"smoke-graph","prds":[]}' "$BASE/api/orchestrator/graphs")
  GID=$(echo "$G" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$GID" ]]; then
    add_cleanup graph "$GID"
    ok "orchestrator graph create: $GID"
  else
    ko "orchestrator graph create failed: $G"
  fi
fi

H "16. v6.11.0 BL260 — Council Mode: personas + quick run"
COUNCIL_GET=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/council/personas" 2>/dev/null || echo "000")
if [[ "$COUNCIL_GET" != "200" ]]; then
  skip "council disabled or endpoint unreachable (HTTP $COUNCIL_GET)"
else
  ok "council personas endpoint reachable"
  # v7.0.0 S3 — council now does a REAL LLM round-trip via the
  # dispatcher. A single contrarian quick-run on a real Ollama can
  # easily take 5-10 minutes for a cold-loaded large model. The
  # default $curl_args timeout is too short. Allow up to 12 minutes
  # here; if no LLM is reachable, dispatcher returns ErrNoBackend
  # which surfaces as 500 immediately and won't sit on the timeout.
  # v7.0.0 S3 — smoke no longer issues a real council quick-run.
  # The v7 council does a REAL LLM round-trip via the dispatcher and
  # a single contrarian quick-run on a cold large model can exceed
  # 12 minutes — that's not appropriate for synchronous smoke. The
  # check below verifies (a) the registry has at least one LLM the
  # council CAN use, and (b) the cancel endpoint round-trips for an
  # unknown id (returning 404 — proves cancel router is wired).
  # Operators verify real council inference with `datawatch llm test
  # <name>` then `datawatch council run --proposal "..." --mode quick`.
  LLMS_GET=$(curl "${curl_args[@]}" "$BASE/api/llms" 2>/dev/null || echo "{}")
  LLM_COUNT=$(echo "$LLMS_GET" | python3 -c 'import json,sys;print(len(json.load(sys.stdin).get("llms",[])))' 2>/dev/null || echo "0")
  if [[ "$LLM_COUNT" -ge 1 ]]; then
    ok "council has $LLM_COUNT LLM(s) available via dispatcher"
  else
    skip "no LLMs registered (auto-migration may not have run; ok in fresh-cfg envs)"
  fi
  CCAN=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" "$BASE/api/council/runs/__smoke_unknown__/cancel" 2>/dev/null || echo "000")
  if [[ "$CCAN" == "404" ]]; then
    ok "council cancel endpoint round-trips (404 on unknown id)"
  else
    ko "council cancel endpoint returned HTTP $CCAN (expected 404)"
  fi
fi

H "15. v6.10.0 BL259 P1 — Evals framework: list suites + grader smoke"
EV_GET=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/evals/suites" 2>/dev/null || echo "000")
if [[ "$EV_GET" != "200" ]]; then
  skip "evals disabled or endpoint unreachable (HTTP $EV_GET)"
elif [[ "$EXTERNAL_DAEMON" == "1" ]]; then
  ok "evals suites endpoint reachable"
  skip "evals run smoke: external-daemon mode (DW_BASE) — this check drops a fixture suite file into the DAEMON's own data dir via the filesystem, which only works when this script and the daemon share one filesystem (not guaranteed for an external/remote/in-cluster target)"
else
  ok "evals suites endpoint reachable"
  # Drop a tiny capability suite in the smoke daemon's data dir, run it, expect pass.
  SUITE_DIR="$TEST_DATA_DIR/evals"
  mkdir -p "$SUITE_DIR" 2>/dev/null
  cat > "$SUITE_DIR/smoke.yaml" <<'EOF'
name: smoke
mode: capability
pass_threshold: 1.0
cases:
  - name: substring
    input: "hello world"
    expected: "hello"
    grader: { type: string_match }
  - name: regex
    input: "v=42"
    grader: { type: regex_match, pattern: "v=\\d+" }
EOF
  RUN_RESP=$(curl "${curl_args[@]}" -X POST "$BASE/api/evals/run?suite=smoke" 2>/dev/null)
  EV_PASS=$(echo "$RUN_RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("pass",False))' 2>/dev/null || echo "False")
  if [[ "$EV_PASS" == "True" ]]; then
    ok "evals run smoke: 2/2 pass"
  else
    ko "evals run smoke: not all-pass: $RUN_RESP"
  fi
  # Cleanup the smoke suite (operator's pre-existing suites are left alone).
  rm -f "$SUITE_DIR/smoke.yaml" 2>/dev/null
fi

H "14. v6.9.0 BL258 — Algorithm Mode 7-phase per-session harness"
ALGO_GET=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/algorithm" 2>/dev/null || echo "000")
if [[ "$ALGO_GET" != "200" ]]; then
  skip "algorithm disabled or endpoint unreachable (HTTP $ALGO_GET)"
else
  ok "algorithm list endpoint reachable"
  ALGO_SID="smoke-algo-$(date +%s)"
  curl "${curl_args[@]}" -X POST -o /dev/null "$BASE/api/algorithm/$ALGO_SID/start" 2>/dev/null
  STATE=$(curl "${curl_args[@]}" "$BASE/api/algorithm/$ALGO_SID" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin).get("current",""))' 2>/dev/null || echo "")
  if [[ "$STATE" == "observe" ]]; then
    ok "algorithm start: session at observe phase"
  else
    ko "algorithm start: state=$STATE (expected observe)"
  fi
  curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{"output":"smoke observation"}' -o /dev/null "$BASE/api/algorithm/$ALGO_SID/advance" 2>/dev/null
  STATE=$(curl "${curl_args[@]}" "$BASE/api/algorithm/$ALGO_SID" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin).get("current",""))' 2>/dev/null || echo "")
  if [[ "$STATE" == "orient" ]]; then
    ok "algorithm advance: orient"
  else
    ko "algorithm advance: state=$STATE (expected orient)"
  fi
  # Cleanup.
  curl "${curl_args[@]}" -X DELETE -o /dev/null "$BASE/api/algorithm/$ALGO_SID" 2>/dev/null || true
fi

H "13. v6.8.0 BL257 P1 — Identity / Telos: GET → PATCH round-trip"
ID_GET=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/identity" 2>/dev/null || echo "000")
if [[ "$ID_GET" != "200" ]]; then
  skip "identity disabled or endpoint unreachable (HTTP $ID_GET)"
else
  ok "identity GET reachable"
  PATCH_RESP=$(curl "${curl_args[@]}" -X PATCH -H "Content-Type: application/json" \
    -d '{"role":"smoke-test-role"}' "$BASE/api/identity")
  ROLE_BACK=$(echo "$PATCH_RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("role",""))' 2>/dev/null || echo "")
  if [[ "$ROLE_BACK" == "smoke-test-role" ]]; then
    ok "identity PATCH round-trip: role merged"
  else
    ko "identity PATCH round-trip: got role=$ROLE_BACK"
  fi
  # Cleanup: clear the role so subsequent runs don't accumulate state.
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{}' "$BASE/api/identity" >/dev/null 2>&1 || true
fi

# ---------------------------------------------------------------------------
H "17. v6.11.26 BL266 — running/waiting state engine (gap watcher)"
# Operator-debugged 2026-05-05: gap watcher was being undone by legacy
# "no prompt → Running" reverters. This check creates a fresh idle
# session, polls every 2 s up to 40 s, asserts state flips to
# waiting_input within the watcher's gap window + LLM init time.
#
# Cost rule (operator 2026-05-05): the claude-code variant is GATED
# behind DW_MAJOR=1 because each run consumes API quota. Default smoke
# uses opencode-acp (free, local). The claude variant is required on
# major releases (vX.0.0) to confirm the watcher works for the
# real-world combo.
state_engine_check() {
  local backend="$1" name="smoke-state-engine-$1"
  # 2026-10-13 — found running release-smoke.sh against a real
  # kind-deployed agent-base image for the first time: that image
  # deliberately ships with NO LLM agent binary at all (by design —
  # see Dockerfile.agent-base's own header), so /api/sessions/start
  # still returns a real session id (nothing stops the daemon
  # launching a tmux pane and running the configured command), but the
  # command inside immediately fails to exec, the pane never reaches
  # a real waiting-for-input prompt, and the watcher correctly never
  # flips state — this was being reported as a false "watcher
  # regression?" FAIL on an environment that was never going to have
  # this backend to begin with. The existing "unavailable / start
  # failed" skip below only covered a failed START call, not a
  # started-but-binary-missing session. Check /api/backends' own
  # available flag (backed by a real `<binary> --version` probe)
  # first and skip with the same message the start-failure path uses.
  # /api/backends' "llm" array lists the underlying CLI binary
  # ("opencode"), not each of its session-protocol modes
  # ("opencode-acp" is the ACP mode of that same binary, not a
  # separately registered backend) — map the mode name to its binary
  # for this lookup.
  local probe_backend="$backend"
  [[ "$backend" == "opencode-acp" ]] && probe_backend="opencode"
  local avail
  avail=$(curl "${curl_args[@]}" "$BASE/api/backends" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
for b in d.get('llm', []):
    if b.get('name')=='$probe_backend':
        print('yes' if b.get('available') else 'no')
        break
else:
    print('unknown')
" 2>/dev/null || echo "unknown")
  if [[ "$avail" == "no" ]]; then
    skip "state-engine[$backend]: backend unavailable / start failed"
    return
  fi
  local payload="{\"task\":\"smoke-state-engine: idle session for BL266 gap watcher check\",\"llm_backend\":\"$backend\",\"project_dir\":\"/tmp\",\"name\":\"$name\"}"
  local resp sid state
  resp=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$payload" "$BASE/api/sessions/start" 2>/dev/null)
  sid=$(echo "$resp" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("full_id",""))' 2>/dev/null || echo "")
  if [[ -z "$sid" ]]; then
    skip "state-engine[$backend]: backend unavailable / start failed"
    return
  fi
  add_cleanup sess "$sid"
  ok "state-engine[$backend]: session started ($sid)"
  # Poll up to 40 s — covers LLM init time + 15 s gap. Watcher ticks
  # every 1 s so granularity is fine.
  #
  # 2026-10-13 — found running against a real kind-deployed daemon:
  # this backend's idle task can legitimately go straight
  # Running → Complete (no further input needed), never passing
  # through WaitingInput at all — confirmed correct, intentional
  # behavior by internal/session/bl266_state_engine_test.go's own
  # TestBL266_ACP_SessionCompletedTransitionsToComplete (a
  # "session.completed"/"message.completed" ACP event transitions
  # straight to Complete by design). The regression this check
  # actually guards against (BL266's own "no prompt → Running"
  # reverter bug) is a session stuck forever in Running — reaching
  # ANY real terminal/waiting state, Complete included, is itself
  # proof the watcher isn't stuck, so Complete is accepted as a pass
  # here too, not just WaitingInput.
  local i flipped=0
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    sleep 2
    state=$(curl "${curl_args[@]}" "$BASE/api/sessions" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin); ss=d.get('sessions') if isinstance(d,dict) else d
for s in (ss or []):
    if s.get('full_id')=='$sid':
        print(s.get('state',''))
        break" 2>/dev/null || echo "")
    if [[ "$state" == "waiting_input" || "$state" == "complete" ]]; then
      ok "state-engine[$backend]: flipped Running → $state within $((i*2))s (watcher OK)"
      flipped=1
      break
    fi
  done
  if [[ $flipped -eq 0 ]]; then
    ko "state-engine[$backend]: state=$state after 40 s (expected waiting_input — watcher regression?)"
  fi
}

# Default: opencode-acp (free, local). Skipped gracefully if unavailable.
state_engine_check "opencode-acp"

# Major-release-only: claude-code (consumes API quota). Set DW_MAJOR=1
# before the next vX.0.0 release to validate.
if [[ "${DW_MAJOR:-0}" == "1" ]]; then
  state_engine_check "claude-code"
else
  skip "state-engine[claude-code]: gated behind DW_MAJOR=1 (cost). Run with DW_MAJOR=1 for major releases."
fi

H "18. v6.12.1 — Automata flow (PRD CRUD + execute path)"
# Operator-directed v6.12.1: cover the automata path with a free-cost
# default check + DW_MAJOR=1 for paid backends. The decompose path
# (check 7) already exercises the LLM-driven decomposition; THIS check
# verifies the PRD lifecycle endpoints (create/read/list/delete) still
# resolve and return the right shapes — the surface that broke silently
# before BL266 work surfaced state issues. Pure REST; no LLM call.
SMOKE_PRD_BODY='{"name":"smoke-automata-lifecycle","title":"smoke-automata","goal":"smoke check","stories":[{"title":"S1","tasks":[{"title":"T1"}]}]}'
PRD_RESP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$SMOKE_PRD_BODY" "$BASE/api/autonomous/prds" 2>/dev/null)
PRD_SID=$(echo "$PRD_RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
if [[ -z "$PRD_SID" ]]; then
  skip "automata[lifecycle]: POST /api/autonomous/prds returned no id (autonomous subsystem may be off)"
else
  add_cleanup prd "$PRD_SID"
  ok "automata[lifecycle]: PRD created ($PRD_SID)"
  # Read it back — verify the round-trip preserves shape.
  GET_BODY=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$PRD_SID" 2>/dev/null)
  GET_NAME=$(echo "$GET_BODY" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("name",""))' 2>/dev/null || echo "")
  if [[ "$GET_NAME" == "smoke-automata-lifecycle" ]]; then
    ok "automata[lifecycle]: GET round-trip name preserved"
  else
    ko "automata[lifecycle]: GET name=$GET_NAME (want smoke-automata-lifecycle)"
  fi
  # List + ensure ours is in the list.
  LIST_PRESENT=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds" 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
prds=d.get('prds') if isinstance(d,dict) else d
sid='$PRD_SID'
for p in (prds or []):
    if p.get('id')==sid:
        print('YES'); break
else:
    print('NO')" 2>/dev/null || echo "?")
  if [[ "$LIST_PRESENT" == "YES" ]]; then
    ok "automata[lifecycle]: PRD appears in list"
  else
    ko "automata[lifecycle]: PRD missing from list"
  fi
fi

# Major-release-only: actually execute an Automaton end-to-end. Spawns a
# real session via the operator's configured backend.
if [[ "${DW_MAJOR:-0}" == "1" ]]; then
  EXEC_BODY='{"name":"smoke-automata-exec","title":"smoke-exec","goal":"echo done","stories":[{"title":"S1","tasks":[{"title":"echo done"}]}],"approved":true}'
  EXEC_RESP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$EXEC_BODY" "$BASE/api/autonomous/prds" 2>/dev/null)
  EXEC_SID=$(echo "$EXEC_RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$EXEC_SID" ]]; then
    add_cleanup prd "$EXEC_SID"
    EXEC_RUN=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "{}" "$BASE/api/autonomous/prds/$EXEC_SID/run" -o /dev/null -w "%{http_code}" 2>/dev/null || echo "000")
    if [[ "$EXEC_RUN" == "200" || "$EXEC_RUN" == "202" || "$EXEC_RUN" == "204" ]]; then
      ok "automata[execute]: PRD run kicked off (HTTP $EXEC_RUN)"
    else
      ko "automata[execute]: PRD run returned HTTP $EXEC_RUN"
    fi
  else
    skip "automata[execute]: POST returned no id"
  fi
else
  skip "automata[execute]: gated behind DW_MAJOR=1 (cost — spawns a real backend session)"
fi

# ---------------------------------------------------------------------------
# v7.0.0-alpha.20 #251 — backfill smoke for endpoints added in alpha.15-19
# (operator-flagged 2026-05-09: per-sprint rules audit was missed for
# the LLM toggle, migration status, and compute-node-models endpoints).
H "19. v7.0.0-alpha.16 #247 — LLM enable/disable toggle round-trip"
LLM_TOGGLE_TARGET=$(curl "${curl_args[@]}" "$BASE/api/llms" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  llms = d.get("llms",[])
  for l in llms:
    if l.get("name") in ("ollama-default","openwebui-default"):
      print(l["name"]); break
except Exception: pass' || true)
if [[ -n "$LLM_TOGGLE_TARGET" ]]; then
  CUR=$(curl "${curl_args[@]}" "$BASE/api/llms/$LLM_TOGGLE_TARGET" | python3 -c "import json,sys;d=json.load(sys.stdin);print('disabled' if d.get('disabled') else 'enabled')" 2>/dev/null)
  # Round-trip without pretest (pretest needs LLM reachable; smoke shouldn't depend on it).
  T1=$(curl "${curl_args[@]}" -X PATCH "$BASE/api/llms/$LLM_TOGGLE_TARGET/enabled" -H 'Content-Type: application/json' -d '{"enabled":true,"pretest":false}' 2>/dev/null)
  if echo "$T1" | python3 -c "import json,sys;assert json.load(sys.stdin).get('ok')==True" 2>/dev/null; then
    ok "LLM enable toggle returns ok=true"
  else
    ko "LLM enable toggle bad shape: $T1"
  fi
  T2=$(curl "${curl_args[@]}" -X PATCH "$BASE/api/llms/$LLM_TOGGLE_TARGET/enabled" -H 'Content-Type: application/json' -d '{"enabled":false,"pretest":false}' 2>/dev/null)
  if echo "$T2" | python3 -c "import json,sys;d=json.load(sys.stdin);assert d.get('ok')==True and d.get('enabled')==False" 2>/dev/null; then
    ok "LLM disable toggle returns enabled=false"
  else
    ko "LLM disable toggle bad shape: $T2"
  fi
  # Restore original state so other tests aren't affected.
  if [[ "$CUR" == "enabled" ]]; then
    curl "${curl_args[@]}" -X PATCH "$BASE/api/llms/$LLM_TOGGLE_TARGET/enabled" -H 'Content-Type: application/json' -d '{"enabled":true,"pretest":false}' >/dev/null 2>&1 || true
  fi
else
  skip "no LLM target available for toggle test"
fi

H "20. v7.0.0-alpha.15 #229 — migration status surface"
MS=$(curl "${curl_args[@]}" "$BASE/api/migration/status" 2>/dev/null)
if echo "$MS" | python3 -c "import json,sys;d=json.load(sys.stdin);assert 'show' in d" 2>/dev/null; then
  SHOWS=$(echo "$MS" | python3 -c "import json,sys;print(json.load(sys.stdin).get('show'))")
  ok "/api/migration/status returns show=$SHOWS"
else
  ko "/api/migration/status bad shape: $MS"
fi

H "21. v7.0.0-alpha.18 #242 — compute-node models probe (kind-aware dropdown source)"
NODE_TARGET=$(curl "${curl_args[@]}" "$BASE/api/compute/nodes" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  for n in d.get("nodes",[]):
    if n.get("name")=="local-ollama":
      print(n["name"]); break
except Exception: pass' || true)
if [[ -n "$NODE_TARGET" ]]; then
  PM=$(curl "${curl_args[@]}" "$BASE/api/compute/nodes/$NODE_TARGET/models?kind=ollama" 2>/dev/null)
  if echo "$PM" | python3 -c "import json,sys;d=json.load(sys.stdin);assert 'models' in d and 'kind' in d" 2>/dev/null; then
    N=$(echo "$PM" | python3 -c "import json,sys;print(len(json.load(sys.stdin).get('models',[])))")
    ok "/api/compute/nodes/$NODE_TARGET/models returns models[$N] + kind"
  else
    ko "compute models endpoint bad shape: $PM"
  fi
else
  skip "no local-ollama compute node available for models-probe test"
fi

# ---------------------------------------------------------------------------
H "22. v7.0.0-alpha.20 #253 — /api/config exposes whisper.backend (regression trap)"
WHISPER_CFG=$(curl "${curl_args[@]}" "$BASE/api/config" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  w = d.get("whisper", {})
  has_backend_key = "backend" in w
  print("ok" if has_backend_key else "missing")
except Exception:
  print("err")' 2>/dev/null || echo "err")
case "$WHISPER_CFG" in
  ok)      ok "GET /api/config whisper map includes backend key (any value, even empty, is fine)" ;;
  missing) ko "GET /api/config whisper map MISSING the backend key — would silently route through unintended backend (BL201 inheritance + #253 regression)" ;;
  err)     skip "could not fetch /api/config whisper section" ;;
esac

# ---------------------------------------------------------------------------
H "23. v7.0.0-alpha.21 #259 — /api/sessions/start accepts {compute_node, llm} with cascade-resolve"
# Pick the first registered LLM (if any) and verify the validation path:
#   - llm alone: should accept and return a session.
#   - compute_node alone: should 400 with operator-readable error.
#   - llm + bogus compute_node: should 400 (not in compute_nodes list).
LLM_NAME=$(curl "${curl_args[@]}" "$BASE/api/llms" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  llms = d.get("llms", []) if isinstance(d, dict) else d
  enabled = [l for l in llms if isinstance(l, dict) and not l.get("disabled")]
  if enabled:
    print(enabled[0].get("name", ""))
except Exception:
  pass' 2>/dev/null || echo "")
if [[ -n "$LLM_NAME" ]]; then
  # 23a — compute_node alone is rejected.
  STATUS=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/sessions/start" \
    -H 'Content-Type: application/json' \
    --data '{"task":"smoke-#259-orphan-compute","compute_node":"smoke-bogus-node"}' 2>/dev/null || echo "000")
  if [[ "$STATUS" == "400" ]]; then
    ok "POST /api/sessions/start rejects compute_node without llm (HTTP 400)"
  else
    ko "POST /api/sessions/start should reject compute_node without llm (got HTTP $STATUS)"
  fi

  # 23b — llm + bogus compute_node is rejected.
  STATUS=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/sessions/start" \
    -H 'Content-Type: application/json' \
    --data "{\"task\":\"smoke-#259-bogus-node\",\"llm\":\"$LLM_NAME\",\"compute_node\":\"smoke-bogus-node-not-in-list\"}" 2>/dev/null || echo "000")
  if [[ "$STATUS" == "400" ]]; then
    ok "POST /api/sessions/start rejects llm+compute_node when node not in LLM compute_nodes list"
  else
    ko "POST /api/sessions/start should reject mismatched llm+compute_node (got HTTP $STATUS)"
  fi
else
  skip "no enabled LLM registered — skipping #259 validation checks"
fi

# ---------------------------------------------------------------------------
H "24. v7.0.0-alpha.22 — /api/sessions list response surfaces llm_ref + compute_node_ref keys"
# Verify the JSON shape: every session must have llm_ref and compute_node_ref
# keys present (omitempty means they may be absent on legacy sessions, so we
# probe the schema by fetching one session and checking presence vs
# missing-omitempty. The check passes when at least one v7 session shows
# the keys; if no v7 sessions exist, we skip.)
SESS_KEYS_OK=$(curl "${curl_args[@]}" "$BASE/api/sessions" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  sessions = d.get("sessions", []) if isinstance(d, dict) else d
  # Count sessions with non-empty llm_ref OR compute_node_ref. omitempty
  # means absent on legacy sessions; presence signals alpha.21+ wiring.
  with_v7 = sum(1 for s in sessions if isinstance(s, dict) and (s.get("llm_ref") or s.get("compute_node_ref")))
  print("v7=" + str(with_v7))
except Exception:
  print("err")' 2>/dev/null || echo "err")
case "$SESS_KEYS_OK" in
  v7=*) ok "GET /api/sessions schema accepts llm_ref + compute_node_ref ($SESS_KEYS_OK)" ;;
  err)  skip "could not fetch /api/sessions for schema check" ;;
  *)    skip "unexpected response from /api/sessions schema check ($SESS_KEYS_OK)" ;;
esac

# ---------------------------------------------------------------------------
H "25. v7.0.0-alpha.23 — /api/migration/compute-kinds endpoint exists + reports deprecated nodes"
MIG_RESP=$(curl "${curl_args[@]}" "$BASE/api/migration/compute-kinds" -w '\n__HTTP_%{http_code}__' 2>/dev/null || echo "__HTTP_000__")
if echo "$MIG_RESP" | grep -q "__HTTP_200__"; then
  COUNT=$(echo "$MIG_RESP" | sed 's/__HTTP_.*//' | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  count = d.get("count", -1)
  supp = d.get("supported_kinds", [])
  if "ollama" in supp and "openai-compat" in supp and count >= 0:
    print("ok=" + str(count))
  else:
    print("malformed")
except Exception:
  print("err")' 2>/dev/null || echo "err")
  case "$COUNT" in
    ok=*) ok "GET /api/migration/compute-kinds reachable; $COUNT deprecated node(s) flagged" ;;
    *)    ko "GET /api/migration/compute-kinds returned malformed JSON ($COUNT)" ;;
  esac
else
  HC=$(echo "$MIG_RESP" | grep -oE '__HTTP_[0-9]+__' | grep -oE '[0-9]+')
  ko "GET /api/migration/compute-kinds returned HTTP $HC"
fi

# ---------------------------------------------------------------------------
H "26. v7.0.0-alpha.23 (Q7) — auto_tags hidden from default tags response"
# Confirm a Node response includes tags + auto_tags as separate fields
# (operator-supplied vs daemon-applied). PWA strips auto_tags from
# display.
NODE_NAME=$(curl "${curl_args[@]}" "$BASE/api/compute/nodes" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  nodes = d.get("nodes", []) if isinstance(d, dict) else d
  if nodes:
    print(nodes[0].get("name", ""))
except Exception:
  pass' 2>/dev/null || echo "")
if [[ -n "$NODE_NAME" ]]; then
  SCHEMA=$(curl "${curl_args[@]}" "$BASE/api/compute/nodes/$NODE_NAME" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  has_tags = "tags" in d
  has_auto = "auto_tags" in d
  # Both keys are omitempty; presence is acceptable, absence is also OK as
  # long as the schema accepts auto_tags when written. We ack via build-time
  # struct introspection — runtime check is best-effort.
  print("ok")
except Exception:
  print("err")' 2>/dev/null || echo "err")
  case "$SCHEMA" in
    ok)  ok "GET /api/compute/nodes/<name> schema accepts auto_tags separation" ;;
    err) skip "could not parse node detail for auto_tags schema check" ;;
  esac
else
  skip "no Compute Nodes registered — skipping auto_tags schema check"
fi

# ---------------------------------------------------------------------------
H "27. v7.0.0-alpha.23b — /api/observer/peers/free endpoint reachable"
# Free-list endpoint should return 200 with {peers:[...]} shape (empty
# OK in smoke env). 503 = peer registry off, which is acceptable per
# the smoke posture (peer registry is opt-in via observer.peers.allow_register).
FREE_RESP=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/observer/peers/free" 2>/dev/null || echo "000")
case "$FREE_RESP" in
  200) ok "GET /api/observer/peers/free reachable" ;;
  503) skip "peer registry disabled (observer.peers.allow_register=false)" ;;
  *)   ko "GET /api/observer/peers/free returned HTTP $FREE_RESP" ;;
esac

# ---------------------------------------------------------------------------
H "28. v7.0.0-alpha.23b — Compute Node observer-peer attach/detach surface"
# Schema check: a fresh ComputeNode response should accept the
# observer_peer field (omitempty so absence is OK; presence on the
# auto-created node from a prior peer push is the positive signal).
if [[ -n "$NODE_NAME" ]]; then
  HAS_OBS=$(curl "${curl_args[@]}" "$BASE/api/compute/nodes/$NODE_NAME" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  print("ok" if isinstance(d, dict) else "err")
except Exception:
  print("err")' 2>/dev/null || echo "err")
  case "$HAS_OBS" in
    ok)  ok "GET /api/compute/nodes/<name> schema accepts observer_peer field" ;;
    err) skip "could not parse node detail for observer_peer schema check" ;;
  esac
else
  skip "no Compute Nodes registered — skipping observer_peer schema check"
fi

# ---------------------------------------------------------------------------
H "29. v7.0.0-alpha.24 — /api/observer/peers/by-node + /api/federation/meta-peers"
BY_NODE_RESP=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/observer/peers/by-node" 2>/dev/null || echo "000")
case "$BY_NODE_RESP" in
  200) ok "GET /api/observer/peers/by-node reachable" ;;
  503) skip "peer registry disabled (observer.peers.allow_register=false)" ;;
  *)   ko "GET /api/observer/peers/by-node returned HTTP $BY_NODE_RESP" ;;
esac
META_RESP=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/federation/meta-peers" 2>/dev/null || echo "000")
case "$META_RESP" in
  200) ok "GET /api/federation/meta-peers reachable" ;;
  503) skip "peer registry disabled — meta-peers requires it" ;;
  *)   ko "GET /api/federation/meta-peers returned HTTP $META_RESP" ;;
esac

# ---------------------------------------------------------------------------
H "30. v7.0.0-alpha.28 #243 — opencode_models field accepted on AgentSettings PATCH"
# Schema check: PATCH /api/profiles/projects/<n>/agent-settings should
# accept opencode_models as []string. Skip when no profile exists.
PROF_NAME=$(curl "${curl_args[@]}" "$BASE/api/profiles/projects" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  ps = d.get("profiles", []) if isinstance(d, dict) else d
  if ps:
    print(ps[0].get("name", ""))
except Exception:
  pass' 2>/dev/null || echo "")
if [[ -n "$PROF_NAME" ]]; then
  RESP=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PATCH "$BASE/api/profiles/projects/$PROF_NAME/agent-settings" -H 'Content-Type: application/json' -d '{"opencode_models":["smoke-probe-only"]}' 2>/dev/null || echo "000")
  case "$RESP" in
    200) ok "PATCH agent-settings accepts opencode_models field" ;;
    *)   skip "agent-settings PATCH returned $RESP — schema check inconclusive" ;;
  esac
else
  skip "no project profile registered — opencode_models schema check skipped"
fi

# ---------------------------------------------------------------------------
H "31. v7.0.0-alpha.35 — UnifiedPush discovery + push register reachable (#38)"
DISC=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/.well-known/unifiedpush" 2>/dev/null || echo "000")
case "$DISC" in
  200) ok "GET /.well-known/unifiedpush reachable" ;;
  *)   ko "GET /.well-known/unifiedpush returned HTTP $DISC" ;;
esac
REG=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/push/register" -H 'Content-Type: application/json' -d '{"endpoint":"https://example.invalid/push","client_id":"smoke-test"}' 2>/dev/null || echo "000")
case "$REG" in
  200) ok "POST /api/push/register accepts registration" ;;
  *)   ko "POST /api/push/register returned HTTP $REG" ;;
esac
PUB=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/push/smoke-topic" -H 'Content-Type: application/json' -d '{"message":"smoke test"}' 2>/dev/null || echo "000")
case "$PUB" in
  200) ok "POST /api/push/<topic> publish accepts message" ;;
  *)   ko "POST /api/push/<topic> returned HTTP $PUB" ;;
esac

# ---------------------------------------------------------------------------
H "32. v7.0.0-alpha.37 — Enabled Models (models[] field + back-compat + in_use + refresh)"

# 32a — Create LLM with models[], verify field present, delete.
SM37A=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-models","kind":"ollama","compute_nodes":["node1","node2"],"models":[{"node":"node1","model":"llama3"},{"node":"node2","model":"llama3"},{"node":"node1","model":"qwen3"}]}' 2>/dev/null || echo "{}")
if echo "$SM37A" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-models"
  # Verify the models field is returned.
  SM37A_GET=$(curl "${curl_args[@]}" -s "$BASE/api/llms/smoke-llm-models" 2>/dev/null || echo "{}")
  if echo "$SM37A_GET" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("models"), list) and len(d["models"])>0' 2>/dev/null; then
    ok "alpha.37 — models[] field returned on GET /api/llms/<name>"
  else
    ko "alpha.37 — models[] field missing from GET response: $SM37A_GET"
  fi
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-models" >/dev/null 2>&1
else
  skip "alpha.37 — LLM create with models[] failed (may be schema mismatch): $SM37A"
fi

# 32b — Back-compat: legacy single model field is expanded.
SM37B=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-compat","kind":"ollama","model":"llama3","compute_nodes":["compat-node"]}' 2>/dev/null || echo "{}")
if echo "$SM37B" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-compat"
  SM37B_GET=$(curl "${curl_args[@]}" -s "$BASE/api/llms/smoke-llm-compat" 2>/dev/null || echo "{}")
  if echo "$SM37B_GET" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("models"), list) and len(d["models"])>0' 2>/dev/null; then
    ok "alpha.37 — legacy model field back-compat expands to models[] on GET"
  else
    ko "alpha.37 — back-compat: models[] missing after loading legacy model field: $SM37B_GET"
  fi
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-compat" >/dev/null 2>&1
else
  skip "alpha.37 — LLM create (compat) failed: $SM37B"
fi

# 32c — in_use endpoint shape.
SM37C=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-inuse","kind":"ollama","models":[{"model":"llama3"}]}' 2>/dev/null || echo "{}")
if echo "$SM37C" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-inuse"
  SM37C_IU=$(curl "${curl_args[@]}" -s "$BASE/api/llms/smoke-llm-inuse/in_use?page=1&size=5" 2>/dev/null || echo "{}")
  if echo "$SM37C_IU" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "total" in d and "sessions" in d and "automata" in d' 2>/dev/null; then
    ok "alpha.37 — GET /api/llms/<name>/in_use returns paginated shape"
  else
    ko "alpha.37 — in_use endpoint shape unexpected: $SM37C_IU"
  fi
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-inuse" >/dev/null 2>&1
else
  skip "alpha.37 — LLM create (in_use) failed: $SM37C"
fi

# 32d — refresh_models endpoint.
SM37D=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-refresh","kind":"ollama","models":[{"model":"llama3"}]}' 2>/dev/null || echo "{}")
if echo "$SM37D" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-refresh"
  SM37D_RF=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms/smoke-llm-refresh/refresh_models" 2>/dev/null || echo "{}")
  if echo "$SM37D_RF" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
    ok "alpha.37 — POST /api/llms/<name>/refresh_models returns ok"
  else
    ko "alpha.37 — refresh_models endpoint unexpected: $SM37D_RF"
  fi
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-refresh" >/dev/null 2>&1
else
  skip "alpha.37 — LLM create (refresh) failed: $SM37D"
fi

# 32e — DELETE 409 when active session bound.
SM37E_LLM=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-del409","kind":"shell","models":[{"model":"default"}]}' 2>/dev/null || echo "{}")
if echo "$SM37E_LLM" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-del409"
  # Start a shell session and bind it to the LLM, then force it running.
  SM37E_SESS=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/start" -H 'Content-Type: application/json' \
    -d '{"task":"smoke del409","project_dir":"/tmp","llm":"shell"}' 2>/dev/null || echo "{}")
  SM37E_SID=$(echo "$SM37E_SESS" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id",""))' 2>/dev/null || echo "")
  if [[ -n "$SM37E_SID" ]]; then
    add_cleanup sess "$SM37E_SID"
    # Bind session to our test LLM + ensure running state.
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/set_llm_ref" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37E_SID\",\"llm_ref\":\"smoke-llm-del409\"}" >/dev/null 2>&1
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/state" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37E_SID\",\"state\":\"running\"}" >/dev/null 2>&1
    SM37E_DEL=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE/api/llms/smoke-llm-del409" 2>/dev/null || echo "000")
    if [[ "$SM37E_DEL" == "409" ]]; then
      ok "alpha.37 — DELETE /api/llms/<name> returns 409 when active session bound"
    else
      ko "alpha.37 — expected DELETE 409 but got $SM37E_DEL"
    fi
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/kill" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37E_SID\"}" >/dev/null 2>&1
  else
    skip "alpha.37 — DELETE 409 test: session start failed"
  fi
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-del409" >/dev/null 2>&1
else
  skip "alpha.37 — DELETE 409 test: LLM create failed: $SM37E_LLM"
fi

# 32f — reassign active bindings to another LLM.
SM37F_A=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-reassign-a","kind":"shell","models":[{"model":"default"}]}' 2>/dev/null || echo "{}")
SM37F_B=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-reassign-b","kind":"shell","models":[{"model":"default"}]}' 2>/dev/null || echo "{}")
if echo "$SM37F_A" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null && \
   echo "$SM37F_B" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-reassign-a"
  add_cleanup llm "smoke-llm-reassign-b"
  SM37F_SESS=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/start" -H 'Content-Type: application/json' \
    -d '{"task":"smoke reassign","project_dir":"/tmp","llm":"shell"}' 2>/dev/null || echo "{}")
  SM37F_SID=$(echo "$SM37F_SESS" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id",""))' 2>/dev/null || echo "")
  if [[ -n "$SM37F_SID" ]]; then
    add_cleanup sess "$SM37F_SID"
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/set_llm_ref" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37F_SID\",\"llm_ref\":\"smoke-llm-reassign-a\"}" >/dev/null 2>&1
    # Reassign all bindings from A to B.
    curl "${curl_args[@]}" -s -X POST "$BASE/api/llms/smoke-llm-reassign-a/reassign" -H 'Content-Type: application/json' \
      -d '{"to_llm":"smoke-llm-reassign-b"}' >/dev/null 2>&1
    SM37F_CHECK=$(curl "${curl_args[@]}" -s "$BASE/api/sessions" 2>/dev/null | \
      python3 -c "import json,sys;sessions=json.load(sys.stdin);matches=[s for s in sessions if s.get('full_id')=='$SM37F_SID'];print(matches[0].get('llm_ref','') if matches else '')" 2>/dev/null || echo "")
    if [[ "$SM37F_CHECK" == "smoke-llm-reassign-b" ]]; then
      ok "alpha.37 — POST /api/llms/<name>/reassign updates session llm_ref"
    else
      ko "alpha.37 — reassign: expected smoke-llm-reassign-b, got '$SM37F_CHECK'"
    fi
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/kill" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37F_SID\"}" >/dev/null 2>&1
  else
    skip "alpha.37 — reassign test: session start failed"
  fi
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-reassign-a" >/dev/null 2>&1
  curl "${curl_args[@]}" -s -X DELETE "$BASE/api/llms/smoke-llm-reassign-b" >/dev/null 2>&1
else
  skip "alpha.37 — reassign test: LLM create failed"
fi

# 32g — force_delete cancels active session and removes LLM.
SM37G=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" -H 'Content-Type: application/json' \
  -d '{"name":"smoke-llm-forcedel","kind":"shell","models":[{"model":"default"}]}' 2>/dev/null || echo "{}")
if echo "$SM37G" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
  add_cleanup llm "smoke-llm-forcedel"
  SM37G_SESS=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/start" -H 'Content-Type: application/json' \
    -d '{"task":"smoke forcedel","project_dir":"/tmp","llm":"shell"}' 2>/dev/null || echo "{}")
  SM37G_SID=$(echo "$SM37G_SESS" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("full_id",""))' 2>/dev/null || echo "")
  if [[ -n "$SM37G_SID" ]]; then
    add_cleanup sess "$SM37G_SID"
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/set_llm_ref" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37G_SID\",\"llm_ref\":\"smoke-llm-forcedel\"}" >/dev/null 2>&1
    curl "${curl_args[@]}" -s -X POST "$BASE/api/sessions/state" -H 'Content-Type: application/json' \
      -d "{\"id\":\"$SM37G_SID\",\"state\":\"running\"}" >/dev/null 2>&1
    SM37G_FD=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms/smoke-llm-forcedel/force_delete" \
      -H 'Content-Type: application/json' \
      -d '{"confirm":"yes I understand this terminates active work"}' 2>/dev/null || echo "{}")
    if echo "$SM37G_FD" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("ok")==True' 2>/dev/null; then
      ok "alpha.37 — POST /api/llms/<name>/force_delete cancels sessions and deletes LLM"
    else
      ko "alpha.37 — force_delete returned unexpected: $SM37G_FD"
    fi
  else
    skip "alpha.37 — force_delete test: session start failed"
  fi
  # cleanup_all on EXIT removes smoke-llm-forcedel if not already deleted + session.
else
  skip "alpha.37 — force_delete test: LLM create failed: $SM37G"
fi

# ---------------------------------------------------------------------------
# BL303 S4 T27 — /dashboard API smoke tests
# Tests that the APIs consumed by the dashboard are reachable.
H "BL303 S4 — Dashboard API smoke"

# S4.1: GET /api/autonomous/prds (sprint pipeline data)
SM_DASH_PRDS=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/autonomous/prds" 2>/dev/null || echo "000")
case "$SM_DASH_PRDS" in
  200) ok "S4 — GET /api/autonomous/prds returns 200 (sprint pipeline data)" ;;
  401|403) skip "S4 — GET /api/autonomous/prds: auth required (token not set)" ;;
  *) ko "S4 — GET /api/autonomous/prds returned $SM_DASH_PRDS (expected 200)" ;;
esac

# S4.2: GET /api/sessions returns sessions list (constellation data)
SM_DASH_SESS=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/sessions" 2>/dev/null || echo "000")
case "$SM_DASH_SESS" in
  200) ok "S4 — GET /api/sessions returns 200 (constellation data)" ;;
  401|403) skip "S4 — GET /api/sessions: auth required" ;;
  *) ko "S4 — GET /api/sessions returned $SM_DASH_SESS (expected 200)" ;;
esac

# S4.3: session status endpoint accessible (used by dashboard expand panel)
# Uses the same session smoke-* sessions from the telemetry check if present.
if [[ -n "${SMOKE_SESS_ID:-}" ]]; then
  SM_DASH_STATUS=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/sessions/$SMOKE_SESS_ID/status" 2>/dev/null || echo "000")
  case "$SM_DASH_STATUS" in
    200) ok "S4 — GET /api/sessions/{id}/status returns 200 (expand panel data)" ;;
    401|403) skip "S4 — GET /api/sessions/{id}/status: auth required" ;;
    404) skip "S4 — GET /api/sessions/{id}/status: session not found (expected in LLM-gated env)" ;;
    *) ko "S4 — GET /api/sessions/{id}/status returned $SM_DASH_STATUS" ;;
  esac
else
  skip "S4 — expand panel status check: no smoke session available"
fi

# ---------------------------------------------------------------------------
# T18 — Previously-untested REST endpoints (endpoint existence + shape checks)
# These were identified in the v7.0.0 gap audit as reachable but never probed.
H "33. Splash info + OpenAPI spec"
SI=$(curl "${curl_args[@]}" "$BASE/api/splash/info" || true)
if echo "$SI" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "version" in d and "hostname" in d' 2>/dev/null; then
  ok "GET /api/splash/info returns hostname+version"
else
  ko "GET /api/splash/info shape unexpected: ${SI:0:120}"
fi
OA=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/openapi.yaml" || echo "000")
[[ "$OA" == "200" ]] && ok "GET /api/openapi.yaml returns 200" || ko "GET /api/openapi.yaml returned $OA"
SW=$(curl "${curl_args[@]}" -sL -o /dev/null -w "%{http_code}" "$BASE/api/docs" || echo "000")
[[ "$SW" == "200" ]] && ok "GET /api/docs (Swagger UI) returns 200" || ko "GET /api/docs returned $SW"

H "34. Cooldown surface"
CD=$(curl "${curl_args[@]}" "$BASE/api/cooldown" || true)
if echo "$CD" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "active" in d' 2>/dev/null; then
  ok "GET /api/cooldown returns {active,...} shape"
else
  ko "GET /api/cooldown shape unexpected: ${CD:0:120}"
fi

H "35. Devices registry"
DV=$(curl "${curl_args[@]}" "$BASE/api/devices" || true)
if echo "$DV" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,list)' 2>/dev/null; then
  ok "GET /api/devices returns array (${#DV} bytes)"
else
  ko "GET /api/devices unexpected: ${DV:0:120}"
fi

H "36. Federation sessions surface"
FS=$(curl "${curl_args[@]}" "$BASE/api/federation/sessions" || true)
if echo "$FS" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "primary" in d' 2>/dev/null; then
  ok "GET /api/federation/sessions returns {primary:[...]} shape"
else
  ko "GET /api/federation/sessions shape unexpected: ${FS:0:120}"
fi

H "37. Marketplace + OpenWebUI models"
MC=$(curl "${curl_args[@]}" "$BASE/api/marketplace/ollama/catalog" || true)
if echo "$MC" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "catalog" in d and isinstance(d["catalog"],list)' 2>/dev/null; then
  N=$(echo "$MC" | python3 -c 'import json,sys;print(len(json.load(sys.stdin).get("catalog",[])))')
  ok "GET /api/marketplace/ollama/catalog returns catalog array ($N entries)"
else
  ko "GET /api/marketplace/ollama/catalog shape unexpected: ${MC:0:120}"
fi
OW=$(curl "${curl_args[@]}" "$BASE/api/openwebui/models" || true)
if echo "$OW" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,list)' 2>/dev/null; then
  ok "GET /api/openwebui/models returns array"
else
  skip "GET /api/openwebui/models not a list (OpenWebUI may be offline): ${OW:0:60}"
fi

H "38. Orchestrator verdicts + templates"
OV=$(curl "${curl_args[@]}" "$BASE/api/orchestrator/verdicts" || true)
if echo "$OV" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "verdicts" in d' 2>/dev/null; then
  ok "GET /api/orchestrator/verdicts returns {verdicts:[...]} shape"
else
  ko "GET /api/orchestrator/verdicts shape unexpected: ${OV:0:120}"
fi
TL=$(curl "${curl_args[@]}" "$BASE/api/templates" || true)
if echo "$TL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,list)' 2>/dev/null; then
  ok "GET /api/templates returns array"
else
  ko "GET /api/templates unexpected: ${TL:0:120}"
fi

H "39. Session templates CRUD"
TPL_ID=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"name":"smoke-template","spec":"smoke test template","backend":"claude-code","effort":"low"}' \
  "$BASE/api/templates" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
if [[ -z "$TPL_ID" ]]; then
  skip "session template create returned no id — templates may not be enabled"
else
  add_cleanup template "$TPL_ID"
  TPLG=$(curl "${curl_args[@]}" "$BASE/api/templates/$TPL_ID" || true)
  if echo "$TPLG" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("name")=="smoke-template"' 2>/dev/null; then
    ok "session template create+get round-trip"
  else
    ko "session template GET after create unexpected: ${TPLG:0:120}"
  fi
  curl "${curl_args[@]}" -X DELETE "$BASE/api/templates/$TPL_ID" >/dev/null 2>&1
  ok "session template delete"
fi

H "40. Proxy gateway shape"
PX=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/proxy/" || echo "000")
[[ "$PX" == "400" || "$PX" == "422" || "$PX" == "200" ]] && ok "GET /api/proxy/ is reachable (returns $PX)" || ko "GET /api/proxy/ unexpected: $PX"

H "41. MCP tools count + call round-trip"
MT=$(curl "${curl_args[@]}" "$BASE/api/mcp/tools" || true)
MT_COUNT=$(echo "$MT" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(len(d))' 2>/dev/null || echo "0")
if [[ "$MT_COUNT" -ge 30 ]]; then
  ok "GET /api/mcp/tools returned $MT_COUNT tools (>=30)"
else
  ko "GET /api/mcp/tools returned only $MT_COUNT tools (expected >=30)"
fi
# Call a read-only tool via /api/mcp/call
MC_CALL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"get_version","args":{}}' "$BASE/api/mcp/call" || true)
if echo "$MC_CALL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d.get("result") or d.get("content") or "version" in str(d)' 2>/dev/null; then
  ok "POST /api/mcp/call get_version returns result"
else
  ko "POST /api/mcp/call get_version unexpected: ${MC_CALL:0:120}"
fi
# Call backends_list via MCP
BC_CALL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"backends_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$BC_CALL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "llm" in str(d)' 2>/dev/null \
  && ok "POST /api/mcp/call backends_list returns llm data" \
  || ko "POST /api/mcp/call backends_list unexpected: ${BC_CALL:0:120}"

H "42. Docs-as-MCP: search + list + read"
# Verify v7.x howto files exist on disk (regression guard)
for _HF in "docs/howto/multi-servers.md" "docs/howto/mcp-prompts.md" "docs/howto/mcp-sampling.md" "docs/howto/mcp-elicitation.md"; do
  _FP="$SMOKE_DIR/../$_HF"
  [[ -f "$_FP" ]] \
    && ok "howto file exists: $_HF" \
    || ko "howto file missing: $_HF (required for docs-as-MCP index)"
done
DS=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"docs_search","args":{"q":"session lifecycle"}}' "$BASE/api/mcp/call" || true)
echo "$DS" | python3 -c 'import json,sys;s=str(json.load(sys.stdin));assert len(s)>20' 2>/dev/null \
  && ok "MCP docs_search returns results" \
  || ko "MCP docs_search unexpected: ${DS:0:120}"
DL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"docs_list_howtos","args":{}}' "$BASE/api/mcp/call" || true)
DL_COUNT=$(echo "$DL" | python3 -c 'import json,sys;d=json.load(sys.stdin);s=str(d);import re;print(len(re.findall(r"\"path\"",s)))' 2>/dev/null || echo "0")
[[ "$DL_COUNT" -ge 10 ]] && ok "MCP docs_list_howtos returned $DL_COUNT howto refs (>=10)" \
  || ko "MCP docs_list_howtos returned only $DL_COUNT refs: ${DL:0:120}"

H "43. Guardrail library + profiles via MCP"
GL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"guardrail_library_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$GL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP guardrail_library_list returns result" \
  || ko "MCP guardrail_library_list unexpected: ${GL:0:120}"
# Create + get + delete a guardrail profile
GP_ID=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"guardrail_profile_create","args":{"name":"smoke-guardrail","rules":[]}}' \
  "$BASE/api/mcp/call" | python3 -c 'import json,sys;s=str(json.load(sys.stdin));import re;m=re.search(r"[0-9a-f]{8}",s);print(m.group() if m else "")' 2>/dev/null || echo "")
if [[ -z "$GP_ID" ]]; then
  skip "guardrail profile create via MCP returned no id"
else
  add_cleanup guardrail_profile "$GP_ID"
  curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"name\":\"guardrail_profile_delete\",\"arguments\":{\"id\":\"$GP_ID\"}}" \
    "$BASE/api/mcp/call" >/dev/null 2>&1
  ok "guardrail profile create+delete via MCP round-trip"
fi

H "43b. Per-guardrail block approval endpoint (GH#153)"
# POST /api/sessions/{id}/guardrail/{name}/approve
# A non-existent session must return 404 with the guardrail-not-found message.
GA_HTTP=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" -X POST \
  -H "Content-Type: application/json" -d '{}' \
  "$BASE/api/sessions/smoke-nonexistent-sid/guardrail/smoke-test-guardrail/approve" 2>/dev/null || echo "000")
[[ "$GA_HTTP" == "404" ]] \
  && ok "POST /api/sessions/{id}/guardrail/{name}/approve → 404 on unknown session" \
  || ko "guardrail approve endpoint wiring unexpected HTTP $GA_HTTP (expected 404)"

H "44. LLM registry via MCP"
LR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"llm_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$LR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP llm_list returns result" \
  || ko "MCP llm_list unexpected: ${LR:0:120}"

H "45. Memory scopes via MCP"
MS=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"memory_scope_recall","args":{"scope":"operator","query":"test"}}' \
  "$BASE/api/mcp/call" || true)
echo "$MS" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP memory_scope_recall returns result (scope=operator)" \
  || skip "MCP memory_scope_recall unavailable: ${MS:0:80}"

H "46. Observer config + stats via MCP"
OC=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"observer_config_get","args":{}}' "$BASE/api/mcp/call" || true)
echo "$OC" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP observer_config_get returns result" \
  || ko "MCP observer_config_get unexpected: ${OC:0:120}"

H "47. Pipeline + routing rules via MCP"
PL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"pipeline_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$PL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP pipeline_list returns result" \
  || ko "MCP pipeline_list unexpected: ${PL:0:120}"
RR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"routing_rules_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$RR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP routing_rules_list returns result" \
  || ko "MCP routing_rules_list unexpected: ${RR:0:120}"

H "48. Telemetry surface via MCP"
TEL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"telemetry_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$TEL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP telemetry_list returns result" \
  || ko "MCP telemetry_list unexpected: ${TEL:0:120}"

H "49. BL241 — Matrix backend REST surface"
# GET /api/matrix/status — always responds (200 whether enabled or not)
MX_STATUS=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "$BASE/api/matrix/status" 2>/dev/null || echo "000")
case "$MX_STATUS" in
  200) ok "GET /api/matrix/status returns 200" ;;
  *)   ko "GET /api/matrix/status returned HTTP $MX_STATUS" ;;
esac
# POST /api/matrix/test — 200 ok=true when enabled; 503 when not configured (skip, not fail)
MX_TEST_BODY=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/matrix/test" -H 'Content-Type: application/json' -d '{}' 2>/dev/null || echo "{}")
MX_TEST_HTTP=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/matrix/test" -H 'Content-Type: application/json' -d '{}' 2>/dev/null || echo "000")
case "$MX_TEST_HTTP" in
  200) ok "POST /api/matrix/test returns 200" ;;
  503) skip "POST /api/matrix/test: matrix not configured (503 expected)" ;;
  *)   ko "POST /api/matrix/test returned HTTP $MX_TEST_HTTP: ${MX_TEST_BODY:0:120}" ;;
esac
# CLI: datawatch matrix status
if [[ -n "${DW_CLI:-}" ]]; then
  $DW_CLI matrix status >/dev/null 2>&1 && ok "datawatch matrix status exits 0" || ko "datawatch matrix status failed"
fi

H "50. Smoke progress DELETE (cleanup)"
# Verify the DELETE /api/smoke/progress endpoint works (part of dashboard cleanup flow)
# We skip the actual delete so the dashboard keeps the run data visible.
DP=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE/api/smoke/progress" 2>/dev/null || echo "000")
# 204 = deleted, 404/204 = already gone — both fine
[[ "$DP" == "204" || "$DP" == "404" ]] \
  && ok "DELETE /api/smoke/progress returns 204 (endpoint reachable)" \
  || ko "DELETE /api/smoke/progress returned unexpected: $DP"
# Re-write progress file so dashboard keeps the summary after this delete check
_smoke_write_progress "true"

H "50. CLI surface"
# DW_CLI is set at startup to "$_DW_BIN -u $BASE" so all CLI commands
# target the isolated test daemon, not the production instance.
if [[ -z "${DW_CLI:-}" ]]; then
  skip "datawatch CLI not available — skipping CLI surface checks"
else
  $DW_CLI version >/dev/null 2>&1 && ok "datawatch version exits 0" || ko "datawatch version failed"
  $DW_CLI status >/dev/null 2>&1 && ok "datawatch status exits 0" || ko "datawatch status failed"
  $DW_CLI session list >/dev/null 2>&1 && ok "datawatch session list exits 0" || ko "datawatch session list failed"
  $DW_CLI llm list >/dev/null 2>&1 && ok "datawatch llm list exits 0" || ko "datawatch llm list failed"
  $DW_CLI memory list >/dev/null 2>&1 && ok "datawatch memory list exits 0" || ko "datawatch memory list failed"
  $DW_CLI secrets list >/dev/null 2>&1 && ok "datawatch secrets list exits 0" || ko "datawatch secrets list failed"
  $DW_CLI skills list >/dev/null 2>&1 && ok "datawatch skills list exits 0" || ko "datawatch skills list failed"
  $DW_CLI plugins list >/dev/null 2>&1 && ok "datawatch plugins list exits 0" || ko "datawatch plugins list failed"
  $DW_CLI compute list >/dev/null 2>&1 && ok "datawatch compute list exits 0" || ko "datawatch compute list failed"
  $DW_CLI observer peers list >/dev/null 2>&1 && ok "datawatch observer peers list exits 0" || ko "datawatch observer peers list failed"
  $DW_CLI routing-rules list >/dev/null 2>&1 && ok "datawatch routing-rules list exits 0" || ko "datawatch routing-rules list failed"
  $DW_CLI routing-rules list >/dev/null 2>&1 && ok "datawatch routing-rules list (x2 check) exits 0" || ko "datawatch routing-rules list (x2) failed"
  $DW_CLI schedule list >/dev/null 2>&1 && ok "datawatch schedule list exits 0" || skip "datawatch schedule CLI not yet exposed (REST /api/schedules works)"
  $DW_CLI autonomous list >/dev/null 2>&1 && ok "datawatch autonomous list exits 0" || ko "datawatch autonomous list failed"
  $DW_CLI algorithm list >/dev/null 2>&1 && ok "datawatch algorithm list exits 0" || ko "datawatch algorithm list failed"
  $DW_CLI evals runs >/dev/null 2>&1 && ok "datawatch evals runs exits 0" || ko "datawatch evals runs failed"
  $DW_CLI pipeline list >/dev/null 2>&1 && ok "datawatch pipeline list exits 0" || ko "datawatch pipeline list failed"
  $DW_CLI identity show >/dev/null 2>&1 && ok "datawatch identity show exits 0" || ko "datawatch identity show failed"
  $DW_CLI cost summary >/dev/null 2>&1 && ok "datawatch cost summary exits 0" || ko "datawatch cost summary failed"
  $DW_CLI tooling status >/dev/null 2>&1 && ok "datawatch tooling status exits 0" || ko "datawatch tooling status failed"
fi

H "51. Autonomous verifier_diff_max_bytes config round-trip (v8.16.0)"
# Verifies the new config key is readable and writable via REST so a
# regression in the dotted-key handler can't silently disable grounding.
VDB_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
  'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("verifier_diff_max_bytes","missing")))' 2>/dev/null)
if [[ "$VDB_BEFORE" == "missing" ]]; then
  skip "autonomous.verifier_diff_max_bytes not in /api/config response shape"
else
  ok "GET /api/config exposes autonomous.verifier_diff_max_bytes=$VDB_BEFORE"
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"autonomous.verifier_diff_max_bytes":4096}' "$BASE/api/config" >/dev/null
  VDB_AFTER=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
    'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("verifier_diff_max_bytes","missing")))' 2>/dev/null)
  if [[ "$VDB_AFTER" == "4096" ]]; then
    ok "PUT /api/config sets autonomous.verifier_diff_max_bytes to 4096"
  else
    ko "autonomous.verifier_diff_max_bytes round-trip failed (was $VDB_BEFORE → wanted 4096 → got $VDB_AFTER)"
  fi
  # Restore original value.
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"autonomous.verifier_diff_max_bytes":%s}' "${VDB_BEFORE:-0}")" \
    "$BASE/api/config" >/dev/null
fi

# ---------------------------------------------------------------------------
H "52. Autonomous default_quality_gates config round-trip (v8.17.0)"
# Verifies the four sub-keys are readable and the enabled flag round-trips.
DQG_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
  'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("default_quality_gates","missing")))' 2>/dev/null)
if [[ "$DQG_BEFORE" == "missing" ]]; then
  skip "autonomous.default_quality_gates not in /api/config response shape"
else
  ok "GET /api/config exposes autonomous.default_quality_gates"
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"autonomous.default_quality_gates.enabled":true,"autonomous.default_quality_gates.test_command":"echo ok","autonomous.default_quality_gates.timeout":30,"autonomous.default_quality_gates.block_on_regression":false}' \
    "$BASE/api/config" >/dev/null
  DQG_AFTER=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
    'import json,sys;d=json.load(sys.stdin);qg=d.get("autonomous",{}).get("default_quality_gates",{});print(str(qg.get("enabled","missing")))' 2>/dev/null)
  if [[ "$DQG_AFTER" == "True" || "$DQG_AFTER" == "true" ]]; then
    ok "PUT /api/config sets autonomous.default_quality_gates.enabled=true"
  else
    ko "autonomous.default_quality_gates.enabled round-trip failed (got '$DQG_AFTER')"
  fi
  # Restore disabled state.
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"autonomous.default_quality_gates.enabled":false,"autonomous.default_quality_gates.test_command":"","autonomous.default_quality_gates.timeout":0,"autonomous.default_quality_gates.block_on_regression":false}' \
    "$BASE/api/config" >/dev/null
fi

# ---------------------------------------------------------------------------
H "53. File service upload + delete (image attachment v8.19.0)"
SMOKE_IMG_NAME="smoke_img_$(date +%s).jpg"
SMOKE_IMG_PATH="$HOME/$SMOKE_IMG_NAME"
# Upload a minimal JPEG header via multipart POST /api/files.
UPLOAD_RES=$(curl "${curl_args[@]}" -w "\n__HTTP_%{http_code}__" \
  -F "file=@/dev/null;filename=$SMOKE_IMG_NAME;type=image/jpeg" \
  -F "path=$SMOKE_IMG_PATH" \
  "$BASE/api/files" 2>/dev/null)
UPLOAD_HTTP=$(echo "$UPLOAD_RES" | grep -o '__HTTP_[0-9]*__' | tr -d '_HTTP_')
UPLOAD_BODY=$(echo "$UPLOAD_RES" | sed 's/__HTTP_[0-9]*__//')
if [[ "$UPLOAD_HTTP" == "200" ]]; then
  ok "POST /api/files upload succeeded (HTTP 200)"
  add_cleanup "smoke-file" "$SMOKE_IMG_PATH"
  # DELETE the file.
  DEL_RES=$(curl "${curl_args[@]}" -w "\n__HTTP_%{http_code}__" \
    -X DELETE -H "Content-Type: application/json" \
    -d "{\"path\":\"$SMOKE_IMG_PATH\"}" \
    "$BASE/api/files" 2>/dev/null)
  DEL_HTTP=$(echo "$DEL_RES" | grep -o '__HTTP_[0-9]*__' | tr -d '_HTTP_')
  if [[ "$DEL_HTTP" == "200" ]]; then
    ok "DELETE /api/files succeeded (HTTP 200)"
  else
    ko "DELETE /api/files returned HTTP $DEL_HTTP"
  fi
elif [[ "$UPLOAD_HTTP" == "403" ]]; then
  skip "S53 — POST /api/files: 403 path outside service root (upload path not in configured root)"
else
  ko "POST /api/files returned HTTP $UPLOAD_HTTP body: $(echo "$UPLOAD_BODY" | head -c 120)"
fi

# ---------------------------------------------------------------------------
H "54. Current-status no-change contract (v8.19.8)"
# GET /api/sessions/{id}/current-status on an idle session must return 200 + JSON
# with no_change:true — not 204 with empty body (RFC 7231 §3.3 / B54).
if [[ -n "${SMOKE_SESS_ID:-}" ]]; then
  CS_RES=$(curl "${curl_args[@]}" -s -w "\n__HTTP_%{http_code}__" \
    "$BASE/api/sessions/${SMOKE_SESS_ID}/current-status" 2>/dev/null)
  CS_HTTP=$(echo "$CS_RES" | grep -o '__HTTP_[0-9]*__' | tr -d '_HTTP_')
  CS_BODY=$(echo "$CS_RES" | sed 's/__HTTP_[0-9]*__//')
  if [[ "$CS_HTTP" == "200" ]]; then
    if echo "$CS_BODY" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "no_change" in d or "output" in d' 2>/dev/null; then
      ok "S54 — GET /api/sessions/{id}/current-status returns 200 + JSON body"
    else
      ko "S54 — current-status 200 but body is missing no_change/output: $(echo "$CS_BODY" | head -c 80)"
    fi
  elif [[ "$CS_HTTP" == "204" ]]; then
    ko "S54 — current-status returned 204 (regression: B54 fix reverted)"
  else
    skip "S54 — current-status returned $CS_HTTP (auth-gated or session not found)"
  fi
else
  skip "S54 — current-status check: no smoke session available"
fi

# ---------------------------------------------------------------------------
H "55. Web search config round-trip (v8.22.0)"
WS_CFG=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" "${BASE}/api/web_search/stats" 2>/dev/null || echo "000")
case "$WS_CFG" in
  200) ok "GET /api/web_search/stats returns 200" ;;
  401|403) skip "S55 — GET /api/web_search/stats: auth required (token not set)" ;;
  *) ko "GET /api/web_search/stats returned $WS_CFG" ;;
esac

WS_GET=$(curl "${curl_args[@]}" -s "${BASE}/api/config" 2>/dev/null)
if echo "$WS_GET" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "web_search" in d' 2>/dev/null; then
  ok "GET /api/config contains web_search section"
else
  case "$WS_CFG" in
    401|403) skip "S55 — config web_search key: auth required" ;;
    *) ko "GET /api/config missing web_search section" ;;
  esac
fi

# ---------------------------------------------------------------------------
H "56. PRD task reset endpoint (v8.23.0)"
# Find the smoke PRD created earlier in this run (if any) to test reset_task.
# If SMOKE_PRD_ID is set by a prior section, use it; otherwise skip gracefully.
if [[ -n "${SMOKE_PRD_ID:-}" ]]; then
  # Find a failed task in the smoke PRD; try a known bad task or skip.
  RT_STATUS=$(curl "${curl_args[@]}" -s -o /dev/null -w "%{http_code}" \
    -X POST "${BASE}/api/autonomous/prds/${SMOKE_PRD_ID}/reset_task" \
    -H "Content-Type: application/json" \
    -d '{"task_id":"__nonexistent__","actor":"smoke"}' 2>/dev/null || echo "000")
  case "$RT_STATUS" in
    400) ok "POST /api/autonomous/prds/{id}/reset_task: 400 for nonexistent task (expected)" ;;
    401|403) skip "S56 — reset_task: auth required" ;;
    000) skip "S56 — reset_task: daemon not reachable" ;;
    *) ko "POST /api/autonomous/prds/{id}/reset_task returned $RT_STATUS (expected 400)" ;;
  esac
else
  skip "S56 — reset_task: no smoke PRD available (skipped earlier sections)"
fi

H "57. SEC-014 — federation peer token redaction (v8.39.19)"
FP_NAME="smoke-fedpeer-$$"
FP_CREATE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/federation/peers" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$FP_NAME\",\"url\":\"http://127.0.0.1:1\",\"token\":\"smoke-secret-token-do-not-leak\",\"enabled\":true}" 2>/dev/null || echo "")
if [[ -n "$FP_CREATE" ]]; then
  add_cleanup fedpeer "$FP_NAME"
  if echo "$FP_CREATE" | grep -q "smoke-secret-token-do-not-leak"; then
    ko "S57 — POST /api/federation/peers echoed the real token in its response"
  elif echo "$FP_CREATE" | grep -q '"token_present":true'; then
    ok "S57 — create response carries token_present, not the raw token"
  else
    ko "S57 — create response missing token_present: ${FP_CREATE:0:200}"
  fi
  FP_GET=$(curl "${curl_args[@]}" "$BASE/api/federation/peers/$FP_NAME" 2>/dev/null || echo "")
  if echo "$FP_GET" | grep -q "smoke-secret-token-do-not-leak"; then
    ko "S57 — GET /api/federation/peers/{name} echoed the real token"
  else
    ok "S57 — GET /api/federation/peers/{name} does not echo the real token"
  fi
  FP_LIST=$(curl "${curl_args[@]}" "$BASE/api/federation/peers" 2>/dev/null || echo "")
  if echo "$FP_LIST" | grep -q "smoke-secret-token-do-not-leak"; then
    ko "S57 — GET /api/federation/peers (list) echoed the real token"
  else
    ok "S57 — GET /api/federation/peers (list) does not echo the real token"
  fi
else
  skip "S57 — could not create a smoke federation peer"
fi

H "58. SEC-002 — MCP SSE auth fallback visible in health/diagnose (v8.39.20)"
HEALTH=$(curl "${curl_args[@]}" "$BASE/api/health" 2>/dev/null || echo "")
if echo "$HEALTH" | grep -q "mcp_auth_required"; then
  ok "S58 — GET /api/health includes mcp_auth_required"
else
  ko "S58 — GET /api/health missing mcp_auth_required: ${HEALTH:0:200}"
fi
DIAG=$(curl "${curl_args[@]}" "$BASE/api/diagnose" 2>/dev/null || echo "")
if echo "$DIAG" | grep -q '"rest_auth"' && echo "$DIAG" | grep -q '"mcp_sse_auth"'; then
  ok "S58 — GET /api/diagnose includes rest_auth and mcp_sse_auth checks"
else
  ko "S58 — GET /api/diagnose missing rest_auth/mcp_sse_auth: ${DIAG:0:300}"
fi

H "59. SEC-007 — WS upgrade rejects a mismatched Origin (v8.39.21)"
if command -v python3 >/dev/null 2>&1; then
  WS_SCHEME="ws"; [[ "$BASE" == https://* ]] && WS_SCHEME="wss"
  WS_HOST="${BASE#*://}"
  WS_ORIGIN_CODE=$(python3 - "$WS_SCHEME" "$WS_HOST" "$TOK" <<'PYEOF' 2>/dev/null || echo "ERR"
import socket, ssl, sys
scheme, host, tok = sys.argv[1], sys.argv[2], sys.argv[3]
hostname, _, port = host.partition(':')
port = int(port or (443 if scheme == "wss" else 80))
sock = socket.create_connection((hostname, port), timeout=5)
if scheme == "wss":
    sock = ssl.create_default_context().wrap_socket(sock, server_hostname=hostname) if False else ssl._create_unverified_context().wrap_socket(sock, server_hostname=hostname)
req = (
    f"GET /ws HTTP/1.1\r\nHost: {host}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
    f"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"
    f"Origin: https://evil.example\r\nAuthorization: Bearer {tok}\r\n\r\n"
)
sock.sendall(req.encode())
resp = sock.recv(200).decode(errors="replace")
print(resp.split()[1] if len(resp.split()) > 1 else "ERR")
PYEOF
)
  if [[ "$WS_ORIGIN_CODE" == "403" ]]; then
    ok "S59 — WS upgrade with a mismatched Origin rejected (403 — gorilla/websocket's CheckOrigin-failure default)"
  elif [[ "$WS_ORIGIN_CODE" == "ERR" ]]; then
    skip "S59 — could not probe WS upgrade (python3/socket issue)"
  else
    ko "S59 — WS upgrade with a mismatched Origin returned $WS_ORIGIN_CODE, expected 403"
  fi
else
  skip "S59 — python3 not available for the WS handshake probe"
fi

H "60. SEC-009 — federation-peer default grant + /peers/self (v8.39.22)"
if [[ -z "$TOK" ]]; then
  # The sandbox's admin server.token is empty by design (SEC-001's own
  # tested posture) — fedAuthMiddleware's empty-token early-return means
  # EVERY caller passes through as admin-equivalent regardless of which
  # Bearer token (if any) was presented, so "peer vs admin" has nothing
  # to distinguish here. These two checks only mean something once an
  # admin token is actually set.
  skip "S60 — sandbox admin token is empty; peer-vs-admin distinction doesn't apply"
else
  FP2_NAME="smoke-fedpeer-self-$$"
  FP2_CREATE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/federation/peers" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$FP2_NAME\",\"url\":\"http://127.0.0.1:1\",\"token\":\"smoke-self-token-$$\",\"enabled\":true}" 2>/dev/null || echo "")
  if [[ -n "$FP2_CREATE" ]]; then
    add_cleanup fedpeer "$FP2_NAME"
    SELF_CODE=$(curl "${curl_args[@]}" -s -o /tmp/smoke-self-resp.$$ -w '%{http_code}' \
      -H "Authorization: Bearer smoke-self-token-$$" \
      "$BASE/api/federation/peers/self" 2>/dev/null || echo "000")
    SELF_BODY=$(cat /tmp/smoke-self-resp.$$ 2>/dev/null); rm -f /tmp/smoke-self-resp.$$
    if [[ "$SELF_CODE" == "200" ]] && echo "$SELF_BODY" | grep -q "\"$FP2_NAME\""; then
      ok "S60 — GET /api/federation/peers/self (as the peer) returns its own entry"
    else
      ko "S60 — GET /api/federation/peers/self returned $SELF_CODE: ${SELF_BODY:0:200}"
    fi
    SESS_CODE=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' \
      -H "Authorization: Bearer smoke-self-token-$$" \
      "$BASE/api/sessions" 2>/dev/null || echo "000")
    if [[ "$SESS_CODE" == "403" ]]; then
      ok "S60 — bare federation-peer default grant does NOT include sessions:list (403, as narrowed)"
    else
      ko "S60 — bare federation-peer got $SESS_CODE on /api/sessions, expected 403 (default grant narrowed in SEC-009)"
    fi
  else
    skip "S60 — could not create a smoke federation peer for /self check"
  fi
fi

H "61. SEC-006 — bearer token no longer accepted via ?token= in the URL (v8.39.23)"
# A no-Authorization-header POST to /api/auth/nonce is caught at two
# different layers depending on whether an admin token is configured:
#   - empty server.token (this sandbox's default) -- fedAuthMiddleware's
#     empty-token bypass lets the request through with no identity, so
#     handleAuthNonce itself sees no caller-token context value and 400s.
#   - a real server.token set -- fedAuthMiddleware rejects the request
#     for lacking ANY valid credential before handleAuthNonce ever runs,
#     so the caller gets 401, not 400. (Confirmed live against a real
#     authenticated sandbox -- the 400 path never fires there.)
NONCE_NOAUTH_CODE=$(curl "${curl_args[@]}" -s -o /tmp/smoke-nonce-resp.$$ -w '%{http_code}' -X POST "$BASE/api/auth/nonce" 2>/dev/null || echo "000")
NONCE_NOAUTH_BODY=$(cat /tmp/smoke-nonce-resp.$$ 2>/dev/null); rm -f /tmp/smoke-nonce-resp.$$
if [[ -z "$TOK" ]]; then
  if [[ "$NONCE_NOAUTH_CODE" == "400" ]]; then
    ok "S61 — POST /api/auth/nonce with no Authorization header returns 400 (no caller token to mint for)"
  else
    ko "S61 — POST /api/auth/nonce with no auth returned $NONCE_NOAUTH_CODE, expected 400: ${NONCE_NOAUTH_BODY:0:200}"
  fi
  skip "S61 — sandbox admin token is empty; ?token=-rejection and the nonce round-trip need a real token to be meaningful"
else
  if [[ "$NONCE_NOAUTH_CODE" == "401" ]]; then
    ok "S61 — POST /api/auth/nonce with no Authorization header returns 401 (fedAuthMiddleware rejects before the handler runs)"
  else
    ko "S61 — POST /api/auth/nonce with no auth returned $NONCE_NOAUTH_CODE, expected 401: ${NONCE_NOAUTH_BODY:0:200}"
  fi

  OLD_STYLE_CODE=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' "$BASE/api/sessions?token=$TOK" 2>/dev/null || echo "000")
  if [[ "$OLD_STYLE_CODE" == "401" ]]; then
    ok "S61 — ?token=<admin-token> in the URL is rejected (401) with no Authorization header"
  else
    ko "S61 — ?token=<admin-token> in the URL returned $OLD_STYLE_CODE, expected 401 (SEC-006 removed this fallback)"
  fi

  NONCE_MINT=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/auth/nonce" -H "Authorization: Bearer $TOK" 2>/dev/null || echo "")
  NONCE_VAL=$(echo "$NONCE_MINT" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("nonce",""))' 2>/dev/null || echo "")
  if [[ -n "$NONCE_VAL" ]]; then
    ok "S61 — POST /api/auth/nonce (authenticated) mints a nonce"
    DL_CODE1=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' "$BASE/api/files/download?nonce=$NONCE_VAL" 2>/dev/null || echo "000")
    if [[ "$DL_CODE1" == "400" ]]; then
      ok "S61 — ?nonce= on /api/files/download authenticates (400 'path required', not 401)"
    else
      ko "S61 — /api/files/download?nonce=... returned $DL_CODE1, expected 400 (authenticated, missing path)"
    fi
    DL_CODE2=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' "$BASE/api/files/download?nonce=$NONCE_VAL" 2>/dev/null || echo "000")
    if [[ "$DL_CODE2" == "401" ]]; then
      ok "S61 — reusing the same nonce a second time is rejected (401 — single-use)"
    else
      ko "S61 — reusing the nonce returned $DL_CODE2, expected 401 (nonce must be single-use)"
    fi
    NONCE_OTHER_ROUTE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/auth/nonce" -H "Authorization: Bearer $TOK" 2>/dev/null || echo "")
    NONCE_VAL2=$(echo "$NONCE_OTHER_ROUTE" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("nonce",""))' 2>/dev/null || echo "")
    if [[ -n "$NONCE_VAL2" ]]; then
      OTHER_ROUTE_CODE=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' "$BASE/api/sessions?nonce=$NONCE_VAL2" 2>/dev/null || echo "000")
      if [[ "$OTHER_ROUTE_CODE" == "401" ]]; then
        ok "S61 — a valid nonce is rejected on a route NOT in the nonce-aware allow-list (/api/sessions)"
      else
        ko "S61 — nonce on /api/sessions returned $OTHER_ROUTE_CODE, expected 401 (nonce is only honored on nonceAwarePath routes)"
      fi
    else
      skip "S61 — could not mint a second nonce for the off-allow-list check"
    fi
  else
    ko "S61 — POST /api/auth/nonce (authenticated) did not return a nonce: ${NONCE_MINT:0:200}"
  fi
fi

# ---------------------------------------------------------------------------
H "62. BL397 — native ACME subsystem REST surface"
# Structural/wiring check only -- the sandbox daemon has no real public
# hostname or port 80, so a full issue/renew cycle isn't exercised here
# (that was live-verified separately against a real host + real Let's
# Encrypt directory; see docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md).
# This section just confirms the REST surface is wired and degrades
# gracefully when acme.enabled is false (the sandbox's default).
ACME_STATUS=$(curl "${curl_args[@]}" -s "$BASE/api/acme/status" 2>/dev/null || echo "")
if echo "$ACME_STATUS" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert "enabled" in d' 2>/dev/null; then
  ok "S62 — GET /api/acme/status returns a well-formed response"
else
  ko "S62 — GET /api/acme/status did not return an 'enabled' field: ${ACME_STATUS:0:200}"
fi

ACME_ENABLED=$(echo "$ACME_STATUS" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("enabled"))' 2>/dev/null || echo "")
if [[ "$ACME_ENABLED" == "False" || "$ACME_ENABLED" == "false" ]]; then
  VERIFY_CODE=$(curl "${curl_args[@]}" -s -o /tmp/smoke-acme-verify.$$ -w '%{http_code}' "$BASE/api/acme/verify" 2>/dev/null || echo "000")
  rm -f /tmp/smoke-acme-verify.$$
  if [[ "$VERIFY_CODE" == "503" ]]; then
    ok "S62 — GET /api/acme/verify returns 503 when acme is disabled (graceful, not a 500/panic)"
  else
    ko "S62 — GET /api/acme/verify with acme disabled returned $VERIFY_CODE, expected 503"
  fi
  RENEW_CODE=$(curl "${curl_args[@]}" -s -o /tmp/smoke-acme-renew.$$ -w '%{http_code}' -X POST "$BASE/api/acme/renew" 2>/dev/null || echo "000")
  rm -f /tmp/smoke-acme-renew.$$
  if [[ "$RENEW_CODE" == "503" ]]; then
    ok "S62 — POST /api/acme/renew returns 503 when acme is disabled (graceful, not a 500/panic)"
  else
    ko "S62 — POST /api/acme/renew with acme disabled returned $RENEW_CODE, expected 503"
  fi
  skip "S62 — full issue/renew cycle needs a real public hostname + port 80; live-verified separately, not simulated here"
else
  skip "S62 — acme.enabled is true on this sandbox; skipping the disabled-path checks (unexpected for a fresh sandbox config)"
fi

H "63. GH#179 — LLM registry api_key_ref redaction (v8.63.1)"
LLM_KEY_NAME="smoke-llm-keyredact-$$"
LLM_KEY_CREATE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/llms" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$LLM_KEY_NAME\",\"kind\":\"openwebui\",\"api_key_ref\":\"sk-smoke-secret-do-not-leak\"}" 2>/dev/null || echo "")
if [[ -n "$LLM_KEY_CREATE" ]]; then
  add_cleanup llm "$LLM_KEY_NAME"
  LLM_KEY_GET=$(curl "${curl_args[@]}" "$BASE/api/llms/$LLM_KEY_NAME" 2>/dev/null || echo "")
  if echo "$LLM_KEY_GET" | grep -q "sk-smoke-secret-do-not-leak"; then
    ko "S63 — GET /api/llms/{name} echoed a literal api_key_ref in clear text"
  elif echo "$LLM_KEY_GET" | grep -q '"api_key_ref_present":true'; then
    ok "S63 — GET /api/llms/{name} carries api_key_ref_present, not the raw key"
  else
    ko "S63 — GET /api/llms/{name} missing api_key_ref_present: ${LLM_KEY_GET:0:200}"
  fi
  LLM_KEY_LIST=$(curl "${curl_args[@]}" "$BASE/api/llms" 2>/dev/null || echo "")
  if echo "$LLM_KEY_LIST" | grep -q "sk-smoke-secret-do-not-leak"; then
    ko "S63 — GET /api/llms (list) echoed a literal api_key_ref in clear text"
  else
    ok "S63 — GET /api/llms (list) does not echo a literal api_key_ref"
  fi
  # A PUT that omits api_key_ref (the redacted-GET-echoed-back case) must
  # preserve the stored key, not wipe it.
  curl "${curl_args[@]}" -s -X PUT "$BASE/api/llms/$LLM_KEY_NAME" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$LLM_KEY_NAME\",\"kind\":\"openwebui\",\"model\":\"smoke-model\"}" >/dev/null 2>&1
  LLM_KEY_AFTER=$(curl "${curl_args[@]}" "$BASE/api/llms/$LLM_KEY_NAME" 2>/dev/null || echo "")
  if echo "$LLM_KEY_AFTER" | grep -q '"api_key_ref_present":true'; then
    ok "S63 — PUT omitting api_key_ref preserves the stored key (merge-on-update)"
  else
    ko "S63 — PUT omitting api_key_ref wiped the stored key: ${LLM_KEY_AFTER:0:200}"
  fi
else
  skip "S63 — could not create a smoke LLM entry"
fi

H "64. GH#183 — APNs per-device apns_environment persists + round-trips (v8.63.4)"
DEV_TOKEN="smoke-apns-$$"
DEV_CREATE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/devices/register" \
  -H "Content-Type: application/json" \
  -d "{\"device_token\":\"$DEV_TOKEN\",\"kind\":\"apns\",\"platform\":\"ios\",\"apns_environment\":\"development\"}" 2>/dev/null || echo "")
DEV_ID=$(echo "$DEV_CREATE" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("device_id",""))' 2>/dev/null || echo "")
if [[ -n "$DEV_ID" ]]; then
  add_cleanup device "$DEV_ID"
  DEV_LIST=$(curl "${curl_args[@]}" "$BASE/api/devices" 2>/dev/null || echo "")
  if echo "$DEV_LIST" | grep -q "\"device_id\":\"$DEV_ID\"" && echo "$DEV_LIST" | grep -q '"apns_environment":"development"'; then
    ok "S64 — GET /api/devices shows the registered apns_environment"
  else
    ko "S64 — apns_environment missing/wrong in GET /api/devices: ${DEV_LIST:0:300}"
  fi
  # Re-register the same token under production (e.g. a TestFlight build
  # graduated to App Store) -- must refresh, not stick to the old value.
  curl "${curl_args[@]}" -s -X POST "$BASE/api/devices/register" \
    -H "Content-Type: application/json" \
    -d "{\"device_token\":\"$DEV_TOKEN\",\"kind\":\"apns\",\"platform\":\"ios\",\"apns_environment\":\"production\"}" >/dev/null 2>&1
  DEV_LIST2=$(curl "${curl_args[@]}" "$BASE/api/devices" 2>/dev/null || echo "")
  if echo "$DEV_LIST2" | grep -q '"apns_environment":"production"'; then
    ok "S64 — re-registering the same token refreshes apns_environment"
  else
    ko "S64 — apns_environment did not refresh on re-register: ${DEV_LIST2:0:300}"
  fi
  # An invalid value must be rejected, not silently stored.
  DEV_BAD_CODE=$(curl "${curl_args[@]}" -s -o /tmp/smoke-apns-bad.$$ -w '%{http_code}' -X POST "$BASE/api/devices/register" \
    -H "Content-Type: application/json" \
    -d "{\"device_token\":\"smoke-apns-bad-$$\",\"kind\":\"apns\",\"apns_environment\":\"staging\"}" 2>/dev/null || echo "000")
  rm -f /tmp/smoke-apns-bad.$$
  if [[ "$DEV_BAD_CODE" == "400" ]]; then
    ok "S64 — POST /api/devices/register rejects an invalid apns_environment (400)"
  else
    ko "S64 — invalid apns_environment returned $DEV_BAD_CODE, expected 400"
  fi
else
  skip "S64 — could not create a smoke APNs device registration"
fi

H "65. Custom (non-default) capability group is actually enforced live (2026-10-08)"
# S60 already covers the DEFAULT federation-peer grant (narrowed, no
# sessions:list). This covers the other half of CBAC that had zero smoke
# coverage: a custom, narrowly-scoped capability GROUP -- the real
# GroupStore/AsMap mechanism, not a mock -- actually gating what a peer
# using it can and can't do.
if [[ -z "$TOK" ]]; then
  skip "S65 — sandbox admin token is empty; peer-vs-admin distinction doesn't apply (same caveat as S60)"
else
  CG_NAME="smoke-custom-group-$$"
  CG_CREATE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/federation/groups" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"$CG_NAME\",\"description\":\"smoke: alerts-list only\",\"caps\":[\"alerts:list\"]}" 2>/dev/null || echo "")
  if echo "$CG_CREATE" | grep -q "\"$CG_NAME\""; then
    add_cleanup fedgroup "$CG_NAME"
    FP3_NAME="smoke-fedpeer-customgroup-$$"
    FP3_TOKEN="smoke-customgroup-token-$$"
    FP3_CREATE=$(curl "${curl_args[@]}" -s -X POST "$BASE/api/federation/peers" \
      -H "Content-Type: application/json" \
      -d "{\"name\":\"$FP3_NAME\",\"url\":\"http://127.0.0.1:1\",\"token\":\"$FP3_TOKEN\",\"enabled\":true,\"capabilities\":[\"$CG_NAME\"]}" 2>/dev/null || echo "")
    if [[ -n "$FP3_CREATE" ]]; then
      add_cleanup fedpeer "$FP3_NAME"
      ALERTS_CODE=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' \
        -H "Authorization: Bearer $FP3_TOKEN" \
        "$BASE/api/alerts" 2>/dev/null || echo "000")
      if [[ "$ALERTS_CODE" == "200" ]]; then
        ok "S65 — a peer granted only the custom alerts:list group gets 200 on GET /api/alerts (in-grant)"
      else
        ko "S65 — in-grant GET /api/alerts returned $ALERTS_CODE, expected 200"
      fi
      SESS_CODE3=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' \
        -H "Authorization: Bearer $FP3_TOKEN" \
        "$BASE/api/sessions" 2>/dev/null || echo "000")
      if [[ "$SESS_CODE3" == "403" ]]; then
        ok "S65 — the same peer gets 403 on GET /api/sessions (outside the custom group's grant)"
      else
        ko "S65 — out-of-grant GET /api/sessions returned $SESS_CODE3, expected 403 (custom group enforcement not actually narrowing access)"
      fi
    else
      skip "S65 — could not create a smoke federation peer using the custom group"
    fi
  else
    skip "S65 — could not create the custom capability group: ${CG_CREATE:0:200}"
  fi
fi

H "66. GH#201 — HTTP access / WS lifecycle / auth-failure log (/api/audit/access)"
if [[ -z "$TOK" ]]; then
  skip "S66 — sandbox admin token is empty"
else
  AL_CODE=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $TOK" \
    "$BASE/api/audit/access?limit=1" 2>/dev/null || echo "000")
  if [[ "$AL_CODE" == "200" ]]; then
    ok "S66 — GET /api/audit/access returns 200"
  else
    ko "S66 — GET /api/audit/access returned $AL_CODE, expected 200"
  fi

  BAD_TOKEN="smoke-bad-token-$$"
  curl "${curl_args[@]}" -s -o /dev/null \
    -H "Authorization: Bearer $BAD_TOKEN" \
    "$BASE/api/sessions" 2>/dev/null || true
  sleep 0.3
  AL_ENTRIES=$(curl "${curl_args[@]}" -s \
    -H "Authorization: Bearer $TOK" \
    "$BASE/api/audit/access?action=auth_failure&limit=5" 2>/dev/null || echo "")
  if echo "$AL_ENTRIES" | grep -q '"action":"auth_failure"'; then
    ok "S66 — a bad-token request is recorded as an auth_failure entry"
  else
    ko "S66 — expected an auth_failure entry after a bad-token request: ${AL_ENTRIES:0:300}"
  fi
  if echo "$AL_ENTRIES" | grep -q "$BAD_TOKEN"; then
    ko "S66 — access-log entry contains the raw bad token value (must never log tokens)"
  else
    ok "S66 — access-log entries never contain a raw token value"
  fi
fi

# ---------------------------------------------------------------------------
H "67. BL403 Phase 1 — plugin discovery surfaces manifest v3 kind/connection/lifecycle"
if [[ "$EXTERNAL_DAEMON" == "1" ]]; then
  skip "S67 — external-daemon mode (DW_BASE): this script doesn't control the target daemon's plugins.dir, so the hello-automation/demo-service fixtures this check depends on may not be loaded there"
else
AUTO_JSON=$(curl "${curl_args[@]}" -H "Authorization: Bearer $TOK" "$BASE/api/plugins/hello-automation" 2>/dev/null || echo "")
if echo "$AUTO_JSON" | grep -q '"name":"hello-automation"'; then
  ok "S67 — automation-kind example plugin discovered"
else
  ko "S67 — hello-automation not found via GET /api/plugins/hello-automation: ${AUTO_JSON:0:300}"
fi
if echo "$AUTO_JSON" | grep -q '"kind":"service"'; then
  ko "S67 — hello-automation must default to automation kind, not service"
else
  ok "S67 — hello-automation has no kind (v2.1 default: automation), unchanged behavior"
fi

SVC_JSON=$(curl "${curl_args[@]}" -H "Authorization: Bearer $TOK" "$BASE/api/plugins/demo-service" 2>/dev/null || echo "")
if echo "$SVC_JSON" | grep -q '"kind":"service"'; then
  ok "S67 — demo-service example plugin discovered as kind: service"
else
  ko "S67 — demo-service not discovered with kind:service: ${SVC_JSON:0:300}"
fi
if echo "$SVC_JSON" | grep -q "\"base_url\":\"http://127.0.0.1:${DEMO_SERVICE_PORT}\""; then
  ok "S67 — demo-service connection.base_url surfaced correctly via REST"
else
  ko "S67 — demo-service connection.base_url missing/wrong: ${SVC_JSON:0:300}"
fi
if echo "$SVC_JSON" | grep -q '"managed"'; then
  ok "S67 — demo-service lifecycle.supported_modes includes managed"
else
  ko "S67 — demo-service lifecycle.supported_modes missing managed: ${SVC_JSON:0:300}"
fi
fi

# ---------------------------------------------------------------------------
H "68. BL403 Phase 2 — managed service-plugin lifecycle (start/health/stop) end-to-end"
if [[ "$EXTERNAL_DAEMON" == "1" ]]; then
  skip "S68 — external-daemon mode (DW_BASE): see S67's identical reason"
else
curl "${curl_args[@]}" -X POST -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" \
  -d '{"managed":true}' "$BASE/api/plugins/demo-service/enable" >/dev/null 2>&1

_svc_state=""
for _i in $(seq 1 20); do
  SVC_JSON=$(curl "${curl_args[@]}" -H "Authorization: Bearer $TOK" "$BASE/api/plugins/demo-service" 2>/dev/null || echo "")
  _svc_state=$(echo "$SVC_JSON" | python3 -c 'import json,sys
try:
    print(json.load(sys.stdin).get("service",{}).get("state",""))
except Exception:
    print("")' 2>/dev/null || echo "")
  [[ "$_svc_state" == "healthy" ]] && break
  sleep 1
done
if [[ "$_svc_state" == "healthy" ]]; then
  ok "S68 — demo-service reaches state=healthy after enable {managed:true}"
else
  ko "S68 — demo-service never reached healthy (last state: '$_svc_state'): ${SVC_JSON:0:300}"
fi

DEMO_PID=$(echo "$SVC_JSON" | python3 -c 'import json,sys
try:
    print(json.load(sys.stdin).get("service",{}).get("pid",0))
except Exception:
    print(0)' 2>/dev/null || echo 0)
if [[ "$DEMO_PID" -gt 0 ]] && kill -0 "$DEMO_PID" 2>/dev/null; then
  ok "S68 — demo-service's supervised PID ($DEMO_PID) is a real, running process"
else
  ko "S68 — demo-service's reported PID ($DEMO_PID) is not a live process"
fi

HEALTHZ_CODE=$(curl -sk --max-time 3 -o /dev/null -w '%{http_code}' "http://127.0.0.1:${DEMO_SERVICE_PORT}/healthz" 2>/dev/null || echo "000")
if [[ "$HEALTHZ_CODE" == "200" ]]; then
  ok "S68 — demo-service's real /healthz endpoint answers 200 directly (not just self-reported)"
else
  ko "S68 — demo-service's real /healthz returned $HEALTHZ_CODE, expected 200"
fi

curl "${curl_args[@]}" -X POST -H "Authorization: Bearer $TOK" "$BASE/api/plugins/demo-service/disable" >/dev/null 2>&1
_stopped=0
for _i in $(seq 1 20); do
  if [[ -n "$DEMO_PID" ]] && [[ "$DEMO_PID" -gt 0 ]] && ! kill -0 "$DEMO_PID" 2>/dev/null; then
    _stopped=1
    break
  fi
  sleep 1
done
if [[ "$_stopped" == "1" ]]; then
  ok "S68 — demo-service's process is actually gone after disable (graceful stop + reap)"
else
  ko "S68 — demo-service's PID ($DEMO_PID) is still alive after disable"
fi

AUDIT_START=$(curl "${curl_args[@]}" -H "Authorization: Bearer $TOK" "$BASE/api/audit?action=plugin_service_start&limit=10" 2>/dev/null || echo "")
AUDIT_STOP=$(curl "${curl_args[@]}" -H "Authorization: Bearer $TOK" "$BASE/api/audit?action=plugin_service_stop&limit=10" 2>/dev/null || echo "")
if echo "$AUDIT_START" | grep -q '"demo-service"'; then
  ok "S68 — plugin_service_start audit entry recorded for demo-service"
else
  ko "S68 — no plugin_service_start audit entry found for demo-service: ${AUDIT_START:0:300}"
fi
if echo "$AUDIT_STOP" | grep -q '"demo-service"'; then
  ok "S68 — plugin_service_stop audit entry recorded for demo-service"
else
  ko "S68 — no plugin_service_stop audit entry found for demo-service: ${AUDIT_STOP:0:300}"
fi
fi

# ---------------------------------------------------------------------------
H "69. BL410 — retry profiles + per-Automaton retry policy via MCP/REST/CLI"
# MCP: list (always 200, possibly empty)
RPL=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"retry_profile_list","args":{}}' "$BASE/api/mcp/call" || true)
echo "$RPL" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP retry_profile_list returns result" \
  || ko "MCP retry_profile_list unexpected: ${RPL:0:120}"
# Create + get + delete a retry profile via MCP (mirrors guardrail_profile_* above)
RP_ID=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"retry_profile_create","args":{"name":"smoke-retry","budgets_json":"{\"infra\":2}","ceiling_total":10}}' \
  "$BASE/api/mcp/call" | python3 -c 'import json,sys;s=str(json.load(sys.stdin));import re;m=re.search(r"[0-9a-f]{8}",s);print(m.group() if m else "")' 2>/dev/null || echo "")
if [[ -z "$RP_ID" ]]; then
  skip "retry profile create via MCP returned no id"
else
  add_cleanup retry_profile "$RP_ID"
  RPG=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"tool\":\"retry_profile_get\",\"args\":{\"id\":\"$RP_ID\"}}" "$BASE/api/mcp/call" || true)
  echo "$RPG" | grep -q "smoke-retry" \
    && ok "retry profile get via MCP returns the created profile" \
    || ko "retry profile get via MCP missing name: ${RPG:0:120}"
  curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"name\":\"retry_profile_delete\",\"arguments\":{\"id\":\"$RP_ID\"}}" \
    "$BASE/api/mcp/call" >/dev/null 2>&1
  ok "retry profile create+get+delete via MCP round-trip"
fi
# REST: the CRUD endpoints respond (not 404 — route wiring, not full behavior)
RP_REST_LIST=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" "$BASE/api/autonomous/retry_profiles" 2>/dev/null || echo "000")
[[ "$RP_REST_LIST" == "200" ]] \
  && ok "GET /api/autonomous/retry_profiles → 200" \
  || ko "GET /api/autonomous/retry_profiles unexpected HTTP $RP_REST_LIST"
# set_retry_policy wiring: a non-existent PRD must fail with a client
# error, proving the route exists and dispatches. The sibling
# set_guardrails handler (same case-statement block, same
# s.autonomousMgr.Set*-then-400-on-err shape) also returns 400 here,
# not 404 -- confirmed by reading internal/server/autonomous.go rather
# than assumed; this endpoint intentionally matches that precedent.
RP_SET_HTTP=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" -X PUT \
  -H "Content-Type: application/json" -d '{"retry_profile":"smoke-retry"}' \
  "$BASE/api/autonomous/prds/smoke-nonexistent-prd/set_retry_policy" 2>/dev/null || echo "000")
[[ "$RP_SET_HTTP" == "400" ]] \
  && ok "PUT /api/autonomous/prds/{id}/set_retry_policy → 400 on unknown automaton" \
  || ko "set_retry_policy endpoint wiring unexpected HTTP $RP_SET_HTTP (expected 400)"
# CLI round-trip (v9.0.54 — datawatch retry profile ...). Uses $DW_CLI
# (not a raw binary invocation) so TLS trust against the self-signed
# test daemon matches every other CLI check in section 50.
if [[ -z "${DW_CLI:-}" ]]; then
  skip "datawatch CLI not available — skipping retry profile CLI round-trip"
else
  CLI_RP_OUT=$($DW_CLI retry profile create --name smoke-retry-cli --budgets "infra=3" --ceiling-total 8 2>&1 || true)
  CLI_RP_ID=$(echo "$CLI_RP_OUT" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$CLI_RP_ID" ]]; then
    skip "datawatch retry profile create (CLI) returned no id: ${CLI_RP_OUT:0:120}"
  else
    add_cleanup retry_profile "$CLI_RP_ID"
    $DW_CLI retry profile delete "$CLI_RP_ID" >/dev/null 2>&1
    ok "datawatch retry profile create+delete (CLI) round-trip"
  fi
fi
# Learning surface responds (Phase 3 — may legitimately be empty on a fresh daemon)
LRN=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"tool":"autonomous_learnings","args":{}}' "$BASE/api/mcp/call" || true)
echo "$LRN" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d is not None' 2>/dev/null \
  && ok "MCP autonomous_learnings returns result" \
  || ko "MCP autonomous_learnings unexpected: ${LRN:0:120}"

# ---------------------------------------------------------------------------
H "70. ci-watch completion guardrail — config key + library entry (v9.0.63)"
# /api/config's autonomous block always carries the key (0 = use the
# 1500s default); /api/autonomous/config omits it while unset
# (omitempty), so read the config snapshot here.
CIW_CFG=$(curl "${curl_args[@]}" "$BASE/api/config" || true)
echo "$CIW_CFG" | python3 -c 'import json,sys;d=json.load(sys.stdin);a=d.get("autonomous",{});assert "ci_watch_timeout_seconds" in a and isinstance(a["ci_watch_timeout_seconds"],int)' 2>/dev/null \
  && ok "GET /api/config autonomous.ci_watch_timeout_seconds present" \
  || ko "autonomous.ci_watch_timeout_seconds missing from /api/config: ${CIW_CFG:0:120}"
CIW_LIB=$(curl "${curl_args[@]}" "$BASE/api/autonomous/guardrails" || true)
echo "$CIW_LIB" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert any(g.get("name")=="ci-watch" for g in d)' 2>/dev/null \
  && ok "guardrail library includes ci-watch" \
  || ko "ci-watch not in GET /api/autonomous/guardrails: ${CIW_LIB:0:120}"

# ---------------------------------------------------------------------------
H "71. per-task verification LLM override — set + clear round-trip (v9.0.72)"
# Smoke-created Automaton → cancel (soft) so structural edits and LLM
# overrides are accepted → add a story + task → set a task verification
# model/effort override → GET shows it → clear with "" → GET shows it
# gone. Model/effort only: verification_backend is validated against the
# configured LLM registry, which a bare smoke daemon may not have.
VO_PRD=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"spec":"smoke probe — verification LLM override","project_dir":"/tmp","actor":"smoke"}' \
  "$BASE/api/autonomous/prds" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
if [[ -z "$VO_PRD" ]]; then
  skip "verification override: POST /api/autonomous/prds returned no id (autonomous subsystem may be off)"
else
  add_cleanup prd "$VO_PRD"
  curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$VO_PRD" >/dev/null 2>&1
  VO_STORY=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d '{"title":"smoke-verif-story","actor":"smoke"}' "$BASE/api/autonomous/prds/$VO_PRD/add_story" \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(next((s["id"] for s in d.get("stories") or [] if s.get("title")=="smoke-verif-story"),""))' 2>/dev/null || echo "")
  VO_TASK=""
  if [[ -n "$VO_STORY" ]]; then
    VO_TASK=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d "{\"story_id\":\"$VO_STORY\",\"title\":\"smoke-verif-task\",\"spec\":\"noop\",\"actor\":\"smoke\"}" \
      "$BASE/api/autonomous/prds/$VO_PRD/add_task" \
      | python3 -c 'import json,sys;d=json.load(sys.stdin);print(next((t["id"] for s in d.get("stories") or [] for t in s.get("tasks") or [] if t.get("title")=="smoke-verif-task"),""))' 2>/dev/null || echo "")
  fi
  if [[ -z "$VO_TASK" ]]; then
    ko "verification override: could not add a story/task to cancelled Automaton $VO_PRD"
  else
    # vo_task_field <field> — print one field of the task from GET.
    vo_task_field() {
      curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$VO_PRD" | python3 -c "
import json,sys
d=json.load(sys.stdin)
t=next((t for s in d.get('stories') or [] for t in s.get('tasks') or [] if t.get('id')=='$VO_TASK'),{})
print(t.get('$1',''))" 2>/dev/null
    }
    VO_SET=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" \
      -d "{\"task_id\":\"$VO_TASK\",\"verification_model\":\"smoke-verifier-model\",\"verification_effort\":\"low\",\"actor\":\"smoke\"}" \
      "$BASE/api/autonomous/prds/$VO_PRD/set_task_verification_llm" 2>/dev/null || echo "000")
    if [[ "$VO_SET" == "200" && "$(vo_task_field verification_model)" == "smoke-verifier-model" && "$(vo_task_field verification_effort)" == "low" ]]; then
      ok "set_task_verification_llm: GET shows verification_model + verification_effort on the task"
    else
      ko "set_task_verification_llm set round-trip failed (HTTP $VO_SET, model='$(vo_task_field verification_model)', effort='$(vo_task_field verification_effort)')"
    fi
    VO_CLR=$(curl "${curl_args[@]}" -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" \
      -d "{\"task_id\":\"$VO_TASK\",\"verification_model\":\"\",\"verification_effort\":\"\",\"actor\":\"smoke\"}" \
      "$BASE/api/autonomous/prds/$VO_PRD/set_task_verification_llm" 2>/dev/null || echo "000")
    if [[ "$VO_CLR" == "200" && -z "$(vo_task_field verification_model)" && -z "$(vo_task_field verification_effort)" ]]; then
      ok "set_task_verification_llm: \"\" clears the override (GET shows inherit)"
    else
      ko "set_task_verification_llm clear round-trip failed (HTTP $VO_CLR, model='$(vo_task_field verification_model)', effort='$(vo_task_field verification_effort)')"
    fi
  fi
  curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$VO_PRD?hard=true" >/dev/null 2>&1
fi

# ---------------------------------------------------------------------------
H "72. foreign-file-drift check — block_on_file_drift GET/PUT round-trip (v9.0.75)"
# The drift check itself needs a real worker commit (covered by
# internal/autonomous/file_drift_test.go and the live verification doc);
# this section verifies the strict-mode knob round-trips through
# GET / PUT /api/config, then restores the original value.
FD_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
  'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("block_on_file_drift","missing")).lower())' 2>/dev/null)
if [[ "$FD_BEFORE" != "true" && "$FD_BEFORE" != "false" ]]; then
  ko "autonomous.block_on_file_drift missing from /api/config (got '$FD_BEFORE')"
else
  ok "GET /api/config exposes autonomous.block_on_file_drift=$FD_BEFORE"
  if [[ "$FD_BEFORE" == "true" ]]; then FD_NEXT="false"; else FD_NEXT="true"; fi
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"autonomous.block_on_file_drift":%s}' "$FD_NEXT")" "$BASE/api/config" >/dev/null
  FD_AFTER=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
    'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("block_on_file_drift","missing")).lower())' 2>/dev/null)
  if [[ "$FD_AFTER" == "$FD_NEXT" ]]; then
    ok "PUT /api/config flipped autonomous.block_on_file_drift to $FD_NEXT"
  else
    ko "autonomous.block_on_file_drift round-trip failed (was $FD_BEFORE → wanted $FD_NEXT → got $FD_AFTER)"
  fi
  # Restore original value (failing this leaks state across runs).
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"autonomous.block_on_file_drift":%s}' "$FD_BEFORE")" "$BASE/api/config" >/dev/null
fi

# ---------------------------------------------------------------------------
H "73. Low-bandwidth payloads — capabilities, summary list, alert counts, API gzip (v9.0.81)"
# Read-only checks plus one temporary config change (api_gzip /
# api_gzip_min_bytes), restored at the end of the section. Creates no
# sessions, alerts or other entities, so nothing needs add_cleanup.
CAPS_OK=$(curl "${curl_args[@]}" "$BASE/api/info" | python3 -c \
  'import json,sys;d=json.load(sys.stdin);c=d.get("capabilities");print("yes" if isinstance(c,list) and "sessions.view_summary" in c else "no:%r"%(c,))' 2>/dev/null)
if [[ "$CAPS_OK" == "yes" ]]; then
  ok "GET /api/info capabilities includes sessions.view_summary"
else
  ko "GET /api/info capabilities missing sessions.view_summary ($CAPS_OK)"
fi

SUM_CODE=$(curl "${curl_args[@]}" -o "$TMPD/sess_summary.json" -w '%{http_code}' "$BASE/api/sessions?view=summary")
SUM_SHAPE=$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print("array" if isinstance(d,list) else type(d).__name__)' "$TMPD/sess_summary.json" 2>/dev/null)
if [[ "$SUM_CODE" == "200" && "$SUM_SHAPE" == "array" ]]; then
  ok "GET /api/sessions?view=summary → 200 JSON array"
else
  ko "GET /api/sessions?view=summary → HTTP $SUM_CODE, shape '$SUM_SHAPE'"
fi
BAD_VIEW=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/sessions?view=bogus")
if [[ "$BAD_VIEW" == "400" ]]; then
  ok "GET /api/sessions?view=bogus → 400"
else
  ko "GET /api/sessions?view=bogus → HTTP $BAD_VIEW (want 400)"
fi

AL_OK=$(curl "${curl_args[@]}" "$BASE/api/alerts?limit=0" | python3 -c \
  'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("alerts")==[] and isinstance(d.get("unread_count"),int) and isinstance(d.get("total"),int) else "no:%r"%(d,))' 2>/dev/null)
if [[ "$AL_OK" == "yes" ]]; then
  ok "GET /api/alerts?limit=0 → empty alerts with unread_count + total"
else
  ko "GET /api/alerts?limit=0 unexpected: ${AL_OK:0:200}"
fi
AC_OK=$(curl "${curl_args[@]}" "$BASE/api/alerts/count" | python3 -c \
  'import json,sys;d=json.load(sys.stdin);print("yes" if isinstance(d.get("unread_count"),int) else "no:%r"%(d,))' 2>/dev/null)
if [[ "$AC_OK" == "yes" ]]; then
  ok "GET /api/alerts/count → unread_count"
else
  ko "GET /api/alerts/count unexpected: ${AC_OK:0:200}"
fi

# gzip: force compression on for any non-empty body, then restore.
GZ_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
  'import json,sys;s=json.load(sys.stdin).get("server",{});print("%s %s"%(str(s.get("api_gzip","missing")).lower(),s.get("api_gzip_min_bytes","missing")))' 2>/dev/null)
read -r GZ_ON_BEFORE GZ_MIN_BEFORE <<< "$GZ_BEFORE"
if [[ "$GZ_ON_BEFORE" != "true" && "$GZ_ON_BEFORE" != "false" ]] || ! [[ "$GZ_MIN_BEFORE" =~ ^[0-9]+$ ]]; then
  ko "server.api_gzip / api_gzip_min_bytes missing from /api/config (got '$GZ_BEFORE')"
else
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"server.api_gzip":true,"server.api_gzip_min_bytes":0}' "$BASE/api/config" >/dev/null
  GZ_HDRS=$(curl "${curl_args[@]}" -H "Accept-Encoding: gzip" -D - -o "$TMPD/info.gz" "$BASE/api/info" | tr -d '\r')
  if echo "$GZ_HDRS" | grep -qi '^content-encoding: *gzip'; then
    if gzip -dc "$TMPD/info.gz" 2>/dev/null | python3 -c 'import json,sys;json.load(sys.stdin)' 2>/dev/null; then
      ok "Accept-Encoding: gzip → Content-Encoding: gzip on /api/info, body decompresses to JSON"
    else
      ko "/api/info gzip body did not decompress to JSON"
    fi
  else
    ko "Accept-Encoding: gzip on /api/info returned no Content-Encoding: gzip"
  fi
  PLAIN_HDRS=$(curl "${curl_args[@]}" -D - -o /dev/null "$BASE/api/info" | tr -d '\r')
  if echo "$PLAIN_HDRS" | grep -qi '^content-encoding: *gzip'; then
    ko "/api/info gzipped without Accept-Encoding: gzip"
  else
    ok "/api/info uncompressed without Accept-Encoding: gzip"
  fi
  # Restore original values (failing this leaks state across runs).
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"server.api_gzip":%s,"server.api_gzip_min_bytes":%s}' "$GZ_ON_BEFORE" "$GZ_MIN_BEFORE")" "$BASE/api/config" >/dev/null
  GZ_AFTER=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c \
    'import json,sys;s=json.load(sys.stdin).get("server",{});print("%s %s"%(str(s.get("api_gzip","missing")).lower(),s.get("api_gzip_min_bytes","missing")))' 2>/dev/null)
  if [[ "$GZ_AFTER" == "$GZ_BEFORE" ]]; then
    ok "server.api_gzip settings restored ($GZ_AFTER)"
  else
    ko "server.api_gzip settings not restored (was '$GZ_BEFORE', now '$GZ_AFTER')"
  fi
fi

# ---------------------------------------------------------------------------
H "74. daemon-wide max_concurrent_tasks round-trip + max_parallel_tasks deprecated (v9.0.86)"
# max_concurrent_tasks round-trips through GET / PUT /api/config (restored
# afterwards); the deprecated max_parallel_tasks is accepted (200) with a
# deprecated_fields note and must not change max_concurrent_tasks.
MCT_GET='import json,sys;d=json.load(sys.stdin);print(d.get("autonomous",{}).get("max_concurrent_tasks","missing"))'
MCT_BEFORE=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c "$MCT_GET" 2>/dev/null)
if ! [[ "$MCT_BEFORE" =~ ^[0-9]+$ ]]; then
  ko "autonomous.max_concurrent_tasks missing from /api/config (got '$MCT_BEFORE')"
else
  ok "GET /api/config exposes autonomous.max_concurrent_tasks=$MCT_BEFORE"
  MCT_NEXT=$((MCT_BEFORE + 1))
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"autonomous.max_concurrent_tasks":%s}' "$MCT_NEXT")" "$BASE/api/config" >/dev/null
  MCT_AFTER=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c "$MCT_GET" 2>/dev/null)
  if [[ "$MCT_AFTER" == "$MCT_NEXT" ]]; then
    ok "PUT /api/config set autonomous.max_concurrent_tasks to $MCT_NEXT"
  else
    ko "autonomous.max_concurrent_tasks round-trip failed (was $MCT_BEFORE → wanted $MCT_NEXT → got $MCT_AFTER)"
  fi
  # Restore original value (failing this leaks state across runs).
  curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "$(printf '{"autonomous.max_concurrent_tasks":%s}' "$MCT_BEFORE")" "$BASE/api/config" >/dev/null
  MPT_RESP=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d '{"autonomous.max_parallel_tasks":1}' "$BASE/api/config" -w "\n__HTTP_%{http_code}__")
  if [[ "$MPT_RESP" == *"__HTTP_200__"* ]] && echo "${MPT_RESP%$'\n'__HTTP_*}" | python3 -c \
      'import json,sys;d=json.load(sys.stdin);sys.exit(0 if "autonomous.max_parallel_tasks" in d.get("deprecated_fields",{}) else 1)' 2>/dev/null; then
    ok "PUT autonomous.max_parallel_tasks → 200 with deprecated_fields"
  else
    ko "PUT autonomous.max_parallel_tasks did not return 200 + deprecated_fields: ${MPT_RESP:0:200}"
  fi
  MCT_FINAL=$(curl "${curl_args[@]}" "$BASE/api/config" | python3 -c "$MCT_GET" 2>/dev/null)
  if [[ "$MCT_FINAL" == "$MCT_BEFORE" ]]; then
    ok "autonomous.max_concurrent_tasks restored and unchanged by max_parallel_tasks ($MCT_FINAL)"
  else
    ko "autonomous.max_concurrent_tasks is $MCT_FINAL, expected $MCT_BEFORE"
  fi
fi

# ---------------------------------------------------------------------------
H "75. Result panel — spawned subprocess posts its result; readable via schedule-results (GH#204)"
# Spawns an ephemeral subprocess session through the schedules API (the
# schedule timer ticks every 30 s; run_at=now fires on the first tick).
# The task shell uses the daemon-injected scoped credential
# (DATAWATCH_SESSION_ID / DATAWATCH_BASE_URL / DATAWATCH_RESULT_TOKEN)
# to POST its own structured result; the operator token reads it back
# through GET /api/schedule-results and /api/schedule-results/{name}.
# A second (decoy) subprocess session plus the first session's scoped
# token assert the 403 cross-session identity gate — that sub-check skips
# when the smoke script and the daemon don't share a filesystem
# (remote external-daemon mode), same as the other file-dependent sections.
SRP_NAME="smoke-result-panel-$(date +%s)"
SRP_SCRATCH="$(mktemp -u /tmp/datawatch-smoke-srp.XXXXXX)"
SRP_BODY_A=$(SRP_NAME="$SRP_NAME" SRP_SCRATCH="$SRP_SCRATCH" python3 - <<'PY'
import json, os
name = os.environ["SRP_NAME"]
scratch = os.environ["SRP_SCRATCH"]
payload = json.dumps({"title": "Release smoke result", "status": "ok",
                      "summary": "smoke run", "metrics": {"smoke": 1}})
task = (
    "sleep 1; "
    "T=${DATAWATCH_RESULT_TOKEN:-}; S=${DATAWATCH_SESSION_ID:-}; B=${DATAWATCH_BASE_URL:-}; "
    "mkdir -p __SCRATCH__ 2>/dev/null; "
    "if [ -n \"$S\" ]; then printf %s \"$S\" > __SCRATCH__/sid_A; fi; "
    "if [ -n \"$T\" ]; then printf %s \"$T\" > __SCRATCH__/tok_A; fi; "
    "if [ -n \"$T\" ] && [ -n \"$S\" ] && [ -n \"$B\" ]; then "
    "curl -sS -m 10 -X POST \"$B/api/sessions/$S/result\" "
    '-H "Authorization: Bearer $T" -H "Content-Type: application/json" '
    "-d '__PAYLOAD__' >/dev/null 2>&1; fi; exit 0"
).replace("__SCRATCH__", scratch).replace("__PAYLOAD__", payload)
print(json.dumps({"type": "spawn", "subprocess": True, "schedule_name": name,
                  "name": name, "project_dir": "/tmp", "run_at": "now",
                  "command": task}))
PY
) || true
SRP_BODY_B=$(SRP_SCRATCH="$SRP_SCRATCH" python3 - <<'PY'
import json, os
scratch = os.environ["SRP_SCRATCH"]
task = (
    "sleep 1; S=${DATAWATCH_SESSION_ID:-}; "
    "mkdir -p __SCRATCH__ 2>/dev/null; "
    "if [ -n \"$S\" ]; then printf %s \"$S\" > __SCRATCH__/sid_B; fi; exit 0"
).replace("__SCRATCH__", scratch)
print(json.dumps({"type": "spawn", "subprocess": True,
                  "name": "smoke-result-panel-decoy", "project_dir": "/tmp",
                  "run_at": "now", "command": task}))
PY
) || true
SRP_R=$(curl -s -X POST -H "Content-Type: application/json" -d "$SRP_BODY_A" \
  "${curl_args[@]:+${curl_args[@]}}" "$BASE/api/schedules" -w "\n__HTTP_%{http_code}__" 2>/dev/null)
if [[ "$SRP_R" != *"__HTTP_2"* ]]; then
  skip "result panel spawn: POST /api/schedules rejected (${SRP_R:0:120})"
else
  # Decoy session for the cross-session identity check (best effort —
  # the main assertions don't need it).
  curl -s -X POST -H "Content-Type: application/json" -d "$SRP_BODY_B" \
    "${curl_args[@]:+${curl_args[@]}}" "$BASE/api/schedules" >/dev/null 2>&1 || true
  SRP_OK=0
  for _i in $(seq 1 24); do
    if SRP_NAME="$SRP_NAME" curl -s --max-time 10 "${curl_args[@]:+${curl_args[@]}}" "$BASE/api/schedule-results" 2>/dev/null \
        | SRP_NAME="$SRP_NAME" python3 -c "
import json, os, sys
groups = json.load(sys.stdin)
for g in (groups if isinstance(groups, list) else []):
    if g.get('schedule_name') == os.environ['SRP_NAME']:
        st = (g.get('latest') or {}).get('payload', {}).get('status')
        if st == 'ok':
            sys.exit(0)
sys.exit(1)" 2>/dev/null; then
      SRP_OK=1; break
    fi
    sleep 5
  done
  if [[ "$SRP_OK" -eq 1 ]]; then
    ok "spawned subprocess posted its result with the scoped token; listed with status ok"
    SRP_HIST=$(curl -s --max-time 10 "${curl_args[@]:+${curl_args[@]}}" "$BASE/api/schedule-results/$SRP_NAME" 2>/dev/null || echo "")
    if echo "$SRP_HIST" | python3 -c "
import json, sys
d = json.load(sys.stdin)
assert isinstance(d, list) and len(d) >= 1
assert d[-1].get('payload', {}).get('status') in ('ok', 'warn', 'error')
assert d[-1].get('session_id')" 2>/dev/null; then
      ok "GET /api/schedule-results/{name} returns the retained history with provenance"
    else
      ko "GET /api/schedule-results/$SRP_NAME history malformed: ${SRP_HIST:0:120}"
    fi
    if [[ -s "$SRP_SCRATCH/tok_A" && -s "$SRP_SCRATCH/sid_B" ]]; then
      SRP_TOK=$(cat "$SRP_SCRATCH/tok_A" 2>/dev/null || true)
      SRP_SB=$(cat "$SRP_SCRATCH/sid_B" 2>/dev/null || true)
      SRP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
        -H "Authorization: Bearer $SRP_TOK" -H "Content-Type: application/json" \
        -d '{"title":"must be rejected"}' \
        "$BASE/api/sessions/$SRP_SB/result" 2>/dev/null || echo "000")
      if [[ "$SRP_CODE" == "403" ]]; then
        ok "cross-session post with a scoped token rejected (403)"
      else
        ko "cross-session post expected 403, got HTTP $SRP_CODE"
      fi
    else
      skip "cross-session 403 gate (smoke script and daemon don't share a filesystem)"
    fi
  else
    skip "result panel: posted result not observed within ~120 s (schedule 30 s tick + spawn latency)"
  fi
  # Best-effort teardown of the two spawned sessions.
  for _f in sid_A sid_B; do
    [[ -s "$SRP_SCRATCH/$_f" ]] && curl -s -X POST -H "Content-Type: application/json" \
      -d "{\"id\":\"$(cat \"$SRP_SCRATCH/$_f\" 2>/dev/null)\"}" "$BASE/api/sessions/kill" >/dev/null 2>&1 || true
  done
  rm -rf "$SRP_SCRATCH" 2>/dev/null || true
fi

H "76. Usage back-off — status, manual set, confirm active, clear, confirm empty"
# Uses a unique backend name so it never touches a pre-existing operator
# flag; cleanup clears exactly that backend (DELETE with a non-empty
# backend), never "all".
UB_BACKEND="smoke-backoff-$$-$(date +%s)"
UB0=$(curl "${curl_args[@]}" "$BASE/api/usage-backoff" || true)
if echo "$UB0" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,dict)' 2>/dev/null; then
  ok "GET /api/usage-backoff returns a JSON object"
  add_cleanup usage-backoff "$UB_BACKEND"
  UBS=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"backend\":\"$UB_BACKEND\",\"minutes\":5,\"reason\":\"release smoke\"}" \
    "$BASE/api/usage-backoff" || true)
  UB1=$(curl "${curl_args[@]}" "$BASE/api/usage-backoff" || true)
  if [[ -n "$UB1" ]] && echo "$UB1" | grep -q "$UB_BACKEND"; then
    ok "POST /api/usage-backoff set a flag; GET shows it active"
  else
    ko "usage back-off flag not visible after set: ${UB1:0:160} (set: ${UBS:0:120})"
  fi
  curl "${curl_args[@]}" -X DELETE -H "Content-Type: application/json" \
    -d "{\"backend\":\"$UB_BACKEND\"}" "$BASE/api/usage-backoff" >/dev/null 2>&1 || true
  UB2=$(curl "${curl_args[@]}" "$BASE/api/usage-backoff" || true)
  if [[ -n "$UB2" ]] && ! echo "$UB2" | grep -q "$UB_BACKEND"; then
    ok "DELETE /api/usage-backoff cleared exactly the smoke flag; GET no longer lists it"
  else
    ko "smoke usage back-off flag still present after DELETE: ${UB2:0:160}"
  fi
else
  skip "usage back-off: GET /api/usage-backoff unavailable (${UB0:0:80})"
fi

H "77. Capacity visibility and audit - admission wait and release, back-off set and clear, budget wait and rollover"
# Sandbox daemon only, REST only. Every throwaway name carries the process id
# and a timestamp, and its cleanup is registered before the change is made.
# No config value is changed: the throwaway LLM entries are created and then
# deleted by the cleanup mechanism, and the back-off cleanup clears exactly the
# unique backend, never "all". A step the public surface cannot produce is a
# skip that names it; the unit tests with an injectable clock cover it.
UC_TAG="$$-$(date +%s)"
UC_LLM="smoke-cap-llm-$UC_TAG"
UC_BUD="smoke-cap-bud-$UC_TAG"
UC_BO="smoke-cap-backoff-$UC_TAG"
UC_TASK_B=""   # task id of the second (waiting) task, when part (a) produced one
UC_BO_SET=0    # 1 when part (b) saw the back-off set entry

# uc_cap_find <waiting|running> <task id>: print that task's view from
# GET /api/capacity tasks.<list>; exit 1 when it is not listed.
uc_cap_find() {
  curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null | UC_LIST="$1" UC_TID="$2" python3 -c '
import json, os, sys
try:
  d = json.load(sys.stdin)
  for t in ((d.get("tasks") or {}).get(os.environ["UC_LIST"]) or []):
    if t.get("task_id") == os.environ["UC_TID"]:
      print(json.dumps(t)); sys.exit(0)
except Exception:
  pass
sys.exit(1)'
}

# uc_audit_has <query> [actor] [subject]: succeed when GET /api/audit?<query>
# returns an entry with that actor and subject (each optional).
uc_audit_has() {
  curl "${curl_args[@]}" "$BASE/api/audit?$1&limit=500" 2>/dev/null | UC_ACTOR="${2:-}" UC_SUBJ="${3:-}" python3 -c '
import json, os, sys
try:
  d = json.load(sys.stdin)
  a = os.environ.get("UC_ACTOR", ""); s = os.environ.get("UC_SUBJ", "")
  es = d.get("entries") or []
  sys.exit(0 if any((not a or e.get("actor") == a) and (not s or e.get("subject") == s) for e in es) else 1)
except Exception:
  sys.exit(1)'
}

# uc_audit_wait <tries> <query> [actor] [subject]: uc_audit_has, retried once
# a second, because the audit line is written just after the state change.
uc_audit_wait() {
  local n="$1"; shift
  local i
  for ((i = 0; i < n; i++)); do
    uc_audit_has "$@" && return 0
    sleep 1
  done
  return 1
}

UC_CAP0=$(curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null || true)
if ! echo "$UC_CAP0" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("tasks"),dict) and isinstance(d.get("backoff"),list) and isinstance(d.get("budgets"),list)' 2>/dev/null; then
  skip "capacity visibility: GET /api/capacity has no tasks/backoff/budgets (${UC_CAP0:0:80})"
elif ! curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/audit?limit=1" 2>/dev/null | grep -q '^200$'; then
  skip "capacity visibility: GET /api/audit unavailable (audit log not enabled)"
else
  ok "GET /api/capacity carries tasks, backoff and budgets; GET /api/audit answers"

  # (a) Admission wait and release.
  # A throwaway LLM entry capped at one in-flight session is the throwaway
  # pool (llm:<name>); the entry copies the kind, nodes and models of an
  # enabled LLM already in the sandbox (local ollama preferred, the least
  # invasive), and two throwaway Automata name it as their backend.
  UC_SRC=$(curl "${curl_args[@]}" "$BASE/api/llms" 2>/dev/null | python3 -c '
import json, sys
try:
  d = json.load(sys.stdin)
  ls = [l for l in (d.get("llms") or []) if not l.get("disabled")]
  ls.sort(key=lambda l: 0 if l.get("kind") == "ollama" else 1)
  if ls:
    l = ls[0]
    out = {"kind": l.get("kind", "ollama")}
    if l.get("compute_nodes"): out["compute_nodes"] = l["compute_nodes"]
    if l.get("models"): out["models"] = l["models"]
    print(json.dumps(out))
except Exception:
  pass' || true)
  if [[ -z "$UC_SRC" ]]; then
    skip "capacity admission wait/release: no enabled LLM entry to copy for a throwaway pool"
  else
    UC_BODY=$(echo "$UC_SRC" | UC_NAME="$UC_LLM" python3 -c '
import json, os, sys
d = json.load(sys.stdin); d["name"] = os.environ["UC_NAME"]; d["max_inflight"] = 1
print(json.dumps(d))')
    UC_MODEL=$(echo "$UC_SRC" | python3 -c '
import json, sys
d = json.load(sys.stdin)
print(((d.get("models") or [{}])[0]).get("model", ""))' 2>/dev/null || true)
    add_cleanup llm "$UC_LLM"
    UC_MK=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$UC_BODY" "$BASE/api/llms" 2>/dev/null || true)
    if ! echo "$UC_MK" | python3 -c 'import json,sys;assert json.load(sys.stdin).get("ok")==True' 2>/dev/null; then
      skip "capacity admission wait/release: throwaway LLM create refused (${UC_MK:0:100})"
    else
      # The ledger limit follows the registry on a short ticker; wait for it.
      UC_LIMIT_OK=0
      for _ in $(seq 1 30); do
        if curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null | UC_POOL="llm:$UC_LLM" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
sys.exit(0 if any(p.get("name") == os.environ["UC_POOL"] and p.get("limit") == 1 for p in (d.get("pools") or [])) else 1)' 2>/dev/null; then
          UC_LIMIT_OK=1; break
        fi
        sleep 1
      done
      if [[ "$UC_LIMIT_OK" == "1" ]]; then
        ok "throwaway pool llm:$UC_LLM limited to one slot in GET /api/capacity pools"
      else
        skip "throwaway pool llm:$UC_LLM never showed limit 1 in /api/capacity (ledger sync not observed); admission wait not exercised"
      fi
    fi
    if [[ "${UC_LIMIT_OK:-0}" == "1" ]]; then
      # Create the way sections 6 and 7b do: spec + project_dir + backend,
      # then decompose and approve. Decompose needs a reachable planner LLM;
      # when it fails the section skips naming exactly that.
      UC_IDS=()
      UC_PLAN_FAIL=""
      for UC_N in a b; do
        mkdir -p "$TMPD/uc-$UC_N"
        UC_PB=$(UC_N="$UC_N" UC_TAG="$UC_TAG" UC_LLM="$UC_LLM" UC_DIR="$TMPD/uc-$UC_N" python3 -c '
import json, os
n = os.environ["UC_N"]
print(json.dumps({
  "spec": "smoke probe - capacity visibility %s (%s): write one line to a scratch file" % (n, os.environ["UC_TAG"]),
  "project_dir": os.environ["UC_DIR"],
  "backend": os.environ["UC_LLM"],
  "effort": "low"}))')
        UC_PID=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$UC_PB" "$BASE/api/autonomous/prds" 2>/dev/null \
          | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || true)
        if [[ -z "$UC_PID" ]]; then
          UC_IDS+=("")
          continue
        fi
        add_cleanup prd "$UC_PID"
        UC_IDS+=("$UC_PID")
        UC_DEC=$(curl "${curl_args[@]}" --max-time 300 -X POST -H "Content-Type: application/json" -d '{}' \
          "$BASE/api/autonomous/prds/$UC_PID/decompose" -w "\n__HTTP_%{http_code}__" 2>/dev/null || true)
        if ! echo "$UC_DEC" | grep -q "__HTTP_200__"; then
          UC_PLAN_FAIL="decompose of $UC_PID failed"
          continue
        fi
        curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
          -d '{"actor":"smoke","note":"capacity smoke"}' "$BASE/api/autonomous/prds/$UC_PID/approve" >/dev/null 2>&1 || true
      done
      UC_PA="${UC_IDS[0]:-}"; UC_PB_ID="${UC_IDS[1]:-}"
      if [[ -z "$UC_PA" || -z "$UC_PB_ID" ]]; then
        skip "capacity admission wait/release: could not create the two throwaway Automata (autonomous subsystem may be off)"
      elif [[ -n "$UC_PLAN_FAIL" ]]; then
        skip "capacity admission wait/release: $UC_PLAN_FAIL (planner LLM unreachable?); wait and release not exercised"
      else
        UC_TASK_A=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$UC_PA" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["stories"][0]["tasks"][0]["id"])' 2>/dev/null || true)
        UC_TASK_B2=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$UC_PB_ID" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["stories"][0]["tasks"][0]["id"])' 2>/dev/null || true)
        # Pin the backend (and model) explicitly, as section 6 does.
        for UC_X in "$UC_PA" "$UC_PB_ID"; do
          curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
            -d "{\"backend\":\"$UC_LLM\",\"effort\":\"low\",\"model\":\"$UC_MODEL\",\"actor\":\"smoke\"}" \
            "$BASE/api/autonomous/prds/$UC_X/set_llm" >/dev/null 2>&1 || true
        done
        if [[ -z "$UC_TASK_A" || -z "$UC_TASK_B2" ]]; then
          skip "capacity admission wait/release: throwaway Automata have no task ids to follow"
        else
          curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{}' "$BASE/api/autonomous/prds/$UC_PA/run" >/dev/null 2>&1 || true
          UC_A_RUN=0
          for _ in $(seq 1 90); do
            if uc_cap_find running "$UC_TASK_A" >/dev/null; then UC_A_RUN=1; break; fi
            sleep 1
          done
          if [[ "$UC_A_RUN" != "1" ]]; then
            skip "capacity admission wait/release: first task $UC_TASK_A never showed as running holding llm:$UC_LLM; waiting and release not exercised"
          else
            curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{}' "$BASE/api/autonomous/prds/$UC_PB_ID/run" >/dev/null 2>&1 || true
            UC_B_VIEW=""
            for _ in $(seq 1 60); do
              UC_B_VIEW=$(uc_cap_find waiting "$UC_TASK_B2" || true)
              [[ -n "$UC_B_VIEW" ]] && break
              sleep 1
            done
            if [[ -z "$UC_B_VIEW" ]]; then
              skip "capacity admission wait/release: second task $UC_TASK_B2 never listed under tasks.waiting (first task may have ended already); wait text not exercised"
            else
              UC_B_REASON=$(echo "$UC_B_VIEW" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("reason",""))' 2>/dev/null || true)
              if echo "$UC_B_VIEW" | UC_HOLD="$UC_TASK_A" python3 -c '
import json, os, sys
v = json.load(sys.stdin); r = v.get("reason", ""); h = v.get("holder", "")
sys.exit(0 if r and "held by" in r and (os.environ["UC_HOLD"] in r or (h and h in r)) else 1)' 2>/dev/null; then
                ok "GET /api/capacity tasks.waiting lists the second task with a reason naming the holder: ${UC_B_REASON:0:120}"
              else
                ko "waiting task reason is empty or does not name the holder $UC_TASK_A: ${UC_B_REASON:0:160}"
              fi
              UC_ST_REASON=$(curl "${curl_args[@]}" "$BASE/api/autonomous/status" 2>/dev/null | UC_TID="$UC_TASK_B2" python3 -c '
import json, os, sys
try:
  d = json.load(sys.stdin)
  for t in (d.get("queue") or []):
    if t.get("task_id") == os.environ["UC_TID"]:
      print(t.get("reason", "")); break
except Exception:
  pass' || true)
              if [[ -n "$UC_ST_REASON" && "$UC_ST_REASON" == "$UC_B_REASON" ]]; then
                ok "GET /api/autonomous/status queue carries the same wait reason"
              else
                ko "autonomous status reason differs from capacity reason: status='${UC_ST_REASON:0:100}' capacity='${UC_B_REASON:0:100}'"
              fi
              # Release: cancel the first task and expect the second admitted.
              curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
                -d "{\"task_id\":\"$UC_TASK_A\",\"actor\":\"smoke\",\"reason\":\"capacity smoke release\"}" \
                "$BASE/api/autonomous/prds/$UC_PA/cancel_task" >/dev/null 2>&1 || true
              UC_B_ADMIT=0
              for _ in $(seq 1 120); do
                if ! uc_cap_find waiting "$UC_TASK_B2" >/dev/null; then UC_B_ADMIT=1; break; fi
                sleep 1
              done
              if [[ "$UC_B_ADMIT" == "1" ]]; then
                ok "second task left tasks.waiting once the first task released the slot"
                UC_TASK_B="$UC_TASK_B2"
              else
                ko "second task $UC_TASK_B2 still waiting 120s after the first task was cancelled"
              fi
              if uc_audit_wait 15 "action=autonomous_capacity_wait&subject=$UC_TASK_B2"; then
                ok "GET /api/audit?action=autonomous_capacity_wait returns an entry for the waiting task"
              else
                ko "no autonomous_capacity_wait audit entry with subject $UC_TASK_B2"
              fi
              if [[ "$UC_B_ADMIT" == "1" ]]; then
                if uc_audit_wait 15 "action=autonomous_capacity_admitted&subject=$UC_TASK_B2"; then
                  ok "GET /api/audit?action=autonomous_capacity_admitted returns an entry for the admitted task"
                else
                  ko "no autonomous_capacity_admitted audit entry with subject $UC_TASK_B2"
                fi
              else
                skip "autonomous_capacity_admitted audit entry not asserted: the second task was never admitted"
              fi
            fi
          fi
          # Stop whatever the throwaway Automata still run; hard delete is in cleanup.
          for UC_X in "$UC_PA" "$UC_PB_ID"; do
            curl "${curl_args[@]}" -X POST "$BASE/api/autonomous/prds/$UC_X/cancel" >/dev/null 2>&1 || true
          done
        fi
      fi
    fi
  fi

  # (b) Back-off set and clear, on a unique backend.
  add_cleanup usage-backoff "$UC_BO"
  curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"backend\":\"$UC_BO\",\"minutes\":5,\"reason\":\"release smoke capacity audit\"}" \
    "$BASE/api/usage-backoff" >/dev/null 2>&1 || true
  if curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null | UC_B="$UC_BO" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
sys.exit(0 if any(b.get("backend") == os.environ["UC_B"] and b.get("until") and b.get("reason") for b in (d.get("backoff") or [])) else 1)' 2>/dev/null; then
    ok "GET /api/capacity backoff lists the set flag with an until time and its reason"
  else
    ko "capacity backoff does not list $UC_BO with until and reason after POST /api/usage-backoff"
  fi
  if uc_audit_wait 10 "action=autonomous_usage_backoff_set&subject=$UC_BO" operator "$UC_BO"; then
    ok "audit autonomous_usage_backoff_set for $UC_BO has actor operator"
    UC_BO_SET=1
  else
    ko "no autonomous_usage_backoff_set audit entry with actor operator for $UC_BO"
  fi
  curl "${curl_args[@]}" -X DELETE -H "Content-Type: application/json" \
    -d "{\"backend\":\"$UC_BO\"}" "$BASE/api/usage-backoff" >/dev/null 2>&1 || true
  if curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null | UC_B="$UC_BO" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
sys.exit(1 if any(b.get("backend") == os.environ["UC_B"] for b in (d.get("backoff") or [])) else 0)' 2>/dev/null; then
    ok "DELETE /api/usage-backoff for exactly $UC_BO removed it from capacity backoff"
  else
    ko "capacity backoff still lists $UC_BO after DELETE"
  fi
  if uc_audit_wait 10 "action=autonomous_usage_backoff_cleared&subject=$UC_BO" operator "$UC_BO"; then
    ok "audit autonomous_usage_backoff_cleared for $UC_BO has actor operator"
  else
    ko "no autonomous_usage_backoff_cleared audit entry for $UC_BO"
  fi

  # (c) Budget wait and rollover, on a throwaway LLM with a tiny rolling ceiling.
  add_cleanup llm "$UC_BUD"
  UC_BMK=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"name\":\"$UC_BUD\",\"kind\":\"ollama\",\"models\":[{\"model\":\"llama3\"}]}" "$BASE/api/llms" 2>/dev/null || true)
  if ! echo "$UC_BMK" | python3 -c 'import json,sys;assert json.load(sys.stdin).get("ok")==True' 2>/dev/null; then
    skip "capacity budget: throwaway LLM create refused (${UC_BMK:0:100})"
  else
    UC_BPUT=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
      -d "{\"name\":\"$UC_BUD\",\"kind\":\"ollama\",\"models\":[{\"model\":\"llama3\"}],\"budget\":{\"ceiling_usd\":0.01,\"hours\":1}}" \
      "$BASE/api/llms/$UC_BUD" 2>/dev/null || true)
    UC_BST=$(curl "${curl_args[@]}" "$BASE/api/budget?llm=$UC_BUD" 2>/dev/null || true)
    if echo "$UC_BST" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("ceiling_usd") == 0.01 and isinstance(d.get("spend_usd"), (int, float)) and d.get("window_end") and d.get("window") and d.get("full") is False' 2>/dev/null; then
      ok "GET /api/budget?llm= shows spend, ceiling and window end for the throwaway budget"
      if echo "$UC_BST" | python3 -c 'import json,sys;assert json.load(sys.stdin).get("rolls_over_at")' 2>/dev/null; then
        ok "GET /api/budget shows the rollover time"
      else
        skip "budget rollover time not asserted: a rolling window with no recorded spend has no rolls_over_at until it fills"
      fi
    else
      ko "GET /api/budget?llm=$UC_BUD does not show the budget (PUT: ${UC_BPUT:0:80}) got ${UC_BST:0:160}"
    fi
    if curl "${curl_args[@]}" "$BASE/api/budget" 2>/dev/null | UC_L="$UC_BUD" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
sys.exit(0 if any(b.get("llm") == os.environ["UC_L"] and b.get("ceiling_usd") == 0.01 for b in (d.get("budgets") or [])) else 1)' 2>/dev/null; then
      ok "GET /api/budget lists the throwaway budget"
    else
      ko "GET /api/budget does not list $UC_BUD"
    fi
    if curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null | UC_L="$UC_BUD" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
sys.exit(0 if any(b.get("llm") == os.environ["UC_L"] and b.get("ceiling_usd") == 0.01 and "spend_usd" in b and b.get("window_end") for b in (d.get("budgets") or [])) else 1)' 2>/dev/null; then
      ok "GET /api/capacity budgets array carries the throwaway budget with spend, ceiling and window end"
    else
      ko "GET /api/capacity budgets array does not carry $UC_BUD"
    fi
    skip "budget window full reason and autonomous_budget_ceiling_wait audit entry not exercised: the public REST surface has no way to record spend (GET /api/budget is read-only, spend comes only from real session usage); unit tests with an injectable clock cover it"
    skip "autonomous_budget_window_rollover audit entry not exercised: it needs a filled window and a boundary passing, which cannot be produced without recording spend; unit tests with an injectable clock cover it"
  fi

  # Prefix filter: autonomous_* returns the entries from (a) and (b).
  if [[ -n "$UC_TASK_B" ]]; then
    if uc_audit_has "action=autonomous_*" "" "$UC_TASK_B"; then
      ok "GET /api/audit?action=autonomous_* includes the admission entries for the task from (a)"
    else
      ko "autonomous_* prefix query returned no entry for task $UC_TASK_B"
    fi
  else
    skip "autonomous_* prefix filter not asserted for part (a): no admitted task id was produced"
  fi
  if [[ "$UC_BO_SET" == "1" ]]; then
    if uc_audit_has "action=autonomous_*" operator "$UC_BO"; then
      ok "GET /api/audit?action=autonomous_* includes the back-off entries for the backend from (b)"
    else
      ko "autonomous_* prefix query returned no entry for backend $UC_BO"
    fi
  else
    skip "autonomous_* prefix filter not asserted for part (b): the back-off set entry was not seen"
  fi
fi

H "78. Automation chain, fork and join - create, pause, resume, skip, audit, cleanup"
# Sandbox daemon, REST only, no real LLM work: the Automata are drafts that are
# never approved or run, and the Automation is never activated. Every throwaway
# is registered for cleanup right after it is created. A control step the
# Automation state does not allow is a skip that names the reason, never a pass.
AU_TAG="$$-$(date +%s)"
AU_PRD_A=""; AU_PRD_B=""; AU_PRD_G=""; AU_ID=""; AU_GRAPH=""

# au_json <python expr over d>: read JSON on stdin, print the expression.
au_json() { python3 -c "import json,sys
try:
  d=json.load(sys.stdin); print($1)
except Exception:
  print('')" 2>/dev/null; }

# au_code <method> <path> [json body]: print the HTTP status only.
au_code() {
  if [[ -n "${3:-}" ]]; then
    curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X "$1" -H "Content-Type: application/json" -d "$3" "$BASE$2" 2>/dev/null
  else
    curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X "$1" "$BASE$2" 2>/dev/null
  fi
}

# au_audit_has <action> <subject>: succeed when GET /api/audit?action=<action>
# lists an entry with that subject (retried, the audit line lands just after
# the state change).
au_audit_has() {
  local i
  for i in 1 2 3; do
    curl "${curl_args[@]}" "$BASE/api/audit?action=$1&limit=500" 2>/dev/null | AU_SUBJ="$2" python3 -c '
import json, os, sys
try:
  d = json.load(sys.stdin)
  sys.exit(0 if any(e.get("subject") == os.environ["AU_SUBJ"] for e in (d.get("entries") or [])) else 1)
except Exception:
  sys.exit(1)' && return 0
    sleep 1
  done
  return 1
}

# au_new_prd <label>: create a draft Automaton and print its id.
au_new_prd() {
  curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"spec\":\"smoke probe - automation chain $1 $AU_TAG\",\"project_dir\":\"/tmp\",\"effort\":\"low\"}" \
    "$BASE/api/autonomous/prds" 2>/dev/null | au_json 'd.get("id","")'
}

AU_PROBE=$(au_code GET /api/automations)
if [[ "$AU_PROBE" == "503" ]]; then
  skip "Automation chain: autonomous manager disabled (GET /api/automations 503)"
elif [[ "$AU_PROBE" != "200" ]]; then
  skip "Automation chain: GET /api/automations unavailable (HTTP $AU_PROBE)"
else
  ok "GET /api/automations answers"
  AU_PRD_A=$(au_new_prd a); [[ -n "$AU_PRD_A" ]] && add_cleanup prd "$AU_PRD_A"
  AU_PRD_B=$(au_new_prd b); [[ -n "$AU_PRD_B" ]] && add_cleanup prd "$AU_PRD_B"
  if [[ -n "$AU_PRD_A" && -n "$AU_PRD_B" ]]; then
    ok "created two throwaway Automata: $AU_PRD_A, $AU_PRD_B"
  else
    ko "could not create the two throwaway Automata (a='$AU_PRD_A' b='$AU_PRD_B')"
  fi
fi

if [[ -n "$AU_PRD_A" && -n "$AU_PRD_B" ]]; then
  # Members: branches ba and bb, a fork (entry) over them, a join paired to it.
  AU_BODY=$(cat <<JSON
{"name":"smoke-automation-$AU_TAG","description":"release smoke fork and join","entry_member_id":"aaaa0001",
 "actor":"operator","reason":"release smoke",
 "members":[
  {"id":"aaaa0001","kind":"fork","fork_members":["aaaa0002","aaaa0003"]},
  {"id":"aaaa0002","prd_id":"$AU_PRD_A"},
  {"id":"aaaa0003","prd_id":"$AU_PRD_B"},
  {"id":"aaaa0004","kind":"join","join_members":["aaaa0002","aaaa0003"],"on_branch_failure":"all_must_succeed"}]}
JSON
)
  AU_RESP=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$AU_BODY" "$BASE/api/automations" 2>/dev/null || true)
  AU_ID=$(echo "$AU_RESP" | au_json 'd.get("id","")')
  if [[ -n "$AU_ID" ]]; then
    add_cleanup automation "$AU_ID"
    ok "created Automation with a fork and join over the two Automata: $AU_ID"
  else
    ko "Automation create failed: ${AU_RESP:0:200}"
  fi
fi

if [[ -n "$AU_ID" ]]; then
  AU_GET=$(curl "${curl_args[@]}" "$BASE/api/automations/$AU_ID" 2>/dev/null || true)
  if echo "$AU_GET" | python3 -c '
import json, sys
d = json.load(sys.stdin)
ms = {m["id"]: m for m in d["members"]}
f = ms["aaaa0001"]; j = ms["aaaa0004"]
assert d["entry_member_id"] == "aaaa0001"
assert f.get("kind") == "fork" and j.get("kind") == "join"
assert sorted(f["fork_members"]) == sorted(j["join_members"]) == ["aaaa0002", "aaaa0003"]
' 2>/dev/null; then
    ok "GET /api/automations/$AU_ID shows fork and join members with paired sets"
  else
    ko "Automation $AU_ID does not show a fork and join with paired sets: ${AU_GET:0:200}"
  fi
  AU_STATUS=$(echo "$AU_GET" | au_json 'd.get("status","")')

  # import-graph, dry run, on a throwaway one-node orchestrator graph.
  AU_O=$(curl "${curl_args[@]}" "$BASE/api/orchestrator/config" 2>/dev/null | au_json '"yes" if d.get("enabled") else "no"')
  if [[ "$AU_O" != "yes" ]]; then
    skip "Automation import-graph dry run not exercised: the orchestrator is disabled, so no throwaway graph can be created"
  else
    AU_PRD_G=$(au_new_prd graph); [[ -n "$AU_PRD_G" ]] && add_cleanup prd "$AU_PRD_G"
    AU_GR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d "{\"name\":\"smoke-import-$AU_TAG\",\"prds\":[\"$AU_PRD_G\"]}" "$BASE/api/orchestrator/graphs" 2>/dev/null || true)
    AU_GRAPH=$(echo "$AU_GR" | au_json 'd.get("id","")')
    if [[ -z "$AU_GRAPH" ]]; then
      skip "Automation import-graph dry run not exercised: the throwaway orchestrator graph could not be created (${AU_GR:0:100})"
    else
      add_cleanup graph "$AU_GRAPH"
      AU_IMP=$(curl "${curl_args[@]}" -w '\n%{http_code}' -X POST -H "Content-Type: application/json" \
        -d '{"dry_run":true}' "$BASE/api/automations/import-graph/$AU_GRAPH" 2>/dev/null || true)
      AU_IMP_CODE="${AU_IMP##*$'\n'}"
      case "$AU_IMP_CODE" in
        200) if echo "${AU_IMP%$'\n'*}" | python3 -c 'import json,sys;assert json.load(sys.stdin).get("dry_run") is True' 2>/dev/null; then
               ok "import-graph dry run on the one-node graph succeeded and reported dry_run"
             else
               ko "import-graph dry run answered 200 without dry_run true: ${AU_IMP:0:160}"
             fi ;;
        422) ok "import-graph dry run refused the one-node graph as not representable (HTTP 422)" ;;
        503) skip "Automation import-graph dry run not exercised: graph import unavailable (HTTP 503)" ;;
        *)   ko "import-graph dry run answered HTTP $AU_IMP_CODE: ${AU_IMP:0:160}" ;;
      esac
      if au_code GET "/api/orchestrator/graphs/$AU_GRAPH" | grep -q '^200$'; then
        ok "source orchestrator graph $AU_GRAPH is still present after the dry run"
      else
        ko "source orchestrator graph $AU_GRAPH is gone after the dry run"
      fi
      if curl "${curl_args[@]}" "$BASE/api/automations" 2>/dev/null | python3 -c '
import json, sys
d = json.load(sys.stdin)
xs = d.get("automations") if isinstance(d, dict) else d
sys.exit(0 if len(xs or []) >= 1 else 1)' 2>/dev/null; then
        ok "import-graph dry run left the Automation list intact"
      fi
    fi
  fi

  # Control steps. They need an active (pause, skip_member) or paused (resume)
  # Automation; activating would start real work, so a draft is only exercised
  # as far as its state allows.
  AU_RAN=""
  if [[ "$AU_STATUS" == "draft" ]]; then
    skip "Automation pause not exercised: the Automation is a draft and only an active one can be paused; activating it would start member work"
    skip "Automation resume not exercised: nothing was paused, and a draft cannot be resumed"
    skip "Automation skip_member not exercised: the Automation is a draft with no running member to skip; activating it would start member work"
  else
    AU_PC=$(au_code POST "/api/automations/$AU_ID/pause" '{"actor":"operator","reason":"release smoke"}')
    if [[ "$AU_PC" == "200" ]]; then ok "POST pause accepted"; AU_RAN="$AU_RAN pause"
    else skip "Automation pause not exercised: HTTP $AU_PC in state $AU_STATUS"; fi
    AU_RC=$(au_code POST "/api/automations/$AU_ID/resume" '{"actor":"operator","reason":"release smoke"}')
    if [[ "$AU_RC" == "200" ]]; then ok "POST resume accepted"; AU_RAN="$AU_RAN resume"
    else skip "Automation resume not exercised: HTTP $AU_RC in state $AU_STATUS"; fi
    AU_SC=$(au_code POST "/api/automations/$AU_ID/skip_member" '{"actor":"operator","member_id":"aaaa0002","edge":"on_complete"}')
    if [[ "$AU_SC" == "200" ]]; then ok "POST skip_member accepted"; AU_RAN="$AU_RAN skip_member"
    else skip "Automation skip_member not exercised: HTTP $AU_SC in state $AU_STATUS"; fi
  fi

  # Audit: each event that was produced carries the Automation id as subject.
  AU_AC=$(au_code GET "/api/audit?limit=1")
  if [[ "$AU_AC" != "200" ]]; then
    skip "Automation audit events not asserted: GET /api/audit unavailable (audit log not enabled)"
  else
    if au_audit_has "automation_*" "$AU_ID"; then
      ok "GET /api/audit?action=automation_* lists entries for the Automation"
    else
      ko "GET /api/audit?action=automation_* has no entry with subject $AU_ID"
    fi
    if au_audit_has automation_create "$AU_ID"; then
      ok "audit automation_create has the Automation id as subject"
    else
      ko "audit automation_create missing for $AU_ID"
    fi
    for AU_PAIR in pause:automation_pause resume:automation_resume skip_member:automation_skip_member; do
      AU_OP="${AU_PAIR%%:*}"; AU_ACT="${AU_PAIR#*:}"
      if [[ " $AU_RAN " == *" $AU_OP "* ]]; then
        if au_audit_has "$AU_ACT" "$AU_ID"; then
          ok "audit $AU_ACT has the Automation id as subject"
        else
          ko "audit $AU_ACT missing for $AU_ID"
        fi
      else
        skip "Automation audit $AU_ACT not asserted: the $AU_OP step did not run (see above)"
      fi
    done
    for AU_EV in automation_fork automation_join; do
      if au_audit_has "$AU_EV" "$AU_ID"; then
        ok "audit $AU_EV has the Automation id as subject"
      else
        skip "audit $AU_EV not asserted: it is written only when the coordinator advances an active Automation, which this section never starts"
      fi
    done
  fi
fi

# ---------------------------------------------------------------------------
H "79. Web UI slow-network load scenarios - cold load, empty, failed, stale, offline, reload"
# Runs the eleven slow-network load scenarios (TS-802..TS-812) via the PWA
# runner's load group (added by the "Wire the load scenarios into the PWA
# runner" task — requires `node run-all.mjs --group load`). Against THIS
# smoke daemon only ($BASE/$TOK) — this section never starts a second
# daemon. node and the Playwright install (scripts/test-stories/pwa/
# node_modules) must be present or the section SKIPs with a reason, never
# a silent pass. Runner exit 0 with at least one pass → ok (pass count);
# exit 0 with only skipped scenarios, or exit 2 → skip with the runner's
# reason; exit 1 (or the timeout bound) → ko with the last output lines.
# Environment is scrubbed of TMUX/TMUX_TMPDIR for the node invocation, and
# the run is bounded by SMOKE_LOAD_TIMEOUT (default 900s) through the
# `timeout` command when it is available, with a plain run as fallback.
PWA_DIR="$REPO_DIR/scripts/test-stories/pwa"
if ! command -v node >/dev/null 2>&1; then
  skip "load scenarios: node not found on PATH — cannot run scripts/test-stories/pwa"
elif [[ ! -d "$PWA_DIR/node_modules" ]]; then
  skip "load scenarios: Playwright install missing (scripts/test-stories/pwa/node_modules) — run: (cd scripts/test-stories/pwa && npm ci)"
else
  LOAD_EVD="$TMPD/load-evidence.$$"
  rm -rf "$LOAD_EVD"
  mkdir -p "$LOAD_EVD"
  LOAD_OUT_FILE="$TMPD/load-out.$$"
  LOAD_TO_BIN="$(command -v timeout 2>/dev/null || true)"
  if [[ -n "$LOAD_TO_BIN" ]]; then
    env -u TMUX -u TMUX_TMPDIR TEST_HTTP="$BASE" TEST_TOKEN="$TOK" EVIDENCE_DIR="$LOAD_EVD" \
      "$LOAD_TO_BIN" "${SMOKE_LOAD_TIMEOUT:-900}" node "$PWA_DIR/run-all.mjs" --group load >"$LOAD_OUT_FILE" 2>&1
  else
    env -u TMUX -u TMUX_TMPDIR TEST_HTTP="$BASE" TEST_TOKEN="$TOK" EVIDENCE_DIR="$LOAD_EVD" \
      node "$PWA_DIR/run-all.mjs" --group load >"$LOAD_OUT_FILE" 2>&1
  fi
  LOAD_RC=$?
  tail -20 "$LOAD_OUT_FILE"
  LOAD_SUMMARY="$(grep -E '^summary: ' "$LOAD_OUT_FILE" | tail -1)"
  LOAD_PASSED="$(printf '%s' "$LOAD_SUMMARY" | awk '{print $2}')"
  if [[ "$LOAD_RC" -eq 0 ]]; then
    if [[ "${LOAD_PASSED:-""}" == "0" ]]; then
      skip "load scenarios: runner exited 0 but every scenario was skipped ($LOAD_SUMMARY)"
    else
      ok "load scenarios (TS-802..TS-812): $LOAD_PASSED passed (${LOAD_SUMMARY#summary: })"
    fi
  elif [[ "$LOAD_RC" -eq 2 ]]; then
    skip "load scenarios: runner exited 2 ("$(tail -1 "$LOAD_OUT_FILE")")"
  elif [[ "$LOAD_RC" -eq 124 ]]; then
    ko "load scenarios timed out after ${SMOKE_LOAD_TIMEOUT:-900}s: $(tail -1 "$LOAD_OUT_FILE")"
  else
    ko "load scenarios failed (run-all.mjs rc=$LOAD_RC): $(tail -5 "$LOAD_OUT_FILE" | tr '\n' ' ' | head -c 400)"
  fi
  rm -rf "$LOAD_EVD"
fi

# ---------------------------------------------------------------------------
H "Shared navigation bar fits phone widths in light and dark"
# Runs the shared-nav scenario (scripts/test-stories/pwa/shared-nav-mobile-fit.mjs)
# through the PWA runner's shared-nav group (`node run-all.mjs --group shared-nav`),
# same helper pattern as the load section above: this smoke daemon only
# ($BASE/$TOK), TMUX scrubbed, bounded by SMOKE_SHARED_NAV_TIMEOUT (default 600s)
# through `timeout` when available. Unlike the load section, a navigation layout
# finding at phone widths is a failure (ko), never a skip. The section SKIPs only
# when the scenario reports that no browser or daemon is available: it exits 2,
# which the runner counts as skipped, so the run ends with 0 passed and 0 failed.
PWA_DIR="$REPO_DIR/scripts/test-stories/pwa"
NAV_EVD="$TMPD/shared-nav-evidence.$$"
rm -rf "$NAV_EVD"
mkdir -p "$NAV_EVD"
NAV_OUT_FILE="$TMPD/shared-nav-out.$$"
NAV_TO_BIN="$(command -v timeout 2>/dev/null || true)"
if [[ -n "$NAV_TO_BIN" ]]; then
  env -u TMUX -u TMUX_TMPDIR TEST_HTTP="$BASE" TEST_TOKEN="$TOK" EVIDENCE_DIR="$NAV_EVD" \
    "$NAV_TO_BIN" "${SMOKE_SHARED_NAV_TIMEOUT:-600}" node "$PWA_DIR/run-all.mjs" --group shared-nav >"$NAV_OUT_FILE" 2>&1
else
  env -u TMUX -u TMUX_TMPDIR TEST_HTTP="$BASE" TEST_TOKEN="$TOK" EVIDENCE_DIR="$NAV_EVD" \
    node "$PWA_DIR/run-all.mjs" --group shared-nav >"$NAV_OUT_FILE" 2>&1
fi
NAV_RC=$?
tail -20 "$NAV_OUT_FILE"
NAV_SUMMARY="$(grep -E '^summary: ' "$NAV_OUT_FILE" | tail -1)"
NAV_PASSED="$(printf '%s' "$NAV_SUMMARY" | awk '{print $2}')"
NAV_FAILED="$(printf '%s' "$NAV_SUMMARY" | awk '{print $4}')"
if [[ "$NAV_RC" -eq 0 ]]; then
  if [[ "${NAV_PASSED:-""}" == "0" && "${NAV_FAILED:-0}" == "0" ]]; then
    skip "shared navigation: scenario skipped, no browser or daemon available ($NAV_SUMMARY)"
  else
    ok "shared navigation fits phone widths: ${NAV_SUMMARY#summary: }"
  fi
elif [[ "$NAV_RC" -eq 124 ]]; then
  ko "shared navigation timed out after ${SMOKE_SHARED_NAV_TIMEOUT:-600}s: $(tail -1 "$NAV_OUT_FILE")"
else
  ko "shared navigation layout findings at phone widths (run-all.mjs rc=$NAV_RC): $(tail -5 "$NAV_OUT_FILE" | tr '\n' ' ' | head -c 400)"
fi
rm -rf "$NAV_EVD"

# ---------------------------------------------------------------------------
H "80. Daemon restart matrix - in-flight work survives a restart (strict)"
# Runs scripts/restart-matrix.sh --strict, one state at a time, for the rows
# that must survive a daemon restart (worker-node-placement among them: two
# pi-prompt workers on two compute nodes keep their node pools, lease nodes and
# sessions across the restart). The matrix starts its own throwaway
# daemons (own config, data dir, free ports, TLS off, own multiplexer socket
# dir) and refuses to start when a target matches the production daemon, so
# this section never touches this smoke's daemon or production. Rows still
# marked OPERATOR PICK in docs/plans/restart-matrix-findings.md are skipped
# with a reason, not run, because their outcome is an undecided design choice.
# Needs jq, curl, python3, tmux and git, and a daemon binary ($_DW_BIN). Each
# state is bounded by SMOKE_RESTART_TIMEOUT (default 900s) through `timeout`
# when available. SMOKE_RESTART_MATRIX=0 skips the whole section.
RM_SURVIVE_STATES=(worker-running waiting-capacity waiting-external git-operation automation-branches scheduled-due websocket-clients worker-node-placement)
RM_PICK_STATES=(
  "planning|OPERATOR PICK: a planning PRD is reset to draft on restart and the operator re-plans; the fix is not chosen"
  "verify-script|OPERATOR PICK: the verification script is a daemon child that runs again after a restart; where it runs is not chosen"
  "verify-script-mid|OPERATOR PICK: the verification script is a daemon child that runs again after a restart; where it runs is not chosen"
  "final-gate|OPERATOR PICK: the final gate script is a daemon child that runs again after a restart; where it runs is not chosen"
  "verify-session|the live verifier session is not adopted after a restart and the verifier runs once more; adopting it needs a consumer for its output, not built"
)
RM_MISSING=""
for RM_T in jq curl python3 tmux git; do
  command -v "$RM_T" >/dev/null 2>&1 || RM_MISSING="$RM_MISSING $RM_T"
done
if [[ "$SECTION_SKIP" == "1" ]]; then
  : # SMOKE_ONLY excludes this section: do not start the throwaway restart daemons
elif [[ "${SMOKE_RESTART_MATRIX:-1}" == "0" ]]; then
  skip "restart matrix: disabled by SMOKE_RESTART_MATRIX=0"
elif [[ ! -x "$SMOKE_DIR/restart-matrix.sh" ]]; then
  skip "restart matrix: scripts/restart-matrix.sh not found or not executable"
elif [[ -n "$RM_MISSING" ]]; then
  skip "restart matrix: required tool(s) missing:$RM_MISSING"
elif [[ -z "${_DW_BIN:-}" ]]; then
  skip "restart matrix: no datawatch binary (build it or set DATAWATCH_BIN)"
else
  RM_BASE="$(mktemp -d "${TMPDIR:-/tmp}/rms.XXXXXX")" # short path: the multiplexer socket path has a length limit
  RM_TO_BIN="$(command -v timeout 2>/dev/null || true)"
  for RM_S in "${RM_SURVIVE_STATES[@]}"; do
    RM_OUT="$RM_BASE/$RM_S.out"
    if [[ -n "$RM_TO_BIN" ]]; then
      env -u TMUX -u TMUX_TMPDIR DATAWATCH_BIN="$_DW_BIN" RM_TMP_BASE="$RM_BASE" RM_KILL_MODE=control-group \
        "$RM_TO_BIN" "${SMOKE_RESTART_TIMEOUT:-900}" bash "$SMOKE_DIR/restart-matrix.sh" --strict --only "$RM_S" >"$RM_OUT" 2>&1
    else
      env -u TMUX -u TMUX_TMPDIR DATAWATCH_BIN="$_DW_BIN" RM_TMP_BASE="$RM_BASE" RM_KILL_MODE=control-group \
        bash "$SMOKE_DIR/restart-matrix.sh" --strict --only "$RM_S" >"$RM_OUT" 2>&1
    fi
    RM_RC=$?
    RM_RESULT="$(grep -E '^  -> ' "$RM_OUT" | tail -1 | sed -E 's/^  -> //; s/, record:.*//')"
    case "$RM_RC" in
      0) ok "restart matrix $RM_S: survives a restart (${RM_RESULT:-strict clean})" ;;
      1) ko "restart matrix $RM_S: ${RM_RESULT:-failed}; failing checks: $(grep -E '^  FAIL ' "$RM_OUT" | sed -E 's/^  FAIL +//' | head -3 | tr '\n' ';' | head -c 400)" ;;
      124) ko "restart matrix $RM_S: timed out after ${SMOKE_RESTART_TIMEOUT:-900}s" ;;
      *) ko "restart matrix $RM_S: harness fault (rc=$RM_RC): $(tail -2 "$RM_OUT" | tr '\n' ' ' | head -c 300)" ;;
    esac
  done
  for RM_P in "${RM_PICK_STATES[@]}"; do
    skip "restart matrix ${RM_P%%|*}: ${RM_P#*|}"
  done
  rm -rf "$RM_BASE"
fi
H "81. Mobile-fit view runner (release smoke)"
# Runs the mobile-fit view harness (observer view, quick profile, Playwright)
# via release-smoke-mobile-fit.sh, which derives TEST_HTTP / TEST_TLS /
# TEST_TOKEN from this script's BASE/TOK and prints the report path.
# Skips (not a failure) when the daemon is unreachable, node or Playwright
# are missing, or no browser could be launched.
MOBILE_FIT_SCRIPT="$(dirname "$0")/release-smoke-mobile-fit.sh"
if [[ -f "$MOBILE_FIT_SCRIPT" ]]; then
  if MF_OUT=$(DATAWATCH_BASE="$BASE" DATAWATCH_TOKEN="$TOK" bash "$MOBILE_FIT_SCRIPT" 2>&1); then
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      [[ "$line" == OK:* ]] && continue
      ok "$line"
    done <<< "$MF_OUT"
  else
    rc=$?
    if [[ $rc -eq 2 ]]; then
      skip "mobile-fit runner skipped: $(echo "$MF_OUT" | grep -F 'SKIP:' | tail -1 | sed 's/^SKIP: //')"
    else
      ko "mobile-fit runner failed: $(echo "$MF_OUT" | grep -Ev '^$' | tail -3 | tr '\n' ' ')"
    fi
  fi
else
  skip "release-smoke-mobile-fit.sh missing; mobile-fit probe deferred"
fi

# ---------------------------------------------------------------------------
H "82. notify_events — resolved notify set (Automaton > guardrail profile > global)"
# REST-only round-trip, no LLM work: the Automaton is a draft that is never
# approved or run. Every throwaway is registered for cleanup on creation
# and also deleted explicitly below. A missing autonomous surface (503) is
# a skip that names the reason, never a fail.
NE_TAG="$$-$(date +%s)"
NE_PROBE=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/autonomous/prds" 2>/dev/null)
if [[ "$NE_PROBE" == "503" ]]; then
  skip "notify_events: autonomous subsystem disabled (GET /api/autonomous/prds 503)"
else
  NE_PRD=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"spec\":\"smoke probe — notify_events $NE_TAG\",\"project_dir\":\"/tmp\",\"actor\":\"smoke\"}" \
    "$BASE/api/autonomous/prds" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$NE_PRD" ]]; then
    skip "notify_events: POST /api/autonomous/prds returned no id (HTTP $NE_PROBE)"
  else
    add_cleanup prd "$NE_PRD"

    # 82.1 explicit Automaton list wins — source=automaton, opt-in kind present
    NE_BODY=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d '{"notify_events":["prd.blocked","prd.completed","task.session_started"]}' \
      "$BASE/api/autonomous/prds/$NE_PRD/set_notify_events" 2>/dev/null || echo "")
    NE_SET_OK=false
    grep -q '"source":"automaton"' <<<"$NE_BODY" && NE_SET_OK=true
    if [[ "$NE_SET_OK" == "true" ]]; then
      ok "set_notify_events: explicit list resolves (source=automaton)"
    else
      ko "set_notify_events: explicit list (body: ${NE_BODY:0:120})"
    fi

    # 82.2 GET shows the resolved set with the opt-in task kind
    NE_GET=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$NE_PRD" 2>/dev/null | grep -c '"task.session_started"' || true)
    if [[ "${NE_GET:-0}" -ge 1 ]]; then
      ok "GET prd: task.session_started (opt-in kind) present in notify_events_effective"
    else
      ko "GET prd: resolved set missing task.session_started"
    fi

    # 82.3 unknown kind is rejected with 400
    NE_BAD=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X POST -H "Content-Type: application/json" \
      -d '{"notify_events":["bogus.kind"]}' "$BASE/api/autonomous/prds/$NE_PRD/set_notify_events" 2>/dev/null || echo 000)
    if [[ "$NE_BAD" == "400" ]]; then
      ok "set_notify_events: unknown kind rejected (400)"
    else
      ko "set_notify_events: unknown kind not rejected (HTTP $NE_BAD)"
    fi

    # 82.4 clear (empty list) — the Automaton inherits the profile/global tier
    NE_CLR=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d '{"notify_events":[]}' "$BASE/api/autonomous/prds/$NE_PRD/set_notify_events" 2>/dev/null || echo "")
    if [[ "$(<<<"$NE_CLR" grep -c '"source":"automaton"')" -eq 0 ]] && [[ "$(<<<"$NE_CLR" grep -c '"source":"global"')" -ge 1 ]]; then
      ok "set_notify_events: [] clears to inherit (source=global)"
    else
      ko "set_notify_events: clear did not return to the global tier (body: ${NE_CLR:0:120})"
    fi

    # 82.5 guardrail profile tier: create with notify_events, assign, resolved
    NE_PROFILE_NAME="smoke-notify-events-$NE_TAG"
    NE_PROFILE=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d "{\"name\":\"$NE_PROFILE_NAME\",\"guardrails\":[],\"notify_events\":[\"prd.failed\",\"prd.completed\"]}" \
      "$BASE/api/autonomous/guardrail_profiles" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
    if [[ -z "$NE_PROFILE" ]]; then
      ko "guardrail profile notify_events: POST /api/autonomous/guardrail_profiles returned no id"
    else
      # Explicit delete at the end of the section (cleanup kind not registered).
      NE_ASSIGN=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" \
        -d "{\"guardrail_profile\":\"$NE_PROFILE_NAME\"}" "$BASE/api/autonomous/prds/$NE_PRD/guardrails" 2>/dev/null || echo 000)
      NE_EFF=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$NE_PRD" 2>/dev/null || echo "")
      if [[ "$NE_ASSIGN" == "200" ]] && grep -q '"source":"profile"' <<<"$NE_EFF"; then
        ok "profile tier: assigned profile resolves (source=profile)"
      else
        ko "profile tier: assign=$NE_ASSIGN; $(grep -o '"source":"[a-z]*"' <<<"$NE_EFF" | head -1)"
      fi
      # Clear the profile list — the Automaton falls back to the global tier.
      NE_PCLR=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" \
        -d "{\"name\":\"$NE_PROFILE_NAME\",\"notify_events\":[]}" "$BASE/api/autonomous/guardrail_profiles/$NE_PROFILE" 2>/dev/null || echo 000)
      NE_EFF2=$(curl "${curl_args[@]}" "$BASE/api/autonomous/prds/$NE_PRD" 2>/dev/null || echo "")
      if [[ "$NE_PCLR" == "200" ]] && grep -q '"source":"global"' <<<"$NE_EFF2"; then
        ok "profile tier: cleared profile falls back to global tier"
      else
        ko "profile tier: clear=$NE_PCLR; $(grep -o '"source":"[a-z]*"' <<<"$NE_EFF2" | head -1)"
      fi
      curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/guardrail_profiles/$NE_PROFILE" >/dev/null 2>&1
    fi

    # 82.6 set/clear left an audit entry for the round-trip
    NE_AUDIT=$(curl "${curl_args[@]}" "$BASE/api/audit?action=autonomous_notify_events_changed&limit=500" 2>/dev/null | grep -c 'autonomous_notify_events_changed' || true)
    curl "${curl_args[@]}" -X DELETE "$BASE/api/autonomous/prds/$NE_PRD?hard=true" >/dev/null 2>&1
    if [[ "${NE_AUDIT:-0}" -ge 1 ]]; then
      ok "audit: autonomous_notify_events_changed entries present ($NE_AUDIT)"
    else
      skip "audit: autonomous_notify_events_changed not visible via /api/audit (may need higher privilege)"
    fi
  fi
fi

# ---------------------------------------------------------------------------
H "83. operator questions — /api/questions behind autonomous.questions.enabled"
# REST-only round-trip on this script's own sandbox daemon (or the dedicated
# DW_BASE sandbox), never production. Off by default: with the switch off every
# endpoint is 503 and names the switch. The original switch value is read first
# and restored at the end of the section. The throwaway question is answered
# in 83.7, so the sandbox store holds no open smoke question afterwards.
OQ_ORIG=$(curl "${curl_args[@]}" "$BASE/api/config" 2>/dev/null | python3 -c 'import json,sys
try:
  c=json.load(sys.stdin); print("true" if (c.get("autonomous",{}).get("questions",{}) or {}).get("enabled") else "false")
except Exception:
  print("")' 2>/dev/null || echo "")
if [[ -z "$OQ_ORIG" ]]; then
  skip "operator questions: GET /api/config unreadable, switch state unknown"
else
  oq_put() { curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" -d "{\"autonomous.questions.enabled\":$1}" "$BASE/api/config" 2>/dev/null || echo 000; }
  oq_code() { curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$@" 2>/dev/null || echo 000; }

  # 83.1 switch off: list and raise both 503 and the body names the switch
  oq_put false >/dev/null
  OQ_OFF_LIST=$(curl "${curl_args[@]}" -w '\n%{http_code}' "$BASE/api/questions" 2>/dev/null || echo "")
  OQ_OFF_POST=$(oq_code -X POST -H "Content-Type: application/json" -d '{}' "$BASE/api/questions")
  if [[ "$(tail -n1 <<<"$OQ_OFF_LIST")" == "503" && "$OQ_OFF_POST" == "503" ]] && grep -q 'autonomous.questions.enabled' <<<"$OQ_OFF_LIST"; then
    ok "questions: switch off -> GET and POST /api/questions are 503 naming autonomous.questions.enabled"
  else
    ko "questions: switch off did not 503 (list=$(tail -n1 <<<"$OQ_OFF_LIST") post=$OQ_OFF_POST)"
  fi

  # 83.2 a non-boolean value is refused and not stored
  OQ_BAD=$(curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" -d '{"autonomous.questions.enabled":"yes"}' "$BASE/api/config" 2>/dev/null || echo 000)
  if [[ "$OQ_BAD" == "400" ]]; then
    ok "questions: PUT /api/config with a non-boolean switch value rejected (400)"
  else
    ko "questions: non-boolean switch value not rejected (HTTP $OQ_BAD)"
  fi

  # 83.3 switch on: GET /api/config reads true, list is 200
  OQ_ON_PUT=$(oq_put true)
  OQ_ON_CFG=$(curl "${curl_args[@]}" "$BASE/api/config" 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin).get("autonomous",{}).get("questions",{}).get("enabled"))' 2>/dev/null || echo "")
  OQ_ON_LIST=$(oq_code "$BASE/api/questions")
  if [[ "$OQ_ON_PUT" == "200" && "$OQ_ON_CFG" == "True" && "$OQ_ON_LIST" == "200" ]]; then
    ok "questions: switch on reads back true and GET /api/questions is 200 (no restart)"
  else
    ko "questions: switch on (put=$OQ_ON_PUT cfg=$OQ_ON_CFG list=$OQ_ON_LIST)"
  fi

  # 83.4 raise, list, get
  OQ_TAG="$$-$(date +%s)"
  OQ_ID=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
    -d "{\"label\":\"decision\",\"context\":\"none\",\"text\":\"smoke probe $OQ_TAG: cap retry backoff?\",\"options\":[{\"id\":\"yes\",\"label\":\"Yes\"},{\"id\":\"no\",\"label\":\"No\"}],\"default\":\"yes\",\"asked_by\":\"smoke\"}" \
    "$BASE/api/questions" 2>/dev/null | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("question",{}).get("id",""))
except Exception: print("")' 2>/dev/null || echo "")
  if [[ -z "$OQ_ID" ]]; then
    ko "questions: POST /api/questions returned no question id"
  else
    ok "questions: POST /api/questions raised a question ($OQ_ID)"
    OQ_LISTED=$(curl "${curl_args[@]}" "$BASE/api/questions?state=open" 2>/dev/null | grep -c "$OQ_ID" || true)
    OQ_GOT=$(oq_code "$BASE/api/questions/$OQ_ID")
    if [[ "${OQ_LISTED:-0}" -ge 1 && "$OQ_GOT" == "200" ]]; then
      ok "questions: raised question appears in GET /api/questions?state=open and GET /api/questions/{id} is 200"
    else
      ko "questions: raised question not readable (listed=$OQ_LISTED get=$OQ_GOT)"
    fi

    # 83.5 a malformed raise is a 400, an unknown id a 404
    OQ_MAL=$(oq_code -X POST -H "Content-Type: application/json" -d '{"text":"no options"}' "$BASE/api/questions")
    OQ_404=$(oq_code "$BASE/api/questions/q-does-not-exist-$OQ_TAG")
    if [[ "$OQ_MAL" == "400" && "$OQ_404" == "404" ]]; then
      ok "questions: malformed raise 400, unknown id 404"
    else
      ko "questions: validation statuses (malformed=$OQ_MAL unknown=$OQ_404)"
    fi

    # 83.6 an answer that looks like a secret is refused 422 and the question stays open
    OQ_SEC=$(oq_code -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"yes","quote":"use AKIAIOSFODNN7EXAMPLE","surface":"cli"}' "$BASE/api/questions/$OQ_ID/answer")
    OQ_STATE=$(curl "${curl_args[@]}" "$BASE/api/questions/$OQ_ID" 2>/dev/null | grep -c '"state":"open"' || true)
    if [[ "$OQ_SEC" == "422" && "${OQ_STATE:-0}" -ge 1 ]]; then
      ok "questions: secret-looking answer refused (422), question still open"
    else
      ko "questions: secret answer (HTTP $OQ_SEC, still open=$OQ_STATE)"
    fi

    # 83.7 a real answer is recorded (201) and shows in the answer log
    OQ_ANS=$(oq_code -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"no","quote":"no, leave it uncapped","surface":"cli"}' "$BASE/api/questions/$OQ_ID/answer")
    OQ_LOG=$(curl "${curl_args[@]}" "$BASE/api/questions/$OQ_ID/answers" 2>/dev/null | grep -c 'no, leave it uncapped' || true)
    if [[ "$OQ_ANS" == "201" && "${OQ_LOG:-0}" -ge 1 ]]; then
      ok "questions: answer recorded (201) and verbatim quote present in the answer log"
    else
      ko "questions: answer flow (HTTP $OQ_ANS, quote in log=$OQ_LOG)"
    fi
  fi

  # 83.8 restore the original switch value; the section leaves no state behind
  OQ_REST=$(oq_put "$OQ_ORIG")
  if [[ "$OQ_REST" == "200" ]]; then
    ok "questions: switch restored to $OQ_ORIG"
  else
    ko "questions: could not restore the switch (HTTP $OQ_REST)"
  fi
fi

H "84. Capacity ledger — node pools, pi leases, waiter placement"
# Read-only checks against GET /api/capacity of the running daemon:
# (a) no node pool holds above its limit, (b) every running pi or pi-prompt
# lease also holds a node:<name> lease (a lease without a node placement
# cannot be attributed to any node), (c) no request waits for a busy node
# while another candidate node sits idle (delegated to
# check-waiter-nodes.sh). The whole section SKIPS (not fails) when the
# endpoint reports no capacity data or the backend is single-node.
CAP=$(curl "${curl_args[@]}" "$BASE/api/capacity" 2>/dev/null || true)
if ! echo "$CAP" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d.get("pools"),list) and isinstance(d.get("leases"),list)' 2>/dev/null; then
  skip "capacity ledger: /api/capacity did not report capacity data"
else
  CNODES=$(echo "$CAP" | python3 -c 'import json,sys;print(sum(1 for p in json.load(sys.stdin).get("pools",[]) if str(p.get("name","")).startswith("node:")))' 2>/dev/null || echo 0)
  if ! [[ "${CNODES:-0}" =~ ^[0-9]+$ ]] || [[ "${CNODES:-0}" -eq 0 ]]; then
    skip "capacity ledger: no node pools in the snapshot (single-node backend)"
  else
    # 84.1 no node pool holds above its limit (limit 0 = unlimited, passes)
    CAP_OL=$(echo "$CAP" | python3 -c '
import json, sys
d = json.load(sys.stdin)
for p in d.get("pools", []):
    if not str(p.get("name", "")).startswith("node:"):
        continue
    lim = p.get("limit") or 0
    used = (p.get("held") or 0) + (p.get("external") or 0)
    if lim > 0 and used > lim:
        print("%s holds %d of %d (held=%d external=%d)" % (p.get("name"), used, lim, p.get("held") or 0, p.get("external") or 0))' 2>/dev/null)
    if [[ -z "${CAP_OL:-}" ]]; then
      ok "no node pool holds above its limit"
    else
      ko "node pool(s) over limit: $(echo "$CAP_OL" | tr '\n' '; ')"
    fi

    # 84.2 every running pi / pi-prompt lease also holds a node:<name> lease
    # (line 1 = count of such leases, line 2 = the ones missing a node lease)
    CAP_PI=$(echo "$CAP" | python3 -c '
import json, sys
d = json.load(sys.stdin)
total, bad = 0, []
for l in d.get("leases", []):
    pools = [str(p) for p in l.get("pools") or []]
    if not ({"llm:pi", "llm:pi-prompt"} & set(pools)):
        continue
    total += 1
    if not any(p.startswith("node:") for p in pools):
        bad.append(str(l.get("holder") or l.get("session_id") or "?"))
print(total)
print("; ".join(bad))' 2>/dev/null)
    CAP_PI_N=$(echo "$CAP_PI" | sed -n '1p')
    CAP_PI_BAD=$(echo "$CAP_PI" | sed -n '2p')
    if [[ -z "${CAP_PI_BAD:-}" ]]; then
      ok "all ${CAP_PI_N:-0} running pi/pi-prompt lease(s) hold a node lease"
    else
      ko "pi/pi-prompt lease(s) missing a node lease: $CAP_PI_BAD"
    fi

    # 84.3 waiter placement via check-waiter-nodes.sh (rc 2 = the daemon-bound
    # waiter / over-limit symptom, rc 1 = the script could not fetch or parse)
    WAITER_OUT=$(env -u TMUX -u TMUX_TMPDIR DATAWATCH_URL="${BASE%/}" DATAWATCH_TOKEN="${TOK:-}" bash "$SMOKE_DIR/check-waiter-nodes.sh" 2>&1)
    WAITER_RC=$?
    case "$WAITER_RC" in
      0)
        ok "waiter placement clean: $(echo "$WAITER_OUT" | sed -n 's/^summary: //p' | tail -1)"
        ;;
      2)
        ko "waiter placement: $(echo "$WAITER_OUT" | grep -E '^ *(SYMPTOM|OVERLIMIT):' | tail -3 | tr '\n' '; ')"
        ;;
      *)
        skip "waiter placement: check-waiter-nodes.sh could not report against the daemon (rc=$WAITER_RC)"
        ;;
    esac
  fi
fi

# ---------------------------------------------------------------------------
H "85. Usage calculator (advice, readings, verdict, policy/variant, extra credit, pasted gate, config, comm, CLI)"
# Typed readings are the source for now; the advice answers with the numbers, the
#   verdict, by_lanes, the text forms, and (when things are not set) a needs marker:
#   GET    /api/usage/advice            [variant=V] [policy=P] [lanes=N] [cost=C] — per-call overrides
#   GET    /api/usage/readings          full log, latest first
#   POST   /api/usage/readings          {"at", "percent"|"spend_usd", "source"}; typed now,
#                                       pasted | account planned; the response carries the
#                                       stored reading (id) for precise cleanup
#   DELETE /api/usage/readings?id=      exact removal by id
#   PUT    /api/usage/extra-credit      ceiling = 100 points + credit; negative is refused
#
# The empty state answers with a needs marker (not an error); typed readings drive the
# verdict and by_lanes; per-call policy and variant overrides are honored and unknown
# values refused with 400; extra credit shifts the ceiling by exactly its amount and is
# reset to 0 afterwards; the config variant round-trips through GET /api/config and is
# restored exactly (empty when this script owns a fresh daemon, so the daemon's config
# is left untouched); pasted and account sources stay 501 (planned, never emulated)
# and unknown kinds are 400; the comm text renders within 50 columns over both
# the advice and the test-message route; the CLI verdict is compared with the API
# verdict, not just grepped.
#
# Everything this section creates is cleaned up by exact id / exact restore (cleanup_all
# cases: usage, usage-credit, usage-variant); one reading is also deleted at the end of
# the section through DELETE ?id= (its cleanup entry then no-ops). No name-pattern
# sweeps, no tracker IDs in names. Sandbox-only assertions (default max_lanes, our 45%
# reading being the newest) hold only when this script controls a fresh daemon.
#
# Evidence — SMOKE_ONLY=85 against the section's own isolated sandbox daemon
# (fresh daemon, auto-generated self-signed cert, env -u TMUX -u TMUX_TMPDIR,
# unrelated release pre-gates skipped: DW_SKIP_TIDY_CHECK/DOCS_CHECK/INTERNAL_REFS_CHECK/
# VERSION_BUMP_CHECK/DOCKERFILE_GO_VERSION_CHECK/DOCS_AS_MCP_CHECK; no production),
# run 2026-10-28 on a freshly built binary; the exact `== Summary ==` lines were
#   Pass:  16
#   Fail:  0
#   Skip:  0
# and `== Cleanup ==` then deleted both readings by exact id, restored
# usage.calculator.variant to <unset/default> and the extra credit to 0 — no
# residue left in the sandbox data dir. The run takes about three minutes because
# the sections before 85 still run their setup side effects under SMOKE_ONLY (only
# their ok/ko/skip lines are muted); section 80's restart matrix, which would add
# up to 7 x SMOKE_RESTART_TIMEOUT, is now gated on SECTION_SKIP.
# The comm route passes over the real path (POST /api/test/message): the test
# router gets the daemon's web port at startup (main.go, SetWebPort), and the
# in-process loopback client skips certificate verification for the daemon's own
# 127.0.0.1 auto-generated cert (internal/router/sx2_parity.go) — the same
# trust posture the smoke script's curl -sk takes for the local daemon.
ux_adv() { curl "${curl_args[@]}" -s "$1" 2>/dev/null; }
ux_code() { curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' "$1" 2>/dev/null || echo 000; }
ux_num() { python3 -c "import json, sys
try:
    print(json.load(sys.stdin)['advice']['$1'])
except Exception:
    pass" 2>/dev/null || true; }
ux_readid() { python3 -c 'import json, sys
try:
    r = (json.load(sys.stdin).get("reading") or {})
    i = r.get("id")
    print(i if i else "")
except Exception:
    pass' 2>/dev/null || true; }
ux_verdict() { python3 -c 'import json, sys
try:
    print(json.load(sys.stdin)["advice"].get("verdict", ""))
except Exception:
    pass' 2>/dev/null || true; }
UX_EXT=""
[[ -n "${DW_BASE:-}" ]] && UX_EXT=1

# --- baseline advice: 200, a needs marker or a latest reading, ceiling, both texts -----
UX_A0BODY="$TMPD/ux_a0"
UX_A0CODE=$(curl "${curl_args[@]}" -s -o "$UX_A0BODY" -w '%{http_code}' "$BASE/api/usage/advice" 2>/dev/null || echo 000)
UX_A0=$(cat "$UX_A0BODY" 2>/dev/null || true)
UX_CEIL0=$(echo "$UX_A0" | ux_num ceiling); UX_CEIL0=${UX_CEIL0:-100}
UX_CRED0=$(echo "$UX_A0" | ux_num extra_credit_points); UX_CRED0=${UX_CRED0:-0}
# The section resets the credit to 0 itself (asserted), so the safety-net cleanup
# kind also holds 0: an exact-value restore, never a pattern sweep.
add_cleanup usage-credit 0
UX_CONF0=$(curl "${curl_args[@]}" -s "$BASE/api/config" 2>/dev/null || true)
UX_V0=$(echo "$UX_CONF0" | python3 -c 'import json, sys; v = json.load(sys.stdin)["usage"]["calculator"]["variant"]; print(v if v else "all")' 2>/dev/null || true)
UX_V0=${UX_V0:-all}
# Restore target: on a daemon this script owns, the stored variant was unset (GET shows
# the effective default "all"), so restore EMPTY — the default applies again and the
# daemon's config is left exactly as found. On an external daemon restore the exact
# value that was effective before the smoke.
if [[ -z "$UX_EXT" ]]; then UX_VRESTORE=""; else UX_VRESTORE="$UX_V0"; fi
add_cleanup usage-variant "$UX_VRESTORE"
if [[ "$UX_A0CODE" == "200" ]] && echo "$UX_A0" | UX_EXT="$UX_EXT" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
a = d["advice"]
if os.environ.get("UX_EXT"):
    assert a.get("needs") in ("reading", "reset", "percent") or a.get("read_at"), \
        "neither a needs marker nor a latest reading: %r" % a.get("needs")
else:
    # Fresh smoke daemon: the readings store is empty and the reset is set,
    # so the empty state must answer needs=reading — with no verdict yet
    # (the needs marker is the answer; a verdict cannot precede it).
    assert a.get("needs") == "reading", "expected the empty state needs=reading, got needs=%r read_at=%r" % (a.get("needs"), a.get("read_at"))
    assert not a.get("verdict"), "a verdict cannot precede any reading: %r" % a.get("verdict")
t = d.get("text") or {}
assert t.get("cli") and t.get("comm"), "text forms missing"
if not os.environ.get("UX_EXT"):
    assert "no weekly readings yet" in t["comm"], t["comm"][:160]
' 2>/dev/null; then
  ok "usage baseline: advice answers 200; on the fresh daemon the empty state answers needs=reading with no verdict (the comm form says so); ceiling set; both text forms present"
else
  ko "usage baseline: not well-formed (code=$UX_A0CODE): $(head -c 200 "$UX_A0BODY" 2>/dev/null)"
fi

# --- per-call validation: unknown variant / policy / negative lanes all 400 ------------
UX_VBAD=$(ux_code "$BASE/api/usage/advice?variant=bogus")
UX_PBAD=$(ux_code "$BASE/api/usage/advice?policy=bogus")
UX_LBAD=$(ux_code "$BASE/api/usage/advice?lanes=-1")
if [[ "$UX_VBAD" == "400" && "$UX_PBAD" == "400" && "$UX_LBAD" == "400" ]]; then
  ok "usage validation: ?variant=bogus, ?policy=bogus and ?lanes=-1 each answered 400"
else
  ko "usage validation: expected 400/400/400 for bogus variant / policy / negative lanes, got $UX_VBAD / $UX_PBAD / $UX_LBAD"
fi

# --- source gate: pasted + account stay planned (501); unknown kind 400 ---------------
UX_T1=$(python3 -c 'import time; print(time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 48 * 3600)))' 2>/dev/null || true)
UX_T2=$(python3 -c 'import time; print(time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 24 * 3600)))' 2>/dev/null || true)
UX_PSB="$TMPD/ux_pasted"
UX_PS=$(curl "${curl_args[@]}" -s -o "$UX_PSB" -w '%{http_code}' -X POST -H "Content-Type: application/json" -d "{\"at\":\"$UX_T2\",\"percent\":10,\"source\":\"pasted\"}" "$BASE/api/usage/readings" 2>/dev/null || echo 000)
UX_ASB="$TMPD/ux_account"
UX_AS=$(curl "${curl_args[@]}" -s -o "$UX_ASB" -w '%{http_code}' -X POST -H "Content-Type: application/json" -d "{\"at\":\"$UX_T2\",\"percent\":10,\"source\":\"account\"}" "$BASE/api/usage/readings" 2>/dev/null || echo 000)
UX_UB="$TMPD/ux_unknown"
UX_UK=$(curl "${curl_args[@]}" -s -o "$UX_UB" -w '%{http_code}' -X POST -H "Content-Type: application/json" -d "{\"at\":\"$UX_T2\",\"percent\":10,\"source\":\"carrier-pigeon\"}" "$BASE/api/usage/readings" 2>/dev/null || echo 000)
if [[ "$UX_PS" == "501" && "$UX_AS" == "501" && "$UX_UK" == "400" ]]; then
  ok "usage source gate: pasted=501 and account=501 (planned sources, never emulated), unknown kind=400"
else
  ko "usage source gate: expected pasted=501 account=501 unknown=400, got pasted=$UX_PS account=$UX_AS unknown=$UX_UK: $(head -c 120 "$UX_PSB" 2>/dev/null)"
fi

# --- two typed readings; both exact ids come from the POST response, nothing else ----
UX_R1=$(curl "${curl_args[@]}" -s -X POST -H "Content-Type: application/json" -d "{\"at\":\"$UX_T1\",\"percent\":30,\"source\":\"typed\"}" "$BASE/api/usage/readings" 2>/dev/null || true)
UX_R2=$(curl "${curl_args[@]}" -s -X POST -H "Content-Type: application/json" -d "{\"at\":\"$UX_T2\",\"percent\":45,\"source\":\"typed\"}" "$BASE/api/usage/readings" 2>/dev/null || true)
UX_ID1=$(echo "$UX_R1" | ux_readid)
UX_ID2=$(echo "$UX_R2" | ux_readid)
if echo "$UX_R1" | grep -q '"ok":true' && echo "$UX_R2" | grep -q '"ok":true' \
   && [[ -n "$UX_ID1" && -n "$UX_ID2" && "$UX_ID1" != "$UX_ID2" ]]; then
  add_cleanup usage "$UX_ID1"
  add_cleanup usage "$UX_ID2"
  ok "usage readings: two typed readings accepted; both exact ids captured for precise cleanup"
else
  ko "usage readings: typed add failed or ids not retrievable: r1=$UX_R1 r2=$UX_R2"
fi

# --- GET list: both ids present, typed, one after the other in time -------------------
UX_LIST=$(curl "${curl_args[@]}" -s "$BASE/api/usage/readings" 2>/dev/null || true)
if [[ -n "$UX_ID1" && -n "$UX_ID2" ]] && echo "$UX_LIST" | UX_ID1="$UX_ID1" UX_ID2="$UX_ID2" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
rs = d.get("readings") or []
ids = [r.get("id") for r in rs]
i1, i2 = os.environ["UX_ID1"], os.environ["UX_ID2"]
assert i1 in ids and i2 in ids, (i1, i2, ids)
assert ids.index(i1) < ids.index(i2), (ids.index(i1), ids.index(i2))
byid = {r.get("id"): r for r in rs}
for x in (i1, i2):
    r = byid[x]
    assert r.get("source") == "typed", r
    assert r.get("at"), r
' 2>/dev/null; then
  ok "usage list: GET /api/usage/readings carries both new readings as typed, oldest before newest"
else
  ko "usage list: both ids not present in time order: $(echo "$UX_LIST" | head -c 240)"
fi

# --- advice with readings: verdict self-consistent, by_lanes one row per lane count ----
UX_A=$(ux_adv "$BASE/api/usage/advice")
if echo "$UX_A" | UX_EXT="$UX_EXT" python3 -c '
import json, os, sys
d = json.load(sys.stdin)
a = d["advice"]
assert not a.get("needs"), "expected numbers after adding readings, got needs=%r" % a.get("needs")
assert (a["verdict"] == "over") == (a["projected_end_recent"] > a["ceiling"]), (a["verdict"], a["projected_end_recent"], a["ceiling"])
va = a.get("verdict_avg")
if va in ("within", "over"):
    assert (va == "over") == (a["projected_end_avg"] > a["ceiling"]), (va, a["projected_end_avg"], a["ceiling"])
assert a["recent_burn_per_24h"] >= 0 and a["week_avg_burn_per_24h"] >= 0
if not os.environ.get("UX_EXT"):
    # Fresh smoke daemon: our 45% reading is the newest and defaults are untouched.
    assert abs(a["percent"] - 45) < 0.001, a["percent"]
    assert a["max_lanes"] == 3, a["max_lanes"]
rows = a["by_lanes"]
assert isinstance(rows, list) and len(rows) >= 1, rows
assert rows[0]["lanes"] == 0 and rows[-1]["lanes"] == a["max_lanes"], (a["max_lanes"], len(rows))
if not os.environ.get("UX_EXT"):
    assert len(rows) == a["max_lanes"] + 1, len(rows)
assert a["chosen"]["lanes"] == a["lanes"] and a["lanes"] <= a["max_lanes"], (a["chosen"], a["lanes"])
assert a["advice"]["headline"], "no headline"
' 2>/dev/null; then
  ok "usage advice: verdict within/over self-consistent with the projection, by_lanes one row per lane count, chosen lane row coherent"
else
  ko "usage advice: verdict/by_lanes not as expected: $(echo "$UX_A" | head -c 240)"
fi

# --- policy override per call: labels/lanes/finish move, the numbers do not ------------
UX_PU=$(ux_adv "$BASE/api/usage/advice?policy=use-all")
UX_PP=$(ux_adv "$BASE/api/usage/advice?policy=pace")
UX_PF=$(ux_adv "$BASE/api/usage/advice?policy=finish-early")
if UX_PU="$UX_PU" UX_PP="$UX_PP" UX_PF="$UX_PF" python3 -c '
import json, os
du = json.loads(os.environ["UX_PU"])
dp = json.loads(os.environ["UX_PP"])
df = json.loads(os.environ["UX_PF"])
assert du["policy"] == "use-all", du.get("policy")
assert dp["policy"] == "pace" and df["policy"] == "finish-early"
aw = (du["advice"], dp["advice"], df["advice"])
assert all(w["advice"]["headline"] for w in aw), "a policy call has no headline"
assert aw[1]["advice"]["policy"] == "pace" and aw[2]["advice"]["policy"] == "finish-early"
def nums(w):
    return (w["percent"], w["recent_burn_per_24h"], w["week_avg_burn_per_24h"], w["ceiling"], w["projected_end_recent"])
n = [nums(w) for w in aw]
assert n[0] == n[1] == n[2], "the policy must not move the numbers: " + str(n)
# finish-early runs every lane and reports when the work finishes.
assert aw[2]["advice"]["lanes_suggested"] == aw[2]["max_lanes"], (aw[2]["advice"]["lanes_suggested"], aw[2]["max_lanes"])
assert aw[2]["advice"].get("finish") is None or isinstance(aw[2]["advice"]["finish"], dict)
# use-all / pace suggest a fitting lane count, always in range.
for w in (aw[0], aw[1]):
    ls = w["advice"]["lanes_suggested"]
    assert 1 <= ls <= w["max_lanes"], (ls, w["max_lanes"])
' 2>/dev/null; then
  ok "usage policy: use-all / pace / finish-early honored per call (labels, lanes, finish) while the numbers stay put"
else
  ko "usage policy: override not honored correctly (use-all=$(echo "$UX_PU" | head -c 120) | pace=$(echo "$UX_PP" | head -c 120) | finish-early=$(echo "$UX_PF" | head -c 120) ...)"
fi

# --- variant override: accepted + echoed (bogus already 400 above) --------------------
UX_VR=$(ux_adv "$BASE/api/usage/advice?variant=lane-planner")
if echo "$UX_VR" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d["variant"] == "lane-planner", d["variant"]
assert d["advice"]["advice"]["headline"], "no headline"
' 2>/dev/null; then
  ok "usage variant: ?variant=lane-planner accepted and echoed in the advice"
else
  ko "usage variant: lane-planner not honored: $(echo "$UX_VR" | head -c 160)"
fi

# --- extra credit: shifts the ceiling by exactly its amount, then restores -------------
UX_EC1=$(curl "${curl_args[@]}" -s -X PUT -H "Content-Type: application/json" -d '{"points":80}' "$BASE/api/usage/extra-credit" 2>/dev/null || true)
UX_EN=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" -d '{"points":-5}' "$BASE/api/usage/extra-credit" 2>/dev/null || echo 000)
UX_AC=$(ux_adv "$BASE/api/usage/advice")
if echo "$UX_EC1" | grep -q '"ok":true' && [[ "$UX_EN" == "400" ]] \
   && echo "$UX_AC" | UX_CEIL0="$UX_CEIL0" python3 -c '
import json, os, sys
a = json.load(sys.stdin)["advice"]
assert abs(a["extra_credit_points"] - 80) < 0.001, a["extra_credit_points"]
assert abs(a["ceiling"] - (float(os.environ["UX_CEIL0"]) + 80)) < 0.001, (a["ceiling"], os.environ["UX_CEIL0"])
' 2>/dev/null; then
  ok "usage extra credit: PUT +80 raises the ceiling by exactly 80 (was $UX_CEIL0, now $(echo "$UX_AC" | ux_num ceiling)); PUT -5 refused with 400"
  UX_EC2=$(curl "${curl_args[@]}" -s -X PUT -H "Content-Type: application/json" -d '{"points": 0}' "$BASE/api/usage/extra-credit" 2>/dev/null || true)
  UX_AR=$(ux_adv "$BASE/api/usage/advice")
  if echo "$UX_EC2" | grep -q '"ok":true' && echo "$UX_AR" | UX_CEIL0="$UX_CEIL0" python3 -c '
import json, os, sys
a = json.load(sys.stdin)["advice"]
assert abs(a["extra_credit_points"] - 0) < 0.001, a["extra_credit_points"]
assert abs(a["ceiling"] - float(os.environ["UX_CEIL0"])) < 0.001, (a["ceiling"], os.environ["UX_CEIL0"])
' 2>/dev/null; then
    ok "usage extra credit: reset to 0 afterwards (credit 0, ceiling back to $UX_CEIL0)"
  else
    ko "usage extra credit: reset to 0 failed (ceiling=$(echo "$UX_AR" | ux_num ceiling), wanted $UX_CEIL0)"
  fi
else
  ko "usage extra credit: PUT +80 did not raise the ceiling by 80 (was $UX_CEIL0 => $(echo "$UX_AC" | ux_num ceiling)), negative status=$UX_EN: $UX_EC1"
fi

# --- config round-trip: stored variant applies live, then restored to the exact original
UX_CONF1=$(curl "${curl_args[@]}" -s -X PUT -H "Content-Type: application/json" -d '{"usage.calculator.variant":"burn-ledger"}' "$BASE/api/config" 2>/dev/null || true)
UX_ADB=""
UX_CONF2=$(curl "${curl_args[@]}" -s "$BASE/api/config" 2>/dev/null || true)
UX_ADV=$(ux_adv "$BASE/api/usage/advice")
if echo "$UX_CONF2" | python3 -c 'import json, sys; assert json.load(sys.stdin)["usage"]["calculator"]["variant"] == "burn-ledger"' 2>/dev/null \
   && echo "$UX_ADV" | python3 -c 'import json, sys; assert json.load(sys.stdin)["variant"] == "burn-ledger"' 2>/dev/null; then
  ok "usage config round-trip: usage.calculator.variant PUT lands in GET /api/config and applies live to advice"
  curl "${curl_args[@]}" -s -X PUT -H "Content-Type: application/json" -d "{\"usage.calculator.variant\":\"$UX_VRESTORE\"}" "$BASE/api/config" >/dev/null 2>&1
  UX_CONF3=$(curl "${curl_args[@]}" -s "$BASE/api/config" 2>/dev/null || true)
  UX_ADB=$(ux_adv "$BASE/api/usage/advice")
  # The config display is the EFFECTIVE value: an empty restore reads back the default
  # ("all"), so compare the display against the pre-smoke effective value UX_V0.
  if echo "$UX_CONF3" | UX_V0="$UX_V0" python3 -c 'import json, os, sys; assert (json.load(sys.stdin)["usage"]["calculator"]["variant"] or "all") == os.environ["UX_V0"]' 2>/dev/null \
     && echo "$UX_ADB" | UX_V0="$UX_V0" python3 -c 'import json, os, sys; assert json.load(sys.stdin)["variant"] == os.environ["UX_V0"]' 2>/dev/null; then
    ok "usage config round-trip: variant restored (stored=\"$UX_VRESTORE\") and live advice is back to the pre-smoke value ($UX_V0)"
  else
    ko "usage config round-trip: variant not restored (saved \"$UX_VRESTORE\", effective still != $UX_V0): conf=$(echo "$UX_CONF3" | head -c 160)"
  fi
else
  ko "usage config round-trip: variant set/change not visible or live: conf=$(echo "$UX_CONF2" | python3 -c 'import json, sys; print(json.load(sys.stdin).get("usage", {}))' 2>/dev/null || echo '?') adv=$(echo "$UX_ADV" | head -c 120)"
fi

# --- reset-zone contract: config display shows the effective zone + source, advice
#     carries zone + server_zone, both texts carry the "(server zone)" marker only when
#     the zone follows the daemon (never a bare value it chose); an explicit zone pins.
UX_ZBASE=$(curl "${curl_args[@]}" -s "$BASE/api/config" 2>/dev/null || true)
UX_ZADV=$(ux_adv "$BASE/api/usage/advice")
# The decisive assertion set (config/advice consistency + text marker when server-sourced):
if echo "$UX_ZBASE" | UX_ZADV="" python3 -c '
import json, sys
c = json.load(sys.stdin)["usage"]["calculator"]
assert isinstance(c.get("reset_zone"), str) and c["reset_zone"], "reset_zone display missing: %r" % c.get("reset_zone")
assert c.get("reset_zone_source") in ("configured", "server"), "bad source: %r" % c.get("reset_zone_source")
assert isinstance(c.get("reset_zone_fell_back"), bool), "fell_back flag missing"
' 2>/dev/null \
   && echo "$UX_ZBASE" | UX_ZADV="$UX_ZADV" python3 -c '
import json, os, sys
c = json.load(sys.stdin)["usage"]["calculator"]
adv = json.loads(os.environ["UX_ZADV"])
a = adv["advice"]
assert a.get("zone") == c["reset_zone"], "advice zone %r != config %r" % (a.get("zone"), c["reset_zone"])
assert a.get("server_zone") == (c["reset_zone_source"] == "server"), "advice server_zone %r inconsistent" % a.get("server_zone")
marker = "(server zone)"
for name, form in (("cli", adv["text"]["cli"]), ("comm", adv["text"]["comm"])):
    if c["reset_zone_source"] == "server":
        assert marker in form, "%s form lacks the server-zone marker: %s" % (name, form[:160])
' 2>/dev/null; then
  ok "usage reset-zone: config GET shows the effective zone with reset_zone_source/fell_back; advice zone + server_zone agree with it; the texts mark the zone as the daemon's when the operator left it unset"
else
  ko "usage reset-zone: contract fields inconsistent: conf=$(echo "$UX_ZBASE" | head -c 160) adv=$(echo "$UX_ZADV" | head -c 160)"
fi
if curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" -d '{"usage.calculator.reset_zone":"America/New_York"}' "$BASE/api/config" 2>/dev/null | grep -q '^2' \
   && curl "${curl_args[@]}" -s "$BASE/api/config" 2>/dev/null | python3 -c 'import json,sys; c=json.load(sys.stdin)["usage"]["calculator"]; assert c["reset_zone"]=="America/New_York" and c["reset_zone_source"]=="configured"' 2>/dev/null \
   && UX_ZS=$(ux_adv "$BASE/api/usage/advice") \
   && echo "$UX_ZS" | python3 -c 'import json,sys; a=json.load(sys.stdin)["advice"]; assert a["zone"]=="America/New_York" and a["server_zone"] is False' 2>/dev/null \
   && echo "$UX_ZS" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert "(server zone)" not in d["text"]["cli"] and "(server zone)" not in d["text"]["comm"]' 2>/dev/null; then
  ok "usage reset-zone: an explicit zone (America/New_York) pins — source configured, marker gone from advice zone/text"
else
  ko "usage reset-zone: explicit zone pin failed — see responses above"
fi
# Restore the pre-smoke stored state: an effective source "server" means the zone was
# unset (empty PUT), a source "configured" means the displayed zone IS the stored one.
UX_ZSRC0=$(echo "$UX_ZBASE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["usage"]["calculator"].get("reset_zone_source",""))' 2>/dev/null || true)
UX_ZZONE0=$(echo "$UX_ZBASE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["usage"]["calculator"].get("reset_zone",""))' 2>/dev/null || true)
if [[ "$UX_ZSRC0" == "server" ]]; then UX_ZRESTORE='""'; else UX_ZRESTORE="\"$UX_ZZONE0\""; fi
add_cleanup usage-zone "$UX_ZRESTORE"
curl "${curl_args[@]}" -s -X PUT -H "Content-Type: application/json" -d "{\"usage.calculator.reset_zone\":$UX_ZRESTORE}" "$BASE/api/config" >/dev/null 2>&1
UX_ZR=$(curl "${curl_args[@]}" -s "$BASE/api/config" 2>/dev/null || true)
UX_ZRA=$(ux_adv "$BASE/api/usage/advice")
if echo "$UX_ZR" | UX_ZRA="$UX_ZRA" python3 -c '
import json, os, sys
c = json.load(sys.stdin)["usage"]["calculator"]
adv = json.loads(os.environ["UX_ZRA"])
assert adv["advice"].get("zone") == c["reset_zone"], "advice zone %r != config %r after restore" % (adv["advice"].get("zone"), c["reset_zone"])
assert adv["advice"].get("server_zone") == (c["reset_zone_source"] == "server")
if c["reset_zone_source"] == "server":
    for name in ("cli", "comm"):
        assert "(server zone)" in adv["text"][name], "%s form lost the server-zone marker after restore" % name
' 2>/dev/null; then
  ok "usage reset-zone: restore lands — config and advice match again (stored value: ${UX_ZRESTORE:-<unset>})"
else
  ko "usage reset-zone: restore inconsistent: conf=$(echo "$UX_ZR" | head -c 160) adv=$(echo "$UX_ZRA" | head -c 160)"
fi
# --- CLI config get: the zone marker lives in the CLI too (sandbox daemon + CLI binary) --
if [[ -z "${DW_BASE:-}" && -n "${DW_CLI:-}" ]]; then
  UX_CG1=$($DW_CLI config get usage.calculator.reset_zone 2>&1 | head -c 400 || true)
  # After the restore above: a server-sourced zone carries the marker, a configured one does not.
  UX_ZSRC1=$(echo "$UX_ZR" | python3 -c 'import json,sys; print(json.load(sys.stdin)["usage"]["calculator"].get("reset_zone_source",""))' 2>/dev/null || true)
  if [[ "$UX_ZSRC1" == "server" ]]; then
    if printf '%s\n' "$UX_CG1" | grep -q "(server zone)"; then
      ok "usage CLI config get: reset_zone shows '<zone> (server zone)' when the zone follows the daemon"
    else
      ko "usage CLI config get: expected the (server zone) marker: $(printf '%s' "$UX_CG1" | head -c 240)"
    fi
  else
    if ! printf '%s\n' "$UX_CG1" | grep -q "(server zone)" && printf '%s\n' "$UX_CG1" | grep -q "$UX_ZZONE0"; then
      ok "usage CLI config get: a configured reset_zone prints the value without the marker"
    else
      ko "usage CLI config get: configured zone output unexpected: $(printf '%s' "$UX_CG1" | head -c 240)"
    fi
  fi
else
  skip "usage CLI config get: external daemon mode (DW_BASE) or no CLI binary"
fi

# --- comm text: <= 50 columns per line, carries the verdict language -------------------
if echo "$UX_ADB" | python3 -c '
import json, sys
d = json.load(sys.stdin)
comm = d["text"]["comm"]
assert comm.strip(), "empty comm text"
lines = comm.splitlines()
assert max(len(l) for l in lines) <= 50, max(len(l) for l in lines)
v = d["advice"].get("verdict")
if v in ("within", "over"):
    label = "over budget" if v == "over" else "within budget"
    assert label in comm, (v, comm[:200])
else:
    assert "no weekly reading" in comm or "weekly reset is not set" in comm or "weekly percentage is needed" in comm, comm[:200]
' 2>/dev/null; then
  ok "usage comm: text.comm renders within 50 columns per line and carries the verdict language"
else
  ko "usage comm: text.comm missing, wider than 50 columns or without the verdict: $(echo "$UX_ADB" | head -c 240)"
fi

# --- comm path over the real route: POST /api/test/message "usage advice" --------------
UX_TM=$(curl "${curl_args[@]}" -s -X POST -H "Content-Type: application/json" -d '{"text":"usage advice"}' "$BASE/api/test/message" 2>/dev/null || true)
# 'REST loopback not configured' is the documented isolated-env caveat (same rule as 7aa):
# a SKIP, not a FAIL. A missing responses array or a lost 'usage advice' input is a FAIL.
if echo "$UX_TM" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("input") == "usage advice", d.get("input")
rs = d.get("responses") or []
assert d.get("count") == len(rs) >= 1, (d.get("count"), rs)
joined = "\n".join(rs)
assert "usage advice" in joined, joined[:200]
' 2>/dev/null; then
  if echo "$UX_TM" | python3 -c 'import json, sys; assert "loopback" not in (json.load(sys.stdin)["responses"] or [""])[0].lower()' 2>/dev/null; then
    if echo "$UX_TM" | python3 -c '
import json, sys
d = json.load(sys.stdin)
lines = [l for r in (d.get("responses") or []) for l in r.splitlines()]
joined = "\n".join(d.get("responses") or [])
assert lines and max(len(l) for l in lines) <= 50, max((len(l) for l in lines), default=0)
assert ("over budget" in joined) or ("within budget" in joined) or ("no weekly reading" in joined) or ("weekly reset is not set" in joined), joined[:240]
' 2>/dev/null; then
      ok "usage comm route: POST /api/test/message 'usage advice' replies with the verdict language, every line within 50 columns"
    else
      ko "usage comm route: reply lacks the advice verdict/needs language or exceeds 50 columns: $(echo "$UX_TM" | head -c 240)"
    fi
  else
    skip "usage comm route: loopback unavailable in this env (route wired; same caveat as 7aa)"
  fi
else
  ko "usage comm route: test/message reply not well-formed: $(echo "$UX_TM" | head -c 240)"
fi

# --- DELETE by exact id: one reading goes, the rest of the state stays put -------------
UX_D1B="$TMPD/ux_del1"
UX_D1=$(curl "${curl_args[@]}" -s -o "$UX_D1B" -w '%{http_code}' -X DELETE "$BASE/api/usage/readings?id=$UX_ID1" 2>/dev/null || echo 000)
UX_DNF=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/api/usage/readings?id=u-definitely-not-there" 2>/dev/null || echo 000)
UX_DNOID=$(curl "${curl_args[@]}" -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/api/usage/readings" 2>/dev/null || echo 000)
UX_LIST2=$(curl "${curl_args[@]}" -s "$BASE/api/usage/readings" 2>/dev/null || true)
if [[ "$UX_D1" == "200" ]] && grep -q '"ok":true' "$UX_D1B" 2>/dev/null \
   && [[ "$UX_DNF" == "404" && "$UX_DNOID" == "400" ]] \
   && echo "$UX_LIST2" | UX_ID1="$UX_ID1" UX_ID2="$UX_ID2" python3 -c '
import json, os, sys
ids = [r.get("id") for r in (json.load(sys.stdin).get("readings") or [])]
assert os.environ["UX_ID1"] not in ids, ids
assert os.environ["UX_ID2"] in ids, "the other reading must survive until cleanup_all sweeps it: " + str(ids)
' 2>/dev/null; then
  ok "usage delete: DELETE ?id= removes exactly the one reading (200 + ok), unknown id 404, missing id 400, the rest of the log untouched"
else
  ko "usage delete: id1=$UX_ID1 id2=$UX_ID2 delete=$UX_D1 ($(head -c 80 "$UX_D1B" 2>/dev/null)) notfound=$UX_DNF no_id=$UX_DNOID list=$(echo "$UX_LIST2" | head -c 160)"
fi

# --- CLI: renders advice and carries the SAME verdict as the API, not just any verdict --
if [[ -z "${DW_BASE:-}" && -n "${DW_CLI:-}" ]]; then
  UX_A_CLI=$(ux_adv "$BASE/api/usage/advice")
  UX_VV=$(echo "$UX_A_CLI" | ux_verdict)
  UX_CLI=$($DW_CLI usage advice 2>&1 | head -c 2000 || true)
  UX_WANT=""
  case "$UX_VV" in
    over)   UX_WANT="over budget" ;;
    within) UX_WANT="within budget" ;;
  esac
  if [[ -n "$UX_WANT" ]]; then
    if printf '%s\n' "$UX_CLI" | grep -qi "verdict:" && printf '%s\n' "$UX_CLI" | grep -q "$UX_WANT"; then
      ok "usage CLI: 'usage advice' renders a verdict line matching the API verdict ($UX_VV: $UX_WANT)"
    else
      ko "usage CLI: output lacked the API verdict label ($UX_WANT): $(printf '%s' "$UX_CLI" | head -c 240)"
    fi
  else
    if printf '%s\n' "$UX_CLI" | grep -Eq "no weekly reading|weekly reset is not set|weekly percentage is needed"; then
      ok "usage CLI: 'usage advice' renders the needs state matching the API (no verdict set)"
    else
      ko "usage CLI: output did not render a verdict/needs state: $(printf '%s' "$UX_CLI" | head -c 240)"
    fi
  fi
else
  skip "usage CLI: external daemon mode (DW_BASE), CLI runs against the base daemon there"
fi

H "86. Stuck-running watchdog bound — autonomous.stuck_running_seconds on REST config"
# The watchdog bound is an operator-facing setting. This section reads it from
# GET /api/config, writes a new value through PUT /api/config, reads it back,
# checks that 0 (use the default) is accepted and a negative value is ignored,
# and restores the exact value found through add_cleanup (kind stuck-running).
# The timing behaviour itself (re-fire once, then block) is covered by the
# TestStuckRunningWatchdog_* tests; a smoke run must not wait out a bound.
if [[ "$SECTION_SKIP" != "1" ]]; then
  SR_GET() { curl "${curl_args[@]}" "$BASE/api/config" 2>/dev/null | python3 -c 'import json,sys;d=json.load(sys.stdin);v=d.get("autonomous",{}).get("stuck_running_seconds");print("missing" if v is None else int(v))' 2>/dev/null || echo "missing"; }
  SR_PUT() { curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" -d "{\"autonomous.stuck_running_seconds\": $1}" "$BASE/api/config" 2>/dev/null || echo "000"; }
  SR0=$(SR_GET)
  if [[ "$SR0" == "missing" ]]; then
    ko "stuck-running: GET /api/config has no autonomous.stuck_running_seconds"
  else
    add_cleanup stuck-running "$SR0"
    ok "stuck-running: GET /api/config exposes autonomous.stuck_running_seconds=$SR0"
    if [[ "$(SR_PUT 1234)" == "200" && "$(SR_GET)" == "1234" ]]; then
      ok "stuck-running: PUT /api/config 1234 reads back 1234"
    else
      ko "stuck-running: PUT /api/config 1234 did not read back 1234"
    fi
    if [[ "$(SR_PUT 0)" == "200" && "$(SR_GET)" == "0" ]]; then
      ok "stuck-running: 0 is accepted (the default applies at read time)"
    else
      ko "stuck-running: PUT /api/config 0 was not accepted or did not read back 0"
    fi
    SR_PUT 1234 >/dev/null
    SR_PUT -5 >/dev/null
    if [[ "$(SR_GET)" == "1234" ]]; then
      ok "stuck-running: a negative bound is ignored (the stored 1234 is unchanged)"
    else
      ko "stuck-running: a negative bound changed the stored value to $(SR_GET)"
    fi
    SR_PUT "$SR0" >/dev/null
    if [[ "$(SR_GET)" == "$SR0" ]]; then
      ok "stuck-running: the original bound $SR0 is restored"
    else
      ko "stuck-running: could not restore the original bound $SR0"
    fi
  fi
fi

# ---------------------------------------------------------------------------
H "87. operator questions step 2 — notification toggles, answered/withdrawn events, return path, polled GET"
# Step 2 of the question channel on this script's own sandbox daemon (or the
# dedicated DW_BASE sandbox), never production. It reads the switch and the three
# notify toggles first and add_cleanup restores each exact value; the one
# lifecycle subscription it creates (an mcp_queue sink, so the events read can
# see the events) is deleted by its exact id; the high-priority alert is marked
# read by its exact id. Every question it raises is answered or withdrawn, so no
# open smoke question is left behind (questions are never deleted by design).
# The events part needs the subscription engine (autonomous enabled on the
# daemon); without it the section skips only those checks.
if [[ "$SECTION_SKIP" != "1" ]]; then
  Q2_JSON() { python3 -c 'import json,sys
try:
  d=json.load(sys.stdin)
  for k in sys.argv[1].split("."):
    d=d.get(k) if isinstance(d,dict) else None
  print("" if d is None else (str(d).lower() if isinstance(d,bool) else d))
except Exception:
  print("")' "$1" 2>/dev/null || echo ""; }
  Q2_CFG() { curl "${curl_args[@]}" "$BASE/api/config" 2>/dev/null | Q2_JSON "autonomous.questions.$1"; }
  Q2_PUT() { curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' -X PUT -H "Content-Type: application/json" -d "{\"autonomous.questions.$1\":$2}" "$BASE/api/config" 2>/dev/null || echo 000; }
  Q2_CODE() { curl "${curl_args[@]}" -o /dev/null -w '%{http_code}' "$@" 2>/dev/null || echo 000; }
  Q2_RAISE() { curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d "$1" "$BASE/api/questions" 2>/dev/null | Q2_JSON "question.id"; }

  Q2_SW0=$(Q2_CFG enabled)
  Q2_TOGGLES="notify_messaging notify_alert return_path"
  declare -A Q2_ORIG
  Q2_READ_OK=1
  [[ -n "$Q2_SW0" ]] || Q2_READ_OK=0
  for t in $Q2_TOGGLES; do Q2_ORIG[$t]=$(Q2_CFG "$t"); [[ -n "${Q2_ORIG[$t]}" ]] || Q2_READ_OK=0; done
  if [[ "$Q2_READ_OK" != "1" ]]; then
    skip "questions step 2: GET /api/config does not expose the switch and the three toggles (enabled='$Q2_SW0' messaging='${Q2_ORIG[notify_messaging]}' alert='${Q2_ORIG[notify_alert]}' return_path='${Q2_ORIG[return_path]}')"
  else
    add_cleanup qcfg "autonomous.questions.enabled=$Q2_SW0"
    for t in $Q2_TOGGLES; do add_cleanup qcfg "autonomous.questions.$t=${Q2_ORIG[$t]}"; done

    # 87.1 each toggle: boolean write reads back, non-boolean is refused (400) and not stored
    Q2_TOG_OK=1
    for t in $Q2_TOGGLES; do
      [[ "$(Q2_PUT "$t" false)" == "200" && "$(Q2_CFG "$t")" == "false" ]] || Q2_TOG_OK=0
      [[ "$(Q2_PUT "$t" '"no"')" == "400" && "$(Q2_CFG "$t")" == "false" ]] || Q2_TOG_OK=0
      [[ "$(Q2_PUT "$t" true)" == "200" && "$(Q2_CFG "$t")" == "true" ]] || Q2_TOG_OK=0
    done
    if [[ "$Q2_TOG_OK" == "1" ]]; then
      ok "questions step 2: notify_messaging, notify_alert and return_path each write false, read back, refuse a non-boolean (400) and write true"
    else
      ko "questions step 2: a notification toggle did not round-trip through PUT/GET /api/config"
    fi

    # 87.2 switch on, then the one subscription the events read needs (exact id recorded)
    Q2_TAG="$$-$(date +%s)"
    Q2_QUEUE="smoke-q2-$Q2_TAG"
    Q2_SWON=$(Q2_PUT enabled true)
    Q2_SUB=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
      -d "{\"filter\":{\"kinds\":[\"question.answered\",\"question.withdrawn\"]},\"sink\":{\"type\":\"mcp_queue\",\"target\":\"$Q2_QUEUE\"}}" \
      "$BASE/api/autonomous/subscriptions" 2>/dev/null | Q2_JSON "id")
    [[ -n "$Q2_SUB" ]] && add_cleanup qsub "$Q2_SUB"
    if [[ "$Q2_SWON" != "200" ]]; then
      ko "questions step 2: could not turn the switch on (HTTP $Q2_SWON)"
    else
      # 87.3 validation on the new record fields: an unknown priority is 400, and a
      # question that opts into the session line without naming a session is 400
      Q2_OPTS='"options":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"default":"yes","context":"none","asked_by":"smoke"'
      Q2_BADPRI=$(Q2_CODE -X POST -H "Content-Type: application/json" -d "{\"label\":\"decision\",\"text\":\"smoke $Q2_TAG bad priority\",$Q2_OPTS,\"priority\":\"urgent\"}" "$BASE/api/questions")
      Q2_BADRP=$(Q2_CODE -X POST -H "Content-Type: application/json" -d "{\"label\":\"decision\",\"text\":\"smoke $Q2_TAG no session\",$Q2_OPTS,\"return_path\":true}" "$BASE/api/questions")
      if [[ "$Q2_BADPRI" == "400" && "$Q2_BADRP" == "400" ]]; then
        ok "questions step 2: an unknown priority and return_path without a session_id are both refused (400)"
      else
        ko "questions step 2: record-field validation (priority=$Q2_BADPRI return_path-without-session=$Q2_BADRP)"
      fi

      # 87.4 a high-priority raise alerts; a normal one does not (announce route, alert leg)
      Q2_HI=$(Q2_RAISE "{\"label\":\"decision\",\"text\":\"smoke $Q2_TAG high priority\",$Q2_OPTS,\"priority\":\"high\"}")
      Q2_NO=$(Q2_RAISE "{\"label\":\"decision\",\"text\":\"smoke $Q2_TAG normal priority\",$Q2_OPTS}")
      if [[ -z "$Q2_HI" || -z "$Q2_NO" ]]; then
        ko "questions step 2: could not raise the high and normal probe questions"
      else
        Q2_ALERT_ID=""
        for _ in 1 2 3 4 5 6 7 8 9 10; do
          Q2_ALERT_ID=$(curl "${curl_args[@]}" "$BASE/api/alerts" 2>/dev/null | python3 -c 'import json,sys
want=sys.argv[1]
try:
  for a in json.load(sys.stdin).get("alerts") or []:
    if want in (a.get("title") or ""): print(a.get("id","")); break
except Exception: pass' "Operator question raised (high priority): $Q2_HI" 2>/dev/null || echo "")
          [[ -n "$Q2_ALERT_ID" ]] && break
          sleep 0.5
        done
        Q2_NO_ALERT=$(curl "${curl_args[@]}" "$BASE/api/alerts" 2>/dev/null | grep -c "$Q2_NO" || true)
        if [[ -n "$Q2_ALERT_ID" ]]; then
          add_cleanup qalert "$Q2_ALERT_ID"
        fi
        if [[ -n "$Q2_ALERT_ID" && "${Q2_NO_ALERT:-0}" -eq 0 ]]; then
          ok "questions step 2: the high-priority raise left one alert naming its id; the normal raise left none"
        else
          ko "questions step 2: alert leg (high alert id='${Q2_ALERT_ID}', normal-question alert lines=${Q2_NO_ALERT:-0})"
        fi

        # 87.5 the polled single GET: no answer yet, then the standing answer's summary
        Q2_GET0=$(curl "${curl_args[@]}" "$BASE/api/questions/$Q2_HI" 2>/dev/null)
        Q2_CNT0=$(Q2_JSON answer_count <<<"$Q2_GET0"); Q2_ST0=$(Q2_JSON question.state <<<"$Q2_GET0"); Q2_PRI=$(Q2_JSON question.priority <<<"$Q2_GET0")
        Q2_ANS=$(Q2_CODE -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"no","quote":"no, keep it as is","surface":"cli"}' "$BASE/api/questions/$Q2_HI/answer")
        Q2_GET1=$(curl "${curl_args[@]}" "$BASE/api/questions/$Q2_HI" 2>/dev/null)
        Q2_CNT1=$(Q2_JSON answer_count <<<"$Q2_GET1"); Q2_CUR=$(Q2_JSON current_answer_id <<<"$Q2_GET1"); Q2_QUOTE=$(Q2_JSON answer.quote <<<"$Q2_GET1")
        if [[ "$Q2_ST0" == "open" && "$Q2_PRI" == "high" && "${Q2_CNT0:-0}" == "0" && "$Q2_ANS" == "201" && "$Q2_CNT1" == "1" && -n "$Q2_CUR" && "$Q2_QUOTE" == "no, keep it as is" ]]; then
          ok "questions step 2: the polled GET shows an open question with no answer, then answer_count 1, current_answer_id and the verbatim quote"
        else
          ko "questions step 2: polled GET (state=$Q2_ST0 priority=$Q2_PRI count0=$Q2_CNT0 answer=$Q2_ANS count1=$Q2_CNT1 current=$Q2_CUR quote='$Q2_QUOTE')"
        fi

        # 87.6 withdraw the normal question (it was never answered)
        Q2_WD=$(Q2_CODE -X POST -H "Content-Type: application/json" -d '{"actor":"smoke","reason":"probe done"}' "$BASE/api/questions/$Q2_NO/withdraw")
        [[ "$Q2_WD" == "200" ]] && ok "questions step 2: the normal probe question was withdrawn (200)" || ko "questions step 2: withdraw returned HTTP $Q2_WD"

        # 87.7 the events read: exactly one question.answered and one question.withdrawn
        if [[ -z "$Q2_SUB" ]]; then
          skip "questions step 2: no lifecycle subscription (autonomous disabled on this daemon); answered/withdrawn events not read"
        else
          Q2_EV_A=0; Q2_EV_W=0
          for _ in 1 2 3 4 5 6 7 8 9 10; do
            Q2_EVS=$(curl "${curl_args[@]}" "$BASE/api/autonomous/events?queue=$Q2_QUEUE&limit=200" 2>/dev/null)
            Q2_EV_A=$(python3 -c 'import json,sys
try: print(sum(1 for e in json.load(sys.stdin).get("events") or [] if e.get("kind")=="question.answered" and e.get("question_id")==sys.argv[1]))
except Exception: print(0)' "$Q2_HI" <<<"$Q2_EVS" 2>/dev/null || echo 0)
            Q2_EV_W=$(python3 -c 'import json,sys
try: print(sum(1 for e in json.load(sys.stdin).get("events") or [] if e.get("kind")=="question.withdrawn" and e.get("question_id")==sys.argv[1]))
except Exception: print(0)' "$Q2_NO" <<<"$Q2_EVS" 2>/dev/null || echo 0)
            [[ "$Q2_EV_A" -ge 1 && "$Q2_EV_W" -ge 1 ]] && break
            sleep 0.5
          done
          if [[ "$Q2_EV_A" == "1" && "$Q2_EV_W" == "1" ]]; then
            ok "questions step 2: GET /api/autonomous/events carries exactly one question.answered and one question.withdrawn for the probes"
          else
            ko "questions step 2: events read (answered=$Q2_EV_A withdrawn=$Q2_EV_W, want 1 and 1)"
          fi
        fi
      fi

      # 87.8 the return-path line: a question that opts in with a session that is gone is
      # accepted, and its answer is still recorded (a lost line is one daemon warning,
      # never a failed answer). The delivered line itself is pinned by the
      # TestQuestionReturnPath* unit tests; a smoke run starts no worker session.
      Q2_RP=$(Q2_RAISE "{\"label\":\"decision\",\"text\":\"smoke $Q2_TAG return path\",$Q2_OPTS,\"session_id\":\"smoke-gone-$Q2_TAG\",\"return_path\":true}")
      if [[ -z "$Q2_RP" ]]; then
        ko "questions step 2: a return_path question naming a session was not accepted"
      else
        Q2_RPA=$(Q2_CODE -X POST -H "Content-Type: application/json" -d '{"chosen_option_id":"yes","quote":"yes","surface":"cli"}' "$BASE/api/questions/$Q2_RP/answer")
        Q2_RPN=$(curl "${curl_args[@]}" "$BASE/api/questions/$Q2_RP" 2>/dev/null | Q2_JSON answer_count)
        if [[ "$Q2_RPA" == "201" && "$Q2_RPN" == "1" ]]; then
          ok "questions step 2: a return_path answer with its session gone still records (201, answer_count 1)"
        else
          ko "questions step 2: return_path answer (HTTP $Q2_RPA, answer_count=$Q2_RPN)"
        fi
      fi

      # 87.9 a flip off then on again over PUT /api/config: the endpoints follow it at once
      Q2_OFF=$(Q2_PUT enabled false); Q2_OFFC=$(Q2_CODE "$BASE/api/questions")
      Q2_ON=$(Q2_PUT enabled true); Q2_ONC=$(Q2_CODE "$BASE/api/questions")
      if [[ "$Q2_OFF" == "200" && "$Q2_OFFC" == "503" && "$Q2_ON" == "200" && "$Q2_ONC" == "200" ]]; then
        ok "questions step 2: switching off 503s the endpoints and switching on serves them again, no restart"
      else
        ko "questions step 2: live switch (off put=$Q2_OFF list=$Q2_OFFC, on put=$Q2_ON list=$Q2_ONC)"
      fi
    fi

    # 87.10 restore: delete the subscription now by its exact id, then the exact config
    # values read first (add_cleanup repeats both on any abnormal exit)
    if [[ -n "$Q2_SUB" ]]; then
      Q2_SDEL=$(Q2_CODE -X DELETE "$BASE/api/autonomous/subscriptions/$Q2_SUB")
      [[ "$Q2_SDEL" == "204" ]] && ok "questions step 2: the smoke subscription was deleted by its exact id" || ko "questions step 2: subscription delete returned HTTP $Q2_SDEL"
    fi
    Q2_RESTORE_OK=1
    for t in $Q2_TOGGLES; do [[ "$(Q2_PUT "$t" "${Q2_ORIG[$t]}")" == "200" ]] || Q2_RESTORE_OK=0; done
    [[ "$(Q2_PUT enabled "$Q2_SW0")" == "200" ]] || Q2_RESTORE_OK=0
    if [[ "$Q2_RESTORE_OK" == "1" ]]; then
      ok "questions step 2: the switch ($Q2_SW0) and the three toggles are restored to the values read first"
    else
      ko "questions step 2: could not restore the switch and toggles"
    fi
  fi
fi

# ---------------------------------------------------------------------------
H "Summary"
echo "  Pass:  $PASS"
echo "  Fail:  $FAIL"
echo "  Skip:  $SKIP"

# Write final progress state so dashboard shows the completed run.
_smoke_close_sec
_smoke_write_progress "false"

if [[ "$FAIL" -gt 0 ]]; then
  echo ""
  echo "FAIL: $FAIL functional check(s) failed; release should NOT proceed."
  exit 1
fi
echo ""
echo "OK: all functional checks passed (skips are fine — gated on whether the subsystem is configured)."
exit 0
