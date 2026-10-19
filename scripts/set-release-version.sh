#!/usr/bin/env bash
# scripts/set-release-version.sh — sets the release version in both
# version-carrying files, cmd/datawatch/main.go and internal/server/api.go,
# and adds the matching heading to CHANGELOG.md.
#
# AGENT.md's Versioning rule keeps `var Version` identical in both files.
# This script rewrites the quoted value on the `var Version = ` line in
# each file and changes nothing else in either file. It also inserts a
# `## vX.Y.Z — title` heading directly below the `## [Unreleased]` line in
# CHANGELOG.md, unless a `## vX.Y.Z ` heading is already there. It does not
# commit.
#
# Usage: bash scripts/set-release-version.sh X.Y.Z 'plain-language title'
#
# Exit codes:
#   0  both files now carry X.Y.Z, and CHANGELOG.md has its heading
#   1  one of the version files has no `var Version = "..."` line, or
#      CHANGELOG.md has no `## [Unreleased]` line; nothing changed
#   2  usage error: version is not three dot-separated whole numbers, or
#      the title is empty
#
# Only grep, sed and awk are used; nothing here touches tmux or services.
#
# The script cds to its own repository root ($(dirname "$0")/..), so it
# can be run from any directory inside the repo.

set -euo pipefail

cd "$(dirname "$0")/.."

MAIN_FILE="cmd/datawatch/main.go"
API_FILE="internal/server/api.go"
CHANGELOG_FILE="CHANGELOG.md"
VERSION_LINE='^var Version = "[^"]*"'
UNRELEASED_LINE='## [Unreleased]'

usage() {
    echo "usage: bash scripts/set-release-version.sh X.Y.Z 'plain-language title'" >&2
}

if [[ $# -ne 2 ]]; then
    usage
    exit 2
fi
VERSION="$1"
TITLE="$2"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "✗ invalid version '$VERSION': expected three dot-separated whole numbers, e.g. 9.5.19" >&2
    usage
    exit 2
fi
if [[ -z "${TITLE//[[:space:]]/}" ]]; then
    echo "✗ the title must not be empty" >&2
    usage
    exit 2
fi

# Check every file before writing any of them, so a missing line never
# leaves one file changed and another not.
for file in "$MAIN_FILE" "$API_FILE"; do
    if ! grep -Eq "$VERSION_LINE" "$file"; then
        echo "✗ FAIL: no 'var Version = \"...\"' line in $file; nothing changed" >&2
        exit 1
    fi
done
if ! grep -Fxq -- "$UNRELEASED_LINE" "$CHANGELOG_FILE"; then
    echo "✗ FAIL: no '$UNRELEASED_LINE' line in $CHANGELOG_FILE; nothing changed" >&2
    exit 1
fi

for file in "$MAIN_FILE" "$API_FILE"; do
    sed -i -E "/$VERSION_LINE/s/\"[^\"]*\"/\"${VERSION}\"/" "$file"
done

# Insert the heading only if no `## vX.Y.Z ` line exists yet, so running
# the script again with the same version leaves the changelog alone.
# The heading goes in the first `## [Unreleased]` line only; everything
# after that line keeps its place. ENVIRON carries the title, so awk does
# not interpret backslashes in it.
if awk -v prefix="## v${VERSION} " 'index($0, prefix) == 1 { found = 1 } END { exit !found }' "$CHANGELOG_FILE"; then
    echo "    CHANGELOG.md already has a ## v${VERSION} heading; left alone"
else
    HEADING="## v${VERSION} — ${TITLE}" awk -v unreleased="$UNRELEASED_LINE" '
        { print }
        $0 == unreleased && !done {
            print ""
            print ENVIRON["HEADING"]
            print ""
            done = 1
        }
    ' "$CHANGELOG_FILE" > "$CHANGELOG_FILE.tmp"
    mv "$CHANGELOG_FILE.tmp" "$CHANGELOG_FILE"
    echo "    CHANGELOG.md: inserted heading for v${VERSION} below $UNRELEASED_LINE"
fi

echo "ok: set version to $VERSION in $MAIN_FILE and $API_FILE"
echo "    title: $TITLE"
echo "    not committed"
