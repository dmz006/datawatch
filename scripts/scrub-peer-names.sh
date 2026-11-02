#!/usr/bin/env bash
#
# scrub-peer-names.sh
#
# Replace machine-specific peer/node names in tracked files with fixed,
# generic placeholders. The patterns come from the operator's
# .leak-patterns.local (or --patterns FILE), read at run time: one
# fixed-string pattern per line, '#' lines and blank lines ignored.
#
# Security rules this script follows:
#   * A pattern (or any matched line) is NEVER printed: dry run emits only
#     `path: N` lines and one final `remaining: N` line.
#   * Patterns are never placed on a command line: replacement runs through
#     perl or sed (whichever `command -v` resolves) with the pattern and
#     replacement passed via the environment / a sed expression file; the
#     dry-run counts use `grep -f` with a pattern file.
#   * Replacement is a case-insensitive substring swap. Each distinct
#     pattern (deduplicated case-insensitively) gets the same placeholder
#     everywhere: the case-insensitively sorted, deduplicated pattern list
#     is mapped onto remote-node, peer-a, peer-b, gpu-node, peer-c,
#     peer-d, peer-e, ... Longer patterns are replaced first so a name
#     containing another name is handled cleanly.
#   * Binary files are skipped. The working tree is only changed with
#     --apply; without it this script is a pure dry run.
#
# Areas (by tracked path, relative to the root):
#   go         *.go files plus files in the same or a parent directory as
#              Go code (e.g. testdata next to the package)
#   plans      docs/plans (not docs/plans/reports) whose basename starts
#              with a date on or after 2026-10-01, plus docs/plans/README.md
#   historical other files under docs/plans (including docs/plans/reports,
#              whatever their basename date)
#   docs       every other tracked non-Go file: how-tos, CHANGELOG.md,
#              openapi and config reference, README, AGENT.md, other
#              scripts — except docs/plans/**, the patterns file,
#              scripts/leak-patterns.local.template, tests/test-*scrub*
#              and tests/test-seed*, and scripts/scrub-peer-names.sh
#   all        the union
#
# Usage:
#   scripts/scrub-peer-names.sh [--area go|docs|plans|historical|all]
#                               [--root DIR] [--patterns FILE] [--list] [--apply]
#
# Output (dry run / apply): `path: N` per file that has matches
# (apply reports the number of matching lines replaced in that file),
# then a final `remaining: N` line (recounted after --apply).
# With --list: the tracked files in the area, one per line.

set -u

usage() {
    # Keep this free of anything that could look like a real name.
    cat <<'EOF'
usage: scrub-peer-names.sh [--area go|docs|plans|historical|all]
                           [--root DIR] [--patterns FILE] [--list] [--apply]
  --area X      file area to scrub (default: all)
  --root DIR    repository root (default: the root this script lives in)
  --patterns F  patterns file (default: <root>/.leak-patterns.local)
  --list        print the tracked files in the area, one per line
  --apply       replace in place (default: dry run, no writes)
EOF
    exit 2
}

die() {
    echo "error: $*" >&2
    exit 2
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" || exit 2
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)" || exit 2
AREA="all"
APPLY=0
LIST=0
PATTERNS=""

while [ $# -gt 0 ]; do
    case "$1" in
        --area)
            [ $# -ge 2 ] || die "missing value for --area"
            shift
            AREA="$1"
            ;;
        --root)
            [ $# -ge 2 ] || die "missing value for --root"
            shift
            ROOT="$1"
            ;;
        --patterns)
            [ $# -ge 2 ] || die "missing value for --patterns"
            shift
            PATTERNS="$1"
            ;;
        --apply) APPLY=1 ;;
        --list)  LIST=1 ;;
        -h|--help) usage ;;
        *)
            echo "error: unknown option: $1" >&2
            usage
            ;;
    esac
    shift
done

case "$AREA" in
    go|docs|plans|historical|all) ;;
    *) die "invalid --area '$AREA' (want go|docs|plans|historical|all)" ;;
esac
[ -d "$ROOT" ] || die "root not found: $ROOT"
ROOT="$(cd "$ROOT" && pwd)"

PATTERNS="${PATTERNS:-$ROOT/.leak-patterns.local}"
[ -f "$PATTERNS" ] || die "patterns file not found (expected $ROOT/.leak-patterns.local or --patterns FILE)"

W="$(mktemp -d)" || die "mktemp failed"
trap 'rm -rf "$W"' EXIT

