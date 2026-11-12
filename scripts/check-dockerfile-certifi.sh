#!/usr/bin/env bash
# scripts/check-dockerfile-certifi.sh — guards the certifi floor pinned in
# docker/dockerfiles/Dockerfile.agent-aider (GHSA-6q48-6c7w-9gw2). Fails if
# the `certifi>=X` pin is absent or X sorts below the minimum.
#
# Usage: scripts/check-dockerfile-certifi.sh [--self-test]

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MIN_VERSION="2026.10.19"
DOCKERFILE="docker/dockerfiles/Dockerfile.agent-aider"

# check_file path — prints result; returns 0 on pass, 1 on fail.
check_file() {
    local file="$1" pinned lower
    pinned=$(grep -oE 'certifi>=[0-9][0-9A-Za-z.]*' "$file" | head -1 | sed -E 's/^certifi>=//' || true)
    if [[ -z "$pinned" ]]; then
        echo "✗ FAIL: $file has no 'certifi>=X' pin (need >= $MIN_VERSION)" >&2
        return 1
    fi
    lower=$(printf '%s\n%s\n' "$pinned" "$MIN_VERSION" | sort -V | head -1)
    if [[ "$pinned" != "$MIN_VERSION" && "$lower" == "$pinned" ]]; then
        echo "✗ FAIL: $file pins certifi>=$pinned, below the required floor $MIN_VERSION (GHSA-6q48-6c7w-9gw2)" >&2
        return 1
    fi
    echo "✓ PASS: $file pins certifi>=$pinned (>= $MIN_VERSION)"
    return 0
}

self_test() {
    local rc=0
    tmp=""
    tmp=$(mktemp -d)
    trap 'rm -rf "${tmp:-}"' EXIT

    printf 'RUN pip install \\\n  "certifi>=2025.8.3" \\\n  aider-chat\n' > "$tmp/low"
    printf 'RUN pip install \\\n  aider-chat\n' > "$tmp/none"
    printf 'RUN pip install \\\n  "certifi>=%s" \\\n  aider-chat\n' "$MIN_VERSION" > "$tmp/good"

    if check_file "$tmp/low" >/dev/null 2>&1; then echo "✗ self-test: low pin should fail" >&2; rc=1; fi
    if check_file "$tmp/none" >/dev/null 2>&1; then echo "✗ self-test: missing pin should fail" >&2; rc=1; fi
    if ! check_file "$tmp/good" >/dev/null 2>&1; then echo "✗ self-test: good pin should pass" >&2; rc=1; fi

    if [[ $rc -eq 0 ]]; then
        echo "✓ PASS: self-test (low pin fails, no pin fails, good pin passes)"
    fi
    return $rc
}

if [[ "${1:-}" == "--self-test" ]]; then
    self_test
    exit $?
fi

cd "$ROOT"
check_file "$DOCKERFILE" || exit 1
