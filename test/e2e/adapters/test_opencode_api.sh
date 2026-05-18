#!/usr/bin/env bash
# TS-387–TS-388 — opencode-api adapter tests.
# Uses mock-opencode nginx container or a real opencode endpoint.
# Requires: DW_BASE_URL, OPENCODE_URL (default http://localhost:18081)

set -euo pipefail

DW="${DW_BASE_URL:-http://localhost:8080}"
OPENCODE="${OPENCODE_URL:-http://localhost:18081}"
TOKEN="${DW_TOKEN:-}"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# TS-387: add opencode-api compute node.
NODE=$(cat <<EOF
{
  "name": "e2e-opencode-node",
  "kind": "opencode-api",
  "address": "${OPENCODE}",
  "routing": "direct",
  "declared_capacity": {"max_concurrent_models": 1}
}
EOF
)
HTTP=$(curl -s -o /tmp/opencode_node.json -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$NODE")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] || fail "add opencode node HTTP $HTTP: $(cat /tmp/opencode_node.json)"
pass "TS-387: opencode-api compute node added"

# TS-388: add opencode-api LLM.
LLM=$(cat <<EOF
{
  "name": "e2e-opencode-llm",
  "kind": "opencode-api",
  "compute_nodes": ["e2e-opencode-node"],
  "models": [{"node":"e2e-opencode-node","model":"claude-sonnet-4-5"}]
}
EOF
)
HTTP=$(curl -s -o /tmp/opencode_llm.json -w "%{http_code}" \
  -X POST "${DW}/api/llms" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$LLM")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] || fail "add opencode LLM HTTP $HTTP: $(cat /tmp/opencode_llm.json)"
pass "TS-388: opencode-api LLM added"

# TS-388: node address required — add node without address.
NO_ADDR=$(cat <<EOF
{
  "name": "e2e-opencode-noaddr",
  "kind": "opencode-api",
  "routing": "direct"
}
EOF
)
HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$NO_ADDR")
# Should fail validation or probe — not 2xx.
[[ "$HTTP" != "200" && "$HTTP" != "201" ]] || fail "TS-388: opencode-api without address should not be 200"
pass "TS-388: opencode-api without address rejected"

# Cleanup.
curl -s -X DELETE "${DW}/api/llms/e2e-opencode-llm" ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null
curl -s -X DELETE "${DW}/api/compute/nodes/e2e-opencode-node" ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null

echo "PASS: test_opencode_api.sh"
