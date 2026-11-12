#!/usr/bin/env bash
# scripts/check-no-local-leaks.sh — whole-tree local-environment leak check.
#
# Enforces the AGENT.md "No local-environment leaks in git" rule across the
# whole tracked tree at once, instead of only the most recent commit diff
# (that narrower check stays in its own script; the two are complementary).
#
# A "local-environment leak" is a value tied to one operator's machine: a
# private or internal IP address, an absolute home-directory path, a
# personal email address, or a machine-specific token the operator lists in
# the local patterns file.
#
# Usage:
#   scripts/check-no-local-leaks.sh [root]
#
#   root  directory to scan (default: the repository root, resolved from
#         this script's own location)
#
# File list: if root is a git work tree the tracked files (git ls-files)
# are scanned; otherwise the directory is walked with find, skipping the
# .git directory. Binary files and paths matching a glob in
# scripts/leak-check.exclude are skipped.
#
# Environment overrides (each resolved relative to this script's location,
# never relative to the scanned root):
#   LEAK_CHECK_CLASSES        comma-separated subset of address,home,email,local
#                             (empty or unset means all classes)
#   LEAK_CHECK_PATTERNS       class patterns file        (default: scripts/leak-check.patterns)
#   LEAK_CHECK_ALLOWED_EMAILS allowed-email glob list    (default: scripts/leak-check.allowed-emails)
#   LEAK_CHECK_EXCLUDE        exclude glob list          (default: scripts/leak-check.exclude)
#   LEAK_CHECK_LOCAL          local fixed-string lists   (default: <root>/.leak-patterns.local)
#
# Classes:
#   address  private / internal / link-local IPv4, ULA + tailnet IPv6 and
#            MagicDNS hostnames; reserved documentation ranges and
#            loopback pass
#   home     absolute home-directory paths for any user; placeholders pass
#   email    every address not covered by the allowed-email glob list
#   local    fixed strings from the operator's .leak-patterns.local
#
# Output: one line per finding,  path:line: class  (path relative to root),
# then a per-class count summary — and nothing else. The output NEVER
# contains the matched text or any other part of the matched line;
# candidate matches are extracted in memory purely to test them and are
# discarded. When the local patterns file is absent or holds only comments
# and blank lines, a single "local patterns: skipped" notice is printed
# (on stderr) and the scan carries on — that is never an error.
#
# Exit codes: 0 clean, 1 findings reported, 2 usage or configuration error.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CLASS_SET="address home email local"
EMAIL_EXTRACT_RE="[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"

die2() {
    echo "check-no-local-leaks: $*" >&2
    exit 2
}

