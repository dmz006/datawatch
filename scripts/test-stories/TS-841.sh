#!/usr/bin/env bash
# TS-841 — Identity backups, the empty-write refusal, restore and reset on REST, CLI and MCP
# tags: surface:api surface:cli surface:mcp feature:identity
#
# Writes identity A then B, checks the backup of A, proves an all-empty write is
# refused (409 identity_wipe_refused) and keeps B, restores A, then repeats
# backups / restore / reset over the CLI and over the MCP call route, and finally
# reads the audit log for identity_set_refused, identity_reset and
# identity_restore. The story creates all its own state inline and leaves the
# sandbox identity reset.
#
# Safety: this story writes (and deliberately attempts to empty) the identity, so
# it only runs against the sandbox daemon named by TEST_BASE. The guard is an
# allowlist, not a denylist: TEST_BASE must be a loopback address whose port is the
# sandbox HTTP port (TEST_PORT) or the sandbox TLS port (TEST_TLS_PORT) the harness
# allocated, and neither of those may be a production port (8080 or 8443). Any other
# TEST_BASE gets no identity write at all. The story never reads a daemon address
# from anywhere but TEST_BASE / TEST_BASE_HTTP / TEST_PORT / TEST_TLS_PORT.
#
# run-tests.sh sets TEST_BASE to https on TEST_TLS_PORT and TEST_BASE_HTTP to plain
# http on TEST_PORT; the REST and MCP steps use TEST_BASE, the CLI steps use the
# port cli_test in lib.sh targets (http on 127.0.0.1:TEST_PORT).
#
# Harness limits, each reported as a FAIL line in the output (never a silent skip):
#   - CLI: needs the built binary at TEST_BINARY, and the port of TEST_BASE_HTTP to be
#     TEST_PORT, because cli_test in lib.sh points the CLI at http://127.0.0.1:TEST_PORT.
#   - MCP: POST /api/mcp/call on the same daemon, the way the other surface:mcp stories do.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-841"
story_preflight "surface:api surface:cli surface:mcp feature:identity" || return 0

_ts841_fails=0
_ts841_pass() { echo "  PASS  [TS-841] $*"; }
_ts841_fail() { echo "  FAIL  [TS-841] $*"; _ts841_fails=$((_ts841_fails + 1)); }

# _ts841_role — print the role from an identity JSON on stdin ("?" when unreadable).
_ts841_role() { python3 -c 'import json,sys;print(json.load(sys.stdin).get("role",""))' 2>/dev/null || echo "?"; }

# _ts841_newest_backup_role — role of the newest entry in a backups JSON on stdin.
_ts841_newest_backup_role() {
  python3 -c 'import json,sys;b=json.load(sys.stdin)["backups"];print(b[0].get("role","") if b else "")' 2>/dev/null || echo "?"
}

# _ts841_is_empty — exit 0 when the identity JSON on stdin has no content.
_ts841_is_empty() {
  python3 -c '
import json,sys
d=json.load(sys.stdin)
keys=("role","north_star_goals","current_projects","values","current_focus","context_notes")
assert not any(d.get(k) for k in keys)
' 2>/dev/null
}

# _ts841_url_port <url> — the port of a URL (empty when it names none).
_ts841_url_port() { local h="${1#*://}"; h="${h%%/*}"; [[ "$h" == *:* ]] && echo "${h##*:}" || echo ""; }

# _ts841_url_host <url> — the host of a URL.
_ts841_url_host() { local h="${1#*://}"; h="${h%%/*}"; echo "${h%%:*}"; }

# _ts841_audit_count <action> <since> — number of audit entries with that action
# written at or after <since> (RFC 3339, UTC), so earlier runs cannot satisfy it.
_ts841_audit_count() {
  api GET "/api/audit?action=$1&since=$2&limit=200" 2>/dev/null |
    python3 -c 'import json,sys;print(len(json.load(sys.stdin).get("entries") or []))' 2>/dev/null || echo "-1"
}

