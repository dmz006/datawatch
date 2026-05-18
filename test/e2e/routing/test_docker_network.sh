#!/usr/bin/env bash
# TS-373–TS-378 — docker-network routing tests.
# Adds a node with docker-network routing, verifies container creation, infers.
# Requires: DW_BASE_URL, docker CLI in PATH, DOCKER_NETWORK (default datawatch-llm)

set -euo pipefail

DW="${DW_BASE_URL:-http://localhost:8080}"
TOKEN="${DW_TOKEN:-}"
NET="${DOCKER_NETWORK:-datawatch-llm}"
CONTAINER="dw-e2e-ollama"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# TS-367: POST node with routing=docker-network, missing image — expect 400.
NO_IMAGE=$(cat <<EOF
{
  "name": "e2e-dn-noimag",
  "kind": "ollama",
  "routing": "docker-network",
  "routing_docker_network": {
    "network_name": "$NET",
    "port": 11434
  }
}
EOF
)
HTTP=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$NO_IMAGE")
[[ "$HTTP" == "400" ]] || fail "TS-367: missing image expected 400, got $HTTP"
pass "TS-367: docker-network missing image → 400"

# Add the real docker-network node.
PAYLOAD=$(cat <<EOF
{
  "name": "e2e-docker-net",
  "kind": "ollama",
  "routing": "docker-network",
  "routing_docker_network": {
    "image": "ollama/ollama:latest",
    "network_name": "$NET",
    "container_name": "$CONTAINER",
    "port": 11434,
    "auto_start": true,
    "auto_pull": false
  },
  "declared_capacity": {"max_concurrent_models": 1}
}
EOF
)
HTTP=$(curl -s -o /tmp/dw_dn.json -w "%{http_code}" \
  -X POST "${DW}/api/compute/nodes" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
  -d "$PAYLOAD")
[[ "$HTTP" == "200" || "$HTTP" == "201" ]] || fail "add docker-network node got HTTP $HTTP: $(cat /tmp/dw_dn.json)"
pass "TS-373: docker-network node added"

# TS-373: verify Docker network exists after daemon contact.
sleep 3
docker network inspect "$NET" &>/dev/null || fail "TS-373: Docker network $NET not created"
pass "TS-373: Docker network $NET created"

# TS-374: verify container is running after first probe.
docker ps --filter "name=$CONTAINER" --format "{{.Names}}" | grep -q "$CONTAINER" || fail "TS-374: container $CONTAINER not running"
pass "TS-374: container $CONTAINER running"

# TS-375: hit the node health endpoint a second time — no new container.
BEFORE=$(docker ps --filter "name=$CONTAINER" --format "{{.ID}}" | wc -l)
curl -s "${DW}/api/compute/nodes/e2e-docker-net/health" ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null
sleep 1
AFTER=$(docker ps --filter "name=$CONTAINER" --format "{{.ID}}" | wc -l)
[[ "$AFTER" -le "$BEFORE" ]] || fail "TS-375: extra container spawned on second call"
pass "TS-375: no extra container on second probe"

# TS-377: GET /detail includes container_running.
HTTP=$(curl -s -o /tmp/dw_detail.json -w "%{http_code}" \
  "${DW}/api/compute/nodes/e2e-docker-net/detail" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"})
[[ "$HTTP" == "200" ]] || fail "TS-377: /detail got HTTP $HTTP"
grep -q '"container_running"' /tmp/dw_detail.json || fail "TS-377: container_running missing from /detail"
pass "TS-377: /detail includes container_running"

# TS-376: DELETE node → container removed.
curl -s -X DELETE "${DW}/api/compute/nodes/e2e-docker-net" \
  ${TOKEN:+-H "Authorization: Bearer $TOKEN"} > /dev/null
sleep 2
docker ps -a --filter "name=$CONTAINER" --format "{{.Names}}" | grep -qv "$CONTAINER" || fail "TS-376: container still present after node delete"
pass "TS-376: container removed after node delete"

echo "PASS: test_docker_network.sh"
