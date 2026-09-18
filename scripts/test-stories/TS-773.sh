#!/usr/bin/env bash
# TS-773 — Vision describe live: POST /api/vision/describe with real PNG
# tags: surface:api feature:vision live:yes parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-773"

story_preflight "surface:api feature:vision" || exit 0

# Check vision is enabled in config.
vis_cfg=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null | \
  python3 -c "import sys,json; c=json.load(sys.stdin); v=c.get('vision',{}); print(v.get('enabled',False),v.get('backend','?'))" 2>/dev/null)
echo "  [TS-773] vision config: $vis_cfg"

# Check moondream model is available locally.
if ! curl -s --max-time 5 http://localhost:11434/api/tags 2>/dev/null | grep -q "moondream"; then
  skip "moondream model not available on localhost:11434 — skip live vision test"
  exit 0
fi

# Create a minimal valid PNG (1x1 pixel, white).
PNG_FILE=$(mktemp --suffix=.png)
python3 -c "
import struct, zlib, sys
def make_png(w, h, color=(255,255,255)):
    def chunk(name, data):
        c = struct.pack('>I', len(data)) + name + data
        return c + struct.pack('>I', zlib.crc32(name + data) & 0xffffffff)
    ihdr = struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0)
    row = b'\\x00' + bytes(color) * w
    idat = zlib.compress(row * h, 9)
    return b'\\x89PNG\\r\\n\\x1a\\n' + chunk(b'IHDR', ihdr) + chunk(b'IDAT', idat) + chunk(b'IEND', b'')
open(sys.argv[1], 'wb').write(make_png(8, 8))
" "$PNG_FILE" 2>/dev/null

if [[ ! -s "$PNG_FILE" ]]; then
  ko "could not create test PNG file"
  exit 0
fi
echo "  [TS-773] test PNG: $PNG_FILE ($(wc -c < "$PNG_FILE") bytes)"

# POST to vision/describe (allow up to 60s for model inference).
vis_raw=$(curl "${curl_args[@]}" --max-time 90 -X POST \
  -F "image=@$PNG_FILE" \
  "$TEST_TLS/api/vision/describe" 2>/dev/null)
vis_code=$(curl "${curl_args[@]}" --max-time 90 -X POST -o /dev/null -w "%{http_code}" \
  -F "image=@$PNG_FILE" \
  "$TEST_TLS/api/vision/describe" 2>/dev/null)
rm -f "$PNG_FILE"

echo "  [TS-773] HTTP $vis_code response=${vis_raw:0:120}"
save_evidence "$CURRENT_STORY" "vision_response.json" "$vis_raw"

if [[ "$vis_code" != "200" ]]; then
  ko "POST /api/vision/describe returned HTTP $vis_code (expected 200): $vis_raw"
  exit 0
fi

vis_desc=$(echo "$vis_raw" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('description','')[:80])" 2>/dev/null)
vis_lat=$(echo "$vis_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('latency_ms',0))" 2>/dev/null)
echo "  [TS-773] description=${vis_desc:0:60} latency_ms=$vis_lat"

if [[ -z "$vis_desc" ]]; then
  ko "vision describe returned no description field: $vis_raw"
  exit 0
fi

ok "vision describe live: HTTP 200 latency_ms=$vis_lat description='${vis_desc:0:50}...'"
