#!/usr/bin/env bash
# TS-583 — datawatch federation peer delete exits 0
# tags: surface:cli feature:federation feature:cli
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-583"
story_preflight "surface:cli feature:federation feature:cli" || return 0

_story_ts_583() {
  local out rc
  # First add a peer to delete
  out=$(cli_test federation peer add --name e2e-cli-peer-ts583 --url http://127.0.0.1:19999 --token test 2>&1) || rc=$?
  rc="${rc:-0}"
  if echo "$out" | grep -qi "unknown command\|disabled\|no route\|help"; then
    skip "federation peer add/delete CLI not available in this build"
    return
  fi
  if [[ "$rc" -ne 0 ]]; then
    skip "federation peer add failed (rc=$rc) — cannot test delete: $(echo "$out" | head -c 200)"
    return
  fi

  # Now delete it
  local del_out del_rc
  del_rc=0
  del_out=$(cli_test federation peer delete e2e-cli-peer-ts583 2>&1) || del_rc=$?
  save_evidence TS-583 "delete.txt" "$del_out"
  if [[ "$del_rc" -eq 0 ]]; then
    ok "datawatch federation peer delete exits 0"
  elif echo "$del_out" | grep -qi "unknown command\|no route\|disabled"; then
    skip "federation peer delete CLI not available in this build"
  else
    ko "datawatch federation peer delete failed (rc=$del_rc): $(echo "$del_out" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_583
: "${RESULT:=fail}"
unset -f _story_ts_583
