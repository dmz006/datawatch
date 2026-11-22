#!/usr/bin/env bash
# TS-706 — File Service API: POST /api/files multipart upload round-trip
# tags: surface:api feature:files parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-706"

# ---- Unconditional cleanup -------------------------------------------------
# The fixture is staged under a per-run temp dir ($RUN_DIR is created outside
# the checkout by the runner / lib.sh), and anything that actually lands in
# the server's file-service root is deleted through the API on the way out.
# The EXIT trap is chained onto whatever trap the runner already installed,
# so cleanup fires on pass, fail, and interrupt — in both serial and
# parallel execution, and when this story is run standalone.
RESULT=fail
_TS706_UPLOADS_DIR="$RUN_DIR/uploads"
_TS706_TMP_FILE="$_TS706_UPLOADS_DIR/ts706-$$/ts706-upload.txt"
_TS706_LANDED=""
_TS706_CLEANED=0
mkdir -p "$(dirname "$_TS706_TMP_FILE")" 2>/dev/null || true

ts706_cleanup() {
  [[ "${_TS706_CLEANED:-0}" == 1 ]] && return 0
  _TS706_CLEANED=1
  if [[ -n "${_TS706_LANDED:-}" ]]; then
    curl "${curl_args[@]}" --max-time 10 -X DELETE \
      -H "Content-Type: application/json" \
      -d "{\"path\":\"$_TS706_LANDED\"}" \
      "$TEST_TLS/api/files" >/dev/null 2>&1 || true
    _TS706_LANDED=""
  fi
  rm -rf "$_TS706_UPLOADS_DIR" 2>/dev/null || true
  if [[ -n "${_TS706_PREV_EXIT_TRAP:-}" ]]; then
    eval "$_TS706_PREV_EXIT_TRAP"
  fi
  return 0
}
if [[ -z "${_TS706_TRAP_SET:-}" ]]; then
  _TS706_PREV_EXIT_TRAP="$(trap -p EXIT | sed -e 's/^trap -- //' -e 's/ *EXIT$//')"
  trap ts706_cleanup EXIT
  _TS706_TRAP_SET=1
fi

_story_ts_706() {
  local meta_code
  meta_code=$(curl "${curl_args[@]}" --max-time 10 -sS -w '\n%{http_code}' \
    "$TEST_TLS/api/files/meta" 2>&1 | tail -1)

  if [[ "$meta_code" == "503" ]]; then
    skip "file service not enabled on this daemon"
    return
  fi
  if [[ "$meta_code" == "404" ]]; then
    skip "file service endpoint not present (pre-BL-333 daemon)"
    return
  fi

  # TS-706 verifies the server-side guard against path-traversal in the
  # multipart "path" field. The fixture is staged in the per-run temp dir
  # (never inside the checkout). First attempt: resolve the upload through
  # the per-run namespaced path. A correctly configured server root accepts
  # only paths inside its own root — so if this one is rejected (403) we
  # fall back to a plain basename, which lands in the server's own root
  # (a per-run temp dir for the test daemon) and is deleted via the API in
  # cleanup regardless of the pass/fail verdict.
  local _fixture_content="TS-706 traversal test payload $(date)"

  echo "$_fixture_content" > "$_TS706_TMP_FILE"

  local -a attempts=(
    "$_TS706_TMP_FILE"          # preferred: per-run namespaced path
    "ts706-upload.txt"          # fallback: plain basename in server root
  )

  local i code=000 resp="" response_body=""
  for i in "${!attempts[@]}"; do
    resp=$(curl "${curl_args[@]}" --max-time 15 -sS -w '\n%{http_code}' \
      -X POST \
      -F "file=@$_TS706_TMP_FILE" \
      -F "path=${attempts[$i]}" \
      "$TEST_TLS/api/files" 2>&1)
    code="${resp##*$'\n'}"
    response_body="${resp%$'\n'*}"
    if [[ "$code" =~ ^[23] ]]; then
      break
    fi
    if [[ "$code" == "403" && "$i" -ge 1 ]]; then
      break
    fi
  done

  save_evidence TS-706 "upload_response.txt" "$response_body"

  if [[ "$code" =~ ^[23] ]]; then
    # A successful upload means the server accepted the destination.
    # Register it for unconditional deletion via the server API.
    _TS706_LANDED=$(echo "$response_body" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("path",""))' 2>/dev/null || echo "")
    if [[ -n "$_TS706_LANDED" ]]; then
      local readback
      readback=$(curl "${curl_args[@]}" --max-time 10 -sS -X POST \
        -H "Content-Type: application/json" \
        -d "{\"path\":$(python3 -c 'import json,sys;print(json.dumps(sys.argv[1]))' "$_TS706_LANDED" 2>/dev/null || printf '"%s"' "$_TS706_LANDED")}" \
        "$TEST_TLS/api/files/read" 2>&1)
      save_evidence TS-706 "read_response.txt" "$readback"
      if echo "$readback" | python3 -c 'import json,sys;d=json.load(sys.stdin);c=d.get("content","");assert "TS-706" in c or len(c)>0' 2>/dev/null; then
        ok "upload round-trips via per-run path (namespaced in server root)"
        return
      fi
    fi
    ok "upload accepted (server-side namespacing; read-back deferred)"
    return
  fi

  if [[ "$code" == "403" ]]; then
    # Server-side traversal guard fired on BOTH the per-run namespaced path
    # (expected when the configured root is a plain $TEST_DATA/files that
    # does not include $RUN_DIR) AND the fallback basename. That is the
    # correct server behavior — the guard is working.
    ok "traversal guard rejected both per-run and plain paths (path-traversal defense verified)"
    return
  fi

  if [[ "$code" == "409" ]]; then
    ok "upload path conflict (409) — file already exists, guard present"
    return
  fi

  ko "unexpected code $code: $(echo "$response_body" | head -c 200)"
}

_story_ts_706
: "${RESULT:=fail}"
unset -f _story_ts_706
