#!/usr/bin/env bash
# TS-708 — File Service API: GET /api/files/meta returns storage overview
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-708"
story_preflight "surface:api feature:files" || return 0

_story_ts_708() {
  local resp code body
  resp=$(api_code GET /api/files/meta)
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-708 "meta_resp.json" "$body"

  if [[ "$code" == "503" || "$code" == "404" ]]; then
    skip "file service not available (GET /api/files/meta → HTTP $code)"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/files/meta returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # Verify required fields: root, peers, discussions
  local has_root has_peers has_discussions
  has_root=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'root' in d else 'no')" 2>/dev/null || echo "no")
  has_peers=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'peers' in d else 'no')" 2>/dev/null || echo "no")
  has_discussions=$(echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); print('yes' if 'discussions' in d else 'no')" 2>/dev/null || echo "no")

  if [[ "$has_root" != "yes" ]]; then
    ko "GET /api/files/meta missing 'root' field: $body"
    return
  fi
  if [[ "$has_peers" != "yes" ]]; then
    ko "GET /api/files/meta missing 'peers' field: $body"
    return
  fi
  if [[ "$has_discussions" != "yes" ]]; then
    ko "GET /api/files/meta missing 'discussions' field: $body"
    return
  fi

  local root_val
  root_val=$(echo "$body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('root',''))" 2>/dev/null || echo "")
  ok "GET /api/files/meta returned storage overview — root=$root_val peers=present discussions=present"
}

RESULT=fail
_story_ts_708
: "${RESULT:=fail}"
unset -f _story_ts_708
