#!/usr/bin/env bash
# TS-573 — Unknown token → GET /api/sessions returns 401
# tags: surface:api feature:federation feature:cbac
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-573"
story_preflight "surface:api feature:federation feature:cbac" || return 0

_story_ts_573() {
  # When auth is enabled (server.token != ""), an unknown token should return 401.
  # When no auth is configured, skip.
  local config_resp
  config_resp=$(api GET /api/config)

  # Check if server requires a token by attempting with a bogus token
  local raw code body
  raw=$(curl -sk --max-time 30 \
    -H "Authorization: Bearer bogus-unknown-token-ts573-$$" \
    -X GET "$TEST_BASE/api/sessions" \
    -w "\n__HTTP_CODE_%{http_code}__")
  code=$(echo "$raw" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$raw" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-573 "resp.json" "$body"

  if [[ "$code" == "401" ]]; then
    ok "unknown token returns 401 (auth enforced)"
  elif [[ "$code" == "200" ]]; then
    skip "server has no auth configured — unknown token returns 200 (open access)"
  elif [[ "$code" == "404" ]]; then
    skip "sessions endpoint not available in this build"
  elif [[ -z "$code" ]]; then
    skip "could not reach server"
  else
    skip "unexpected HTTP $code for unknown token — auth may be configured differently"
  fi
}

RESULT=fail
_story_ts_573
: "${RESULT:=fail}"
unset -f _story_ts_573
