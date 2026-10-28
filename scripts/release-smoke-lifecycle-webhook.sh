#!/usr/bin/env bash
# Standalone smoke for the lifecycle outbound webhook sink and peer forwarding.
#
# Drives a live sandbox daemon over its REST API, with a throwaway HTTP
# receiver on 127.0.0.1, then runs the in-process forwarding tests:
#
#   1. start the receiver (python3 -I, 127.0.0.1, ephemeral port)
#   2. store a throwaway signing secret in the daemon's vault
#   3. enable the webhook sink, with the receiver's host:port allow-listed
#   4. create a webhook subscription and test-fire it: exactly one signed POST
#      arrives, the signature verifies, the envelope parses, and the audit log
#      holds the delivery record
#   5. negatives: a host that is not allow-listed and the cloud metadata
#      address are refused at creation (400); with the allow-list emptied, a
#      test-fire makes no request at all
#   6. retries: two 500s then a 200 give three attempts with growing gaps and a
#      delivered final state
#   7. failure: the receiver is stopped, the test-fire fails, and an alert in
#      GET /api/alerts plus a final failure audit record are present
#   8. forwarding: the TestForwardedEvents* and TestForward* Go tests
#
# Two modes:
#   fresh sandbox (default, DATAWATCH_BASE unset): builds the daemon from this
#     tree into the scratch directory, starts it the way scripts/run-tests.sh
#     does (config from testdata/datawatch.yaml, a data directory and a home
#     directory under the scratch directory, free ports, TMUX and TMUX_TMPDIR
#     unset) and ends it by the exact PID it started. This is the mode that
#     can validate the feature; it never touches a daemon that is already
#     running.
#   existing sandbox (DATAWATCH_BASE set, as the story runner does): drives
#     that daemon, which must come from a build of this tree.
#
# Environment:
#   DATAWATCH_BASE              sandbox base URL; setting it selects the existing-sandbox mode
#   DATAWATCH_TOKEN             bearer token (default: the sandbox test token)
#   DATAWATCH_SMOKE_KEEP_WORK=1 keep the scratch directory for inspection
#   DATAWATCH_SMOKE_EVIDENCE_DIR  directory that receives the forwarding test log
#                                 and an environment record (for docs/testing/runs)
#
# Exit code: 0 = pass, 1 = real failure, 2 = skip. A skip only happens in the
# existing-sandbox mode (daemon unreachable, or built before the webhook
# feature and so without webhook settings); the fresh-sandbox mode never
# skips: a build or start problem is a failure.
# The daemon is reached over HTTPS only (the sandbox certificate is
# self-signed, so curl runs with -k). The webhook sink and the forwarding ship
# disabled; this smoke enables the webhook sink on the sandbox for its run and
# restores the original values on exit.

if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Shared daemon lifecycle (scripts/test-daemon-lib.sh): the fresh-sandbox
# daemon started below is recorded at start and ended on EVERY exit path
# (normal, failure, INT, TERM, HUP) — first its live sessions through the
# daemon's own API, then the exact PID recorded at start (marker-verified,
# waited for) — and only after that runs the cleanup() below, so the
# scratch directory is never deleted while the daemon is still running.
# Existing-sandbox mode (DATAWATCH_BASE set) records nothing, so the
# daemon it drives is never touched.
source "$REPO_ROOT/scripts/test-daemon-lib.sh"
TD_EXTRA_CLEANUP=cleanup
td_install_traps "$@"

FRESH=0
[[ -z "${DATAWATCH_BASE:-}" ]] && FRESH=1
BASE="${DATAWATCH_BASE:-}"
TOKEN="${DATAWATCH_TOKEN:-dw-test-token-12345}"
SECRET_NAME="lifecycle-smoke-signing"
# Obviously fake, and only ever stored for the length of this run.
SECRET_VALUE="fake-smoke-value-not-a-real-secret"
ALERT_TITLE="Lifecycle webhook delivery failed"

WORK="$(mktemp -d)"
RECV_PID=""
RECV_HOSTPORT=""
SANDBOX_PID=""
EVIDENCE_DIR="${DATAWATCH_SMOKE_EVIDENCE_DIR:-}"
ORIG_JSON=""
SUB_ID=""
SUB_IDS=()
SECRET_MADE=0
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

# jfield EXPR — reads JSON on stdin and prints EXPR (booleans in lowercase).
jfield() {
  python3 -I -c "import json,sys; d=json.load(sys.stdin); v=$1; print(str(v).lower() if isinstance(v, bool) else v)"
}

