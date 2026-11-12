#!/usr/bin/env bash
# TS-820 — two pi-prompt workers hold different compute nodes, on a sandbox daemon.
# tags: surface:api feature:capacity
#
# The story starts its own sandbox daemon from the built binary, with a config
# generated from testdata/datawatch.yaml on free loopback ports. It registers
# two compute nodes (loopback Ollama stand-ins with different scheduling
# priorities, one slot each) and one pi-prompt LLM entry listing both. A fake
# pi on PATH reads the per-session provider override the launch writes
# (PI_CODING_AGENT_DIR/models.json) and posts one request to that provider's
# base URL. After two pi-prompt sessions start through the API, GET
# /api/capacity must show the two workers holding different nodes, and each
# stand-in must have received exactly one pi request. The sessions, the LLM
# entry, the nodes, the stand-ins, the daemon and the scratch directory are all
# stopped or removed afterwards.
#
# TMUX and TMUX_TMPDIR are removed for everything this story starts. The daemon
# reaches tmux through a shim on its PATH that pins every call to a socket this
# story owns, so no other tmux server is used.
#
# Set TS817_KEEP=1 to keep the scratch directory for inspection.

unset TMUX TMUX_TMPDIR

TS817_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TS817_TMP="$(mktemp -d "${TMPDIR:-/tmp}/dw-ts817-XXXXXX")"
export DATAWATCH_TEST_DIR="${DATAWATCH_TEST_DIR:-$TS817_TMP}"
TEST_TOKEN="dw-ts817-token"

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

TS817_BINARY="${TEST_BINARY:-$TS817_REPO/bin/datawatch}"
TS817_BIN="$TS817_TMP/bin"
TS817_DATA="$TS817_TMP/data"
TS817_AGENT="$TS817_TMP/pi-agent"
TS817_CFG="$TS817_TMP/config.yaml"
TS817_SOCK="$TS817_TMP/tmux.sock"
TS817_LOG_A="$TS817_TMP/alpha.log"
TS817_LOG_B="$TS817_TMP/beta.log"
TS817_NODE_A="ts817-alpha"
TS817_NODE_B="ts817-beta"
# The LLM entry that lists both nodes is an ollama-kind entry: the admission
# gate looks up the model's provider ("ollama") among entries by kind. The
# pi-prompt backend the sessions start on is named by its own entry, which
# the admission reads as the backend name.
TS817_LLM="ts817-ollama"
TS817_BACKEND="pi-prompt"
TS817_MODEL="ollama/qwen3.8:27b"
TS817_SESSION_IDS=()
TS817_DAEMON_PID=""
TS817_FAKE_A_PID=""
TS817_FAKE_B_PID=""
TS817_FAILED=""
TS817_LEASE_MAP="{}"
TS817_NODES_SEEN=""
TS817_CODE=""
TS817_BODY=""

# ts817_fail <message> — record a failed check and keep going to cleanup.
ts817_fail() {
  echo "  detail [TS-820] $*"
  TS817_FAILED="${TS817_FAILED:+$TS817_FAILED; }$*"
}

