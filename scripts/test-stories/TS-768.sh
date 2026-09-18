#!/usr/bin/env bash
# TS-768 — File service: path traversal blocked (403)
# tags: surface:api feature:files feature:security parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-768"

story_preflight "surface:api feature:files" || exit 0

TMPF=$(mktemp)
echo "traversal test payload" > "$TMPF"

# Attempt path traversal with relative escape.
code1=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" \
  -F "file=@$TMPF" -F "path=../../etc/passwd" \
  "$TEST_TLS/api/files" 2>/dev/null)
echo "  [TS-768] ../../etc/passwd → HTTP $code1 (expected 403)"

# Attempt traversal with absolute path outside root.
code2=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" \
  -F "file=@$TMPF" -F "path=/etc/passwd" \
  "$TEST_TLS/api/files" 2>/dev/null)
echo "  [TS-768] /etc/passwd → HTTP $code2 (expected 403)"

# Attempt with null-byte (should also fail).
code3=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" \
  -F "file=@$TMPF" -F "path=ts768%00../../etc/shadow" \
  "$TEST_TLS/api/files" 2>/dev/null)
echo "  [TS-768] null-byte traversal → HTTP $code3 (expected 403 or 400)"

# Legitimate upload succeeds.
code_ok=$(curl "${curl_args[@]}" -X POST -o /dev/null -w "%{http_code}" \
  -F "file=@$TMPF" -F "path=ts768_legitimate.txt" \
  "$TEST_TLS/api/files" 2>/dev/null)
echo "  [TS-768] legitimate path → HTTP $code_ok (expected 200)"

rm -f "$TMPF"

# Clean up legitimate upload if it succeeded.
if [[ "$code_ok" == "200" ]]; then
  meta_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/files/meta" 2>/dev/null)
  fs_root=$(echo "$meta_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('root',''))" 2>/dev/null)
  if [[ -n "$fs_root" ]]; then
    curl "${curl_args[@]}" -X DELETE -H "Content-Type: application/json" \
      -d "{\"path\":\"$fs_root/ts768_legitimate.txt\"}" \
      "$TEST_TLS/api/files" >/dev/null 2>&1 || true
  fi
fi

save_evidence "$CURRENT_STORY" "codes.json" \
  "{\"relative_escape\":\"$code1\",\"absolute\":\"$code2\",\"nullbyte\":\"$code3\",\"legitimate\":\"$code_ok\"}"

if [[ "$code1" != "403" ]]; then
  ko "relative traversal not blocked: ../../etc/passwd returned $code1 (expected 403)"
  exit 0
fi
if [[ "$code2" != "403" ]]; then
  ko "absolute path not blocked: /etc/passwd returned $code2 (expected 403)"
  exit 0
fi
if [[ "$code_ok" != "200" ]]; then
  ko "legitimate upload failed with $code_ok (expected 200)"
  exit 0
fi

ok "path traversal blocked (../../etc/passwd=$code1, /etc/passwd=$code2); legitimate upload=$code_ok"
