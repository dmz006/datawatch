#!/usr/bin/env bash
# release-smoke-stdio-mcp.sh — isolated stdio MCP smoke (BL171 smoke-fix, BL463 hardening)
#
# The main smoke covers `mcp --sse`; this covers the stdio path — the one
# Cursor (and Claude Desktop) actually use — which the SSE path doesn't
# exercise at all (different serve loop, same session-store wiring).
#
# It spawns `datawatch mcp` (or the binary named by $DW_BIN), speaks raw
# JSON-RPC 2.0 over its stdin/stdout pair, and checks the protocol works
# (no parse errors, expected responses, tool call round-trips).
#
# Probes (in order):
#   1. initialize                      → result has protocolVersion, serverInfo
#   2. notifications/initialized       → no response expected
#   3. tools/list                      → get_my_scope is in the list (BL463)
#   4. tools/call memory_recall        → call succeeded (response exists;
#                                        content/error shape is valid). The
#                                        daemon may be fresh or unreachable;
#                                        we assert the *protocol* contract,
#                                        not a specific business result.
#   5. BL463 — tools/call get_my_scope → a session can call get_my_scope:
#      - with $DW_SESSION_HINT (a known session id): MUST return
#        isError=false and a scope body (session_id/full_id/mcp_server_name/
#        shared_entry_name/peers/resolved_via="hint").
#      - without a hint: either a well-formed success body OR a well-formed
#        named error ("get_my_scope: ...") — never an empty/malformed result.
#   6. BL463 — failing call contract #1 (in-process): get_my_scope
#      hint="__smoke_missing__" MUST return isError=true with a named body
#      (the "status + body" of the MCP error result).
#   7. BL463 — failing call contract #2 (loopback proxy): template_delete
#      name="__smoke_missing__" proxies to the daemon and MUST return
#      isError=true with "template_delete: HTTP <status>: <body>" — the
#      same info shape REST gives a failed curl. When
#      $DW_REQUIRE_HTTP_STATUS=1 the upstream HTTP status code is asserted;
#      with =0 (standalone, daemon reachability unknown) a non-empty named
#      body is required.
#
# Env overrides (set by release-smoke.sh §7r when it runs against the
# isolated smoke daemon):
#   DW_BIN                  binary to spawn (default: `datawatch` on PATH)
#   DW_CONFIG               config file → spawned as `datawatch --config <file> mcp`
#                           (loopback tools then hit the smoke daemon's port,
#                           against the smoke data dir)
#   DW_SESSION_HINT         known session id for the positive get_my_scope probe
#   DW_REQUIRE_HTTP_STATUS  1 → assert "HTTP <code>" in the proxy failure body
#
# Exit codes:
#   0  pass
#   1  fail (a probe got a malformed/missing response)
#   2  skip — `datawatch` binary (or $DW_BIN) not found. The main smoke
#      treats 2 as "skip" so it can run in CI images without the binary.

set -uo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DW_BIN="${DW_BIN:-}"
if [[ -z "$DW_BIN" ]]; then
  if command -v datawatch >/dev/null 2>&1; then
    DW_BIN="datawatch"
  else
    echo "release-smoke-stdio-mcp: SKIP — no datawatch binary on PATH (set DW_BIN=<path> to force)"
    exit 2
  fi
fi

TMP="$(mktemp -d)"
RAW="$TMP/raw.log"
trap 'rm -rf "$TMP"' EXIT

ARGS=(mcp)
[[ -n "${DW_CONFIG:-}" ]] && ARGS=(--config "$DW_CONFIG" mcp)

