#!/usr/bin/env bash
# scripts/test-stories/lib.sh — shared helpers + env for E2E story scripts.
#
# Sourced by every TS-NNN.sh story file. Idempotent — safe to source multiple
# times within a single run. Translates the new runner's working-dir model
# (DATAWATCH_TEST_DIR, TEST_PORT, TEST_TLS_PORT, EVIDENCE_DIR) into the
# variables expected by the legacy story implementations
# (TEST_BASE, TEST_TLS, TEST_TOKEN, TEST_BINARY, RUN_DIR, etc.).
#
# Each story sets RESULT=pass|fail|skip and returns. The runner reads RESULT.

# guard against double-source
[[ -n "${_DW_LIB_LOADED:-}" ]] && return 0
_DW_LIB_LOADED=1

# The legacy runner used `set -uo pipefail` (no -e) so individual command
# failures inside a story did not abort the whole run. The new runner uses
# `set -euo pipefail`; relax it here so story bodies keep matching the old
# behaviour (they rely on `|| true`, conditionals, etc. for error handling).
set +e

# ---------------------------------------------------------------------------
# Repo + paths derived from the new runner
# ---------------------------------------------------------------------------
REPO_ROOT="${DATAWATCH_REPO_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
TEST_DIR="${DATAWATCH_TEST_DIR:-${TEST_DIR:-/tmp/dw-test-$$}}"
EVIDENCE_DIR="${EVIDENCE_DIR:-$TEST_DIR/evidence}"
RUN_DIR="${RUN_DIR:-$TEST_DIR}"
mkdir -p "$EVIDENCE_DIR" "$RUN_DIR" 2>/dev/null || true

# ---------------------------------------------------------------------------
# Daemon ports + endpoints
#   New runner provides TEST_PORT (HTTP) and TEST_TLS_PORT.
#   Legacy stories expect TEST_BASE / TEST_TLS / TEST_HTTP /
#   TEST_MCP_PORT / TEST_CHAN_PORT. Derive them, allowing env override.
# ---------------------------------------------------------------------------
TEST_PORT="${TEST_PORT:-18080}"
TEST_TLS_PORT="${TEST_TLS_PORT:-18443}"
TEST_MCP_PORT="${TEST_MCP_PORT:-$((TEST_PORT + 1))}"
TEST_CHAN_PORT="${TEST_CHAN_PORT:-$((TEST_TLS_PORT - 10))}"

TEST_BASE="${TEST_BASE:-http://127.0.0.1:$TEST_PORT}"
TEST_TLS="${TEST_TLS:-https://127.0.0.1:$TEST_TLS_PORT}"
TEST_HTTP="${TEST_HTTP:-http://127.0.0.1:$TEST_PORT}"

TEST_TOKEN="${TEST_TOKEN:-dw-test-token-12345}"
TEST_DATA="${TEST_DATA:-${DATAWATCH_TEST_DATA:-$TEST_DIR/.datawatch-test}}"
TEST_BINARY="${TEST_BINARY:-$REPO_ROOT/bin/datawatch}"
TEST_SIGNAL_GROUP="${TEST_SIGNAL_GROUP:-YOJtFDXm8WQCjna6dVGTOM8b4+aINRx4D4QgQ8Nmo54=}"
TEST_NTFY_TOPIC="${TEST_NTFY_TOPIC:-}"
TEST_WEBHOOK_PORT="${TEST_WEBHOOK_PORT:-19080}"

# Isolated Docker-sim ports (T13)
DOCKER_SIM_HTTP="${DOCKER_SIM_HTTP:-19180}"
DOCKER_SIM_TLS="${DOCKER_SIM_TLS:-19543}"
DOCKER_SIM_MCP="${DOCKER_SIM_MCP:-19281}"
DOCKER_SIM_CHAN="${DOCKER_SIM_CHAN:-19533}"
DOCKER_SIM_DATA="${DOCKER_SIM_DATA:-/tmp/dw-docker-sim-$$}"

# K8s
K8S_CONTEXT="${K8S_CONTEXT:-testing}"
K8S_NAMESPACE="${K8S_NAMESPACE:-datawatch-e2e}"
K8S_PF_PORT="${K8S_PF_PORT:-19443}"

# Legacy global state used by stories (preserved across sourced files).
: "${SESSION_ID:=}"; : "${AUTOMATON_ID:=}"; : "${PERSONA_ID:=}"
: "${MEM_ID:=}"; : "${KG_ID:=}"; : "${FILTER_ID:=}"; : "${SCHED_ID:=}"
: "${AGT_ID:=}"
: "${CURRENT_STORY:=}"
: "${DAEMON_VERSION:=}"

