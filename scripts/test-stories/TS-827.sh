#!/usr/bin/env bash
# TS-827 — three single-node ollama entries, a two-node pi-prompt entry, and
# three pi-prompt workers on a sandbox daemon.
# tags: surface:api feature:capacity
#
# The story starts its own sandbox daemon from the built binary, with a
# config generated from testdata/datawatch.yaml on free loopback ports. It
# registers two compute nodes (loopback Ollama stand-ins, one slot each) and
# the production entry shape the story protects: three single-node ollama
# LLM entries (one node each), plus a pi-prompt entry whose compute_nodes
# lists two of those nodes, plus a pi entry and an opencode entry. A fake pi
# on PATH reads the per-session provider override the launch writes
# (PI_CODING_AGENT_DIR/models.json) and posts one request to that provider's
# base URL.
#
# Three pi-prompt sessions start through the API. The pi-prompt entry's
# own two-node list is the admission pool, so the ledger spreads the first
# two over the two different nodes. The third must sit in the wait queue —
# both slots are held — and must never take a node that is already in use.
# GET /api/capacity must show exactly that: two leases on two different
# nodes, one waiter for the third session, no pool over its limit, and each
# stand-in receiving exactly one pi request. The sessions, the LLM entries,
# the nodes, the stand-ins, the daemon and the scratch directory are all
# stopped or removed afterwards.
#
# TMUX and TMUX_TMPDIR are removed for everything this story starts. The
# daemon reaches tmux through a shim on its PATH that pins every call to a
# socket this story owns, so no other tmux server is used.
#
# Set DWP_KEEP=1 to keep the scratch directory for inspection.

unset TMUX TMUX_TMPDIR

DWP_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DWP_TMP="$(mktemp -d "${TMPDIR:-/tmp}/dw-dwp-XXXXXX")"
export DATAWATCH_TEST_DIR="${DATAWATCH_TEST_DIR:-$DWP_TMP}"
TEST_TOKEN="dw-dwp-token"
CURRENT_STORY="TS-827"

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DWP_BINARY="${TEST_BINARY:-$DWP_REPO/bin/datawatch}"
DWP_BIN="$DWP_TMP/bin"
DWP_DATA="$DWP_TMP/data"
DWP_AGENT="$DWP_TMP/pi-agent"
DWP_CFG="$DWP_TMP/config.yaml"
DWP_SOCK="$DWP_TMP/tmux.sock"
DWP_LOG_A="$DWP_TMP/alpha.log"
DWP_LOG_B="$DWP_TMP/bravo.log"
# Two compute nodes. alpha-node and bravo-node are the ledger's slots.
DWP_NODE_A="alpha-node"
DWP_NODE_B="bravo-node"
# Production entry shape: three single-node ollama entries, one node each
# (delta shares bravo-node, the production pattern of several entries on one
# node), and the pi-prompt entry whose compute_nodes lists the two nodes the
# sessions may take. The pi and opencode entries complete the shape without
# claiming a node.
DWP_LLM_ALPHA="ollama-alpha"
DWP_LLM_BRAVO="ollama-bravo"
DWP_LLM_DELTA="ollama-delta"
DWP_LLM_PROMPT="pi-prompt"
DWP_LLM_PI="pi"
DWP_LLM_OPENCODE="opencode"
# The backend the sessions start on, and the model they run.
DWP_BACKEND="pi-prompt"
DWP_MODEL="ollama/qwen3.8:27b"
DWP_SESSION_IDS=()
DWP_DAEMON_PID=""
DWP_FAKE_A_PID=""
DWP_FAKE_B_PID=""
DWP_THIRD_PID=""
DWP_THIRD_DONE=0
DWP_FAILED=""
DWP_LEASE_MAP="{}"
DWP_NODE_LIST=""
DWP_THIRD_CODE=""
DWP_CODE=""
DWP_BODY=""

# dwp_fail <message> — record a failed check and keep going to cleanup.
dwp_fail() {
  echo "  detail: $*"
  DWP_FAILED="${DWP_FAILED:+$DWP_FAILED; }$*"
}

# dwp_free_port <start> — first free loopback TCP port at or above start.
dwp_free_port() {
  python3 -I -c '
import socket, sys
p = int(sys.argv[1])
while p < 65000:
    s = socket.socket()
    try:
        s.bind(("127.0.0.1", p))
        print(p)
        break
    except OSError:
        pass
    finally:
        s.close()
    p += 1
' "$1"
}

