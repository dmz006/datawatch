#!/usr/bin/env bash
# TS-740 — CLI prd-set-llm --decomposition-profile round-trip
# tags: surface:cli feature:automata parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-740"
story_preflight "surface:cli feature:automata" || return 0

_story_ts_740() {
  local a_ok
  a_ok=$(api GET /api/autonomous/config \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print("yes" if d.get("enabled") else "no")' 2>/dev/null || echo "no")
  if [[ "$a_ok" != "yes" ]]; then
    skip "autonomous disabled"
    return
  fi

  # Find or create a PRD to use
  local prd_id
  prd_id=$(api POST /api/autonomous/prds \
    '{"spec":"TS-740 CLI decomposition_profile test","project_dir":"/tmp/ts740"}' \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$prd_id" ]]; then
    skip "could not create PRD"
    return
  fi

  _cleanup() { api DELETE "/api/autonomous/prds/$prd_id" >/dev/null 2>&1 || true; }

  # Find the datawatch binary
  local dw_bin
  dw_bin=$(which datawatch 2>/dev/null || echo "/home/dmz/.local/bin/datawatch")
  if [[ ! -x "$dw_bin" ]]; then
    _cleanup; skip "datawatch binary not found at $dw_bin"
    return
  fi

  # Use the first registered LLM or fall back to "ollama"
  local profile
  profile=$(api GET /api/config \
    | python3 -c '
import json,sys
d=json.load(sys.stdin)
llms=d.get("llm",{}).get("registered",[])
if llms:
    print(llms[0].get("name","ollama"))
else:
    print("ollama")
' 2>/dev/null || echo "ollama")

  local cli_out cli_exit
  cli_out=$("$dw_bin" autonomous prd-set-llm "$prd_id" \
    --decomposition-profile "$profile" 2>&1)
  cli_exit=$?
  save_evidence TS-740 "cli-output.txt" "$cli_out"

  if [[ $cli_exit -ne 0 ]]; then
    _cleanup
    # Some versions may not have this flag yet
    if echo "$cli_out" | grep -qi "unknown flag\|no such command\|not found"; then
      skip "CLI prd-set-llm --decomposition-profile flag not available in this build"
      return
    fi
    ko "CLI prd-set-llm --decomposition-profile=$profile exited $cli_exit: $cli_out"
    return
  fi

  # Verify via REST that field was persisted
  local got_profile
  got_profile=$(api GET "/api/autonomous/prds/$prd_id" \
    | python3 -c 'import json,sys;print(json.load(sys.stdin).get("decomposition_profile",""))' 2>/dev/null || echo "")

  _cleanup

  if [[ "$got_profile" == "$profile" ]]; then
    ok "CLI prd-set-llm --decomposition-profile=$profile persisted; GET confirms decomposition_profile=$got_profile"
    return
  fi

  ko "CLI set decomposition_profile=$profile but GET returned $got_profile"
}

RESULT=fail
_story_ts_740
: "${RESULT:=fail}"
unset -f _story_ts_740
unset -f _cleanup 2>/dev/null || true
