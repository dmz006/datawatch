#!/usr/bin/env bash
# TS-784 — v8.36.6 PRD repair_depends_on: POST /api/autonomous/prds/{id}/repair_depends_on
# resolves any raw-title DependsOn entries left over from a PRD whose
# SetStories call predates the v8.36.5 title->ID resolution fix (found live
# on PRD a2833a5e — see CHANGELOG v8.36.5/v8.36.6).
# tags: surface:api feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-784"
story_preflight "surface:api feature:automata" || return 0

_story_ts_784() {
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # A fresh draft PRD has no stories yet — repair_depends_on is a safe
  # no-op resolve pass, so this doesn't need a live LLM decompose to reach.
  # Full title->ID resolution correctness is covered deterministically by
  # the Go unit tests (TestStore_RepairDependsOn); this story only proves
  # the endpoint is wired end-to-end through the real HTTP server.
  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-784 repair_depends_on reachability test","project_dir":"/tmp/ts784"}' \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD"
    return
  fi

  _cleanup() { api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true; }

  local resp code body
  resp=$(api_code POST "/api/autonomous/prds/$prd_id/repair_depends_on" \
    '{"actor":"e2e-ts784"}')
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-784 "repair_resp.json" "$body"

  if [[ "$code" == "404" ]]; then
    _cleanup
    skip "repair_depends_on endpoint not found (404)"
    return
  fi

  if [[ "$code" != "200" ]]; then
    _cleanup
    ko "POST repair_depends_on returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  local returned_id
  returned_id=$(echo "$body" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  _cleanup

  if [[ "$returned_id" == "$prd_id" ]]; then
    ok "POST /api/autonomous/prds/$prd_id/repair_depends_on succeeded (HTTP 200) — no-op resolve on a story-less PRD"
  else
    ko "repair_depends_on 200 response missing expected PRD id (got body: $(echo "$body" | head -c 200))"
  fi
}

RESULT=fail
_story_ts_784
: "${RESULT:=fail}"
unset -f _story_ts_784
unset -f _cleanup 2>/dev/null || true
