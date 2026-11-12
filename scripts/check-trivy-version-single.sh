#!/usr/bin/env bash
# scripts/check-trivy-version-single.sh — enforces a single source of truth
# for the pinned Trivy version:
#   1. scripts/trivy-version.txt must exist and hold exactly one vX.Y.Z tag.
#   2. No .github/workflows/*.y*ml line may carry a `TRIVY_VERSION:` value or
#      pass a literal version tag (v0.NN.N) to scripts/ci-install-trivy.sh.
#   3. Every workflow that calls scripts/ci-install-trivy.sh must read
#      scripts/trivy-version.txt.
#
# Usage: scripts/check-trivy-version-single.sh [repo-root]
#   repo-root defaults to the git top level (tests pass a fixture tree).

set -euo pipefail

ROOT="${1:-}"
if [[ -z "$ROOT" ]]; then
    ROOT=$(git rev-parse --show-toplevel)
fi

fail=0
VERSION_FILE="scripts/trivy-version.txt"

# (a) the version file
if [[ ! -f "$ROOT/$VERSION_FILE" ]]; then
    echo "✗ FAIL: $VERSION_FILE is missing" >&2
    fail=1
else
    nonblank=$(grep -c '[^[:space:]]' "$ROOT/$VERSION_FILE" || true)
    first=$(grep -m1 '[^[:space:]]' "$ROOT/$VERSION_FILE" | tr -d '[:space:]' || true)
    if [[ "$nonblank" != "1" ]] || [[ ! "$first" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "✗ FAIL: $VERSION_FILE:1 must hold a single vX.Y.Z tag (found '$first', $nonblank non-blank line(s))" >&2
        fail=1
    fi
fi

shopt -s nullglob
for wf in "$ROOT"/.github/workflows/*.y*ml; do
    rel=".github/workflows/$(basename "$wf")"
    # (b) literal pins
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        n=${hit%%:*}
        echo "✗ FAIL: $rel:$n carries a TRIVY_VERSION value — read $VERSION_FILE instead" >&2
        fail=1
    done < <(grep -nE '^[[:space:]#-]*TRIVY_VERSION:[[:space:]]*[^[:space:]#]' "$wf" || true)
    while IFS= read -r hit; do
        [[ -z "$hit" ]] && continue
        n=${hit%%:*}
        echo "✗ FAIL: $rel:$n passes a literal version tag to scripts/ci-install-trivy.sh — read $VERSION_FILE instead" >&2
        fail=1
    done < <(grep -nE 'ci-install-trivy\.sh[[:space:]]+["'"'"']?v[0-9]+\.[0-9]+\.[0-9]+' "$wf" || true)
    # (c) installer callers must read the version file
    if grep -q 'scripts/ci-install-trivy\.sh' "$wf" && ! grep -q 'scripts/trivy-version\.txt' "$wf"; then
        n=$(grep -n -m1 'scripts/ci-install-trivy\.sh' "$wf" | cut -d: -f1)
        echo "✗ FAIL: $rel:$n calls scripts/ci-install-trivy.sh but never reads $VERSION_FILE" >&2
        fail=1
    fi
done

if (( fail )); then
    exit 1
fi
echo "✓ OK: Trivy version is pinned only in $VERSION_FILE"
