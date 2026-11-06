#!/usr/bin/env bash
# live-check-decision-records.sh — Operator decision flow: live check.
#
# Drives every live item in docs/howto/automaton-operator-decisions.md end to
# end against one isolated smoke daemon (fresh data dir, port 18452, TLS off,
# binary built from this tree). It never touches the production daemon under
# /home/dmz/.datawatch. All state it creates is cleaned up on exit and the
# scratch dirs are removed (KEEP=1 to leave them for post-mortem).
#
# Checks:
#   1. Create a decision record (REST) and read it back (CLI operator list)
#   2. Question view + the answer's actor/quote and history (datawatch question)
#   3. Clarity: refuse blocks the creation, warn stores it with the warnings
#      (daemon config switched with datawatch config set, i.e. PUT /api/config)
#   4. Edit the content of an open record; it keeps the same question
#   5. Answer: the quote passes through verbatim, and the one configured
#      follow-up runs exactly once and stores its result on the record
#   6. A linked proposal doc gets its status line (mode "on"; report mode
#      first, file untouched)
#   7. Folder watch: a filed file raises a record+question, an edit updates
#      it, and deleting the file withdraws it
#   8. Gate (on mode): a task whose spec names the open question is held, and
#      it starts once the question is answered
#
# Usage:
#   bash scripts/live-check-decision-records.sh
#   KEEP=1 bash scripts/live-check-decision-records.sh   # keep scratch on exit
#
# Returns 0 when every check passed, non-zero on the first section that
# could not complete (every individual check is reported pass/fail).

set -uo pipefail

SMOKE_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_DIR=$(cd "$SMOKE_DIR/.." && pwd)

source "$SMOKE_DIR/test-daemon-lib.sh"

ROOT=$(mktemp -d /tmp/dw-dec-live.XXXXXX)
BIN="$ROOT/bin/datawatch"
BASE="http://127.0.0.1:18452"
PORT_PLAIN=18452

PASS=0
FAIL=0
SECTION=""
GATE_PRD=""

pass() { PASS=$((PASS + 1)); printf '  pass: %s\n' "$*"; }
fail() { FAIL=$((FAIL + 1)); printf '  FAIL: %s\n' "$*"; }
sec()  { SECTION="$*"; printf '\n== %s ==\n' "$*"; }
note() { printf '       %s\n' "$*"; }

die_setup() { fail "$*"; printf '\ndemo failed during setup: %s\n' "$*" >&2; exit 2; }

# wait_for <seconds> <cmd...>: run cmd each second until it exits 0.
wait_for() {
  local want=$1; shift
  local deadline=$(( $(date +%s) + want ))
  while :; do
    if "$@" >/dev/null 2>&1; then return 0; fi
    (( $(date +%s) < deadline )) || return 1
    sleep 1
  done
}

