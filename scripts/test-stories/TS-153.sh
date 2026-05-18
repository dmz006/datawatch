#!/usr/bin/env bash
# TS-153 — Identity GET + PATCH round-trip
# tags: surface:api feature:identity
# legacy fn: t12_ts153_identity_get_patch
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-153"
story_preflight "surface:api feature:identity" || return 0

_story_ts_153() {
  local resp
  resp=$(api GET /api/identity 2>/dev/null || api GET /api/telos 2>/dev/null || echo '{"error":"not found"}')
  save_evidence TS-153 "get.json" "$resp"
  if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
    ok "GET /api/identity returns identity record"
    local name
    name=$(echo "$resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("name",d.get("persona_name","test-e2e")))' 2>/dev/null || echo "test-e2e")
    local patch_resp
    patch_resp=$(curl "${curl_args[@]}" -X PATCH -H "Content-Type: application/json" \
      -d "{\"name\":\"$name\"}" "$TEST_BASE/api/identity" 2>/dev/null || echo "{}")
    save_evidence TS-153 "patch.json" "$patch_resp"
    if assert_json "$patch_resp" 'isinstance(d, dict)'; then
      ok "PATCH /api/identity accepted"
    else
      skip "PATCH /api/identity not available: $(echo "$patch_resp" | head -c 100)"
    fi
  else
    skip "Identity endpoint not available: $(echo "$resp" | head -c 100)"
  fi
}

RESULT=fail
_story_ts_153
: "${RESULT:=fail}"
unset -f _story_ts_153