{
  python3 - <<'PY'
import json
def r(id, method, params): 
    msg={"jsonrpc":"2.0","method":method}
    if id is not None: msg["id"]=id
    if params is not None: msg["params"]=params
    print(json.dumps(msg), flush=True)
r(1,"initialize",{
    "protocolVersion":"2024-11-05",
    "capabilities":{},
    "clientInfo":{"name":"release-smoke","version":"0"}})
r(None,"notifications/initialized",None)
r(3,"tools/list",{})
r(4,"tools/call",{"name":"memory_recall",
                   "arguments":{"query":"release smoke stdio fixture"}})
PY
  # BL463 — 5. positive get_my_scope probe (live check "a session can call
  # get_my_scope"). With DW_SESSION_HINT: the hint MUST resolve. Without
  # one: the tool still answers — either a real scope body (a channel-ready
  # session exists) or a well-formed named error — never malformed.
  if [[ -n "${DW_SESSION_HINT:-}" ]]; then
    python3 - "$DW_SESSION_HINT" <<'PY'
import json, sys
r={"jsonrpc":"2.0","id":5,"method":"tools/call",
   "params":{"name":"get_my_scope","arguments":{"hint":sys.argv[1]}}}
print(json.dumps(r), flush=True)
PY
  else
    python3 - <<'PY'
import json
r={"jsonrpc":"2.0","id":5,"method":"tools/call",
   "params":{"name":"get_my_scope","arguments":{}}}
print(json.dumps(r), flush=True)
PY
  fi
  # BL463 — 6. failing call, in-process: hint must not resolve.
  python3 - <<'PY'
import json
r={"jsonrpc":"2.0","id":6,"method":"tools/call",
   "params":{"name":"get_my_scope",
             "arguments":{"hint":"__smoke_missing__"}}}
print(json.dumps(r), flush=True)
PY
  # BL463 — 7. failing call, loopback proxy: daemon must 404 a missing
  # template and the MCP result must carry status + body.
  python3 - <<'PY'
import json
r={"jsonrpc":"2.0","id":7,"method":"tools/call",
   "params":{"name":"template_delete",
             "arguments":{"name":"__smoke_missing__"}}}
print(json.dumps(r), flush=True)
PY
  # Give the server a moment to drain the responses before the stdin EOF
  # triggers its shutdown path.
  sleep 2
} | "$DW_BIN" "${ARGS[@]}" > "$RAW" 2>"$TMP/stderr.log"

# --- validate the response stream with python (robust JSON parsing) --------
python3 - "${DW_SESSION_HINT:-}" "${DW_REQUIRE_HTTP_STATUS:-0}" <<'PY' "$RAW"
import json, re, sys

raw_path = sys.argv[-1]
hint = sys.argv[1] if len(sys.argv) > 2 and sys.argv[1] else ""
require_http_status = (sys.argv[2] if len(sys.argv) > 3 else "0") == "1"

msgs = []
for line in open(raw_path, encoding="utf8", errors="replace"):
    line = line.strip()
    if not line:
        continue
    try:
        msgs.append(json.loads(line))
    except Exception as e:
        # A non-JSON line on the MCP server's stdout is a protocol violation
        # (e.g. a log statement leaking into the protocol channel).
        print(f"FAIL: non-JSON on MCP stdout: {line[:200]!r} ({e})")
        sys.exit(1)

by_id = {m.get("id"): m for m in msgs if isinstance(m, dict) and "id" in m and "id" is not None}

# 1. initialize
m = by_id.get(1)
if not m:
    print("FAIL: no response to initialize (id=1)")
    sys.exit(1)
if "error" in m:
    print(f"FAIL: initialize errored: {m['error']}")
    sys.exit(1)
res = m.get("result", {})
if not res.get("protocolVersion"):
    print(f"FAIL: initialize result missing protocolVersion: {res}")
    sys.exit(1)
if not res.get("serverInfo", {}).get("name"):
    print(f"FAIL: initialize result missing serverInfo.name: {res}")
    sys.exit(1)

# 3. tools/list — must include the core tool set AND get_my_scope (BL463)
m = by_id.get(3)
if not m:
    print("FAIL: no response to tools/list (id=3)")
    sys.exit(1)
if "error" in m:
    print(f"FAIL: tools/list errored: {m['error']}")
    sys.exit(1)
tools = [t.get("name") for t in m.get("result", {}).get("tools", [])]
required = {'list_sessions','start_session','send_input','schedule_add','profile_list','agent_list','get_my_scope'}
missing = required - set(tools)
if missing:
    print(f"FAIL: tools/list missing required tools: {sorted(missing)}")
    sys.exit(1)

# 4. memory_recall — call must complete (result or protocol-error, either way);
#    a *tool* error result is acceptable in a fresh/standalone env (no
#    daemon / no session), but the shape must be a valid tool-result.
def tool_body(m):
    """text content of a tools/call result (isError or not)."""
    parts=[]
    for c in m.get("result",{}).get("content",[])[:1]:
        if isinstance(c,dict) and c.get("type")=="text" and isinstance(c.get("text"),str):
            parts.append(c["text"])
    return "".join(parts)

m = by_id.get(4)
if not m:
    print("FAIL: no response to tools/call memory_recall (id=4)")
    sys.exit(1)
if "error" in m and "result" not in m:
    # protocol-level error — acceptable only for a transport reason,
    # but we record it; the call must still not be malformed.
    print(f"NOTE: memory_recall returned protocol error: {m['error']}")
elif not isinstance(m.get("result"), dict):
    print(f"FAIL: memory_recall result not an object: {m}")
    sys.exit(1)