# ts817_free_port <start> — first free loopback TCP port at or above start.
ts817_free_port() {
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

# ts817_req <METHOD> <path> [json-body] — sets TS817_CODE and TS817_BODY.
ts817_req() {
  local method="$1" path="$2" body="${3:-}" out
  if [[ -n "$body" ]]; then
    out=$(curl -sk --max-time 30 -X "$method" -H "Authorization: Bearer $TEST_TOKEN" \
      -H "Content-Type: application/json" -d "$body" -w '\n%{http_code}' "$TEST_BASE$path")
  else
    out=$(curl -sk --max-time 30 -X "$method" -H "Authorization: Bearer $TEST_TOKEN" \
      -w '\n%{http_code}' "$TEST_BASE$path")
  fi
  TS817_CODE="${out##*$'\n'}"
  TS817_BODY="${out%$'\n'*}"
}

# ts817_wait_until <seconds> <command...> — polls until the command succeeds.
ts817_wait_until() {
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

ts817_healthy() {
  curl -skf --max-time 3 "$TEST_BASE/api/health" >/dev/null 2>&1
}

ts817_fake_ready() {
  curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$1/api/ps"
}

# ts817_chat_count <log> — number of pi requests (POST /v1/chat/completions) logged.
ts817_chat_count() {
  awk '$0 == "POST /v1/chat/completions" { n++ } END { print n + 0 }' "$1" 2>/dev/null
}

# ts817_request_count <log> — every request a stand-in has logged.
ts817_request_count() {
  awk 'NF { n++ } END { print n + 0 }' "$1" 2>/dev/null
}

ts817_both_chat_seen() {
  (($(ts817_chat_count "$TS817_LOG_A") >= 1 && $(ts817_chat_count "$TS817_LOG_B") >= 1))
}

ts817_pools_ready() {
  ts817_req GET /api/capacity
  [[ "$(printf '%s' "$TS817_BODY" | python3 -I -c '
import json, sys
d = json.load(sys.stdin)
lim = {p.get("name"): p.get("limit") for p in d.get("pools") or []}
print(1 if lim.get("node:'"$TS817_NODE_A"'") == 1 and lim.get("node:'"$TS817_NODE_B"'") == 1 else 0)
' 2>/dev/null)" == 1 ]]
}

# ts817_lease_map — sets TS817_LEASE_MAP to {session id: node} for the TS-820 sessions.
ts817_lease_map() {
  ts817_req GET /api/capacity
  printf '%s' "$TS817_BODY" > "$TS817_TMP/capacity.json"
  TS817_LEASE_MAP=$(printf '%s' "$TS817_BODY" | python3 -I -c '
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
' "${TS817_SESSION_IDS[@]}" 2>/dev/null)
}

ts817_leases_ready() {
  ts817_lease_map
  [[ "$(printf '%s' "$TS817_LEASE_MAP" | python3 -I -c '
import json, sys
m = json.load(sys.stdin)
print(1 if len(m) == 2 and all(m.values()) else 0)
' 2>/dev/null)" == 1 ]]
}

ts817_write_config() {
  local tmpl="$TS817_REPO/testdata/datawatch.yaml"
  sed \
    -e "s|data_dir: /data|data_dir: $TS817_DATA|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $TS817_DATA/projects|g" \
    -e "s|port: 8080|port: $TS817_HTTP|g" \
    -e "s|tls_port: 8443|tls_port: $TS817_TLS|g" \
    -e "s|sse_port: 9090|sse_port: $TS817_MCP|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TEST_TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$TS817_DNS\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$TS817_WEBHOOK|g" \
    -e '/^ollama:/,/^$/d' \
    "$tmpl" > "$TS817_CFG" || return 1
  # The legacy ollama block is migrated into a kind-ollama entry linked to a
  # template node. Capacity keeps one entry per kind, so that migrated entry
  # would shadow the story's own kind-ollama entry; the block is left out.
  # The pi-prompt launch resolves its binary from this block; "pi" is found on
  # the daemon's PATH, which starts with the story's bin directory.
  cat >> "$TS817_CFG" <<'PI_BLOCK'

pi:
  enabled: true
  binary: pi
PI_BLOCK
}

# ts817_write_fakes — the fake pi, the tmux shim and the fake Ollama server.
ts817_write_fakes() {
  mkdir -p "$TS817_BIN" "$TS817_AGENT" "$TS817_DATA"
  local real_tmux
  real_tmux="$(command -v tmux)" || return 1

  # Source provider table the per-session agent directory is built from. Its
  # baseUrl points at a closed port, so a launch that ignored the override
  # would fail rather than reach a stand-in.
  cat > "$TS817_AGENT/models.json" <<'MODELS'
{"providers":{"ollama":{"api":"openai-completions","apiKey":"ollama","baseUrl":"http://127.0.0.1:9/v1","models":[{"id":"qwen3.8:27b"}]}}}
MODELS

  cat > "$TS817_BIN/pi" <<'FAKE_PI'
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
  chmod +x "$TS817_BIN/pi"

  # tmux shim: every tmux call the daemon makes goes to the story's own socket.
  cat > "$TS817_BIN/tmux" <<SHIM
#!/bin/sh
exec "$real_tmux" -S "$TS817_SOCK" "\$@"
SHIM
  chmod +x "$TS817_BIN/tmux"

  cat > "$TS817_TMP/fake_ollama.py" <<'FAKE_OLLAMA'
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

ts817_start_daemon() {
  # Shared lifecycle library first, so the EXIT/INT/TERM/HUP traps are in
  # place before the daemon forks (a no-op under the e2e runner, which has
  # already loaded the library and its traps with shared TD_STATE_DIR).
  story_daemon_lifecycle
  # Register this story's own cleanup — its sessions, LLM entries, Ollama
  # stand-ins and scratch — as the hook the shared traps run after they end
  # the recorded daemons, on failure and interrupt exits. The body's
  # ts817_cleanup is idempotent, so running it a second time from a trap is
  # a no-op (its steps check for what they remove; the PIDs are cleared
  # after first use).
  export DW_STORY_CLEANUP=ts817_cleanup
  # The daemon's own temp files (tooling directories, per-session pi agent
  # directories) land in the story's scratch area, so cleanup removes them too.
  mkdir -p "$TS817_TMP/daemon-tmp"
  env -u TMUX -u TMUX_TMPDIR PATH="$TS817_BIN:$PATH" PI_CODING_AGENT_DIR="$TS817_AGENT" \
    TMPDIR="$TS817_TMP/daemon-tmp" \
    "$TS817_BINARY" start --foreground --config "$TS817_CFG" >> "$TS817_TMP/daemon.log" 2>&1 &
  TS817_DAEMON_PID=$!
  # Record the daemon (PID + config-path marker) in the shared library and
  # set its API/token so teardown destroys its sessions through its own API
  # before signalling the recorded PID, on every exit path.
  td_track_pid "$TS817_DAEMON_PID" "$TS817_CFG"
  td_set_api "$TS817_DAEMON_PID" "$TEST_BASE" "$TEST_TOKEN"
}

# ts817_end_pid <pid> <marker> — end one process this story started, by the
# recorded PID only (marker-checked). Returns 0 when it is gone, nonzero when
# it may still be running; a PID that is already gone needs no signal.
ts817_end_pid() {
  local pid="$1" marker="$2"
  [[ -n "$pid" ]] || return 0
  td_alive "$pid" || return 0
  dw_terminate_pid "$pid" "$marker"
}

ts817_stop_daemon() {
  [[ -n "$TS817_DAEMON_PID" ]] || return 0
  # End the daemon through its recorded PID (sessions first via its own API,
  # then TERM/KILL only while the live cmdline still carries the recorded
  # marker) — the shared traps do the same on failure and interrupt exits.
  # The PID is forgotten only once the daemon is really gone: a daemon that
  # survives keeps the scratch folder in place and is reported.
  if ts817_end_pid "$TS817_DAEMON_PID" "$TS817_CFG"; then
    TS817_DAEMON_PID=""
    return 0
  fi
  ts817_fail "sandbox daemon $TS817_DAEMON_PID could not be ended; scratch folder $TS817_TMP kept"
  return 1
}

# ts817_run — the story body. Every failure is recorded by ts817_fail and the
# function returns, so ts817_cleanup always runs.
ts817_run() {
  if [[ ! -x "$TS817_BINARY" ]]; then
    skip "built binary not found at $TS817_BINARY"
    return 0
  fi
  if ! command -v tmux >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
    skip "tmux, curl and python3 are required"
    return 0
  fi

  # Free sandbox ports; the template's production ports (8080/8443) are never chosen.
  TS817_HTTP="$(ts817_free_port 18080)"
  TS817_TLS="$(ts817_free_port $((TS817_HTTP + 1)))"
  TS817_MCP="$(ts817_free_port $((TS817_TLS + 1)))"
  TS817_WEBHOOK="$(ts817_free_port 19080)"
  TS817_DNS="$(ts817_free_port 19053)"
  local port_a port_b
  port_a="$(ts817_free_port 19200)"
  port_b="$(ts817_free_port $((port_a + 1)))"
  TEST_BASE="https://127.0.0.1:$TS817_TLS"

  ts817_write_fakes || { ts817_fail "could not write fakes"; return 0; }
  ts817_write_config || { ts817_fail "could not write sandbox config"; return 0; }

  ts817_start_daemon
  if ! ts817_wait_until 40 ts817_healthy; then
    ts817_fail "sandbox daemon did not answer /api/health; daemon.log tail: $(tail -n 5 "$TS817_TMP/daemon.log" 2>/dev/null | tr '\n' ' ')"
    return 0
  fi

  # Two loopback Ollama stand-ins. The readiness probe is logged, so reset the
  # logs before the test traffic starts.
  env -u TMUX -u TMUX_TMPDIR python3 -I "$TS817_TMP/fake_ollama.py" "$port_a" "$TS817_NODE_A" "$TS817_LOG_A" >/dev/null 2>&1 &
  TS817_FAKE_A_PID=$!
  env -u TMUX -u TMUX_TMPDIR python3 -I "$TS817_TMP/fake_ollama.py" "$port_b" "$TS817_NODE_B" "$TS817_LOG_B" >/dev/null 2>&1 &
  TS817_FAKE_B_PID=$!
  # Record the stand-ins in the shared library state so the shared
  # EXIT/INT/TERM/HUP traps end them (their recorded PID, marker-checked
  # against the fake_ollama.py path still in their live cmdline) on every
  # exit path — not only the body's ts817_cleanup.
  declare -F td_track_pid >/dev/null 2>&1 && {
    td_track_pid "$TS817_FAKE_A_PID" "$TS817_TMP/fake_ollama.py"
    td_track_pid "$TS817_FAKE_B_PID" "$TS817_TMP/fake_ollama.py"
  } || true
  if ! ts817_wait_until 20 ts817_fake_ready "$port_a" || ! ts817_wait_until 20 ts817_fake_ready "$port_b"; then
    ts817_fail "loopback Ollama stand-ins did not start on $port_a / $port_b"
    return 0
  fi
  : > "$TS817_LOG_A"
  : > "$TS817_LOG_B"

  # Two compute nodes with different scheduling priorities; one slot each, so
  # the second worker has to take the other node.
  ts817_req POST /api/compute/nodes "{\"name\":\"$TS817_NODE_A\",\"kind\":\"ollama\",\"address\":\"http://127.0.0.1:$port_a\",\"scheduling_priority\":90,\"session_slots\":1}"
  [[ "$TS817_CODE" == 2* ]] || { ts817_fail "node $TS817_NODE_A not registered (HTTP $TS817_CODE): $TS817_BODY"; return 0; }
  ts817_req POST /api/compute/nodes "{\"name\":\"$TS817_NODE_B\",\"kind\":\"ollama\",\"address\":\"http://127.0.0.1:$port_b\",\"scheduling_priority\":50,\"session_slots\":1}"
  [[ "$TS817_CODE" == 2* ]] || { ts817_fail "node $TS817_NODE_B not registered (HTTP $TS817_CODE): $TS817_BODY"; return 0; }

  # One LLM entry that lists both nodes, plus the pi-prompt backend entry.
  ts817_req POST /api/llms "{\"name\":\"$TS817_LLM\",\"kind\":\"ollama\",\"compute_nodes\":[\"$TS817_NODE_A\",\"$TS817_NODE_B\"]}"
  [[ "$TS817_CODE" == 2* ]] || { ts817_fail "LLM entry not registered (HTTP $TS817_CODE): $TS817_BODY"; return 0; }
  # The in-flight cap is two so the entry is never the limit; the node slots are.
  ts817_req POST /api/llms "{\"name\":\"$TS817_BACKEND\",\"kind\":\"pi-prompt\",\"max_inflight\":2}"
  [[ "$TS817_CODE" == 2* ]] || { ts817_fail "pi-prompt backend entry not registered (HTTP $TS817_CODE): $TS817_BODY"; return 0; }

  if ! ts817_wait_until 60 ts817_pools_ready; then
    ts817_req GET /api/capacity
    printf '%s' "$TS817_BODY" > "$TS817_TMP/capacity-no-limits.json"
    ts817_fail "node slot limits of 1 never reached the capacity ledger (snapshot: $TS817_TMP/capacity-no-limits.json)"
    return 0
  fi

  local project sid i
  for i in 1 2; do
    project="$TS817_TMP/project-$i"
    mkdir -p "$project"
    ts817_req POST /api/sessions/start "{\"task\":\"ts817 pi-prompt worker $i\",\"project_dir\":\"$project\",\"backend\":\"$TS817_BACKEND\",\"model\":\"$TS817_MODEL\",\"name\":\"ts817-worker-$i\"}"
    if [[ "$TS817_CODE" != 2* ]]; then
      ts817_fail "session $i did not start (HTTP $TS817_CODE): $TS817_BODY"
      return 0
    fi
    sid="$(printf '%s' "$TS817_BODY" | python3 -I -c 'import json, sys; print(json.load(sys.stdin).get("id", ""))' 2>/dev/null)"
    if [[ -z "$sid" ]]; then
      ts817_fail "session $i response had no id: $TS817_BODY"
      return 0
    fi
    TS817_SESSION_IDS+=("$sid")
  done

  if ! ts817_wait_until 120 ts817_both_chat_seen; then
    ts817_fail "pi requests did not reach both stand-ins (alpha=$(ts817_chat_count "$TS817_LOG_A") beta=$(ts817_chat_count "$TS817_LOG_B"))"
    return 0
  fi
  # Let any extra request arrive before counting. Every request a stand-in
  # receives is counted, not only the pi ones.
  sleep 5
  local count_a count_b
  count_a="$(ts817_request_count "$TS817_LOG_A")"
  count_b="$(ts817_request_count "$TS817_LOG_B")"
  if [[ "$count_a" != 1 || "$count_b" != 1 ]]; then
    ts817_fail "each stand-in should receive exactly one request; alpha=$count_a beta=$count_b ($(tr '\n' ';' < "$TS817_LOG_A") $(tr '\n' ';' < "$TS817_LOG_B"))"
  fi

  if ! ts817_wait_until 30 ts817_leases_ready; then
    ts817_fail "capacity leases for both sessions not found: $TS817_LEASE_MAP"
    return 0
  fi
  local verdict
  verdict="$(printf '%s' "$TS817_LEASE_MAP" | python3 -I -c '
import json, sys
m = json.load(sys.stdin)
ids = sys.argv[1:]
nodes = [m.get(i, "") for i in ids]
want = {"'"$TS817_NODE_A"'", "'"$TS817_NODE_B"'"}
print("ok" if all(nodes) and len(set(nodes)) == len(nodes) and set(nodes) == want else "bad " + json.dumps(m))
' "${TS817_SESSION_IDS[@]}" 2>/dev/null)"
  if [[ "$verdict" != ok ]]; then
    ts817_fail "the two workers do not hold different nodes: $verdict"
  fi
  TS817_NODES_SEEN="$(printf '%s' "$TS817_LEASE_MAP" | python3 -I -c 'import json,sys; print(", ".join(sorted(set(json.load(sys.stdin).values()))))' 2>/dev/null)"
}

# ts817_cleanup — removes what the story created. Each step runs only if the
# thing it removes was created here.
ts817_cleanup() {
  local sid
  if [[ -n "$TS817_DAEMON_PID" ]]; then
    for sid in "${TS817_SESSION_IDS[@]}"; do
      ts817_req POST /api/sessions/kill "{\"id\":\"$sid\"}"
    done
    ts817_req DELETE "/api/llms/$TS817_LLM"
    ts817_req DELETE "/api/llms/$TS817_BACKEND"
    ts817_req DELETE "/api/compute/nodes/$TS817_NODE_A"
    ts817_req DELETE "/api/compute/nodes/$TS817_NODE_B"
    ts817_req GET /api/llms
    if [[ "$TS817_BODY" == *"\"name\":\"$TS817_LLM\""* || "$TS817_BODY" == *"\"name\":\"$TS817_BACKEND\""* ]]; then
      ts817_fail "LLM entries $TS817_LLM / $TS817_BACKEND still listed after cleanup"
    fi
    ts817_req GET /api/compute/nodes
    if [[ "$TS817_BODY" == *"$TS817_NODE_A"* || "$TS817_BODY" == *"$TS817_NODE_B"* ]]; then
      ts817_fail "compute nodes still listed after cleanup"
    fi
  fi
  # Scratch is removed only when every process this story started is gone.
  local gone=1
  ts817_stop_daemon || gone=0
  # End the fake ollama helpers by the PIDs this script recorded, marker-
  # checked against their live cmdline, before the scratch tree is removed.
  if ts817_end_pid "$TS817_FAKE_A_PID" "$TS817_TMP/fake_ollama.py"; then
    TS817_FAKE_A_PID=""
  else
    ts817_fail "stand-in $TS817_FAKE_A_PID could not be ended; scratch folder $TS817_TMP kept"
    gone=0
  fi
  if ts817_end_pid "$TS817_FAKE_B_PID" "$TS817_TMP/fake_ollama.py"; then
    TS817_FAKE_B_PID=""
  else
    ts817_fail "stand-in $TS817_FAKE_B_PID could not be ended; scratch folder $TS817_TMP kept"
    gone=0
  fi
  # Only the story's own socket is probed. A stale socket file is harmless, so
  # the check is whether any session is still listed on it after cleanup.
  local sock="$TS817_SOCK" left
  if [[ -S "$sock" ]]; then
    left="$("$(command -v tmux)" -S "$sock" list-sessions 2>/dev/null)"
    if [[ -n "$left" ]]; then
      ts817_fail "tmux sessions still listed on the story socket after cleanup"
    fi
  fi
  if [[ -z "${TS817_KEEP:-}" && "$gone" == 1 ]]; then
    rm -rf "$TS817_TMP"
  fi
}

ts817_main() {
  story_preflight "surface:api feature:capacity" || return 0
  CURRENT_STORY="TS-820"
  ts817_run
  ts817_cleanup
  if [[ -n "$TS817_FAILED" ]]; then
    ko "two pi-prompt workers on compute nodes: $TS817_FAILED"
  elif [[ -n "$TS817_NODES_SEEN" ]]; then
    ok "two pi-prompt workers hold different nodes ($TS817_NODES_SEEN); each stand-in received one pi request"
  else
    skip "story ended without a verdict"
  fi
}

ts817_main

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  [[ "$RESULT" == fail ]] && exit 1
  exit 0
fi
