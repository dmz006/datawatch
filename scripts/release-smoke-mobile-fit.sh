#!/usr/bin/env bash
# release-smoke-mobile-fit.sh — Standalone smoke for the mobile-fit view runner.
#
# Drive scripts/test-stories/pwa/mobile-fit.mjs (the Playwright
# scenario harness for the mobile-fit view suite) against a running
# daemon: observer view, quick profile. The PWA story env (TEST_HTTP /
# TEST_TLS / TEST_TOKEN) is derived from DATAWATCH_BASE + DATAWATCH_TOKEN;
# the report is written into a fresh temp directory and the report path
# is printed.
#
# Skips cleanly (exit 2, one 'SKIP:' line) when:
#   - daemon unreachable at DATAWATCH_BASE
#   - node is missing from PATH
#   - the Playwright dependency in scripts/test-stories/pwa is not
#     installed (no resolvable 'playwright' module there)
#   - the runner exits 2 (no browser could be launched)
#
# Exit code 0 = pass, 1 = real failure, 2 = skip.

set -uo pipefail

BASE="${DATAWATCH_BASE:-https://localhost:8443}"
TOKEN="${DATAWATCH_TOKEN:-}"
INSECURE="${DATAWATCH_INSECURE:-1}"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PWA_DIR="$SCRIPT_DIR/test-stories/pwa"

if [[ ! -f "$PWA_DIR/mobile-fit.mjs" ]]; then
  echo "SKIP: mobile-fit runner not found at $PWA_DIR/mobile-fit.mjs"
  exit 2
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "SKIP: curl not found in PATH"
  exit 2
fi

curl_args=(--silent --show-error --max-time 10)
[[ "$INSECURE" == "1" ]] && curl_args+=(-k)

if ! curl "${curl_args[@]}" "$BASE/api/health" >/dev/null 2>&1; then
  echo "SKIP: daemon unreachable at $BASE"
  exit 2
fi

if ! command -v node >/dev/null 2>&1; then
  echo "SKIP: node not found in PATH (mobile-fit runner needs Node.js)"
  exit 2
fi

if ! ( cd "$PWA_DIR" && node -e "require('playwright')" ) >/dev/null 2>&1; then
  echo "SKIP: Playwright dependency not installed in $PWA_DIR (run: cd $PWA_DIR && npm ci)"
  exit 2
fi

# Derive the PWA story env from the base URL. The PWA renders on
# TEST_HTTP; prefer the scheme variant that actually serves the app root
# (some daemons serve TLS only on their primary port).
origin_serves() {
  local code
  code=$(curl -k -o /dev/null -s -w '%{http_code}' --max-time 10 "$1/" 2>/dev/null) || return 1
  [[ "$code" == 2* ]]
}

TEST_HTTP="$BASE"
TEST_TLS="$BASE"
case "$BASE" in
  http://*)
    TEST_TLS="${BASE/http:\/\//https://}"
    origin_serves "$TEST_TLS" || TEST_TLS="$BASE"   # TLS optional
    ;;
  https://*)
    LOCAL_HTTP="${BASE/https:\/\//http://}"
    TEST_HTTP="$(origin_serves "$LOCAL_HTTP" && printf '%s' "$LOCAL_HTTP" || printf '%s' "$BASE")"
    ;;
esac

REPORT_DIR=$(mktemp -d "${TMPDIR:-/tmp}/release-smoke-mobile-fit-XXXXXX")
echo "mobile-fit: report dir: $REPORT_DIR"

(
  cd "$PWA_DIR" && \
  MOBILE_FIT_VIEW=observer \
  MOBILE_FIT_PROFILE=quick \
  MOBILE_FIT_OUT="$REPORT_DIR/mobile-fit" \
  TEST_HTTP="$TEST_HTTP" \
  TEST_TLS="$TEST_TLS" \
  TEST_TOKEN="$TOKEN" \
  node mobile-fit.mjs
)
rc=$?

case $rc in
  0)
    echo "OK: mobile-fit quick (observer) run passed; report: $REPORT_DIR"
    exit 0
    ;;
  2)
    echo "SKIP: mobile-fit runner exited 2 (no browser could be launched); report dir kept at $REPORT_DIR"
    exit 2
    ;;
  *)
    echo "FAIL: mobile-fit runner exited $rc; report kept at $REPORT_DIR"
    exit 1
    ;;
esac
