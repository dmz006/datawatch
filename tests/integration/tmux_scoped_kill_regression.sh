#!/usr/bin/env bash
# Regression: the worker tmux kill guard shim (autonomous.tmux_kill_guard).
#
# The shim is the `tmux` a worker pane finds first on PATH. It must refuse a
# server-wide kill that is not scoped to an explicit throwaway socket, and let
# scoped kills and every other tmux command through unchanged.
#
# Safety: no real tmux is ever run. The "real" tmux behind the shim is a stub
# that only records its arguments, so a missing or broken guard cannot kill
# any server. Run: env -u TMUX -u TMUX_TMPDIR bash tests/integration/tmux_scoped_kill_regression.sh

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SHIM_SRC="$ROOT/internal/session/tmux_guard_shim.sh"
[[ -f "$SHIM_SRC" ]] || { echo "FAIL shim source missing: $SHIM_SRC" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

mkdir -p "$WORK/guard" "$WORK/real" "$WORK/prod"
cp "$SHIM_SRC" "$WORK/guard/tmux"
chmod 755 "$WORK/guard/tmux"
CALLS="$WORK/calls.log"
cat >"$WORK/real/tmux" <<STUB
#!/bin/sh
printf '%s\n' "\$*" >> "$CALLS"
exit 0
STUB
chmod 755 "$WORK/real/tmux"

FAILS=0
fail() { echo "  FAIL $1" >&2; FAILS=$((FAILS + 1)); }

# run_shim <args...>: runs the shim with the stub behind it; sets RC and ERR.
run_shim() {
    : >"$CALLS"
    set +e
    ERR="$(PATH="$WORK/guard:$WORK/real:/usr/bin:/bin" DATAWATCH_TMUX_GUARD_PROTECTED="$WORK/prod" "$WORK/guard/tmux" "$@" 2>&1 >/dev/null)"
    RC=$?
    set -e
    CALLED="$(cat "$CALLS")"
}

expect_refused() {
    local label="$1"; shift
    run_shim "$@"
    [[ $RC -ne 0 ]] || fail "$label: exit 0, want refusal"
    [[ -z "$CALLED" ]] || fail "$label: reached the real tmux ($CALLED)"
    [[ "$ERR" == *"tmux kill guard"* ]] || fail "$label: no guard message ($ERR)"
}

expect_passed() {
    local label="$1"; shift
    run_shim "$@"
    [[ $RC -eq 0 ]] || fail "$label: exit $RC, want 0 ($ERR)"
    [[ "$CALLED" == "$*" ]] || fail "$label: real tmux got '$CALLED', want '$*'"
}

# Unscoped server-wide kills are refused.
expect_refused "bare kill-server" kill-server
expect_refused "kill-server with -L default" -L default kill-server
expect_refused "kill-server with a -S under the protected socket dir" -S "$WORK/prod/tmux-1/default" kill-server
expect_refused "kill-session -a" kill-session -a
expect_refused "kill-session -a -t keep" kill-session -a -t keep
expect_refused "chained kill-server" kill-session -t x \; kill-server
# Scoped kills and everything else pass through untouched.
expect_passed "scoped kill-server" -S "$WORK/throwaway.sock" kill-server
expect_passed "named-socket kill-server" -L throwaway kill-server
expect_passed "scoped kill-session -a" -S "$WORK/throwaway.sock" kill-session -a
expect_passed "kill-session by target" kill-session -t one
expect_passed "list-sessions" list-sessions
expect_passed "send-keys text mentioning kill-server" send-keys -t w "echo kill-server" Enter

if ((FAILS > 0)); then
    echo "tmux_scoped_kill_regression: $FAILS failure(s)" >&2
    exit 1
fi
echo "tmux_scoped_kill_regression: ok"
