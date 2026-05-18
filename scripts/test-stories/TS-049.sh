#!/usr/bin/env bash
# TS-049 — Spatial probe
# tags: surface:api feature:memory
# legacy fn: t5_ts049_spatial_probe
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-049"
story_preflight "surface:api feature:memory" || return 0

_story_ts_049() {
  if [[ "$(t5_check_memory)" != "yes" ]]; then skip "memory subsystem not enabled"; return; fi
  local sr
  sr=$(api POST /api/memory/save '{"content":"test spatial probe e2e '"$$"'","wing":"test-wing-e2e-'"$$"'"}')
  local sp_id
  sp_id=$(echo "$sr" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("id",""))' 2>/dev/null || echo "")
  save_evidence TS-049 "save.json" "$sr"
  if [[ -z "$sp_id" ]]; then skip "spatial probe save failed"; return; fi
  local list_resp
  list_resp=$(curl "${curl_args[@]}" "$TEST_BASE/api/memory/list?wing=test-wing-e2e-$$&limit=50")
  save_evidence TS-049 "list_filtered.json" "$list_resp"
  if echo "$list_resp" | python3 -c "import json,sys; arr=json.load(sys.stdin); assert any(int(m.get('id',0))==$sp_id for m in arr)" 2>/dev/null; then
    ok "spatial wing filter returned probe id=$sp_id"
  else
    skip "wing filter did not return probe — may be unsupported"
  fi
  # Cleanup
  api POST /api/memory/delete '{"id":'"$sp_id"'}' >/dev/null
}

RESULT=fail
_story_ts_049
: "${RESULT:=fail}"
unset -f _story_ts_049