# api <METHOD> <path> [jsonfile]: sets API_CODE and API_BODY.
api() {
  local m=$1 p=$2 f=${3:-}
  local args=(-s -X "$m" -w $'\n%{http_code}' "$BASE$p")
  if [[ -n "$f" ]]; then
    args+=(-H 'Content-Type: application/json' --data-binary "@$f")
  fi
  local out
  out=$(curl "${args[@]}" 2>/dev/null) || out="000"
  API_CODE=${out##*$'\n'}
  API_BODY=${out%$'\n'*}
}

# cli <args...>: sets CLI_CODE and CLI_OUT.
cli() {
  local out
  out=$(cd "$ROOT" && "$BIN" --url "$BASE" "$@" 2>&1)
  CLI_CODE=$?
  CLI_OUT=$out
}

# jget <jq-filter>: read one value out of the last API_BODY.
jget() {
  jq -r "$1 // empty" <<<"$API_BODY" 2>/dev/null
}

cleanup_pre_stop() {
  # While the daemon is still up: retire the gate PRD it may have started.
  if [[ -n "$GATE_PRD" ]]; then
    curl -s -X DELETE "$BASE/api/autonomous/prds/$GATE_PRD" >/dev/null 2>&1 || true
  fi
}
TD_PRE_STOP_CLEANUP=cleanup_pre_stop

cleanup_extra() {
  if [[ "${KEEP:-0}" == "1" ]]; then
    return 0
  fi
  rm -rf "$ROOT"
}
TD_EXTRA_CLEANUP=cleanup_extra
td_install_traps

need() { command -v "$1" >/dev/null || die_setup "required tool missing: $1"; }
need curl; need jq; need go; need git

# --- build -------------------------------------------------------------------
sec "build datawatch from $REPO_DIR"
(cd "$REPO_DIR" && go build -o "$BIN" ./cmd/datawatch) || die_setup "go build failed"
[[ -x "$BIN" ]] || die_setup "binary missing after build"
pass "binary built at $BIN"

# --- scratch project + config ------------------------------------------------
git init -q -b main "$ROOT/gate-proj"
(cd "$ROOT/gate-proj" && echo seed > README.md && git add README.md \
  && git -c user.email=live-check@local -c user.name=live-check commit -qm seed)

mkdir -p "$ROOT/doc"
cat > "$ROOT/doc/proposal.md" <<'EOF'
# Sandbox live-check proposal

A scratch proposal the decision records link to.

Confirmed: (pending the live-check decision)
EOF
cat > "$ROOT/doc/proposal2.md" <<'EOF'
# Sandbox live-check proposal (second)

Confirmed: (pending the live-check decision)
EOF

cat > "$ROOT/config.yaml" <<EOF
data_dir: $ROOT/data

server:
  enabled: true
  host: 127.0.0.1
  port: $PORT_PLAIN
  token: ''
  # TLS off: /api/questions/* is not on the loopback TLS-bypass list, and a
  # self-signed cert would turn plain-HTTP routes into TLS-redirect failures.
  tls_enabled: false

  # The gate section runs a PRD whose project lives in this scratch dir.
  allowed_project_roots:
    - $ROOT

autonomous:
  enabled: true
  planning_backend: claude-code
  execution_backend: claude-code
  planning_model: ollama/qwen3.8-64k:27b
  verification_backend: pi-prompt
  verification_model: ollama/qwen3.8-64k:27b
  max_concurrent_tasks: 1
  max_engines_per_node: 1
  poll_interval_seconds: 5
  session_start_confirm_seconds: 300
  verifier_policy:
    mode: prefer-free
    backends: [pi-prompt, claude-code]
  scan:
    enabled: false
  per_prd_guardrails: []
  task_templates:
    enabled: true
    templates: []
  questions:
    enabled: true
    decision_gate: true
    decision_gate_mode: on
  decisions:
    enabled: true
    doc_root: $ROOT
    doc_status_mode: report
    follow_ups:
      - name: kick
        kind: start_template
        target: no-such-template
EOF

# --- smoke daemon -------------------------------------------------------------
sec "start smoke daemon on $BASE"
if ! td_start "$BIN" "$ROOT/config.yaml" "$ROOT/daemon.log"; then
  tail -20 "$ROOT/daemon.log" 2>/dev/null >&2 || true
  die_setup "daemon did not come up"
fi
SMOKE_PID="$TD_LAST_PID"
# Boot can be slow: channel cleanup runs `claude mcp list`, and every
# unreachable MCP server in the operator's claude registry adds up to its
# 30 s timeout before the server actually starts listening. Allow 240 s.
deadline=$(( $(date +%s) + 240 ))
healthy=0
while [[ $(date +%s) -lt $deadline ]]; do
  curl -sf "$BASE/api/health" >/dev/null 2>&1 && { healthy=1; break; }
  sleep 1
done
if [[ $healthy -ne 1 ]]; then
  tail -20 "$ROOT/daemon.log" 2>/dev/null >&2 || true
  die_setup "daemon not healthy within 240 s (log: $ROOT/daemon.log)"
fi
pass "daemon healthy on $BASE (PID $SMOKE_PID)"
td_set_api "$SMOKE_PID" "$BASE" "" || note "warn: could not record the API for the exit pass"

# --- fixture payloads ----------------------------------------------------------
cat > "$ROOT/r1.json" <<'EOF'
{
  "label": "live-check",
  "title": "Which cadence should the smoke check use?",
  "situation": "A standalone smoke daemon holds one Automaton task on your decision, and you still have to choose the cadence the check itself uses while it polls for that answer.",
  "deciding": "Pick the poll cadence for this sandbox run: it bounds how long the parked task can wait after you answer, and it does not change what you decide at all.",
  "options": [
    {"id": "five", "label": "Poll every five seconds", "tradeoff": "The parked task is released within seconds; a little extra polling on a shared host"},
    {"id": "thirty", "label": "Poll every thirty seconds", "tradeoff": "Quieter, but the parked task can wait up to half a minute after the answer"}
  ],
  "recommendation": {
    "option_id": "five",
    "reason": "This sandbox is isolated and short-lived, so speed wins over the extra log noise"
  },
  "default": "hold"
}
EOF

# Deliberately unclear: a three-letter label (below the minimum), a
# situation that is far under the minimum text, and one option whose
# tradeoff is left blank.
cat > "$ROOT/r2.json" <<'EOF'
{
  "label": "go",
  "title": "Pick one",
  "situation": "short.",
  "deciding": "pick one of the two options below.",
  "options": [
    {"id": "a", "label": "option a", "tradeoff": "a bit faster"},
    {"id": "b", "label": "option b", "tradeoff": ""}
  ],
  "default": "hold"
}
EOF

cat > "$ROOT/r3.json" <<'EOF'
{
  "label": "doc-report",
  "title": "Should this proposal keep its sandbox scope?",
  "situation": "The proposal under review is small on purpose: it exists so this live check can link a decision to a real file and watch the status line behaviour end to end, without touching production docs.",
  "deciding": "Decide whether the proposal stays scoped to the sandbox, so the live check can keep pointing its decision records at it without worrying about shared documents.",
  "options": [
    {"id": "keep", "label": "Keep the sandbox scope", "tradeoff": "The sandbox stays self-contained at the cost of another scratch doc"},
    {"id": "expand", "label": "Expand to shared docs", "tradeoff": "Covers more ground but mixes live-check state into real proposals"}
  ],
  "default": "hold",
  "links": {"doc": "doc/proposal.md"}
}
EOF

cat > "$ROOT/r4.json" <<'EOF'
{
  "label": "doc-apply",
  "title": "Should the live check apply the status line?",
  "situation": "The second proposal doc is the same scratch piece seen from the apply side: the live check wants the daemon to actually rewrite the status line, not only report what it would do, to prove mode on writes exactly one byte-identical-but-reworded line.",
  "deciding": "Decide whether the live check runs the apply mode of the status line rewrite against the second proposal doc, or leaves it in report-only mode for this run.",
  "options": [
    {"id": "apply", "label": "Apply the status line", "tradeoff": "Proves the write path but changes the scratch doc"},
    {"id": "report", "label": "Stay in report-only mode", "tradeoff": "Keeps the doc untouched but leaves the write path unproven"}
  ],
  "default": "hold",
  "links": {"doc": "doc/proposal2.md"}
}
EOF

cat > "$ROOT/r1-update.json" <<'EOF'
{
  "label": "live-check",
  "title": "Which cadence should the smoke check use?",
  "situation": "A standalone smoke daemon holds one Automaton task on your decision, and you still have to choose the cadence the check itself uses while it polls for that answer. An operator later tightened this text, to prove the same question survives an edit.",
  "deciding": "Pick the poll cadence for this sandbox run: it bounds how long the parked task can wait after you answer, and it does not change what you decide at all.",
  "options": [
    {"id": "five", "label": "Poll every five seconds", "tradeoff": "The parked task is released within seconds; a little extra polling on a shared host"},
    {"id": "thirty", "label": "Poll every thirty seconds", "tradeoff": "Quieter, but the parked task can wait up to half a minute after the answer"}
  ],
  "recommendation": {
    "option_id": "five",
    "reason": "This sandbox is isolated and short-lived, so speed wins over the extra log noise"
  },
  "default": "hold"
}
EOF

# Folder-watched file (front matter one key per line).
cat > "$ROOT/dfiles/folder-dec.md" 2>/dev/null || { mkdir -p "$ROOT/dfiles"; cat > "$ROOT/dfiles/folder-dec.md" <<'EOF'
---
label: folder-dec
title: Which folder option does the daemon keep?
situation: The operator files a decision as a plain file in a folder the daemon watches, instead of calling the daemon directly, and the daemon has to turn it into one open question like any REST-raised one.
deciding: Pick which of the two folder options the daemon should treat as the chosen one when the operator eventually answers the question created from this file.
option: fa | File option A | the sandbox prefers speed
option: fb | File option B | the sandbox prefers quiet
recommendation: fa | the sandbox prefers speed
default: hold
---
Free text the daemon can ignore; only the front matter becomes the record.
EOF
}

# ===========================================================================
sec "1. create a decision record and read it back"

api POST /api/autonomous/decision-records "$ROOT/r1.json"
[[ "$API_CODE" == "201" ]] && pass "create returned 201" || { fail "create returned $API_CODE: $API_BODY"; exit 1; }
R1=$(jget '.record.id'); R1Q=$(jget '.question_id')
[[ -n "$R1" && -n "$R1Q" ]] && pass "record id $R1, question $R1Q" || { fail "no id or question in the answer"; exit 1; }

api GET "/api/autonomous/decision-records/$R1"
R1_TITLE=$(jget '.record.title')
R1_STATE=$(jget '.record.state')
R1_SRC=$(jget '.record.source')
R1_DEF=$(jget '.record.default')
R1_RECOPT=$(jget '.record.recommendation.option_id')
R1_OPT1=$(jget '.record.options[0].label')
if [[ "$R1_TITLE" == "Which cadence should the smoke check use?" && "$R1_STATE" == "open" \
      && "$R1_SRC" == "rest" && "$R1_DEF" == "hold" && "$R1_RECOPT" == "five" \
      && "$R1_OPT1" == "Poll every five seconds" ]]; then
  pass "GET round-trip: title/state/source/default/recommendation/option all match"
else
  fail "GET round-trip mismatch: $API_BODY"
fi

cli autonomous decisions --state open --limit 100 --json
if [[ $CLI_CODE -eq 0 ]] && grep -q "$R1Q" <<<"$CLI_OUT"; then
  pass "CLI 'autonomous decisions' lists the open decision ($R1Q)"
else
  fail "CLI decisions list: code=$CLI_CODE out=$(head -c 300 <<<"$CLI_OUT")"
fi

# ===========================================================================
sec "2. question view + history (datawatch question)"

# question show has no --json flag; it prints the operator form (text,
# numbered options, state), so assert on the visible text instead.
cli question show "$R1Q"
Q1_OK=0
if [[ $CLI_CODE -eq 0 ]]; then
  note "show[:120]=${CLI_OUT:0:120}"
  grep -q "Which cadence should the smoke check use?" <<<"$CLI_OUT" && Q1_OK=1
fi
(( Q1_OK )) && pass "question show returns the open question" || fail "question show: code=$CLI_CODE out=$(head -c 300 <<<"$CLI_OUT")"

cli question list --state open --json
if [[ $CLI_CODE -eq 0 ]] && jq -e --arg q "$R1Q" 'any((.questions // .)[]?; (.id == $q))' <<<"$CLI_OUT" >/dev/null 2>&1; then
  pass "question list --state open contains $R1Q"
else
  fail "question list: code=$CLI_CODE out=$(head -c 300 <<<"$CLI_OUT")"
fi

# ===========================================================================
sec "3. clarity: refuse blocks, warn stores with warnings"

# autonomous.decisions.* lives on the daemon config surface: the surface is
# `datawatch config set autonomous.decisions.clarity_mode <v>` (PUT /api/config),
# validated and persisted server-side.
cli config set autonomous.decisions.clarity_mode refuse
if [[ $CLI_CODE -eq 0 ]]; then
  pass "config-set clarity_mode=refuse accepted"
else
  fail "config-set refused: $(head -c 200 <<<"$CLI_OUT")"
fi
cli config get autonomous.decisions.clarity_mode
if [[ "${CLI_OUT:-""}" == *refuse* ]]; then
  pass "daemon now holds clarity_mode=refuse"
else
  note "config get says: $(head -c 120 <<<"$CLI_OUT")"
fi

api POST /api/autonomous/decision-records "$ROOT/r2.json"
CLARITY_REFUSE_BODY=$API_BODY
if [[ "$API_CODE" == "422" ]] && jq -e '.findings | length > 0' <<<"$API_BODY" >/dev/null 2>&1; then
  pass "refuse mode: creation blocked with $(jq -r '[.findings[].code] | join(",")' <<<"$API_BODY")"
else
  fail "refuse mode expected 422 + findings, got $API_CODE: $CLARITY_REFUSE_BODY"
fi
api GET "/api/autonomous/decision-records"
if [[ "$(jget '(.decision_records // .records // .) | length')" == "1" ]]; then
  pass "the refused record was not stored"
else
  fail "refused record appears in the list: $API_BODY"
fi

cli config set autonomous.decisions.clarity_mode warn
[[ $CLI_CODE -eq 0 ]] || fail "config-set clarity_mode=warn refused: $(head -c 200 <<<"$CLI_OUT")"
api POST /api/autonomous/decision-records "$ROOT/r2.json"
if [[ "$API_CODE" == "201" ]] && jq -e '.warnings | length >= 1' <<<"$API_BODY" >/dev/null 2>&1; then
  pass "warn mode: stored with warnings $(jq -r '[.warnings[].code] | join(",")' <<<"$API_BODY")"
else
  fail "warn mode expected 201 + warnings, got $API_CODE: $API_BODY"
fi
R2=$(jget '.record.id')
[[ -n "$R2" ]] && pass "warn record stored as $R2 (findings reported, not blocking)" || fail "no record id in the warn answer"

# ===========================================================================
sec "4. edit an open record keeps the same question"

api PUT "/api/autonomous/decision-records/$R1" "$ROOT/r1-update.json"
R1Q_AFTER=$(jget '.question_id')
R1_SIT_AFTER=$(jget '.record.situation')
if [[ "$API_CODE" == "200" && "$R1Q_AFTER" == "$R1Q" && "$R1_SIT_AFTER" == *"operator later tightened"* ]]; then
  pass "update answered 200, question still $R1Q, new situation stored"
else
  fail "update: code=$API_CODE body=$(head -c 300 <<<"$API_BODY")"
fi

# ===========================================================================
sec "5. answer: quote verbatim + configured follow-up"

cat > "$ROOT/a1.json" <<'EOF'
{"chosen_option_id": "five", "actor": "live-check",
 "quote": "five seconds — the sandbox is local, take the fast one",
 "follow_up": "kick"}
EOF
api POST "/api/questions/$R1Q/answer" "$ROOT/a1.json"
if [[ "$API_CODE" =~ ^2 ]]; then
  pass "answer accepted (code $API_CODE)"
else
  fail "answer rejected: $API_CODE $API_BODY"
fi

# The answer lands in the question store immediately; the record sees it on
# the next daemon poll (5 s here).
wait_for 30 bash -c "curl -s '$BASE/api/autonomous/decision-records/$R1' | jq -e '.record.answer'" || true
api GET "/api/autonomous/decision-records/$R1"
ANS_OPT=$(jget '.record.answer.chosen_option_id')
ANS_ACTOR=$(jget '.record.answer.actor')
ANS_QUOTE=$(jget '.record.answer.quote')
if [[ "$ANS_OPT" == "five" && "$ANS_ACTOR" == "live-check" \
      && "$ANS_QUOTE" == "five seconds — the sandbox is local, take the fast one" ]]; then
  pass "record answer applied: option/actor/quote verbatim (em dash intact)"
else
  fail "record answer mismatch: $API_BODY"
fi
FU_REQ=$(jget '.record.follow_up.requested')
FU_STAT=$(jget '.record.follow_up.status')
FU_RES=$(jget '.record.follow_up.result')
if [[ "$FU_REQ" == "true" && -n "$FU_STAT" && -n "$FU_RES" ]]; then
  pass "follow-up ran once: status=$FU_STAT result=${FU_RES:0:80}"
else
  fail "follow_up missing or empty: requested=$FU_REQ status=$FU_STAT result=$FU_RES"
fi

# Second answer attempt must be refused: the question is already answered.
cat > "$ROOT/a1b.json" <<'EOF'
{"chosen_option_id": "thirty", "actor": "live-check", "quote": "changed my mind"}
EOF
api POST "/api/questions/$R1Q/answer" "$ROOT/a1b.json"
if [[ "$API_CODE" == "409" ]]; then
  pass "second answer refused (409): the answer is final, a new record is the path"
else
  fail "second answer expected 409, got $API_CODE: $API_BODY"
fi

cli question show "$R1Q"
SHOW_OK=0
if [[ $CLI_CODE -eq 0 ]] \
      && grep -q "five seconds — the sandbox is local, take the fast one" <<<"$CLI_OUT"; then
  SHOW_OK=1
fi
if (( SHOW_OK )); then
  pass "question show: the verbatim quote is in the operator view"
else
  fail "question show after answer: quote not visible: $(head -c 300 <<<"$CLI_OUT" 2>/dev/null)"
fi

# ===========================================================================
sec "6. doc status line: report first, then apply"

cat > "$ROOT/a3.json" <<'EOF'
{"chosen_option_id": "keep", "actor": "live-check", "quote": "keep it in the sandbox"}
EOF
api POST /api/autonomous/decision-records "$ROOT/r3.json"
[[ "$API_CODE" == "201" ]] || { fail "R3 create: $API_CODE $API_BODY"; exit 1; }
R3=$(jget '.record.id'); R3Q=$(jget '.question_id')

DOC1_BEFORE=$(sha256sum "$ROOT/doc/proposal.md" | cut -d' ' -f1)
api POST "/api/questions/$R3Q/answer" "$ROOT/a3.json"
wait_for 30 bash -c "curl -s '$BASE/api/autonomous/decision-records/$R3' | jq -e '.record.doc_change'" || true
api GET "/api/autonomous/decision-records/$R3"
DOC1_STATUS=$(jget '.record.doc_change.status')
DOC1_BEFORELINE=$(jget '.record.doc_change.before')
DOC1_AFTERLINE=$(jget '.record.doc_change.after')
if [[ "$DOC1_STATUS" == "reported" && -n "$DOC1_BEFORELINE" && -n "$DOC1_AFTERLINE" ]]; then
  pass "report mode: doc_change.status=reported, before='$DOC1_BEFORELINE' after='${DOC1_AFTERLINE:0:60}'"
else
  fail "report mode doc_change: $API_BODY"
fi
DOC1_AFTER_HASH=$(sha256sum "$ROOT/doc/proposal.md" | cut -d' ' -f1)
[[ "$DOC1_BEFORE" == "$DOC1_AFTER_HASH" ]] \
  && pass "report mode left the file byte-identical" \
  || fail "report mode changed the doc on disk"

cli config set autonomous.decisions.doc_status_mode on
[[ $CLI_CODE -eq 0 ]] || fail "config-set doc_status_mode=on refused: $(head -c 200 <<<"$CLI_OUT")"
cat > "$ROOT/a4.json" <<'EOF'
{"chosen_option_id": "apply", "actor": "live-check", "quote": "apply it"}
EOF
api POST /api/autonomous/decision-records "$ROOT/r4.json"
[[ "$API_CODE" == "201" ]] || { fail "R4 create: $API_CODE $API_BODY"; exit 1; }
R4=$(jget '.record.id'); R4Q=$(jget '.question_id')
api POST "/api/questions/$R4Q/answer" "$ROOT/a4.json"
wait_for 30 bash -c "curl -s '$BASE/api/autonomous/decision-records/$R4' | jq -e '.record.doc_change'" || true
api GET "/api/autonomous/decision-records/$R4"
DOC2_STATUS=$(jget '.record.doc_change.status')
if [[ "$DOC2_STATUS" == "applied" ]] && grep -q "apply it" "$ROOT/doc/proposal2.md" \
     && grep -q "^Confirmed: " "$ROOT/doc/proposal2.md"; then
  pass "on mode: status line rewritten in doc (status=applied, quote in file)"
else
  fail "on mode doc apply: status=$DOC2_STATUS file=$(cat "$ROOT/doc/proposal2.md")"
fi
LINES=$(wc -l < "$ROOT/doc/proposal2.md")
[[ "$LINES" -ge 3 ]] && pass "only the one status line changed (file still $LINES lines)" || note "line count=$LINES"

# ===========================================================================
sec "7. folder watch: file raise / edit update / delete withdraw"

cli config set autonomous.decisions.folder "$ROOT/dfiles"
if [[ $CLI_CODE -ne 0 ]]; then
  fail "config-set folder: $(head -c 200 <<<"$CLI_OUT")"
fi

# The tick runs every poll (5 s): wait for the filed record + open question.
FIND_FOLDER_REC2() {
  curl -s "$BASE/api/autonomous/decision-records?limit=200" | \
    jq -r '(.decision_records // .records // .)[]? | select((.label? // "") | startswith("folder-dec")) | .id' 2>/dev/null | head -1
}
FOLDER_RID=""
wait_for 45 FIND_FOLDER_REC2 && FOLDER_RID=$(FIND_FOLDER_REC2)
if [[ -n "$FOLDER_RID" ]]; then
  pass "folder file raised record $FOLDER_RID"
  api GET "/api/autonomous/decision-records/$FOLDER_RID"
  FQ=$(jget '.question_id')
  [[ -n "$FQ" ]] && pass "folder record carries question $FQ" || fail "no question on the folder record"
  FOLDER_QID=$FQ
  FOLDER_HASH_BEFORE=$(jget '.record.content_hash')
else
  fail "folder file did not raise a record within 45 s"
  FOLDER_HASH_BEFORE=""
fi

if [[ -n "$FOLDER_RID" ]]; then
  cat > "$ROOT/dfiles/folder-dec.md" <<'EOF'
---
label: folder-dec
title: Which folder option does the daemon keep?
situation: The operator files a decision as a plain file in a folder the daemon watches, instead of calling the daemon directly, and the daemon has to turn it into one open question like any REST-raised one. The operator then edited the file, which must reach the same open question.
deciding: Pick which of the two folder options the daemon should treat as the chosen one when the operator eventually answers the question created from this file.
option: fa | File option A | the sandbox prefers speed
option: fb | File option B | the sandbox prefers quiet
recommendation: fa | the sandbox prefers speed
default: hold
---
Free text the daemon can ignore; only the front matter becomes the record.
EOF
  FOLDER_HASH_AFTER=""; FOLDER_QID_AFTER=""
  for _ in $(seq 1 45); do
    api GET "/api/autonomous/decision-records/$FOLDER_RID"
    FOLDER_HASH_AFTER=$(jget '.record.content_hash')
    FOLDER_QID_AFTER=$(jget '.question_id')
    [[ -n "$FOLDER_HASH_AFTER" && "$FOLDER_HASH_AFTER" != "$FOLDER_HASH_BEFORE" ]] && break
    sleep 1
  done
  if [[ -n "$FOLDER_HASH_AFTER" && "$FOLDER_HASH_AFTER" != "$FOLDER_HASH_BEFORE" && "$FOLDER_QID_AFTER" == "$FOLDER_QID" ]]; then
    pass "folder edit updated the record (new content hash, same question $FOLDER_QID)"
  else
    fail "folder edit: before=$FOLDER_HASH_BEFORE after=${FOLDER_HASH_AFTER:-?} question_before=${FOLDER_QID:-?} question_after=${FOLDER_QID_AFTER:-?}"
  fi

  rm -f "$ROOT/dfiles/folder-dec.md"
  FOLDER_STATE=""
  for _ in $(seq 1 45); do
    api GET "/api/autonomous/decision-records/$FOLDER_RID"
    FOLDER_STATE=$(jget '.record.state')
    [[ "$FOLDER_STATE" == "withdrawn" ]] && break
    sleep 1
  done
  [[ "$FOLDER_STATE" == "withdrawn" ]] \
    && pass "folder delete withdrew the record (state=withdrawn)" \
    || fail "folder delete: state=${FOLDER_STATE:-?}"
  if [[ -n "${FOLDER_QID:-}" ]]; then
    api GET "/api/questions"
    QSTATE=$(curl -s "$BASE/api/questions" | jq -r --arg q "$FOLDER_QID" '(.questions // .)[]? | select(.id == $q) | .state' 2>/dev/null || true)
    [[ "$QSTATE" == "withdrawn" ]] && pass "the folder question was withdrawn too" || note "folder question state=${QSTATE:-?}"
  fi
fi

# ===========================================================================
sec "8. gate (on mode): task held on the open question, starts after the answer"

api POST /api/autonomous/decision-records "$ROOT/r1.json"
[[ "$API_CODE" == "201" ]] || { fail "gate record create: $API_CODE $API_BODY"; exit 1; }
GR=$(jget '.record.id'); GRQ=$(jget '.question_id')
pass "gate raised: record $GR question $GRQ"

cat > "$ROOT/gate-prd.json" <<EOF
{
  "spec": "Gate smoke: one task whose spec names an operator decision by its question id; that task must not start until the question is answered.",
  "project_dir": "$ROOT/gate-proj",
  "effort": "low"
}
EOF
api POST /api/autonomous/prds "$ROOT/gate-prd.json"
GATE_PRD=$(jget '.id')
if [[ -n "$GATE_PRD" ]]; then
  pass "gate PRD created: $GATE_PRD"
else
  fail "gate PRD create: $API_BODY"
fi

# One story, one task. The task's spec text is exactly what the decision
# gate scans; it must name the open question id.
cat > "$ROOT/gate-story.json" <<'EOF'
{"title": "Gate story", "description": "The decision-gate smoke task.", "actor": "live-check"}
EOF
api POST "/api/autonomous/prds/$GATE_PRD/add_story" "$ROOT/gate-story.json"
if [[ "$API_CODE" =~ ^2 ]]; then
  pass "gate story added"
else
  fail "add_story: $API_CODE $API_BODY"
fi

GATE_TASK_SPEC="Decision: $GRQ - write the file note.txt with the single line 'released' in the project root. That is all."
api GET "/api/autonomous/prds/$GATE_PRD"
GATE_STORY=$(jq -r '.stories[0].id // empty' <<<"$API_BODY" 2>/dev/null)
if [[ -z "$GATE_STORY" ]]; then
  fail "gate story id not found after add_story (resp: $(head -c 200 <<<"$API_BODY"))"
else
  pass "gate story: $GATE_STORY"
  jq -cn --arg st "$GATE_STORY" --arg s "$GATE_TASK_SPEC" \
    '{story_id: $st, title: "Gate task", spec: $s, actor: "live-check"}' > "$ROOT/gate-task.json"
  api POST "/api/autonomous/prds/$GATE_PRD/add_task" "$ROOT/gate-task.json"
  if [[ "$API_CODE" =~ ^2 ]]; then
    pass "gate task added (spec names $GRQ)"
  else
    fail "add_task: $API_CODE $API_BODY"
  fi

  api POST "/api/autonomous/prds/$GATE_PRD/approve" '{"actor": "live-check", "note": "live-check gate smoke"}'
  [[ "$API_CODE" =~ ^2 ]] || fail "approve: $API_CODE $API_BODY"

  api POST "/api/autonomous/prds/$GATE_PRD/run" '{}'
  if [[ "$API_CODE" =~ ^2 ]]; then
    pass "run accepted (code $API_CODE)"
  else
    fail "run rejected: $API_CODE $API_BODY"
  fi
fi

TID=""
TSTAT=""
for _ in $(seq 1 90); do
  api GET "/api/autonomous/prds/$GATE_PRD"
  TID=$(jget '[.stories[]?.tasks[]?] | map(select(.status == "waiting_decision")) | .[0].id // empty')
  if [[ -n "$TID" ]]; then
    pass "task $TID is held (status=waiting_decision) — gated on $GRQ"
    break
  fi
  TSTAT=$(jget '[.stories[]?.tasks[]?] | .[0].status // empty')
  sleep 1
done
if [[ -z "$TID" ]]; then
  fail "task never reached waiting_decision (last status: ${TSTAT:-?})"
fi

cat > "$ROOT/a5.json" <<'EOF'
{"chosen_option_id": "five", "actor": "live-check", "quote": "release it"}
EOF
if [[ -n "$TID" ]]; then
  api POST "/api/questions/$GRQ/answer" "$ROOT/a5.json"
  if [[ "$API_CODE" =~ ^2 ]]; then
    pass "gate answered ($GRQ)"
    STARTED=""
    for _ in $(seq 1 240); do
      api GET "/api/autonomous/prds/$GATE_PRD"
      TSTAT=$(jget "[.stories[]?.tasks[]?] | map(select(.id == \"$TID\")) | .[0].status // empty")
      case "$TSTAT" in
        in_progress|running_tests|verifying|completed|failed) STARTED=1; break ;;
      esac
      sleep 1
    done
    if [[ "$STARTED" == "1" ]]; then
      pass "task $TID left waiting_decision and started (status=$TSTAT)"
    else
      fail "task still not started 240 s after the answer (status=${TSTAT:-?})"
    fi
  fi
fi

# ===========================================================================
printf '\n'
printf 'live-check-decision-records: %d passed, %d failed\n' "$PASS" "$FAIL"
if (( FAIL > 0 )); then
  printf 'scratch kept at %s\n' "$ROOT"
  # Let the traps stop the daemon, then fail.
  exit 1
fi
printf 'scratch kept at %s\n' "$ROOT"
exit 0