# dwp_req <METHOD> <path> [json-body] — sets DWP_CODE and DWP_BODY.
dwp_req() {
  local method="$1" path="$2" body="${3:-}" out
  if [[ -n "$body" ]]; then
    out=$(curl -sk --max-time 30 -X "$method" -H "Authorization: Bearer $TEST_TOKEN" \
      -H "Content-Type: application/json" -d "$body" -w '\n%{http_code}' "$TEST_BASE$path")
  else
    out=$(curl -sk --max-time 30 -X "$method" -H "Authorization: Bearer $TEST_TOKEN" \
      -w '\n%{http_code}' "$TEST_BASE$path")
  fi
  DWP_CODE="${out##*$'\n'}"
  DWP_BODY="${out%$'\n'*}"
}

# dwp_wait_until <seconds> <command...> — polls until the command succeeds.
dwp_wait_until() {
  local limit="$1" end
  shift
  end=$((SECONDS + limit))
  while ((SECONDS < end)); do
    if "$@"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

dwp_healthy() {
  curl -skf --max-time 3 "$TEST_BASE/api/health" >/dev/null 2>&1
}

dwp_fake_ready() {
  curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$1/api/ps"
}

# dwp_chat_count <log> — number of pi requests (POST /v1/chat/completions) logged.
dwp_chat_count() {
  awk '$0 == "POST /v1/chat/completions" { n++ } END { print n + 0 }' "$1" 2>/dev/null
}

# dwp_request_count <log> — every request a stand-in has logged.
dwp_request_count() {
  awk 'NF { n++ } END { print n + 0 }' "$1" 2>/dev/null
}

dwp_both_chat_seen() {
  (($(dwp_chat_count "$DWP_LOG_A") >= 1 && $(dwp_chat_count "$DWP_LOG_B") >= 1))
}

dwp_pools_ready() {
  dwp_req GET /api/capacity
  [[ "$(printf '%s' "$DWP_BODY" | python3 -I -c '
import json, sys
d = json.load(sys.stdin)
lim = {p.get("name"): p.get("limit") for p in d.get("pools") or []}
print(1 if lim.get("node:'"$DWP_NODE_A"'") == 1 and lim.get("node:'"$DWP_NODE_B"'") == 1 else 0)
' 2>/dev/null)" == 1 ]]
}

# dwp_lease_map — sets DWP_LEASE_MAP to {session id: node} for the story's sessions.
dwp_lease_map() {
  dwp_req GET /api/capacity
  DWP_LEASE_MAP=$(printf '%s' "$DWP_BODY" | python3 -I -c '
import json, sys
ids = sys.argv[1:]
d = json.load(sys.stdin)
out = {}
for lease in d.get("leases") or []:
    text = (lease.get("session_id") or "") + " " + (lease.get("holder") or "")
    for sid in ids:
        if sid and sid in text:
            out[sid] = lease.get("node") or ""
print(json.dumps(out))
' "${DWP_SESSION_IDS[@]}" 2>/dev/null)
}

dwp_leases_ready() {
  dwp_lease_map
  [[ "$(printf '%s' "$DWP_LEASE_MAP" | python3 -I -c '
import json, sys
m = json.load(sys.stdin)
print(1 if len(m) == 2 and all(m.values()) else 0)
' 2>/dev/null)" == 1 ]]
}

# dwp_waiting_seen — the third session is in the ledger's wait queue: a
# waiter whose pools include the pi-prompt entry and whose candidate nodes
# are exactly the two the entry lists, held by nobody already leased.
dwp_waiting_seen() {
  dwp_req GET /api/capacity
  local snap="$DWP_TMP/capacity-wait.json"
  printf '%s' "$DWP_BODY" > "$snap"
  printf '%s' "$DWP_BODY" | python3 -I -c '
import json, sys
d = json.load(sys.stdin)
leased = {l.get("holder") for l in d.get("leases") or []}
want = {"'"$DWP_NODE_A"'", "'"$DWP_NODE_B"'"}
for w in d.get("waiting") or []:
    pools = w.get("pools") or []
    if "llm:pi-prompt" in pools and set(w.get("nodes") or []) == want and w.get("holder") not in leased:
        sys.exit(0)
sys.exit(1)
' 2>/dev/null
}

# dwp_within_limits — no pool in the last capacity snapshot holds more (or,
# with external sessions, reaches more) than its limit.
dwp_within_limits() {
  dwp_req GET /api/capacity
  printf '%s' "$DWP_BODY" | python3 -I -c '
import json, sys
d = json.load(sys.stdin)
for p in d.get("pools") or []:
    lim = p.get("limit")
    if lim is None:
        continue
    if (p.get("held") or 0) + (p.get("external") or 0) > lim:
        sys.exit(1)
' 2>/dev/null
}

# dwp_third_done — the background third start's request has finished.
dwp_third_done() {
  kill -0 "$DWP_THIRD_PID" 2>/dev/null
  return $((1 - $?))
}

dwp_write_config() {
  local tmpl="$DWP_REPO/testdata/datawatch.yaml"
  sed \
    -e "s|data_dir: /data|data_dir: $DWP_DATA|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $DWP_DATA/projects|g" \
    -e "s|port: 8080|port: $DWP_HTTP|g" \
    -e "s|tls_enabled: true|tls_enabled: true\n  allowed_project_roots:\n    - $DWP_TMP|" \
    -e "s|tls_port: 8443|tls_port: $DWP_TLS|g" \
    -e "s|sse_port: 9090|sse_port: $DWP_MCP|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TEST_TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$DWP_DNS\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$DWP_WEBHOOK|g" \
    -e '/^ollama:/,/^$/d' \
    "$tmpl" > "$DWP_CFG" || return 1
  # The legacy ollama block is migrated into a kind-ollama entry. Capacity
  # keeps one entry per kind, so that migrated entry would shadow the story's
  # own kind-ollama entries; the block is left out.
  # The pi-prompt launch resolves its binary from this block; "pi" is found on
  # the daemon's PATH, which starts with the story's bin directory.
  cat >> "$DWP_CFG" <<'PI_BLOCK'

pi:
  enabled: true
  binary: pi
PI_BLOCK
}

# dwp_write_fakes — the fake pi, the tmux shim and the fake Ollama server.
dwp_write_fakes() {
  mkdir -p "$DWP_BIN" "$DWP_AGENT" "$DWP_DATA"
  local real_tmux
  real_tmux="$(command -v tmux)" || return 1

  # Source provider table the per-session agent directory is built from. Its
  # baseUrl points at a closed port, so a launch that ignored the override
  # would fail rather than reach a stand-in.
  cat > "$DWP_AGENT/models.json" <<'MODELS'
{"providers":{"ollama":{"api":"openai-completions","apiKey":"ollama","baseUrl":"http://127.0.0.1:9/v1","models":[{"id":"qwen3.8:27b"}]}}}
MODELS

  cat > "$DWP_BIN/pi" <<'FAKE_PI'
#!/bin/sh
# Fake pi: reads the provider base URL from the per-session agent directory
# the launch set, then makes one chat request there.
base=$(sed -n 's/.*"baseUrl": *"\([^"]*\)".*/\1/p' "$PI_CODING_AGENT_DIR/models.json" | head -n 1)
if [ -z "$base" ]; then
  echo "fake pi: no baseUrl in $PI_CODING_AGENT_DIR/models.json" >&2
  exit 2
fi
exec curl -fsS -X POST -H 'Content-Type: application/json' \
  -d '{"model":"qwen3.8:27b","messages":[{"role":"user","content":"ping"}]}' \
  "$base/chat/completions"
FAKE_PI
  chmod +x "$DWP_BIN/pi"

  # tmux shim: every tmux call the daemon makes goes to the story's own socket.
  cat > "$DWP_BIN/tmux" <<SHIM
#!/bin/sh
exec "$real_tmux" -S "$DWP_SOCK" "\$@"
SHIM
  chmod +x "$DWP_BIN/tmux"

  cat > "$DWP_TMP/fake_ollama.py" <<'FAKE_OLLAMA'
import http.server
import json
import sys

port, name, log = int(sys.argv[1]), sys.argv[2], sys.argv[3]
resident = set()


class Handler(http.server.BaseHTTPRequestHandler):
    def record(self):
        with open(log, "a") as f:
            f.write(f"{self.command} {self.path}\n")

    def reply(self, obj):
        body = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.record()
        if self.path == "/api/ps":
            self.reply({"models": [{"name": m, "model": m} for m in sorted(resident)]})
        else:
            self.send_error(404)

    def do_POST(self):
        self.record()
        length = int(self.headers.get("Content-Length") or 0)
        data = json.loads(self.rfile.read(length) or b"{}")
        if self.path == "/api/generate":
            resident.add(data.get("model", ""))
            self.reply({"done": True})
        elif self.path == "/v1/chat/completions":
            self.reply({"choices": [{"message": {"role": "assistant", "content": f"answer from {name}"}}]})
        else:
            self.send_error(404)

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
FAKE_OLLAMA
}

dwp_start_daemon() {
  # The daemon's own temp files (tooling directories, per-session pi agent
  # directories) land in the story's scratch area, so cleanup removes them too.
  mkdir -p "$DWP_TMP/daemon-tmp"
  env -u TMUX -u TMUX_TMPDIR PATH="$DWP_BIN:$PATH" PI_CODING_AGENT_DIR="$DWP_AGENT" \
    TMPDIR="$DWP_TMP/daemon-tmp" \
    "$DWP_BINARY" start --foreground --config "$DWP_CFG" >> "$DWP_TMP/daemon.log" 2>&1 &
  DWP_DAEMON_PID=$!
}

dwp_stop_daemon() {
  [[ -n "$DWP_DAEMON_PID" ]] || return 0
  kill "$DWP_DAEMON_PID" 2>/dev/null || true
  local end=$((SECONDS + 20))
  while kill -0 "$DWP_DAEMON_PID" 2>/dev/null && ((SECONDS < end)); do
    sleep 1
  done
  kill -0 "$DWP_DAEMON_PID" 2>/dev/null && kill -9 "$DWP_DAEMON_PID" 2>/dev/null
  wait "$DWP_DAEMON_PID" 2>/dev/null || true
  DWP_DAEMON_PID=""
}

# dwp_run — the story body. Every failure is recorded by dwp_fail and the
# function returns, so dwp_cleanup always runs.
dwp_run() {
  if [[ ! -x "$DWP_BINARY" ]]; then
    skip "built binary not found at $DWP_BINARY"
    return 0
  fi
  if ! command -v tmux >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
    skip "tmux, curl and python3 are required"
    return 0
  fi

  # Free sandbox ports; the template's production ports (8080/8443) are never chosen.
  DWP_HTTP="$(dwp_free_port 18080)"
  DWP_TLS="$(dwp_free_port $((DWP_HTTP + 1)))"
  DWP_MCP="$(dwp_free_port $((DWP_TLS + 1)))"
  DWP_WEBHOOK="$(dwp_free_port 19080)"
  DWP_DNS="$(dwp_free_port 19053)"
  local port_a port_b
  port_a="$(dwp_free_port 19200)"
  port_b="$(dwp_free_port $((port_a + 1)))"
  TEST_BASE="https://127.0.0.1:$DWP_TLS"

  dwp_write_fakes || { dwp_fail "could not write fakes"; return 0; }
  dwp_write_config || { dwp_fail "could not write sandbox config"; return 0; }

  dwp_start_daemon
  if ! dwp_wait_until 40 dwp_healthy; then
    dwp_fail "sandbox daemon did not answer /api/health; daemon.log tail: $(tail -n 5 "$DWP_TMP/daemon.log" 2>/dev/null | tr '\n' ' ')"
    return 0
  fi

  # Two loopback Ollama stand-ins, one per node. The readiness probe is
  # logged, so reset the logs before the test traffic starts.
  env -u TMUX -u TMUX_TMPDIR python3 -I "$DWP_TMP/fake_ollama.py" "$port_a" "$DWP_NODE_A" "$DWP_LOG_A" >/dev/null 2>&1 &
  DWP_FAKE_A_PID=$!
  env -u TMUX -u TMUX_TMPDIR python3 -I "$DWP_TMP/fake_ollama.py" "$port_b" "$DWP_NODE_B" "$DWP_LOG_B" >/dev/null 2>&1 &
  DWP_FAKE_B_PID=$!
  if ! dwp_wait_until 20 dwp_fake_ready "$port_a" || ! dwp_wait_until 20 dwp_fake_ready "$port_b"; then
    dwp_fail "loopback Ollama stand-ins did not start on $port_a / $port_b"
    return 0
  fi
  : > "$DWP_LOG_A"
  : > "$DWP_LOG_B"

  # Two compute nodes with different scheduling priorities; one slot each, so
  # each of the first two workers has to take a different node, and the third
  # has nowhere to go but the wait queue.
  dwp_req POST /api/compute/nodes "{\"name\":\"$DWP_NODE_A\",\"kind\":\"ollama\",\"address\":\"http://127.0.0.1:$port_a\",\"scheduling_priority\":90,\"session_slots\":1}"
  [[ "$DWP_CODE" == 2* ]] || { dwp_fail "node $DWP_NODE_A not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0; }
  dwp_req POST /api/compute/nodes "{\"name\":\"$DWP_NODE_B\",\"kind\":\"ollama\",\"address\":\"http://127.0.0.1:$port_b\",\"scheduling_priority\":50,\"session_slots\":1}"
  [[ "$DWP_CODE" == 2* ]] || { dwp_fail "node $DWP_NODE_B not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0; }

  # The production entry shape. The three single-node ollama entries are what
  # the admission gate finds when it looks up the model's provider; each lists
  # exactly one node (delta shares bravo-node, the production pattern of
  # several entries on one node). The pi-prompt entry's own two-node
  # compute_nodes list is the pool the sessions are admitted against — the
  # ledger, not the entry count, is what limits them.
  dwp_req POST /api/llms "{\"name\":\"$DWP_LLM_ALPHA\",\"kind\":\"ollama\",\"compute_nodes\":[\"$DWP_NODE_A\"]}"
  [[ "$DWP_CODE" == 2* ]] || { dwp_fail "entry $DWP_LLM_ALPHA not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0; }
  dwp_req POST /api/llms "{\"name\":\"$DWP_LLM_BRAVO\",\"kind\":\"ollama\",\"compute_nodes\":[\"$DWP_NODE_B\"]}"
  [[ "$DWP_CODE" == 2* ]] || { dwp_fail "entry $DWP_LLM_BRAVO not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0; }
  dwp_req POST /api/llms "{\"name\":\"$DWP_LLM_DELTA\",\"kind\":\"ollama\",\"compute_nodes\":[\"$DWP_NODE_B\"]}"
  [[ "$DWP_CODE" == 2* ]] || { dwp_fail "entry $DWP_LLM_DELTA not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0; }
  # The in-flight cap is three so the entry is never the limit; the node
  # slots are. Its two-node compute_nodes list is the admission pool.
  dwp_req POST /api/llms "{\"name\":\"$DWP_LLM_PROMPT\",\"kind\":\"pi-prompt\",\"max_inflight\":3,\"compute_nodes\":[\"$DWP_NODE_A\",\"$DWP_NODE_B\"]}"
  [[ "$DWP_CODE" == 2* ]] || { dwp_fail "entry $DWP_LLM_PROMPT not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0; }
  # The pi and opencode entries complete the production shape; neither claims
  # a compute node, so they take no slot in the ledger. The daemon may already
  # host them, migrated from the test config's legacy blocks: a conflict
  # then means the shape is already complete, so accept it.
  dwp_req POST /api/llms "{\"name\":\"$DWP_LLM_PI\",\"kind\":\"pi\"}"
  if [[ "$DWP_CODE" != 2* && "$DWP_CODE" != 409 ]]; then
    dwp_fail "entry $DWP_LLM_PI not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0
  fi
  dwp_req POST /api/llms "{\"name\":\"$DWP_LLM_OPENCODE\",\"kind\":\"opencode\"}"
  if [[ "$DWP_CODE" != 2* && "$DWP_CODE" != 409 ]]; then
    dwp_fail "entry $DWP_LLM_OPENCODE not registered (HTTP $DWP_CODE): $DWP_BODY"; return 0
  fi

  if ! dwp_wait_until 60 dwp_pools_ready; then
    dwp_fail "node slot limits of 1 never reached the capacity ledger"
    return 0
  fi

  local project i sid
  for i in 1 2; do
    project="$DWP_TMP/project-$i"
    mkdir -p "$project"
    dwp_req POST /api/sessions/start "{\"task\":\"production-shape worker $i\",\"project_dir\":\"$project\",\"backend\":\"$DWP_BACKEND\",\"model\":\"$DWP_MODEL\",\"name\":\"worker-$i\"}"
    if [[ "$DWP_CODE" != 2* ]]; then
      dwp_fail "session $i did not start (HTTP $DWP_CODE): $DWP_BODY"
      return 0
    fi
    sid="$(printf '%s' "$DWP_BODY" | python3 -I -c 'import json, sys; print(json.load(sys.stdin).get("id", ""))' 2>/dev/null)"
    if [[ -z "$sid" ]]; then
      dwp_fail "session $i response had no id: $DWP_BODY"
      return 0
    fi
    DWP_SESSION_IDS+=("$sid")
  done

  # Both admitted workers must have actually run their one pi request (a fake
  # pi that timed out or crashed would hang this rather than pass silently).
  if ! dwp_wait_until 120 dwp_both_chat_seen; then
    dwp_fail "pi requests did not reach both stand-ins (alpha=$(dwp_chat_count "$DWP_LOG_A") bravo=$(dwp_chat_count "$DWP_LOG_B"))"
    return 0
  fi

  # The two sessions hold two different nodes — the ledger spread them, one
  # per node.
  if ! dwp_wait_until 30 dwp_leases_ready; then
    dwp_fail "capacity leases for both workers not found: $DWP_LEASE_MAP"
    return 0
  fi
  local verdict
  verdict="$(printf '%s' "$DWP_LEASE_MAP" | python3 -I -c '
import json, sys
m = json.load(sys.stdin)
ids = sys.argv[1:]
nodes = [m.get(i, "") for i in ids]
want = {"'"$DWP_NODE_A"'", "'"$DWP_NODE_B"'"}
print("ok" if all(nodes) and len(set(nodes)) == len(nodes) and set(nodes) == want else "bad " + json.dumps(m))
' "${DWP_SESSION_IDS[@]}" 2>/dev/null)"
  if [[ "$verdict" != ok ]]; then
    dwp_fail "the two admitted workers do not hold different nodes: $verdict"
    return 0
  fi
  DWP_NODE_LIST="$(printf '%s' "$DWP_LEASE_MAP" | python3 -I -c 'import json,sys; print(", ".join(sorted(set(json.load(sys.stdin).values()))))' 2>/dev/null)"

  # The first two hold different nodes. No pool is over its limit while they
  # hold exactly one slot each.
  if ! dwp_within_limits; then
    dwp_fail "a node pool is over its one-slot limit while two sessions hold one slot each"
  fi

  # The third worker: its admission must sit in the wait queue, not take a
  # held node. The interactive start blocks on admission, so it runs in the
  # background while the ledger is observed in the wait state.
  project="$DWP_TMP/project-3"
  mkdir -p "$project"
  curl -sk --max-time 30 -X POST -H "Authorization: Bearer $TEST_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"task\":\"production-shape worker 3\",\"project_dir\":\"$project\",\"backend\":\"$DWP_BACKEND\",\"model\":\"$DWP_MODEL\",\"name\":\"worker-3\"}" \
    -w '\n%{http_code}' "$TEST_BASE/api/sessions/start" > "$DWP_TMP/third_start.out" 2>&1 &
  DWP_THIRD_PID=$!

  if ! dwp_wait_until 20 dwp_waiting_seen; then
    dwp_fail "third worker not seen in the wait queue (capacity snapshot: $(cat "$DWP_TMP/capacity-wait.json" 2>/dev/null | tr '\n' ' '))"
  else
    # The ledger says "waiting" for the third worker while both slots are
    # held: it has not been granted a node.
    if ! dwp_within_limits; then
      dwp_fail "the waiting worker pushed a node pool past its one-slot limit"
    fi
  fi

  # The blocked start resolves: after the short interactive wait it is
  # refused, because no slot frees. Success would mean a held node granted a
  # third session, which is exactly what this story guards against.
  dwp_wait_until 45 dwp_third_done && DWP_THIRD_DONE=1
  DWP_THIRD_CODE="$(tail -n 1 "$DWP_TMP/third_start.out" 2>/dev/null)"
  if [[ "$DWP_THIRD_CODE" == 200 ]]; then
    dwp_fail "third worker was granted a node both workers are already holding (HTTP $DWP_THIRD_CODE)"
    local third_sid
    third_sid="$(head -n -1 "$DWP_TMP/third_start.out" 2>/dev/null | python3 -I -c 'import json,sys
try: print(json.load(sys.stdin).get("id") or "")
except Exception: print("")' 2>/dev/null)"
    [[ -n "$third_sid" ]] && DWP_SESSION_IDS+=("$third_sid")
  fi

  # Settle: give a straggling request a moment to arrive, then each stand-in
  # must have received exactly one pi request — two on one stand-in is the
  # visible trace of a second session on a node that was already in use, and
  # zero means an admitted worker never ran.
  sleep 3
  local count_a count_b
  count_a="$(dwp_chat_count "$DWP_LOG_A")"
  count_b="$(dwp_chat_count "$DWP_LOG_B")"
  if [[ "$count_a" != 1 || "$count_b" != 1 ]]; then
    dwp_fail "each stand-in should have exactly one pi request; alpha=$count_a bravo=$count_b (alpha log: $(tr '\n' ';' < "$DWP_LOG_A") bravo log: $(tr '\n' ';' < "$DWP_LOG_B"))"
  fi
}

# dwp_cleanup — removes what the story created. Each step runs only if the
# thing removed was created here.
dwp_cleanup() {
  local sid
  if [[ -n "$DWP_DAEMON_PID" ]]; then
    for sid in "${DWP_SESSION_IDS[@]}"; do
      dwp_req POST /api/sessions/kill "{\"id\":\"$sid\"}"
    done
    dwp_req DELETE "/api/llms/$DWP_LLM_ALPHA"
    dwp_req DELETE "/api/llms/$DWP_LLM_BRAVO"
    dwp_req DELETE "/api/llms/$DWP_LLM_DELTA"
    dwp_req DELETE "/api/llms/$DWP_LLM_PROMPT"
    # pi and opencode may have been the daemon's own migrated entries (409 at
    # registration); delete what the story owns either way — the daemon goes
    # down with the story, so they are not re-checked in the listing below.
    dwp_req DELETE "/api/llms/$DWP_LLM_PI"
    dwp_req DELETE "/api/llms/$DWP_LLM_OPENCODE"
    dwp_req DELETE "/api/compute/nodes/$DWP_NODE_A"
    dwp_req DELETE "/api/compute/nodes/$DWP_NODE_B"
    dwp_req GET /api/llms
    # pi and opencode are exempt: they may be the daemon's own migrated entries
    # (409 at registration), not story-created ones.
    case "$DWP_BODY" in
      *'"name":"ollama-alpha"'*|      *'"name":"ollama-bravo"'*|      *'"name":"ollama-delta"'*|      *'"name":"pi-prompt"'*)
        dwp_fail "story LLM entries still listed after cleanup" ;;
    esac
    dwp_req GET /api/compute/nodes
    if [[ "$DWP_BODY" == *"$DWP_NODE_A"* || "$DWP_BODY" == *"$DWP_NODE_B"* ]]; then
      dwp_fail "compute nodes still listed after cleanup"
    fi
  fi
  dwp_stop_daemon
  # The blocked third start, if it has not finished on its own, is stopped by
  # its recorded pid; the ledger releases its waiter when the request drops.
  if [[ -n "$DWP_THIRD_PID" ]] && kill -0 "$DWP_THIRD_PID" 2>/dev/null; then
    kill "$DWP_THIRD_PID" 2>/dev/null || true
    wait "$DWP_THIRD_PID" 2>/dev/null || true
  fi
  [[ -n "$DWP_FAKE_A_PID" ]] && kill "$DWP_FAKE_A_PID" 2>/dev/null
  [[ -n "$DWP_FAKE_B_PID" ]] && kill "$DWP_FAKE_B_PID" 2>/dev/null
  wait "$DWP_FAKE_A_PID" "$DWP_FAKE_B_PID" 2>/dev/null || true
  # Only the story's own socket is probed. A stale socket file is harmless, so
  # the check is whether any session is still listed on it after cleanup.
  local sock="$DWP_SOCK" left
  if [[ -S "$sock" ]]; then
    left="$("$(command -v tmux)" -S "$sock" list-sessions 2>/dev/null)"
    if [[ -n "$left" ]]; then
      dwp_fail "tmux sessions still listed on the story socket after cleanup"
    fi
  fi
  if [[ -z "${DWP_KEEP:-}" ]]; then
    rm -rf "$DWP_TMP"
  fi
}

dwp_main() {
  story_preflight "surface:api feature:capacity" || return 0
  dwp_run
  dwp_cleanup
  if [[ -n "$DWP_FAILED" ]]; then
    ko "three pi-prompt workers on two one-slot nodes: $DWP_FAILED"
  elif [[ "${RESULT:-}" == skip ]]; then
    return 0
  elif [[ -n "$DWP_NODE_LIST" ]]; then
    ok "two workers hold different nodes ($DWP_NODE_LIST), the third waited; each stand-in received one pi request"
  else
    skip "story ended without a verdict"
  fi
}

dwp_main

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  [[ "$RESULT" == fail ]] && exit 1
  exit 0
fi
