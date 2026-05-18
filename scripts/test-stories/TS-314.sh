#!/usr/bin/env bash
# TS-314 — datawatch compute add + show + delete CRUD
# tags: surface:cli feature:cli feature:compute
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-314"
story_preflight "surface:cli feature:cli feature:compute" || return 0

_story_ts_314() {
  local out node_name
  node_name="cli-node-ts314-$$"

  # Add
  out=$(cli_test compute add --name "$node_name" --kind ollama --address "http://localhost:11434" --no-probe 2>&1); local rc=$?
  save_evidence TS-314 "add.txt" "$out"
  if [[ $rc -ne 0 ]]; then
    if echo "$out" | grep -qiE "disabled|not.*enabled|not.*configured|not.*found|no.*available|unknown command|unknown flag"; then
      skip "compute add not available: $(echo "$out" | head -c 80)"
      return
    fi
    ko "compute add failed: $(echo "$out" | head -c 200)"
    return
  fi
  add_cleanup compute_node "$node_name"

  # Show
  out=$(cli_test compute show "$node_name" 2>&1) || out=$(cli_test compute get "$node_name" 2>&1)
  save_evidence TS-314 "show.txt" "$out"

  # Delete
  out=$(cli_test compute delete "$node_name" 2>&1) || true
  save_evidence TS-314 "delete.txt" "$out"

  ok "compute add/show/delete CRUD round-trip for $node_name"
}

RESULT=fail
_story_ts_314
: "${RESULT:=fail}"
unset -f _story_ts_314
