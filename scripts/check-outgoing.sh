#!/usr/bin/env bash
# scripts/check-outgoing.sh — this repository's check of what is about to leave
# the machine (the `make check-outgoing` target).
#
# datawatch runs the target from the base commit (the head of the remote's
# default branch), so a branch cannot change the check that judges it. The
# input is the manifest datawatch writes (internal/git/outgoing.go):
#
#   OUTGOING_MANIFEST_DIR  added lines, commit and tag objects, new file names,
#                          destination refs and identities of exactly what the
#                          push would send
#   OUTGOING_PROJECT_DIR   the project's main working tree, which holds the
#                          private pattern list
#
# By hand: `make outgoing-check [REF=HEAD] [DEST=refs/heads/<branch>]`.
#
# It applies the four leak classes of scripts/check-no-local-leaks.sh to all of
# that, requires the private pattern list (.leak-patterns.local with at least
# one pattern; a missing list refuses), and refuses any author, committer or
# tagger address that is not on the allowed list.
#
# The output never contains matched text: a finding is a place and a class. A
# file or ref name that itself matches is shown by number, never by name.
#
# Exit codes: 0 clean, 1 findings, 2 the check could not run.

set -euo pipefail
# Bytes, not characters: a byte that is not valid in the locale's encoding must
# never make grep treat the input as binary and report nothing.
export LC_ALL=C
unset LEAK_CHECK_CLASSES LEAK_CHECK_PATTERNS LEAK_CHECK_ALLOWED_EMAILS \
    LEAK_CHECK_ALLOWED_ADDRESSES LEAK_CHECK_EXCLUDE

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCANNER="${SCRIPT_DIR}/check-no-local-leaks.sh"

die2() {
    echo "check-outgoing: $*" >&2
    exit 2
}

M="${OUTGOING_MANIFEST_DIR:-}"
[[ -n "$M" ]] || die2 "OUTGOING_MANIFEST_DIR is not set; to check by hand run: make outgoing-check"
for f in refs.tsv identities.tsv files.tsv added.txt added.idx; do
    [[ -f "$M/$f" ]] || die2 "the manifest lacks $f"
done
[[ -d "$M/commits" && -d "$M/tags" ]] || die2 "the manifest lacks the commit or tag objects"

LOCAL_LIST="${LEAK_CHECK_LOCAL:-}"
if [[ -z "$LOCAL_LIST" ]]; then
    [[ -n "${OUTGOING_PROJECT_DIR:-}" ]] || die2 "OUTGOING_PROJECT_DIR is not set"
    LOCAL_LIST="${OUTGOING_PROJECT_DIR}/.leak-patterns.local"
