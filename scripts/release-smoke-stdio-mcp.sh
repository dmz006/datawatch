#!/usr/bin/env bash
# v5.26.71 — Standalone smoke for the stdio-MCP surface.
#
# Spawns `datawatch mcp` as a subprocess, writes JSON-RPC initialize
# + tools/list + tools/call(memory_recall) over stdin, reads framed
# responses from stdout, and asserts each one returns shape-correct
# JSON. The release-smoke.sh §7r section calls this with a 30s
# budget; it can also be run standalone for debugging.
#
# Skips cleanly when:
#   - `datawatch` not in PATH
#   - `datawatch mcp` subcommand absent (older builds)
#
# Exit code 0 = pass, 1 = real failure, 2 = skip.

set -uo pipefail

if ! command -v datawatch >/dev/null 2>&1; then
  echo "SKIP: datawatch not in PATH"
  exit 2
fi
if ! datawatch --help 2>&1 | grep -q '^\s*mcp\s'; then
  echo "SKIP: datawatch mcp subcommand absent"
  exit 2
fi

REQ1='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"datawatch-smoke","version":"1.0"}}}'
REQ2='{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
REQ3='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
REQ4='{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"smoke stdio probe","top_k":1}}}'

OUTFILE=$(mktemp)
trap 'rm -f "$OUTFILE"' EXIT
# Hold stdin open briefly after sending all 4 requests so the server
# has time to process them before EOF terminates the read loop.
( printf '%s\n%s\n%s\n%s\n' "$REQ1" "$REQ2" "$REQ3" "$REQ4"; sleep 2 ) \
  | timeout 20 datawatch mcp >"$OUTFILE" 2>/dev/null
RC=$?
if [[ $RC -ne 0 && $RC -ne 124 ]]; then
  echo "FAIL: datawatch mcp exited rc=$RC"
  exit 1
fi

# Validate replies with python — read directly from the spool file
# so a multi-MB tools/list response doesn't blow the bash env limit.
RESULT=$(STDIO_MCP_FILE="$OUTFILE" python3 -c '
import json, os, sys
with open(os.environ["STDIO_MCP_FILE"]) as f:
    out = f.read()

ids_seen = set()
saw_memory_recall = False
recall_response_ok = False

for raw in out.splitlines():
    line = raw.strip()
    if not line.startswith("{"):
        continue
    try:
        msg = json.loads(line)
    except Exception:
        continue
    if msg.get("jsonrpc") != "2.0":
        continue
    if "id" in msg:
        ids_seen.add(msg["id"])
    if msg.get("id") == 2 and "result" in msg:
        tools = msg["result"].get("tools") or []
        names = [t.get("name") for t in tools]
        if "memory_recall" in names:
            saw_memory_recall = True
    if msg.get("id") == 3 and ("result" in msg or "error" in msg):
        recall_response_ok = True

problems = []
if 1 not in ids_seen: problems.append("initialize response missing")
if 2 not in ids_seen: problems.append("tools/list response missing")
if 3 not in ids_seen: problems.append("tools/call response missing")
if not saw_memory_recall: problems.append("memory_recall not in tools/list")
if not recall_response_ok: problems.append("memory_recall call had no result/error")

if problems:
    print("FAIL: " + "; ".join(problems))
    sys.exit(1)

print("PASS: stdio MCP probe (initialize + tools/list + tools/call memory_recall)")
' 2>&1)

echo "$RESULT"
[[ "$RESULT" == PASS:* ]]
