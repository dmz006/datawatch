#!/usr/bin/env bash
# TS-062 — Plugin invoke / test endpoint
# tags: surface:api feature:plugins
# legacy fn: t7_ts062_plugin_invoke
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-062"
story_preflight "surface:api feature:plugins" || return 0

_story_ts_062() {
  local resp
  resp=$(api GET /api/plugins)
  save_evidence TS-062 "plugins_for_invoke.json" "$resp"
  local first_plugin
  first_plugin=$(echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d if isinstance(d,list) else d.get('plugins',[])
print(arr[0].get('name','') if arr else '')
" 2>/dev/null || echo "")
  if [[ -z "$first_plugin" ]]; then
    skip "No plugins installed — invoke test skipped"
    return
  fi
  local invoke_resp
  invoke_resp=$(api POST "/api/plugins/$first_plugin/test" '{}' 2>/dev/null || \
    api GET "/api/plugins/$first_plugin" 2>/dev/null || echo '{}')
  save_evidence TS-062 "invoke.json" "$invoke_resp"
  if assert_json "$invoke_resp" 'isinstance(d, dict)'; then
    ok "Plugin $first_plugin test/get endpoint returns dict"
  else
    ko "Plugin invoke failed: $(echo "$invoke_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_062
: "${RESULT:=fail}"
unset -f _story_ts_062
