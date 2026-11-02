#!/usr/bin/env bash
#
# test-seed-leak-patterns.sh
#
# Self-contained test suite for scripts/seed-leak-patterns.sh (seeds the
# operator's .leak-patterns.local with the names the local daemon knows —
# federation peers, compute nodes, observer peers, pool hosts).
#
# Design rules this suite follows:
#   * No real peer/node names appear anywhere: every name is a generic
#     fixture composed with printf, so the leak check over this file itself
#     stays clean and running the suite can never seed a real name.
#   * Every case runs through the LEAK_SEED_NAMES_CMD test hook (daemon
#     stand-in) or, for the unreachable case, points DW_BASE at a loopback
#     port nothing listens on — no daemon is ever started.
#   * All fixtures live in a fresh `mktemp -d` directory removed on the exit
#     path; the working tree is never touched.
#   * The script under test is honoured from LEAK_SEED_SCRIPT (default:
#     scripts/seed-leak-patterns.sh; relative paths resolve against the
#     repository root). The leak checker used by the integration cases is
#     honoured from LEAK_CHECK_SCRIPT (default:
#     scripts/check-no-local-leaks.sh, resolved the same way).
#   * Every script invocation runs under `env -u TMUX -u TMUX_TMPDIR`;
#     no tmux server commands, no kill by name, no git mutations outside
#     the throwaway `mktemp -d` trees the integration cases commit.
#   * The script's environment is explicit per case: LEAK_SEED_NAMES_CMD,
#     DW_BASE, DW_BASE_TOKEN and DW_CONFIG are unset in this suite's
#     environment and only ever set here, one case at a time; the
#     LEAK_CHECK_* variables likewise only ever reach the checker
#     per-invocation.
#   * The integration cases use one fake name composed at run time from
#     fragments with a zz-fake prefix (printf %s, never a literal), seed
#     it the same way an operator would, and then run the real leak
#     checker over throwaway git trees — including a case-variant hit —
#     with LEAK_CHECK_LOCAL pointing at the seeded file. The combined
#     stdout and stderr of the seeding step and of the checker step must
#     never contain the fake name.
#
# Usage:
#   bash tests/test-seed-leak-patterns.sh
#   LEAK_SEED_SCRIPT=scripts/other-seed.sh bash tests/test-seed-leak-patterns.sh
#
# Output: one `ok <case>` or `FAIL <case> — reason` line per case, then a
# summary. Exit 0 when every case passed, 1 otherwise (2 on setup error).

set -u

