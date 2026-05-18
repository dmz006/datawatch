#!/usr/bin/env bash
# TS-095 — !help comm command
# tags: surface:api feature:comms
# legacy fn: t9_ts095_help_command
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-095"
story_preflight "surface:api feature:comms" || return 0

_story_ts_095() {
  local resp
  resp=$(api POST /api/test/message '{"text":"help"}')
  save_evidence TS-095 "help.json" "$resp"
  if echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('count', 0) >= 1
resp = ' '.join(d.get('responses', []))
assert 'datawatch commands' in resp.lower() or 'command' in resp.lower()
" 2>/dev/null; then
    ok "!help command returns canonical command list"
  else
    ko "!help command failed: $resp"
  fi
}

RESULT=fail
_story_ts_095
: "${RESULT:=fail}"
unset -f _story_ts_095
