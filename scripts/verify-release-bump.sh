#!/usr/bin/env bash
# scripts/verify-release-bump.sh — read-only check of a release bump.
#
# Makes the same five checks as ValidateReleaseBump in
# internal/mergepipeline/release_validator.go, under the same check names:
#
#   version_variables_equal    cmd/datawatch/main.go and internal/server/api.go
#                              carry the same X.Y.Z in `var Version = "..."`
#   changelog_heading_matches  the first `## vX.Y.Z` heading in CHANGELOG.md is
#                              that version
#   one_step_above_last_tag    the version is exactly one patch, minor or major
#                              step above the newest local vX.Y.Z tag
#   tag_not_taken              the tag vX.Y.Z is not on the remote, and any local
#                              tag of that name points at HEAD
#   readme_line_matches        the first "Current release:" line in README.md
#                              names that version
#
# One PASS or FAIL line is printed per check, in that order. The exit status is
# 0 only when all five checks pass, and 1 otherwise. The script never writes a
# file, a tag or a remote: it reads the working tree, the local tags and, when a
# remote is configured, the remote's tags through git ls-remote.
#
# Environment:
#   VERIFY_RELEASE_REMOTE  remote queried for tags (default: origin). When the
#                          named remote is not configured, only local tags are
#                          checked and the tag check says so.
#
# The script cds to its own repository root ($(dirname "$0")/..), so it can be
# run from any directory inside the repository. It is a verification script:
# TMUX and TMUX_TMPDIR are removed from its environment first, so nothing it
# runs can reach a tmux server.
#
# Usage: bash scripts/verify-release-bump.sh

set -euo pipefail
unset TMUX TMUX_TMPDIR

cd "$(dirname "$0")/.."

VERSION_FILES=(cmd/datawatch/main.go internal/server/api.go)
CHANGELOG_FILE="CHANGELOG.md"
README_FILE="README.md"
REMOTE="${VERIFY_RELEASE_REMOTE:-origin}"

FAILED=0
# VERSION is the X.Y.Z read by the version check; the other checks compare
# against it. It is empty when no version could be read.
VERSION=""

