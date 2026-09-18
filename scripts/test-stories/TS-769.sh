#!/usr/bin/env bash
# TS-769 — BL369: injection guard REST config round-trip
# tags: surface:api feature:autonomous feature:security parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-769"

story_preflight "surface:api feature:autonomous" || exit 0

# Read baseline state.
before_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null)
before_guard=$(echo "$before_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('autonomous',{}).get('injection_guard',False))" 2>/dev/null)
before_block=$(echo "$before_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('autonomous',{}).get('block_on_injection',False))" 2>/dev/null)
echo "  [TS-769] baseline: injection_guard=$before_guard block_on_injection=$before_block"

# Enable injection guard.
set_raw1=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.injection_guard": "true"}' \
  "$TEST_TLS/api/config" 2>/dev/null)
set_status1=$(echo "$set_raw1" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null)

# Enable block on injection.
set_raw2=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.block_on_injection": "true"}' \
  "$TEST_TLS/api/config" 2>/dev/null)
set_status2=$(echo "$set_raw2" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status','?'))" 2>/dev/null)

echo "  [TS-769] set injection_guard: $set_status1  set block_on_injection: $set_status2"

# Read back and verify.
after_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null)
after_guard=$(echo "$after_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('autonomous',{}).get('injection_guard',False))" 2>/dev/null)
after_block=$(echo "$after_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('autonomous',{}).get('block_on_injection',False))" 2>/dev/null)
echo "  [TS-769] after set: injection_guard=$after_guard block_on_injection=$after_block"

# Restore baseline.
restore_guard="false"; [[ "${before_guard,,}" == "true" ]] && restore_guard="true"
restore_block="false"; [[ "${before_block,,}" == "true" ]] && restore_block="true"
curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
  -d "{\"autonomous.injection_guard\": \"$restore_guard\", \"autonomous.block_on_injection\": \"$restore_block\"}" \
  "$TEST_TLS/api/config" >/dev/null 2>&1 || true

save_evidence "$CURRENT_STORY" "config.json" "$after_raw"

if [[ "$set_status1" != "ok" ]]; then
  ko "PUT injection_guard returned status=$set_status1 (expected ok)"
  exit 0
fi
if [[ "${after_guard,,}" != "true" ]]; then
  ko "injection_guard not set: got $after_guard after PUT"
  exit 0
fi
if [[ "${after_block,,}" != "true" ]]; then
  ko "block_on_injection not set: got $after_block after PUT"
  exit 0
fi

ok "BL369 injection guard REST: injection_guard=$after_guard block_on_injection=$after_block (both enabled via PUT /api/config)"
