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
TEST_DNS_PORT="${TEST_DNS_PORT:-19053}"

# Isolated Docker-sim ports (T13) — dynamically allocated by run-tests.sh; fallbacks for direct invocation
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
# Docker-sim state (T13 stories) — must be initialised so bash 5.3 set -u is happy
: "${DOCKER_SIM_PID:=}"; : "${DOCKER_SIM_CONTAINER:=}"; : "${DOCKER_SIM_IMAGE:=}"

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
  "$TEST_BINARY" --config "$TEST_DATA/config.yaml" --url "http://127.0.0.1:${TEST_PORT:-8080}" "$@"
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

# api_mcp <tool> [params_json] — POST to /api/mcp/call and unwrap content envelope
api_mcp() {
  local tool="$1" params="${2:-{}}"
  local raw
  raw=$(api POST /api/mcp/call "{\"tool\":\"$tool\",\"params\":$params}")
  mcp_unwrap "$raw"
}

# mcp_unwrap — extract inner payload from MCP content envelope
# {"content":[{"text":"...","type":"text"}]} → inner text; passthrough otherwise
mcp_unwrap() {
  local raw="$1"
  echo "$raw" | python3 -c "
import json, sys
raw = sys.stdin.read()
try:
    d = json.loads(raw)
    if isinstance(d, dict) and 'content' in d:
        items = d['content']
        if isinstance(items, list) and items and 'text' in items[0]:
            print(items[0]['text'])
            sys.exit(0)
    print(raw)
except Exception:
    print(raw)
" 2>/dev/null || echo "$raw"
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
    local chk sid_check
    sid_check="$SESSION_ID"
    # Verify session still exists in the sessions list (no single-item GET endpoint)
    chk=$(api GET /api/sessions 2>/dev/null)
    if echo "$chk" | python3 -c "
import json,sys
d=json.load(sys.stdin)
items=d if isinstance(d,list) else d.get('sessions',[])
sid='$sid_check'
found=any(s.get('id','')==sid or s.get('full_id','').endswith('-'+sid) for s in items)
assert found
" 2>/dev/null; then
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

  local pwa_rc=0
  "$node_bin" "$pwa_script" 2>"${EVIDENCE_DIR:-/tmp}/${story_id}/playwright.log" || pwa_rc=$?
  if [[ $pwa_rc -eq 0 ]]; then
    ok "PWA visual test passed"
  elif [[ $pwa_rc -eq 2 ]]; then
    local skip_reason
    skip_reason=$(cat "${EVIDENCE_DIR:-/tmp}/${story_id}/result.txt" 2>/dev/null || echo "precondition not met")
    skip "PWA story skipped: $skip_reason"
  else
    ko "PWA visual test failed (see ${EVIDENCE_DIR:-/tmp}/${story_id}/playwright.log)"
  fi
}

# ---------------------------------------------------------------------------
# Legacy helper: write isolated config for docker-sim / standalone instances.
# write_test_config <data_dir> <http_port> <tls_port> <mcp_port> <chan_port> [token]
# ---------------------------------------------------------------------------
write_test_config() {
  local data_dir="$1" http_port="$2" _tls_port="$3" mcp_port="$4" _chan_port="$5"
  local token="${6:-${TEST_TOKEN:-dw-test-token-12345}}"
  mkdir -p "$data_dir"
  local tmpl="${REPO_ROOT}/testdata/datawatch.yaml"
  if [[ -f "$tmpl" ]]; then
    sed \
      -e "s|data_dir: /data|data_dir: $data_dir|g" \
      -e "s|port: 8080|port: $http_port|g" \
      -e "s|tls_port: 8443|tls_port: $_tls_port|g" \
      -e "s|sse_port: 9090|sse_port: $mcp_port|g" \
      -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
      -e "s|listen: \"127\.0\.0\.1:19053\"|listen: \"127.0.0.1:${TEST_DNS_PORT:-19053}\"|g" \
      "$tmpl" > "$data_dir/config.yaml"
  else
    cat > "$data_dir/config.yaml" <<YAML
server:
  host: 127.0.0.1
  port: ${http_port}
  data_dir: ${data_dir}
  token: ${token}
YAML
  fi
}