# check MODE ARGS... — the verifier in $WORK/check.py (stdin for audit/alert modes).
check() {
  SMOKE_SECRET="$SECRET_VALUE" SMOKE_HOWTO="$REPO_ROOT/docs/howto/automaton-lifecycle-webhooks.md" \
    python3 -I "$WORK/check.py" "$WORK" "$@"
}

count_requests() {
  find "$WORK" -maxdepth 1 -name 'req-*.json' | wc -l | tr -d ' '
}

SUB_COUNT=0
count_subscriptions() { # sets SUB_COUNT
  api GET /api/autonomous/subscriptions
  [[ "$HTTP_CODE" == 200 ]] || die "GET /api/autonomous/subscriptions returned $HTTP_CODE"
  SUB_COUNT=$(printf '%s' "$API_BODY" | jfield "len(d)")
}

put_config() {
  api PUT /api/config "$1"
  [[ "$HTTP_CODE" == 200 ]] || die "PUT /api/config returned $HTTP_CODE: $API_BODY"
}

create_sub() { # create_sub TARGET — sets HTTP_CODE, API_BODY
  api POST /api/autonomous/subscriptions \
    "$(printf '{"filter":{"kinds":["task.session_started"]},"sink":{"type":"webhook","target":"%s","secret_ref":"${secret:%s}"}}' "$1" "$SECRET_NAME")"
}

# test_fire — POST the subscription's test endpoint; sets HTTP_CODE, API_BODY.
test_fire() {
  api POST "/api/autonomous/subscriptions/$SUB_ID/test"
  [[ "$HTTP_CODE" == 200 ]] || die "test-fire returned $HTTP_CODE: $API_BODY"
}

start_receiver() {
  rm -f "$WORK/port"
  python3 -I "$WORK/receiver.py" "$WORK" > "$WORK/receiver.log" 2>&1 &
  RECV_PID=$!
  local i
  for i in $(seq 1 100); do
    [[ -s "$WORK/port" ]] && break
    kill -0 "$RECV_PID" 2>/dev/null || die "receiver exited: $(cat "$WORK/receiver.log")"
    sleep 0.1
  done
  [[ -s "$WORK/port" ]] || die "receiver did not report a port"
  RECV_HOSTPORT="127.0.0.1:$(cat "$WORK/port")"
}

stop_receiver() {
  if [[ -n "$RECV_PID" ]]; then
    # Signal the pid only while it is still identifiable as this session's
    # receiver (marker in /proc/<pid>/cmdline) — never by pid alone: a
    # recycled pid may be an unrelated process.
    if [[ -d "/proc/$RECV_PID" ]] && \
       tr '\0' ' ' < "/proc/$RECV_PID/cmdline" 2>/dev/null | \
       grep -qF 'webhook-receiver'; then
      kill "$RECV_PID" 2>/dev/null || true
      wait "$RECV_PID" 2>/dev/null || true
    fi
    RECV_PID=""
  fi
}

# free_ports N — N distinct free loopback TCP ports, one per line.
free_ports() {
  python3 -I -c '
import socket, sys
socks = [socket.socket() for _ in range(int(sys.argv[1]))]
for s in socks:
    s.bind(("127.0.0.1", 0))
for s in socks:
    print(s.getsockname()[1])
' "$1"
}

# start_sandbox — build this tree and start a throwaway daemon (fresh mode).
start_sandbox() {
  local sbx="$WORK/sandbox" bin="$WORK/datawatch" tpl="$REPO_ROOT/testdata/datawatch.yaml"
  [[ -f "$tpl" ]] || die "sandbox config template missing: testdata/datawatch.yaml"
  step "fresh sandbox: build the daemon from this tree"
  local commit dirty
  commit=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)
  dirty=$(git -C "$REPO_ROOT" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
  echo "tree: commit $commit, $dirty uncommitted path(s)"
  (cd "$REPO_ROOT" && go build -o "$bin" ./cmd/datawatch/) > "$WORK/build.log" 2>&1 \
    || { cat "$WORK/build.log"; die "go build of this tree failed"; }
  pass "built $bin"

  local ports p_http p_tls p_mcp p_dns p_hook p_ntfy
  ports=$(free_ports 6) || die "could not pick free ports"
  { read -r p_http; read -r p_tls; read -r p_mcp; read -r p_dns; read -r p_hook; read -r p_ntfy; } <<<"$ports"
  mkdir -p "$sbx/data/projects" "$sbx/home"
  sed \
    -e "s|data_dir: /data|data_dir: $sbx/data|g" \
    -e "s|default_project_dir: /data/projects|default_project_dir: $sbx/data/projects|g" \
    -e "s|port: 8080|port: $p_http|g" \
    -e "s|tls_port: 8443|tls_port: $p_tls|g" \
    -e "s|sse_port: 9090|sse_port: $p_mcp|g" \
    -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
    -e "s|token: \"\"|token: \"$TOKEN\"|g" \
    -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
    -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$p_dns\"|g" \
    -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$p_hook|g" \
    -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$p_ntfy|g" \
    "$tpl" > "$sbx/config.yaml"
  cp "$sbx/config.yaml" "$sbx/data/config.yaml"

  # Only the daemon gets the scratch home directory, so it cannot read or
  # write any real data directory.
  HOME="$sbx/home" env -u TMUX -u TMUX_TMPDIR "$bin" start --foreground --config "$sbx/config.yaml" \
    > "$sbx/daemon.log" 2>&1 &
  SANDBOX_PID=$!
  # Record it so every exit path (including dying mid health-wait below)
  # ends this daemon: marker = the config path, present in the daemon's
  # cmdline for its whole life (a recycled PID is left alone).
  td_track_pid "$SANDBOX_PID" "$sbx/config.yaml" \
    || die "could not record sandbox daemon pid $SANDBOX_PID for cleanup"
  BASE="https://127.0.0.1:$p_tls"
  # Remember its API: on stop the shared library ends this daemon's live
  # sessions through its own API before signalling the PID.
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
  pass "sandbox daemon pid $SANDBOX_PID healthy at $BASE (built from commit $commit)"
}

