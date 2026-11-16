#!/usr/bin/env bash
# Prove scripts/trivy-scan-image.sh passes a clean image and fails a vulnerable one.
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
for t in docker trivy; do
  command -v "$t" >/dev/null 2>&1 || { echo "SKIP: $t not found"; exit 0; }
done
clean=datawatch-trivy-fixture:clean
vuln=datawatch-trivy-fixture:vulnerable
cleanup() { docker rmi -f "$clean" "$vuln" >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
docker build -q -f "$here/Dockerfile.clean" -t "$clean" "$here" || { echo "build clean failed"; exit 1; }
docker build -q -f "$here/Dockerfile.vulnerable" -t "$vuln" "$here" || { echo "build vulnerable failed"; exit 1; }
export TRIVY_IGNOREFILE="$here/empty.trivyignore"
cd "$repo"
bash scripts/trivy-scan-image.sh "$clean"; rc_clean=$?
bash scripts/trivy-scan-image.sh "$vuln"; rc_vuln=$?
echo "clean exit code: $rc_clean"
echo "vulnerable exit code: $rc_vuln"
if [ "$rc_clean" -eq 0 ] && [ "$rc_vuln" -eq 1 ]; then echo PASS; exit 0; fi
echo FAIL; exit 1
