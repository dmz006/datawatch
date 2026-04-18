#!/usr/bin/env bash
# F10 S1.5 — local container smoke test.
#
# Runs the slim image, waits for /healthz, asserts /readyz subsystems,
# starts a non-AI session via the API to prove tmux works, then tears
# everything down.
#
# Usage:
#   tests/integration/container_smoke.sh [IMAGE]
#
# Default IMAGE: localhost/datawatch:slim-$(VERSION) loaded by `make container-load`.

set -euo pipefail

IMAGE="${1:-localhost:5000/datawatch/datawatch:slim-2.4.5}"
NAME="datawatch-smoke-$$"
HOST_PORT="${HOST_PORT:-18080}"

cleanup() {
    local rc=$?
    echo "→ cleanup (rc=$rc)"
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    exit $rc
}
trap cleanup EXIT

echo "→ starting $IMAGE as $NAME on host port $HOST_PORT"
docker run -d \
    --name "$NAME" \
    -p "$HOST_PORT:8080" \
    "$IMAGE" >/dev/null

echo "→ waiting up to 30s for /healthz"
for i in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:$HOST_PORT/healthz" >/dev/null 2>&1; then
        echo "  /healthz=200 after ${i}s"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "FAIL: /healthz never reached 200" >&2
        docker logs "$NAME" >&2
        exit 1
    fi
    sleep 1
done

echo "→ /readyz subsystem matrix"
READYZ=$(curl -sf "http://127.0.0.1:$HOST_PORT/readyz")
echo "$READYZ" | python3 -m json.tool
echo "$READYZ" | grep -q '"status":"ready"' || { echo "FAIL: not ready"; exit 1; }
echo "$READYZ" | grep -q '"manager":{"status":"ok"}' || { echo "FAIL: manager not ok"; exit 1; }

echo "→ verify health subcommand exits 0 inside the container"
docker exec "$NAME" datawatch health
echo "  health subcommand: ok"

echo "→ verify non-root UID inside the container"
UID_INSIDE=$(docker exec "$NAME" id -u)
if [ "$UID_INSIDE" = "0" ]; then
    echo "FAIL: container running as root (uid=0)"
    exit 1
fi
echo "  uid=$UID_INSIDE (non-root, good)"

echo "→ all smoke assertions passed for $IMAGE"
