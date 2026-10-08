#!/usr/bin/env bash
# scripts/check-version-bump.sh — enforces AGENT.md's Versioning rule:
# cmd/datawatch/main.go and internal/server/api.go must carry the same
# `var Version`, and a commit that changes behavior must not reuse the
# version already used by its base commit.
#
# Found by a 2026-10-07 compliance audit: commit 71397878 made
# substantive changes (new metrics, a new PWA card, a new locale key, a
# gosec suppression) but reused the version label its own parent commit
# had already used. This check exists so that stops happening silently.
#
# Usage:
#   scripts/check-version-bump.sh [base-ref]
# base-ref defaults to HEAD^ (the previous commit). In CI, pass the
# merge-base of the PR/push against the target branch instead.
#
# A base-ref with no history (shallow clone, first commit) is treated as
# "nothing to compare" and the check passes trivially — this script
# only catches reuse, it is not a substitute for the full A3 pre-commit
# check in AGENT.md (which also requires the two files to literally
# match on every commit, not just differ from the base).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASE_REF="${1:-HEAD^}"

extract_version() {
    # $1 = git ref ("" means working tree), $2 = file path
    local ref="$1" file="$2"
    if [[ -z "$ref" ]]; then
        grep -E '^var Version = "' "$file" | head -1 | sed -E 's/.*"([^"]+)".*/\1/'
    else
        git show "${ref}:${file}" 2>/dev/null | grep -E '^var Version = "' | head -1 | sed -E 's/.*"([^"]+)".*/\1/'
    fi
}

MAIN_FILE="cmd/datawatch/main.go"
API_FILE="internal/server/api.go"

CUR_MAIN=$(extract_version "" "$MAIN_FILE")
CUR_API=$(extract_version "" "$API_FILE")

if [[ -z "$CUR_MAIN" || -z "$CUR_API" ]]; then
    echo "✗ FAIL: could not find 'var Version = \"...\"' in $MAIN_FILE and/or $API_FILE"
    exit 1
fi

if [[ "$CUR_MAIN" != "$CUR_API" ]]; then
    echo "✗ FAIL: version mismatch — $MAIN_FILE has $CUR_MAIN, $API_FILE has $CUR_API"
    echo "  AGENT.md Versioning rule: both files MUST match on every commit."
    exit 1
fi

echo "==> Version check: $CUR_MAIN (both files match)"

if ! git rev-parse --verify "$BASE_REF" >/dev/null 2>&1; then
    echo "  (base ref '$BASE_REF' not resolvable — shallow history, skipping reuse check)"
    exit 0
fi

BASE_MAIN=$(extract_version "$BASE_REF" "$MAIN_FILE" || true)

if [[ -z "$BASE_MAIN" ]]; then
    echo "  (no prior version found at $BASE_REF — nothing to compare)"
    exit 0
fi

if [[ "$CUR_MAIN" != "$BASE_MAIN" ]]; then
    echo "✓ PASS: version bumped ($BASE_MAIN -> $CUR_MAIN)"
    exit 0
fi

# Version unchanged from base. That's fine ONLY if every changed file is
# a recognized zero-behavior-change path (tests, plan docs, chore/CI) —
# the one exception AGENT.md's Versioning rule carves out.
CHANGED=$(git diff --name-only "$BASE_REF" HEAD 2>/dev/null || true)
if [[ -z "$CHANGED" ]]; then
    # Nothing changed between base and HEAD on disk (e.g. comparing
    # working tree against itself) — not a reuse, just unchanged.
    exit 0
fi

NON_EXEMPT=$(echo "$CHANGED" | grep -vE '_test\.go$|_test\.js$|^docs/plans/|\.github/workflows/|^\.golangci|^\.gosec-exclude$' || true)

if [[ -z "$NON_EXEMPT" ]]; then
    echo "✓ PASS: version unchanged ($CUR_MAIN), but every changed file is test-only/plan-doc/chore (recognized exception)"
    exit 0
fi

echo "✗ FAIL: version $CUR_MAIN reused from base $BASE_REF — these files changed without a version bump:"
echo "$NON_EXEMPT" | sed 's/^/  - /'
echo "  AGENT.md Versioning rule: multi-commit features bump on every commit; only"
echo "  test-only/plan-doc/chore-only commits may skip it."
exit 1
