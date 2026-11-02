#!/usr/bin/env bash
# scripts/tests/merge-helper-shipped-record.sh — tests the shipped-record
# step of scripts/merge-branches-bump-version.sh (scripts/merge-shipped-record.sh).
#
# Approach: build a throwaway git repository under a mktemp directory, put
# a stub `datawatch` on PATH that logs every call to a file (and behaves
# per scenario), source the helper's record functions, run
# record_shipped_for_branch, and assert on the logged arguments.
#
# Scenario coverage:
#   1. automaton/<id> branch -> the automaton record call carries exactly
#      --release, --commit (the throwaway repo's commit), --at and
#      --source merge_step, and autonomous.shipped.enabled was read first.
#   2. all members shipped -> the matching `--automation` call is made.
#   3. one member not shipped -> no `--automation` call.
#   4. a branch name that is not automaton/<id> -> no record call at all.
#   5. autonomous.shipped.enabled off -> no record call at all.
#   6. a failing datawatch (record refused) -> the step still exits 0 with
#      a warning: a bad daemon must never fail the merge.
#   7. daemon unreachable (every call fails) -> the step still exits 0.
#
# The test never starts or controls a terminal multiplexer, and it runs
# with HOME and the XDG directories pointed inside its mktemp directory so
# the real home directory is never touched. Uses only bash, git, jq, mktemp.
#
# Exit codes: 0 all assertions passed, 1 at least one failed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HELPER="$REPO_ROOT/scripts/merge-shipped-record.sh"

if [[ ! -f "$HELPER" ]]; then
    echo "✗ helper not found: $HELPER" >&2
    exit 1
fi
command -v jq >/dev/null || { echo "✗ jq is required" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Keep the test inside the mktemp directory: the real home directory is
# never read or written.
export HOME="$WORK/home"
mkdir -p "$HOME" "$HOME/.config" "$HOME/.local/share"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_DATA_HOME="$HOME/.local/share"

# --- throwaway git repository ---------------------------------------------
REPO="$WORK/repo"
git init -q -b main "$REPO"
git -C "$REPO" config user.email "test@example.com"
git -C "$REPO" config user.name "merge-helper-test"
echo fixture > "$REPO/fixture.txt"
git -C "$REPO" add fixture.txt
git -C "$REPO" commit -qm "fixture commit"
MERGE_SHA="$(git -C "$REPO" rev-parse HEAD)"

# --- stub datawatch on PATH -------------------------------------------------
BIN="$WORK/bin"
mkdir -p "$BIN"
LOG="$WORK/calls.log"
PRDS="$WORK/prds.json"
export PATH="$BIN:$PATH"

# STUB_MODE is baked into each stub:
#   ok        config on, prd list served, records accepted
#   off       autonomous.shipped.enabled read as false
#   fail      records refused (exit 1), the rest works
#   nodaemon  every call fails, like an unreachable daemon
write_stub() {
    local mode="$1"
    cat > "$BIN/datawatch" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "$LOG"
mode="$mode"
if [[ "\$mode" == "nodaemon" ]]; then
    echo "stub: daemon unreachable" >&2
    exit 1
fi
case "\${1:-} \${2:-}" in
    "config get")
        if [[ "\$mode" == "off" && "\${3:-}" == "autonomous.shipped.enabled" ]]; then
            echo false
        else
            echo true
        fi
        exit 0 ;;
    "autonomous prd-list")
        cat "$PRDS"
        exit 0 ;;
    "autonomous shipped")
        if [[ "\$mode" == "fail" ]]; then
            echo "stub: refusing the shipped record" >&2
            exit 1
        fi
        exit 0 ;;
esac
exit 0
EOF
    chmod +x "$BIN/datawatch"
}

# --- scenario fixtures -------------------------------------------------------
PRD_ID=89560001
AUTO_ID=auto-1
RELEASE=v9.54.1
AT=2026-11-01T12:00:00Z

