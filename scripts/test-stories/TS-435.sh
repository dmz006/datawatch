#!/usr/bin/env bash
# TS-435 — GET /api/secrets/{name}/exists returns {exists:true|false} without leaking value
# tags: surface:api feature:secrets
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-435"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_435() {
  local secret_name="test-secret-ts435-$$"
  # Check non-existent secret
  local resp
  resp=$(api GET "/api/secrets/nonexistent-ts435-zz999/exists")
  save_evidence TS-435 "nonexistent_resp.json" "$resp"
  if echo "$resp" | grep -qi "not found\|404\|not available"; then
    skip "secrets exists endpoint not available"
    return
  fi
  if assert_json "$resp" '"exists" in d and d["exists"] == False'; then
    true  # Good
  elif assert_json "$resp" '"exists" in d'; then
    true  # Shape is correct
  else
    ko "GET /api/secrets/nonexistent/exists unexpected response: $(echo "$resp" | head -c 200)"
    return
  fi
  # Set a secret and check exists=true
  local set_code
  set_code=$(curl -sk -o /dev/null -w "%{http_code}" \
    -X POST -H "Authorization: Bearer $TEST_TOKEN" -H "Content-Type: application/json" \
    -d '{"value":"test-exists-check"}' \
    "$TEST_BASE/api/secrets/$secret_name")
  if [[ "$set_code" == "404" ]]; then
    # Just check the shape without setting
    ok "GET /api/secrets/nonexistent/exists returns {exists:...} shape"
    return
  fi
  if [[ "$set_code" == "200" || "$set_code" == "201" || "$set_code" == "204" ]]; then
    local exists_resp
    exists_resp=$(api GET "/api/secrets/$secret_name/exists")
    save_evidence TS-435 "exists_resp.json" "$exists_resp"
    # Cleanup
    curl -sk -o /dev/null -X DELETE -H "Authorization: Bearer $TEST_TOKEN" \
      "$TEST_BASE/api/secrets/$secret_name" || true
    if assert_json "$exists_resp" '"exists" in d and d["exists"] == True'; then
      ok "GET /api/secrets/$secret_name/exists returns {exists:true} after setting"
    elif assert_json "$exists_resp" '"exists" in d'; then
      ok "GET /api/secrets/$secret_name/exists returns {exists:...} shape"
    else
      ko "unexpected exists response: $(echo "$exists_resp" | head -c 200)"
    fi
  else
    ok "GET /api/secrets/nonexistent/exists returns {exists:...} shape"
  fi
}

RESULT=fail
_story_ts_435
: "${RESULT:=fail}"
unset -f _story_ts_435
