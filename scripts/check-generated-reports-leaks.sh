#!/usr/bin/env bash
# scripts/check-generated-reports-leaks.sh — local-environment leak check for
# the generated reports folder.
#
# Generated reports (plan-coverage gates, final-gate outputs and similar
# session artifacts) pile up under docs/plans/reports, and they are written
# by sessions that may have picked up values from the operator's own
# environment. This script collects every file under the reports folder —
# tracked (git ls-files) and untracked non-ignored
# (git ls-files --others --exclude-standard) — copies them into a
# throwaway directory (mktemp -d, removed by a trap on EXIT) and hands
# that directory to scripts/check-no-local-leaks.sh, with
# LEAK_CHECK_LOCAL pointing at this repository's .leak-patterns.local so
# the "local" class runs against the operator's fixed strings even though
# the scan root is the throwaway copy. When .leak-patterns.local is absent
# that class is skipped with a stderr notice — never an error.
#
# Usage:
#   scripts/check-generated-reports-leaks.sh [folder]
#
#   folder  directory to scan, relative to the repository root
#           (default: docs/plans/reports)
#
# Output: one line per finding, `path:line: class`, with paths relative to
# the repository root — matched text is never printed.
# Exit codes: 0 clean, 1 findings reported (by the scanner),
#             2 usage or configuration error.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SCANNER="${SCRIPT_DIR}/check-no-local-leaks.sh"

usage_error() {
    echo "usage: $(basename "$0") [folder]" >&2
    exit 2
}

die2() {
    echo "check-generated-reports-leaks: $*" >&2
    exit 2
}

FOLDER="docs/plans/reports"
if (( $# == 1 )); then
    case "$1" in
        -h | --help) usage_error ;;
    esac
    FOLDER="$1"
elif (( $# > 1 )); then
    usage_error
fi

FOLDER="${FOLDER%/}"
[[ -n "$FOLDER" ]] || usage_error

[[ -f "$SCANNER" ]] || die2 "scanner not found: $SCANNER"
git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
    || die2 "repository not found (run from inside a git work tree)"
[[ -d "$ROOT/$FOLDER" ]] || die2 "folder is not a directory: $FOLDER"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# ------------------------------------------------------------- file list
# Tracked and untracked non-ignored files under the folder, deduplicated
# (a file is either tracked or untracked, but the two listings are merged
# through an associative array anyway so ordering is stable).
declare -A WANTED=()
while IFS= read -r -d '' p; do
    [[ -n "$p" ]] && WANTED["$p"]=1
done < <(git -C "$ROOT" ls-files -z -- "$FOLDER")
while IFS= read -r -d '' p; do
    [[ -n "$p" ]] && WANTED["$p"]=1
done < <(git -C "$ROOT" ls-files --others --exclude-standard -z -- "$FOLDER")

FILES=()
if (( ${#WANTED[@]} > 0 )); then
    while IFS= read -r -d '' p; do
        FILES+=("$p")
    done < <(printf '%s\0' "${!WANTED[@]}" | LC_ALL=C sort -z)
fi

# --------------------------------------- copy into the throwaway scan root
# Keep the repository-relative structure so the scanner's `path:line: class`
# findings name the real location, not the temp directory.
for p in "${FILES[@]}"; do
    f="$ROOT/$p"
    # A tracked file that was deleted from the working tree has no
    # content left to leak — removing a leak is never blocked.
    [[ -f "$f" ]] || continue
    dst="$TMP/$p"
    mkdir -p "$(dirname "$dst")"
    cp -p -- "$f" "$dst"
done

# ------------------------------------------------- local patterns class
# Point the scanner at THIS repository's operator patterns file (an
# absolute path, since the scanner resolves env overrides relative to its
# own location, never relative to the scan root). Absent file: skip the
# class with a stderr notice and run the remaining classes.
LOCAL_FILE="$ROOT/.leak-patterns.local"
if [[ -f "$LOCAL_FILE" ]]; then
    LEAK_CHECK_LOCAL="$LOCAL_FILE"
    export LEAK_CHECK_LOCAL
else
    echo "check-generated-reports-leaks: local patterns: skipped (no .leak-patterns.local)" >&2
fi

# The scanner's exit code IS this script's: 0 clean, 1 findings, 2
# configuration error. Its output is already `path:line: class` only.
bash "$SCANNER" "$TMP"
