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
#
# The smoke passes (0) when the suite ran to completion and the report was
# written — even with layout findings in the report (tap-target, clipped-text,
# …): those are measurement OUTPUT the suite exists to surface, and the mobile
# fit state of the tree is the plan doc's business, not the smoke's. The smoke
# fails (1) when the wiring itself broke: no usable report was produced,
# or a scenario could not even open its view ('open-failed' findings in the
# report).

set -uo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# The runner (playwright) is tracked through the shared daemon lifecycle
# library so a Ctrl-C or failure ends it on EVERY exit path: its PID is
# recorded at start (marker = the runner script name, present in its
# cmdline for its whole life) and the library's EXIT/INT/TERM/HUP traps
# signal only that recorded PID — only while the marker is still in its
# cmdline — so the browser it launches (a child of the runner) never
# survives an interrupt (the leak fixed here, on 2026-09-28: a thrown test
# phase used to abandon the runner). td_start is a datawatch-daemon
# launcher (it appends "start --foreground --config"), so the runner is
# started directly and recorded with td_track_pid instead.
source "$SCRIPT_DIR/test-daemon-lib.sh"
# Activate the library's EXIT/INT/TERM/HUP traps NOW, before anything is
# created or the runner forks: td_track_pid only records — without installed
# traps the record is inert and the runner would survive every exit path —
# and the library may rerun this script once (to make INT trappable), which
# would orphan anything created earlier (the report dir below). When the
# script ends (normal, failure, Ctrl-C) the handler signals the recorded
# runner pid — only while its marker is still in /proc/<pid>/cmdline —
# which takes the browser child down with it.
td_install_traps "$@"

BASE="${DATAWATCH_BASE:-https://localhost:8443}"
TOKEN="${DATAWATCH_TOKEN:-}"
INSECURE="${DATAWATCH_INSECURE:-1}"

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
REPORT_JSON="$REPORT_DIR/mobile-fit/mobile-fit-report.json"

# report_usable <json> — 0 when a completed-run report exists and no scenario
# failed to open its view (an 'open-failed' finding is a wiring break, not a
# measured layout finding).
report_usable() {
  node -e '
const fs = require("fs");
try {
  const j = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  if (!j || !j.summary || !Array.isArray(j.groups)) process.exit(1);
  if ((j.summary.scenarios || 0) < 1) process.exit(1);
  if (j.groups.some((g) => g && g.check === "open-failed")) process.exit(1);
} catch {
  process.exit(1);
}
' "$1"
}

# summary_line <json> — the one-line report summary for the smoke output.
summary_line() {
  node -e '
const j = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
const s = j.summary || {};
console.log(`mobile-fit: ${s.scenarios} scenarios, ${s.total} findings in ${s.groups} groups`);
' "$1"
}

if command -v setsid >/dev/null 2>&1; then
  ( cd "$PWA_DIR" && exec setsid env \
      MOBILE_FIT_VIEW=observer MOBILE_FIT_PROFILE=quick \
      MOBILE_FIT_OUT="$REPORT_DIR/mobile-fit" \
      TEST_HTTP="$TEST_HTTP" TEST_TLS="$TEST_TLS" TEST_TOKEN="$TOKEN" \
      node mobile-fit.mjs >>"$REPORT_DIR/runner.log" 2>&1 ) &
else
  ( cd "$PWA_DIR" && exec env \
      MOBILE_FIT_VIEW=observer MOBILE_FIT_PROFILE=quick \
      MOBILE_FIT_OUT="$REPORT_DIR/mobile-fit" \
      TEST_HTTP="$TEST_HTTP" TEST_TLS="$TEST_TLS" TEST_TOKEN="$TOKEN" \
      node mobile-fit.mjs >>"$REPORT_DIR/runner.log" 2>&1 ) &
fi
runner_pid=$!
# Record it so every exit path ends it: marker = the runner script name,
# present in the runner's cmdline for its whole life (a recycled PID is
# left alone).
td_track_pid "$runner_pid" "mobile-fit.mjs" || exit 1
# The verdict and the script's exit status are the runner's own exit code,
# uncollapsed: 0 pass, 1 reportless failure, 2 "no browser", anything else
# whatever the runner actually exited with.
rc=0
if [[ -n "$runner_pid" ]]; then wait "$runner_pid" 2>/dev/null || rc=$?; fi

case $rc in
  0)
    summary_line "$REPORT_JSON" 2>/dev/null || true
    echo "OK: mobile-fit quick (observer) run passed with no findings"
    echo "OK: report: $REPORT_JSON"
    exit 0
    ;;
  1)
    if report_usable "$REPORT_JSON"; then
      summary_line "$REPORT_JSON" 2>/dev/null || true
      echo "OK: mobile-fit quick (observer) suite completed and reported (findings above are measured output, see the report)"
      echo "OK: report: $REPORT_JSON"
      exit 0
    fi
    echo "FAIL: mobile-fit runner exited 1 and produced no usable report (no report at $REPORT_JSON, or a scenario could not open its view); report dir kept at $REPORT_DIR"
    exit 1
    ;;
  2)
    echo "SKIP: mobile-fit runner exited 2 (no browser could be launched); report dir kept at $REPORT_DIR"
    exit 2
    ;;
  *)
    echo "FAIL: mobile-fit runner exited $rc; report kept at $REPORT_DIR"
    exit "$rc"
    ;;
esac
