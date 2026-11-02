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
#     repository root).
#   * Every script invocation runs under `env -u TMUX -u TMUX_TMPDIR`;
#     no tmux server commands, no kill by name, no git mutations.
#   * The script's environment is explicit per case: LEAK_SEED_NAMES_CMD,
#     DW_BASE, DW_BASE_TOKEN and DW_CONFIG are unset in this suite's
#     environment and only ever set here, one case at a time.
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

# --------------------------------------------------------------- summary
echo "----------------------------------------"
echo "cases passed: $PASS, failed: $FAILN"
if [ "$FAILN" -eq 0 ]; then
    exit 0
fi
exit 1
