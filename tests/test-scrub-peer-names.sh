#!/usr/bin/env bash
#
# test-scrub-peer-names.sh
#
# Self-contained test suite for scripts/scrub-peer-names.sh (replaces the
# operator's machine-specific peer/node names in tracked files with fixed
# generic placeholders; patterns come from .leak-patterns.local read at run
# time).
#
# Design rules this suite follows:
#   * No real peer/node names appear anywhere: every name is a generic
#     fixture composed with printf from fragments carrying a zz-fake
#     prefix, so the leak check over this file itself stays clean and the
#     suite can never leak or scrub a real name.
#   * Every case lives in a fresh `mktemp -d` tree removed on the exit
#     path; the working tree is never touched. Patterns files are fakes
#     composed in those trees.
#   * Every script invocation runs under `env -u TMUX -u TMUX_TMPDIR`;
#     no tmux server commands, no kill by name.
#   * Nothing that could carry a name is printed: every captured script
#     output is held in per-case files under the work dir and asserted on
#     with grep -q / -c only; a final case proves no fixture name appears
#     in any captured output.
#
# Cases:
#   1. dry-run counts (`path: N` per file, final `remaining: N`)
#   2. --apply replacement, longer/name mapping, untouched files, clean
#      re-run
#   3. case-insensitivity of replacement
#   4. same name -> same placeholder across two files
#   5. areas split correctly (--list membership, per area, including
#      non-testdata assets that sit beside Go code: those must stay in
#      docs, not go)
#   6. --list: one file per line, exact sets including the `all` union
#   7. find fallback for a non-git root
#   8. placeholder table keeps going past the first five entries
#      (peer-c .. peer-z, then peer-c1, ...) without broken characters
#   9. no fixture name in any output of any case (scans every capture)
#  10. go-area pattern matching more than 500 lines is reported
#      'ambiguous: skipped K' (count only), never applied even with
#      --apply, while the surviving pattern keeps its placeholder
#
# Usage:
#   bash tests/test-scrub-peer-names.sh
#   SCRUB_SCRIPT=scripts/other.sh bash tests/test-scrub-peer-names.sh
#
# Output: one `ok <case>` or `FAIL <case> — reason` line per case, then a
# summary. Exit 0 when every case passed, 1 otherwise (2 on setup error).

set -u

