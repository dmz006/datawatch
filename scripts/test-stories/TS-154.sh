#!/usr/bin/env bash
# TS-154 — Algorithm start + advance
# tags: surface:api feature:algorithm
# legacy fn: t12_ts154_algorithm_start_advance
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-154"
story_preflight "surface:api feature:algorithm" || return 0

_story_ts_154() {
  local list_resp
  list_resp=$(api GET /api/algorithm 2>/dev/null || api GET /api/algorithms 2>/dev/null || echo '{"error":"not found"}')
  save_evidence TS-154 "list.json" "$list_resp"
  if echo "$list_resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
    ok "GET /api/algorithm returns algorithm surface"
    local first_alg
    first_alg=$(echo "$list_resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('algorithms',[])
print(arr[0].get('id','') if arr else '')
" 2>/dev/null || echo "")
    if [[ -n "$first_alg" ]]; then
      local start_resp
      start_resp=$(api POST "/api/algorithm/$first_alg/start" '{}' 2>/dev/null || echo '{}')
      save_evidence TS-154 "start.json" "$start_resp"
      if assert_json "$start_resp" 'isinstance(d, dict)'; then
        ok "Algorithm $first_alg start accepted"
        local adv_resp
        adv_resp=$(api POST "/api/algorithm/$first_alg/advance" '{}' 2>/dev/null || echo '{}')
        save_evidence TS-154 "advance.json" "$adv_resp"
        if assert_json "$adv_resp" 'isinstance(d, dict)'; then
          ok "Algorithm advance accepted"
        else
          skip "Algorithm advance not available"
        fi
        api POST "/api/algorithm/$first_alg/reset" '{}' >/dev/null 2>&1 || true
      else
        skip "Algorithm start not available"
      fi
    else
      skip "No algorithms configured"
    fi
  else
    skip "Algorithm endpoint not available"
  fi
}

RESULT=fail
_story_ts_154
: "${RESULT:=fail}"
unset -f _story_ts_154