# ---------------------------------------------------------- pattern list
# One distinct pattern per line, case-insensitively deduplicated, in
# case-insensitive alphabetical order (that order fixes placeholder
# assignment). No pattern text is ever echoed.
PATLIST="$W/patterns"
LC_ALL=C grep -vE '^[[:space:]]*(#|$)' -- "$PATTERNS" \
    | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
    | LC_ALL=C sed -n '/[^[:space:]]/p' \
    | LC_ALL=C sort -f -u > "$PATLIST"
[ -s "$PATLIST" ] || die "patterns file holds no usable patterns"

mapfile -t PATS < "$PATLIST"

# placeholder_for INDEX -> the fixed placeholder at that index of the
# case-insensitively sorted, deduplicated pattern list.
placeholder_for() {
    local i="$1"
    case "$i" in
        0) printf 'remote-node' ;;
        1) printf 'peer-a' ;;
        2) printf 'peer-b' ;;
        3) printf 'gpu-node' ;;
        *)
            local idx=$((i - 4)) base=0 L
            while [ "$idx" -ge 26 ]; do
                idx=$((idx - 26))
                base=$((base + 1))
            done
            L="$(printf "\\x$(printf '%x' $((99 + idx)))")"
            if [ "$base" -gt 0 ]; then
                printf 'peer-%s%s' "$L" "$base"
            else
                printf 'peer-%s' "$L"
            fi
            ;;
    esac
}

# Replacement order: longer patterns first (so a name containing another
# is replaced before the shorter one), ties in stable index order.
ORDER="$(
    i=0
    while IFS= read -r p; do
        printf '%d\t%d\n' "${#p}" "$i"
        i=$((i + 1))
    done < "$PATLIST" \
        | LC_ALL=C sort -k1,1nr -k2,2n | cut -f2
)"
# shellcheck disable=SC2034
i=0

# ---------------------------------------------------------- file listing
is_text() {
    # Binary heuristic: any byte among NUL/low control bytes (beyond
    # tab, LF, CR) in the first 8 KiB marks the file binary. Printable
    # UTF-8 content stays eligible. The file is only read.
    local f="$1"
    [ -s "$f" ] || return 0  # empty files count as text; nothing to do
    ! head -c 8192 -- "$f" \
        | LC_ALL=C tr -d ' \11\12\15\40-\176\200-\377' \
        | LC_ALL=C grep -q .
}

declare -A GO_DIR=()
FILES=()
get_files() {
    local f d
    if command -v git >/dev/null 2>&1 \
        && (cd "$ROOT" && git rev-parse --is-inside-work-tree >/dev/null 2>&1); then
        while IFS= read -r f; do
            FILES+=("$f")
        done < <(cd "$ROOT" && git ls-files --)
    else
        while IFS= read -r f; do
            FILES+=("$f")
        done < <(cd "$ROOT" && find . -type f -not -path './.git/*' | sed 's|^\./||' | LC_ALL=C sort)
    fi
    for f in "${FILES[@]}"; do
        case "$f" in
            *.go) d="${f%/*}"; [ "$d" != "$f" ] && GO_DIR["$d"]=1 ;;
        esac
    done
}
get_files

in_go_dir() {
    # The file's directory, or any parent directory above it, holds Go
    # code (e.g. a testdata tree next to its package).
    local d="${1%/*}"
    [ "$d" != "$1" ] || return 1
    while :; do
        [ -n "${GO_DIR[$d]:-}" ] && return 0
        case "$d" in
            */*) d="${d%/*}" ;;
            *) return 1 ;;
        esac
    done
}

in_plans() {
    local f="$1" b d
    case "$f" in
        docs/plans/README.md) return 0 ;;
        docs/plans/reports/*) return 1 ;;
        docs/plans/*) ;;
        *) return 1 ;;
    esac
    b="${f##*/}"
    case "$b" in
        [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]*)
            d="${b:0:10}"
            [ ! "$d" \< "2026-10-01" ]
            ;;
        *) return 1 ;;
    esac
}

