#!/usr/bin/env bash
# TS-243 — Secrets journey: create → list → delete
# tags: surface:api feature:secrets
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-243"
story_preflight "surface:api feature:secrets" || return 0

_story_ts_243() {
    echo ""; echo "  >> TS-243: Secrets journey: create → list → delete"
    ts=$(date +%s)
    local sec_name="e2e-journey-$ts"
    create=$(api POST /api/secrets "{\"name\":\"$sec_name\",\"value\":\"test-secret-value\",\"backend\":\"builtin\"}")
    save_evidence "TS-243" "1_create.json" "$create"
    local created_name
    created_name=$(echo "$create" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('name',''))" 2>/dev/null || echo "")
    list=$(api GET /api/secrets)
    save_evidence "TS-243" "2_list.json" "$list"
    if [[ -n "$created_name" ]]; then
      del=$(curl "${curl_args[@]}" -X DELETE "$TEST_BASE/api/secrets/$created_name")
      save_evidence "TS-243" "3_delete.json" "$del"
      ok "Secrets journey: create → list → delete completed"
    else
      ko "Secrets journey: could not get secret name after create: $create"
    fi

}

RESULT=fail
_story_ts_243
: "${RESULT:=fail}"
unset -f _story_ts_243