stop_sandbox() {
  # End the recorded sandbox daemon: its live sessions via its own API, then
  # the exact PID recorded at start (marker-verified), waited for.
  if [[ -n "$SANDBOX_PID" ]]; then
    td_stop_one "$SANDBOX_PID" \
      || echo "WARN: sandbox daemon pid $SANDBOX_PID still running" >&2
    echo "stopped: sandbox daemon pid $SANDBOX_PID"
    SANDBOX_PID=""
  fi
}

cleanup() {
  # Runs from the library's TD_EXTRA_CLEANUP hook (subshell): it must not
  # `exit` — the shared handler owns the script's status (TD_EXIT_STATUS)
  # and still has to finish after this hook. API cleanup below is
  # best-effort: in fresh-sandbox mode the subscription and secret die
  # with the sandbox anyway; in existing-sandbox mode a failed restore is
  # reported, not fatal.
  stop_receiver
  if [[ -n "$SANDBOX_PID" ]]; then
    # Fresh-sandbox mode: this cleanup() runs from the library's
    # TD_EXTRA_CLEANUP hook, i.e. AFTER td_stop_all has already ended the
    # recorded daemon — its live sessions via its own API, then the exact
    # PID recorded at start, marker-verified and waited for — so the stop
    # here is belt-and-braces for a no-op. The daemon is a throwaway; its
    # config, data dir and home dir all live under $WORK, which is deleted
    # below. Nothing in it needs restoring.
    td_stop_one "$SANDBOX_PID" \
      || echo "WARN: sandbox daemon pid $SANDBOX_PID still running" >&2
    SANDBOX_PID=""
  else
    local id
    for id in ${SUB_IDS[@]+"${SUB_IDS[@]}"}; do
      api DELETE "/api/autonomous/subscriptions/$id" >/dev/null 2>&1
    done
    if [[ -n "$ORIG_JSON" ]]; then
      api PUT /api/config "$ORIG_JSON" >/dev/null 2>&1
      if [[ "$HTTP_CODE" == 200 ]]; then
        echo "restored: autonomous.notify.webhook.* to the values found at start"
      else
        echo "WARN: restoring autonomous.notify.webhook.* returned $HTTP_CODE; set the keys back by hand"
      fi
    fi
    if [[ "$SECRET_MADE" == 1 ]]; then
      api DELETE "/api/secrets/$SECRET_NAME" >/dev/null 2>&1
      echo "removed: throwaway signing secret"
    fi
  fi
  if [[ "${DATAWATCH_SMOKE_KEEP_WORK:-0}" == 1 ]]; then
    echo "kept scratch directory: $WORK"
  else
    rm -rf "$WORK"
  fi
}
# The EXIT/INT/TERM/HUP traps were installed by the shared daemon
# lifecycle library at the top of this script (TD_EXTRA_CLEANUP=cleanup):
# on every exit path it ends the recorded fresh-sandbox daemon — first its
# live sessions through the daemon's own API, then the exact PID it
# started, marker-verified and waited for — and only after that runs
# cleanup() below. Existing-sandbox mode records nothing, so the daemon it
# drives is never touched.

