#!/usr/bin/env bash
# Prints which release smoke test this merge needs: full, targeted or skip.
# Every minor and major release gets the full run, tagged or not. A merge that
# changes only documentation is skipped. Everything else gets the targeted run.
set -euo pipefail
source "$(dirname "$0")/lib.sh" 2>/dev/null || { echo "merge check: not in a repository" >&2; exit 2; }
merge_need_var MERGE_BASE_COMMIT
merge_need_var MERGE_RELEASE_VERSION

case "${MERGE_RELEASE_SIZE:-}" in
    minor | major) echo full; exit 0 ;;
    none) ;;
    *) if [[ "${MERGE_RELEASE_VERSION##*.}" == 0 ]]; then echo full; exit 0; fi ;;
esac

# The release bump commit changes code files in every release, so it does not
# count as a change.
head=HEAD
if git log -1 --format=%s HEAD | grep -q '^chore(release): '; then head=HEAD~1; fi
changed=$(git diff --name-only "$MERGE_BASE_COMMIT" "$head")
if grep -qvE '^$|\.md$|^docs/|^internal/server/web/docs/|^internal/docsindex/assets/' <<<"$changed"; then
    echo targeted
else
    echo skip
fi
