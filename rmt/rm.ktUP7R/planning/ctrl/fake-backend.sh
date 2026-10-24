#!/usr/bin/env bash
task="${1:-}"
dir="${2:-.}"
ctrl="/home/dmz/.datawatch/prd-worktrees/d0210001/rmt/rm.ktUP7R/planning/ctrl"
num() { local v; v="$(cat "$ctrl/$1" 2>/dev/null)"; printf '%s' "${v:-$2}"; }
out="$(printf '%s' "$task" | grep -oE '[^[:space:]]*\.(decompose|verify|guardrail)-output-[0-9a-f]+\.json' | tail -1)"
case "$task" in
  *decompose-output*) role=planner ;;
  *verify-output*)    role=verifier ;;
  *guardrail-output*) role=guardrail ;;
  *)                  role=worker ;;
esac
echo "$(date +%s) pid=$$" >>"$ctrl/$role.starts"
case "$role" in
  planner)
    sleep "$(num planner_delay 0)"
    [[ -n "$out" ]] && cp "$ctrl/plan.json" "$out"
    ;;
  verifier)
    sleep "$(num verifier_delay 0)"
    [[ -n "$out" ]] && printf '%s\n' '{"ok":true,"severity":"info","summary":"fake verifier accepts"}' >"$out"
    echo "$(date +%s) done" >>"$ctrl/verifier.done"
    ;;
  guardrail)
    [[ -n "$out" ]] && printf '%s\n' '{"guardrail":"rules","outcome":"pass","summary":"fake guardrail accepts"}' >"$out"
    ;;
  worker)
    ticks="$(num worker_ticks 6)"
    : >"$dir/worker-counter.txt"
    for i in $(seq 1 "$ticks"); do
      echo "tick $i" >>"$dir/worker-counter.txt"
      echo "worker tick $i"
      sleep 1
    done
    echo "work for this task" >"$dir/worker-result.txt"
    echo "$(date +%s) done" >>"$ctrl/worker.done"
    ;;
esac
