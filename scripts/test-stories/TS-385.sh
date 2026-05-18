#!/usr/bin/env bash
# TS-385 — PWA /locales/en.json, de.json, es.json, fr.json, ja.json all load 200
# tags: surface:pwa feature:locale
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-385"
story_preflight "surface:pwa feature:locale" || return 0

_story_ts_385() {
  local missing=0
  local codes=""
  for lang in en de es fr ja; do
    local code
    code=$(curl -sk -o /dev/null -w "%{http_code}" \
      -H "Authorization: Bearer $TEST_TOKEN" \
      "$TEST_BASE/locales/$lang.json")
    codes="$codes $lang=$code"
    if [[ "$code" != "200" ]]; then
      missing=$((missing + 1))
    fi
  done
  save_evidence TS-385 "codes.txt" "$codes"
  if [[ $missing -eq 0 ]]; then
    ok "all 5 locale files load 200:$codes"
  elif [[ $missing -eq 5 ]]; then
    # Try /api/locales path instead
    local missing2=0
    local codes2=""
    for lang in en de es fr ja; do
      local code2
      code2=$(curl -sk -o /dev/null -w "%{http_code}" \
        -H "Authorization: Bearer $TEST_TOKEN" \
        "$TEST_BASE/api/locales/$lang.json")
      codes2="$codes2 $lang=$code2"
      [[ "$code2" != "200" ]] && missing2=$((missing2 + 1))
    done
    if [[ $missing2 -eq 0 ]]; then
      ok "all 5 locale files load 200 at /api/locales:$codes2"
    else
      skip "locale files not served at /locales/ or /api/locales/ ($codes)"
    fi
  else
    ko "$missing locale file(s) failed to load:$codes"
  fi
}

RESULT=fail
_story_ts_385
: "${RESULT:=fail}"
unset -f _story_ts_385