_story_ts_841() {
  local role_a="story-role-a" role_b="story-role-b"
  local base_port base_host http_base="${TEST_BASE_HTTP:-$TEST_HTTP}" http_port
  base_port=$(_ts841_url_port "$TEST_BASE")
  base_host=$(_ts841_url_host "$TEST_BASE")
  http_port=$(_ts841_url_port "$http_base")

  # Sandbox guard (allowlist): loopback host, and a port the harness gave the sandbox
  # daemon. Nothing below runs, and no identity write is sent, otherwise.
  local sandbox_ok=1 p
  [[ "$base_host" == "127.0.0.1" || "$base_host" == "localhost" || "$base_host" == "[::1]" ]] || sandbox_ok=0
  [[ -n "$base_port" && ( "$base_port" == "${TEST_PORT:-}" || "$base_port" == "${TEST_TLS_PORT:-}" ) ]] || sandbox_ok=0
  for p in "${TEST_PORT:-}" "${TEST_TLS_PORT:-}" "$base_port"; do
    [[ "$p" == "8080" || "$p" == "8443" ]] && sandbox_ok=0
  done
  if [[ "$sandbox_ok" != "1" ]]; then
    ko "refusing to run: TEST_BASE=$TEST_BASE is not the sandbox daemon (need a loopback host on TEST_PORT=${TEST_PORT:-} or TEST_TLS_PORT=${TEST_TLS_PORT:-}, never 8080 or 8443); this story writes the identity and runs only against the sandbox"
    return
  fi

  # Audit entries are counted from here on, so leftovers from earlier runs never count.
  local since
  since=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

  # --- (1) write identity A, then identity B, through PUT /api/identity ---
  local a_code b_code
  a_code=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "{\"role\":\"$role_a\",\"north_star_goals\":[\"story goal\"]}" -o /dev/null -w '%{http_code}' "$TEST_BASE/api/identity" 2>/dev/null || echo "000")
  b_code=$(curl "${curl_args[@]}" -X PUT -H "Content-Type: application/json" \
    -d "{\"role\":\"$role_b\"}" -o /dev/null -w '%{http_code}' "$TEST_BASE/api/identity" 2>/dev/null || echo "000")
  save_evidence TS-841 "put-codes.txt" "A=$a_code B=$b_code"
  if [[ "$a_code" == "200" && "$b_code" == "200" ]]; then
    _ts841_pass "PUT /api/identity accepted identity A then identity B"
  else
    _ts841_fail "PUT identity A answered HTTP $a_code, PUT identity B answered HTTP $b_code (expected 200 and 200)"
  fi

  # --- (2) GET /api/identity/backups shows a backup holding A ---
  local bk
  bk=$(api GET /api/identity/backups 2>/dev/null || echo "")
  save_evidence TS-841 "backups.json" "$bk"
  if echo "$bk" | ROLE="$role_a" python3 -c '
