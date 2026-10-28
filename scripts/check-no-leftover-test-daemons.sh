#!/usr/bin/env bash
# scripts/check-no-leftover-test-daemons.sh — detects datawatch daemons that a
# job started on a throwaway config under a temporary location and left alive
# after its work finished (a sandbox daemon from a one-shot live-test setup in
# a temp folder, an end-to-end test daemon from a mktemp folder, and the like).
# Every job that starts a datawatch daemon must end that daemon, and every
# session the daemon started, on every exit path: normal exit, failure and
# interrupt. This script only reports; it never kills anything.
#
# A test daemon is a process whose argument vector holds `start`,
# `--foreground` and a `--config <path>` argument where the path lies under a
# temporary location: /tmp, the value of TMPDIR when set, or a
# datawatch-<hex> run folder next to the repository (a sibling of this repo).
# A daemon whose config lives anywhere else (for example the operator's own
# data directory) is never a test daemon, so the operator's production daemon
# is never flagged.
#
# Detection scans /proc/<pid>/cmdline read-only (it never signals anything)
# and tolerates a process vanishing between listing and reading.
#
# Modes:
#   snapshot <file>   write the PIDs of every live test daemon to <file>,
#                     one PID per line (empty file when none are live)
#   check <file>      list the live test daemons now and fail (exit 1) when
#                     any of them is not in the snapshot, printing for each
#                     leftover its PID, age, config path and the TCP ports it
#                     listens on (ss -ltnp filtered by PID when ss exists,
#                     "unknown" without ss); exit 0 with a one-line OK when
#                     nothing new is left
#
# With no mode it prints usage and exits 2.
#
# The test runner takes a snapshot at the start of a run and runs `check`
# at the end (a later task wires that in); the script can be run by hand too:
#   env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-leftover-test-daemons.sh snapshot /tmp/base.txt
#   env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-leftover-test-daemons.sh check /tmp/base.txt
# CHECK_ROOT overrides the repository root (tests point it at a fixture tree).

set -euo pipefail

ROOT="${CHECK_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

