#!/usr/bin/env bash
# TS-775 — Goose session spawn: POST /api/sessions/start with backend=goose
# tags: surface:api feature:goose live:yes parallel:no
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-775"

story_preflight "surface:api feature:goose" || return 0

# Pre-check: goose must be configured and binary must exist.
cfg_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null)
goose_enabled=$(echo "$cfg_raw" | python3 -c "import sys,json; c=json.load(sys.stdin); print(c.get('goose',{}).get('enabled',False))" 2>/dev/null)
goose_binary=$(echo "$cfg_raw" | python3 -c "import sys,json; c=json.load(sys.stdin); print(c.get('goose',{}).get('binary',''))" 2>/dev/null)
if [[ "${goose_enabled,,}" != "true" ]]; then
  skip "goose not enabled in daemon config — skip live session spawn"
  return 0
fi
if [[ ! -x "$goose_binary" ]]; then
  skip "goose binary $goose_binary not executable — skip"
  return 0
fi
echo "  [TS-775] goose binary=$goose_binary enabled=$goose_enabled"

# Spawn a goose session.
sess_raw=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d '{"task":"ts775-goose-spawn","project_dir":"/tmp","backend":"goose"}' \
  "$TEST_TLS/api/sessions/start" 2>/dev/null)
sess_id=$(echo "$sess_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('id',''))" 2>/dev/null)
sess_backend=$(echo "$sess_raw" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('backend_family',d.get('llm_ref','?')))" 2>/dev/null)
sess_state=$(echo "$sess_raw" | python3 -c "import sys,json; print(json.load(sys.stdin).get('state','?'))" 2>/dev/null)
echo "  [TS-775] session: id=$sess_id backend_family=$sess_backend state=$sess_state"
save_evidence "$CURRENT_STORY" "session.json" "$sess_raw"

if [[ -z "$sess_id" ]]; then
  ko "POST /api/sessions/start with backend=goose returned no id: $sess_raw"
  return 0
fi
add_cleanup sess "$sess_id"

# Verify backend_family reflects goose.
if ! echo "$sess_backend" | grep -qi "goose"; then
  ko "session backend_family does not reflect goose: got $sess_backend (session=$sess_id)"
  return 0
fi

# Kill the session to avoid orphan processes.
curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" \
  -d "{\"id\":\"$sess_id\"}" \
  "$TEST_TLS/api/sessions/kill" >/dev/null 2>&1 || true

ok "goose session spawned: id=$sess_id backend_family=$sess_backend state=$sess_state"
