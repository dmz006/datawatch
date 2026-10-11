#!/usr/bin/env bash
# TS-795 — B113: GET/PUT /api/autonomous/scan/config round-trips the
# SAST/secrets/deps toggles (previously zero-valued regardless of
# scan.DefaultConfig()'s all-on intent, and any write was lost on
# restart -- fixed v9.0.11). This story pins the REST round-trip half
# (write a non-default value, read it back from a fresh GET); the
# restart-survives-this half was already verified once via a real
# kill-9 + restart live smoke at ship time (see CHANGELOG v9.0.11) and
# isn't safe to repeat generically here (restarting the shared sandbox
# daemon mid-suite would affect every other concurrently-running
# story) -- same deferral precedent as the "boot-resume on real
# daemon" item in this cookbook's conflict:llm table.
# tags: surface:api feature:automata conflict:selfconfig
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-795"
story_preflight "surface:api feature:automata conflict:selfconfig" || return 0

_story_ts_795() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local before sast_before
  before=$(api GET /api/autonomous/scan/config)
  save_evidence TS-795 "0_before.json" "$before"
  sast_before=$(echo "$before" | python3 -c 'import json,sys;print(str(json.load(sys.stdin).get("sast_enabled","true")).lower())' 2>/dev/null || echo "true")

  local flipped
  if [[ "$sast_before" == "true" ]]; then flipped=false; else flipped=true; fi
  api PUT /api/autonomous/scan/config "{\"sast_enabled\":$flipped}" >/dev/null

  local after sast_after
  after=$(api GET /api/autonomous/scan/config)
  save_evidence TS-795 "1_after.json" "$after"
  sast_after=$(echo "$after" | python3 -c 'import json,sys;print(str(json.load(sys.stdin).get("sast_enabled","")).lower())' 2>/dev/null || echo "")

  # Restore before asserting.
  api PUT /api/autonomous/scan/config "{\"sast_enabled\":$sast_before}" >/dev/null

  if [[ "$sast_after" == "$flipped" ]]; then
    ok "PUT /api/autonomous/scan/config sast_enabled round-trips via a fresh GET (B113, v9.0.11)"
  else
    ko "sast_enabled round-trip mismatch: wrote $flipped, read back '$sast_after'"
  fi
}

RESULT=fail
_story_ts_795
: "${RESULT:=fail}"
unset -f _story_ts_795