# ---------------------------------------------------------------------------
# Verifier and receiver, written into the scratch directory.
# ---------------------------------------------------------------------------
cat > "$WORK/receiver.py" <<'PY'
# Throwaway receiver: records every POST and answers with the next queued code.
import http.server
import json
import os
import sys
import threading
import time

WORK = sys.argv[1]
LOCK = threading.Lock()
COUNT = [0]


def next_code():
    path = os.path.join(WORK, "codes")
    try:
        with open(path) as f:
            queue = f.read().split()
    except FileNotFoundError:
        return 200
    if not queue:
        return 200
    code = int(queue.pop(0))
    with open(path, "w") as f:
        f.write(" ".join(queue))
    return code


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length)
        with LOCK:
            COUNT[0] += 1
            n = COUNT[0]
            code = next_code()
            base = os.path.join(WORK, "req-%03d" % n)
            with open(base + ".body", "wb") as f:
                f.write(body)
            with open(base + ".json", "w") as f:
                json.dump({
                    "ts": time.time(),
                    "path": self.path,
                    "code": code,
                    "headers": dict(self.headers.items()),
                }, f)
        reply = b"ok\n" if code < 400 else b"rejected\n"
        self.send_response(code)
        self.send_header("Content-Length", str(len(reply)))
        self.end_headers()
        self.wfile.write(reply)

    def log_message(self, *args):
        pass


class Server(http.server.ThreadingHTTPServer):
    daemon_threads = True


server = Server(("127.0.0.1", 0), Handler)
with open(os.path.join(WORK, "port"), "w") as f:
    f.write(str(server.server_address[1]))
server.serve_forever()
PY

cat > "$WORK/check.py" <<'PY'
# Verifier for the webhook smoke. Modes:
#   signature N SUB_ID   request N verifies, parses and names SUB_ID (env SMOKE_SECRET)
#   snippet N            the receiver snippet from the how-to (env SMOKE_HOWTO) accepts request N
#                        and rejects the same request with one byte of the body changed
#   delivery N           print the X-Datawatch-Delivery id of request N
#   gaps FIRST LAST      the gaps between requests FIRST..LAST grow
#   same FIRST LAST      the requests share one delivery id; codes are 500,500,200
#   latest-delivery ACTION SUB_ID   delivery id of the newest matching audit entry (stdin)
#   audit DELIVERY ACTION COUNTS    outcome counts for a delivery, e.g. retry=2,delivered=1 (stdin)
#   alert SUB_ID ATTEMPTS  a failure alert names SUB_ID and a drop after ATTEMPTS (stdin: GET /api/alerts)
import hashlib
import hmac
import json
import os
import sys
import time

WORK = sys.argv[1]
MODE = sys.argv[2]
ALERT_TITLE = "Lifecycle webhook delivery failed"
TOLERANCE = 300


def fail(msg):
    print("FAIL: " + msg)
    sys.exit(1)


def load(n):
    base = os.path.join(WORK, "req-%03d" % n)
    with open(base + ".json") as f:
        meta = json.load(f)
    with open(base + ".body", "rb") as f:
        body = f.read()
    return meta, body


