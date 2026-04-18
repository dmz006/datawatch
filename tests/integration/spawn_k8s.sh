#!/usr/bin/env bash
# F10 S4.5 — end-to-end smoke for the k8s agent spawn flow.
#
# Parallel of spawn_docker.sh. Walks the same REST chain but drives a
# Cluster Profile with kind=k8s, so the K8s driver shells out to
# kubectl to actually create + delete a Pod. Pairs with the S4.1 K8s
# driver + S4.4 Helm chart: run this against a parent installed via
# `helm install dw ./charts/datawatch`.
#
#   1. POST /api/profiles/project + /api/profiles/cluster (kind=k8s)
#   2. POST /api/agents → driver shells out to `kubectl apply -f -`
#   3. kubectl get pod -l datawatch.agent_id=<id> — confirm Pod exists
#   4. POST /api/agents/bootstrap with bad token → 401
#   5. [optional RUN_BOOTSTRAP=1] poll state=ready with a real
#      worker image that actually calls bootstrap back
#   6. DELETE /api/agents/{id} → kubectl deletes Pod
#   7. Confirm Pod gone; delete profiles
#
# Default IMAGE is busybox:latest + `sleep infinity` — the Pod just
# needs to exist so Terminate can reap it. Use RUN_BOOTSTRAP=1 with a
# real datawatch worker image to exercise the bootstrap leg.
#
# Env knobs:
#   BASE_URL         default http://127.0.0.1:8080
#   KUBE_CONTEXT     default (empty; uses current-context)
#   NAMESPACE        default datawatch-smoke
#   IMAGE            default busybox:latest
#   RUN_BOOTSTRAP    default unset; "1" waits for state=ready
#   KEEP_ON_FAIL     default unset; "1" preserves state on failure

set -euo pipefail

BASE_URL="${1:-${BASE_URL:-http://127.0.0.1:8080}}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
NAMESPACE="${NAMESPACE:-datawatch-smoke}"
IMAGE="${IMAGE:-busybox:latest}"
RUN_BOOTSTRAP="${RUN_BOOTSTRAP:-}"

SMOKE_ID="$$-$(date +%s)"
PROJECT_NAME="smoke-k8s-project-${SMOKE_ID}"
CLUSTER_NAME="smoke-k8s-cluster-${SMOKE_ID}"
AGENT_ID=""

pass() { echo "✓ $*"; }
fail() { echo "✗ $*" >&2; exit 1; }

kubectl_args=()
if [ -n "$KUBE_CONTEXT" ]; then
    kubectl_args+=("--context" "$KUBE_CONTEXT")
fi

cleanup() {
    local rc=$?
    if [ "${KEEP_ON_FAIL:-}" = "1" ] && [ "$rc" -ne 0 ]; then
        echo "⚠ preserving state on failure (KEEP_ON_FAIL=1)"
        echo "  project=$PROJECT_NAME  cluster=$CLUSTER_NAME  agent=$AGENT_ID"
        exit "$rc"
    fi
    echo "→ cleanup"
    if [ -n "$AGENT_ID" ]; then
        curl -sf -X DELETE "$BASE_URL/api/agents/$AGENT_ID" >/dev/null 2>&1 || true
        # Belt + braces: reap any Pod by label if the REST delete missed it.
        kubectl "${kubectl_args[@]}" -n "$NAMESPACE" delete pod \
            -l "datawatch.agent_id=$AGENT_ID" --ignore-not-found --grace-period=0 --force >/dev/null 2>&1 || true
    fi
    curl -sf -X DELETE "$BASE_URL/api/profiles/project/$PROJECT_NAME" >/dev/null 2>&1 || true
    curl -sf -X DELETE "$BASE_URL/api/profiles/cluster/$CLUSTER_NAME" >/dev/null 2>&1 || true
    exit "$rc"
}
trap cleanup EXIT

require() { command -v "$1" >/dev/null 2>&1 || fail "prereq missing: $1"; }
require curl
require jq
require kubectl

echo "→ preflight: $BASE_URL reachable + kubectl apiserver reachable"
curl -sf "$BASE_URL/healthz" >/dev/null || fail "daemon not reachable at $BASE_URL"
kubectl "${kubectl_args[@]}" cluster-info --request-timeout=5s >/dev/null \
    || fail "kubectl can't reach apiserver (context=${KUBE_CONTEXT:-default})"
pass "daemon + apiserver reachable"

echo "→ ensure namespace $NAMESPACE exists"
kubectl "${kubectl_args[@]}" get ns "$NAMESPACE" >/dev/null 2>&1 \
    || kubectl "${kubectl_args[@]}" create namespace "$NAMESPACE" >/dev/null