SCRIPT_OVERRIDE="${LEAK_SEED_SCRIPT:-scripts/seed-leak-patterns.sh}"
unset LEAK_SEED_NAMES_CMD DW_BASE DW_BASE_TOKEN DW_CONFIG 2>/dev/null || true

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.."
case "$SCRIPT_OVERRIDE" in
../*) SEED_SCRIPT="$SCRIPT_DIR/${SCRIPT_OVERRIDE#../}" ;;
./*) SEED_SCRIPT="$SCRIPT_DIR/${SCRIPT_OVERRIDE#./}" ;;
*) SEED_SCRIPT="$SCRIPT_DIR/$SCRIPT_OVERRIDE" ;;
esac
if [ ! -f "$SEED_SCRIPT" ]; then
    echo "setup: seed script not found: $SEED_SCRIPT" >&2
    exit 2
fi

CHECKER_OVERRIDE="${LEAK_CHECK_SCRIPT:-scripts/check-no-local-leaks.sh}"
unset LEAK_CHECK_CLASSES LEAK_CHECK_PATTERNS LEAK_CHECK_ALLOWED_EMAILS \
      LEAK_CHECK_ALLOWED_ADDRESSES LEAK_CHECK_EXCLUDE LEAK_CHECK_LOCAL \
      LEAK_CHECK_SCRIPT 2>/dev/null || true
CHECKER=""
case "$CHECKER_OVERRIDE" in
../*) CHECKER="$SCRIPT_DIR/${CHECKER_OVERRIDE#../}" ;;
./*)  CHECKER="$SCRIPT_DIR/${CHECKER_OVERRIDE#./}" ;;
*)    CHECKER="$SCRIPT_DIR/$CHECKER_OVERRIDE" ;;
esac
if [ ! -f "$CHECKER" ]; then
    echo "setup: leak checker not found: $CHECKER" >&2
    exit 2
fi

W="$(mktemp -d)" || { echo "setup: mktemp failed" >&2; exit 2; }
case "$W" in "$PWD"* | *"$PWD"*)
    echo "setup: work dir collides with the current directory: $W" >&2
    rm -rf "$W"
    exit 2
    ;;
esac
trap 'rm -rf "$W"' EXIT

PASS=0
FAILN=0
ok() {
    echo "ok $1"
    PASS=$((PASS + 1))
}
fail() {
    echo "FAIL $1 — $2"
    FAILN=$((FAILN + 1))
}

# run_in CASE_DIR -> runs the script under test with cwd CASE_DIR
run_seeded() { # DIR [ARG...]
    local dir="$1"
    shift
    (
        cd "$dir" || exit 9
        env -u TMUX -u TMUX_TMPDIR bash "$SEED_SCRIPT" "$@"
    )
}

# Fixture name list for the hook (generic names, composed with printf).
NAMES_HOOK='printf "%s\n" "alpha-1" "Alpha-1" "  beta-2  " "" "# not-a-name" "gamma-3"'

# --------------------------------------------------------------- case 1
D="$W/c1"
mkdir -p "$D"
out="$(LEAK_SEED_NAMES_CMD="$NAMES_HOOK" run_seeded "$D" .leak-patterns.local)"
if [ "$out" != "seeded 3 names" ]; then
    fail "creates absent target from template and seeds names" "stdout is [$out], want exactly 'seeded 3 names'"
elif ! head -1 "$D/.leak-patterns.local" | grep -q "leak-patterns.local.template"; then
    fail "creates absent target from template and seeds names" "file was not created from the template"
elif [ "$(grep -cE '^[[:space:]]*#[[:space:]]*seeded peer names[[:space:]]*$' "$D/.leak-patterns.local")" != "1" ]; then
    fail "creates absent target from template and seeds names" "marker appears zero or more than once"
else
    want="alpha-1 beta-2 gamma-3 "
    got="$(sed -n '/^# seeded peer names$/,$p' "$D/.leak-patterns.local" | tail -n +2 | tr '\n' ' ')"
    if [ "$got" = "$want" ] && ! grep -q "# not-a-name" "$D/.leak-patterns.local"; then
        ok "creates absent target from template and seeds names"
    else
        fail "creates absent target from template and seeds names" "seeded lines are wrong ($got) or the hook's comment line leaked into the file"
    fi
fi

# --------------------------------------------------------------- case 2
D="$W/c2"
mkdir -p "$D"
printf 'sentinel-line-0\n' >"$D/t"
out="$(LEAK_SEED_NAMES_CMD="$NAMES_HOOK" run_seeded "$D" t)"
if [ "$out" = "seeded 3 names" ] && head -1 "$D/t" | grep -q "sentinel-line-0"; then
    ok "never overwrites an existing target"
else
    fail "never overwrites an existing target" "output or the pre-existing content changed (out=[$out])"
fi

# --------------------------------------------------------------- case 3
D="$W/c3"
mkdir -p "$D"
printf 'Alpha-1\nother-9\n' >"$D/t"
out="$(LEAK_SEED_NAMES_CMD="$NAMES_HOOK" run_seeded "$D" t)"
if [ "$out" = "seeded 2 names" ] \
    && [ "$(grep -ci 'alpha-1' "$D/t")" = "1" ] \
    && grep -q 'beta-2' "$D/t" && grep -q 'gamma-3' "$D/t"; then
    ok "dedups new names against existing lines case-insensitively"
else
    fail "dedups new names against existing lines case-insensitively" "out=[$out] file:"$(grep -vE '^#|^$' "$D/t" | tr '\n' '|')
fi

# --------------------------------------------------------------- case 4
D="$W/c4"
mkdir -p "$D"
out="$(LEAK_SEED_NAMES_CMD="$NAMES_HOOK" run_seeded "$D" t)"
out2="$(LEAK_SEED_NAMES_CMD="$NAMES_HOOK" run_seeded "$D" t)"
if [ "$out2" = "seeded 0 names" ] \
    && [ "$(grep -cE '^[[:space:]]*#[[:space:]]*seeded peer names[[:space:]]*$' "$D/t")" = "1" ]; then
    ok "re-running seeds nothing new and keeps one marker"
else
    fail "re-running seeds nothing new and keeps one marker" "second run out=[$out2]"
fi

# --------------------------------------------------------------- case 7
# A candidate name shorter than 5 characters is refused: it is not
# seeded, the normal name alongside it is, and the skip is reported as
# a count only ('skipped 1 generic names', no name anywhere in output).
D="$W/c7"
mkdir -p "$D"
K7_HOOK='printf "%s\\n" "kb" "delta-5"'
out="$(LEAK_SEED_NAMES_CMD="$K7_HOOK" run_seeded "$D" t)"
if [ "$out" = "$(printf '%s\n' 'seeded 1 names' 'skipped 1 generic names')" ] \
    && [ "$(grep -cvE '^[[:space:]]*(#|$)' "$D/t")" = "1" ] \
    && [ "$(grep -c 'delta-5' "$D/t")" = "1" ] \
    && ! grep -vE '^[[:space:]]*(#|$)' "$D/t" | grep -q 'kb'; then
    ok "2-character name is skipped with 'skipped 1 generic names'; normal name seeded"
else
    fail "2-character name is skipped with 'skipped 1 generic names'; normal name seeded" \
        "out=[$(tr '\n' '|' <<<"$out")] non-comment-lines=$(grep -cvE '^[[:space:]]*(#|$)' "$D/t")"
fi

# --------------------------------------------------------------- case 8
# A candidate name equal to a whole word in scripts/leak-patterns.skip
# is refused the same way: not seeded, normal name seeded, count-only
# skip notice. The stoplist word used here is the literal word on line 19
# of that tracked file ('server') — an ordinary English word, safe to
# write in this test.
D="$W/c8"
mkdir -p "$D"
K8_HOOK='printf "%s\\n" "server" "omega-6"'
out="$(LEAK_SEED_NAMES_CMD="$K8_HOOK" run_seeded "$D" t)"
if [ "$out" = "$(printf '%s\n' 'seeded 1 names' 'skipped 1 generic names')" ] \
    && [ "$(grep -cvE '^[[:space:]]*(#|$)' "$D/t")" = "1" ] \
    && grep -q 'omega-6' "$D/t" \
    && ! grep -vE '^[[:space:]]*(#|$)' "$D/t" | grep -qi 'server'; then
    ok "stoplist name is skipped with 'skipped 1 generic names'; normal name seeded"
else
    fail "stoplist name is skipped with 'skipped 1 generic names'; normal name seeded" \
        "out=[$(tr '\n' '|' <<<"$out")] non-comment-lines=$(grep -cvE '^[[:space:]]*(#|$)' "$D/t")"
fi

# --------------------------------------------------------------- case 9
# Both refusals in one run: one short name plus one stoplist word are
# both skipped (count 2) while the normal name is the sole seeded line.
D="$W/c9"
mkdir -p "$D"
K9_HOOK='printf "%s\\n" "jq" "cluster" "theta-7"'
out="$(LEAK_SEED_NAMES_CMD="$K9_HOOK" run_seeded "$D" t)"
if [ "$out" = "$(printf '%s\n' 'seeded 1 names' 'skipped 2 generic names')" ] \
    && [ "$(grep -cvE '^[[:space:]]*(#|$)' "$D/t")" = "1" ] \
    && grep -q 'theta-7' "$D/t"; then
    ok "short and stoplist names both skipped in one run ('skipped 2 generic names')"
else
    fail "short and stoplist names both skipped in one run ('skipped 2 generic names')" \
        "out=[$(tr '\n' '|' <<<"$out")] non-comment-lines=$(grep -cvE '^[[:space:]]*(#|$)' "$D/t")"
fi

# --------------------------------------------------------------- case 5
D="$W/c5"
mkdir -p "$D"
env -u TMUX -u TMUX_TMPDIR \
    DW_BASE="$(printf 'http://127.0.%s:%s' "0.1" "1")" \
    bash "$SEED_SCRIPT" "$D/t" >"$W/c5.out" 2>"$W/c5.err"
rc=$?
if [ "$rc" = "0" ] \
    && grep -q "not reachable" "$W/c5.out" \
    && [ "$(wc -l < "$W/c5.out")" = "1" ] \
    && [ -f "$D/t" ] \
    && ! grep -vE '^[[:space:]]*(#|$)' "$D/t" | grep -q .; then
    ok "daemon unreachable: one 'not reachable' line, nothing seeded, exit 0"
else
    fail "daemon unreachable: one 'not reachable' line, nothing seeded, exit 0" \
        "rc=$rc out=[$(tr '\n' '|' < "$W/c5.out")]"
fi

# --------------------------------------------------------------- case 6
# Neither jq nor python3: seeds nothing, graceful, exit 0, count printed.
D="$W/c6"
mkdir -p "$D" "$W/nojson"
for tool in bash sh cp cat grep awk tr tail head mktemp dirname printf sed rm wc; do
    p="$(command -v "$tool" 2>/dev/null)" || continue
    ln -s "$p" "$W/nojson/$(basename "$tool")" 2>/dev/null
done
out="$(
    cd "$D" || exit 9
    env -u TMUX -u TMUX_TMPDIR PATH="$W/nojson" \
        DW_BASE="$(printf 'http://127.0.%s:%s' "0.1" "1")" \
        bash "$SEED_SCRIPT" t
)"
rc=$?
if [ "$out" = "seeded 0 names" ] && [ "$rc" = "0" ]; then
    ok "no jq and no python3: graceful run, nothing seeded, exit 0"
else
    fail "no jq and no python3: graceful run, nothing seeded, exit 0" "out=[$out] rc=$rc"
fi

# ------------------------------------------------------ leak-check integration
# One fake name, composed at run time from fragments with a zz-fake prefix,
# is the sole fixture for cases i1..i5: no tracked file holds it, and the
# case-variant spelling is derived at run time.
FN_P1="zz-"
FN_P2="fake-"
FN_P3="cypress-41"
FN="$(printf '%s%s%s' "$FN_P1" "$FN_P2" "$FN_P3")"
VARN="$(printf '%s' "$FN" | tr '[:lower:]' '[:upper:]')"

# run_check DIR [ENV=VALUE ...] — runs the leak checker over one throwaway
# git tree with exactly the given environment assignments (nothing else is
# set); rc in C_RC, combined stdout+stderr in C_OUT.
run_check() {
    local dir="$1"
    shift
    if (( $# > 0 )); then
        C_OUT="$(env -u TMUX -u TMUX_TMPDIR "$@" bash "$CHECKER" "$dir" 2>&1)"
    else
        C_OUT="$(env -u TMUX -u TMUX_TMPDIR bash "$CHECKER" "$dir" 2>&1)"
    fi
    C_RC=$?
}

# has_name TEXT — true when TEXT contains the fake name (either spelling)
has_name() {
    printf '%s\n' "$1" | grep -qiF -- "$FN"
}

# new_git_tree DIR FILE LINE — throwaway work tree: one tracked text file
new_git_tree() {
    local dir="$1" f="$2" line="$3"
    mkdir -p "$dir"
    printf '%s\n' "$line" >"$dir/$f"
    git -C "$dir" init -q
    git -C "$dir" config user.name "t"
    git -C "$dir" config user.email "t@example.invalid"
    git -C "$dir" add -A
    git -C "$dir" commit -qm "init"
}

# --------------------------------------------------------------- case i1
# (a) Seeding a name plus a case-variant duplicate lands exactly one
# pattern line in the seeded file.
D="$W/i1"
mkdir -p "$D"
I1_HOOK="$(printf "printf '%%s\\n' '%s' '%s'" "$FN" "$VARN")"
out="$(LEAK_SEED_NAMES_CMD="$I1_HOOK" run_seeded "$D" patterns)"
PAT="$D/patterns"
if [ "$out" = "seeded 1 names" ] \
    && [ "$(grep -ci -- "$FN" "$PAT")" = "1" ] \
    && [ "$(grep -cvE '^[[:space:]]*(#|$)' "$PAT")" = "1" ]; then
    ok "seeding name plus case-variant duplicate leaves exactly one pattern line"
else
    fail "seeding name plus case-variant duplicate leaves exactly one pattern line" "out=[$out] pattern-lines=$(grep -ci -- "$FN" "$PAT" 2>/dev/null)"
fi

# --------------------------------------------------------------- case i2
# (b) A tree whose only line is the name in a different case is reported
# by the checker as `path:line: local` and the run exits 1.
D="$W/i2"
new_git_tree "$D" "notes.txt" "$VARN"
run_check "$D" "LEAK_CHECK_CLASSES=local" "LEAK_CHECK_LOCAL=$PAT"
if [ "$C_RC" = "1" ] && printf '%s\n' "$C_OUT" | grep -Eq '^notes\.txt:1: local$' \
    && printf '%s\n' "$C_OUT" | grep -q '^local: 1$'; then
    ok "tree with the name in different case is reported as notes.txt:1: local, exit 1"
else
    fail "tree with the name in different case is reported as notes.txt:1: local, exit 1" "rc=$C_RC report-lines=$(printf '%s\n' "$C_OUT" | grep -cE '^[^ ]+:[0-9]+: local')"
fi

# --------------------------------------------------------------- case i3
# (c) The combined stdout+stderr of the seeding step (i1) and of the
# checker step (i2) must never contain the fake name.
if has_name "$out" || has_name "$C_OUT"; then
    fail "seeding output and checker output never contain the fake name" "a captured step output holds the name"
else
    ok "seeding output and checker output never contain the fake name"
fi

# --------------------------------------------------------------- case i4
# (d) Unreachable / empty name source: nothing seeded, the notice is
# printed, exit 0.
D="$W/i4"
mkdir -p "$D"
D4_OUT="$W/i4.out"
D4_ERR="$W/i4.err"
( cd "$D" || exit 9
  LEAK_SEED_NAMES_CMD=false env -u TMUX -u TMUX_TMPDIR bash "$SEED_SCRIPT" t \
      >"$D4_OUT" 2>"$D4_ERR" )
rc=$?
D4_COMBINED="$(cat "$D4_OUT" "$D4_ERR" 2>/dev/null)"
if [ "$rc" = "0" ] \
    && ! has_name "$D4_COMBINED" \
    && printf '%s\n' "$D4_COMBINED" | grep -q 'seeded 0 names' \
    && printf '%s\n' "$D4_COMBINED" | grep -qi -- 'nothing seeded' \
    && ! grep -vE '^[[:space:]]*(#|$)' "$D/t" 2>/dev/null | grep -q .; then
    ok "unreachable name source: nothing seeded, notice printed, exit 0"
else
    fail "unreachable name source: nothing seeded, notice printed, exit 0" "rc=$rc lines=$(printf '%s\n' "$D4_COMBINED" | tr '\n' '|')"
fi

# --------------------------------------------------------------- case i5
# (e) A clean tree passes: exit 0, no local finding, the checker still
# reads the seeded local patterns file.
D="$W/i5"
new_git_tree "$D" "notes.txt" "nothing to see in this note"
run_check "$D" "LEAK_CHECK_CLASSES=local" "LEAK_CHECK_LOCAL=$PAT"
if [ "$C_RC" = "0" ] && printf '%s\n' "$C_OUT" | grep -q '^local: 0$' \
    && ! has_name "$C_OUT"; then
    ok "clean tree passes against the seeded local patterns file, exit 0"
else
    fail "clean tree passes against the seeded local patterns file, exit 0" "rc=$C_RC finding-lines=$(printf '%s\n' "$C_OUT" | grep -cE '^[^ ]+:[0-9]+: local')"
fi

echo "----------------------------------------"
echo "cases passed: $PASS, failed: $FAILN"
if [ "$FAILN" -eq 0 ]; then
    exit 0
fi
exit 1
