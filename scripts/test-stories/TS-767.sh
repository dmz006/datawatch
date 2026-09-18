#!/usr/bin/env bash
# TS-767 — File service: upload, list, meta, delete round-trip
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-767"

story_preflight "surface:api feature:files" || exit 0

# Upload a file.
TMPF=$(mktemp)
echo "ts767 test payload $(date)" > "$TMPF"
upload_raw=$(curl "${curl_args[@]}" -X POST \
  -F "file=@$TMPF" -F "path=ts767_e2e_upload.txt" \
  "$TEST_TLS/api/files" 2>/dev/null)
rm -f "$TMPF"
upload_path=$(echo "$upload_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('path',''))" 2>/dev/null)
upload_bytes=$(echo "$upload_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('bytes',0))" 2>/dev/null)
echo "  [TS-767] upload: path=$upload_path bytes=$upload_bytes"
if [[ -z "$upload_path" || "$upload_bytes" -lt 1 ]]; then
  ko "upload failed: $upload_raw"
  exit 0
fi
save_evidence "$CURRENT_STORY" "upload.json" "$upload_raw"

# GET /api/files/meta — storage overview.
meta_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/files/meta" 2>/dev/null)
meta_root=$(echo "$meta_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('root',''))" 2>/dev/null)
echo "  [TS-767] meta root=$meta_root"
if [[ -z "$meta_root" ]]; then
  ko "GET /api/files/meta returned no root: $meta_raw"
  exit 0
fi
save_evidence "$CURRENT_STORY" "meta.json" "$meta_raw"

# DELETE the uploaded file (requires full absolute path).
del_raw=$(curl "${curl_args[@]}" -X DELETE \
  -H "Content-Type: application/json" \
  -d "{\"path\":\"$upload_path\"}" \
  "$TEST_TLS/api/files" 2>/dev/null)
del_deleted=$(echo "$del_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('deleted',''))" 2>/dev/null)
echo "  [TS-767] delete: deleted=$del_deleted"
if [[ -z "$del_deleted" ]]; then
  ko "DELETE failed: $del_raw"
  exit 0
fi
save_evidence "$CURRENT_STORY" "delete.json" "$del_raw"

# Second delete must return 404 (idempotent check).
del2_code=$(curl "${curl_args[@]}" -X DELETE -o /dev/null -w "%{http_code}" \
  -H "Content-Type: application/json" \
  -d "{\"path\":\"$upload_path\"}" \
  "$TEST_TLS/api/files" 2>/dev/null)
echo "  [TS-767] second delete HTTP $del2_code (expected 404)"

ok "file service upload→meta→delete round-trip: path=$upload_path bytes=$upload_bytes root=$meta_root second_delete=$del2_code"