# ---------------------------------------------------------------------------
# Legacy helper: check if a k8s cluster is reachable (T14 stories).
# Echoes "yes" or "no".
# ---------------------------------------------------------------------------
t14_check_cluster() {
  local ctx="${K8S_CONTEXT:-testing}"
  if ! command -v kubectl >/dev/null 2>&1; then echo "no"; return; fi
  kubectl --context="$ctx" cluster-info >/dev/null 2>&1 && echo "yes" || echo "no"
}

# ---------------------------------------------------------------------------
# Legacy helper: check if autonomous mode is enabled in the test daemon.
# Echoes "yes" or "no".
# ---------------------------------------------------------------------------
t3_check_autonomous() {
  api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' \
    2>/dev/null || echo "no"
}

# ---------------------------------------------------------------------------
# Legacy helper: check if the memory subsystem is enabled and responsive.
# Echoes "yes" or "no".
# ---------------------------------------------------------------------------
t5_check_memory() {
  api GET /api/memory/stats \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' \
    2>/dev/null || echo "no"
}

# ---------------------------------------------------------------------------
# wait_for_llm_backend [max_tries] [sleep_sec]
#   Returns the comma-joined list of available+enabled LLM backend names, or
#   empty string if none available after all retries. Retries to allow Ollama
#   (and other backends) time to load the model on first use.
# ---------------------------------------------------------------------------
wait_for_llm_backend() {
  local max_tries="${1:-3}" sleep_sec="${2:-15}"
  local avail=""
  for ((i=1; i<=max_tries; i++)); do
    avail=$(api GET /api/backends | python3 -c '
import json,sys
d=json.load(sys.stdin)
have=[b["name"] for b in d.get("llm",[]) if b.get("enabled") and b.get("available")]
print(",".join(have))
' 2>/dev/null || echo "")
    [[ -n "$avail" ]] && break
    if (( i < max_tries )); then
      echo "  [llm] no backend available yet (attempt $i/$max_tries) — waiting ${sleep_sec}s for model load..."
      sleep "$sleep_sec"
    fi
  done
  echo "$avail"
}

# ---------------------------------------------------------------------------
# ensure_test_plugin — idempotently creates a minimal test plugin in the
# test daemon's plugins directory and triggers a reload so the daemon picks
# it up. Returns 0 if the plugin is now listed; 1 on failure.
#
# The plugin is named "dw-test-plugin" and listens on the pre_session_start
# and on_alert hooks; it always responds with {"ok":true,"action":"pass"}.
# ---------------------------------------------------------------------------
ensure_test_plugin() {
  local plugin_dir="${TEST_DATA}/plugins/dw-test-plugin"
  mkdir -p "$plugin_dir"

  # Write the entry script (executable shell plugin)
  cat > "$plugin_dir/run.sh" <<'PLUGIN_SCRIPT'
#!/usr/bin/env bash
# datawatch test plugin — reads one JSON line on stdin, writes pass response.
read -r _line
echo '{"ok":true,"action":"pass"}'
PLUGIN_SCRIPT
  chmod +x "$plugin_dir/run.sh"

  # Write manifest.yaml
  cat > "$plugin_dir/manifest.yaml" <<MANIFEST
name: dw-test-plugin
description: Minimal E2E test plugin — validates plugin discovery and invocation.
version: "1.0.0"
entry: run.sh
hooks:
  - pre_session_start
  - on_alert
timeout_ms: 5000
mode: oneshot
MANIFEST

  # Trigger reload so the running daemon discovers the plugin
  local reload_code
  reload_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer ${TEST_TOKEN:-}" \
    "${TEST_BASE}/api/plugins/reload" 2>/dev/null || echo "000")
  if [[ "$reload_code" != "200" ]]; then
    return 1
  fi

  # Verify the plugin is now listed
  api GET /api/plugins | python3 -c '
import json,sys
d=json.load(sys.stdin)
arr=d.get("plugins",[]) if isinstance(d,dict) else d
found=any(p.get("name")=="dw-test-plugin" for p in (arr or []))
sys.exit(0 if found else 1)
' 2>/dev/null
}

true
