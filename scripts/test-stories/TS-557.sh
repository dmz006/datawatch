#!/usr/bin/env bash
# TS-557 — release-smoke.sh exits 0
# tags: surface:meta
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-557"
story_preflight "surface:meta" || return 0

_story_ts_557() {
  local smoke_script="$REPO_ROOT/scripts/release-smoke.sh"
  if [[ ! -f "$smoke_script" ]]; then
    skip "release-smoke.sh not found at $smoke_script"
    return
  fi
  if [[ ! -f "$TEST_BINARY" ]]; then
    skip "datawatch binary not found at $TEST_BINARY"
    return
  fi
  local out
  # Run with the built binary exposed via DATAWATCH_BIN; generous 180s timeout.
  out=$(DATAWATCH_BIN="$TEST_BINARY" timeout 180 bash "$smoke_script" 2>&1) || {
    local rc=$?
    save_evidence TS-557 "smoke-output.txt" "$out"
    if [[ $rc -eq 124 ]]; then
      ko "release-smoke.sh timed out after 180s"
    else
      ko "release-smoke.sh exited with code $rc"
    fi
    return
  }
  save_evidence TS-557 "smoke-output.txt" "$out"
  ok "release-smoke.sh exited 0"
}

RESULT=fail
_story_ts_557
: "${RESULT:=fail}"
unset -f _story_ts_557
