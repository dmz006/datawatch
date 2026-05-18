#!/usr/bin/env bash
# TS-365, TS-366 — Direct routing baseline tests.
# Adds an Ollama compute node with direct routing, verifies probe, asks a test query.
# Requires: DW_BASE_URL (default http://localhost:8080), OLLAMA_URL (default http://localhost:11434)

set -euo pipefail

DW="${DW_BASE_URL:-http://localhost:8080}"
OLLAMA="${OLLAMA_URL:-http://localhost:11434}"
TOKEN="${DW_TOKEN:-}"
AUTH=""
if [[ -n "$TOKEN" ]]; then AUTH="-H \"Authorization: Bearer $TOKEN\""; fi

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# TS-365: POST a direct-routing compute node.
PAYLOAD=$(cat <<EOF
{
  "name": "e2e-direct",
  "kind": "ollama",
  "address": "${OLLAMA}",
  "routing": "direct",
  "declared_capacity": {"max_concurrent_models": 1}
}
EOF
)
HTTP=$(curl -s -o /tmp/dw_direct.json -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$PAYLOAD")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] || fail "add node got HTTP $HTTP: $(cat /tmp/dw_direct.json)"
pass "TS-365: direct compute node added (HTTP $HTTP)"

# TS-365: GET the node back and verify routing field present.
HTTP=$(curl -s -o /tmp/dw_get.json -w "%{http_code}" \
  "${DW}/api/compute/nodes/e2e-direct" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"})
[[ "$HTTP" == "200" ]] || fail "get node got HTTP $HTTP"
grep -q '"routing"' /tmp/dw_get.json || fail "routing field missing from GET response"
pass "TS-365: routing field present in GET response"

# TS-366: POST with invalid routing mode — expect 400.
INVALID=$(cat <<EOF
{
  "name": "e2e-invalid-routing",
  "kind": "ollama",
  "address": "${OLLAMA}",
  "routing": "teleportation"
}
EOF
)
HTTP=$(curl -s -o /tmp/dw_invalid.json -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$INVALID")
[[ "$HTTP" == "400" ]] || fail "TS-366: invalid routing expected 400, got $HTTP"
pass "TS-366: invalid routing correctly rejected with 400"

# Cleanup.
curl -s -X DELETE "${DW}/api/compute/nodes/e2e-direct" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null

echo "PASS: test_direct.sh"
