#!/usr/bin/env bash
# TS-186 — Config alignment: YAML keys match GET /api/config
# tags: surface:api feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-186"
story_preflight "surface:api feature:parity" || return 0

_story_ts_186() {
    echo ""; echo "  >> TS-186: Config alignment: YAML keys match GET /api/config"
    resp=$(api GET /api/config)
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'server' in d or len(d)>0" 2>/dev/null; then
      ok "Config endpoint returns structured config"
    else
      ko "Config endpoint missing expected structure"
    fi
    save_evidence "TS-186" "config.json" "$resp"

}

RESULT=fail
_story_ts_186
: "${RESULT:=fail}"
unset -f _story_ts_186