fi
[[ "$LOCAL_LIST" == /* ]] || die2 "LEAK_CHECK_LOCAL must be an absolute path"

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
chmod 700 "$W"
# Scan roots. "all" gets every class, "strict" (binary content and the paths
# scripts/leak-check.exclude skips) the private list only, and "paths" the
# changed file names, scanned only to decide which names may be printed.
mkdir -p "$W/all" "$W/strict" "$W/paths" "$W/map"
: >"$W/all/added.txt"
: >"$W/all/meta.txt"
: >"$W/all/names.txt"
: >"$W/strict/added.txt"
: >"$W/paths/changed.txt"
: >"$W/map/all-added"
: >"$W/map/strict-added"
: >"$W/map/meta"
: >"$W/map/names"

# ------------------------------------------------------------ added lines
EXCLUDE_GLOBS=()
while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    EXCLUDE_GLOBS+=("$line")
done <"${SCRIPT_DIR}/leak-check.exclude"

excluded() {
    local g
    for g in "${EXCLUDE_GLOBS[@]:-}"; do
        # shellcheck disable=SC2053  # intentional glob match
        [[ -n "$g" && "$1" == $g ]] && return 0
    done
    return 1
}

exec 3<"$M/added.txt" 4<"$M/added.idx" 5>>"$W/all/added.txt" 6>>"$W/map/all-added" \
    7>>"$W/strict/added.txt" 8>>"$W/map/strict-added"
declare -A PATH_NO=() PATH_SKIPPED=()
PATHS=()
ADDED=0
while IFS=$'\t' read -r -u4 commit path lineno kind; do
    IFS= read -r -u3 text || die2 "the manifest's added lines and their index differ in length"
    ADDED=$((ADDED + 1))
    if [[ -z "${PATH_NO[$path]:-}" ]]; then
        PATHS+=("$path")
        PATH_NO[$path]=${#PATHS[@]}
        printf '%s\n' "$path" >>"$W/paths/changed.txt"
        if excluded "$path"; then PATH_SKIPPED[$path]=1; else PATH_SKIPPED[$path]=0; fi
    fi
    if [[ "$kind" == text && "${PATH_SKIPPED[$path]}" == 0 ]]; then
        printf '%s\n' "$text" >&5
        printf '%s\t%s\t%s\n' "${commit:0:12}" "$lineno" "$path" >&6
    else
        printf '%s\n' "$text" >&7
        printf '%s\t%s\t%s\n' "${commit:0:12}" "$lineno" "$path" >&8
    fi
done
exec 3<&- 4<&- 5>&- 6>&- 7>&- 8>&-

# ------------------------------------------------- commit and tag objects
# Object ids and signature blocks are not text anybody wrote.
add_object() { # kind file
    local kind="$1" f="$2" id n=0 header=1 sig=0 line
    id="$(basename "$f")"
    while IFS= read -r line || [[ -n "$line" ]]; do
        n=$((n + 1))
        if ((sig)); then
            [[ "$line" == *"-----END "*"SIGNATURE-----"* ]] && sig=0
            continue
        fi
        if [[ "$line" == *"-----BEGIN "*"SIGNATURE-----"* ]]; then
            sig=1
            continue
        fi
        if ((header)); then
            [[ -z "$line" ]] && header=0
            case "$line" in "tree "* | "parent "* | "object "* | "type "*) continue ;; esac
        fi
        printf '%s\n' "$line" >>"$W/all/meta.txt"
        printf '%s %s line %s\n' "$kind" "${id:0:12}" "$n" >>"$W/map/meta"
    done <"$f"
}
COMMITS=0
for f in "$M"/commits/*; do
    [[ -f "$f" ]] || continue
    COMMITS=$((COMMITS + 1))
    add_object commit "$f"
done
for f in "$M"/tags/*; do
    [[ -f "$f" ]] || continue
    add_object tag "$f"
done

# -------------------------------------------------- new file and ref names
n=0
while IFS=$'\t' read -r commit path; do
    n=$((n + 1))
    printf '%s\n' "$path" >>"$W/all/names.txt"
    printf 'new file name #%s (commit %s)\n' "$n" "${commit:0:12}" >>"$W/map/names"
done <"$M/files.tsv"
r=0
while IFS=$'\t' read -r ref _ _; do
    r=$((r + 1))
    printf '%s\n' "$ref" >>"$W/all/names.txt"
    printf 'destination ref name #%s\n' "$r" >>"$W/map/names"
done <"$M/refs.tsv"

# ------------------------------------------------------------------ scans
# The scanner skips a whole file as binary when it holds a NUL or a 0x01 byte,
# so none may reach it: one such byte anywhere would hide every other line.
for f in "$W"/all/*.txt "$W"/strict/*.txt "$W"/paths/*.txt; do
    tr -d '\000\001' <"$f" >"$W/clean"
    cat "$W/clean" >"$f"
done

FINDINGS=()
PATTERN_COUNT=""
scan() { # root classes -> sets HITS to "file line class" rows
    local root="$1" classes="$2" rc=0 line
    HITS=()
    LEAK_CHECK_REQUIRE_LOCAL=1 LEAK_CHECK_CLASSES="$classes" LEAK_CHECK_LOCAL="$LOCAL_LIST" \
        bash "$SCANNER" "$root" >"$W/out" 2>"$W/err" || rc=$?
    if ((rc > 1)); then
        # The scanner's configuration messages hold no pattern and no match.
        cat "$W/err" >&2
        die2 "the leak scan could not run"
    fi
    while IFS= read -r line; do
        if [[ "$line" =~ ^([a-z]+\.txt):([0-9]+):\ (address|home|email|local)$ ]]; then
            HITS+=("${BASH_REMATCH[1]} ${BASH_REMATCH[2]} ${BASH_REMATCH[3]}")
        fi
    done <"$W/out"
    if ((rc == 1 && ${#HITS[@]} == 0)); then
        die2 "the leak scan reported findings this check cannot read"
    fi
    line="$(grep -E '^local patterns: [0-9]+ loaded$' "$W/err" | head -1 || true)"
    [[ -n "$line" ]] && PATTERN_COUNT="${line//[^0-9]/}"
    return 0
}

# Which changed paths may be printed: none that matches any class.
declare -A HIDE=()
scan "$W/paths" "address,home,email,local"
for h in "${HITS[@]:-}"; do
    [[ -n "$h" ]] || continue
    read -r _ n _ <<<"$h"
    HIDE["${PATHS[$((n - 1))]}"]=1
done

# The line descriptions, read once.
mapfile -t ALL_ADDED <"$W/map/all-added"
mapfile -t STRICT_ADDED <"$W/map/strict-added"
mapfile -t META <"$W/map/meta"
mapfile -t NAMES <"$W/map/names"

report_added() { # description class
    local commit lineno path shown
    IFS=$'\t' read -r commit lineno path <<<"$1"
    if [[ -n "${HIDE[$path]:-}" ]]; then
        shown="changed file #${PATH_NO[$path]}"
    else
        shown="$path"
    fi
    FINDINGS+=("commit $commit $shown:$lineno: $2")
}

scan "$W/all" "address,home,email,local"
for h in "${HITS[@]:-}"; do
    [[ -n "$h" ]] || continue
    read -r file n cls <<<"$h"
    case "$file" in
        added.txt) report_added "${ALL_ADDED[$((n - 1))]}" "$cls" ;;
        meta.txt) FINDINGS+=("${META[$((n - 1))]}: $cls") ;;
        names.txt) FINDINGS+=("${NAMES[$((n - 1))]}: $cls") ;;
    esac
done
scan "$W/strict" "local"
for h in "${HITS[@]:-}"; do
    [[ -n "$h" ]] || continue
    read -r _ n cls <<<"$h"
    report_added "${STRICT_ADDED[$((n - 1))]}" "$cls"
done

# ------------------------------------------------------------- identities
# Every address must be on the allowed list, whatever shape it has: a default
# identity built from the machine's name does not look like an email address.
ALLOWED=()
while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    ALLOWED+=("$line")
done <"${SCRIPT_DIR}/leak-check.allowed-emails"

allowed_address() {
    local g
    [[ -n "$1" ]] || return 1
    shopt -s nocasematch
    for g in "${ALLOWED[@]}"; do
        # shellcheck disable=SC2053  # intentional glob match
        if [[ "$1" == $g ]]; then
            shopt -u nocasematch
            return 0
        fi
    done
    shopt -u nocasematch
    return 1
}
while IFS= read -r line; do
    id="${line%%$'\t'*}"
    rest="${line#*$'\t'}"
    role="${rest%%$'\t'*}"
    address="${line##*$'\t'}"
    allowed_address "$address" || FINDINGS+=("object ${id:0:12} $role: address not on the allowed list")
done <"$M/identities.tsv"

# ------------------------------------------------------------------ result
if ((${#FINDINGS[@]} > 0)); then
    # awk reads everything: head would end the pipe early and pipefail would
    # then end the script before its summary.
    printf '%s\n' "${FINDINGS[@]}" | sort -u | awk 'NR <= 20'
    total="$(printf '%s\n' "${FINDINGS[@]}" | sort -u | wc -l)"
    echo "check-outgoing: ${total// /} finding(s); ${PATTERN_COUNT:-0} private pattern(s) loaded"
    exit 1
fi
echo "check-outgoing: clean ($COMMITS commit(s), $ADDED added line(s), ${PATTERN_COUNT:-0} private pattern(s) loaded)"
