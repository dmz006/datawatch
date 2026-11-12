#!/usr/bin/env bash
# scripts/clean-test-tmp.sh — remove stale test scratch directories
#
# Cleans direct children of the program temp directory (TMPDIR if set,
# otherwise the parent of `mktemp -u -d`'s default location, i.e. /tmp).
# An entry is removed only if ALL of the following hold:
#   - it is a direct child of the temp directory (never a nested path),
#   - lstat says it is a real directory (a symlink is never a candidate),
#   - its name starts with one of the known throwaway test prefixes
#     (dw-test-tmux-, dw-test-home-, dw-leak-, dw-tmp-leak-guard-,
#      svchelper-, sched-test-, datawatch-pi-task-, datawatch-pi-status-,
#      datawatch-pi-out-, datawatch-claudecode-task-,
#      datawatch-opencode-task-, datawatch-aider-task-,
#      datawatch-goose-task-),
#   - it is owned by the current user,
#   - its mtime is older than one hour.
#
# Before removing, the tree is made writable (chmod -R u+rwX, which GNU
# applies without following symlinks during recursion). rm -rf is then
# run with --; it never follows symlinks when deleting the entry.
#
# Manifest: if <tmp>/dw-test-manifest exists (one absolute directory per
# line, see internal/testhome/manifest.go), its entries are also
# candidates under exactly the same safety checks, and the manifest is
# rewritten with the survivors afterwards (a directory that still needs
# another attempt stays listed). A missing manifest is not an error.
#
# Safety: this script never touches tmux, never follows symlinks, and
# uses no pkill/kill/killall at all — it only deletes the specific
# directory trees described above and prints a one-line summary.
# It is meant to be run under `env -u TMUX -u TMUX_TMPDIR` (the
# Makefile target does that); the variables are also stripped here so a
# direct invocation is equally safe.
#
# Exit status: 0 on success (including "nothing to remove"), 1 when
# some candidate could not be removed, 2 on usage errors.

set -euo pipefail

# Belt-and-braces: nothing in this script may ever talk to a tmux server.
unset TMUX TMUX_TMPDIR

TMP="${TMPDIR:-}"
if [[ -z "$TMP" ]]; then
    # Default: parent of where `mktemp -u -d` would create its tree
    # (which is /tmp when TMPDIR is unset).
    TMP="$(dirname "$(mktemp -u -d)")"
fi
if [[ ! -d "$TMP" ]]; then
    echo "clean-test-tmp: temp directory not found: $TMP" >&2
    exit 2
fi
TMP="$(CDPATH= cd -- "$TMP" && pwd)"

MAX_AGE_MIN=60   # older than one hour

is_known_prefix() {
    case "$1" in
    dw-test-tmux-* | dw-test-home-* | dw-leak-* | dw-tmp-leak-guard-* | svchelper-* | sched-test-* | \
        datawatch-pi-task-* | datawatch-pi-status-* | datawatch-pi-out-* | \
        datawatch-claudecode-task-* | datawatch-opencode-task-* | \
        datawatch-aider-task-* | datawatch-goose-task-*)
        return 0
        ;;
    *)
        return 1
        ;;
    esac
}

# removable: passes every safety check. Never follows symlinks:
# -L / -d are lstat-based, and find -maxdepth 0 does not follow either.
removable() {
    local d="$1" uid
    [[ -d "$d" && ! -L "$d" ]] || return 1
    is_known_prefix "${d##*/}" || return 1
    uid="$(stat -c %u -- "$d" 2>/dev/null)" || return 1
    [[ "$uid" == "$(id -u)" ]] || return 1
    [[ -n "$(find "$d" -maxdepth 0 -mmin +${MAX_AGE_MIN} -print -quit 2>/dev/null)" ]] || return 1
    return 0
}

removed=()
kept=()

# remove_entry: make the tree writable (never follows symlinks in the
# recursion), then delete it. Returns 1 (does not abort the script) if
# it could not be fully removed.
remove_entry() {
    local d="$1"
    chmod -R u+rwX -- "$d" 2>/dev/null || true
    if rm -rf -- "$d" 2>/dev/null; then
        removed+=("${d##*/}")
        return 0
    fi
    kept+=("${d##*/}")
    echo "clean-test-tmp: WARNING: could not remove $d" >&2
    return 1
}

# Pass 1: everything in the temp directory.
shopt -s nullglob
for d in "$TMP"/*; do
    if removable "$d"; then
        remove_entry "$d" || true
    fi
done

# Pass 2: honour the manifest (entries may name directories the scan
# already removed — the survivors list is what gets rewritten).
manifest="$TMP/dw-test-manifest"
if [[ -f "$manifest" && ! -L "$manifest" ]]; then
    survivors=()
    while IFS= read -r line || [[ -n "${line:-}" ]]; do
        # trim surrounding whitespace
        line="${line#"${line%%[![:space:]]*}"}"
        line="${line%"${line##*[![:space:]]}"}"
        if [[ -z "$line" ]]; then
            continue
        fi
        # same checks as the Go sweeper: absolute direct child of TMP,
        # a removable directory, and not already gone
        if [[ "$line" == /* ]] \
            && [[ "$(CDPATH= cd -- "$(dirname -- "$line")" 2>/dev/null && pwd 2>/dev/null)" == "$TMP" ]]; then
            if [[ -d "$line" && ! -L "$line" ]] && removable "$line"; then
                remove_entry "$line" || survivors+=("$line")
            elif [[ ! -e "$line" && ! -L "$line" ]] && is_known_prefix "${line##*/}"; then
                : # already gone and was a valid candidate — drop the line
            else
                survivors+=("$line")
            fi
        else
            survivors+=("$line")
        fi
    done <"$manifest"
    : >"$manifest"
    if ((${#survivors[@]} > 0)); then
        printf '%s\n' "${survivors[@]}" >"$manifest"
    fi
fi

if ((${#kept[@]} > 0)); then
    echo "clean-test-tmp: removed ${#removed[@]} entries from $TMP (${removed[*]:-}); kept ${#kept[@]}: ${kept[*]}"
    exit 1
fi
echo "clean-test-tmp: removed ${#removed[@]} entries from $TMP (${removed[*]:-none})"