usage() {
    echo "Usage: $0 snapshot <file>   remember the live test daemons now"
    echo "       $0 check <file>      fail when a live test daemon is not in <file>"
    exit 2
}
if [[ $# -ne 2 ]]; then
    usage
fi
mode="$1"
file="$2"
if [[ "$mode" != "snapshot" && "$mode" != "check" ]]; then
    usage
fi

BASELINE_PIDS=()

is_temp_path() {
    # Return 0 when <path> is under /tmp, under $TMPDIR (when set), or inside
    # a datawatch-<hex> run folder that is a sibling of the repository root.
    local p="$1" re
    case "$p" in
        /tmp|/tmp/*) return 0 ;;
    esac
    if [[ -n "${TMPDIR:-}" ]]; then
        case "$p" in
            "$TMPDIR"|"$TMPDIR"/*) return 0 ;;
        esac
    fi
    re="^$(dirname "$ROOT")/datawatch-[0-9a-fA-F]+(/.*)?$"
    if [[ "$p" =~ $re ]]; then
        return 0
    fi
    return 1
}

collect_test_daemons() {
    # Print "pid<TAB>config" lines for every live test daemon. Read-only over
    # /proc; a pid that vanishes between listing and reading is skipped.
    local d pid cmd cfg rest
    for d in /proc/[0-9]*; do
        [[ -d "$d" ]] || continue
        pid="${d#/proc/}"
        [[ -r "$d/cmdline" ]] || continue
        # argv elements are NUL-separated; rejoining with spaces lets both
        # plain word arguments and argv[0] strings that embed the whole
        # command be matched as words.
        cmd="$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null || true)"
        [[ -n "$cmd" ]] || continue
        # Word-boundary matching with globs (no capture groups, no regex).
        case " $cmd " in
            *" start "*) ;;
            *) continue ;;
        esac
        case " $cmd " in
            *" --foreground "*) ;;
            *) continue ;;
        esac
        case " $cmd " in
            *" --config="*)
                rest="${cmd#*" --config="}"
                cfg="${rest%%[[:space:]]*}"
                ;;
            *" --config "*)
                rest="${cmd#*" --config "}"
                cfg="${rest%%[[:space:]]*}"
                ;;
            *) continue ;;
        esac
        [[ -n "$cfg" ]] || continue
        is_temp_path "$cfg" || continue
        printf '%s\t%s\n' "$pid" "$cfg"
    done
    return 0
}

in_baseline() {
    local pid="$1" p
    for p in "${BASELINE_PIDS[@]}"; do
        if [[ "$p" == "$pid" ]]; then
            return 0
        fi
    done
    return 1
}

age_human() {
    local s="$1"
    if ((s >= 86400)); then
        printf '%dd %dh %dm' $((s / 86400)) $(((s % 86400) / 3600)) $(((s % 3600) / 60))
    elif ((s >= 3600)); then
        printf '%dh %dm %ds' $((s / 3600)) $(((s % 3600) / 60)) $((s % 60))
    elif ((s >= 60)); then
        printf '%dm %ds' $((s / 60)) $((s % 60))
    else
        printf '%ds' "$s"
    fi
}

ports_for() {
    # TCP ports this pid listens on, from ss -ltnp; empty when ss is missing
    # or the pid holds no listening sockets.
    local pid="$1" out=""
    if ! command -v ss >/dev/null 2>&1; then
        return 0
    fi
    out="$(ss -ltnp 2>/dev/null | awk -v p="$pid" '
        index($0, "pid=" p ",") || index($0, "pid=" p ")") {
            pt = $4; sub(/.*:/, "", pt)
            if (pt ~ /^[0-9]+$/ && !seen[pt]++)
                res = (res == "" ? pt : res "," pt)
        }
        END { if (res != "") print res }' || true)"
    # the pid may have vanished between listing and this ss scan
    printf '%s' "${out:-}"
    return 0
}

load_baseline() {
    local line
    if [[ -f "$file" ]]; then
        while IFS= read -r line; do
            line="${line//[[:space:]]/}"
            if [[ -n "$line" ]]; then
                BASELINE_PIDS+=("$line")
            fi
        done < "$file"
    fi
    return 0
}

do_snapshot() {
    local pid cfg n=0
    if [[ -n "$(dirname "$file")" ]]; then
        mkdir -p "$(dirname "$file")" 2>/dev/null || true
    fi
    : > "$file"
    while IFS=$'\t' read -r pid cfg; do
        if [[ -z "${pid:-}" ]]; then
            continue
        fi
        printf '%s\n' "$pid" >> "$file"
        n=$((n + 1))
    done < <(collect_test_daemons)
    echo "snapshot: $n live test daemon pid(s) recorded in $file"
}

do_check() {
    local pid cfg et age ports
    load_baseline
    n_live=0
    while IFS=$'\t' read -r pid cfg; do
        if [[ -z "${pid:-}" ]]; then
            continue
        fi
        n_live=$((n_live + 1))
        if in_baseline "$pid"; then
            continue
        fi
        age="unknown"
        if [[ -d "/proc/$pid" ]]; then
            et="$(ps -o etimes= -p "$pid" 2>/dev/null | tr -d '[:space:]' || true)"
            if [[ "$et" =~ ^[0-9]+$ ]]; then
                age="$(age_human "$et")"
            fi
        fi
        ports="$(ports_for "$pid")"
        if [[ -z "$ports" ]]; then
            ports="unknown"
        fi
        report "leftover test daemon: pid=$pid age=$age config=${cfg:-unknown} tcp_listening=$ports"
    done < <(collect_test_daemons)
    if ((ERRORS > 0)); then
        echo
        echo "  hint: every job that starts a datawatch daemon must end that"
        echo "  daemon, and every session the daemon started, on every exit"
        echo "  path - normal exit, failure and interrupt."
        if [[ ! -s "$file" ]]; then
            echo "  note: the baseline $file is empty, so every live test daemon"
            echo "  right now is reported - on a shared host that includes the"
            echo "  daemons of other concurrent sessions, not only this run."
            echo "  Take a snapshot before the run you are investigating"
            echo "  (snapshot <file>) to tell the new ones from the existing"
            echo "  ones, and end only the daemons a job actually owns."
        fi
        echo "FAIL: $ERRORS leftover test daemon(s) not in the baseline $file (live test daemons now: $n_live)"
        exit 1
    fi
    echo "OK: no leftover test daemons"
}

case "$mode" in
    snapshot) do_snapshot ;;
    check) do_check ;;
esac
