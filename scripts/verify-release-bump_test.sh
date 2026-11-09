#!/usr/bin/env bash
# scripts/verify-release-bump_test.sh — shell tests for
# scripts/verify-release-bump.sh.
#
# Every case builds a fresh fake repository under a mktemp -d directory
# (outside the real repository) with a bare repository as its origin. The
# history is a 1.0.0 commit tagged v1.0.0, then a 1.0.1 commit at HEAD, with
# main and the tag pushed to origin. Each case then either leaves that state
# passing, or breaks exactly one of the five checks, and asserts the exit
# status and the named FAIL line.
#
# Run: env -u TMUX -u TMUX_TMPDIR bash scripts/verify-release-bump_test.sh
# Exit codes: 0 all cases passed, 1 at least one case failed.

set -uo pipefail
unset TMUX TMUX_TMPDIR

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_UNDER_TEST="${REPO_ROOT}/scripts/verify-release-bump.sh"
[[ -f "$SCRIPT_UNDER_TEST" ]] || { echo "script under test is missing: $SCRIPT_UNDER_TEST" >&2; exit 1; }

if ! bash -n "$SCRIPT_UNDER_TEST"; then
    echo "syntax check failed for $SCRIPT_UNDER_TEST" >&2
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

case_no=0
pass_count=0
fail_count=0
CHECKS="version_variables_equal changelog_heading_matches one_step_above_last_tag tag_not_taken readme_line_matches"

# ---------------------------------------------------------------------------
# Fixture writers. Each writes the files for one version into $REPO.
# ---------------------------------------------------------------------------
write_version_files() { # VERSION
    mkdir -p "$REPO/cmd/datawatch" "$REPO/internal/server"
    printf 'package main\n\nvar Version = "%s"\n' "$1" > "$REPO/cmd/datawatch/main.go"
    printf 'package server\n\nvar Version = "%s"\n' "$1" > "$REPO/internal/server/api.go"
}

write_changelog() { # VERSION
    printf '# Changelog\n\n## v%s - test release\n\n- change\n\n## v1.0.0 - first release\n' "$1" > "$REPO/CHANGELOG.md"
}

write_readme() { # VERSION
    printf '# fake repo\n\nCurrent release: v%s (2026-11-08).\n' "$1" > "$REPO/README.md"
}

write_release_files() { # VERSION: every file that names the version
    write_version_files "$1"
    write_changelog "$1"
    write_readme "$1"
}

# new_repo builds the passing fixture and sets REPO and REMOTE_DIR.
new_repo() {
    case_no=$((case_no + 1))
    REPO="$TMP/repo-$case_no"
    REMOTE_DIR="$TMP/remote-$case_no.git"
    mkdir -p "$REPO/scripts"
    git -C "$REPO" init -q -b main
    git -C "$REPO" config user.email "test@example.invalid"
    git -C "$REPO" config user.name "Script Test"
    cp "$SCRIPT_UNDER_TEST" "$REPO/scripts/verify-release-bump.sh"
    write_release_files 1.0.0
    git -C "$REPO" add -A
    git -C "$REPO" commit -q -m "release 1.0.0"
    git -C "$REPO" tag v1.0.0
    write_release_files 1.0.1
    git -C "$REPO" add -A
    git -C "$REPO" commit -q -m "release 1.0.1"
    git init -q --bare "$REMOTE_DIR"
    git -C "$REPO" remote add origin "$REMOTE_DIR"
    git -C "$REPO" push -q origin main --tags
}

# run_verify runs the script under test in $REPO and sets OUT and RC.
run_verify() {
    OUT="$(cd "$REPO" && bash scripts/verify-release-bump.sh 2>&1)"
    RC=$?
}

# snapshot prints everything the script must leave alone: the working tree
# contents, HEAD, local tags, and the remote's tags.
snapshot() {
    {
        git -C "$REPO" rev-parse HEAD
        git -C "$REPO" status --porcelain
        git -C "$REPO" tag --list
        git -C "$REPO" ls-remote --tags origin
        find "$REPO" -path "$REPO/.git" -prune -o -type f -print0 | sort -z | xargs -0 sha256sum
    } 2>&1
}

