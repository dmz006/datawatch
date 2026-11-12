#!/usr/bin/env bash
# checks-and-ship-live-check.sh — live check of the Automata checks and ship
# surfaces on a SANDBOX daemon: defaults, the settings round trip and its
# rejections, the two read routes, the command line, and the served PWA code.
#
# What it does NOT run: the ship step itself (push, tag, CI watch) needs a
# finished release Automaton and a model-driven run, which a sandbox has no
# model for. That path is covered by the in-process test
# TestChecksShipIntegration (cmd/datawatch) against a local bare remote, and the
# report says so.
#
# Safety: SANDBOX only. A generated config under a throwaway directory inside
# this checkout, loopback ports, its own data directory. The production daemon
# is never touched; the daemon is ended only by the PID scripts/test-daemon-lib.sh
# recorded. No session is started, so no multiplexer is used.
#
# Usage: env -u TMUX -u TMUX_TMPDIR bash scripts/checks-and-ship-live-check.sh
# Env:   CHECKS_SHIP_REPORT  report path (default docs/plans/2026-11-02-checks-and-ship-live-check.md)
#        TEST_BINARY         daemon binary (default: built from this checkout)
if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
REPORT="${CHECKS_SHIP_REPORT:-$ROOT/docs/plans/2026-11-02-checks-and-ship-live-check.md}"
S="$(mktemp -d "$ROOT/.checks-ship-live.XXXXXX")"
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"
check_cleanup_extra() { rm -rf "$S" 2>/dev/null || true; }
TD_EXTRA_CLEANUP=check_cleanup_extra
td_install_traps "$@"

for t in curl python3 jq go; do command -v "$t" >/dev/null 2>&1 || { echo "SKIP: $t missing"; exit 0; }; done
mkdir -p "$S/bin" "$S/data" "$S/projects"
BINARY="${TEST_BINARY:-}"
if [[ -z "$BINARY" ]]; then
  (cd "$ROOT" && go build -o "$S/bin/datawatch" ./cmd/datawatch/) >/dev/null 2>&1 && BINARY="$S/bin/datawatch"
fi
[[ -x "$BINARY" ]] || { echo "SKIP: no binary"; exit 0; }