import json,os,sys
d=json.load(sys.stdin)
assert any(b.get("role")==os.environ["ROLE"] for b in d["backups"])
' 2>/dev/null; then
    _ts841_pass "GET /api/identity/backups lists a backup whose role is $role_a"
  else
    _ts841_fail "no backup with role $role_a: $(echo "$bk" | head -c 200)"
  fi

  # --- (3) an all-empty PUT is refused with 409 identity_wipe_refused; GET still returns B ---
  local resp ecode ebody ecodefield role_now
  resp=$(curl "${curl_args[@]}" -w '\n%{http_code}' -X PUT -H "Content-Type: application/json" -d '{"role":"","north_star_goals":[],"values":[]}' "$TEST_BASE/api/identity" 2>/dev/null || true) # identity-wipe-ok: asserts the refusal
  ecode=$(echo "$resp" | tail -n1)
  ebody=$(echo "$resp" | sed '$d')
  ecodefield=$(echo "$ebody" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("code",""))' 2>/dev/null || echo "")
  role_now=$(api GET /api/identity 2>/dev/null | _ts841_role)
  save_evidence TS-841 "empty-put.txt" "$resp"
  if [[ "$ecode" == "409" && "$ecodefield" == "identity_wipe_refused" && "$role_now" == "$role_b" ]]; then
    _ts841_pass "an all-empty PUT is refused (409 identity_wipe_refused) and GET still returns $role_b"
  else
    _ts841_fail "empty PUT answered HTTP $ecode code='$ecodefield', role now '$role_now' (expected 409, identity_wipe_refused, $role_b)"
  fi

  # --- (4) restore A through POST /api/identity/restore with latest ---
  local rs_code
  rs_code=$(curl "${curl_args[@]}" -X POST -H "Content-Type: application/json" -d '{"backup":"latest"}' \
    -o /dev/null -w '%{http_code}' "$TEST_BASE/api/identity/restore" 2>/dev/null || echo "000")
  role_now=$(api GET /api/identity 2>/dev/null | _ts841_role)
  if [[ "$rs_code" == "200" && "$role_now" == "$role_a" ]]; then
    _ts841_pass "POST /api/identity/restore with latest brings back $role_a"
  else
    _ts841_fail "restore latest answered HTTP $rs_code, role now '$role_now' (expected 200, $role_a)"
  fi

  # --- (5) the CLI: identity backups, identity restore, identity reset --yes ---
  # The CLI talks to http://127.0.0.1:TEST_PORT (cli_test in lib.sh). It is only used
  # when that port is the HTTP port of the sandbox daemon (TEST_BASE_HTTP) and the
  # binary exists.
  local cli_ok=1
  if [[ ! -x "$TEST_BINARY" ]]; then
    _ts841_fail "CLI steps not run: no executable datawatch binary at TEST_BINARY=$TEST_BINARY (build it, or set TEST_BINARY)"
    cli_ok=0
  elif [[ -z "$http_port" || "$http_port" != "${TEST_PORT:-}" ]]; then
    _ts841_fail "CLI steps not run: cli_test targets http://127.0.0.1:${TEST_PORT:-} but the sandbox HTTP base is $http_base, so the CLI would not reach the sandbox daemon (TEST_BASE_HTTP and TEST_PORT must name the same port)"
    cli_ok=0
  fi
  if [[ "$cli_ok" == "1" ]]; then
    local expect_role cli_out cli_rc
    # What the next restore latest should bring back: the role of the newest backup.
    expect_role=$(api GET /api/identity/backups 2>/dev/null | _ts841_newest_backup_role)

    cli_out=$(cli_test identity backups 2>&1); cli_rc=$?
    save_evidence TS-841 "cli-backups.txt" "$cli_out"
    if [[ $cli_rc -eq 0 ]] && echo "$cli_out" | grep -q "$role_a" && echo "$cli_out" | grep -q "$role_b"; then
      _ts841_pass "datawatch identity backups lists the backups holding $role_a and $role_b"
    else
      _ts841_fail "datawatch identity backups exited $cli_rc without both roles: $(echo "$cli_out" | head -c 200)"
    fi

    cli_out=$(cli_test identity restore 2>&1); cli_rc=$?
    save_evidence TS-841 "cli-restore.txt" "$cli_out"
    role_now=$(api GET /api/identity 2>/dev/null | _ts841_role)
    if [[ $cli_rc -eq 0 && -n "$expect_role" && "$expect_role" != "?" && "$role_now" == "$expect_role" ]] && echo "$cli_out" | grep -q "$expect_role"; then
      _ts841_pass "datawatch identity restore brought back the newest backup ($expect_role)"
    else
      _ts841_fail "datawatch identity restore exited $cli_rc, role now '$role_now' (expected '$expect_role'): $(echo "$cli_out" | head -c 200)"
    fi

    cli_out=$(cli_test identity reset --yes 2>&1); cli_rc=$?
    save_evidence TS-841 "cli-reset.txt" "$cli_out"
    if [[ $cli_rc -eq 0 ]] && api GET /api/identity 2>/dev/null | _ts841_is_empty; then
      _ts841_pass "datawatch identity reset --yes leaves an empty identity"
    else
      _ts841_fail "datawatch identity reset --yes exited $cli_rc or the identity still has content: $(echo "$cli_out" | head -c 200)"
    fi
  fi

  # --- (6) MCP through POST /api/mcp/call: identity_backups, restore_identity,
  #         set_identity with no fields, reset_identity ---
  local mcp_bk mcp_expect mcp_restore mcp_raw
  mcp_bk=$(api_mcp identity_backups '{}')
  save_evidence TS-841 "mcp-backups.json" "$mcp_bk"
  mcp_expect=$(echo "$mcp_bk" | _ts841_newest_backup_role)
  if echo "$mcp_bk" | python3 -c 'import json,sys;assert len(json.load(sys.stdin)["backups"])>0' 2>/dev/null; then
    _ts841_pass "MCP identity_backups lists the saved backups"
  else
    _ts841_fail "MCP identity_backups returned no backups: $(echo "$mcp_bk" | head -c 200)"
  fi

  mcp_restore=$(api_mcp restore_identity '{"backup":"latest"}')
  save_evidence TS-841 "mcp-restore.json" "$mcp_restore"
  role_now=$(api GET /api/identity 2>/dev/null | _ts841_role)
  if [[ -n "$mcp_expect" && "$mcp_expect" != "?" && "$role_now" == "$mcp_expect" ]] && echo "$mcp_restore" | grep -q "$mcp_expect"; then
    _ts841_pass "MCP restore_identity brought back the newest backup ($mcp_expect)"
  else
    _ts841_fail "MCP restore_identity left role '$role_now' (expected '$mcp_expect'): $(echo "$mcp_restore" | head -c 200)"
  fi

  # set_identity with no fields over a saved identity: refused, text names the code, identity kept.
  mcp_raw=$(api POST /api/mcp/call '{"tool":"set_identity","params":{}}')
  save_evidence TS-841 "mcp-set-empty.json" "$mcp_raw"
  local role_after_refusal
  role_after_refusal=$(api GET /api/identity 2>/dev/null | _ts841_role)
  if echo "$mcp_raw" | grep -q "identity_wipe_refused" && echo "$mcp_raw" | grep -q "reset_identity" && [[ "$role_after_refusal" == "$role_now" ]]; then
    _ts841_pass "MCP set_identity with no fields returns the refusal text and keeps $role_after_refusal"
  else
    _ts841_fail "MCP set_identity with no fields did not return the refusal (role now '$role_after_refusal'): $(echo "$mcp_raw" | head -c 300)"
  fi

  api_mcp reset_identity '{}' > /dev/null
  if api GET /api/identity 2>/dev/null | _ts841_is_empty; then
    _ts841_pass "MCP reset_identity leaves an empty identity"
  else
    _ts841_fail "identity still has content after MCP reset_identity"
  fi

  # --- (7) the audit log shows the refusal, the reset and the restore ---
  local n_refused n_reset n_restore
  n_refused=$(_ts841_audit_count identity_set_refused "$since")
  n_reset=$(_ts841_audit_count identity_reset "$since")
  n_restore=$(_ts841_audit_count identity_restore "$since")
  save_evidence TS-841 "audit-counts.txt" "identity_set_refused=$n_refused identity_reset=$n_reset identity_restore=$n_restore"
  # Counted since this run started. Refusals: the REST PUT and the MCP set_identity.
  # Resets: MCP always, CLI too when it ran. Restores: REST and MCP always, CLI too when it ran.
  local want_reset=1 want_restore=2
  if [[ "$cli_ok" == "1" ]]; then want_reset=2; want_restore=3; fi
  if [[ "$n_refused" -ge 2 && "$n_reset" -ge "$want_reset" && "$n_restore" -ge "$want_restore" ]]; then
    _ts841_pass "audit log has identity_set_refused ($n_refused), identity_reset ($n_reset) and identity_restore ($n_restore) entries written since $since"
  else
    _ts841_fail "audit log counts: identity_set_refused=$n_refused (want >=2), identity_reset=$n_reset (want >=$want_reset), identity_restore=$n_restore (want >=$want_restore) since $since; -1 means /api/audit was unreadable"
  fi

  # Leave the sandbox identity reset (the explicit, backed-up clear).
  api POST /api/identity/reset > /dev/null 2>&1 || true

  if [[ "$_ts841_fails" -eq 0 ]]; then
    ok "identity backups, empty-write refusal, restore and reset hold on REST, CLI and MCP, and the audit log records them"
  else
    ko "$_ts841_fails identity check(s) failed"
  fi
}

RESULT=fail
_story_ts_841
: "${RESULT:=fail}"
unset -f _story_ts_841 _ts841_pass _ts841_fail _ts841_role _ts841_newest_backup_role _ts841_is_empty _ts841_url_port _ts841_url_host _ts841_audit_count
