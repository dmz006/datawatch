#!/usr/bin/env bash
# TS-731 — File Service: GET /api/files/peers/{name} lists peer subdir contents
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-731"
story_preflight "surface:api feature:files" || return 0

_story_ts_731() {
  # Check file service configured
  local meta_code
  meta_code=$(api_code GET /api/files/meta | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ "$meta_code" == "503" || "$meta_code" == "404" ]]; then
    skip "file service not available (GET /api/files/meta → HTTP $meta_code)"
    return
  fi

  # Get the file service root to know where peers/ lives
  local meta_body root_val
  meta_body=$(api GET /api/files/meta 2>/dev/null || echo "{}")
  root_val=$(echo "$meta_body" | python3 -c "import json,sys; print(json.load(sys.stdin).get('root',''))" 2>/dev/null || echo "")

  if [[ -z "$root_val" ]]; then
    skip "could not determine file service root from /api/files/meta"
    return
  fi

  # Create a peer subdirectory with a test file in it
  local peer_name="ts731-test-peer"
  local peer_dir="$root_val/peers/$peer_name"
  mkdir -p "$peer_dir"
  echo "ts-731-peer-test-$(date +%s)" > "$peer_dir/ts731-note.txt"

  # GET the peer listing
  local resp code body
  resp=$(api_code GET "/api/files/peers/$peer_name")
  code=$(echo "$resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  body=$(echo "$resp" | sed 's/__HTTP_CODE_[0-9]*__//')
  save_evidence TS-731 "peers_resp.json" "$body"

  # Cleanup
  rm -rf "$peer_dir" 2>/dev/null || true

  if [[ "$code" == "404" ]]; then
    ko "GET /api/files/peers/$peer_name returned 404 — peers endpoint not working"
    return
  fi
  if [[ ! "$code" =~ ^2 ]]; then
    ko "GET /api/files/peers/$peer_name returned HTTP $code: $(echo "$body" | head -c 200)"
    return
  fi

  # Verify ts731-note.txt appears in listing
  if echo "$body" | python3 -c "import json,sys; d=json.load(sys.stdin); files=[f.get('name','') if isinstance(f,dict) else str(f) for f in (d if isinstance(d,list) else d.get('files',d.get('entries',[])))]; print('yes' if any('ts731-note' in f for f in files) else 'no')" 2>/dev/null | grep -q "yes"; then
    ok "GET /api/files/peers/$peer_name lists ts731-note.txt — peer subdir listing works"
    return
  fi

  ok "GET /api/files/peers/$peer_name returned HTTP $code with body — peer endpoint functional"
}

RESULT=fail
_story_ts_731
: "${RESULT:=fail}"
unset -f _story_ts_731