pass() { printf 'PASS %s: %s\n' "$1" "$2"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2"; FAILED=1; }

is_semver() { [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; }

# check_version_variables reads the version from every version file. It fails
# when a file is unreadable, has no Version line, carries a non-X.Y.Z value, or
# when the files disagree. VERSION is the first readable version, as in the Go
# check, so the other checks still have something to compare with.
check_version_variables() {
    local f line v first="" problems="" pairs="" differ=0
    for f in "${VERSION_FILES[@]}"; do
        if [[ ! -r "$f" ]]; then
            problems+="could not read $f; "
            continue
        fi
        line="$(grep -m 1 -E '^var Version = "' "$f" || true)"
        if [[ -z "$line" ]]; then
            problems+="no var Version = \"...\" line in $f; "
            continue
        fi
        v="$(printf '%s\n' "$line" | sed -E 's/^var Version = "([^"]*)".*/\1/')"
        if ! is_semver "$v"; then
            problems+="$f carries \"$v\", which is not X.Y.Z; "
            continue
        fi
        if [[ -z "$first" ]]; then
            first="$v"
        fi
        if [[ "$v" != "$first" ]]; then
            differ=1
        fi
        pairs+="$f=$v, "
    done
    VERSION="$first"
    if (( differ )); then
        problems+="version files differ: ${pairs%, }; "
    fi
    if [[ -n "$problems" ]]; then
        fail version_variables_equal "${problems%; }"
    else
        pass version_variables_equal "all ${#VERSION_FILES[@]} version files carry $VERSION"
    fi
}

# check_changelog_heading passes when the first release heading in the
# changelog is for VERSION.
check_changelog_heading() {
    if [[ -z "$VERSION" ]]; then
        fail changelog_heading_matches "no version was read to compare with"
        return 0
    fi
    if [[ ! -r "$CHANGELOG_FILE" ]]; then
        fail changelog_heading_matches "could not read $CHANGELOG_FILE"
        return 0
    fi
    local line heading
    line="$(grep -m 1 -E '^## v[0-9]+\.[0-9]+\.[0-9]+([[:space:]]|$)' "$CHANGELOG_FILE" || true)"
    if [[ -z "$line" ]]; then
        fail changelog_heading_matches "no release heading (## vX.Y.Z) in $CHANGELOG_FILE"
        return 0
    fi
    heading="$(printf '%s\n' "$line" | sed -E 's/^## v([0-9]+\.[0-9]+\.[0-9]+).*/\1/')"
    if [[ "$heading" != "$VERSION" ]]; then
        fail changelog_heading_matches "the first release heading in $CHANGELOG_FILE is v$heading, not v$VERSION"
    else
        pass changelog_heading_matches "the first release heading in $CHANGELOG_FILE is v$VERSION"
    fi
}

# require_one_step prints an explanation and returns 1 unless NEXT is exactly
# one patch, minor or major step above LAST. Both are X.Y.Z strings.
require_one_step() {
    local last="$1" next="$2" lM lm lp nM nm np
    IFS=. read -r lM lm lp <<< "$last"
    IFS=. read -r nM nm np <<< "$next"
    # 10# keeps a leading zero from being read as octal.
    lM=$((10#$lM)); lm=$((10#$lm)); lp=$((10#$lp))
    nM=$((10#$nM)); nm=$((10#$nm)); np=$((10#$np))
    if (( nM == lM && nm == lm && np == lp + 1 )) ||
       (( nM == lM && nm == lm + 1 && np == 0 )) ||
       (( nM == lM + 1 && nm == 0 && np == 0 )); then
        return 0
    fi
    echo "v$next is not one patch, minor or major step above v$last"
    return 1
}

# check_one_step_above_tag passes when VERSION is one step above the newest
# local vX.Y.Z tag.
check_one_step_above_tag() {
    if [[ -z "$VERSION" ]]; then
        fail one_step_above_last_tag "no version was read to compare with"
        return 0
    fi
    local tags last err
    if ! tags="$(git tag --list 'v*' 2>&1)"; then
        fail one_step_above_last_tag "could not list tags: $tags"
        return 0
    fi
    last="$(printf '%s\n' "$tags" | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed 's/^v//' |
        sort -t . -k1,1n -k2,2n -k3,3n | tail -n 1 || true)"
    if [[ -z "$last" ]]; then
        fail one_step_above_last_tag "no v-tag exists to compare with"
        return 0
    fi
    if err="$(require_one_step "$last" "$VERSION")"; then
        pass one_step_above_last_tag "v$VERSION is one step above the newest tag v$last"
    else
        fail one_step_above_last_tag "$err"
    fi
}

# check_tag_not_taken passes when the tag vVERSION is not on the remote and any
# local tag of that name points at HEAD. With no remote configured, only the
# local tag is checked.
check_tag_not_taken() {
    if [[ -z "$VERSION" ]]; then
        fail tag_not_taken "no version was read to compare with"
        return 0
    fi
    local tag="v$VERSION" out ref found=0 remote_note local_tags tag_sha head_sha
    if git remote get-url "$REMOTE" >/dev/null 2>&1; then
        if ! out="$(GIT_TERMINAL_PROMPT=0 git ls-remote --tags "$REMOTE" "refs/tags/$tag" 2>&1)"; then
            fail tag_not_taken "could not query remote $REMOTE: $(printf '%s' "$out" | tr '\n' ' ')"
            return 0
        fi
        while read -r _ ref; do
            if [[ "$ref" == "refs/tags/$tag" ]]; then
                found=1
            fi
        done <<< "$out"
        if (( found )); then
            fail tag_not_taken "tag $tag is already on remote $REMOTE"
            return 0
        fi
        remote_note="free on remote $REMOTE"
    else
        remote_note="free (no remote named $REMOTE is configured, so only local tags were checked)"
    fi
    if ! local_tags="$(git tag --list "$tag" 2>&1)"; then
        fail tag_not_taken "could not list local tags: $local_tags"
        return 0
    fi
    if [[ -z "$local_tags" ]]; then
        pass tag_not_taken "tag $tag is $remote_note and not set locally"
        return 0
    fi
    tag_sha="$(git rev-parse --verify --quiet "$tag^{commit}" 2>/dev/null || true)"
    head_sha="$(git rev-parse HEAD 2>/dev/null || true)"
    if [[ -z "$tag_sha" || -z "$head_sha" ]]; then
        fail tag_not_taken "could not resolve local tag $tag or HEAD"
        return 0
    fi
    if [[ "$tag_sha" != "$head_sha" ]]; then
        fail tag_not_taken "local tag $tag exists and does not point at HEAD"
    else
        pass tag_not_taken "tag $tag is $remote_note; the local tag points at HEAD"
    fi
}

# check_readme_line passes when the first README line starting with
# "Current release:" names VERSION.
check_readme_line() {
    if [[ -z "$VERSION" ]]; then
        fail readme_line_matches "no version was read to compare with"
        return 0
    fi
    if [[ ! -r "$README_FILE" ]]; then
        fail readme_line_matches "could not read $README_FILE"
        return 0
    fi
    local line rest named
    line="$(grep -m 1 -E '^[[:space:]]*Current release:' "$README_FILE" || true)"
    if [[ -z "$line" ]]; then
        fail readme_line_matches "no Current release: line in $README_FILE"
        return 0
    fi
    rest="${line#*Current release:}"
    named="$(printf '%s\n' "$rest" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n 1 || true)"
    if [[ -z "$named" ]]; then
        fail readme_line_matches "the Current release: line in $README_FILE names no version"
    elif [[ "$named" != "$VERSION" ]]; then
        fail readme_line_matches "the Current release: line in $README_FILE names v$named, not v$VERSION"
    else
        pass readme_line_matches "the Current release: line in $README_FILE names v$VERSION"
    fi
}

# The version check runs in this shell (not a subshell) so VERSION is set for
# the checks that follow.
check_version_variables
check_changelog_heading
check_one_step_above_tag
check_tag_not_taken
check_readme_line

exit "$FAILED"
