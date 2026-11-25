#!/usr/bin/env bash
# Tests the compute-node stand-in scripts/restart-matrix/lib.sh writes for its
# pi-node states. The daemon's placement check reads a node's served models from
# GET /api/tags; a stub that answers only /api/ps leaves the node with no served
# models, so the worker waits for placement approval and the state never runs.
# Only the stub runs here, on a loopback port, under its own time limit; no
# daemon is started.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIB="${RM_MATRIX_LIB:-$ROOT/scripts/restart-matrix/lib.sh}"
fails=0
ok()  { echo "ok   - $1"; }
bad() { echo "FAIL - $1"; fails=$((fails+1)); }

[[ -f "$LIB" ]] || { echo "FAIL - $LIB not found"; exit 1; }
for t in python3 curl jq timeout; do
  command -v "$t" >/dev/null 2>&1 || { echo "SKIP - restart-matrix node stub: required tool missing: $t"; exit 0; }
done

WORK="$(mktemp -d /tmp/rmns.XXXXXX)"
stub=""
cleanup() { [[ -n "$stub" ]] && kill -TERM "$stub" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

awk '/cat >"\$RM_CTRL\/node-stub.py" <<.EOF./{f=1; next} f && /^EOF$/{exit} f' "$LIB" >"$WORK/node-stub.py"
[[ -s "$WORK/node-stub.py" ]] || { echo "FAIL - node stub not found in $LIB"; exit 1; }
port="$(python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
timeout 20 python3 -I "$WORK/node-stub.py" qwen-test:7b "$port" >"$WORK/stub.log" 2>&1 &
stub=$!
for _ in $(seq 1 50); do
  curl -s -o /dev/null --max-time 1 "http://127.0.0.1:$port/api/ps" && break
  sleep 0.1
done

for path in /api/tags /api/ps; do
  name="$(curl -s --max-time 2 "http://127.0.0.1:$port$path" | jq -r '.models[0].name // empty' 2>/dev/null)"
  if [[ "$name" == qwen-test:7b ]]; then ok "GET $path lists the node's model"; else bad "GET $path lists the node's model (got '${name}')"; fi
done
code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$port/api/other")"
if [[ "$code" == 404 ]]; then ok "any other path is refused"; else bad "any other path is refused (got $code)"; fi

echo
if [[ $fails -eq 0 ]]; then echo "all passed"; else echo "$fails failure(s)"; exit 1; fi
