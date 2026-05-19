#!/usr/bin/env bash
# TS-630 — datawatch alert-rules add exits 0
# tags: surface:cli feature:alert-rules
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-630"
story_preflight "surface:cli feature:alert-rules" || return 0

_story_ts_630() {
  local out rc
  out=$(cli_test alert-rules add test-cpu-rule --metric cpu_pct --operator '>' --threshold 90 --action alert 2>&1); rc=$?
  save_evidence TS-630 "out.txt" "$out"
  if echo "$out" | grep -qiE "unknown command|unknown flag|no such|help.*alert"; then
    skip "alert-rules add CLI not available in this build"
    return
  fi
  if [[ $rc -eq 0 ]]; then
    ok "datawatch alert-rules add exits 0"
    # cleanup
    cli_test alert-rules delete test-cpu-rule >/dev/null 2>&1 || \
      api DELETE /api/alert-rules/test-cpu-rule >/dev/null 2>&1 || true
  else
    ko "datawatch alert-rules add failed (rc=$rc): $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_630
: "${RESULT:=fail}"
unset -f _story_ts_630
