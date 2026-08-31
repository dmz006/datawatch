#!/usr/bin/env bash
# TS-668 — webhook comms image injection: session output contains [image: ...] prefix
# tags: surface:live feature:vision group:vision conflict:ollama-llava
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-668"
story_preflight "surface:live feature:vision group:vision conflict:ollama-llava" || return 0

_story_ts_668() {
  # Enable vision on sandbox daemon pointing at the datawatch ollama instance
  local cfg_code
  cfg_code=$(api_code PUT /api/config \
    '{"vision.enabled":true,"vision.backend":"ollama","vision.endpoint":"http://datawatch:11434","vision.model":"Gemma3:12b"}' \
    | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')
  if [[ ! "$cfg_code" =~ ^2 ]]; then
    skip "could not enable vision on sandbox daemon (HTTP $cfg_code)"
    return
  fi

  # Get the webhook backend address from config (testdata fixes addr to 127.0.0.1:19082)
  local webhook_addr
  webhook_addr=$(api GET /api/config 2>/dev/null | python3 -c \
    "import json,sys; d=json.load(sys.stdin); print(d.get('webhook',{}).get('addr',''))" 2>/dev/null || true)
  if [[ -z "$webhook_addr" || "$webhook_addr" == "null" ]]; then
    skip "webhook addr not found in /api/config"
    return
  fi

  # Build a small 64x64 red PNG as a base64 data URI (no external file needed)
  local image_b64
  image_b64=$(python3 -c "
import base64, struct, zlib
def make_png(w,h,r,g,b):
    raw = b''.join(b'\x00'+bytes([r,g,b]*w) for _ in range(h))
    def chunk(tag,data):
        c=zlib.crc32(tag+data)&0xffffffff
        return struct.pack('>I',len(data))+tag+data+struct.pack('>I',c)
    ihdr=struct.pack('>IIBBBBB',w,h,8,2,0,0,0)
    return b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',ihdr)+chunk(b'IDAT',zlib.compress(raw))+chunk(b'IEND',b'')
print('data:image/png;base64,'+base64.b64encode(make_png(64,64,255,0,0)).decode())
" 2>/dev/null || true)
  if [[ -z "$image_b64" ]]; then
    skip "could not generate test PNG"
    return
  fi

  # POST to the webhook /task endpoint with image_url (new image attachment field)
  local wh_code wh_resp payload
  payload="{\"task\":\"describe what you see\",\"image_url\":\"$image_b64\"}"
  wh_resp=$(curl "${curl_args[@]}" -s -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "http://$webhook_addr/task" \
    -w "\n__HTTP_CODE_%{http_code}__" 2>/dev/null || echo "__HTTP_CODE_000__")
  wh_code=$(echo "$wh_resp" | sed -n 's/.*__HTTP_CODE_\([0-9]*\)__.*/\1/p')

  if [[ "$wh_code" == "000" || "$wh_code" == "503" ]]; then
    skip "webhook /task not reachable (HTTP $wh_code)"
    return
  fi
  if [[ "$wh_code" == "404" ]]; then
    skip "webhook /task returned 404 — webhook backend may not be running"
    return
  fi
  if [[ ! "$wh_code" =~ ^2 ]]; then
    ko "POST to webhook /task returned HTTP $wh_code: $(echo "$wh_resp" | head -c 200)"
    return
  fi

  # Give the session a moment to process the image attachment
  sleep 4

  # Find the most recent session and check its output for [image:
  local sessions sess_id output
  sessions=$(api GET /api/sessions 2>/dev/null || echo "[]")
  sess_id=$(echo "$sessions" | python3 -c \
    "import json,sys; s=json.load(sys.stdin); print(s[0]['id'] if s else '')" 2>/dev/null || true)

  if [[ -z "$sess_id" ]]; then
    skip "no sessions found after webhook delivery"
    return
  fi

  output=$(api GET "/api/sessions/$sess_id/output" 2>/dev/null || echo "")
  save_evidence TS-668 "session_output.txt" "$output"

  if echo "$output" | grep -q "\[image:"; then
    ok "session output contains [image: ...] — vision injection fired via webhook comms"
  else
    ko "session output did not contain [image: ...] — vision injection may have failed; check daemon logs"
  fi
}

RESULT=fail
_story_ts_668
: "${RESULT:=fail}"
unset -f _story_ts_668