pass "namespace $NAMESPACE"

echo "→ step 1: create project + cluster profiles (kind=k8s)"
curl -sf -X POST "$BASE_URL/api/profiles/project" \
    -H 'Content-Type: application/json' \
    -d "$(jq -n --arg name "$PROJECT_NAME" '{
        name: $name,
        git: {url: "https://github.com/example/smoke.git"},
        image_pair: {agent: "agent-claude"},
        memory: {mode: "ephemeral"}
    }')" >/dev/null || fail "project profile create"
pass "project profile $PROJECT_NAME"

curl -sf -X POST "$BASE_URL/api/profiles/cluster" \
    -H 'Content-Type: application/json' \
    -d "$(jq -n \
        --arg name "$CLUSTER_NAME" \
        --arg ctx "$KUBE_CONTEXT" \
        --arg ns "$NAMESPACE" \
        '{name: $name, kind: "k8s", context: $ctx, namespace: $ns}')" \
    >/dev/null || fail "cluster profile create"
pass "cluster profile $CLUSTER_NAME (ns=$NAMESPACE, ctx=${KUBE_CONTEXT:-default})"

echo "→ step 2: spawn an agent (driver shells out to kubectl apply)"
SPAWN_RESP=$(curl -sf -X POST "$BASE_URL/api/agents" \
    -H 'Content-Type: application/json' \
    -d "$(jq -n \
        --arg pp "$PROJECT_NAME" \
        --arg cp "$CLUSTER_NAME" \
        '{project_profile: $pp, cluster_profile: $cp, task: "k8s smoke ping"}')") \
    || fail "agent spawn"

AGENT_ID=$(echo "$SPAWN_RESP" | jq -r '.id // empty')
[ -n "$AGENT_ID" ] || fail "spawn response missing id: $SPAWN_RESP"
pass "agent spawned id=$AGENT_ID"

echo "→ step 3: Pod visible via kubectl label selector"
for i in $(seq 1 15); do
    if kubectl "${kubectl_args[@]}" -n "$NAMESPACE" get pod \
        -l "datawatch.agent_id=$AGENT_ID" -o name 2>/dev/null | grep -q pod/; then
        pass "Pod present after ${i}s"
        break
    fi
    [ "$i" -eq 15 ] && fail "Pod never appeared"
    sleep 1
done

echo "→ step 4: bootstrap token validation (bad token → 401)"
REJECT_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
    -X POST "$BASE_URL/api/agents/bootstrap" \
    -H 'Content-Type: application/json' \
    -d "{\"agent_id\":\"$AGENT_ID\",\"token\":\"deadbeef\"}")
[ "$REJECT_CODE" = "401" ] || fail "bootstrap accepted bad token (got HTTP $REJECT_CODE)"
pass "bootstrap correctly rejects bad token"

if [ "$RUN_BOOTSTRAP" = "1" ]; then
    echo "→ step 4b: RUN_BOOTSTRAP=1 — wait up to 120s for state=ready"
    for i in $(seq 1 120); do
        STATE=$(curl -sf "$BASE_URL/api/agents/$AGENT_ID" | jq -r '.state')
        case "$STATE" in
            ready|running) pass "agent reached state=$STATE after ${i}s"; break ;;
            failed) fail "agent failed: $(curl -sf "$BASE_URL/api/agents/$AGENT_ID" | jq -r '.failure_reason')" ;;
        esac
        [ "$i" -eq 120 ] && fail "agent never became ready (last state: $STATE)"
        sleep 1
    done
fi

echo "→ step 5: terminate agent via REST"
TERM_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
    -X DELETE "$BASE_URL/api/agents/$AGENT_ID")
[ "$TERM_CODE" = "204" ] || fail "terminate returned HTTP $TERM_CODE"
pass "agent terminated"

echo "→ step 6: Pod reaped"
for i in $(seq 1 10); do
    if ! kubectl "${kubectl_args[@]}" -n "$NAMESPACE" get pod \
        -l "datawatch.agent_id=$AGENT_ID" -o name 2>/dev/null | grep -q pod/; then
        pass "Pod removed after ${i}s"
        AGENT_ID=""
        break
    fi
    sleep 1
done
[ -n "$AGENT_ID" ] && fail "Pod still present after terminate"

echo "→ step 7: profile cleanup"
curl -sf -X DELETE "$BASE_URL/api/profiles/project/$PROJECT_NAME" >/dev/null || fail "project delete"
curl -sf -X DELETE "$BASE_URL/api/profiles/cluster/$CLUSTER_NAME" >/dev/null || fail "cluster delete"
pass "profiles removed"

echo
echo "✅ spawn_k8s smoke passed"