# Cleanup log (in-memory list of "kind id" lines) — written through the run.
CLEANUP_LOG="${CLEANUP_LOG:-$TEST_DIR/cleanup.log}"
: > "$CLEANUP_LOG" 2>/dev/null || CLEANUP_LOG=$(mktemp)

# ---------------------------------------------------------------------------
# curl args (auth header)
# ---------------------------------------------------------------------------
curl_args=(-sk --max-time 30 -H "Authorization: Bearer $TEST_TOKEN")

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
add_cleanup() { echo "$1 $2" >> "$CLEANUP_LOG"; }

# api <METHOD> <path> [body]
api() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl "${curl_args[@]}" -X "$method" -H "Content-Type: application/json" -d "$body" "$TEST_BASE$path"
  else
    curl "${curl_args[@]}" -X "$method" "$TEST_BASE$path"
  fi
}

# api_code — appends __HTTP_CODE_NNN__ marker
api_code() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl "${curl_args[@]}" -X "$method" -H "Content-Type: application/json" -d "$body" "$TEST_BASE$path" -w "\n__HTTP_CODE_%{http_code}__"
  else
    curl "${curl_args[@]}" -X "$method" "$TEST_BASE$path" -w "\n__HTTP_CODE_%{http_code}__"
  fi
}

# cli_test — invoke datawatch CLI against the isolated test daemon
cli_test() {
  "$TEST_BINARY" --config "$TEST_DATA/config.yaml" "$@"
}

# Evidence helpers — write to per-story directory under $EVIDENCE_DIR
save_evidence() {
  local story="$1" filename="$2" content="$3"
  local dir="$EVIDENCE_DIR/$story"
  mkdir -p "$dir"
  printf '%s' "$content" > "$dir/$filename"
}

save_evidence_file() {
  local story="$1" filename="$2" src="$3"
  local dir="$EVIDENCE_DIR/$story"
  mkdir -p "$dir"
  cp "$src" "$dir/$filename" 2>/dev/null || true
}

# assert_json <content> <python-expression>
assert_json() {
  local content="$1" expr="$2"
  echo "$content" | python3 -c "import json,sys; d=json.load(sys.stdin); assert $expr" 2>/dev/null
}

# semver_lt a b — returns 0 if a < b
semver_lt() {
  local a="$1" b="$2"
  a="${a#v}"; b="${b#v}"
  [[ "$(printf '%s\n%s\n' "$a" "$b" | sort -V | head -1)" == "$a" && "$a" != "$b" ]]
}

# get_daemon_version — cached from /api/health
get_daemon_version() {
  if [[ -z "$DAEMON_VERSION" ]]; then
    DAEMON_VERSION=$(api GET /api/health | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("version","0.0.0"))' 2>/dev/null || echo "0.0.0")
  fi
  echo "$DAEMON_VERSION"
}

# section — print a banner (used by sprint blocks)
section() { echo ""; echo "== $* =="; }

# ---------------------------------------------------------------------------
# Result setters — replace legacy ok()/ko()/skip() exit-style helpers.
# These set RESULT and return; the runner aggregates outcomes.
# ---------------------------------------------------------------------------
ok()   { local msg="$*"; [[ -n "$msg" ]] && echo "  PASS  [${CURRENT_STORY:-?}] $msg"; RESULT=pass; }
pass() { ok "$@"; }
ko()   { local msg="$*"; [[ -n "$msg" ]] && echo "  FAIL  [${CURRENT_STORY:-?}] $msg"; RESULT=fail; }
fail() { ko "$@"; }
skip() { local msg="$*"; [[ -n "$msg" ]] && echo "  SKIP  [${CURRENT_STORY:-?}] $msg"; RESULT=skip; }

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------
ensure_test_session() {
  if [[ -n "$SESSION_ID" ]]; then
    local chk
    chk=$(api GET "/api/sessions/$SESSION_ID" 2>/dev/null)
    if echo "$chk" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'id' in d" 2>/dev/null; then
      return 0
    fi
    SESSION_ID=""
  fi
  local resp
  resp=$(api POST /api/sessions/start '{"task":"test-fixture-session","name":"test-fixture-session","backend":"shell","project_dir":"/tmp","effort":"quick"}')
  SESSION_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$SESSION_ID" ]]; then
    add_cleanup sess "$SESSION_ID"
    echo "  [fixture] created test session: $SESSION_ID"
    return 0
  fi
  skip "could not create test session fixture: $(echo "$resp" | head -c 200)"
  return 1
}

