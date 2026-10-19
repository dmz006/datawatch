#!/usr/bin/env bash
# scripts/merge-branches-bump-version.sh — merges one or more branches into
# main, then sets the next patch version for the merge in both
# version-carrying files and in CHANGELOG.md, and commits that bump once.
#
# Usage: bash scripts/merge-branches-bump-version.sh 'plain-language title' BRANCH [BRANCH...]
#
# Steps, in order:
#   1. Require a non-empty title and at least one branch (exit 2 otherwise).
#   2. Require the current branch to be MAIN_BRANCH (default: main) and the
#      working tree to be clean (exit 1 otherwise).
#   3. Run check-merge-version.sh for every branch before merging anything.
#      If any check fails, nothing is merged (exit 1).
#   4. Compute NEXT: the patch number of the version in
#      cmd/datawatch/main.go, plus one.
#   5. Merge each branch in order with --no-ff. A conflicting merge is
#      aborted and the script exits 1; merges that already succeeded stay
#      in place and are reported.
#   6. Run set-release-version.sh with NEXT and the title.
#   7. Stage only cmd/datawatch/main.go, internal/server/api.go and
#      CHANGELOG.md, and commit once if anything is staged.
#
# Exit codes:
#   0  all branches merged (or already merged) and the bump committed
#   1  precondition, check, merge or version step failed
#   2  usage error
#
# Never runs tmux kill-server, pkill, killall or systemctl.
#
# The script cds to its own repository root ($(dirname "$0")/..), so it
# can be run from any directory inside the repo.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$(dirname "$0")/.."

MAIN_FILE="cmd/datawatch/main.go"
API_FILE="internal/server/api.go"
CHANGELOG_FILE="CHANGELOG.md"
MAIN_BRANCH="${MAIN_BRANCH:-main}"
export MAIN_BRANCH

usage() {
    echo "usage: bash scripts/merge-branches-bump-version.sh 'plain-language title' BRANCH [BRANCH...]" >&2
}

# --- 1. arguments -----------------------------------------------------------
if [[ $# -lt 2 ]]; then
    usage
    exit 2
fi
TITLE="$1"
shift
if [[ -z "${TITLE//[[:space:]]/}" ]]; then
    echo "✗ the title must not be empty" >&2
    usage
    exit 2
fi
BRANCHES=("$@")

# --- 2. preconditions -------------------------------------------------------
CURRENT=$(git rev-parse --abbrev-ref HEAD)
if [[ "$CURRENT" != "$MAIN_BRANCH" ]]; then
    echo "✗ current branch is '$CURRENT'; it must be '$MAIN_BRANCH' to merge into it" >&2
    exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
    echo "✗ working tree is not clean; commit or stash your changes first" >&2
    git status --short >&2
    exit 1
fi

# --- 3. check every branch before merging anything --------------------------
echo "==> Checking ${#BRANCHES[@]} branch(es) before merging"
for branch in "${BRANCHES[@]}"; do
    if ! bash "$SCRIPT_DIR/check-merge-version.sh" "$branch"; then
        echo "✗ version check failed for '$branch'; nothing was merged" >&2
        exit 1
    fi
done

# --- 4. next version --------------------------------------------------------
MAIN_V=$(grep -E '^var Version = "' "$MAIN_FILE" | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || true)
if [[ ! "$MAIN_V" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "✗ could not read a X.Y.Z version from $MAIN_FILE (got '$MAIN_V')" >&2
    exit 1
fi
NEXT="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
echo "==> Version on $MAIN_BRANCH is $MAIN_V; the merge bump will be $NEXT"

# --- 5. merge each branch in order ------------------------------------------
MERGED=()
for branch in "${BRANCHES[@]}"; do
    echo "==> Merging $branch into $MAIN_BRANCH"
    if ! git merge --no-ff --no-edit "$branch"; then
        git merge --abort 2>/dev/null || true
        echo "✗ merge of '$branch' failed and was aborted" >&2
        if [[ ${#MERGED[@]} -gt 0 ]]; then
            echo "  already merged and left in place: ${MERGED[*]}" >&2
        fi
        echo "  not merged: $branch and every branch after it" >&2
        exit 1
    fi
    MERGED+=("$branch")
done
echo "==> Merged: ${MERGED[*]}"

# --- 6. set the release version ---------------------------------------------
bash "$SCRIPT_DIR/set-release-version.sh" "$NEXT" "$TITLE"

# --- 7. stage explicit paths and commit once --------------------------------
git add -- "$MAIN_FILE" "$API_FILE" "$CHANGELOG_FILE"
if git diff --cached --quiet; then
    echo "==> Nothing staged; no bump commit made"
else
    git commit -m "chore(release): v${NEXT} bump for merged branches"
    echo "==> Committed chore(release): v${NEXT} bump for merged branches"
fi
echo "ok: merged ${MERGED[*]} and set version $NEXT"
