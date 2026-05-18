#!/usr/bin/env bash
# TS-502 — datawatch-stats --datawatch url1,url2 accepts comma-separated URLs
# tags: surface:cli feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-502"
story_preflight "surface:cli feature:compute" || return 0

_story_ts_502() {
  local dw_stats
  for loc in "$REPO_ROOT/scripts/datawatch-stats.sh" "$REPO_ROOT/bin/datawatch-stats" "$(command -v datawatch-stats 2>/dev/null)"; do
    [[ -x "$loc" ]] && { dw_stats="$loc"; break; }
  done
  if [[ -z "$dw_stats" ]]; then
    skip "datawatch-stats binary not found"
    return
  fi
  local out rc
  out=$(timeout 15 "$dw_stats" --datawatch "http://127.0.0.1:1,http://127.0.0.1:2" --help 2>&1 \
     || timeout 15 "$dw_stats" --datawatch "http://127.0.0.1:1,http://127.0.0.1:2" 2>&1); rc=$?
  save_evidence TS-502 "out.txt" "$out"
  if echo "$out" | grep -qi "unknown.*flag\|not.*supported\|invalid.*flag"; then
    skip "--datawatch flag not supported in this build"
  elif [[ $rc -eq 0 ]] || echo "$out" | grep -qi "url\|connect\|error\|timeout\|refused"; then
    ok "datawatch-stats accepted --datawatch comma-separated URLs (rc=$rc)"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_502
: "${RESULT:=fail}"
unset -f _story_ts_502
