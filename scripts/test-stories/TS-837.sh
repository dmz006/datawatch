#!/usr/bin/env bash
# TS-837 — built CLI binary: automation pause version discipline against the daemon
# tags: surface:cli feature:automata
#
# Drives the actually built datawatch binary (not a fixture harness) against a
# minimal stub daemon that speaks the two REST calls the control path uses:
#   GET  /api/automations/<id>          -> {"version": N}
#   POST /api/automations/<id>/pause    -> 200 or 409 "version mismatch"
#
# What it proves end-to-end on the real binary:
#   A. Omitted --expected-version: the CLI reads the current version first,
#      and when its POST is refused 409 (a concurrent writer bumped the
#      Automation) it re-reads and posts exactly once more — then succeeds.
#   B. Explicit --expected-version 99 (stale): refused 409, the CLI exits
#      non-zero, sends exactly ONE POST, and never does an implicit GET that
#      would have clobbered the operator's explicit version.
#   C. Explicit --expected-version 5 (current): accepted on the single POST,
#      no GET at all.
#   D. GET failure (unknown Automation) is returned without any POST.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-837"
story_preflight "surface:cli feature:automata" || return 0

_story_ts_837() {
  local bin="${TEST_BINARY:-$REPO_ROOT/bin/datawatch}"
  if [[ ! -x "$bin" ]]; then skip "datawatch binary not built (expected $bin)"; return; fi
  if ! command -v python3 >/dev/null 2>&1; then skip "python3 unavailable for the stub daemon"; return; fi

  local port
  port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null) || { skip "could not allocate a free port"; return; }

  local stub logf
  stub=$(mktemp)
  logf=$(mktemp)
  cat > "$stub" <<'PY'
import json, os, sys, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOCK = threading.Lock()
STATE = {"version": 5, "gets": 0}
AUTO = "TS837-AUTO"
LOGF = open(os.environ["TS837_LOG"], "a")

def rec(kind, **extra):
    with LOCK:
        LOGF.write(json.dumps({"kind": kind, **extra}) + "\n")
        LOGF.flush()

class H(BaseHTTPRequestHandler):
    def _send(self, code, obj):
        b = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def _reset(self):
        with LOCK:
            STATE["version"], STATE["gets"] = 5, 0
        rec("reset")
        self._send(200, {"ok": True})

    def do_GET(self):
        if self.path in ("/_reset",):
            self._reset()
            return
        if self.path == "/api/automations/%s" % AUTO:
            with LOCK:
                STATE["gets"] += 1
                v = STATE["version"]
                if STATE["gets"] == 1:
                    # Simulate a concurrent operator bumping the Automation
                    # right after this read: the CLI's first POST below will
                    # see a version mismatch and must retry exactly once.
                    STATE["version"] = 6
            rec("GET", version=v)
            self._send(200, {"id": AUTO, "name": "ts837 stub", "status": "active", "version": v})
            return
        rec("GET", path=self.path)
        self._send(404, {"error": "unknown automation"})

    def do_POST(self):
        if self.path in ("/_reset",):
            self._reset()
            return
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        if self.path != "/api/automations/%s/pause" % AUTO:
            rec("POST", path=self.path, body=body)
            self._send(404, {"error": "unknown route"})
            return
        ev, cur = body.get("expected_version"), STATE["version"]
        accepted = (ev == cur)
        rec("POST", expected_version=ev, accepted=accepted, current=cur)
        if accepted:
            self._send(200, {"id": AUTO, "name": "ts837 stub", "status": "paused", "version": ev})
        else:
            self._send(409, {"error": "version mismatch: expected %s, current %s" % (ev, cur)})

    def log_message(self, *args):
        pass

ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY
  TS837_LOG="$logf" python3 "$stub" "$port" >/dev/null 2>&1 &
  local stub_pid=$!

  # Kill the stub no matter how this story exits.
  cleanup_rc() { kill "${stub_pid}" >/dev/null 2>&1; rm -f "${stub}" "${logf}"; }
  trap cleanup_rc RETURN

  # Ready? (also serves as the initial reset)
  local i ready=0
  for i in $(seq 1 50); do
    if curl -s -o /dev/null --noproxy 127.0.0.1 "http://127.0.0.1:$port/_reset" 2>/dev/null; then ready=1; break; fi
    sleep 0.1
  done
  if [[ $ready -ne 1 ]]; then
    ko "stub daemon never came up on 127.0.0.1:$port"; return
  fi

  local url AUTO
  AUTO="TS837-AUTO"
  url="http://127.0.0.1:$port"

  # ---- A. Omitted --expected-version: implicit read, then exactly one retry
  local out rc
  out=$("$bin" --config "$TEST_DATA/config.yaml" --url "$url" automation pause "$AUTO" 2>&1); rc=$?
  save_evidence TS-837 "A-implicit-race.txt" "rc=$rc"$'\n'"$out"$'\n'"---- request log ----"$'\n'"$(cat "$logf")"
  if [[ $rc -ne 0 ]]; then
    ko "A: implicit pause after a 409 must succeed, got rc=$rc: $out"
    return
  fi
  if ! python3 - "$logf" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