# --------------------------------------------------------------- arguments
usage_error() {
    echo "usage: $(basename "$0") [root]" >&2
    exit 2
}

ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
if (( $# == 1 )); then
    case "$1" in
        -h | --help) usage_error ;;
    esac
    ROOT="$1"
elif (( $# > 1 )); then
    usage_error
fi

if ! [[ -d "$ROOT" ]]; then
    die2 "root is not a directory: $ROOT"
fi
ROOT="$(cd "$ROOT" && pwd)"

# ------------------------------------------ env overrides (script-dir
# relative, never scanned-root relative)
resolve_from_script() {
    case "$1" in
        /*) printf '%s' "$1" ;;
        *)  printf '%s' "${SCRIPT_DIR}/${1}" ;;
    esac
}

if [[ -n "${LEAK_CHECK_LOCAL:-}" ]]; then
    LOCAL_FILE="$(resolve_from_script "$LEAK_CHECK_LOCAL")"
else
    LOCAL_FILE="${ROOT}/.leak-patterns.local"
fi
PATTERNS_FILE="$(resolve_from_script "${LEAK_CHECK_PATTERNS:-leak-check.patterns}")"
ALLOWED_FILE="$(resolve_from_script "${LEAK_CHECK_ALLOWED_EMAILS:-leak-check.allowed-emails}")"
EXCLUDE_FILE="$(resolve_from_script "${LEAK_CHECK_EXCLUDE:-leak-check.exclude}")"
ADDR_ALLOWED_FILE="$(resolve_from_script "${LEAK_CHECK_ALLOWED_ADDRESSES:-leak-check.allowed-addresses}")"

[[ -f "$PATTERNS_FILE" ]] || die2 "patterns file not found: $PATTERNS_FILE"
[[ -f "$ALLOWED_FILE" ]] || die2 "allowed-emails file not found: $ALLOWED_FILE"
[[ -f "$EXCLUDE_FILE" ]] || die2 "exclude file not found: $EXCLUDE_FILE"

# -------------------------------------------------------- class selection
trim() {
    local s="$1"
    s="${s#"${s%%[![:space:]]*}"}"
    s="${s%"${s##*[![:space:]]}"}"
    printf '%s' "$s"
}

SEL="$CLASS_SET"
if [[ -n "${LEAK_CHECK_CLASSES:-}" ]]; then
    SEL=""
    IFS=',' read -r -a _parts <<<"$LEAK_CHECK_CLASSES"
    picked=()
    for p in "${_parts[@]}"; do
        p="$(trim "$p")"
        [[ -z "$p" ]] && continue
        case " $CLASS_SET " in
            *" $p "*): ;;
            *) die2 "unknown class in LEAK_CHECK_CLASSES: $p (valid: ${CLASS_SET// /,})" ;;
        esac
        case " ${picked[*]:-} " in *" $p "*) continue ;; esac
        picked+=("$p")
    done
    for c in $CLASS_SET; do
        case " ${picked[*]:-} " in *" $c "*) SEL+="$c " ;; esac
    done
    SEL="$(trim "$SEL")"
    [[ -n "$SEL" ]] || die2 "LEAK_CHECK_CLASSES named no usable class (valid: ${CLASS_SET// /,})"
fi

wants() { case " $SEL " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

# ---------------------------------------------------------- config parsing
declare -A PATTERNS_RE=()
while IFS= read -r line || [[ -n "$line" ]]; do
    line="$(trim "$line")"
    [[ -z "$line" || "$line" == \#* ]] && continue
    cls="${line%%[[:space:]]*}"
    re="$(trim "${line#"$cls"}")"
    case " $CLASS_SET " in *" $cls "*) ;; *) die2 "patterns file: unknown class: $cls" ;; esac
    [[ -n "$re" ]] || die2 "patterns file: class '$cls' has no regex"
    rc=0
    printf '' | grep -E -e "$re" >/dev/null 2>&1 || rc=$?
    if (( rc == 2 )); then
        die2 "patterns file: class '$cls' regex is not a valid extended regex"
    fi
    if [[ -n "${PATTERNS_RE[$cls]:-}" ]]; then
        PATTERNS_RE[$cls]+=$'\n'"$re"
    else
        PATTERNS_RE[$cls]="$re"
    fi
done <"$PATTERNS_FILE"

ALLOWED_EMAILS=()
while IFS= read -r line || [[ -n "$line" ]]; do
    line="$(trim "$line")"
    [[ -z "$line" || "$line" == \#* ]] && continue
    ALLOWED_EMAILS+=("$line")
done <"$ALLOWED_FILE"

EXCLUDE_GLOBS=()
while IFS= read -r line || [[ -n "$line" ]]; do
    line="$(trim "$line")"
    [[ -z "$line" || "$line" == \#* ]] && continue
    EXCLUDE_GLOBS+=("$line")
done <"$EXCLUDE_FILE"

ADDR_ALLOWED=()
if [[ -f "$ADDR_ALLOWED_FILE" && -r "$ADDR_ALLOWED_FILE" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
        line="$(trim "$line")"
        [[ -z "$line" || "$line" == \#* ]] && continue
        ADDR_ALLOWED+=("$line")
    done <"$ADDR_ALLOWED_FILE"
fi

LOCAL_PATTERNS=()
if [[ -f "$LOCAL_FILE" && -r "$LOCAL_FILE" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
        line="$(trim "$line")"
        [[ -z "$line" || "$line" == \#* ]] && continue
        LOCAL_PATTERNS+=("$line")
    done <"$LOCAL_FILE"
fi
HAVE_LOCAL=0
if (( ${#LOCAL_PATTERNS[@]} > 0 )); then
    HAVE_LOCAL=1
else
    if wants local; then
        SEL="${SEL// local / }"
    fi
    echo "local patterns: skipped" >&2
fi

# ---------------------------------------------------------------- file list
FILES=()
if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    while IFS= read -r -d '' f; do
        FILES+=("$f")
    done < <(cd "$ROOT" && git ls-files -z)
else
    while IFS= read -r -d '' f; do
        FILES+=("${f#./}")
    done < <(cd "$ROOT" && find . -name .git -prune -o -type f -print0)
fi

# --------------------------------------------------------------- helpers
REPORT_LINES=()
declare -A COUNT=([address]=0 [home]=0 [email]=0 [local]=0)

record() { # class rel n
    COUNT[$1]=$(( COUNT[$1] + 1 ))
    REPORT_LINES+=("$2:$3: $1")
}

is_binary() { # file — succeeds when the file holds a NUL byte (the portable
    # "binary" test; UTF-8 and other multi-byte text is NOT binary)
    head -c 65536 -- "$1" 2>/dev/null | LC_ALL=C tr '\000' '\001' | grep -q $'\001'
}

allowed_email() { # already-lowercased address
    local g
    shopt -s nocasematch
    for g in "${ALLOWED_EMAILS[@]}"; do
        # shellcheck disable=SC2053  # intentional glob match
        if [[ "$1" == $g ]]; then
            shopt -u nocasematch
            return 0
        fi
    done
    shopt -u nocasematch
    return 1
}

# -------------------------------------------- per-class regex scanning
# grep only supplies line numbers; the matched text is discarded and never
# printed. SEEN keeps one finding per (class, line).
declare -A SEEN=()
match_class() { # class rel file
    local cls="$1" rel="$2" f="$3" re n
    [[ -n "${PATTERNS_RE[$cls]:-}" ]] || return 0
    while IFS= read -r re; do
        [[ -z "$re" ]] && continue
        mapfile -t NS < <(grep -nE -e "$re" -- "$f" 2>/dev/null | cut -d: -f1)
        for n in "${NS[@]}"; do
            [[ -n "${SEEN[$cls:$n]:-}" ]] && continue
            if [[ "$cls" == address ]] && (( ${#ADDR_ALLOWED[@]} > 0 )); then
                # Well-known public constants (the cloud metadata endpoints)
                # may stay; a line whose only matches are those passes.
                aline="$(sed -n "${n}p" -- "$f")"
                for a in "${ADDR_ALLOWED[@]}"; do aline="${aline//"$a"/ }"; done
                printf '%s\n' "$aline" | grep -qE -e "$re" || continue
            fi
            SEEN["$cls:$n"]=1
            record "$cls" "$rel" "$n"
        done
    done < <(printf '%s\n' "${PATTERNS_RE[$cls]}")
}

# Gate: a loose superset of the active classes' match sets. Every line an
# active class could flag contains one of these fragments, so a file with
# no gate hits cannot produce a finding and needs no per-class passes.
GATE_PARTS=()
if wants address; then
    GATE_PARTS+=('(^|[^0-9./-])(10[.][0-9]{1,3}[.][0-9]{1,3}[.][0-9]{1,3}|172[.](1[6-9]|2[0-9]|3[01])[.][0-9]{1,3}[.][0-9]{1,3}|192[.]168[.][0-9]{1,3}[.][0-9]{1,3}|100[.](6[4-9]|[7-9][0-9]|1[012][0-7])[.][0-9]{1,3}[.][0-9]{1,3}|169[.]254[.][0-9]{1,3}[.][0-9]{1,3})([^0-9./-]|$)'  '(^|[^0-9a-fA-F:])(fc|fd)[0-9a-fA-F]{2}[:]'  '2606[0-9a-fA-F:]'  '[.]ts[.]net')
fi
if wants home; then
    GATE_PARTS+=('/home/[A-Za-z0-9._-]'  '/Users/[A-Za-z0-9._-]'  '[A-Za-z]:[\\/]Users[\\/][A-Za-z0-9._-]')
fi
if wants email; then
    GATE_PARTS+=("${EMAIL_EXTRACT_RE}")
fi
GATE_RE=""
for _part in "${GATE_PARTS[@]}"; do
    GATE_RE+="|${_part}"
done
GATE_RE="${GATE_RE#|}"

for rel in "${FILES[@]}"; do
    rel="${rel#./}"
    [[ -n "$rel" ]] || continue
    f="$ROOT/$rel"
    [[ -f "$f" ]] || continue
    skip=0
    for g in "${EXCLUDE_GLOBS[@]}"; do
        # shellcheck disable=SC2053  # intentional glob match
        if [[ "$rel" == $g ]]; then
            skip=1
            break
        fi
    done
    if (( skip )); then
        continue
    fi
    # The operator's local patterns file is configuration, not content: a
    # pattern line always substring-matches itself, so never scan it.
    local_rel=""
    if [[ "$LOCAL_FILE" == "$ROOT"/* ]]; then
        local_rel="${LOCAL_FILE#"$ROOT"/}"
    fi
    if [[ -n "$local_rel" && "$rel" == "$local_rel" ]]; then
        continue
    fi
    file_interest=0
    if [[ -n "$GATE_RE" ]] && grep -qE -e "$GATE_RE" -- "$f" 2>/dev/null; then
        file_interest=1
    fi
    if (( file_interest == 0 )) && (( HAVE_LOCAL )); then
        for _pat in "${LOCAL_PATTERNS[@]}"; do
            if grep -qiF -e "$_pat" -- "$f" 2>/dev/null; then
                file_interest=1
                break
            fi
        done
    fi
    if (( file_interest == 0 )); then
        continue
    fi
    if is_binary "$f"; then
        continue
    fi

    SEEN=()
    if wants address; then match_class address "$rel" "$f"; fi
    if wants home;    then match_class home    "$rel" "$f"; fi
    if wants email && [[ -n "${PATTERNS_RE[email]:-}" ]]; then
        while IFS= read -r re; do
            [[ -z "$re" ]] && continue
            mapfile -t NS < <(grep -nE -e "$re" -- "$f" 2>/dev/null | cut -d: -f1)
            for n in "${NS[@]}"; do
                [[ -n "${SEEN[email:$n]:-}" ]] && continue
                # Read just this line, test the candidates, discard both.
                line="$(sed -n "${n}p" -- "$f")"
                all_allowed=1
                while IFS= read -r cand; do
                    [[ -z "$cand" ]] && continue
                    if ! allowed_email "${cand,,}"; then
                        all_allowed=0
                        break
                    fi
                done < <(printf '%s\n' "$line" | grep -oE -- "$EMAIL_EXTRACT_RE" | tr '[:upper:]' '[:lower:]' | sort -u)
                (( all_allowed )) && continue
                SEEN["email:$n"]=1
                record email "$rel" "$n"
            done
        done < <(printf '%s\n' "${PATTERNS_RE[email]}")
    fi
    if (( HAVE_LOCAL )) && wants local; then
        lineno=0
        while IFS= read -r ln || [[ -n "$ln" ]]; do
            lineno=$(( lineno + 1 ))
            lnc="${ln,,}"
            for pat in "${LOCAL_PATTERNS[@]}"; do
                if [[ "$lnc" == *"${pat,,}"* ]]; then
                    if [[ -z "${SEEN[local:$lineno]:-}" ]]; then
                        SEEN["local:$lineno"]=1
                        record local "$rel" "$lineno"
                    fi
                    break
                fi
            done
        done <"$f"
    fi
done

# ---------------------------------------------------------------- reporting
for line in "${REPORT_LINES[@]}"; do
    printf '%s\n' "$line"
done
for cls in $CLASS_SET; do
    printf '%s: %d\n' "$cls" "${COUNT[$cls]}"
done

TOTAL=$(( COUNT[address] + COUNT[home] + COUNT[email] + COUNT[local] ))
if (( TOTAL > 0 )); then
    exit 1
fi
exit 0
