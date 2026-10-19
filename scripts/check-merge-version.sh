#!/usr/bin/env bash
# scripts/check-merge-version.sh — before a branch is merged into main,
# decides whether the merge step may set the version for it.
#
# AGENT.md's Versioning rule keeps `var Version` in
# cmd/datawatch/main.go and internal/server/api.go identical. When a
# branch is merged, the merge step sets the next patch version on main.
# This check makes sure that can happen without a clash:
#
#   M  version on main (both files must agree there)
#   F  version at the merge base of main and BRANCH
#   B  version at the tip of BRANCH (both files must agree there)
#   E  expected version: M with the patch number increased by one
#
# Outcomes:
#   B == F   the branch did not change the version; the merge step will
#            set it to E. Pass (exit 0).
#   B == E   the branch already carries the expected version. Pass (exit 0).
#   otherwise the branch set its own version B, which conflicts with the
#            merge step's choice. Refuse (exit 1).
#
# Either file disagreeing with the other on main or on BRANCH is also a
# refusal (exit 1).
#
# Usage: bash scripts/check-merge-version.sh BRANCH
#        MAIN_BRANCH (environment variable, default: main) names the
#        branch the merge goes into.
#
# Version lines are read with grep and sed only; nothing here touches
# tmux or services.
#
# The script cds to its own repository root ($(dirname "$0")/..), so it
# can be run from any directory inside the repo.

set -euo pipefail

cd "$(dirname "$0")/.."

MAIN_FILE="cmd/datawatch/main.go"
API_FILE="internal/server/api.go"
MAIN_BRANCH="${MAIN_BRANCH:-main}"

if [[ $# -ne 1 || -z "$1" ]]; then
    echo "usage: bash scripts/check-merge-version.sh BRANCH" >&2
    exit 1
fi
BRANCH="$1"

# --- helpers ---------------------------------------------------------------
extract_version() {
    # $1 = git ref, $2 = file path; prints the Version string or nothing.
    local ref="$1" file="$2" content
    content=$(git show "${ref}:${file}" 2>/dev/null || true)
    printf '%s\n' "$content" | grep -E '^var Version = "' | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || true
}

next_patch() {
    # $1 = major.minor.patch; prints major.minor.(patch+1).
    local v="$1" major minor patch
    major=$(printf '%s\n' "$v" | sed -E 's/^([0-9]+)\.([0-9]+)\.([0-9]+).*/\1/')
    minor=$(printf '%s\n' "$v" | sed -E 's/^([0-9]+)\.([0-9]+)\.([0-9]+).*/\2/')
    patch=$(printf '%s\n' "$v" | sed -E 's/^([0-9]+)\.([0-9]+)\.([0-9]+).*/\3/')
    printf '%s.%s.%s\n' "$major" "$minor" "$((patch + 1))"
}

# version_at REF LABEL: prints the agreed version of both files at REF.
# Exits 1 when a file is missing its Version line or the two disagree.
version_at() {
    local ref="$1" label="$2" main_v api_v
    main_v=$(extract_version "$ref" "$MAIN_FILE")
    api_v=$(extract_version "$ref" "$API_FILE")
    if [[ -z "$main_v" || -z "$api_v" ]]; then
        echo "✗ FAIL: could not find 'var Version = \"...\"' in $MAIN_FILE and/or $API_FILE at $label ($ref)" >&2
        exit 1
    fi
    if [[ "$main_v" != "$api_v" ]]; then
        echo "✗ FAIL: version mismatch at $label ($ref) — $MAIN_FILE has $main_v, $API_FILE has $api_v" >&2
        echo "  AGENT.md Versioning rule: both files MUST match on every commit." >&2
        exit 1
    fi
    printf '%s\n' "$main_v"
}

# --- resolve refs -----------------------------------------------------------
if ! git rev-parse --quiet --verify "${BRANCH}^{commit}" >/dev/null 2>&1; then
    echo "✗ FAIL: branch '$BRANCH' does not resolve to a commit" >&2
    exit 1
fi
if ! git rev-parse --quiet --verify "${MAIN_BRANCH}^{commit}" >/dev/null 2>&1; then
    echo "✗ FAIL: main branch '$MAIN_BRANCH' does not resolve to a commit (set MAIN_BRANCH)" >&2
    exit 1
fi

# --- read the versions ------------------------------------------------------
M=$(version_at "$MAIN_BRANCH" "main")
BASE=$(git merge-base "$MAIN_BRANCH" "$BRANCH")
F=$(version_at "$BASE" "merge base")
B=$(version_at "$BRANCH" "branch tip")
E=$(next_patch "$M")

echo "==> Merge version check: $BRANCH into $MAIN_BRANCH"
echo "    main: $M   base: $F ($BASE)   branch: $B   expected: $E"

if [[ "$B" == "$F" ]]; then
    echo "ok: branch $BRANCH does not change the version (still $F); the merge step will set it to $E"
    exit 0
fi

if [[ "$B" == "$E" ]]; then
    echo "ok: branch $BRANCH already carries the expected version $E"
    exit 0
fi

echo "✗ REFUSED: branch $BRANCH sets version $B, main is at $M, and the expected version is $E." >&2
echo "  Set the branch's version back to $F (the merge base) or to $E so the merge step does not clash." >&2
exit 1
