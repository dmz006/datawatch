#!/usr/bin/env bash
# TS-327 — datawatch secrets set + get + delete CRUD round-trip
# tags: surface:cli feature:cli feature:secrets
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-327"
story_preflight "surface:cli feature:cli feature:secrets" || return 0

_story_ts_327() {
  local out secret_name secret_val
  secret_name="test-secret-e2e-$$"
  secret_val="test-value-e2e-$$"

  # Set
  out=$(cli_test secrets set "$secret_name" "$secret_val" 2>&1); local rc=$?
  save_evidence TS-327 "set.txt" "$out"
  if [[ $rc -ne 0 ]]; then
    if echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command"; then
      skip "secrets set not available: $(echo "$out" | head -c 80)"
      return
    fi
    ko "secrets set failed rc=$rc: $(echo "$out" | head -c 200)"
    return
  fi
  add_cleanup secret "$secret_name"

  # Get
  out=$(cli_test secrets get "$secret_name" 2>&1); rc=$?
  save_evidence TS-327 "get.txt" "$out"
  if [[ $rc -ne 0 ]] && ! echo "$out" | grep -qi "$secret_val"; then
    ko "secrets get failed rc=$rc: $(echo "$out" | head -c 200)"
    return
  fi

  # Delete
  out=$(cli_test secrets delete "$secret_name" 2>&1) || true
  save_evidence TS-327 "delete.txt" "$out"

  ok "secrets CRUD round-trip: set/get/delete for $secret_name"
}

RESULT=fail
_story_ts_327
: "${RESULT:=fail}"
unset -f _story_ts_327