SCRIPT_OVERRIDE="${SCRUB_SCRIPT:-scripts/scrub-peer-names.sh}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.."
case "$SCRIPT_OVERRIDE" in
../*) SCRUB="$SCRIPT_DIR/${SCRIPT_OVERRIDE#../}" ;;
./*)  SCRUB="$SCRIPT_DIR/${SCRIPT_OVERRIDE#./}" ;;
*)    SCRUB="$SCRIPT_DIR/$SCRIPT_OVERRIDE" ;;
esac
if [ ! -f "$SCRUB" ]; then
    echo "setup: scrub script not found: $SCRUB" >&2
    exit 2
fi

W="$(mktemp -d)" || { echo "setup: mktemp failed" >&2; exit 2; }
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

# ---------------------------------------------------------------- fixtures
# Fixture names composed at run time from fragments (zz-fake prefix);
# neither this file nor any fixture holds a real name.
FP1="zz-"
FP2="fake-"
N1="$(printf '%s%s%s' "$FP1" "$FP2" "argos-7")"
N2="$(printf '%s%s%s' "$FP1" "$FP2" "boreal-9")"
N3="$(printf '%s%s%s' "$FP1" "$FP2" "cinder-3")"
N1U="$(printf '%s' "$N1" | tr '[:lower:]' '[:upper:]')"
N2U="$(printf '%s' "$N2" | tr '[:lower:]' '[:upper:]')"

# Placeholder mapping is the fixture list in case-insensitive sorted,
# deduplicated order onto the fixed placeholder table: with N1 < N2 < N3
# sorted, N1 -> remote-node, N2 -> peer-a, N3 -> peer-b.
PAT1="remote-node"
PAT2="peer-a"
PAT3="peer-b"

# Captured outputs of every scrub invocation; case 8 scans all of them.
CAPS="$W/caps"
mkdir -p "$CAPS"
NCAP=0

# run_scrub DIR PATFILE AREA [EXTRA OPTS...]
# Runs the scrub script with cwd DIR; combines stdout+stderr into the next
# capture file and exposes RUN_OUT / RUN_RC.
run_scrub() {
    local dir="$1" pf="$2" area="$3"
    shift 3
    NCAP=$((NCAP + 1))
    local cap="$CAPS/c$NCAP.out"
    if [ $# -gt 0 ]; then
        RUN_OUT="$(
            cd "$dir" || exit 9
            env -u TMUX -u TMUX_TMPDIR bash "$SCRUB" \
                --area "$area" --root "$dir" --patterns "$pf" "$@" 2>&1
        )"
    else
        RUN_OUT="$(
            cd "$dir" || exit 9
            env -u TMUX -u TMUX_TMPDIR bash "$SCRUB" \
                --area "$area" --root "$dir" --patterns "$pf" 2>&1
        )"
    fi
    RUN_RC=$?
    printf '%s\n' "$RUN_OUT" > "$cap"
}

# new_tree DIR — finish a prepared tree: git init + single initial commit.
new_tree() {
    local d="$1"
    git -C "$d" init -q
    git -C "$d" config user.name t
    git -C "$d" config user.email t@example.invalid
    git -C "$d" add -A
    git -C "$d" commit -qm init
}

# list_area DIR PATFILE AREA — run the scrub script with --list in the
# CURRENT shell (never inside a command substitution, so run_scrub's
# NCAP counter and its capture-file write land in the main process);
# the sorted line list ends up in AREA_LIST.
list_area() {
    local dir="$1" pf="$2" area="$3"
    run_scrub "$dir" "$pf" "$area" --list
    printf '%s\n' "$RUN_OUT" | LC_ALL=C sort > "$W/sorted-area"
    AREA_LIST="$(cat "$W/sorted-area")"
}

# names_grep TEXTFILE NAME — true when the file contains NAME
# (case-insensitive, fixed string).
names_grep() {
    grep -qiF -- "$2" "$1" 2>/dev/null
}

# ------------------------------------------------------------------ case 1
T1="$W/t1"
mkdir -p "$T1/internal/pkg/testdata" "$T1/docs/howto"
printf '%s\n' \
    "on $N1 the job lands" \
    "route via $N1 now" \
    "$N1U is back" >"$T1/internal/pkg/a.go"
printf 'x %s y\n' "$N2" >"$T1/internal/pkg/testdata/case.txt"
printf 'untouched %s line for other area\n' "$N1" >"$T1/docs/howto/other.md"
P1F="$W/patterns1"
printf '%s\n%s\n%s\n# comment\n\n' "$N1" "$N2U" "$N3" >"$P1F"
new_tree "$T1"

run_scrub "$T1" "$P1F" go
EXP1="$(printf '%s\n' \
    'internal/pkg/a.go: 3' \
    'internal/pkg/testdata/case.txt: 1' \
    'remaining: 4')"
if [ "$RUN_RC" = "0" ] && [ "$RUN_OUT" = "$EXP1" ]; then
    ok "dry run prints path: N per file plus remaining: N"
else
    fail "dry run prints path: N per file plus remaining: N" \
        "rc=$RUN_RC line-count=$(grep -c . <<<"$RUN_OUT")"
fi

# ------------------------------------------------------------------ case 2
run_scrub "$T1" "$P1F" go --apply
EXP2="$(printf '%s\n' \
    'internal/pkg/a.go: 3' \
    'internal/pkg/testdata/case.txt: 1' \
    'remaining: 0')"
A_OK=1
[ "$RUN_RC" = "0" ] && [ "$RUN_OUT" = "$EXP2" ] || A_OK=0
[ "$(grep -c "$PAT1" "$T1/internal/pkg/a.go")" = "3" ] || A_OK=0
[ "$(grep -c "$PAT2" "$T1/internal/pkg/testdata/case.txt")" = "1" ] || A_OK=0
names_grep "$T1/internal/pkg/a.go" "$N1" && A_OK=0
names_grep "$T1/internal/pkg/a.go" "$N1U" && A_OK=0
names_grep "$T1/internal/pkg/testdata/case.txt" "$N2" && A_OK=0
names_grep "$T1/internal/pkg/testdata/case.txt" "$N2U" && A_OK=0
grep -qiF -- "$N1" "$T1/docs/howto/other.md" || A_OK=0   # outside the go area
run_scrub "$T1" "$P1F" go   # re-run: clean
[ "$RUN_OUT" = "remaining: 0" ] || A_OK=0
if [ "$A_OK" = "1" ]; then
    ok "--apply replaces in place, leaves other areas, re-run is clean"
else
    fail "--apply replaces in place, leaves other areas, re-run is clean" \
        "replacement, exclusivity or re-run assertions failed"
fi

# ------------------------------------------------------------------ case 3
T3="$W/t3"
mkdir -p "$T3"
printf '%s\n%s\n' "$N1" "$N1U" >"$T3/README.md"
new_tree "$T3"
run_scrub "$T3" "$P1F" docs --apply
B_OK=1
[ "$RUN_RC" = "0" ] || B_OK=0
[ "$(grep -c "$PAT1" "$T3/README.md")" = "2" ] || B_OK=0
names_grep "$T3/README.md" "$N1" && B_OK=0
names_grep "$T3/README.md" "$N1U" && B_OK=0
[ "$RUN_OUT" = "$(printf '%s\n' 'README.md: 2' 'remaining: 0')" ] || B_OK=0
if [ "$B_OK" = "1" ]; then
    ok "case-insensitive replacement handles mixed-case spellings"
else
    fail "case-insensitive replacement handles mixed-case spellings" \
        "output or replacement assertions failed"
fi

# -------------------------------------------------------------- case 4
T4="$W/t4"
mkdir -p "$T4/internal/pkg" "$T4/docs/howto"
printf 'one uses %s\n' "$N1" >"$T4/internal/pkg/x.go"
printf 'two uses %s\n' "$N1" >"$T4/docs/howto/y.md"
printf 'three uses %s\n' "$N2" >"$T4/README.md"
printf 'four uses %s\n' "$N3" >"$T4/CHANGELOG.md"
new_tree "$T4"
run_scrub "$T4" "$P1F" all --apply
C_OK=1
[ "$RUN_RC" = "0" ] || C_OK=0
L1="$(grep -c "$PAT1" "$T4/internal/pkg/x.go")"
L2="$(grep -c "$PAT1" "$T4/docs/howto/y.md")"
[ "$L1" = "1" ] && [ "$L2" = "1" ] || C_OK=0   # same placeholder, both files
[ "$(grep -c "$PAT2" "$T4/README.md")" = "1" ] || C_OK=0
[ "$(grep -c "$PAT3" "$T4/CHANGELOG.md")" = "1" ] || C_OK=0
[ "$PAT1" != "$PAT2" ] || C_OK=0
names_grep "$T4/CHANGELOG.md" "$N3" && C_OK=0
names_grep "$T4/internal/pkg/x.go" "$N1" && C_OK=0
names_grep "$T4/docs/howto/y.md" "$N1" && C_OK=0
if [ "$C_OK" = "1" ]; then
    ok "same name maps to the same placeholder in every file"
else
    fail "same name maps to the same placeholder in every file" \
        "placeholder equality or mapping assertions failed"
fi

# -------------------------------------------------------------- case 5
# Areas split: exact --list membership per area.
T5="$W/t5"
mkdir -p "$T5/internal/pkg/testdata" \
    "$T5/internal/mcp" \
    "$T5/internal/server/web" \
    "$T5/internal/channel/embed" \
    "$T5/docs/plans/reports" \
    "$T5/docs/howto" "$T5/scripts" "$T5/tests"
printf 'a\n' >"$T5/internal/pkg/a.go"
printf 'b\n' >"$T5/internal/pkg/testdata/case.txt"
# Non-testdata assets that live inside directories that also hold Go
# code: they belong to the docs area, not the go area.
printf 'g1\n' >"$T5/internal/mcp/mcp.go"
printf 'g2\n' >"$T5/internal/mcp/README.md"
printf 'g3\n' >"$T5/internal/server/web/web.go"
printf 'g4\n' >"$T5/internal/server/web/app.js"
printf 'g5\n' >"$T5/internal/server/web/index.html"
printf 'g6\n' >"$T5/internal/channel/embed/embed.go"
printf 'g7\n' >"$T5/internal/channel/embed/channel.js"
printf 'c\n' >"$T5/docs/plans/2026-11-01-p.md"
printf 'd\n' >"$T5/docs/plans/2026-09-30-q.md"
printf 'e\n' >"$T5/docs/plans/README.md"
printf 'f\n' >"$T5/docs/plans/notes.md"
printf 'g\n' >"$T5/docs/plans/reports/report.md"
printf 'h\n' >"$T5/docs/plans/reports/2026-11-01-r.md"
printf 'i\n' >"$T5/README.md"
printf 'j\n' >"$T5/scripts/other.sh"
printf 'k\n' >"$T5/docs/howto/x.md"
printf 'l\n' >"$T5/scripts/leak-patterns.local.template"
printf 'm\n' >"$T5/scripts/scrub-peer-names.sh"
printf 'n\n' >"$T5/tests/test-scrub-x.sh"
printf 'o\n' >"$T5/tests/test-seed-y.sh"
printf '%s\n%s\n# c\n\n%s\n' "$N1" "$N2U" "$N3" >"$T5/.leak-patterns.local"
new_tree "$T5"
P5F="$T5/.leak-patterns.local"

exp() {
    printf '%s\n' "$@" | LC_ALL=C sort
}

D_OK=1
list_area "$T5" "$P5F" go
[ "$AREA_LIST" = "$(exp \
    internal/channel/embed/embed.go internal/mcp/mcp.go internal/pkg/a.go \
    internal/pkg/testdata/case.txt internal/server/web/web.go)" ] || D_OK=0
list_area "$T5" "$P5F" plans
[ "$AREA_LIST" = "$(exp \
    docs/plans/2026-11-01-p.md docs/plans/README.md)" ] || D_OK=0
list_area "$T5" "$P5F" historical
[ "$AREA_LIST" = "$(exp \
    docs/plans/2026-09-30-q.md docs/plans/notes.md \
    docs/plans/reports/2026-11-01-r.md docs/plans/reports/report.md)" ] || D_OK=0
list_area "$T5" "$P5F" docs
[ "$AREA_LIST" = "$(exp \
    README.md docs/howto/x.md \
    internal/channel/embed/channel.js internal/mcp/README.md \
    internal/server/web/app.js internal/server/web/index.html \
    scripts/other.sh)" ] || D_OK=0
if [ "$D_OK" = "1" ]; then
    ok "areas split go/plans/historical/docs by tracked path"
else
    fail "areas split go/plans/historical/docs by tracked path" \
        "a --list output differs from its expected set (non-testdata assets beside Go code belong to docs)"
fi

# -------------------------------------------------------------- case 6
E_OK=1
list_area "$T5" "$P5F" all
ALL_OUT="$AREA_LIST"
[ "$(printf '%s\n' "$ALL_OUT" | grep -c .)" = "18" ] || E_OK=0
for f in scripts/leak-patterns.local.template scripts/scrub-peer-names.sh \
         tests/test-scrub-x.sh tests/test-seed-y.sh .leak-patterns.local; do
    case "$ALL_OUT" in
        *"$f"*) E_OK=0 ;;
    esac
done
for f in internal/pkg/a.go internal/pkg/testdata/case.txt \
         internal/mcp/mcp.go internal/server/web/web.go \
         internal/channel/embed/embed.go \
         docs/plans/2026-11-01-p.md docs/plans/README.md \
         docs/plans/2026-09-30-q.md docs/plans/notes.md \
         docs/plans/reports/report.md docs/plans/reports/2026-11-01-r.md \
         README.md docs/howto/x.md scripts/other.sh \
         internal/mcp/README.md internal/server/web/app.js \
         internal/server/web/index.html internal/channel/embed/channel.js; do
    case "$ALL_OUT" in
        *"$f"*) ;;
        *) E_OK=0 ;;
    esac
done
if [ "$E_OK" = "1" ]; then
    ok "--list all is the union: one path per line, exclusions held"
else
    fail "--list all is the union: one path per line, exclusions held" \
        "union membership is wrong (11 files expected; excluded paths must not appear)"
fi

# -------------------------------------------------------------- case 7
T7="$W/t7"       # no git: the find fallback must list tracked-style files
mkdir -p "$T7/internal/pkg/testdata"
printf 'z\n' >"$T7/internal/pkg/a.go"
printf 'w\n' >"$T7/internal/pkg/testdata/case.txt"
printf 'v\n' >"$T7/README.md"
run_scrub "$T7" "$P1F" go --list
if [ "$RUN_RC" = "0" ] && [ "$(LC_ALL=C sort <<<"$RUN_OUT")" = "$(exp \
    internal/pkg/a.go internal/pkg/testdata/case.txt)" ]; then
    ok "non-git root falls back to find and still lists the area"
else
    fail "non-git root falls back to find and still lists the area" \
        "rc=$RUN_RC"
fi

# -------------------------------------------------------------- case 8
# Placeholder continuation: pattern indexes 4..27 map onto peer-c ..
# peer-z, index 28 onto peer-c1 — no broken characters, stable wrap.
T8="$W/t8"
mkdir -p "$T8"
NM8=()
for k in 00 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 \
         20 21 22 23 24 25 26 27 28; do
    NM8+=("$(printf '%s%s%s' "$FP1" "$FP2" "node-$k")")
done
{
    for nm in "${NM8[@]}"; do
        printf 'line for %s\n' "$nm"
    done
} >"$T8/README.md"
P8F="$W/patterns8"
{
    for nm in "${NM8[@]}"; do
        printf '%s\n' "$nm"
    done
} >"$P8F"
new_tree "$T8"
run_scrub "$T8" "$P8F" docs --apply
H_OK=1
[ "$RUN_RC" = "0" ] || H_OK=0
[ "$RUN_OUT" = "$(printf '%s\n' 'README.md: 29' 'remaining: 0')" ] || H_OK=0
# Sorted order: index i below, placeholder above.
cnt8() { grep -cxF -- "$1" "$T8/README.md" 2>/dev/null; }
[ "$(cnt8 'line for remote-node')" = "1" ] || H_OK=0   # index 0
[ "$(cnt8 'line for peer-a')" = "1" ] || H_OK=0        # index 1
[ "$(cnt8 'line for peer-b')" = "1" ] || H_OK=0        # index 2
[ "$(cnt8 'line for gpu-node')" = "1" ] || H_OK=0      # index 3
[ "$(cnt8 'line for peer-c')" = "1" ] || H_OK=0        # index 4
[ "$(cnt8 'line for peer-w')" = "1" ] || H_OK=0        # index 24
[ "$(cnt8 'line for peer-z')" = "1" ] || H_OK=0        # index 27
[ "$(cnt8 'line for peer-c1')" = "1" ] || H_OK=0       # index 28
[ "$(grep -c . "$T8/README.md")" = "29" ] || H_OK=0
for nm in "${NM8[@]}"; do
    names_grep "$T8/README.md" "$nm" && H_OK=0
done
if [ "$H_OK" = "1" ]; then
    ok "placeholder table continues past the first five entries without broken characters"
else
    fail "placeholder table continues past the first five entries without broken characters" \
        "index-to-placeholder mapping or re-run output differs"
fi

# -------------------------------------------------------------- case 10
# Ambiguity guard (go area): a pattern matching more than 500 lines of
# the selected files is treated as a generic word — reported as a
# count-only line, dropped before placeholder assignment, and never
# applied; the surviving pattern still maps to the first placeholder.
T10="$W/t10"
mkdir -p "$T10/internal/pkg"
NW="$(printf '%s%s%s' "$FP1" "$FP2" "wide-0")"
{
    printf 'uses %s\n' "$N1"
    k=1
    while [ "$k" -le 501 ]; do
        printf 'x %s x\n' "$NW"
        k=$((k + 1))
    done
} >"$T10/internal/pkg/a.go"
P10F="$W/patterns10"
printf '%s\n%s\n' "$N1" "$NW" >"$P10F"   # sorted: N1 (index 0) -> remote-node
new_tree "$T10"

run_scrub "$T10" "$P10F" go
J_OK=1
J_EXP1="$(printf '%s\n' \
    'ambiguous: skipped 1' \
    'internal/pkg/a.go: 1' \
    'remaining: 1')"
[ "$RUN_RC" = "0" ] && [ "$RUN_OUT" = "$J_EXP1" ] || J_OK=0

run_scrub "$T10" "$P10F" go --apply
J_EXP2="$(printf '%s\n' \
    'ambiguous: skipped 1' \
    'internal/pkg/a.go: 1' \
    'remaining: 0')"
[ "$RUN_RC" = "0" ] && [ "$RUN_OUT" = "$J_EXP2" ] || J_OK=0
[ "$(grep -c 'remote-node' "$T10/internal/pkg/a.go")" = "1" ] || J_OK=0
[ "$(grep -ciF -- "$NW" "$T10/internal/pkg/a.go")" = "501" ] || J_OK=0   # still untouched

run_scrub "$T10" "$P10F" go
J_EXP3="$(printf '%s\n' \
    'ambiguous: skipped 1' \
    'remaining: 0')"
[ "$RUN_RC" = "0" ] && [ "$RUN_OUT" = "$J_EXP3" ] || J_OK=0
if [ "$J_OK" = "1" ]; then
    ok "go-area pattern over 500 matching lines is 'ambiguous: skipped', never applied"
else
    fail "go-area pattern over 500 matching lines is 'ambiguous: skipped', never applied" \
        "one of the dry-run, apply or re-run assertions failed"
fi

# -------------------------------------------------------------- case 9
# Every scrub invocation above ran in the main shell, so the capture
# counter and the capture files are consistent; this case scans them
# all for fixture names.
F_OK=1
[ "$NCAP" = "15" ] || F_OK=0   # one invocation per listed call site
[ "$(find "$CAPS" -type f -name 'c*.out' | grep -c .)" = "15" ] || F_OK=0
while IFS= read -r cap; do
    [ -s "$cap" ] || continue
    for n in "$N1" "$N1U" "$N2" "$N2U" "$N3"; do
        if names_grep "$cap" "$n"; then
            F_OK=0
        fi
    done
done < <(find "$CAPS" -type f | LC_ALL=C sort)
if [ "$F_OK" = "1" ]; then
    ok "no fixture name appears in any captured output"
else
    fail "no fixture name appears in any captured output" \
        "a captured output contains one of the fixtures"
fi

echo "----------------------------------------"
echo "cases passed: $PASS, failed: $FAILN"
[ "$FAILN" -eq 0 ]
exit $?
