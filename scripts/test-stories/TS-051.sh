#!/usr/bin/env bash
# TS-051 — List secrets
# tags: surface:api feature:secrets
# legacy fn: t6_ts051_list_secrets
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-051"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_051() {
  local resp
  resp=$(api GET /api/secrets)
  save_evidence TS-051 "list.json" "$resp"
  if assert_json "$resp" 'isinstance(d, (dict, list))'; then
    # Verify no plaintext values in list
    if echo "$resp" | python3 -c 'import json,sys; txt=sys.stdin.read(); assert "test-secret-value-12345" not in txt' 2>/dev/null; then
      ok "secrets list: no plaintext values exposed"
    else
      ko "secrets list exposes plaintext values"
    fi
  else
    ko "secrets list shape wrong: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_051
: "${RESULT:=fail}"
unset -f _story_ts_051
