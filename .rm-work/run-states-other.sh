#!/usr/bin/env bash
cd /home/dmz/.datawatch/prd-worktrees/d0210001 || exit 2
export RM_TMP_BASE="$PWD/.rm-work"
for s in waiting-capacity waiting-external final-gate git-operation automation-branches scheduled-due websocket-clients; do
  echo "[$(date '+%H:%M:%S')] start $s"
  env -u TMUX -u TMUX_TMPDIR bash scripts/restart-matrix.sh --only "$s" >".rm-work/run-logs/$s.log" 2>&1
  echo "$s rc=$?" >>".rm-work/run-logs/runs.txt"
  echo "[$(date '+%H:%M:%S')] done $s"
done
echo ALL_DONE >>".rm-work/run-logs/runs.txt"
