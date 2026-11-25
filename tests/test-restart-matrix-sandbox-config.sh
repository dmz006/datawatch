#!/usr/bin/env bash
# Tests scripts/restart-matrix/lib.sh: the sandbox config every restart-matrix state boots
# from, and the pid contract the harness holds its daemon to.
#
# The fault this guards: the generated config named the sandbox dir in
# server.allowed_project_roots only for the pi-node states. Every other state that creates
# an Automaton (worker-running, waiting-capacity, waiting-external, git-operation,
# automation-branches) has its project dir refused by the daemon, so the state never gets
# its work started.
#
#   1. config shape   plain and pi-node sandboxes both grant their own dir as a project root,
#                     once, inside the server block
#   2. pid contract   rm_verify_sandbox_pid still refuses a pid that is not this sandbox's
#                     datawatch (strict, never loosened)
# Nothing here starts a daemon.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIB="${RM_MATRIX_LIB:-$ROOT/scripts/restart-matrix/lib.sh}"
fails=0
ok()   { echo "ok   - $1"; }
bad()  { echo "FAIL - $1"; fails=$((fails+1)); }
skip() { echo "SKIP - $1"; }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }

[[ -f "$LIB" ]] || { echo "FAIL - $LIB not found"; exit 1; }
for t in jq curl python3 tmux git; do
  command -v "$t" >/dev/null 2>&1 || { skip "restart-matrix sandbox config: required tool missing: $t"; exit 0; }
done

# a short work root: the multiplexer socket path has a length limit
WORK="$(mktemp -d /tmp/rmt.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# --- 1. config shape ---------------------------------------------------------
roots_in_server_block() { # CONFIG — the allowed_project_roots lines inside the server: block
  awk '/^server:/{in_s=1; next} /^[^[:space:]]/{in_s=0} in_s && /^  allowed_project_roots:/' "$1"
}
shape() { # LABEL — runs in a subshell so one sandbox's variables never leak into the next
  (
    export RM_TMP_BASE="$WORK/$1"
    mkdir -p "$RM_TMP_BASE"
    # shellcheck source=../scripts/restart-matrix/lib.sh
    . "$LIB"
    rm_sandbox_new probe >/dev/null 2>&1 || { echo "sandbox-new-failed"; exit 0; }
    n="$(roots_in_server_block "$RM_CONFIG" | wc -l | tr -d ' ')"
    total="$(grep -c 'allowed_project_roots' "$RM_CONFIG")"
    has="$(roots_in_server_block "$RM_CONFIG" | grep -cF "\"$RM_SB\"")"
    echo "server_block=$n total=$total names_sandbox=$has"
  )
}
plain="$(unset RM_PI_NODES; shape plain)"
pi="$(RM_PI_NODES=1 shape pi)"
check "plain sandbox config grants its own dir as a project root, once, in the server block ($plain)" \
  '[[ "$plain" == "server_block=1 total=1 names_sandbox=1" ]]'
check "pi-node sandbox config grants its own dir as a project root, once, in the server block ($pi)" \
  '[[ "$pi" == "server_block=1 total=1 names_sandbox=1" ]]'

# --- 2. pid contract stays strict --------------------------------------------
contract="$(
  export RM_TMP_BASE="$WORK/contract"
  mkdir -p "$RM_TMP_BASE"
  # shellcheck source=../scripts/restart-matrix/lib.sh
  . "$LIB"
  rm_sandbox_new probe >/dev/null 2>&1 || { echo "sandbox-new-failed"; exit 0; }
  res=""
  # this shell is alive but is not a datawatch process carrying the sandbox config
  rm_verify_sandbox_pid "$$" && res="$res accepted-foreign-pid"
  rm_verify_sandbox_pid "" && res="$res accepted-empty"
  rm_verify_sandbox_pid "1" && res="$res accepted-init"
  rm_verify_sandbox_pid "not-a-pid" && res="$res accepted-garbage"
  # a datawatch-named process of ANOTHER config must be refused as well
  mkdir -p "$WORK/fake"
  printf '#!/usr/bin/env bash\nsleep 5\n' >"$WORK/fake/datawatch"
  chmod +x "$WORK/fake/datawatch"
  "$WORK/fake/datawatch" --config /nonexistent/other-config.yaml start --foreground >/dev/null 2>&1 &
  other=$!
  # ends only this recorded stand-in, on every way out of this subshell
  trap 'kill -TERM "$other" 2>/dev/null' EXIT INT TERM
  sleep 0.3
  rm_verify_sandbox_pid "$other" && res="$res accepted-other-config"
  kill -TERM "$other" 2>/dev/null
  wait "$other" 2>/dev/null
  trap - EXIT INT TERM
  echo "${res:-strict}"
)"
check "rm_verify_sandbox_pid refuses a foreign pid, an empty pid, init, garbage and another config ($contract)" \
  '[[ "$contract" == strict ]]'

echo
if [[ $fails -eq 0 ]]; then echo "all passed"; else echo "$fails failure(s)"; exit 1; fi
