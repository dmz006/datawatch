#!/usr/bin/env bash
# slot-reconcile-live-check.sh — live check of the capacity slot reconcile
# grace behavior on a SANDBOX daemon: a seeded judge slot on the pi-prompt
# pool (llm:pi-prompt) stays held while the slot-reconcile grace is
# unelapsed, is freed by the reconcile once it has run out, and the
# "Capacity slot freed" alert is raised exactly once (the reconcile is
# idempotent — later ticks free nothing).
#
# How the orphan slot is produced: the daemon-under-test is started with
# DATAWATCH_LIVE_SEED_JUDGE_SLOT=1, the test-only seam in
# cmd/datawatch/capacity_slot_reconcile.go that acquires exactly one judge
# lease ("judge:livecheck-sr") on the llm:pi-prompt pool. That lease holds
# no node pool, binds no session, and no Automaton exists for its unit, so:
#   - the general reaper (cmd/datawatch/capacity_reap.go) keeps it alive —
#     with no bound session the spawn grace is 10 minutes, far longer than
#     this check runs; and
#   - the orphan-slot reconcile (internal/autonomous/slot_reconcile.go) is
#     the only thing that can free it: while
#     autonomous.capacity_slot_reconcile_grace_seconds is unelapsed the
#     ledger still counts the fresh lease as having a node lease, and once
#     it has run out the reconcile releases the slot and raises the alert.
# The production daemon never sets the seam variable.
#
# Safety: SANDBOX only. A generated config under a throwaway directory
# inside this checkout, loopback ports, its own data directory. The
# production daemon, its service and its data are never touched, and the
# daemon is ended only by the PID scripts/test-daemon-lib.sh recorded. No
# session is started, so no multiplexer is used.
#
# Usage: env -u TMUX -u TMUX_TMPDIR bash scripts/slot-reconcile-live-check.sh
# Env:   TEST_BINARY  daemon binary (default: built from this checkout)
if [ -n "${TMUX:-}${TMUX_TMPDIR:-}" ]; then exec env -u TMUX -u TMUX_TMPDIR bash "$0" "$@"; fi
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIB="$ROOT/scripts/test-daemon-lib.sh"
S="$(mktemp -d "$ROOT/.slot-reconcile-live.XXXXXX")"
export TD_STATE_DIR="$S/tdstate"
# shellcheck source=/dev/null
source "$LIB"
slot_cleanup_extra() { rm -rf "$S" 2>/dev/null || true; }
TD_EXTRA_CLEANUP=slot_cleanup_extra
td_install_traps "$@"

for t in curl python3 jq go; do command -v "$t" >/dev/null 2>&1 || { echo "SKIP: $t missing"; exit 0; }; done
mkdir -p "$S/bin" "$S/data" "$S/projects"
if [[ -z "${TEST_BINARY:-}" ]]; then
  (cd "$ROOT" && go build -o "$S/bin/datawatch" ./cmd/datawatch/) >/dev/null 2>&1
  BINARY="$S/bin/datawatch"
else BINARY="$TEST_BINARY"
fi
[[ -x "$BINARY" ]] || { echo "SKIP: no usable daemon binary (go build failed?)"; exit 0; }

free_port() { python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }
PORT="$(free_port)"; TLS_PORT="$(free_port)"; MCP_PORT="$(free_port)"
TOKEN="dw-test-token-12345"; BASE="https://127.0.0.1:$TLS_PORT"
CFG="$S/config.yaml"; LOG="$S/daemon.log"
UNIT="livecheck-sr"; HOLDER="judge:$UNIT"
GRACE=15   # autonomous.capacity_slot_reconcile_grace_seconds (min accepted: 10)

# Same substitutions the daemon test uses; the only feature addition is the
# short slot-reconcile grace so the live check stays fast. The key is
# inserted inside the existing autonomous: block (the YAML parser rejects a
# second autonomous: key).
sed \
  -e "s|data_dir: /data|data_dir: $S/data|g" \
  -e "s|default_project_dir: /data/projects|default_project_dir: $S/projects|g" \
  -e "s|port: 8080|port: $PORT|g" -e "s|tls_port: 8443|tls_port: $TLS_PORT|g" \
  -e "s|sse_port: 9090|sse_port: $MCP_PORT|g" -e "s|host: 0\.0\.0\.0|host: 127.0.0.1|g" \
  -e "s|token: \"\"|token: \"$TOKEN\"|g" -e "s|token: \"WEBHOOK_NOAUTH\"|token: \"\"|g" \
  "$ROOT/testdata/datawatch.yaml" > "$CFG"
# Insert the two functional keys inside the existing autonomous: block
# (the YAML parser rejects a second autonomous: key).
python3 - "$CFG" "$GRACE" <<'PY'
import sys
cfg, grace = sys.argv[1], sys.argv[2]
lines = open(cfg).read().splitlines(keepends=True)
out = []
for l in lines:
    out.append(l)
    if l.rstrip() == 'autonomous:':
        out.append('  capacity_slot_reconcile_grace_seconds: %s\n' % grace)
        out.append('  capacity_adopt_before_admit: false\n')
open(cfg, 'w').write(''.join(out))
PY

api() { local m="$1" p="$2" b="${3:-}"; curl -sk -m 30 -X "$m" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${b:+-d "$b"} "$BASE$p"; }

