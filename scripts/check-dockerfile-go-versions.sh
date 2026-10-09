#!/usr/bin/env bash
# scripts/check-dockerfile-go-versions.sh — catches a Dockerfile's
# exact-pinned `ARG GO_VERSION=X.Y.Z` falling behind go.mod's `go`
# directive.
#
# Found by the v9.0.0 release run (2026-10-09): docker/dockerfiles/
# Dockerfile.agent-base pinned GO_VERSION=1.26.6 while go.mod had
# moved to `go 1.26.9`. `go mod download` fails outright in that
# image's builder stage once the pinned Go toolchain is older than
# go.mod requires (GOTOOLCHAIN=local, deliberately, to avoid auto-
# downloading an unpinned toolchain at build time — see the comment
# in Dockerfile.agent-base). Because every agent-* image builds FROM
# agent-base, that one failure cascaded into 6 more images being
# skipped entirely in CI, and it wasn't caught until the actual
# release workflow ran against a pushed tag.
#
# A floating two-component pin (e.g. `ARG GO_VERSION=1.26`, no patch)
# always resolves to the latest patch in that minor line at build
# time, so it can't drift behind go.mod this way — only exact
# three-component pins are checked here.
#
# Usage: scripts/check-dockerfile-go-versions.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GOMOD_VERSION=$(grep -E '^go [0-9]+\.[0-9]+\.[0-9]+$' go.mod | head -1 | awk '{print $2}')

if [[ -z "$GOMOD_VERSION" ]]; then
    echo "✗ FAIL: could not find a 'go X.Y.Z' directive in go.mod" >&2
    exit 1
fi

# version_lt a b — true if semver a < b (both "X.Y.Z", no pre-release).
version_lt() {
    [[ "$1" == "$2" ]] && return 1
    local lower
    lower=$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)
    [[ "$lower" == "$1" ]]
}

FAIL=0
while IFS= read -r -d '' file; do
    while IFS= read -r pinned; do
        [[ "$pinned" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || continue  # skip floating X.Y pins
        if version_lt "$pinned" "$GOMOD_VERSION"; then
            echo "✗ FAIL: $file pins GO_VERSION=$pinned, but go.mod requires >= $GOMOD_VERSION" >&2
            FAIL=1
        fi
    done < <(grep -oE '^ARG GO_VERSION=[0-9]+\.[0-9]+(\.[0-9]+)?' "$file" | sed -E 's/^ARG GO_VERSION=//')
done < <(find docker/dockerfiles -maxdepth 1 -type f -print0)

if [[ $FAIL -ne 0 ]]; then
    echo "  Fix: bump the ARG GO_VERSION pin to >= $GOMOD_VERSION (confirm the" >&2
    echo "  exact tag exists on Docker Hub for both amd64/arm64 before committing)." >&2
    exit 1
fi

echo "✓ PASS: all exact-pinned Dockerfile GO_VERSION args are >= go.mod's $GOMOD_VERSION"
