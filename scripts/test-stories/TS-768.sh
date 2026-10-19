#!/usr/bin/env bash
# TS-768 — File Service API: POST /api/files upload — verify a LEGITIMATE
# path (inside the allowed root) is accepted, confirming the guard does not
# false-positive on well-formed requests.
#
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-768"

# ---- Unconditional cleanup -------------------------------------------------
# The fixture is staged under a per-run temp dir ($RUN_DIR is created outside
# the checkout by lib.sh), and the legitimate upload — namespaced under a
# unique per-run subdirectory of the server's file-service root — is removed
# through the server API on the way out. The EXIT trap is chained onto
# whatever trap the runner already installed, so both the fixture and the
# uploaded artifact are unconditionally removed on success AND failure
# (and on interrupt), whether serial, parallel, or standalone.
RESULT=fail
_TS768_UPLOADS_DIR="$RUN_DIR/uploads"
_TS768_TMPF="$_TS768_UPLOADS_DIR/ts768-$$/ts768_legitimate.txt"
_TS768_LANDED=""
_TS768_CLEANED=0
mkdir -p "$(dirname "$_TS768_TMPF")" 2>/dev/null || true

ts768_cleanup() {
  [[ "${_TS768_CLEANED:-0}" == 1 ]] && return 0
  _TS768_CLEANED=1
  if [[ -n "${_TS768_LANDED:-}" ]]; then
    curl "${curl_args[@]}" --max-time 10 -X DELETE \
      -H "Content-Type: application/json" \
      -d "{\"path\":\"$_TS768_LANDED\"}" \
      "$TEST_TLS/api/files" >/dev/null 2>&1 || true
    _TS768_LANDED=""
  fi
  rm -rf "$_TS768_UPLOADS_DIR" 2>/dev/null || true
  if [[ -n "${_TS768_PREV_EXIT_TRAP:-}" ]]; then
    eval "$_TS768_PREV_EXIT_TRAP"
  fi
  return 0
}
if [[ -z "${_TS768_TRAP_SET:-}" ]]; then
  _TS768_PREV_EXIT_TRAP="$(trap -p EXIT | sed -e 's/^trap -- //' -e 's/ *EXIT$//')"
  trap ts768_cleanup EXIT
  _TS768_TRAP_SET=1
fi

# Stage the local payload fixture in the per-run temp dir (never in the
# checkout).
printf 'TS-768 legitimate payload\n' > "$_TS768_TMPF"

# Upload to a per-run namespaced path *inside* the server's allowed root.
# A relative path resolves under root/<relative>, so "ts768-<pid>/
# ts768_legitimate.txt" is always inside root for any configured root —
# no traversal, no cross-tenant path reuse.
resp=$(curl "${curl_args[@]}" --max-time 15 -sS -w '\n%{http_code}' \
  -X POST \
  -F "file=@$_TS768_TMPF" \
  -F "path=ts768-$$/ts768_legitimate.txt" \
  "$TEST_TLS/api/files" 2>&1)
code="$(echo "$resp" | tail -1)"
body="$(echo "$resp" | sed '$d')"

if [[ "$code" =~ ^[23] ]]; then
  # A successful upload — register it for unconditional deletion.
  _TS768_LANDED=$(echo "$body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("path",""))' 2>/dev/null || echo "")
  echo "TS-768 upload: HTTP $code"
  echo "TS-768 accepted a legit in-root upload (code $code)"
  if [[ -n "${_TS768_LANDED}" ]]; then
    echo "TS-768 server-stored at: $_TS768_LANDED (will be removed on exit)"
  fi
  save_evidence TS-768 "upload_response.txt" "$body"
  ok "legitimate path accepted (code $code) — guard allows in-root uploads"
  RESULT=pass
  exit 0
else
  save_evidence TS-768 "upload_fairness_code.txt" "$code"
  ko "legitimate path was REJECTED (code $code) — guard is over-restrictive"
  exit 0
fi
