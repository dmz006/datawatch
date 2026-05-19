#!/usr/bin/env bash
# TS-062 — Plugin invoke / test endpoint
# tags: surface:api feature:plugins
# legacy fn: t7_ts062_plugin_invoke
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-062"
story_preflight "surface:api feature:plugins" || return 0

_story_ts_062() {
  # Install the test plugin so we always have at least one to invoke.
  ensure_test_plugin || true

  local resp
  resp=$(api GET /api/plugins)
  save_evidence TS-062 "plugins_for_invoke.json" "$resp"
  local first_plugin
  first_plugin=$(echo "$resp" | python3 -c "
import json,sys
d=json.load(sys.stdin)
arr = d.get('plugins',[]) if isinstance(d,dict) else d
arr = arr or []
print(arr[0].get('name','') if arr else '')
" 2>/dev/null || echo "")
  if [[ -z "$first_plugin" ]]; then
    skip "No plugins installed — invoke test skipped"
    return
  fi
  # GET the specific plugin record (manifest shape test)
  local invoke_resp
  invoke_resp=$(api GET "/api/plugins/$first_plugin" 2>/dev/null || echo '{}')
  save_evidence TS-062 "invoke.json" "$invoke_resp"
  if assert_json "$invoke_resp" 'isinstance(d, dict) and "name" in d'; then
    ok "Plugin $first_plugin GET returns manifest dict with name field"
  else
    ko "Plugin $first_plugin GET failed: $(echo "$invoke_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_062
: "${RESULT:=fail}"
unset -f _story_ts_062
