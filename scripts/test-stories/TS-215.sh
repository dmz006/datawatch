#!/usr/bin/env bash
# TS-215 — secrets-manager: list surface
# tags: surface:api feature:secrets
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-215"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_215() {
    echo ""; echo "  >> TS-215: secrets-manager: list surface"
    resp=$(api GET /api/secrets)
    save_evidence "TS-215" "secrets_list.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin)" 2>/dev/null; then
      ok "Secrets endpoint reachable"
    else
      ko "Secrets endpoint did not return JSON"
    fi

  # Gap-fill stories
}

RESULT=fail
_story_ts_215
: "${RESULT:=fail}"
unset -f _story_ts_215