free_port() { python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
DNS_PORT="$(free_port)"; HOOK_PORT="$(free_port)"; NTFY_PORT="$(free_port)"; OLLAMA_PORT="$(free_port)"
TOKEN="dw-test-token-12345"; BASE="https://127.0.0.1:$TLS_PORT"
CFG="$S/config.yaml"; LOG="$S/daemon.log"

sed \
  -e "s|data_dir: /data|data_dir: $S/data|g" \
  -e "s|default_project_dir: /data/projects|default_project_dir: $S/projects|g" \
  -e "s|port: 8080|port: $PORT|g" -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
  -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
  -e "s|token: \"\"|token: \"$TOKEN\"|g" -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
  -e "s|listen: \"127\.0\.0\.1:19053\".*|listen: \"127.0.0.1:$DNS_PORT\"|g" \
  -e "s|addr: 127\.0\.0\.1:19080|addr: 127.0.0.1:$HOOK_PORT|g" \
  -e "s|server_url: http://127\.0\.0\.1:19180|server_url: http://127.0.0.1:$NTFY_PORT|g" \
  -e "s|http://local-node:11434|http://127.0.0.1:$OLLAMA_PORT|g" \
  -e "s|http://localhost:11434|http://127.0.0.1:$OLLAMA_PORT|g" \
  -e '/^compute_nodes:/,/^observer:/{/^observer:/!d}' \
  "$ROOT/testdata/datawatch.yaml" > "$CFG"

api() { local m="$1" p="$2" b="${3:-}"; curl -sk -m 30 -X "$m" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p"; }
code() { local m="$1" p="$2" b="${3:-}"; curl -sk -m 30 -o /dev/null -w '%{http_code}' -X "$m" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p"; }
dw() { DATAWATCH_TOKEN="$TOKEN" "$BINARY" --config "$CFG" -u "$BASE" "$@"; }

TOTAL=0; FAILS=0; LINES=""
ck() { # ck <rc> <what> [evidence]
  TOTAL=$((TOTAL+1)); local v=PASS; [[ "$1" == 0 ]] || { v=FAIL; FAILS=$((FAILS+1)); }
  echo "  $v  $2"; LINES+="- **$v** — $2"$'\n'; [[ -n "${3:-}" ]] && LINES+="  - $3"$'\n'
}
eq() { [[ "$1" == "$2" ]]; }

if ! td_start "$BINARY" "$CFG" "$LOG"; then echo "FAIL: daemon did not start: $(tail -5 "$LOG")"; exit 1; fi
td_set_api "$TD_LAST_PID" "$BASE" "$TOKEN"
end=$((SECONDS+90)); until curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; do ((SECONDS>end)) && { echo "FAIL: no health"; exit 1; }; sleep 2; done
echo "daemon healthy (pid $TD_LAST_PID)"

echo "== 1. defaults"
CHK0="$(api GET /api/autonomous/checks)"; SHP0="$(api GET /api/autonomous/ship)"
ck "$(jq -e 'type=="object"' <<<"$CHK0" >/dev/null 2>&1; echo $?)" "GET /api/autonomous/checks answers a JSON object" "$(head -c 300 <<<"$CHK0")"
ck "$(jq -e 'type=="object"' <<<"$SHP0" >/dev/null 2>&1; echo $?)" "GET /api/autonomous/ship answers a JSON object" "$(head -c 300 <<<"$SHP0")"
ck "$(eq "$(jq -r '.policy.pre_start_verifier' <<<"$CHK0")" on; echo $?)" "pre_start_verifier reads the default mode on"
ck "$(eq "$(jq -r '.policy.push' <<<"$SHP0")" on; echo $?)" "ship push reads the default mode on"
ck "$(eq "$(jq -r '.policy.tag_format' <<<"$SHP0")" 'v{version}'; echo $?)" "tag_format reads the default v{version}"

echo "== 2. settings round trip and rejections"
C1="$(code PUT /api/config '{"autonomous.checks.pre_start_verifier":"shadow","autonomous.checks.after_done":"off","autonomous.ship.push":"shadow","autonomous.ship.tag":"shadow","autonomous.ship.tag_format":"rel-{version}","autonomous.ship.remote":"upstream","autonomous.ship.ci_workflows":"release,ci"}')"
ck "$(eq "$C1" 200; echo $?)" "PUT /api/config with the new keys answers 200" "status $C1"
CFGB="$(api GET /api/config)"
ck "$(eq "$(jq -r '.autonomous.checks.pre_start_verifier' <<<"$CFGB")" shadow; echo $?)" "checks.pre_start_verifier reads back shadow"
ck "$(eq "$(jq -r '.autonomous.checks.after_done' <<<"$CFGB")" off; echo $?)" "checks.after_done reads back off"
ck "$(eq "$(jq -r '.autonomous.ship.tag_format' <<<"$CFGB")" 'rel-{version}'; echo $?)" "ship.tag_format reads back rel-{version}"
ck "$(eq "$(jq -r '.autonomous.ship.remote' <<<"$CFGB")" upstream; echo $?)" "ship.remote reads back upstream"
ck "$(eq "$(jq -c '.autonomous.ship.ci_workflows' <<<"$CFGB")" '["release","ci"]'; echo $?)" "ship.ci_workflows reads back as a list" "$(jq -c '.autonomous.ship.ci_workflows' <<<"$CFGB")"
B1="$(code PUT /api/config '{"autonomous.checks.plan_rules":"maybe"}')"
ck "$(eq "$B1" 400; echo $?)" "a bad check mode is rejected with 400" "status $B1"
B2="$(code PUT /api/config '{"autonomous.ship.tag_format":"nover"}')"
ck "$(eq "$B2" 400; echo $?)" "a tag_format without {version} is rejected with 400" "status $B2"
CFGC="$(api GET /api/config)"
ck "$(eq "$(jq -r '.autonomous.checks.plan_rules' <<<"$CFGC")" "$(jq -r '.autonomous.checks.plan_rules' <<<"$CFGB")"; echo $?)" "the rejected values changed nothing"

echo "== 3. the read routes follow the settings"
CHK1="$(api GET /api/autonomous/checks)"; SHP1="$(api GET /api/autonomous/ship)"
ck "$(grep -q shadow <<<"$CHK1"; echo $?)" "checks route shows the shadow mode just set" "$(head -c 300 <<<"$CHK1")"
ck "$(grep -q 'rel-{version}' <<<"$SHP1"; echo $?)" "ship route shows the new tag format" "$(head -c 300 <<<"$SHP1")"
ck "$(eq "$(code GET '/api/autonomous/checks?prd_id=none-such')" 200; echo $?)" "checks route accepts prd_id for an unknown id"
ck "$(eq "$(code POST /api/autonomous/checks '{}')" 405; echo $?)" "checks route refuses a write with 405"
ck "$(eq "$(code POST /api/autonomous/ship '{}')" 405; echo $?)" "ship route refuses a write with 405"

echo "== 4. command line"
CLI1="$(dw autonomous checks 2>&1)"; CLI2="$(dw autonomous ship 2>&1)"
ck "$(grep -qi shadow <<<"$CLI1"; echo $?)" "datawatch autonomous checks prints the modes" "$(head -c 300 <<<"$CLI1")"
ck "$(grep -q 'rel-{version}' <<<"$CLI2"; echo $?)" "datawatch autonomous ship prints the tag format" "$(head -c 300 <<<"$CLI2")"

echo "== 5. served PWA code and locales"
APPJS="$(curl -sk -m 30 "$BASE/app.js")"
ck "$(grep -q 'checks_card_title' <<<"$APPJS"; echo $?)" "served app.js carries the checks card"
ck "$(grep -q 'ship_card_title' <<<"$APPJS"; echo $?)" "served app.js carries the ship card"
LOC_OK=0
for l in en de es fr ja; do
  B="$(curl -sk -m 30 "$BASE/locales/$l.json")"
  if ! grep -q 'checks_card_title' <<<"$B" || ! grep -q 'settings_ship_ci_workflows' <<<"$B"; then LOC_OK=1; fi
done
ck "$LOC_OK" "all five served locale bundles carry the checks and ship keys"

echo "== 6. cleanup"
td_stop_all >/dev/null 2>&1
ck "$(curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; [[ $? != 0 ]] && echo 0 || echo 1)" "the sandbox daemon is gone"

echo "result: $((TOTAL-FAILS)) of $TOTAL passed"
{
  echo "# Checks and ship live check — $(date '+%Y-%m-%d')"
  echo
  echo "Run by \`scripts/checks-and-ship-live-check.sh\` against a **sandbox daemon** (generated config, loopback ports, own data directory), source revision \`$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null)\`. The release number is not recorded here."
  echo
  echo "**Result: $((TOTAL-FAILS)) of $TOTAL checks passed.**"
  echo
  echo "$LINES"
  echo "## Not run live"
  echo
  echo "- The ship step itself (push, annotated tag, CI watch) and the pre-start block need a finished or started Automaton driven by a model, which a sandbox has none of. They are covered in-process by \`TestChecksShipIntegration\` (\`cmd/datawatch/checks_ship_integration_test.go\`) against a local bare remote, and are **not** validated on a live daemon."
  echo "- The MCP tools and the chat verbs are covered by unit tests only; the PWA cards are covered by node tests and by the served-code check above, not by opening a browser."
} > "$REPORT"
[[ $FAILS == 0 ]]
