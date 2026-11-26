#!/usr/bin/env bash
# The rest of what CI, the release workflow and the security scans check on a
# push, run before it.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_var MERGE_BASE_COMMIT
for tool in go python3 govulncheck gosec gitleaks goreleaser; do
    merge_need_tool "$tool"
done
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

step() {
    local name=$1
    shift
    echo "== $name"
    if ! "$@"; then
        echo "GATE FAILED: $name" >&2
        exit 1
    fi
}

shipped_go=go$(awk '/^go /{print $2; exit}' go.mod)

step docs-sync bash -c 'make sync-docs >/dev/null && bash scripts/check-docs-sync.sh'
step docs-index make docs-index
if [[ "${MERGE_RELEASE_SIZE:-}" != none ]]; then
    step version-bump bash scripts/check-version-bump.sh "$MERGE_BASE_COMMIT"
fi
step tmux-kill bash scripts/check-no-unscoped-tmux.sh
step dockerfile-go-versions bash scripts/check-dockerfile-go-versions.sh
step go-toolchain-pins bash -c 'bash scripts/check-go-toolchain-pins.sh && bash scripts/check-go-toolchain-pins.sh --self-test'
step trivy-single-version bash -c 'bash scripts/check-trivy-version-single.sh && bash scripts/tests/check-trivy-version-single_test.sh'
step certifi-floor bash -c 'bash scripts/check-dockerfile-certifi.sh && bash scripts/check-dockerfile-certifi.sh --self-test'
step go-mod-tidy bash -c 'go mod tidy && git diff --exit-code go.mod go.sum'
step govulncheck env GOTOOLCHAIN="$shipped_go" govulncheck ./...

gosec_gate() {
    local exclude
    exclude=$(grep -v '^#' .gosec-exclude | tr '\n' ',' | sed 's/,$//')
    gosec -exclude="$exclude" -exclude-dir=.claude -exclude-dir=.datawatch/eval -severity=high -confidence=medium -fmt=json ./... >"$work/gosec.json" 2>/dev/null || true
    python3 -I - "$work/gosec.json" .gosec-baseline.json <<'PY'
import json, sys
live = json.load(open(sys.argv[1]))
base = json.load(open(sys.argv[2]))
found = len(live.get("Issues", []))
print("gosec: found=%d baseline=%d" % (found, base["total"]))
sys.exit(1 if found > base["total"] else 0)
PY
}
step gosec gosec_gate
step gitleaks gitleaks detect --source . --redact --exit-code 1
step security-registry python3 scripts/check_accepted_risks.py --base "$MERGE_BASE_COMMIT"
step security-registry-generated bash -c 'python3 scripts/gen_trivyignore.py && git diff --exit-code .trivyignore docs/security-review.md'
step release-build goreleaser build --snapshot --clean

if git diff --name-only "$MERGE_BASE_COMMIT" HEAD | grep -qE '(^|/)Dockerfile[^/]*$|^\.github/workflows/containers\.yaml$|^security/accepted-risks\.yml$'; then
    merge_need_tool docker
    image="datawatch-merge-agent-base:$(git rev-parse --short HEAD)"
    trap 'docker rmi -f "$image" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
    step agent-base-build timeout 900 docker build -f docker/dockerfiles/Dockerfile.agent-base --build-arg VERSION=merge-check -t "$image" .
    step agent-base-scan timeout 900 bash scripts/trivy-scan-image.sh "$image"
fi
echo "parity gates: all passed"