order = [r["kind"] for r in rows if r["kind"] in ("GET", "POST")]
posts = [r for r in rows if r["kind"] == "POST"]
gets = [r for r in rows if r["kind"] == "GET" and "version" in r]
assert order == ["GET", "POST", "GET", "POST"], order
assert gets[0]["version"] == 5 and gets[1]["version"] == 6, gets
assert posts[0]["expected_version"] == 5 and posts[0]["accepted"] is False, posts
assert posts[1]["expected_version"] == 6 and posts[1]["accepted"] is True, posts
PY
  then
    ko "A: request order must be GET(5), POST(5, 409), GET(6), POST(6, 200)"
    return
  fi
  echo "  PASS  [TS-837] A: implicit version read + exactly one 409 retry"

  # ---- B. Explicit stale --expected-version: refused, ONE POST, no GET
  curl -s -o /dev/null --noproxy 127.0.0.1 "$url/_reset"
  : > "$logf"
  out=$("$bin" --config "$TEST_DATA/config.yaml" --url "$url" automation pause "$AUTO" --expected-version 99 2>&1); rc=$?
  save_evidence TS-837 "B-explicit-stale.txt" "rc=$rc"$'\n'"$out"$'\n'"---- request log ----"$'\n'"$(cat "$logf")"
  if [[ $rc -eq 0 ]]; then
    ko "B: a stale explicit expected-version must be refused, but the CLI exited 0"
    return
  fi
  if ! echo "$out" | grep -qi "version mismatch\|409"; then
    ko "B: expected the daemon's 409 version-mismatch in the CLI output, got: $out"
    return
  fi
  if ! python3 - "$logf" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
order = [r["kind"] for r in rows if r["kind"] in ("GET", "POST")]
posts = [r for r in rows if r["kind"] == "POST"]
assert order == ["POST"], order          # explicit version: never an implicit GET
assert len(posts) == 1, posts
assert posts[0]["expected_version"] == 99 and posts[0]["accepted"] is False, posts
PY
  then
    ko "B: explicit stale version must send exactly one POST and no GET"
    return
  fi
  echo "  PASS  [TS-837] B: explicit stale version refused, no implicit GET, no retry"

  # ---- C. Explicit current --expected-version: one POST, accepted, no GET
  curl -s -o /dev/null --noproxy 127.0.0.1 "$url/_reset"
  : > "$logf"
  out=$("$bin" --config "$TEST_DATA/config.yaml" --url "$url" automation pause "$AUTO" --expected-version 5 2>&1); rc=$?
  save_evidence TS-837 "C-explicit-current.txt" "rc=$rc"$'\n'"$out"$'\n'"---- request log ----"$'\n'"$(cat "$logf")"
  if [[ $rc -ne 0 ]]; then
    ko "C: explicit current version must be accepted, got rc=$rc: $out"
    return
  fi
  if ! python3 - "$logf" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
order = [r["kind"] for r in rows if r["kind"] in ("GET", "POST")]
posts = [r for r in rows if r["kind"] == "POST"]
assert order == ["POST"], order
assert len(posts) == 1
assert posts[0]["expected_version"] == 5 and posts[0]["accepted"] is True, posts
PY
  then
    ko "C: explicit current version must be a single accepted POST with no GET"
    return
  fi
  echo "  PASS  [TS-837] C: explicit current version accepted on one POST"

  # ---- D. GET failure (unknown Automation): error, and no control POST at all
  curl -s -o /dev/null --noproxy 127.0.0.1 "$url/_reset"
  : > "$logf"
  out=$("$bin" --config "$TEST_DATA/config.yaml" --url "$url" automation pause "TS837-NOPE" 2>&1); rc=$?
  save_evidence TS-837 "D-get-failure.txt" "rc=$rc"$'\n'"$out"$'\n'"---- request log ----"$'\n'"$(cat "$logf")"
  if [[ $rc -eq 0 ]]; then
    ko "D: pausing an unknown Automation must fail, but the CLI exited 0"
    return
  fi
  if ! python3 - "$logf" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
assert not [r for r in rows if r["kind"] == "POST"], rows    # nothing posted
assert [r for r in rows if r["kind"] == "GET" and r.get("path") == "/api/automations/TS837-NOPE"], rows
PY
  then
    ko "D: a failed version GET must stop before any control POST"
    return
  fi
  echo "  PASS  [TS-837] D: failed GET stops before any POST"

  ok "built binary enforces version discipline on automation pause (implicit retry, explicit strictness, no POST after failed read)"
}

_story_ts_837

: "${RESULT:=fail}"
