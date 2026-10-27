#!/usr/bin/env bash
# scratch smoke: driver semantics for daemon cleanup modes (not a deliverable)
set -u
ROOT=/home/dmz/.datawatch/prd-worktrees/18e70001
LIB="$ROOT/scripts/test-daemon-lib.sh"
FIX="${SMOKE_FIX:?}"
export TD_STATE_DIR="$FIX/state"
mkdir -p "$TD_STATE_DIR"
CFG="$FIX/config.yaml"
touch "$CFG"
source "$LIB"
td_install_traps
mode="${SMOKE_MODE:?}"

bash -c "exec -a 'datawatch start --foreground --config $CFG' sleep 300" &
DPID=$!
td_track_pid "$DPID" "$CFG"
bash -c "exec -a 'datawatch start --foreground --config $CFG' sleep 300" &
TWIN=$!
echo "DAEMON_PID=$DPID TWIN=$TWIN"
echo "READY"
case "$mode" in
  normal) exit 0 ;;
  fail) exit 3 ;;
  wait) wait "$DPID" ;;
esac
