#!/usr/bin/env bash
# scripts/set-release-version.sh — sets the release version in both
# version-carrying files, cmd/datawatch/main.go and internal/server/api.go.
#
# AGENT.md's Versioning rule keeps `var Version` identical in both files.
# This script rewrites the quoted value on the `var Version = ` line in
# each file and changes nothing else in either file. It does not commit,
# and it does not touch CHANGELOG.md.
#
# Usage: bash scripts/set-release-version.sh X.Y.Z 'plain-language title'
#
# Exit codes:
#   0  both files now carry X.Y.Z
#   1  one of the files has no `var Version = "..."` line; nothing changed
#   2  usage error: version is not three dot-separated whole numbers, or
#      the title is empty
#
# Only grep and sed are used; nothing here touches tmux or services.
#
# The script cds to its own repository root ($(dirname "$0")/..), so it
# can be run from any directory inside the repo.

set -euo pipefail

cd "$(dirname "$0")/.."

MAIN_FILE="cmd/datawatch/main.go"
API_FILE="internal/server/api.go"
VERSION_LINE='^var Version = "[^"]*"'

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

# Check both files before writing either, so a missing line never leaves
# one file changed and the other not.
for file in "$MAIN_FILE" "$API_FILE"; do
    if ! grep -Eq "$VERSION_LINE" "$file"; then
        echo "✗ FAIL: no 'var Version = \"...\"' line in $file; nothing changed" >&2
        exit 1
    fi
done

for file in "$MAIN_FILE" "$API_FILE"; do
    sed -i -E "/$VERSION_LINE/s/\"[^\"]*\"/\"${VERSION}\"/" "$file"
done

echo "ok: set version to $VERSION in $MAIN_FILE and $API_FILE"
echo "    title: $TITLE"
echo "    not committed; CHANGELOG.md not touched"