in_historical() {
    local f="$1"
    case "$f" in
        docs/plans/*)
            if in_plans "$f"; then
                return 1
            fi
            return 0
            ;;
        *) return 1 ;;
    esac
}

# Relative path of the patterns file (to exclude it from the docs area).
PATREL=""
case "$PATTERNS" in
    "$ROOT"/*) PATREL="${PATTERNS#"$ROOT"/}" ;;
esac

in_docs() {
    local f="$1"
    case "$f" in
        *.go) return 1 ;;
    esac
    in_go_dir "$f" && return 1
    case "$f" in
        docs/plans/*) return 1 ;;
        scripts/leak-patterns.local.template) return 1 ;;
        scripts/scrub-peer-names.sh) return 1 ;;
        tests/test-*scrub*) return 1 ;;
        tests/test-seed*) return 1 ;;
    esac
    if [ -n "$PATREL" ] && [ "$f" = "$PATREL" ]; then
        return 1
    fi
    return 0
}

in_area() {
    local f="$1"
    case "$AREA" in
        go)
            case "$f" in *.go) return 0 ;; esac
            in_go_dir "$f"
            ;;
        plans) in_plans "$f" ;;
        historical) in_historical "$f" ;;
        docs) in_docs "$f" ;;
        all)
            case "$f" in *.go) return 0 ;; esac
            in_go_dir "$f" && return 0
            in_plans "$f" && return 0
            case "$f" in docs/plans/*) return 0 ;; esac
            in_docs "$f"
            ;;
    esac
}

# ------------------------------------------------------------ selection
SEL="$W/selected"
: > "$SEL"
for f in "${FILES[@]}"; do
    in_area "$f" || continue
    is_text "$ROOT/$f" || continue
    printf '%s\n' "$f" >> "$SEL"
done

# ------------------------------------------------------------- --list
if [ "$LIST" -eq 1 ]; then
    cat "$SEL"
    exit 0
fi

# ------------------------------------------------------------- counting
# Count matching lines with `grep -f` so no pattern ever reaches argv or
# the terminal. Prints the line count (0 when none).
count_hits() {
    # $1 = file path already rooted into $ROOT by the caller
    local n
    n="$(grep -icFf "$PATLIST" -- "$1" 2>/dev/null)" || true
    # grep exits 1 on zero matches; normalise to a plain number
    case "${n:-0}" in
        ''|*[!0-9]*) n=0 ;;
    esac
    printf '%s' "$n"
}

# ------------------------------------------------------------ replacing
REPLACER=""
if command -v perl >/dev/null 2>&1; then
    REPLACER="perl"
elif command -v sed >/dev/null 2>&1; then
    REPLACER="sed"
fi

# replace_once FILE PATTERN INDEX — one case-insensitive pass of one
# pattern over one file (--apply only). Pattern and replacement travel
# through the environment (perl) or an expression file (sed), never the
# command line.
replace_once() {
    local f="$1" pat="$2" rep idx="$3"
    rep="$(placeholder_for "$idx")"
    if [ "$REPLACER" = perl ]; then
        SCRB_PAT="$pat" SCRB_REPL="$rep" \
            perl -i -pe 's/\Q$ENV{SCRB_PAT}\E/$ENV{SCRB_REPL}/gi' -- "$f"
    else
        local esc expr="$W/sed-expr"
        esc="$(printf '%s\n' "$pat" \
            | sed -e 's/[]\/&$.*+?{}()^|#\\]/\\&/g')"
        printf 's/%s/%s/gi\n' "$esc" "$rep" > "$expr"
        sed -E -i -f "$expr" -- "$f"
    fi
}

# -------------------------------------------------------------- main
TOTAL=0
CHANGED=0
while IFS= read -r f; do
    [ -n "$f" ] || continue
    n="$(count_hits "$ROOT/$f")"
    TOTAL=$((TOTAL + n))
    if [ "$n" -gt 0 ]; then
        if [ "$APPLY" -eq 1 ]; then
            [ -n "$REPLACER" ] || die "need perl or sed to replace"
            for idx in $ORDER; do
                replace_once "$ROOT/$f" "${PATS[$idx]}" "$idx"
            done
            CHANGED=$((CHANGED + n))
        fi
        printf '%s: %s\n' "$f" "$n"
    fi
done < "$SEL"

if [ "$APPLY" -eq 1 ]; then
    # Recount after the passes so `remaining` reflects reality (e.g. a
    # pattern that also matches our own placeholder text).
    REM=0
    while IFS= read -r f; do
        [ -n "$f" ] || continue
        n="$(count_hits "$ROOT/$f")"
        REM=$((REM + n))
    done < "$SEL"
    printf 'remaining: %s\n' "$REM"
else
    printf 'remaining: %s\n' "$TOTAL"
fi
