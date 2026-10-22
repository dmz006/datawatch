#!/bin/bash
# Reproduce BL550 flake: many parallel test processes + CPU load.
cd /home/dmz/.datawatch/prd-worktrees/efed0001
export TMUX= TMUX_TMPDIR=
NPROC=${1:-16}
RUNS=${2:-15}
BIN=$(pwd)/.tmp-bl550/test.bin6
OUT=$(pwd)/.tmp-bl550/stress-logs
rm -rf "$OUT" && mkdir -p "$OUT"
# CPU load generators
LOAD=$(( NPROC * 2 ))
for i in $(seq 1 "$LOAD"); do
  ( while :; do :; done ) &
  LOADPID=$!
  echo $LOADPID > "$OUT/load-$i.pid"
done
trap 'for f in "$OUT"/load-*.pid; do [ -f "$f" ] && kill -9 $(cat "$f") 2>/dev/null; done; pkill -9 -P $$ 2>/dev/null' EXIT
pids=()
for i in $(seq 1 "$NPROC"); do
  ( GOMAXPROCS=2 GOGC=20 env -u TMUX -u TMUX_TMPDIR "$BIN" \
      -test.run WSOutputScope_UnsubscribeStops -test.count="$RUNS" -test.v -test.timeout 300s ) \
      > "$OUT/run-$i.log" 2>&1 &
  pids+=($!)
done
fails=0
for p in "${pids[@]}"; do
  wait "$p" || fails=$((fails+1))
done
echo "FAILS=$fails / $NPROC"
grep -l "FAIL\|panic\|race" "$OUT"/run-*.log 2>/dev/null | head
for f in $(grep -l "FAIL" "$OUT"/run-*.log 2>/dev/null | head -3); do
  echo "=== $f ==="; grep -B4 -A20 "^--- FAIL\|FAIL:" "$f" | head -60
done
