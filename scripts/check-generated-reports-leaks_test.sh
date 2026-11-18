#!/usr/bin/env bash
# scripts/check-generated-reports-leaks_test.sh — shell tests for
# scripts/check-generated-reports-leaks.sh.
#
# Every case builds a fresh fake git repository under a mktemp -d directory
# (outside the real repository), copies the script under test — and the
# scanner plus config files it delegates to — into the fake repository's
# scripts/ directory, seeds a fake .leak-patterns.local containing the fake
# name 'fakehost-test', writes reports under docs/plans/reports, runs the
# script, and asserts on the exit status, the reported local-class finding
# count, and — most importantly — that the fake name itself never appears
# in the output.
#
# No real hostname is used anywhere in this test.
#
# Run independently: bash scripts/check-generated-reports-leaks_test.sh
# Exit codes: 0 all cases passed, 1 at least one case failed.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT_UNDER_TEST="${REPO_ROOT}/scripts/check-generated-reports-leaks.sh"
[[ -f "$SCRIPT_UNDER_TEST" ]] || { echo "script under test is missing: $SCRIPT_UNDER_TEST" >&2; exit 1; }

if ! bash -n "$SCRIPT_UNDER_TEST"; then
    echo "syntax check failed for $SCRIPT_UNDER_TEST" >&2
    exit 1
fi

FAKE_NAME="fakehost-test"
REPORT_DIR="docs/plans/reports"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

case_no=0
pass_count=0
fail_count=0

# ---------------------------------------------------------------------------
# Helpers. All state lives under $TMP; nothing is written to the real repo.
# ---------------------------------------------------------------------------
new_repo() { # sets REPO
    case_no=$((case_no + 1))
    REPO="$TMP/repo-$case_no"
    mkdir -p "$REPO/scripts"
    git -C "$REPO" init -q -b main
    git -C "$REPO" config user.email "test@example.invalid"
    git -C "$REPO" config user.name "Script Test"
    cp "$SCRIPT_UNDER_TEST" "$REPO/scripts/check-generated-reports-leaks.sh"
    # The script under test delegates to the whole-tree scanner and its
    # three config files; the fake repository needs the same set.
    cp "$REPO_ROOT/scripts/check-no-local-leaks.sh" "$REPO/scripts/check-no-local-leaks.sh"
    cp "$REPO_ROOT/scripts/leak-check.patterns" "$REPO/scripts/leak-check.patterns"
    cp "$REPO_ROOT/scripts/leak-check.allowed-emails" "$REPO/scripts/leak-check.allowed-emails"
    cp "$REPO_ROOT/scripts/leak-check.exclude" "$REPO/scripts/leak-check.exclude"
    # The operator's local patterns file, faked with a name that is not a
    # real hostname of anyone's machine.
    printf '%s\n' "$FAKE_NAME" > "$REPO/.leak-patterns.local"
    printf '# fake repo\n' > "$REPO/README.md"
    (cd "$REPO" && git add README.md && git commit -q -m base)
}

run_check() { # [args...] -> sets RC, OUT, LOCAL_COUNT, LEAKED
    OUT="$(cd "$REPO" && bash scripts/check-generated-reports-leaks.sh "$@" 2>&1)"
    RC=$?
    # local-class findings: the scanner's "local: N" summary line
    LOCAL_COUNT="$(printf '%s\n' "$OUT" | sed -n 's/^local: \([0-9][0-9]*\)$/\1/p' | tail -n 1)"
    LOCAL_COUNT="${LOCAL_COUNT:-0}"
    if [[ "$OUT" == *"$FAKE_NAME"* ]]; then LEAKED=yes; else LEAKED=no; fi
}

# expect_case name want_rc want_local want_leaked [must_contain]
# The immediately preceding run_check produced RC/OUT/LOCAL_COUNT/LEAKED.
expect_case() {
    local name="$1" want_rc="$2" want_local="$3" want_leak="${4:-no}" want_contains="${5:-}"
    local ok=1
    [[ "$RC" -eq "$want_rc" ]] || ok=0
    [[ "$LOCAL_COUNT" -eq "$want_local" ]] || ok=0
    [[ "$LEAKED" == "$want_leak" ]] || ok=0
    if [[ -n "$want_contains" ]] && [[ "$OUT" != *"$want_contains"* ]]; then
        ok=0
    fi
    if (( ok )); then
        pass_count=$((pass_count + 1))
        printf 'ok   %-40s rc=%s local=%s name-leak=%s\n' "$name" "$RC" "$LOCAL_COUNT" "$LEAKED"
    else
        fail_count=$((fail_count + 1))
        printf 'FAIL %-40s want rc=%s local=%s name-leak=%s, got rc=%s local=%s name-leak=%s\n' \
            "$name" "$want_rc" "$want_local" "$want_leak" "$RC" "$LOCAL_COUNT" "$LEAKED"
        printf '%s\n' "$OUT" | sed 's/^/     | /'
    fi
}

# ---------------------------------------------------------------------------
# Case 1: a clean report -> passes
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/$REPORT_DIR"
printf 'a clean generated report\n' > "$REPO/$REPORT_DIR/clean-report.md"
run_check
expect_case "clean report passes" 0 0 no

# ---------------------------------------------------------------------------
# Case 2: an untracked report containing the fake name -> fails, and the
# name never appears in the output (only path:line: class)
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/$REPORT_DIR"
printf 'connected to %s while running the gate\n' "$FAKE_NAME" > "$REPO/$REPORT_DIR/dirty-report.md"
run_check
expect_case "untracked report with local pattern fails" 1 1 no \
    "$REPORT_DIR/dirty-report.md:1: local"

# ---------------------------------------------------------------------------
# Case 3: a tracked (committed) report containing the fake name -> fails;
# also exercises the explicit folder argument
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/$REPORT_DIR"
printf '%s\n' "$FAKE_NAME" > "$REPO/$REPORT_DIR/old-report.md"
(cd "$REPO" && git add "$REPORT_DIR/old-report.md" && git commit -q -m "tracked report")
run_check "$REPORT_DIR"
expect_case "tracked report with local pattern fails" 1 1 no \
    "$REPORT_DIR/old-report.md:1: local"

# ---------------------------------------------------------------------------
# Case 4: no .leak-patterns.local -> the local class is skipped with a
# stderr notice, and a report holding the fake name passes
# ---------------------------------------------------------------------------
new_repo
rm -f "$REPO/.leak-patterns.local"
mkdir -p "$REPO/$REPORT_DIR"
printf '%s\n' "$FAKE_NAME" > "$REPO/$REPORT_DIR/dirty-report.md"
run_check
expect_case "absent local patterns: class skipped, passes" 0 0 no \
    "local patterns: skipped"

# ---------------------------------------------------------------------------
# Case 5: an existing but empty reports folder -> passes with no findings
# ---------------------------------------------------------------------------
new_repo
mkdir -p "$REPO/$REPORT_DIR"
run_check
expect_case "empty reports folder passes" 0 0 no

# ---------------------------------------------------------------------------
# Case 6: usage errors -> exit 2
# ---------------------------------------------------------------------------
new_repo
run_check a b
expect_case "two arguments is a usage error" 2 0 no
new_repo
run_check no/such/folder
expect_case "missing folder is a usage error" 2 0 no

# ---------------------------------------------------------------------------
printf '\n%d case(s) passed, %d case(s) failed\n' "$pass_count" "$fail_count"
if (( fail_count > 0 )); then
    exit 1
fi
exit 0
