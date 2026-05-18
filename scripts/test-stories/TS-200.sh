#!/usr/bin/env bash
# TS-200 — setup-and-install: health + version + auth flow
# tags: surface:api feature:bootstrap
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-200"
story_preflight "surface:api feature:bootstrap" || return 0

_story_ts_200() {
    echo ""; echo "  >> TS-200: setup-and-install: health + version + auth flow"
    resp=$(api GET /api/health)
    ver=$(echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('version','unknown'))" 2>/dev/null || echo "unknown")
    save_evidence "TS-200" "health.json" "$resp"
    if [[ "$ver" != "unknown" && "$ver" != "0.0.0" ]]; then
      ok "Health returns version $ver"
    else
      ko "Health did not return a real version"
    fi

}

RESULT=fail
_story_ts_200
: "${RESULT:=fail}"
unset -f _story_ts_200
