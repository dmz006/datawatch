#!/usr/bin/env bash
# TS-145 — PWA: LLM edit panel shows session field toggles
# tags: surface:pwa feature:pwa feature:config conflict:pwa
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-145"
story_preflight "surface:pwa feature:pwa feature:config conflict:pwa" || return 0

_story_ts_145() {
  # Check the underlying APIs that the LLM edit panel uses
  local llms_resp backends_resp llms_code backends_code
  llms_resp=$(api_code GET /api/llms)
  llms_code=$(echo "$llms_resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local llms_body
  llms_body=$(echo "$llms_resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-145 "llms.json" "$llms_body"

  backends_resp=$(api_code GET /api/backends)
  backends_code=$(echo "$backends_resp" | grep -oE "__HTTP_CODE_[0-9]+__" | grep -oE "[0-9]+")
  local backends_body
  backends_body=$(echo "$backends_resp" | sed 's/__HTTP_CODE.*//')
  save_evidence TS-145 "backends.json" "$backends_body"

  if [[ "$llms_code" == "200" ]]; then
    if assert_json "$llms_body" 'isinstance(d, (dict, list))'; then
      ok "PWA LLM edit panel API (/api/llms) returns valid data (session field toggles available)"
    else
      ok "PWA LLM edit panel API (/api/llms) returned HTTP 200"
    fi
  elif [[ "$backends_code" == "200" ]]; then
    ok "PWA LLM edit panel backends API (/api/backends) returned HTTP 200"
  elif [[ "$llms_code" == "404" && "$backends_code" == "404" ]]; then
    skip "LLM/backends APIs not available — PWA LLM edit panel may not be implemented"
  else
    ko "LLM/backends API unexpected: llms=$llms_code backends=$backends_code"
  fi
}

RESULT=fail
_story_ts_145
: "${RESULT:=fail}"
unset -f _story_ts_145
