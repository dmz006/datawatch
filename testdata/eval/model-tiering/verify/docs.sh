#!/usr/bin/env bash
# Eval cell verifier — docs task (CHANGELOG + docs/session-tracking.md only).
# Usage: docs.sh <cell-dir>. Exit 0 only if the cell satisfies specs/docs.md.
set -euo pipefail
echo "[$(date '+%H:%M:%S')] start docs verify"
CELL=${1:?usage: docs.sh <cell-dir>}
cd "$CELL"
root=$(git rev-list --max-parents=0 HEAD | tail -n1)
# Everything changed since the cell's baseline commit: committed, staged,
# unstaged and untracked.
changed=$( { git diff --name-only "$root"; git ls-files --others --exclude-standard; } | sort -u)
[ -n "$changed" ] || { echo "FAIL: no changes in cell"; exit 1; }
bad=$(printf '%s\n' "$changed" | grep -vE '^(CHANGELOG\.md|docs/.*)$' || true)
[ -z "$bad" ] || { echo "FAIL: files outside CHANGELOG.md/docs/ changed:"; echo "$bad"; exit 1; }
printf '%s\n' "$changed" | grep -qx 'CHANGELOG.md' || { echo "FAIL: CHANGELOG.md not changed"; exit 1; }
printf '%s\n' "$changed" | grep -qx 'docs/session-tracking.md' || { echo "FAIL: docs/session-tracking.md not changed"; exit 1; }
# No tracker IDs in any added line.
ids=$(git diff "$root" -- CHANGELOG.md docs/ | grep -E '^\+' | grep -vE '^\+\+\+ ' | grep -nE 'BL[0-9]+' || true)
[ -z "$ids" ] || { echo "FAIL: tracker IDs in added lines:"; echo "$ids"; exit 1; }
# Version strings unchanged.
for f in cmd/datawatch/main.go internal/server/api.go; do
  [ "$(git show "$root:$f" | grep -E '^var Version')" = "$(grep -E '^var Version' "$f")" ] \
    || { echo "FAIL: Version changed in $f"; exit 1; }
done
# CHANGELOG: the Unreleased section (up to the next '## ' heading) must now
# mention output.log and gc, and no new version heading may be added.
unrel=$(awk '/^## \[Unreleased\]/{f=1;next} f&&/^## /{exit} f' CHANGELOG.md)
printf '%s\n' "$unrel" | grep -q 'output.log' || { echo "FAIL: Unreleased section does not mention output.log"; exit 1; }
printf '%s\n' "$unrel" | grep -qiE '(^|[^[:alnum:]])gc([^[:alnum:]]|$)|gc\.auto|garbage' || { echo "FAIL: Unreleased section does not mention gc"; exit 1; }
[ "$(git show "$root:CHANGELOG.md" | grep -m1 -E '^## v')" = "$(grep -m1 -E '^## v' CHANGELOG.md)" ] \
  || { echo "FAIL: a new version heading was added (no version bump allowed)"; exit 1; }
# docs/session-tracking.md: output.log row says not committed; maintenance section exists.
row=$(grep -E '^\|[[:space:]]*`?output\.log`?[[:space:]]*\|' docs/session-tracking.md || true)
[ -n "$row" ] || { echo "FAIL: output.log row missing from docs/session-tracking.md"; exit 1; }
printf '%s\n' "$row" | grep -qiE 'not (be(ing)? )?committed|never committed|not (stored |kept |tracked )?in git|excluded from git|untracked' \
  || { echo "FAIL: output.log row does not say it is not committed: $row"; exit 1; }
grep -qiE '^#+ .*repository size' docs/session-tracking.md || { echo "FAIL: no 'Repository size & maintenance' section"; exit 1; }
echo "RESULT: PASS"
echo "[$(date '+%H:%M:%S')] done docs verify"
