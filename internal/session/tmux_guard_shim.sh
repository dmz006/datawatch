#!/bin/bash
# datawatch tmux kill guard (autonomous.tmux_kill_guard, default on).
#
# Installed by the daemon as "tmux" in a private directory that a worker pane
# puts first on PATH, so it covers every backend that runs in a pane (claude,
# pi, opencode, ...). It refuses a server-wide kill that is not scoped to an
# explicit throwaway socket, and hands every other call to the real tmux.
#
# Refused: the server kill command, and the session kill command with -a,
# unless the call names a socket with -S <path> (not under the protected
# directory) or -L <name> (not "default"). VAR=value prefixes such as
# TMUX_TMPDIR do not count as scoping: tmux prefers TMUX over TMUX_TMPDIR.
#
# This stops the accidental and the careless call. It is not a sandbox: a
# caller that runs the real tmux binary by its absolute path is not stopped.

self_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
protected="${DATAWATCH_TMUX_GUARD_PROTECTED:-}"

# Find the real tmux: the first "tmux" on PATH that is not this directory.
real=""
IFS=: read -r -a path_entries <<<"$PATH"
for entry in "${path_entries[@]}"; do
    [[ -n "$entry" ]] || continue
    dir="$(cd "$entry" 2>/dev/null && pwd)" || continue
    [[ "$dir" == "$self_dir" ]] && continue
    if [[ -x "$dir/tmux" && ! -d "$dir/tmux" ]]; then
        real="$dir/tmux"
        break
    fi
done
if [[ -z "$real" ]]; then
    echo "tmux kill guard: no real tmux found on PATH" >&2
    exit 127
fi

refuse() {
    echo "tmux kill guard: refused '$*': a server-wide kill must name a throwaway socket with -S <path> or -L <name>. Plain tmux here can reach the operator's tmux server." >&2
    exit 1
}

# Global options: -S and -L take a value; -c -f -T take a value we skip.
socket_path=""
socket_name=""
args=("$@")
i=0
while ((i < ${#args[@]})); do
    a="${args[$i]}"
    case "$a" in
    -S) socket_path="${args[$((i + 1))]:-}"; i=$((i + 2)) ;;
    -L) socket_name="${args[$((i + 1))]:-}"; i=$((i + 2)) ;;
    -c | -f | -T) i=$((i + 2)) ;;
    -S?*) socket_path="${a#-S}"; i=$((i + 1)) ;;
    -L?*) socket_name="${a#-L}"; i=$((i + 1)) ;;
    -*) i=$((i + 1)) ;;
    *) break ;;
    esac
done

scoped=0
if [[ -n "$socket_path" ]]; then
    scoped=1
    if [[ -n "$protected" ]]; then
        resolved="$(realpath -m -- "$socket_path" 2>/dev/null || echo "$socket_path")"
        prot="$(realpath -m -- "$protected" 2>/dev/null || echo "$protected")"
        if [[ "$resolved" == "$prot" || "$resolved" == "$prot"/* ]]; then
            scoped=0
        fi
    fi
elif [[ -n "$socket_name" && "$socket_name" != "default" ]]; then
    scoped=1
fi

# Scan every argument from the command on (a call can chain commands with ';').
if ((scoped == 0)); then
    saw_session_kill=0
    for ((j = i; j < ${#args[@]}; j++)); do
        w="${args[$j]}"
        # tmux accepts an unambiguous prefix of a command name.
        if ((${#w} >= 8)) && [[ "kill-server" == "$w"* ]]; then
            refuse "$@"
        fi
        if ((${#w} >= 8)) && [[ "kill-session" == "$w"* ]]; then
            saw_session_kill=1
            continue
        fi
        if [[ "$w" == ";" || "$w" == '\;' ]]; then
            saw_session_kill=0
            continue
        fi
        if ((saw_session_kill == 1)) && [[ "$w" =~ ^-[A-Za-z]*a[A-Za-z]*$ ]]; then
            refuse "$@"
        fi
    done
fi

exec "$real" "$@"
