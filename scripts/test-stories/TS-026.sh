#!/usr/bin/env bash
# TS-026 — Automaton per-story approval gate
# tags: surface:api feature:automata conflict:llm
# legacy fn: t3_ts026_per_story_approval
#
# conflict:llm (reused, not really about LLM contention here): this test
# flips the GLOBAL, daemon-wide autonomous.per_story_approval config to
# true, then restores it. Found live (2026-10-05): with no exclusivity
# tag, this could run concurrently with another autonomous-PRD test
# (e.g. TS-695) in the serial lane, and if the toggle window overlapped
# that test's PRD-approval call, the OTHER test's stories got wrongly
# gated into awaiting_approval and hung forever (nothing ever calls
# approve_story for them). conflict:llm puts this in the same mutual-
# exclusion group as the other autonomous-PRD-lifecycle tests so they
# never overlap with this toggle.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-026"
story_preflight "surface:api feature:automata" || return 0

_story_ts_026() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi
  # Save current value and flip
  local psa_before
  psa_before=$(api GET /api/config | python3 -c 'import json,sys;d=json.load(sys.stdin);print(str(d.get("autonomous",{}).get("per_story_approval","false")).lower())' 2>/dev/null || echo "false")
  api PUT /api/config '{"autonomous.per_story_approval":true}' >/dev/null
  save_evidence TS-026 "before.json" "{\"per_story_approval_before\":\"$psa_before\"}"
  ok "per_story_approval toggled (round-trip)"
  # Restore
  if [[ "$psa_before" == "true" ]]; then
    api PUT /api/config '{"autonomous.per_story_approval":true}' >/dev/null
  else
    api PUT /api/config '{"autonomous.per_story_approval":false}' >/dev/null
  fi
  ok "per_story_approval restored to $psa_before"
}

RESULT=fail
_story_ts_026
: "${RESULT:=fail}"
unset -f _story_ts_026