def header(meta, name):
    for k, v in meta["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


if MODE == "signature":
    n, sub_id = int(sys.argv[3]), sys.argv[4]
    secret = os.environ["SMOKE_SECRET"].encode()
    meta, body = load(n)
    sig = header(meta, "X-Datawatch-Signature")
    if not sig:
        fail("request %d carries no signature header" % n)
    parts = dict(p.split("=", 1) for p in sig.split(",") if "=" in p)
    t, v1 = parts.get("t", ""), parts.get("v1", "")
    if not t.isdigit() or not v1:
        fail("request %d signature header is malformed" % n)
    want = hmac.new(secret, t.encode() + b"." + body, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(want, v1):
        fail("request %d signature does not verify" % n)
    if abs(time.time() - int(t)) > TOLERANCE:
        fail("request %d signature timestamp is outside the replay window" % n)
    if not header(meta, "X-Datawatch-Event"):
        fail("request %d carries no event header" % n)
    if not header(meta, "X-Datawatch-Delivery"):
        fail("request %d carries no delivery header" % n)
    env = json.loads(body)
    if env.get("version") != 1:
        fail("envelope version is %r, want 1" % env.get("version"))
    if env.get("subscription_id") != sub_id:
        fail("envelope names subscription %r, want %s" % (env.get("subscription_id"), sub_id))
    if env.get("delivery_id") != header(meta, "X-Datawatch-Delivery"):
        fail("envelope delivery_id does not match the delivery header")
    if (env.get("event") or {}).get("test") is not True:
        fail("envelope event is not the labelled test event")
    print("PASS: request %d signature verifies, envelope parses, event is the labelled test" % n)

elif MODE == "snippet":
    n = int(sys.argv[3])
    secret = os.environ["SMOKE_SECRET"].encode()
    with open(os.environ["SMOKE_HOWTO"]) as f:
        text = f.read()
    marker = text.index("This Python snippet is a complete check")
    start = text.index("```python\n", marker) + len("```python\n")
    code = text[start:text.index("```", start)]
    ns = {}
    exec(compile(code, "howto-snippet", "exec"), ns)
    meta, body = load(n)
    sig = header(meta, "X-Datawatch-Signature")
    if ns["verify"](secret, sig, body) is not True:
        fail("the how-to snippet rejects the genuine request %d" % n)
    if ns["verify"](secret, sig, body + b" ") is not False:
        fail("the how-to snippet accepts a changed body")
    if ns["verify"](b"not-the-secret", sig, body) is not False:
        fail("the how-to snippet accepts the wrong secret")
    print("PASS: the how-to snippet accepts request %d, and rejects a changed body and a wrong secret" % n)

elif MODE == "delivery":
    meta, _ = load(int(sys.argv[3]))
    print(header(meta, "X-Datawatch-Delivery"))

elif MODE == "gaps":
    first, last = int(sys.argv[3]), int(sys.argv[4])
    ts = [load(n)[0]["ts"] for n in range(first, last + 1)]
    gaps = [b - a for a, b in zip(ts, ts[1:])]
    print("PASS: attempt gaps " + " ".join("%.2fs" % g for g in gaps))
    if len(gaps) < 2 or not all(gaps[i] < gaps[i + 1] for i in range(len(gaps) - 1)):
        fail("attempt gaps are not increasing")

elif MODE == "same":
    first, last = int(sys.argv[3]), int(sys.argv[4])
    metas = [load(n)[0] for n in range(first, last + 1)]
    ids = {header(m, "X-Datawatch-Delivery") for m in metas}
    if len(ids) != 1:
        fail("attempts carry %d delivery ids, want 1" % len(ids))
    codes = [m["code"] for m in metas]
    if codes != [500, 500, 200]:
        fail("receiver answered %s, want [500, 500, 200]" % codes)
    print("PASS: %d attempts share delivery %s; receiver answered %s" % (len(metas), ids.pop(), codes))

elif MODE == "latest-delivery":
    action, sub_id = sys.argv[3], sys.argv[4]
    entries = json.load(sys.stdin).get("entries") or []
    for e in entries:  # the audit query returns newest first
        d = e.get("details") or {}
        if e.get("action") == action and d.get("subscription_id") == sub_id:
            print(d.get("delivery_id", ""))
            sys.exit(0)
    fail("no %s audit entry for subscription %s" % (action, sub_id))

elif MODE == "audit":
    delivery, action, want = sys.argv[3], sys.argv[4], sys.argv[5]
    entries = json.load(sys.stdin).get("entries") or []
    got = {}
    for e in entries:
        d = e.get("details") or {}
        if e.get("action") == action and d.get("delivery_id") == delivery:
            o = d.get("outcome", "")
            got[o] = got.get(o, 0) + 1
    for pair in want.split(","):
        k, v = pair.split("=")
        if got.get(k, 0) != int(v):
            fail("audit %s for delivery %s: want %s=%s, got %s" % (action, delivery, k, v, got))
    print("PASS: audit %s for delivery %s: %s" % (action, delivery, got))

elif MODE == "clean":
    # The throwaway secret value must not appear in the API response on stdin.
    if os.environ["SMOKE_SECRET"] in sys.stdin.read():
        fail("the throwaway secret value appears in an API response")
    print("PASS: the throwaway secret value is absent from the API response")

elif MODE == "clean-requests":
    # Every request the receiver got (body and headers) must lack the secret value.
    secret = os.environ["SMOKE_SECRET"].encode()
    names = sorted(n for n in os.listdir(WORK) if n.startswith("req-"))
    if not names:
        fail("the receiver captured no requests to inspect")
    for name in names:
        with open(os.path.join(WORK, name), "rb") as f:
            if secret in f.read():
                fail("the throwaway secret value appears in receiver file %s" % name)
    print("PASS: the throwaway secret value is absent from %d receiver file(s)" % len(names))

elif MODE == "alert":
    sub_id, attempts = sys.argv[3], sys.argv[4]
    data = json.load(sys.stdin)
    alerts = data.get("alerts") if isinstance(data, dict) else data
    want = "%s: delivery to" % sub_id
    hits = [a for a in alerts or []
            if a.get("title") == ALERT_TITLE
            and want in (a.get("body") or "")
            and "dropped after %s attempt(s)" % attempts in a["body"]]
    if not hits:
        fail("no failure alert names subscription %s with a drop after %s attempt(s)" % (sub_id, attempts))
    print("PASS: failure alert names subscription %s after %s attempts (%d matching)" % (sub_id, attempts, len(hits)))

else:
    fail("unknown check mode %r" % MODE)
PY

# ---------------------------------------------------------------------------
# Steps
# ---------------------------------------------------------------------------
if [[ "$FRESH" == 1 ]]; then
  start_sandbox
fi
api GET /api/health
if [[ "$HTTP_CODE" != 200 ]]; then
  if [[ "$FRESH" == 1 ]]; then
    die "the fresh sandbox daemon at $BASE is not healthy (health returned $HTTP_CODE)"
  fi
  echo "SKIP: sandbox daemon unreachable at $BASE (health returned $HTTP_CODE)"
  exit 2
fi

api GET /api/config
[[ "$HTTP_CODE" == 200 ]] || die "GET /api/config returned $HTTP_CODE: check DATAWATCH_TOKEN"
# A daemon built before the webhook feature has no autonomous.notify.webhook
# section. It cannot run this smoke, so skip with a message rather than fail
# with a traceback; a daemon that is not JSON at all is a real failure.
WEBHOOK_CONFIG=$(printf '%s' "$API_BODY" | python3 -I -c '
import json, sys
try:
    w = json.load(sys.stdin)
except ValueError:
    print("invalid")
    sys.exit(0)
w = (((w.get("autonomous") or {}).get("notify") or {}).get("webhook"))
keys = ("enabled", "allow_hosts", "backoff_seconds")
print("present" if isinstance(w, dict) and all(k in w for k in keys) else "missing")
') || die "could not inspect GET /api/config"
case "$WEBHOOK_CONFIG" in
  present) ;;
  missing)
    if [[ "$FRESH" == 1 ]]; then
      die "the daemon built from this tree has no autonomous.notify.webhook settings in GET /api/config"
    fi
    echo "SKIP: the daemon at $BASE predates the lifecycle webhook feature (GET /api/config has no autonomous.notify.webhook settings); point DATAWATCH_BASE at a sandbox started from this build"
    exit 2
    ;;
  *) die "GET /api/config did not return JSON";;
