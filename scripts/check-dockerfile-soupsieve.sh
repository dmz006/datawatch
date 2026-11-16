#!/usr/bin/env bash
# scripts/check-dockerfile-soupsieve.sh — guards the soupsieve floor pinned in
# docker/dockerfiles/Dockerfile.agent-aider (soupsieve 4 CVE fix; needs Python
# >= 3.11.5, which is why the aider venv runs on a uv-managed Python 3.12).
# Fails if the `soupsieve>=X` pin is absent or X sorts below the minimum.
#
# Usage: scripts/check-dockerfile-soupsieve.sh [--self-test]

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MIN_VERSION="4.0.1"
DOCKERFILE="docker/dockerfiles/Dockerfile.agent-aider"

# check_file path — prints result; returns 0 on pass, 1 on fail.
# Uses the LAST `soupsieve>=X` pin: the floor is forced by the final install step.
check_file() {
    local file="$1" pinned lower
    pinned=$(grep -oE 'soupsieve>=[0-9][0-9A-Za-z.]*' "$file" | tail -1 | sed -E 's/^soupsieve>=//' || true)
    if [[ -z "$pinned" ]]; then
        echo "✗ FAIL: $file has no 'soupsieve>=X' pin (need >= $MIN_VERSION)" >&2
        return 1
    fi
    lower=$(printf '%s\n%s\n' "$pinned" "$MIN_VERSION" | sort -V | head -1)
    if [[ "$pinned" != "$MIN_VERSION" && "$lower" == "$pinned" ]]; then
        echo "✗ FAIL: $file pins soupsieve>=$pinned, below the required floor $MIN_VERSION" >&2
        return 1
    fi
    echo "✓ PASS: $file pins soupsieve>=$pinned (>= $MIN_VERSION)"
    return 0
}

self_test() {
    local rc=0
    tmp=""
    tmp=$(mktemp -d)
    trap 'rm -rf "${tmp:-}"' EXIT

    printf 'RUN pip install \\\n  "soupsieve>=2.8.4" \\\n  aider-chat\n' > "$tmp/low"
    printf 'RUN pip install \\\n  aider-chat\n' > "$tmp/none"
    printf 'RUN pip install \\\n  "soupsieve>=%s" \\\n  aider-chat\n' "$MIN_VERSION" > "$tmp/good"
    # Several pins: the last one found is the one that counts.
    printf 'RUN pip install "soupsieve>=2.8.4"\nRUN pip install "soupsieve>=%s"\n' "$MIN_VERSION" > "$tmp/last-good"
    printf 'RUN pip install "soupsieve>=%s"\nRUN pip install "soupsieve>=2.8.4"\n' "$MIN_VERSION" > "$tmp/last-low"

    if check_file "$tmp/low" >/dev/null 2>&1; then echo "✗ self-test: low pin should fail" >&2; rc=1; fi
    if check_file "$tmp/none" >/dev/null 2>&1; then echo "✗ self-test: missing pin should fail" >&2; rc=1; fi
    if ! check_file "$tmp/good" >/dev/null 2>&1; then echo "✗ self-test: good pin should pass" >&2; rc=1; fi
    if ! check_file "$tmp/last-good" >/dev/null 2>&1; then echo "✗ self-test: good last pin should pass" >&2; rc=1; fi
    if check_file "$tmp/last-low" >/dev/null 2>&1; then echo "✗ self-test: low last pin should fail" >&2; rc=1; fi

    if [[ $rc -eq 0 ]]; then
        echo "✓ PASS: self-test (low pin fails, no pin fails, good pin passes, last pin decides)"
    fi
    return $rc
}

if [[ "${1:-}" == "--self-test" ]]; then
    self_test
    exit $?
fi

cd "$ROOT"
check_file "$DOCKERFILE" || exit 1
