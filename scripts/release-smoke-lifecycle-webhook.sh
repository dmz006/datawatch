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
# Environment:
#   DATAWATCH_BASE              sandbox base URL (default https://127.0.0.1:18443)
#   DATAWATCH_TOKEN             bearer token (default: the sandbox test token)
#   DATAWATCH_SMOKE_KEEP_WORK=1 keep the scratch directory for inspection
#
# Exit code: 0 = pass, 1 = real failure, 2 = skip (sandbox daemon unreachable).
# The daemon is reached over HTTPS only (the sandbox certificate is
# self-signed, so curl runs with -k). The webhook sink and the forwarding ship
# disabled; this smoke enables the webhook sink on the sandbox for its run and
# restores the original values on exit.

if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE="${DATAWATCH_BASE:-https://127.0.0.1:18443}"
TOKEN="${DATAWATCH_TOKEN:-dw-test-token-12345}"
SECRET_NAME="lifecycle-smoke-signing"
# Obviously fake, and only ever stored for the length of this run.
SECRET_VALUE="fake-smoke-value-not-a-real-secret"
ALERT_TITLE="Lifecycle webhook delivery failed"

WORK="$(mktemp -d)"
RECV_PID=""
RECV_HOSTPORT=""
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
  SMOKE_SECRET="$SECRET_VALUE" python3 -I "$WORK/check.py" "$WORK" "$@"
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
    kill "$RECV_PID" 2>/dev/null || true
    wait "$RECV_PID" 2>/dev/null || true
    RECV_PID=""
  fi
}

cleanup() {
  local rc=$?
  stop_receiver
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
  if [[ "${DATAWATCH_SMOKE_KEEP_WORK:-0}" == 1 ]]; then
    echo "kept scratch directory: $WORK"
  else
    rm -rf "$WORK"
  fi
  exit "$rc"
}
trap cleanup EXIT

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
api GET /api/health
if [[ "$HTTP_CODE" != 200 ]]; then
  echo "SKIP: sandbox daemon unreachable at $BASE (health returned $HTTP_CODE)"
  exit 2
fi

api GET /api/config
[[ "$HTTP_CODE" == 200 ]] || die "GET /api/config returned $HTTP_CODE: check DATAWATCH_TOKEN"
ORIG_JSON=$(printf '%s' "$API_BODY" | python3 -I -c '
import json, sys
w = json.load(sys.stdin)["autonomous"]["notify"]["webhook"]
print(json.dumps({
    "autonomous.notify.webhook.enabled": w["enabled"],
    "autonomous.notify.webhook.allow_hosts": w["allow_hosts"],
    "autonomous.notify.webhook.backoff_seconds": w["backoff_seconds"],
}))') || die "could not read the current webhook settings"
echo "start: sandbox $BASE, webhook settings $ORIG_JSON"

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
api GET "/api/audit?action=autonomous_lifecycle_webhook_delivery&limit=200"
DELIVERY=$(printf '%s' "$API_BODY" | check latest-delivery autonomous_lifecycle_webhook_delivery "$SUB_ID") || die "no delivery audit record"
printf '%s' "$API_BODY" | check audit "$DELIVERY" autonomous_lifecycle_webhook_delivery "delivered=1" || die "delivery audit record missing"

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
api GET /api/alerts
printf '%s' "$API_BODY" | check alert "$SUB_ID" 5 || die "failure alert missing"

step "step 8: forwarding (in-process Go tests)"
GO_LOG="$WORK/forwarding-tests.log"
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

echo "OK: lifecycle webhook smoke passed"
exit 0