write_prds_all_shipped() {
    cat > "$PRDS" <<EOF
[
  {"id": "$PRD_ID", "title": "member one", "automation_id": "$AUTO_ID", "merge_commit": "$MERGE_SHA"},
  {"id": "89560002", "title": "member two", "automation_id": "$AUTO_ID", "merge_commit": "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}
]
EOF
}

write_prds_one_unshipped() {
    cat > "$PRDS" <<EOF
[
  {"id": "$PRD_ID", "title": "member one", "automation_id": "$AUTO_ID", "merge_commit": "$MERGE_SHA"},
  {"id": "89560002", "title": "member two", "automation_id": "$AUTO_ID"}
]
EOF
}

# --- assertion helpers -------------------------------------------------------
FAILURES=0

check_rc() {
    if [[ $1 -ne 0 ]]; then
        echo "✗ record step exited $1 (a failing daemon must not fail the merge)" >&2
        FAILURES=$((FAILURES + 1))
    fi
}

expect_call() {
    local needle="$1"
    if grep -qxF "$needle" "$LOG"; then
        echo "  ✓ logged: $needle"
    else
        echo "  ✗ expected logged call: $needle" >&2
        echo "  -- actual calls: --" >&2
        sed 's/^/  | /' "$LOG" >&2
        FAILURES=$((FAILURES + 1))
    fi
}

expect_no_call_matching() {
    local pattern="$1"
    if grep -q -- "$pattern" "$LOG"; then
        echo "  ✗ did not expect calls matching: $pattern" >&2
        grep -- "$pattern" "$LOG" | sed 's/^/  | /' >&2
        FAILURES=$((FAILURES + 1))
    else
        echo "  ✓ no call matching: $pattern"
    fi
}

reset_log() { : > "$LOG"; }

# Load the helper's functions (it defines none of the test's variables and
# sources nothing itself).
source "$HELPER"

AUTOMATION_CALL="autonomous shipped record $AUTO_ID --automation --release $RELEASE --commit $MERGE_SHA --at $AT --source merge_step"
PRD_CALL="autonomous shipped record $PRD_ID --release $RELEASE --commit $MERGE_SHA --at $AT --source merge_step"
CONFIG_CALL="config get autonomous.shipped.enabled"

# --- 1+2. enabled by default, all members shipped ---------------------------
echo "scenario: record the automaton and, all members shipped, the automation"
write_stub ok
write_prds_all_shipped
reset_log
rc=0
record_shipped_for_branch "automaton/$PRD_ID" "$RELEASE" "$MERGE_SHA" "$AT" || rc=$?
check_rc "$rc"
expect_call "$CONFIG_CALL"
expect_call "$PRD_CALL"
expect_call "$AUTOMATION_CALL"

# --- 3. one member not shipped ----------------------------------------------
echo "scenario: a member without a merge commit -> no automation call"
write_prds_one_unshipped
reset_log
record_shipped_for_branch "automaton/$PRD_ID" "$RELEASE" "$MERGE_SHA" "$AT"
expect_call "$PRD_CALL"
expect_no_call_matching "--automation"

# --- 4. branch not in the automaton namespace --------------------------------
echo "scenario: a branch that is not automaton/<id> -> no record call"
write_stub ok
reset_log
record_shipped_for_branch "feature/some-thing" "$RELEASE" "$MERGE_SHA" "$AT"
expect_no_call_matching "shipped record"

# --- 5. autonomous.shipped.enabled off --------------------------------------
echo "scenario: autonomous.shipped.enabled is off -> no record call"
write_stub off
reset_log
record_shipped_for_branch "automaton/$PRD_ID" "$RELEASE" "$MERGE_SHA" "$AT"
expect_no_call_matching "shipped record"

# --- 6. the daemon refuses the record ---------------------------------------
echo "scenario: a failing stub does not fail the step"
write_stub fail
write_prds_all_shipped
reset_log
stderr="$WORK/fail.stderr"
record_shipped_for_branch "automaton/$PRD_ID" "$RELEASE" "$MERGE_SHA" "$AT" 2> "$stderr"
check_rc "$?"
expect_call "$PRD_CALL"
if grep -q "warning" "$stderr"; then
    echo "  ✓ warning printed: $(head -1 "$stderr")"
else
    echo "  ✗ expected a warning on stderr" >&2
    FAILURES=$((FAILURES + 1))
fi
expect_no_call_matching "--automation"

# --- 7. the daemon is unreachable --------------------------------------------
echo "scenario: daemon unreachable -> the step still exits 0"
write_stub nodaemon
reset_log
rc=0
record_shipped_for_branch "automaton/$PRD_ID" "$RELEASE" "$MERGE_SHA" "$AT" 2> "$WORK/nodaemon.stderr" || rc=$?
check_rc "$rc"
if [[ $rc -eq 0 ]]; then
    echo "  ✓ step exited 0 despite the unreachable daemon"
else
    echo "✗ step exited $rc with an unreachable daemon (must not fail the merge)" >&2
    FAILURES=$((FAILURES + 1))
fi

# --- result -------------------------------------------------------------------
echo
if [[ $FAILURES -eq 0 ]]; then
    echo "ok: all shipped-record assertions passed"
    exit 0
fi
echo "✗ $FAILURES assertion(s) failed" >&2
exit 1
