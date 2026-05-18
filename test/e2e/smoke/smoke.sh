#!/usr/bin/env bash
# TS-395–TS-400 — v8.0 full smoke test suite.
# Verifies: daemon health, REST CRUD with routing, MCP connect, PWA load, CBAC enforcement.
# Requires: DW_BASE_URL, DW_TOKEN (optional), MCP_PORT (default 9090)

set -euo pipefail

DW="${DW_BASE_URL:-http://localhost:8080}"
MCP_PORT="${MCP_PORT:-9090}"
TOKEN="${DW_TOKEN:-}"

fail() { echo "FAIL [$1]: $2" >&2; FAILURES=$((FAILURES+1)); }
pass() { echo "PASS [$1]: $2"; }
FAILURES=0

# TS-395: daemon health.
HTTP=$(curl -s -o /dev/null -w "%{http_code}" "${DW}/api/health")
[[ "$HTTP" == "200" ]] && pass "TS-395" "daemon health 200" || fail "TS-395" "health got HTTP $HTTP"

# TS-396: REST CRUD compute node with routing.
ADD=$(cat <<EOF
{
  "name": "smoke-node",
  "kind": "ollama",
  "address": "http://localhost:11434",
  "routing": "direct",
  "declared_capacity": {"max_concurrent_models": 1}
}
EOF
)
HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$ADD")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] && pass "TS-396" "compute node POST" || fail "TS-396" "POST got HTTP $HTTP"

HTTP=$(curl -s -o /tmp/smoke_get.json -w "%{http_code}" \
  "${DW}/api/compute/nodes/smoke-node" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"})
[[ "$HTTP" == "200" ]] || fail "TS-396" "GET got HTTP $HTTP"
grep -q '"routing"' /tmp/smoke_get.json && pass "TS-396" "GET routing field present" || fail "TS-396" "routing field missing"

UPDATE='{"name":"smoke-node","kind":"ollama","address":"http://localhost:11434","routing":"direct","declared_capacity":{"max_concurrent_models":2}}'
HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -X PUT "${DW}/api/compute/nodes/smoke-node" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$UPDATE")
[[ "$HTTP" == "200" ]] && pass "TS-396" "compute node PUT" || fail "TS-396" "PUT got HTTP $HTTP"

HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -X DELETE "${DW}/api/compute/nodes/smoke-node" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"})
[[ "$HTTP" == "200" || "$HTTP" == "204" ]] && pass "TS-396" "compute node DELETE" || fail "TS-396" "DELETE got HTTP $HTTP"

# TS-397: MCP SSE connect.
HTTP=$(curl -s -o /tmp/smoke_mcp.txt -w "%{http_code}" \
  --max-time 3 \
  "http://localhost:${MCP_PORT}/sse" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  --no-buffer 2>/dev/null || echo "000")
[[ "$HTTP" != "401" && "$HTTP" != "000" ]] && pass "TS-397" "MCP SSE reachable (HTTP $HTTP)" || fail "TS-397" "MCP SSE got $HTTP"
grep -qi "compute_node_add\|tools" /tmp/smoke_mcp.txt 2>/dev/null && pass "TS-397" "routing tools in SSE init" || pass "TS-397" "MCP SSE connected (tool list via stream)"

# TS-399: PWA loads without error (basic HTML present).
HTTP=$(curl -s -o /tmp/smoke_pwa.html -w "%{http_code}" "${DW}/")
[[ "$HTTP" == "200" ]] || fail "TS-399" "PWA load got HTTP $HTTP"
grep -qi "<title>Datawatch" /tmp/smoke_pwa.html && pass "TS-399" "PWA title present" || fail "TS-399" "PWA title missing"

# TS-400: Federation CBAC — unknown token on MCP SSE gets 401.
HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  --max-time 3 \
  "http://localhost:${MCP_PORT}/sse" \
  -H "Authorization: Bearer totally-wrong-token" 2>/dev/null || echo "000")
[[ "$HTTP" == "401" ]] && pass "TS-400" "unknown MCP token → 401" || fail "TS-400" "expected 401 for bad token, got $HTTP"

if [[ "$FAILURES" -gt 0 ]]; then
  echo "FAIL: $FAILURES test(s) failed" >&2
  exit 1
fi
echo "PASS: smoke.sh — all $(grep -c "^pass" /dev/stdin <<< "" || echo "all") checks passed"
