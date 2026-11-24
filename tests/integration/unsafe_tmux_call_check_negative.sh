#!/usr/bin/env bash
# Negative regression for scripts/check-unsafe-tmux-calls.sh: the checker must
# FAIL on every unsafe tmux call shape, name the file and line, and pass on the
# scoped forms. Fixtures are built at run time in a temp dir (never executed,
# never run against tmux), and the unsafe words are assembled from pieces so
# this file does not trip the repo scan itself.
#
# Run: env -u TMUX -u TMUX_TMPDIR bash tests/integration/unsafe_tmux_call_check_negative.sh

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECK="$ROOT/scripts/check-unsafe-tmux-calls.sh"
[[ -f "$CHECK" ]] || { echo "FAIL checker missing: $CHECK" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

KS="kill-""server"
KSESS="kill-""session"
FAILS=0
fail() { echo "  FAIL $1" >&2; FAILS=$((FAILS + 1)); }

# fixture <name> <line>: writes a one-line shell fixture.
fixture() {
    mkdir -p "$WORK/$1"
    printf '#!/usr/bin/env bash\n%s\n' "$2" >"$WORK/$1/f.sh"
}

# expect_flagged <label> <line>: the checker exits 1 and names f.sh:2.
expect_flagged() {
    fixture "$1" "$2"
    local out rc
    set +e
    out="$(bash "$CHECK" "$WORK/$1" 2>&1)"
    rc=$?
    set -e
    [[ $rc -eq 1 ]] || fail "$1: exit $rc, want 1"
    [[ "$out" == *"f.sh:2:"* ]] || fail "$1: output does not name f.sh:2 ($out)"
}

# expect_clean <label> <line>: the checker exits 0.
expect_clean() {
    fixture "$1" "$2"
    local out rc
    set +e
    out="$(bash "$CHECK" "$WORK/$1" 2>&1)"
    rc=$?
    set -e
    [[ $rc -eq 0 ]] || fail "$1: exit $rc, want 0 ($out)"
}

expect_flagged "server-kill-bare" "tmux $KS"
expect_flagged "server-kill-tmpdir-only" "TMUX_TMPDIR=/x tmux $KS"
expect_flagged "server-kill-after-semicolon" "echo hi; tmux $KS"
expect_flagged "session-kill-all" "tmux $KSESS -a"
expect_flagged "session-kill-all-after-pipe" "true | tmux $KSESS -a"
# The fixture words are assembled at run time so this file never carries the banned literals.
PK_WORD="pk""ill"; KA_WORD="kill""all"
expect_flagged "name-kill-tmux" "$PK_WORD -9 tmux"
expect_flagged "name-killall-tmux" "$KA_WORD tmux"

expect_clean "server-kill-scoped-socket" "tmux -S /tmp/throwaway.sock $KS"
expect_clean "session-kill-by-target" "tmux $KSESS -t one"
expect_clean "comment-only" "# tmux $KS"
expect_clean "plain-list" "tmux list-sessions"

# An empty scan scope fails closed, a missing target is a usage error.
mkdir -p "$WORK/empty"
set +e
bash "$CHECK" "$WORK/empty" >/dev/null 2>&1
[[ $? -eq 1 ]] || fail "empty scope: want exit 1"
bash "$CHECK" "$WORK/does-not-exist" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "missing target: want exit 2"
set -e

if ((FAILS > 0)); then
    echo "unsafe_tmux_call_check_negative: $FAILS failure(s)" >&2
    exit 1
fi
echo "unsafe_tmux_call_check_negative: ok"
