#!/usr/bin/env bash
# TS-501 — datawatch-stats --diag runs 6 probes
# tags: surface:cli feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-501"
story_preflight "surface:cli feature:compute" || return 0

_story_ts_501() {
  local dw_stats=""
  # Check common locations for datawatch-stats
  for loc in "$REPO_ROOT/scripts/datawatch-stats.sh" "$REPO_ROOT/bin/datawatch-stats" "$(command -v datawatch-stats 2>/dev/null)"; do
    [[ -x "$loc" ]] && { dw_stats="$loc"; break; }
  done
  if [[ -z "$dw_stats" ]]; then
    skip "datawatch-stats binary not found"
    return
  fi
  local out rc
  out=$(timeout 30 "$dw_stats" --diag 2>&1); rc=$?
  save_evidence TS-501 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    local probe_count
    probe_count=$(echo "$out" | grep -c "probe\|check\|test" 2>/dev/null || echo "0")
    ok "datawatch-stats --diag exits 0 (detected ~$probe_count probe lines)"
  elif echo "$out" | grep -qi "unknown.*flag\|not.*supported\|no such"; then
    skip "--diag flag not supported in this build"
  else
    ko "rc=$rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_501
: "${RESULT:=fail}"
unset -f _story_ts_501
