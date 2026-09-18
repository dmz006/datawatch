#!/usr/bin/env bash
# TS-777 — BL366: verifier_diff_max_bytes config round-trip
# tags: surface:api feature:autonomous parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-777"

story_preflight "surface:api feature:autonomous" || exit 0

# Read baseline.
before_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null)
before_val=$(echo "$before_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('autonomous',{}).get('verifier_diff_max_bytes',0))" 2>/dev/null)
echo "  [TS-777] baseline verifier_diff_max_bytes=$before_val (0 = 8192 sentinel default)"

# Set to 4096.
set_raw=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.verifier_diff_max_bytes": 4096}' \
  "$TEST_TLS/api/config" 2>/dev/null)
set_status=$(echo "$set_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null)
echo "  [TS-777] set 4096: $set_status"

# Read back.
after_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null)
after_val=$(echo "$after_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('autonomous',{}).get('verifier_diff_max_bytes',0))" 2>/dev/null)
echo "  [TS-777] after set: verifier_diff_max_bytes=$after_val"
save_evidence "$CURRENT_STORY" "config.json" "$after_raw"

# Restore baseline.
curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d "{\"autonomous.verifier_diff_max_bytes\": $before_val}" \
  "$TEST_TLS/api/config" >/dev/null 2>&1 || true

# Verify source documents default semantics (0 = use 8192).
src_default=$(grep -r "verifier_diff_max_bytes\|VerifierDiffMaxBytes" \
  "$REPO_ROOT/internal/" 2>/dev/null | grep -v "_test.go" | grep "8192\|default\|sentinel" | head -3)
echo "  [TS-777] source sentinel: ${src_default:0:120}"
save_evidence "$CURRENT_STORY" "sentinel.txt" "$src_default"

if [[ "$set_status" != "ok" ]]; then
  ko "PUT verifier_diff_max_bytes returned status=$set_status (expected ok)"
  exit 0
fi
if [[ "$after_val" != "4096" ]]; then
  ko "verifier_diff_max_bytes not persisted: got $after_val (expected 4096)"
  exit 0
fi

ok "BL366 verifier_diff_max_bytes: baseline=$before_val set→4096 read-back=$after_val; sentinel=0 means 8192"
