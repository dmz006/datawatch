#!/usr/bin/env bash
#
# test-check-no-local-leaks.sh
#
# Self-contained test suite for scripts/check-no-local-leaks.sh (the
# whole-tree local-environment leak check).
#
# Design rules this suite follows:
#   * No tracked file contains a literal leak. Every leak sample is
#     composed at run time from non-leaking fragments with printf, so
#     running the leak check over this file itself stays clean.
#   * All fixtures live in a fresh `mktemp -d` directory that is removed
#     on the exit path; the working tree is never touched.
#   * The checker under test is honoured from LEAK_CHECK_SCRIPT (default:
#     scripts/check-no-local-leaks.sh; relative paths resolve against the
#     repository root), so a substitute checker can be pointed at the same
#     suite. A checker that never flags and one that always flags both
#     fail at least one expectation below, so the suite exits non-zero
#     against either.
#   * Every checker invocation runs under `env -u TMUX -u TMUX_TMPDIR`;
#     no tmux server commands, no kill by name, no git mutations.
#   * The checker's environment is explicit per case: the LEAK_CHECK_*
#     override variables are unset in this suite's environment and are
#     only ever set here, one case at a time.
#
# Usage:
#   bash tests/test-check-no-local-leaks.sh
#   LEAK_CHECK_SCRIPT=scripts/other-checker.sh bash tests/test-check-no-local-leaks.sh
#
# Output: one `ok <case>` or `FAIL <case> — reason` line per case, then a
# summary. Exit 0 when every case passed, 1 otherwise (2 on setup error).

set -u

# ------------------------------------------------------------- environment
# The suite's own surface is only LEAK_CHECK_SCRIPT; every other
# LEAK_CHECK_* override is cleared so ambient values cannot leak into a
# case, and each case sets exactly the variables it needs.
SCRIPT_OVERRIDE="${LEAK_CHECK_SCRIPT:-scripts/check-no-local-leaks.sh}"
unset LEAK_CHECK_CLASSES LEAK_CHECK_PATTERNS LEAK_CHECK_ALLOWED_EMAILS \
      LEAK_CHECK_ALLOWED_ADDRESSES LEAK_CHECK_EXCLUDE LEAK_CHECK_LOCAL LEAK_CHECK_SCRIPT
