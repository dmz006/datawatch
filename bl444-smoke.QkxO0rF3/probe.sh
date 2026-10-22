#!/usr/bin/env bash
S=/home/dmz/.datawatch/prd-worktrees/10110001/bl444-smoke.QkxO0rF3
"$S/tools/holder" 39945 4s > "$S/holder.log" 2>&1 &
HOLDER=$!
sleep 0.3
"$S/dw" start --foreground --config "$S/config.yaml" > "$S/daemon.log" 2>&1 &
DPID=$!
echo "t0 holder=$HOLDER daemon=$DPID"
for i in 1 2 3 4 5 6 7 8; do
  sleep 1
  if kill -0 "$DPID" 2>/dev/null; then st=alive; else st=DEAD; fi
  echo "t$i daemon=$st holder=$(kill -0 $HOLDER 2>/dev/null && echo alive || echo gone) loglines=$(wc -l < "$S/daemon.log" 2>/dev/null)"
  [ "$st" = DEAD ] && break
done
echo "--- daemon.log:"; cat "$S/daemon.log"
exit 0
