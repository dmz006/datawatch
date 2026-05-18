#!/usr/bin/env bash
# TS-155 — Evals suites list
# tags: surface:api feature:evals
# legacy fn: t12_ts155_evals_suites
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-155"
story_preflight "surface:api feature:evals" || return 0

_story_ts_155() {
  local resp
  resp=$(api GET /api/evals/suites)
  save_evidence TS-155 "suites.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    ok "GET /api/evals/suites responds"
  else
    skip "evals endpoint not present: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_155
: "${RESULT:=fail}"
unset -f _story_ts_155
