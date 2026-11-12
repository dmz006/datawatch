#!/usr/bin/env bash
# scripts/survey-test-tmp.sh — survey what test binaries leave behind in
# their temporary directory. Runs `go test -count=1` one package at a time,
# each with TMPDIR pointed at a fresh private directory (mktemp -d) and
# without TMUX / TMUX_TMPDIR in its environment. After each package the
# entries that survive in that private directory are listed and grouped by
# name prefix, and a table of (prefix, count, package) is printed.
#
# The private directory is removed on every exit path: normal exit,
# failure and interrupt (one active directory at a time, tracked by trap).
# The script only observes leftover files; it never kills or signals
# anything, and it runs no tmux server command.
#
# With no package arguments the package list comes from `go list ./...`.
# Pass packages (or any patterns `go list` accepts) to survey a subset:
#   ./scripts/survey-test-tmp.sh ./internal/testhome/
#
# Exit status is 0 when every surveyed package passed, 1 when any package
# failed, 2 on usage errors.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

usage() {
    cat <<'EOF'
Usage: survey-test-tmp.sh [package ...]
       survey-test-tmp.sh --help

Run `go test -count=1` one package at a time. Each package runs with
TMPDIR set to a fresh private directory (mktemp -d) and without TMUX /
TMUX_TMPDIR in its environment. After each package, the entries left
behind in that private directory are listed and grouped by name prefix
into a (prefix, count, package) table. The private directory is removed
on every exit path — normal exit, failure and interrupt.

With no package arguments the package list comes from `go list ./...`;
pass packages (or patterns accepted by `go list`) to survey a subset.
The script only reports; it never kills anything.

Exit status: 0 every package passed, 1 any package failed, 2 usage error.
EOF
    exit "${1:-2}"
}

case "${1:-}" in
    -h|--help)
        usage 0
        ;;
esac

PRIVATE_DIR=""
cleanup() {
    if [[ -n "$PRIVATE_DIR" && -d "$PRIVATE_DIR" ]]; then
        rm -rf -- "$PRIVATE_DIR" 2>/dev/null || true
    fi
}
trap cleanup EXIT

mapfile -t PKGS < <(
    if (($#)); then
        go list "$@"
    else
        go list ./...
    fi
)
if ((${#PKGS[@]} == 0)); then
    echo "no packages matched" >&2
    exit 2
fi

name_prefix() {
    # The leading word of an entry name: chop a trailing random suffix
    # (a run of at least four hex characters, possibly preceded by a
    # separator) plus the separator, keep the first word characters that
    # remain, and bucket anything else under (other).
    local b="$1"
    b="${b%%[0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]*}"
    b="${b%[-._/ ]}"
    [[ "$b" =~ ^[A-Za-z][A-Za-z0-9_-]*$ ]] || b="(other)"
    printf '%s' "$b"
}

print_report() {
    # $1 = package, $2 = private directory. Lists survivors, groups them by
    # name prefix and prints the (prefix, count, package) table.
    local pkg="$1" dir="$2"
    local -A counts=()
    local entry n=0
    printf 'package %-40s leftovers in %-24s\n' "$pkg" "$dir"
    while IFS= read -r entry; do
        [[ -n "$entry" ]] || continue
        printf '  %s\n' "$entry"
        local base="${entry##*/}"
        local p
        p="$(name_prefix "$base")"
        counts["$p"]=$(( ${counts["$p"]:-0} + 1 ))
        n=$((n + 1))
    done < <(find "$dir" -mindepth 1 2>/dev/null || true)
    if ((n == 0)); then
        printf '  (private directory empty)\n'
    else
        printf '  %-18s %-6s %s\n' "prefix" "count" "package"
        while IFS=$'\t' read -r p c; do
            printf '  %-18s %-6s %s\n' "$p" "$c" "$pkg"
        done < <(
            for p in "${!counts[@]}"; do
                printf '%s\t%s\n' "$p" "${counts[$p]}"
            done | sort
        )
    fi
    printf '\n'
}

FAILURES=0
for pkg in "${PKGS[@]}"; do
    PRIVATE_DIR="$(mktemp -d)"
    printf '== %s ==\n' "$pkg"
    if output="$(env -u TMUX -u TMUX_TMPDIR TMPDIR="$PRIVATE_DIR" go test -count=1 "$pkg" 2>&1)"; then
        printf '%s\n' "$output"
    else
        printf '%s\n' "$output"
        FAILURES=$((FAILURES + 1))
    fi
    print_report "$pkg" "$PRIVATE_DIR"
    rm -rf -- "$PRIVATE_DIR"
    PRIVATE_DIR=""
    if ((FAILURES > 0)); then
        # Report the failure but keep surveying the remaining packages so
        # the table covers every selection.
        :
    fi
done

if ((FAILURES > 0)); then
    echo "FAIL: $FAILURES package(s) failed (leftovers surveyed above)"
    exit 1
fi
echo "OK: all ${#PKGS[@]} package(s) passed"