# 5. get_my_scope — positive (hint) or well-formed response (no hint)
m = by_id.get(5)
if not m:
    print("FAIL: no response to tools/call get_my_scope (id=5)")
    sys.exit(1)
if "error" in m and "result" not in m:
    print(f"FAIL: get_my_scope returned protocol error: {m['error']}")
    sys.exit(1)
res = m.get("result")
if not isinstance(res, dict):
    print(f"FAIL: get_my_scope result not an object: {m}")
    sys.exit(1)

def check_scope_body(body, resolved_via_expect):
    try:
        d = json.loads(body)
    except Exception:
        print(f"FAIL: get_my_scope body not JSON: {body[:300]!r}")
        return 1
    for k in ("session_id","full_id","mcp_server_name","shared_entry_name","mode","peers","channel_ready"):
        if k not in d:
            print(f"FAIL: get_my_scope body missing key {k!r}: {body[:300]!r}")
            return 1
    if not isinstance(d["peers"], list):
        print("FAIL: get_my_scope peers is not a list")
        return 1
    if d.get("resolved_via") != resolved_via_expect:
        print(f"FAIL: get_my_scope resolved_via={d.get('resolved_via')!r}, want {resolved_via_expect!r}")
        return 1
    if hint and hint not in (d["session_id"], d["full_id"]):
        print(f"FAIL: get_my_scope body does not resolve hint {hint!r}: id={d['session_id']!r} full={d['full_id']!r}")
        return 1
    return 0

if hint:
    if res.get("isError"):
        print(f"FAIL: get_my_scope hint={hint!r} returned error result: {tool_body(m)[:300]!r}")
        sys.exit(1)
    rc = check_scope_body(tool_body(m), "hint")
    if rc:
        sys.exit(1)
else:
    if res.get("isError"):
        body = tool_body(m)
        if not body.startswith("get_my_scope: "):
            print(f"FAIL: get_my_scope error body not named: {body[:300]!r}")
            sys.exit(1)
    else:
        rc = check_scope_body(tool_body(m), "channel-ready-fallback")
        if rc:
            sys.exit(1)

# 6. get_my_scope hint=__smoke_missing__ — MUST be a named error result
m = by_id.get(6)
if not m:
    print("FAIL: no response to failing get_my_scope call (id=6)")
    sys.exit(1)
res = m.get("result")
if not isinstance(res, dict) or not res.get("isError"):
    print(f"FAIL: get_my_scope(hint=__smoke_missing__) must return isError=true, got: {str(m)[:300]}")
    sys.exit(1)
body = tool_body(m)
if not body.startswith("get_my_scope: ") or "not found" not in body:
    print(f"FAIL: failing get_my_scope body must be 'get_my_scope: ... not found ...', got: {body[:300]!r}")
    sys.exit(1)

# 7. template_delete — proxy failure must carry status + body (BL463)
m = by_id.get(7)
if not m:
    print("FAIL: no response to failing template_delete call (id=7)")
    sys.exit(1)
res = m.get("result")
if not isinstance(res, dict) or not res.get("isError"):
    print(f"FAIL: template_delete(__smoke_missing__) must return isError=true, got: {str(m)[:300]}")
    sys.exit(1)
body = tool_body(m)
if not body.startswith("template_delete: "):
    print(f"FAIL: template_delete error body must carry the tool name, got: {body[:300]!r}")
    sys.exit(1)
detail = body[len("template_delete: "):].strip()
if not detail:
    print("FAIL: template_delete error body empty after tool name")
    sys.exit(1)
if require_http_status:
    mm = re.match(r"HTTP\s+([1-5][0-9][0-9])\s*:?\s*(.*)$", detail, re.S)
    if not mm:
        print(f"FAIL: template_delete failure must carry 'HTTP <status>: <body>', got: {body[:300]!r}")
        sys.exit(1)
    if not mm.group(2).strip():
        print(f"FAIL: template_delete failure must carry an upstream body, got: {body[:300]!r}")
        sys.exit(1)

print("PASS: stdio MCP protocol round-trip (initialize, tools/list incl. get_my_scope, "
      "memory_recall, get_my_scope positive/negative, failing-call status+body contract)")
PY
rc=$?

if [[ $rc -ne 0 ]]; then
  echo "  --- last 80 lines of raw stdio output ---"
  tail -80 "$RAW" | sed 's/^/  /'
  echo "  --- stderr ---"
  tail -20 "$TMP/stderr.log" | sed 's/^/  /'
fi
exit $rc
