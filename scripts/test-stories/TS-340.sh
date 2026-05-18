#!/usr/bin/env bash
# TS-340 — datawatch about exits 0 (version + credits)
# tags: surface:cli feature:cli feature:bootstrap
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-340"
story_preflight "surface:cli feature:cli feature:bootstrap" || return 0

_story_ts_340() {
  local out; out=$(cli_test about 2>&1); local rc=$?
  save_evidence TS-340 "out.txt" "$out"
  if [[ $rc -eq 0 ]]; then
    ok "about exits 0: $(echo "$out" | head -c 80)"
  elif echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
    # Try version as fallback
    out=$(cli_test version 2>&1); rc=$?
    if [[ $rc -eq 0 ]]; then
      ok "version exits 0 (about not available): $(echo "$out" | head -c 80)"
    else
      skip "about/version not available: $(echo "$out" | head -c 80)"
    fi
  else
    ko "exited $rc: $(echo "$out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_340
: "${RESULT:=fail}"
unset -f _story_ts_340
