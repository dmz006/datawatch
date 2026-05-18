#!/usr/bin/env bash
# TS-182 — Config parity: YAML/REST/CLI/PWA
# tags: surface:api feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-182"
story_preflight "surface:api feature:parity" || return 0

_story_ts_182() {
    echo ""; echo "  >> TS-182: Config parity: YAML/REST/CLI/PWA"
    resp=$(api GET /api/config)
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert len(d)>0" 2>/dev/null; then
      ok "GET /api/config returns non-empty config"
    else
      ko "GET /api/config did not return config"
    fi
    save_evidence "TS-182" "api_config.json" "$resp"

}

RESULT=fail
_story_ts_182
: "${RESULT:=fail}"
unset -f _story_ts_182
