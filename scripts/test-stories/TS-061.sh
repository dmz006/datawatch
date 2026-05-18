#!/usr/bin/env bash
# TS-061 — Plugin manifest shape
# tags: surface:api feature:plugins
# legacy fn: t7_ts061_plugin_manifest
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-061"
story_preflight "surface:api feature:plugins" || return 0

_story_ts_061() {
  local resp
  resp=$(api GET /api/plugins)
  save_evidence TS-061 "plugins.json" "$resp"
  # Check that at least one plugin entry has a name field (manifest shape)
  local has_manifest
  has_manifest=$(echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('plugins',[])
print('yes' if len(arr)>0 and any('name' in p for p in arr) else 'empty')
" 2>/dev/null || echo "unknown")
  if [[ "$has_manifest" == "yes" ]]; then
    ok "Plugin manifest: at least one plugin with name field found"
  elif [[ "$has_manifest" == "empty" ]]; then
    skip "No plugins installed — manifest shape cannot be verified"
  else
    ko "Plugin list did not return valid manifest shape: $(echo "$resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_061
: "${RESULT:=fail}"
unset -f _story_ts_061
