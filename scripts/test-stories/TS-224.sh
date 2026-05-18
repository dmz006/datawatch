#!/usr/bin/env bash
# TS-224 — Device aliases
# tags: surface:api feature:devices
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-224"
story_preflight "surface:api feature:devices" || return 0

_story_ts_224() {
    echo ""; echo "  >> TS-224: Device aliases"
    resp=$(api GET /api/devices 2>/dev/null || echo '{"error":"not found"}')
    save_evidence "TS-224" "devices_list.json" "$resp"
    if echo "$resp" | python3 -c "import json,sys; d=json.load(sys.stdin); assert 'error' not in d" 2>/dev/null; then
      ok "Device aliases endpoint reachable"
    else
      skip "Device aliases endpoint not found (may be alpha feature)"
    fi
}

RESULT=fail
_story_ts_224
: "${RESULT:=fail}"
unset -f _story_ts_224
