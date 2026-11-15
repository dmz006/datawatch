#!/usr/bin/env bash
# Scan a local docker image with trivy by archive instead of by daemon.
#
# Usage: scripts/trivy-scan-image.sh <image-ref>
#
# Under docker 29.1.x trivy cannot read image layers from the docker daemon
# ("failed to analyze layer"), so this helper saves the image to a tar archive
# with `docker save` and scans the archive with `trivy image --input`. No
# network pulls are made; the image must already exist locally.
#
# Exit code: trivy's exit code unchanged (0 clean, 1 HIGH/CRITICAL finding not
# in the ignore file); 2 for usage errors or missing tools; 3 when docker save
# fails.
#
# Environment:
#   TRIVY_SCAN_TMPDIR  base directory for the temporary archive
#                      (default: <repo>/.datawatch/tmp). Removed on exit.
#   TRIVY_IGNOREFILE   ignore file override, for tests only
#                      (default: .trivyignore).
set -uo pipefail

if [ "$#" -ne 1 ] || [ -z "${1:-}" ]; then
  echo "usage: $0 <image-ref>" >&2
  exit 2
fi
image="$1"

docker_bin="$(command -v docker)" || { echo "trivy-scan-image: docker not found" >&2; exit 2; }
trivy_bin="$(command -v trivy)" || { echo "trivy-scan-image: trivy not found" >&2; exit 2; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base="${TRIVY_SCAN_TMPDIR:-$repo_root/.datawatch/tmp}"
mkdir -p "$base" || { echo "trivy-scan-image: cannot create $base" >&2; exit 2; }
dir="$(mktemp -d "$base/trivy-scan.XXXXXX")" || { echo "trivy-scan-image: cannot create temp dir under $base" >&2; exit 2; }

cleanup() { rm -rf "$dir"; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

tar="$dir/image.tar"
if ! "$docker_bin" save -o "$tar" "$image"; then
  echo "trivy-scan-image: docker save failed for $image" >&2
  exit 3
fi

"$trivy_bin" image --input "$tar" --severity HIGH,CRITICAL \
  --ignorefile "${TRIVY_IGNOREFILE:-.trivyignore}" --exit-code 1 --format table
exit $?
