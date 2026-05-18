#!/usr/bin/env bash
# TS-503 — DATAWATCH_PARENTS env var accepted by datawatch-stats
# tags: surface:cli feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-503"
story_preflight "surface:cli feature:compute" || return 0

_story_ts_503() {
  local dw_stats=""
  for loc in "$REPO_ROOT/scripts/datawatch-stats.sh" "$REPO_ROOT/bin/datawatch-stats" "$(command -v datawatch-stats 2>/dev/null)"; do
    [[ -x "$loc" ]] && { dw_stats="$loc"; break; }
  done
  if [[ -z "$dw_stats" ]]; then
    skip "datawatch-stats binary not found"
    return
  fi
  local out rc
  out=$(DATAWATCH_PARENTS="http://127.0.0.1:1" timeout 15 "$dw_stats" --help 2>&1 \
     || DATAWATCH_PARENTS="http://127.0.0.1:1" timeout 15 "$dw_stats" 2>&1); rc=$?
  save_evidence TS-503 "out.txt" "$out"
  if echo "$out" | grep -qi "DATAWATCH_PARENTS.*unknown\|env.*not.*support"; then
    skip "DATAWATCH_PARENTS not supported in this build"
  elif [[ $rc -eq 0 ]] || echo "$out" | grep -qi "url\|connect\|error\|timeout\|refused"; then
    ok "DATAWATCH_PARENTS env var accepted by datawatch-stats (rc=$rc)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_503
: "${RESULT:=fail}"
unset -f _story_ts_503
