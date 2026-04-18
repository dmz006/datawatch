#!/usr/bin/env bash
# F10 S1.5b — kubectl smoke test.
#
# Creates a Pod from the slim image in the current kubectl context's "default"
# namespace (override with NAMESPACE=...), waits for it to become Ready, hits
# /healthz via port-forward, then deletes the pod.
#
# Pre-req:
#   - kubectl context set to a writable cluster (`kubectl config current-context`)
#   - Image already pushed to a reachable registry (default: harbor.dmzs.com)
#     OR pre-loaded via tarball if testing air-gap.
#
# Usage:
#   tests/integration/k8s_smoke.sh [IMAGE]

set -euo pipefail

IMAGE="${1:-harbor.dmzs.com/datawatch/datawatch:slim-2.4.5}"
NAMESPACE="${NAMESPACE:-default}"
NAME="datawatch-smoke-$$"
LOCAL_PORT="${LOCAL_PORT:-18080}"

# Refuse to run against contexts whose name contains "prod" — cheap safety.
CTX=$(kubectl config current-context)
if echo "$CTX" | grep -qi prod; then
    echo "REFUSING to smoke test against context '$CTX' (contains 'prod')" >&2
    exit 2
fi

cleanup() {
    local rc=$?
    echo "→ cleanup (rc=$rc)"
    [ -n "${PF_PID:-}" ] && kill "$PF_PID" 2>/dev/null || true
    kubectl delete pod "$NAME" -n "$NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    exit $rc
}
trap cleanup EXIT

echo "→ context=$CTX namespace=$NAMESPACE image=$IMAGE"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: $NAME
  namespace: $NAMESPACE
  labels:
    app: datawatch
    role: smoke-test
spec:
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    runAsGroup: 10001
    fsGroup: 10001
  containers:
  - name: datawatch
    image: $IMAGE
    imagePullPolicy: IfNotPresent
    ports:
    - containerPort: 8080
      name: http
    livenessProbe:
      exec:
        command: ["datawatch", "health"]
      initialDelaySeconds: 10
      periodSeconds: 30
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 5
    resources:
      requests: { cpu: "50m", memory: "64Mi" }
      limits:   { cpu: "500m", memory: "256Mi" }
EOF

echo "→ waiting up to 90s for Pod Ready"
kubectl wait --for=condition=Ready pod/"$NAME" -n "$NAMESPACE" --timeout=90s

echo "→ port-forward 127.0.0.1:$LOCAL_PORT → pod:8080"
kubectl port-forward -n "$NAMESPACE" "pod/$NAME" "$LOCAL_PORT:8080" >/dev/null 2>&1 &
PF_PID=$!
sleep 2

echo "→ /healthz check"
curl -sf "http://127.0.0.1:$LOCAL_PORT/healthz" | python3 -m json.tool

echo "→ /readyz subsystem matrix"
curl -sf "http://127.0.0.1:$LOCAL_PORT/readyz" | python3 -m json.tool

echo "→ confirm non-root inside the pod"
UID_INSIDE=$(kubectl exec -n "$NAMESPACE" "$NAME" -- id -u)
[ "$UID_INSIDE" = "10001" ] || { echo "FAIL: uid=$UID_INSIDE expected 10001"; exit 1; }

echo "→ k8s smoke passed"