report() { # name ok(1|0) — prints the result line and counts it
    if [[ "$2" -eq 1 ]]; then
        pass_count=$((pass_count + 1))
        printf 'ok   %s\n' "$1"
    else
        fail_count=$((fail_count + 1))
        printf 'FAIL %s\n' "$1"
        printf '%s\n' "$OUT" | sed 's/^/     | /'
    fi
}

# expect_all_pass name: every check PASS, exit 0, and exactly five lines.
expect_all_pass() {
    local ok=1 check lines
    for check in $CHECKS; do
        grep -qx "PASS $check: .*" <<< "$OUT" || ok=0
    done
    lines="$(grep -c . <<< "$OUT" || true)"
    [[ "$RC" -eq 0 && "$ok" -eq 1 && "$lines" -eq 5 ]] || ok=0
    report "$1" "$ok"
}

# expect_only_fail name check: exit 1, exactly one FAIL line, and it is check.
expect_only_fail() {
    local name="$1" check="$2" ok=1 nfail lines
    nfail="$(grep -c '^FAIL ' <<< "$OUT" || true)"
    lines="$(grep -c . <<< "$OUT" || true)"
    [[ "$RC" -eq 1 ]] || ok=0
    [[ "$nfail" -eq 1 ]] || ok=0
    [[ "$lines" -eq 5 ]] || ok=0
    grep -q "^FAIL $check: " <<< "$OUT" || ok=0
    report "$name" "$ok"
}

# ---------------------------------------------------------------------------
# Case 1: the passing fixture -> five PASS lines, exit 0
# ---------------------------------------------------------------------------
new_repo
before="$(snapshot)"
run_verify
first_out="$OUT"
first_rc="$RC"
expect_all_pass "passing repository passes every check"

# ---------------------------------------------------------------------------
# Case 2: a second run gives the same output and exit status
# ---------------------------------------------------------------------------
run_verify
if [[ "$OUT" == "$first_out" && "$RC" -eq "$first_rc" ]]; then
    report "second run gives the same output" 1
else
    OUT="$first_out vs $OUT"
    report "second run gives the same output" 0
fi

# ---------------------------------------------------------------------------
# Case 3: the run changes no file, tag or remote
# ---------------------------------------------------------------------------
after="$(snapshot)"
OUT="$after"
if [[ "$before" == "$after" ]]; then
    report "run changes no file, tag or remote" 1
else
    report "run changes no file, tag or remote" 0
fi

# ---------------------------------------------------------------------------
# Case 4: version_variables_equal broken (api.go carries another version)
# ---------------------------------------------------------------------------
new_repo
printf 'package server\n\nvar Version = "1.0.2"\n' > "$REPO/internal/server/api.go"
run_verify
expect_only_fail "version files differing fails version_variables_equal" version_variables_equal

# ---------------------------------------------------------------------------
# Case 5: changelog_heading_matches broken (first heading is the old version)
# ---------------------------------------------------------------------------
new_repo
write_changelog 1.0.0
run_verify
expect_only_fail "stale changelog heading fails changelog_heading_matches" changelog_heading_matches

# ---------------------------------------------------------------------------
# Case 6: one_step_above_last_tag broken (1.0.3 is two patches above v1.0.0)
# ---------------------------------------------------------------------------
new_repo
write_release_files 1.0.3
run_verify
expect_only_fail "two steps above the newest tag fails one_step_above_last_tag" one_step_above_last_tag

# ---------------------------------------------------------------------------
# Case 7: tag_not_taken broken (origin already has v1.0.1 on another commit)
# ---------------------------------------------------------------------------
new_repo
git -C "$REPO" push -q origin HEAD~1:refs/tags/v1.0.1
run_verify
expect_only_fail "tag already on the remote fails tag_not_taken" tag_not_taken

# ---------------------------------------------------------------------------
# Case 8: readme_line_matches broken (Current release: names the old version)
# ---------------------------------------------------------------------------
new_repo
write_readme 1.0.0
run_verify
expect_only_fail "stale Current release line fails readme_line_matches" readme_line_matches

# ---------------------------------------------------------------------------
# Case 9: no remote configured -> the tag check uses local tags only
# ---------------------------------------------------------------------------
new_repo
git -C "$REPO" remote remove origin
run_verify
expect_all_pass "no remote configured still passes every check"

# ---------------------------------------------------------------------------
printf '\n%d case(s) passed, %d case(s) failed\n' "$pass_count" "$fail_count"
if (( fail_count > 0 )); then
    exit 1
fi
exit 0