ensure_test_automaton() {
  if [[ -n "$AUTOMATON_ID" ]]; then
    local chk
    chk=$(api GET "/api/autonomous/prds/$AUTOMATON_ID" 2>/dev/null)
    if echo "$chk" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'id' in d" 2>/dev/null; then
      return 0
    fi
    AUTOMATON_ID=""
  fi
  local a_enabled
  a_enabled=$(api GET /api/autonomous/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_enabled" != "yes" ]]; then
    skip "autonomous disabled — cannot create test automaton fixture"
    return 1
  fi
  local resp
  resp=$(api POST /api/autonomous/prds '{"spec":"test-prd-fixture: echo hello world","project_dir":"/tmp","backend":"claude-code","effort":"low"}')
  AUTOMATON_ID=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  if [[ -n "$AUTOMATON_ID" ]]; then
    add_cleanup automaton "$AUTOMATON_ID"
    echo "  [fixture] created test automaton: $AUTOMATON_ID"
    return 0
  fi
  skip "could not create test automaton fixture: $(echo "$resp" | head -c 200)"
  return 1
}

# ---------------------------------------------------------------------------
# Filter helpers
# Stories may consult these globals (exported by run-tests.sh):
#   FILTER_SURFACE, FILTER_FEATURE, FILTER_STORY, SKIP_CONFLICT
# Tags format: "surface:api feature:sessions conflict:llm blocking"
# Return 0 (true) when this story should run, 1 when it should be skipped.
# ---------------------------------------------------------------------------
story_matches_filter() {
  local tags="$1"
  if [[ -n "${FILTER_SURFACE:-}" ]]; then
    echo "$tags" | grep -q "surface:${FILTER_SURFACE}" || return 1
  fi
  if [[ -n "${FILTER_FEATURE:-}" ]]; then
    echo "$tags" | grep -q "feature:${FILTER_FEATURE}" || return 1
  fi
  if [[ -n "${SKIP_CONFLICT:-}" ]]; then
    echo "$tags" | grep -qE "conflict:(${SKIP_CONFLICT})" && return 1
  fi
  return 0
}

# preflight — call at top of each story; sets RESULT=skip + returns 1 when
# the current story is filtered out by surface/feature/conflict flags.
# Usage:
#   story_preflight "surface:api feature:sessions" || return 0
story_preflight() {
  local tags="$1"
  if ! story_matches_filter "$tags"; then
    RESULT=skip
    echo "  SKIP  [${CURRENT_STORY:-?}] (filtered out by surface/feature/conflict)"
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# PWA visual test helper (Playwright via Node.js)
# ---------------------------------------------------------------------------
# run_pwa_story <story_id>
#   Runs scripts/test-stories/pwa/<story_id>.mjs with system Chrome via
#   Playwright. Sets RESULT=pass|fail|skip (skip when Node is unavailable).
#   Evidence (screenshots, logs) written to $EVIDENCE_DIR/<story_id>/.
run_pwa_story() {
  local story_id="${1:-${CURRENT_STORY:-TS-000}}"
  local pwa_dir
  pwa_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/pwa" && pwd)"
  local pwa_script="$pwa_dir/${story_id}.mjs"

  # Find node — respect nvm if present
  local node_bin=""
  if [[ -n "${NVM_DIR:-}" && -s "${NVM_DIR}/nvm.sh" ]]; then
    node_bin="$(. "${NVM_DIR}/nvm.sh" --no-use 2>/dev/null && command -v node 2>/dev/null || true)"
  fi
  [[ -z "$node_bin" ]] && node_bin="$(command -v node 2>/dev/null || true)"

  if [[ -z "$node_bin" ]]; then
    skip "Node.js not available — skip PWA visual test $story_id"
    return
  fi

  if [[ ! -f "$pwa_script" ]]; then
    skip "No PWA script yet for $story_id — stub"
    return
  fi

  mkdir -p "${EVIDENCE_DIR:-/tmp}/${story_id}"
  export TEST_HTTP TEST_TLS TEST_TOKEN EVIDENCE_DIR CURRENT_STORY="$story_id"

  if "$node_bin" "$pwa_script" 2>"${EVIDENCE_DIR:-/tmp}/${story_id}/playwright.log"; then
    ok "PWA visual test passed"
  else
    ko "PWA visual test failed (see ${EVIDENCE_DIR:-/tmp}/${story_id}/playwright.log)"
  fi
}

true
