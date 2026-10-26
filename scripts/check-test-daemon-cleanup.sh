#!/usr/bin/env bash
# scripts/check-test-daemon-cleanup.sh — static audit: every job that starts a
# datawatch daemon must end that daemon, and every session the daemon started,
# on every exit path (normal exit, failure and interrupt). A sandbox daemon
# left alive from a one-shot live-test setup held its ports and a session open
# for days; the same class of leak keeps recurring in test/verify scripts.
#
# Rules:
#   1. A shell file that starts a daemon (a non-comment line holding `start`
#      and `--foreground`, or a call to td_start) passes only if it calls
#      td_start or td_track_pid from scripts/test-daemon-lib.sh (cleanup is
#      then owned by the shared traps), or has its own trap naming EXIT, INT
#      and TERM (HUP optional). A trap on EXIT alone fails.
#   2. No non-comment line of a shell file may end processes by name:
#      pkill, killall, `pgrep -f`, `xargs kill`, or a tmux kill-session fed by
#      a grep over session names (a pipeline that greps a name prefix and
#      then kills). Only a PID the script itself recorded may be signalled,
#      and only after its live /proc/<pid>/cmdline still carries the marker.
#   3. A Go test file that runs the binary as a daemon (exec.Command or
#      exec.CommandContext whose arguments include "start" and "--foreground")
#      must register t.Cleanup in the same file.
# A line may carry the `daemon-cleanup-ok` marker with a comment saying why it
# is safe (an audit regular expression, or a long-lived sandbox start that has
# a maximum lifetime and a wipe command); marked lines and comment-only lines
# are skipped. The checker only reads files; it never signals any process and
# never touches a multiplexer server.
#
# With no arguments it scans every *.sh under scripts/ and tests/ and every
# *_test.go under internal/, cmd/ and tests/. With arguments it checks exactly
# the files given (tests point it at fixture files).
# CHECK_ROOT overrides the repository root (tests point it at a fixture tree).
# Run independently: env -u TMUX -u TMUX_TMPDIR bash scripts/check-test-daemon-cleanup.sh

set -euo pipefail

ROOT="${CHECK_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"

SELF="check-test-daemon-cleanup.sh"
MARKER="daemon-cleanup-ok"
ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

# Banned end-by-name patterns, word-bounded so a longer word does not match.
# The audit's own regular expressions therefore hold the words they forbid;
# this file is exempt by its basename, and these definitions are marked.
R_PKILL='(^|[^[:alnum:]_])pkill([^[:alnum:]_]|$)' # daemon-cleanup-ok: audit regex for a banned word
R_KILLALL='(^|[^[:alnum:]_])killall([^[:alnum:]_]|$)' # daemon-cleanup-ok: audit regex for a banned word
R_PGREP_F='(^|[^[:alnum:]_])pgrep([[:space:]]+-[A-Za-z]+)*[[:space:]]+-f' # daemon-cleanup-ok: audit regex for a name lookup
R_XARGS_KILL='(^|[^[:alnum:]_])xargs[^|;&]*[[:space:]]+kill([^[:alnum:]_]|$)' # daemon-cleanup-ok: audit regex for a kill fed by a name lookup
R_KILLSESSION='kill-session'
R_SESSION_GREP='grep([[:space:]]+-[A-Za-z]+)*[[:space:]]+.{0,8}\^'
R_START_WORD='(^|[^[:alnum:]_.-])start([^[:alnum:]_]|$)'
R_TD_CALL='(^|[[:space:];|&(])td_start([[:space:]]|\()'
R_TD_TRACK='(^|[[:space:];|&(])td_track_pid([[:space:]]|\()'
R_SIG_EXIT='(^|[^[:alnum:]_])EXIT([^[:alnum:]_]|$)'
R_SIG_INT='(^|[^[:alnum:]_])INT([^[:alnum:]_]|$)'
R_SIG_TERM='(^|[^[:alnum:]_])TERM([^[:alnum:]_]|$)'
R_GOCMD='exec\.Command(Context)?[[:space:]]*\('

