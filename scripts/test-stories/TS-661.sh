#!/usr/bin/env bash
# TS-661 — datawatch config set vision.backend openai exits 0; GET confirms
# tags: surface:cli feature:vision group:vision parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-661"
story_preflight "surface:cli feature:vision group:vision parallel:ok" || return 0

_story_ts_661() {
  local out rc
  out=$(cli_test config set vision.backend openai 2>&1); rc=$?
  save_evidence TS-661 "cli_set.txt" "$out"

  if [[ $rc -ne 0 ]]; then
    ko "datawatch config set vision.backend openai failed (rc=$rc): $(echo "$out" | head -c 200)"
    return
  fi

  local get_resp
  get_resp=$(api GET /api/config)
  if ! echo "$get_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['vision']['backend']=='openai'" 2>/dev/null; then
    # Restore and fail
    api PUT /api/config '{"vision.backend":"ollama"}' >/dev/null 2>&1 || true
    ko "GET /api/config vision.backend was not 'openai' after CLI set"
    return
  fi

  # Restore
  cli_test config set vision.backend ollama >/dev/null 2>&1 || true
  ok "datawatch config set vision.backend openai exits 0 and GET confirms value"
}

RESULT=fail
_story_ts_661
: "${RESULT:=fail}"
unset -f _story_ts_661
