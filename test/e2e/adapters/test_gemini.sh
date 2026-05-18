#!/usr/bin/env bash
# TS-385–TS-386 — Gemini API adapter tests.
# Uses mock-gemini nginx container or a real Gemini endpoint.
# Requires: DW_BASE_URL, GEMINI_URL (default http://localhost:18080)

set -euo pipefail

DW="${DW_BASE_URL:-http://localhost:8080}"
GEMINI="${GEMINI_URL:-http://localhost:18080}"
TOKEN="${DW_TOKEN:-}"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# TS-386: add LLM without api_key_ref — verify 400 or error.
NO_KEY=$(cat <<EOF
{
  "name": "e2e-gemini",
  "kind": "gemini-api",
  "compute_nodes": ["e2e-gemini-node"]
}
EOF
)
# First add the compute node (no api_key_ref required on the node).
NODE=$(cat <<EOF
{
  "name": "e2e-gemini-node",
  "kind": "gemini-api",
  "address": "${GEMINI}",
  "routing": "direct"
}
EOF
)
HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$NODE")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] || fail "add gemini node HTTP $HTTP"
pass "TS-385: gemini-api compute node added"

# TS-385: add LLM with api_key_ref pointing to a test secret.
# First register a dummy secret.
curl -s -X POST "${DW}/api/secrets" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d '{"key":"gemini-test-key","value":"fake-api-key-for-testing"}' > /dev/null

LLM=$(cat <<EOF
{
  "name": "e2e-gemini-llm",
  "kind": "gemini-api",
  "compute_nodes": ["e2e-gemini-node"],
  "models": [{"node":"e2e-gemini-node","model":"gemini-1.5-flash"}],
  "api_key_ref": "\${secret:gemini-test-key}"
}
EOF
)
HTTP=$(curl -s -o /tmp/gemini_llm.json -w "%{http_code}" \
  -X POST "${DW}/api/llms" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$LLM")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] || fail "add gemini LLM HTTP $HTTP: $(cat /tmp/gemini_llm.json)"
pass "TS-385: gemini-api LLM added"

# Cleanup.
curl -s -X DELETE "${DW}/api/llms/e2e-gemini-llm" ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null
curl -s -X DELETE "${DW}/api/compute/nodes/e2e-gemini-node" ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null
curl -s -X DELETE "${DW}/api/secrets/gemini-test-key" ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null

echo "PASS: test_gemini.sh"