REPO="$(cd "$(dirname "$0")/.." && pwd)"
case "$SCRIPT_OVERRIDE" in
    /*) CHECKER="$SCRIPT_OVERRIDE" ;;
    *)  CHECKER="$REPO/${SCRIPT_OVERRIDE}" ;;
esac
if [[ ! -f "$CHECKER" ]]; then
    echo "FAIL setup — checker not found: $CHECKER"
    exit 2
fi

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

# --------------------------------------------------------------- utilities
P4() { printf '%s.%s.%s.%s' "$1" "$2" "$3" "$4"; }   # an IPv4 from 4 octets

PASS=0
FAILN=0
RC=""
OUT=""
ERR=""

ok()  { printf 'ok %s\n' "$1"; PASS=$((PASS + 1)); }
bad() { printf 'FAIL %s — %s\n' "$1" "$2"; FAILN=$((FAILN + 1)); }

# run_checker [ENV=value ...] -- arg...
# Runs the checker with exactly the given environment assignments (plus
# this suite's cleaned environment) and the given arguments; stdout and
# stderr are captured into $T/out and $T/err. The child's exit status is
# the function's.
run_checker() {
    local -a A=()
    while [[ $# -gt 0 && "$1" != "--" ]]; do
        A+=("$1")
        shift
    done
    [[ $# -ge 1 ]] || { echo "FAIL usage — run_checker needs -- <root>" >&2; exit 2; }
    shift
    LC_ALL=C env -u TMUX -u TMUX_TMPDIR ${A[@]+"${A[@]}"} "$CHECKER" "$@" \
        >"$T/out" 2>"$T/err"
}

# do_run <root> [ENV=value ...]  — sets RC, OUT, ERR
do_run() {
    local root="$1"
    shift
    run_checker "$@" -- "$root"
    RC=$?
    OUT="$(cat "$T/out")"
    ERR="$(cat "$T/err")"
}

# mkfx <dir> — creates $T/<dir>/f.txt (empty); the case then writes it
mkfx() {
    mkdir -p "$T/$1"
    : > "$T/$1/f.txt"
}

# findings_in <text> — true when the text holds a path:line: finding
findings_in() {
    printf '%s\n' "$1" | grep -Eq '^[^ :]+:[0-9]+: '
}

# contains <needle> — true when the captured report or notice output holds
# the fixed string (checked case-insensitively only when called with a
# case-insensitive marker, callers use -i via contains_i)
contains()  { printf '%s\n' "$OUT" | grep -qF -- "$1" || printf '%s\n' "$ERR" | grep -qF -- "$1"; }
contains_i() { printf '%s\n' "$OUT" | grep -qiF -- "$1" || printf '%s\n' "$ERR" | grep -qiF -- "$1"; }

# expect_pass <name> — RC must be 0 and the report must hold no finding
expect_pass() {
    local name="$1" reason=""
    [[ "$RC" -eq 0 ]] || reason="expected exit 0, got $RC"
    if [[ -z "$reason" ]] && { findings_in "$OUT" || findings_in "$ERR"; }; then
        reason="report holds a finding line"
    fi
    [[ -z "$reason" ]] && ok "$name" || bad "$name" "$reason"
}

# expect_pass_skipped <name> <notice-fragment> — exit 0, no finding, and
# the skip notice (matched case-insensitively) present in the captured
# notice output. One verdict line per case.
expect_pass_skipped() {
    local name="$1" notice="$2" reason=""
    [[ "$RC" -eq 0 ]] || reason="expected exit 0, got $RC"
    if [[ -z "$reason" ]] && { findings_in "$OUT" || findings_in "$ERR"; }; then
        reason="report holds a finding line"
    fi
    if [[ -z "$reason" ]] && ! printf '%s\n' "$ERR" | grep -qi -- "$notice"; then
        reason="no '${notice}' notice on stderr"
    fi
    [[ -z "$reason" ]] && ok "$name" || bad "$name" "$reason"
}

# expect_fail <name> <class> <matched>
# RC must be 1, the report must name the fixture file and line
# (f.txt:1: <class>) and must not contain the matched text.
expect_fail() {
    local name="$1" cls="$2" matched="$3" reason=""
    [[ "$RC" -eq 1 ]] || reason="expected exit 1, got $RC"
    if [[ -z "$reason" ]] && ! printf '%s\n' "$OUT" | grep -Eq "^f\.txt:1: ${cls}\$"; then
        reason="report lacks the f.txt:1: ${cls} line"
    fi
    if [[ -z "$reason" ]] && { contains_i "$matched"; }; then
        reason="report contains the matched text"
    fi
    [[ -z "$reason" ]] && ok "$name" || bad "$name" "$reason"
}

# ------------------------------------------------------------------- cases
# Case 1 — placeholders pass: reserved documentation addresses, loopback,
# example-domain hosts and emails, an angle-bracket home placeholder and a
# tilde path. All fragments are harmless on their own.
case_placeholders_pass() {
    local d="c1-pass"
    mkfx "$d"
    {
        printf 'docs endpoint %s ok\n'  "$(P4 192 0 2 12)"
        printf 'docs endpoint %s ok\n'  "$(P4 198 51 100 77)"
        printf 'docs endpoint %s ok\n'  "$(P4 203 0 113 9)"
        printf 'loopback %s ok\n'       "$(P4 127 0 0 1)"
        printf 'site https://cdn.example.com/lib/asset.js\n'
        printf 'mail %s@%s\n' "ops"   "example.com"
        printf 'mail %s@%s\n' "qa"    "example.org"
        printf 'mail %s@%s\n' "dev"   "example.net"
        printf 'dir /home/<user>/projects\n'
        printf 'dir ~/src/notes\n'
    } > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_pass "placeholders pass (documentation ranges, example hosts, <user>, ~, example mail)"
}

# Cases 2 — one failing fixture per class. Each fixture holds exactly one
# line of exactly one sample, so the finding is unambiguously f.txt:1.
case_fail_private10() {
    local d="c2-p10" m
    mkfx "$d"
    m="$(P4 10 20 31 7)"
    printf 'endpoint=%s\n' "$m" > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_fail "private IPv4, first private block, fails and is reported without the address" "address" "$m"
}

case_fail_private172() {
    local d="c2-p172" m
    mkfx "$d"
    m="$(P4 172 30 5 9)"
    printf 'endpoint=%s\n' "$m" > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_fail "private IPv4, second private block, fails and is reported without the address" "address" "$m"
}

case_fail_private192() {
    local d="c2-p192" m
    mkfx "$d"
    m="$(P4 192 168 44 9)"
    printf 'endpoint=%s\n' "$m" > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_fail "private IPv4, third private block, fails and is reported without the address" "address" "$m"
}

case_fail_tailnet() {
    local d="c2-tail" m
    mkfx "$d"
    m="$(P4 100 81 12 3)"
    printf 'endpoint=%s\n' "$m" > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_fail "tailnet-range IPv4 fails and is reported without the address" "address" "$m"
}

case_fail_home() {
    local d="c2-home" m
    mkfx "$d"
    # Composed at run time; nothing in this file sits after "/home/" with
    # a printable user name adjacent.
    m="/home/$(printf '%s' willow)/scratch"
    printf 'logdir=%s\n' "$m" > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_fail "absolute home path for an arbitrary user fails and is reported without the path" "home" "$m"
}

case_fail_email() {
    local d="c2-mail" m
    mkfx "$d"
    m="$(printf '%s@%s' person team-mailbox.dev)"
    printf 'contact %s for review\n' "$m" > "$T/$d/f.txt"
    do_run "$T/$d"
    expect_fail "email not on the allowed list fails and is reported without the address" "email" "$m"
}

case_fail_local() {
    # Local fixed-string hit; the fixture text is a different case than the
    # pattern line, which also proves matching is case-insensitive.
    local d="c2-local" pat
    mkfx "$d"
    pat="zx-orchard-signal-77"
    printf 'ZX-ORCHARD-SIGNAL-77\n' > "$T/$d/f.txt"
    printf '%s\n' "$pat" > "$T/lp-c2"
    do_run "$T/$d" "LEAK_CHECK_LOCAL=$T/lp-c2"
    expect_fail "local-patterns hit (case-insensitive) fails and is reported without the token" "local" "ZX-ORCHARD-SIGNAL-77"
}

# Case 3 — the allowed list is configuration: a custom list via
# LEAK_CHECK_ALLOWED_EMAILS turns the case-2 failing email into a pass.
case_allowed_list_override() {
    local d="c3-allow" m
    mkfx "$d"
    m="$(printf '%s@%s' person team-mailbox.dev)"
    printf 'contact %s for review\n' "$m" > "$T/$d/f.txt"
    printf '*@team-mailbox.dev\n' > "$T/allow-c3"
    do_run "$T/$d" "LEAK_CHECK_ALLOWED_EMAILS=$T/allow-c3"
    expect_pass "custom LEAK_CHECK_ALLOWED_EMAILS list turns the failing email into a pass"
}

# Case 4 — the local patterns file is respected, and its absence (or a
# comment-only body) only skips that part of the check with exit 0.
case_local_file_hit_fails() {
    # Covered by the local failure case above; here we assert the positive
    # side explicitly: pattern present, fixture present -> exit 1 (already
    # asserted) is expected, and an unchanged-clean fixture stays clean
    # with the same file. This case uses the same local file with a clean
    # fixture.
    local d="c4-clean"
    mkfx "$d"
    printf 'nothing sensitive on this line\n' > "$T/$d/f.txt"
    printf 'zx-orchard-signal-77\n' > "$T/lp-c4"
    do_run "$T/$d" "LEAK_CHECK_LOCAL=$T/lp-c4"
    expect_pass "local patterns file set: a clean fixture still passes"
}

case_local_file_absent() {
    local d="c4-absent"
    mkfx "$d"
    printf 'nothing sensitive on this line\n' > "$T/$d/f.txt"
    do_run "$T/$d" "LEAK_CHECK_LOCAL=$T/definitely-absent.local"
    expect_pass_skipped "absent local patterns file: only that part is skipped, exit 0" "local patterns: skipped"
}

case_local_file_comment_only() {
    local d="c4-comment"
    mkfx "$d"
    printf 'nothing sensitive on this line\n' > "$T/$d/f.txt"
    {
        printf '# only a comment in the local patterns file\n'
        printf '\n'
        printf '# nothing active\n'
    } > "$T/lp-c4-comment"
    do_run "$T/$d" "LEAK_CHECK_LOCAL=$T/lp-c4-comment"
    expect_pass_skipped "comment-only local patterns file: only that part is skipped, exit 0" "local patterns: skipped"
}

# Case 5 — LEAK_CHECK_CLASSES restricts the classes being checked.
CASE5_IP="$(P4 10 20 31 7)"
CASE5_MAIL="$(printf '%s@%s' person team-mailbox.dev)"

case5_fixture() {
    local d="c5-mixed"
    mkfx "$d"
    printf 'endpoint=%s\n' "$CASE5_IP" > "$T/$d/f.txt"
    printf 'contact %s for review\n' "$CASE5_MAIL" >> "$T/$d/f.txt"
}

case_classes_home_only_passes() {
    case5_fixture
    do_run "$T/c5-mixed" "LEAK_CHECK_CLASSES=home"
    expect_pass "LEAK_CHECK_CLASSES=home leaves both samples of the mixed fixture unflagged"
}

case_classes_email_only() {
    case5_fixture
    do_run "$T/c5-mixed" "LEAK_CHECK_CLASSES=email"
    local reason=""
    [[ "$RC" -eq 1 ]] || reason="expected exit 1, got $RC"
    if [[ -z "$reason" ]] && ! printf '%s\n' "$OUT" | grep -Eq '^f\.txt:2: email$'; then
        reason="report lacks the f.txt:2: email line"
    fi
    if [[ -z "$reason" ]] && printf '%s\n' "$OUT" | grep -Eq '^f\.txt:1: '; then
        reason="address sample of line 1 was flagged although only email is selected"
    fi
    if [[ -z "$reason" ]] && contains "$CASE5_MAIL"; then
        reason="report contains the matched email"
    fi
    if [[ -z "$reason" ]] && contains "$CASE5_IP"; then
        reason="report contains the address sample (class was not selected)"
    fi
    [[ -z "$reason" ]] \
        && ok "LEAK_CHECK_CLASSES=email flags only the email line" \
        || bad "LEAK_CHECK_CLASSES=email flags only the email line" "$reason"
}

# Allowed addresses: a listed public constant passes; any other address on
# another line is still flagged, and the report never prints either value.
allow_fixture() {
    mkfx c6-allow
    CASE6_META="$(printf '%s%s' '169.254' '.169.254')"
    CASE6_OTHER="$(printf '%s%s' '169.254' '.7.7')"
    printf '%s\n' "$CASE6_META" > "$T/allow.txt"
}

case_allowed_address_passes() {
    allow_fixture
    printf 'refused endpoint %s here\n' "$CASE6_META" > "$T/c6-allow/f.txt"
    do_run "$T/c6-allow" "LEAK_CHECK_CLASSES=address" "LEAK_CHECK_ALLOWED_ADDRESSES=$T/allow.txt"
    expect_pass "an address on the allowed-address list passes"
}

case_allowed_address_other_still_flagged() {
    allow_fixture
    printf 'refused endpoint %s here\n' "$CASE6_META" > "$T/c6-allow/f.txt"
    printf 'other host %s here\n' "$CASE6_OTHER" >> "$T/c6-allow/f.txt"
    do_run "$T/c6-allow" "LEAK_CHECK_CLASSES=address" "LEAK_CHECK_ALLOWED_ADDRESSES=$T/allow.txt"
    local reason=""
    [[ "$RC" -eq 1 ]] || reason="expected exit 1, got $RC"
    if [[ -z "$reason" ]] && ! printf '%s\n' "$OUT" | grep -Eq '^f\.txt:2: address$'; then
        reason="report lacks the f.txt:2: address line"
    fi
    if [[ -z "$reason" ]] && printf '%s\n' "$OUT" | grep -Eq '^f\.txt:1: '; then
        reason="the allowed address on line 1 was flagged"
    fi
    if [[ -z "$reason" ]] && { contains "$CASE6_META" || contains "$CASE6_OTHER"; }; then
        reason="report contains an address value"
    fi
    [[ -z "$reason" ]] \
        && ok "an address outside the allowed list is still flagged" \
        || bad "an address outside the allowed list is still flagged" "$reason"
}

case_classes_address_only() {
    case5_fixture
    do_run "$T/c5-mixed" "LEAK_CHECK_CLASSES=address"
    local reason=""
    [[ "$RC" -eq 1 ]] || reason="expected exit 1, got $RC"
    if [[ -z "$reason" ]] && ! printf '%s\n' "$OUT" | grep -Eq '^f\.txt:1: address$'; then
        reason="report lacks the f.txt:1: address line"
    fi
    if [[ -z "$reason" ]] && printf '%s\n' "$OUT" | grep -Eq '^f\.txt:2: '; then
        reason="email sample of line 2 was flagged although only address is selected"
    fi
    if [[ -z "$reason" ]] && contains "$CASE5_IP"; then
        reason="report contains the matched address"
    fi
    [[ -z "$reason" ]] \
        && ok "LEAK_CHECK_CLASSES=address flags only the address line" \
        || bad "LEAK_CHECK_CLASSES=address flags only the address line" "$reason"
}

# Case 6 — bad usage exits 2.
case_usage_missing_root() {
    do_run "$T/no-such-root"
    local reason=""
    [[ "$RC" -eq 2 ]] || reason="expected exit 2, got $RC"
    [[ -z "$reason" ]] \
        && ok "root directory missing: usage error, exit 2" \
        || bad "root directory missing: usage error, exit 2" "$reason"
}

case_usage_two_args() {
    local a="c6-a" b="c6-b" reason=""
    mkfx "$a"; mkfx "$b"
    # Two positional roots: hand both straight to the checker (do_run
    # accepts exactly one root and would mistake the second for an
    # environment assignment).
    run_checker -- "$T/$a" "$T/$b"
    RC=$?
    OUT="$(cat "$T/out")"
    ERR="$(cat "$T/err")"
    [[ "$RC" -eq 2 ]] || reason="expected exit 2, got $RC"
    [[ -z "$reason" ]] \
        && ok "two root arguments: usage error, exit 2" \
        || bad "two root arguments: usage error, exit 2" "$reason"
}

# --------------------------------------------------------------------- run
case_placeholders_pass
case_fail_private10
case_fail_private172
case_fail_private192
case_fail_tailnet
case_fail_home
case_fail_email
case_fail_local
case_allowed_list_override
case_local_file_hit_fails
case_local_file_absent
case_local_file_comment_only
case_classes_home_only_passes
case_classes_email_only
case_classes_address_only
case_allowed_address_passes
case_allowed_address_other_still_flagged
case_usage_missing_root
case_usage_two_args

# ------------------------------------------------------------------ summary
TOTAL=$((PASS + FAILN))
printf 'summary: %d of %d cases passed, %d failed\n' "$PASS" "$TOTAL" "$FAILN"
if [[ "$FAILN" -ne 0 ]]; then
    exit 1
fi
exit 0