TOTAL=0; FAILS=0
ck() { # ck <rc> <what> [evidence]
  TOTAL=$((TOTAL+1)); local v=PASS; [[ "$1" == 0 ]] || { v=FAIL; FAILS=$((FAILS+1)); }
  echo "  $v  $2"; [[ -n "${3:-}" ]] && echo "       ${3:0:220}"
}
rc_of() { "$@" >/dev/null 2>&1; }

echo "== daemon"
# The seam reads the environment in the daemon process; export before start.
export DATAWATCH_LIVE_SEED_JUDGE_SLOT=1 DATAWATCH_LIVE_SEED_JUDGE_UNIT="$UNIT"
if ! td_start "$BINARY" "$CFG" "$LOG"; then
  echo "FAIL: sandbox daemon did not start: $(tail -5 "$LOG" 2>/dev/null)"
  exit 1
fi
td_set_api "$TD_LAST_PID" "$BASE" "$TOKEN"
end=$((SECONDS+60)); ok=0
while ((SECONDS<end)); do
  if curl -sk -m 3 -o /dev/null -f "$BASE/api/health"; then ok=1; break; fi
  sleep 1
done
((ok)) || { echo "FAIL: sandbox daemon never answered /api/health: $(tail -5 "$LOG" 2>/dev/null)"; exit 1; }
grep -q "live-check seeded judge slot holder=$HOLDER pool=llm:pi-prompt" "$LOG"
ck $? "the test seam seeded exactly one judge slot (holder $HOLDER, pool llm:pi-prompt)"

# seed_lease → the seeded holder's lease from GET /api/capacity, or "".
seed_lease() {
  api GET "/api/capacity" | jq -c --arg h "$HOLDER" \
    '.leases[]? | select(.holder==$h) | select(.pools | index("llm:pi-prompt"))' 2>/dev/null | head -n1
}
echo "== 1. held while the grace is unelapsed"
EARLY="$(seed_lease || true)"
R1=1
if [[ -n "${EARLY:-}" ]] && jq -e '.holder=="'"$HOLDER"'" and (.pools | index("llm:pi-prompt")) != null' <<<"$EARLY" >/dev/null 2>&1; then R1=0; fi
ck "$R1" \
   "the judge lease is still held on llm:pi-prompt (nothing freed it before grace — the reaper is not in play)" "${EARLY:-<absent>}"
N0="$(api GET '/api/alerts' | jq '[.alerts[]? | select(.title=="Capacity slot freed")] | length' 2>/dev/null || echo '?')"
ck "$( [[ "$N0" == "0" ]] && echo 0 || echo 1 )" \
   "no \"Capacity slot freed\" alert yet while the slot is still held"

echo "== 2. freed by the reconcile once the grace has run out"
# The reconcile runs once every 10s in the capacity loop; the slot frees at
# the first tick after the grace, so the bound is the grace plus two ticks.
end=$((SECONDS+GRACE+30)); freed=""
while ((SECONDS<end)); do
  if [[ -z "$(seed_lease)" && -z "$(seed_lease)" ]]; then freed=yes; break; fi
  sleep 2
done
N_FREE="$(api GET '/api/alerts' | jq '[.alerts[]? | select(.title=="Capacity slot freed")] | length' 2>/dev/null)"
ck "$( [[ -n "$freed" ]] && echo 0 || echo 1 )" \
   "the judge slot was freed within $((GRACE+30)) s — by the reconcile, which is the only freer left" \
   "lease=$(seed_lease || true) freed-alerts-so-far=${N_FREE:-0}"

echo "== 3. the \"Capacity slot freed\" alert was raised exactly once"
ALERTS="$(api GET '/api/alerts')"
N="$(jq '[.alerts[]? | select(.title=="Capacity slot freed")] | length' <<<"$ALERTS" 2>/dev/null)"
ALERT="$(jq -c '[.alerts[]? | select(.title=="Capacity slot freed")][-1] // empty' <<<"$ALERTS" 2>/dev/null)"
ck "$( [[ "$N" == "1" ]] && echo 0 || echo 1 )" \
   "exactly one \"Capacity slot freed\" alert (freed once; subsequent 10s ticks free nothing)" "count=$N"
# The body humanizes the holder ("Judging: (untitled …)"), so assert on the pool name and the free reason.
R5=1
if [[ -n "${ALERT:-}" ]] && jq -e '.level=="warn"
  and (.body | contains("llm:pi-prompt"))
  and (.body | contains("no live session and no node lease"))' <<<"$ALERT" >/dev/null 2>&1; then R5=0; fi
ck "$R5" \
   "the alert is warn-level, names the pi-prompt pool and the free reason" "${ALERT:-<no alert>}"

echo
echo "result: $((TOTAL-FAILS)) of $TOTAL checks passed"
if ((TOTAL > 0)); then
  if ((FAILS > 0)); then
    echo "result: FAILED ($FAILS failing)"
  else
    echo "result: OK (all checks passed)"
  fi
fi

echo "== cleanup"
td_stop_all >/dev/null 2>&1
sleep 1
GONE=1; curl -sk -m 3 -o /dev/null -f "$BASE/api/health" 2>/dev/null && GONE=0
ck "$( [[ $GONE == 1 ]] && echo 0 || echo 1 )" "the sandbox daemon is gone"
exit $((FAILS > 0 ? 1 : 0))
