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
#      then kills), a kill whose PID list came from a name search (pgrep in
#      any flag form, or ps piped into grep feeding a kill in the same
#      pipeline). Only a PID the script itself recorded may be signalled,
#      and only after its live /proc/<pid>/cmdline still carries that
#      config.
#   2b. Marker semantics: a line may carry the `daemon-cleanup-ok` marker
#      when that line needs an exemption — a daemon start whose cleanup is
#      owned elsewhere, a banned-pattern definition (an assignment holding
#      the very words the audit bans), or a recorded-PID line. A marker on a
#      line that needs no exemption is itself a violation, and a marker never
#      authorizes ending a process by name on the line it sits on.
#   3. A Go test file that runs the binary as a daemon (exec.Command or
#      exec.CommandContext whose arguments include "start" and "--foreground")
#      must register t.Cleanup in the same file.
#   4. A shell file that records a daemon or helper PID through the daemon
#      library (td_start / td_track_pid) must make the shared traps active —
#      it calls td_install_traps, or goes through story_daemon_lifecycle
#      (which does), or owns EXIT+INT+TERM traps itself. Recording without
#      installed traps is inert: nothing ends the pid on any exit path.
#      Sourced helper libraries named lib.sh are exempt: their callers
#      install.
#   5. A function passed to a TD_*_CLEANUP hook must not capture
#      `local rc=$?` to exit with: the hook runs in a subshell of the shared
#      handler, and the handler must always finish (state wipe included)
#      with the script's own status available as TD_EXIT_STATUS.
#   6. The exit code of a job a script waits on is its verdict and the
#      script's own exit status: it must not be collapsed into 0/1
#      bookkeeping around `wait <pid>` with no `rc=$?` anywhere in the file.
# A line may carry the `daemon-cleanup-ok` marker with a comment saying why it
# is safe (an audit regular expression, or a long-lived sandbox start that
# has a maximum lifetime and a wipe command); comment-only lines are
# skipped. The marker's scope is rule 2b: it exempts the line it sits on
# only when that line needs an exemption, and it never authorizes ending
# a process by name. The checker only reads files; it never signals any
# process and never touches a multiplexer server.
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
R_KILLWORD='(^|[^[:alnum:]_-])kill([^[:alnum:]_-]|$)' # daemon-cleanup-ok: a bare kill word as a pattern, the PID side of kill-by-lookup; a definition, never a call
R_PG_ANY='(^|[^[:alnum:]_-])pgrep([^[:alnum:]_-]|$)' # daemon-cleanup-ok: the pgrep(1) word for the kill-by-lookup rule
R_PS_WORD='(^|[^[:alnum:]_-])ps([^[:alnum:]_-]|$)' # daemon-cleanup-ok: the ps(1) word for the pipeline-kill rule
R_GREP_ANY='(^|[^[:alnum:]_-])grep([^[:alnum:]_-]|$)' # daemon-cleanup-ok: the grep word for the pipeline-kill rule
R_ASSIGN='^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*|export[[:space:]]+[A-Za-z_][A-Za-z0-9_]*)=' # daemon-cleanup-ok: an assignment — a pattern definition
R_KILLSESSION='kill-session'
R_SESSION_GREP='grep([[:space:]]+-[A-Za-z]+)*[[:space:]]+.{0,8}\^'
R_START_WORD='(^|[^[:alnum:]_.-])start([^[:alnum:]_]|$)'
R_TD_CALL='(^|[^[:alnum:]_.])td_start([[:space:]]|$|\()'
R_TD_TRACK='(^|[^[:alnum:]_.])td_track_pid([[:space:]]|$|\()'
R_TD_INSTALL='(^|[^[:alnum:]_.])td_install_traps([[:space:]]|$|\()'
R_STORY_LC='(^|[^[:alnum:]_.])story_daemon_lifecycle([[:space:]]|$)'
R_TDHOOK='TD_(PRE|EXTRA)_CLEANUP='
R_LOCALRC='local[[:space:]]+rc[[:space:]]*=[[:space:]]*\$\?'
R_WAITJOB='wait[[:space:]]+[^[:space:]]*\$[A-Za-z]'
R_RC0A='[^[:alnum:]_]rc=[[:space:]]*0([[:space:]]|;|\}|\)|#|$)'
R_RC0B='^[[:space:]]*rc=[[:space:]]*0$'
R_RC1A='[^[:alnum:]_]rc=[[:space:]]*1([[:space:]]|;|\}|\)|#|$)'
R_RC1B='^[[:space:]]*rc=[[:space:]]*1$'
R_RCREAL='rc[[:space:]]*=[[:space:]]*\$\?'
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
    local i text marked is_assign exempt needed
    local daemon_line="" td_helper=0
    local td_install=0 story_lc=0 td_hook=0 localrc=0
    local waitjob=0 rc0=0 rc1=0 rcreal=0
    local any_trap=0 trap_exit=0 trap_int=0 trap_term=0

    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        if [[ "$text" =~ ^[[:space:]]*# ]]; then
            continue
        fi
        marked=0; is_assign=0; exempt=0; needed=0
        if [[ "$text" == *"$MARKER"* ]]; then
            marked=1
        fi
        if [[ "$text" =~ $R_ASSIGN ]]; then
            is_assign=1
        fi
        # A marker + a banned-pattern assignment is a pattern definition
        # (this checker's own regexes, a tooling definition): exempt. On any
        # other line the marker never authorizes an end-by-name: it is not
        # a blanket line skip, and where the line needs no exemption at all
        # a bare marker is itself a violation (checked after the line's own
        # pattern classification below).
        if [[ "$marked" -eq 1 && "$is_assign" -eq 1 ]]; then
            exempt=1
        fi
        if [[ "$exempt" -eq 1 ]]; then
            needed=1
        elif [[ "$text" =~ $R_PKILL || "$text" =~ $R_KILLALL ]]; then
            if [[ "$marked" -eq 1 ]]; then
                report "$file:$((i + 1)): ends processes by name on a line carrying the daemon-cleanup-ok marker — the marker does not authorize ending a process by name (it exempts daemon starts and pattern definitions)"
            else
                report "$file:$((i + 1)): ends processes by name instead of signalling only a PID it recorded"
            fi
            continue
        elif [[ "$text" =~ $R_PGREP_F ]]; then
            if [[ "$marked" -eq 1 ]]; then
                report "$file:$((i + 1)): looks up processes by name to signal them, on a line carrying the marker — the marker does not authorize a name lookup"
            else
                report "$file:$((i + 1)): looks up processes by name to signal them"
            fi
            continue
        elif [[ "$text" =~ $R_XARGS_KILL ]]; then
            if [[ "$marked" -eq 1 ]]; then
                report "$file:$((i + 1)): signals processes found by a name lookup, on a line carrying the marker — the marker does not authorize that"
            else
                report "$file:$((i + 1)): signals processes found by a name lookup"
            fi
            continue
        elif [[ "$text" =~ $R_KILLSESSION && "$text" =~ $R_SESSION_GREP ]]; then
            if [[ "$marked" -eq 1 ]]; then
                report "$file:$((i + 1)): kills a tmux session whose name comes from a name-prefix search, on a line carrying the marker — the marker does not authorize that"
            else
                report "$file:$((i + 1)): kills a tmux session whose name comes from a name-prefix search"
            fi
            continue
        fi
        # A kill whose PID came from a name search: `kill $(pgrep ...)` in
        # any flag form, or a ps|…|grep pipeline feeding a kill in the same
        # line. A bare recorded-PID kill (kill -n "$pid") never matches.
        if [[ "$text" =~ $R_KILLWORD ]]; then
            needed=1
            if [[ "$exempt" -eq 0 && "$text" =~ $R_PG_ANY ]]; then
                if [[ "$marked" -eq 1 ]]; then
                    report "$file:$((i + 1)): signals a PID found by a name search (pgrep), on a line carrying the marker — the marker does not authorize that"
                else
                    report "$file:$((i + 1)): signals a PID found by a name search (pgrep) instead of signalling only the PID it recorded"
                fi
                continue
            fi
            if [[ "$exempt" -eq 0 && "$text" =~ $R_PS_WORD && "$text" =~ $R_GREP_ANY && "$text" == *"|"* ]]; then
                if [[ "$marked" -eq 1 ]]; then
                    report "$file:$((i + 1)): kills PIDs found by a ps/grep pipeline, on a line carrying the marker — the marker does not authorize that"
                else
                    report "$file:$((i + 1)): kills PIDs found by a ps/grep pipeline instead of signalling only the PID it recorded"
                fi
                continue
            fi
        fi
        if [[ "$text" =~ $R_START_WORD && "$text" == *--foreground* ]]; then
            needed=1
            if [[ "$marked" -eq 0 && -z "$daemon_line" ]]; then
                daemon_line="$((i + 1))"
            fi
        fi
        if [[ "$marked" -eq 1 && "$needed" -eq 0 ]]; then
            report "$file:$((i + 1)): carries the marker on a line that needs no exemption — a bare marker is itself a violation (the marker must sit on a daemon start, a banned-pattern definition, or a recorded-PID line it authorizes)"
        fi
        if [[ "$text" =~ $R_TD_CALL || "$text" =~ $R_TD_TRACK ]]; then
            td_helper=1
        fi
        if [[ "$text" =~ $R_TD_INSTALL ]]; then
            td_install=1
        fi
        if [[ "$text" =~ $R_STORY_LC ]]; then
            story_lc=1
        fi
        if [[ "$text" =~ $R_TDHOOK ]]; then
            td_hook=1
        fi
        if [[ "$text" =~ $R_LOCALRC ]]; then
            localrc=1
        fi
        if [[ "$text" =~ $R_WAITJOB ]]; then
            waitjob=1
        fi
        if [[ "$text" =~ $R_RC0A || "$text" =~ $R_RC0B ]]; then
            rc0=1
        fi
        if [[ "$text" =~ $R_RC1A || "$text" =~ $R_RC1B ]]; then
            rc1=1
        fi
        if [[ "$text" =~ $R_RCREAL ]]; then
            rcreal=1
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
    # Rule 4: recording a PID through the daemon library is inert unless the
    # shared traps were installed (or the story helper / own traps do the
    # job); otherwise nothing ends the pid on any exit path.
    if [[ "$td_helper" -eq 1 && "$td_install" -eq 0 && "$story_lc" -eq 0 ]] \
        && [[ "$(basename "$file")" != "lib.sh" ]] \
        && { [[ "$trap_exit" -eq 0 ]] || [[ "$trap_int" -eq 0 ]] || [[ "$trap_term" -eq 0 ]]; }; then
        report "$file: records a daemon or helper PID via the daemon library but the shared traps are never installed (td_install_traps, story_daemon_lifecycle, or own EXIT+INT+TERM traps) — the record is inert and the pid survives every exit path"
    fi

    # Rule 5: TD_*_CLEANUP hooks run in a subshell of the shared handler; a
    # hook that captures `local rc=$?` to exit with cuts the handler off
    # (state wipe included) and can change the script's own exit status.
    if [[ "$td_hook" -eq 1 && "$localrc" -eq 1 ]]; then
        report "$file: passes a cleanup function to a TD_*_CLEANUP hook that captures local rc=\$? to exit with — the hook must return; the shared handler owns the exit status (the original is available to the hook as TD_EXIT_STATUS)"
    fi

    # Rule 6: the exit code of a job a script waits on is its verdict; it
    # must not be collapsed into 0/1 bookkeeping.
    if [[ "$waitjob" -eq 1 && "$rc0" -eq 1 && "$rc1" -eq 1 && "$rcreal" -eq 0 ]]; then
        report "$file: collapses the exit code of a job it waits on into 0/1 bookkeeping instead of capturing the real one (wait <job> … || rc=\$?) and exiting with it"
    fi
    return 0
}

# audit_go <file> — rule 3 over one Go test file.
audit_go() {
    local file="$1"
    local -a lines=()
    mapfile -t lines < "$file"
    local n=${#lines[@]}
    local i k text win bt has_cleanup=0 marked daemon_shape inraw=0

    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        # A backtick-delimited raw-string literal (a test's fixture body) carries
        # marker, start and cleanup text as data, not code: an odd number of
        # backticks on a line opens or closes one.
        bt="${text//[^\`]/}"
        if (( ${#bt} % 2 )); then
            inraw=$((1 - inraw))
            continue
        fi
        if (( inraw )); then
            continue
        fi
        if [[ "$text" =~ ^[[:space:]]*(//|/\*) || "$text" =~ ^[[:space:]]*\* ]]; then
            continue
        fi
        if [[ "$text" == *"t.Cleanup"* ]]; then
            has_cleanup=1
        fi
    done

    inraw=0
    for ((i = 0; i < n; i++)); do
        text="${lines[$i]}"
        bt="${text//[^\`]/}"
        if (( ${#bt} % 2 )); then
            inraw=$((1 - inraw))
            continue
        fi
        if (( inraw )); then
            continue
        fi
        if [[ "$text" =~ ^[[:space:]]*(//|/\*) || "$text" =~ ^[[:space:]]*\* ]]; then
            continue
        fi
        marked=0; daemon_shape=0
        if [[ "$text" == *"$MARKER"* ]]; then
            marked=1
        fi
        if [[ "$text" =~ $R_GOCMD ]]; then
            # The argument list may continue on the following lines.
            win="$text"
            for ((k = i + 1; k < n && k <= i + 6; k++)); do
                win="$win ${lines[$k]}"
            done
            if [[ "$win" == *'"start"'* ]] && [[ "$win" == *--foreground* ]]; then
                daemon_shape=1
            fi
        fi
        if [[ "$daemon_shape" -eq 1 ]]; then
            if [[ "$has_cleanup" -eq 0 ]]; then
                report "$file:$((i + 1)): starts a datawatch daemon without registering a t.Cleanup that ends it"
            fi
            continue
        fi
        if [[ "$marked" -eq 1 ]]; then
            report "$file:$((i + 1)): carries the marker on a line that needs no exemption — a bare marker is itself a violation (it must sit on a daemon start, a banned-pattern definition, or a recorded-PID line it authorizes)"
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
    echo "    shell: recording a PID without td_install_traps is a no-op — install the"
    echo "           traps for the record to mean anything"
    echo "    shell: TD_*_CLEANUP hooks run in a subshell and must return — the shared"
    echo "           handler owns the exit status (TD_EXIT_STATUS carries the original)"
    echo "    shell: or add your own trap naming EXIT, INT and TERM, signalling only a PID"
    echo "           the script recorded while /proc/<pid>/cmdline still carries its marker"
    echo "    shell: never end processes by name, never stop a multiplexer server"
    echo "    shell: don't collapse a waited job's exit code into 0/1 — the script's"
    echo "           verdict and exit status are the job's own code"
    echo "    go:    register t.Cleanup in the test that starts the daemon"
    echo
    echo "FAIL: $ERRORS daemon cleanup violation(s)"
    exit 1
fi
echo "OK: no daemon cleanup violations"