# audit_shell <file> — rules 1 and 2 over one shell file.
audit_shell() {
    local file="$1"
    local -a lines=()
    mapfile -t lines < "$file"
    local n=${#lines[@]}
    local i text
    local daemon_line="" td_helper=0
    local any_trap=0 trap_exit=0 trap_int=0 trap_term=0

    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        if [[ "$text" =~ ^[[:space:]]*# ]]; then
            continue
        fi
        if [[ "$text" == *"$MARKER"* ]]; then
            continue
        fi
        if [[ "$text" =~ $R_PKILL ]]; then
            report "$file:$((i + 1)): ends processes by name instead of signalling only a PID it recorded"
            continue
        fi
        if [[ "$text" =~ $R_KILLALL ]]; then
            report "$file:$((i + 1)): ends processes by name instead of signalling only a PID it recorded"
            continue
        fi
        if [[ "$text" =~ $R_PGREP_F ]]; then
            report "$file:$((i + 1)): looks up processes by name to signal them"
            continue
        fi
        if [[ "$text" =~ $R_XARGS_KILL ]]; then
            report "$file:$((i + 1)): signals processes found by a name lookup"
            continue
        fi
        if [[ "$text" =~ $R_KILLSESSION && "$text" =~ $R_SESSION_GREP ]]; then
            report "$file:$((i + 1)): kills a tmux session whose name comes from a name-prefix search"
        fi
        if [[ "$text" =~ $R_START_WORD && "$text" == *--foreground* ]]; then
            if [[ -z "$daemon_line" ]]; then
                daemon_line="$((i + 1))"
            fi
        fi
        if [[ "$text" =~ $R_TD_CALL || "$text" =~ $R_TD_TRACK ]]; then
            td_helper=1
        fi
    done

    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        if [[ "$text" =~ ^[[:space:]]*# ]]; then
            continue
        fi
        if [[ ! "$text" =~ ^[[:space:]]*trap[[:space:]] ]]; then
            continue
        fi
        any_trap=1
        if [[ "$text" =~ $R_SIG_EXIT ]]; then trap_exit=1; fi
        if [[ "$text" =~ $R_SIG_INT ]];  then trap_int=1;  fi
        if [[ "$text" =~ $R_SIG_TERM ]]; then trap_term=1; fi
    done

    if [[ -n "$daemon_line" && "$td_helper" -eq 0 ]]; then
        if [[ "$trap_exit" -eq 1 && "$trap_int" -eq 1 && "$trap_term" -eq 1 ]]; then
            :
        elif [[ "$any_trap" -eq 1 ]]; then
            report "$file:$daemon_line: starts a datawatch daemon, but its traps do not cover EXIT, INT and TERM (a trap on EXIT alone leaves failure and interrupt open); use td_start/td_track_pid from scripts/test-daemon-lib.sh or trap EXIT, INT and TERM"
        else
            report "$file:$daemon_line: starts a datawatch daemon with no cleanup on every exit path; use td_start/td_track_pid from scripts/test-daemon-lib.sh, trap EXIT, INT and TERM, or end the PID it recorded by hand"
        fi
    fi
    return 0
}

# audit_go <file> — rule 3 over one Go test file.
audit_go() {
    local file="$1"
    local -a lines=()
    mapfile -t lines < "$file"
    local n=${#lines[@]}
    local i k text win has_cleanup=0

    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        if [[ "$text" =~ ^[[:space:]]*(//|/\*) || "$text" =~ ^[[:space:]]*\* ]]; then
            continue
        fi
        if [[ "$text" == *"$MARKER"* ]]; then
            continue
        fi
        if [[ "$text" == *"t.Cleanup"* ]]; then
            has_cleanup=1
        fi
    done

    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        if [[ "$text" =~ ^[[:space:]]*(//|/\*) || "$text" =~ ^[[:space:]]*\* ]]; then
            continue
        fi
        if [[ "$text" == *"$MARKER"* ]]; then
            continue
        fi
        if [[ "$text" =~ $R_GOCMD ]]; then
            # The argument list may continue on the following lines.
            win="$text"
            for ((k = i + 1; k < n && k <= i + 6; k++)); do
                win="$win ${lines[$k]}"
            done
            if [[ "$win" == *'"start"'* ]] && [[ "$win" == *--foreground* ]]; then
                if [[ "$has_cleanup" -eq 0 ]]; then
                    report "$file:$((i + 1)): starts a datawatch daemon without registering a t.Cleanup that ends it"
                fi
            fi
        fi
    done
    return 0
}

FILES=("$@")
if [[ $# -gt 0 ]]; then
    for i in "${!FILES[@]}"; do
        if [[ "${FILES[$i]}" != /* ]]; then
            FILES[$i]="$PWD/${FILES[$i]}"
        fi
    done
else
    cd "$ROOT"
    for d in scripts tests; do
        if [[ -d "$d" ]]; then
            while IFS= read -r f; do
                FILES+=("$f")
            done < <(find "$d" -type f -name '*.sh' 2>/dev/null | LC_ALL=C sort)
        fi
    done
    for d in internal cmd tests; do
        if [[ -d "$d" ]]; then
            while IFS= read -r f; do
                FILES+=("$f")
            done < <(find "$d" -type f -name '*_test.go' 2>/dev/null | LC_ALL=C sort)
        fi
    done
fi

echo "==> Datawatch daemon cleanup audit (daemon start without cleanup / end-by-name / Go tests missing t.Cleanup)"

for f in "${FILES[@]}"; do
    if [[ ! -f "$f" ]]; then
        continue
    fi
    if [[ "$(basename "$f")" == "$SELF" ]]; then
        continue
    fi
    case "$f" in
        *.sh) audit_shell "$f" ;;
        *.go) audit_go "$f" ;;
    esac
done

if [[ "$ERRORS" -gt 0 ]]; then
    echo
    echo "  hint: every job that starts a datawatch daemon must end that daemon, and every"
    echo "  session the daemon started, on every exit path (normal exit, failure, interrupt):"
    echo "    shell: source scripts/test-daemon-lib.sh and use td_start (or td_track_pid);"
    echo "           its shared traps cover EXIT, INT and TERM and end only the recorded PID"
    echo "    shell: or add your own trap naming EXIT, INT and TERM, signalling only a PID"
    echo "           the script recorded while /proc/<pid>/cmdline still carries its marker"
    echo "    shell: never end processes by name, never stop a multiplexer server"
    echo "    go:    register t.Cleanup in the test that starts the daemon"
    echo
    echo "FAIL: $ERRORS daemon cleanup violation(s)"
    exit 1
fi
echo "OK: no daemon cleanup violations"