esac
ORIG_JSON=$(printf '%s' "$API_BODY" | python3 -I -c '
import json, sys
w = json.load(sys.stdin)["autonomous"]["notify"]["webhook"]
print(json.dumps({
    "autonomous.notify.webhook.enabled": w["enabled"],
    "autonomous.notify.webhook.allow_hosts": w["allow_hosts"],
    "autonomous.notify.webhook.backoff_seconds": w["backoff_seconds"],
}))') || die "could not read the current webhook settings"
echo "start: sandbox $BASE, webhook settings $ORIG_JSON"
if [[ -n "$EVIDENCE_DIR" ]]; then
  mkdir -p "$EVIDENCE_DIR"
  api GET /api/health
  {
    echo "mode: $([[ "$FRESH" == 1 ]] && echo fresh-sandbox || echo existing-sandbox)"
    echo "base: $BASE"
    echo "tree commit: $(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
    echo "uncommitted paths: $(git -C "$REPO_ROOT" status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
    echo "health status: $(printf '%s' "$API_BODY" | jfield "d['status']")"
    echo "go: $(go version 2>/dev/null)"
    echo "os: $(uname -sr)"
    echo "webhook settings at start: $ORIG_JSON"
  } > "$EVIDENCE_DIR/environment.txt"
fi

step "step 1: start the throwaway receiver"
: > "$WORK/codes"
start_receiver
pass "receiver listening on $RECV_HOSTPORT (pid $RECV_PID)"

step "step 2: store the throwaway signing secret"
api DELETE "/api/secrets/$SECRET_NAME" >/dev/null
api POST /api/secrets "$(printf '{"name":"%s","value":"%s","description":"release smoke throwaway"}' "$SECRET_NAME" "$SECRET_VALUE")"
[[ "$HTTP_CODE" == 200 || "$HTTP_CODE" == 201 ]] || die "POST /api/secrets returned $HTTP_CODE: $API_BODY"
SECRET_MADE=1
pass "secret $SECRET_NAME stored"

step "step 3: enable the webhook sink with the receiver allow-listed"
# With the switch off a webhook subscription is refused, even for an
# allow-listed target.
put_config "$(printf '{"autonomous.notify.webhook.enabled":false,"autonomous.notify.webhook.allow_hosts":["%s"]}' "$RECV_HOSTPORT")"
count_subscriptions
n_subs=$SUB_COUNT
create_sub "http://$RECV_HOSTPORT/hook"
[[ "$HTTP_CODE" == 400 ]] || die "a webhook subscription was accepted while the sink is switched off (HTTP $HTTP_CODE)"
[[ "$API_BODY" == *"disabled"* ]] || die "the refusal while switched off does not say the sink is disabled: $API_BODY"
count_subscriptions
[[ "$SUB_COUNT" == "$n_subs" ]] || die "a subscription refused while switched off was stored"
pass "with the sink switched off, a webhook subscription is refused (400, says disabled)"
put_config "$(printf '{"autonomous.notify.webhook.enabled":true,"autonomous.notify.webhook.allow_hosts":["%s"]}' "$RECV_HOSTPORT")"
api GET /api/config
[[ "$(printf '%s' "$API_BODY" | jfield "d['autonomous']['notify']['webhook']['enabled']")" == "true" ]] || die "config GET does not show the sink enabled"
[[ "$(printf '%s' "$API_BODY" | jfield "d['autonomous']['notify']['webhook']['allow_hosts']==['$RECV_HOSTPORT']")" == "true" ]] || die "config GET does not show the allow-list"
pass "sink enabled, allow-list is [$RECV_HOSTPORT]"

step "step 4: create a subscription and test-fire it"
create_sub "http://$RECV_HOSTPORT/hook"
[[ "$HTTP_CODE" == 201 ]] || die "create subscription returned $HTTP_CODE: $API_BODY"
SUB_ID=$(printf '%s' "$API_BODY" | jfield "d['id']")
SUB_IDS+=("$SUB_ID")
pass "subscription $SUB_ID created (201)"
before=$(count_requests)
test_fire
[[ "$(printf '%s' "$API_BODY" | jfield "d['ok']")" == "true" ]] || die "test-fire did not report ok: $API_BODY"
after=$(count_requests)
[[ $((after - before)) -eq 1 ]] || die "expected exactly one POST at the receiver, got $((after - before))"
pass "test-fire returned ok with exactly one POST at the receiver"
check signature "$after" "$SUB_ID" || die "signature check failed"
check snippet "$after" || die "the documented receiver snippet check failed"
api GET "/api/audit?action=autonomous_lifecycle_webhook_delivery&limit=200"
DELIVERY=$(printf '%s' "$API_BODY" | check latest-delivery autonomous_lifecycle_webhook_delivery "$SUB_ID") || die "no delivery audit record"
printf '%s' "$API_BODY" | check audit "$DELIVERY" autonomous_lifecycle_webhook_delivery "delivered=1" || die "delivery audit record missing"
printf '%s' "$API_BODY" | check clean || die "the secret value leaked into the delivery audit records"
check clean-requests || die "the secret value leaked into the receiver's request"

step "step 5: negative checks"
count_subscriptions
n_subs=$SUB_COUNT
create_sub "https://hooks.example.net/datawatch"
[[ "$HTTP_CODE" == 400 ]] || die "a host that is not allow-listed was accepted (HTTP $HTTP_CODE)"
pass "a host that is not allow-listed is refused at creation (400)"
create_sub "http://169.254.169.254/latest/meta-data/"
[[ "$HTTP_CODE" == 400 ]] || die "the cloud metadata address was accepted (HTTP $HTTP_CODE)"
pass "the cloud metadata address is refused at creation (400)"
count_subscriptions
[[ "$SUB_COUNT" == "$n_subs" ]] || die "a refused subscription was stored"

put_config '{"autonomous.notify.webhook.allow_hosts":[]}'
before=$(count_requests)
test_fire
[[ "$(printf '%s' "$API_BODY" | jfield "d['ok']")" == "false" ]] || die "test-fire succeeded with an empty allow-list: $API_BODY"
after=$(count_requests)
[[ $((after - before)) -eq 0 ]] || die "empty allow-list still produced $((after - before)) request(s)"
pass "with the allow-list emptied, test-fire is refused and makes zero requests"
put_config "$(printf '{"autonomous.notify.webhook.allow_hosts":["%s"]}' "$RECV_HOSTPORT")"

step "step 6: retries with growing gaps"
put_config '{"autonomous.notify.webhook.backoff_seconds":[1,2]}'
printf '%s' "500 500" > "$WORK/codes"
before=$(count_requests)
test_fire
[[ "$(printf '%s' "$API_BODY" | jfield "d['ok']")" == "true" ]] || die "retried test-fire did not end delivered: $API_BODY"
[[ "$(printf '%s' "$API_BODY" | jfield "d['attempts']")" == "3" ]] || die "expected 3 attempts: $API_BODY"
after=$(count_requests)
[[ $((after - before)) -eq 3 ]] || die "expected 3 requests at the receiver, got $((after - before))"
check gaps $((before + 1)) "$after" || die "retry gaps check failed"
check same $((before + 1)) "$after" || die "retry identity check failed"
api GET "/api/audit?action=autonomous_lifecycle_webhook_delivery&limit=200"
DELIVERY=$(printf '%s' "$API_BODY" | check latest-delivery autonomous_lifecycle_webhook_delivery "$SUB_ID") || die "no delivery audit record"
printf '%s' "$API_BODY" | check audit "$DELIVERY" autonomous_lifecycle_webhook_delivery "retry=2,delivered=1" || die "retry audit records missing"

step "step 7: failure alert and final failure record"
stop_receiver
pass "receiver stopped"
put_config '{"autonomous.notify.webhook.backoff_seconds":[1,1,1,1]}'
test_fire
[[ "$(printf '%s' "$API_BODY" | jfield "d['ok']")" == "false" ]] || die "test-fire to a stopped receiver reported ok: $API_BODY"
[[ "$(printf '%s' "$API_BODY" | jfield "d['attempts']")" == "5" ]] || die "expected 5 attempts before giving up: $API_BODY"
api GET "/api/audit?action=autonomous_lifecycle_webhook_delivery&limit=200"
DELIVERY=$(printf '%s' "$API_BODY" | check latest-delivery autonomous_lifecycle_webhook_delivery "$SUB_ID") || die "no delivery audit record"
printf '%s' "$API_BODY" | check audit "$DELIVERY" autonomous_lifecycle_webhook_delivery "retry=4,failed=1" || die "attempt audit records missing"
api GET "/api/audit?action=autonomous_lifecycle_webhook_failed&limit=200"
printf '%s' "$API_BODY" | check audit "$DELIVERY" autonomous_lifecycle_webhook_failed "failed=1" || die "final failure audit record missing"
printf '%s' "$API_BODY" | check clean || die "the secret value leaked into the failure audit records"
api GET /api/alerts
printf '%s' "$API_BODY" | check alert "$SUB_ID" 5 || die "failure alert missing"
printf '%s' "$API_BODY" | check clean || die "the secret value leaked into the alert list"

# Fresh sandbox only: the daemon log and the on-disk audit files hold neither
# the secret value nor a signature. (The vault itself holds the value, so it
# is not scanned.)
if [[ "$FRESH" == 1 ]]; then
  scanned=0
  while IFS= read -r f; do
    scanned=$((scanned + 1))
    grep -qF -- "$SECRET_VALUE" "$f" && die "the throwaway secret value appears in $f"
    grep -qE 'v1=[0-9a-f]{64}' "$f" && die "a request signature appears in $f"
  done < <(find "$WORK/sandbox/data" -maxdepth 2 -type f -name 'audit*' -print; echo "$WORK/sandbox/daemon.log")
  [[ "$scanned" -ge 2 ]] || die "expected an audit file and the daemon log to scan, found $scanned file(s)"
  audit_lines=$(cat "$WORK"/sandbox/data/audit* 2>/dev/null | grep -c autonomous_lifecycle_webhook)
  [[ "$audit_lines" -ge 1 ]] || die "the on-disk audit file has no webhook records"
  pass "the daemon log and $((scanned - 1)) audit file(s) hold no secret value and no signature ($audit_lines webhook audit line(s) on disk)"
fi

step "step 8: forwarding (in-process Go tests)"
GO_LOG="$WORK/forwarding-tests.txt"
(cd "$REPO_ROOT" && go test -count=1 -v -run '^(TestForwardedEvents|TestForward)' \
  ./internal/autonomous/ ./internal/server/ ./cmd/datawatch/) > "$GO_LOG" 2>&1
go_rc=$?
go_pass=$(grep -c '^--- PASS: \(TestForwardedEvents\|TestForward\)' "$GO_LOG")
go_fail=$(grep -c '^--- FAIL: ' "$GO_LOG")
if [[ $go_rc -ne 0 || $go_fail -ne 0 || $go_pass -eq 0 ]]; then
  tail -40 "$GO_LOG"
  die "forwarding tests: rc=$go_rc passed=$go_pass failed=$go_fail"
fi
pass "forwarding tests: $go_pass top-level tests passed"
if [[ -n "$EVIDENCE_DIR" ]]; then
  cp "$GO_LOG" "$EVIDENCE_DIR/forwarding-tests.txt"
fi

check clean-requests || die "the secret value leaked into a receiver request"
echo "OK: lifecycle webhook smoke passed"
exit 0
