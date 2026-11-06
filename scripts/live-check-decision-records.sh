#!/usr/bin/env bash
# live-check-decision-records.sh — live check of the operator decision records.
#
# Starts one SANDBOX daemon (never the production daemon): binary built from
# this tree into a temporary directory, its own data dir and config, TLS on
# the sandbox TLS port 18443, an API token, the question channel and the
# decisions switched on. Everything the check needs is created inline through
# the API with the sandbox token, each step is recorded as PASS, FAIL or
# SKIPPED with what was observed, and the result is written to
# tests/live-smoke/decision-records-report.md.
#
# Steps:
#   1  create a record over REST, read back the one question the daemon built
#   2  one question per record across REST, the CLI list and the comm-style path
#   3  clarity refusal in refuse mode and a warning in warn mode
#   4  edit an open record; the question text changes on the same question id
#   5  answer with a multi-line quote, an unconfigured follow_up (400), then
#      without it; read the record and the list
#   6  document status line: report mode leaves the file alone, on mode
#      rewrites exactly the one line
#   7  folder watch: raise, edit, delete; repeated with another delimiter and
#      separator
#   8  a task spec naming the question id waits, then starts after the answer
#      (SKIPPED when an Automaton cannot be created in the sandbox)
#
# Cleanup on every exit path: records withdrawn, the Automaton and any session
# ended, the daemon ended by its recorded PID, temporary folders removed, then
# scripts/check-no-leftover-test-daemons.sh runs.
#
# Usage: bash scripts/live-check-decision-records.sh   (KEEP=1 keeps scratch)

if [[ -z "${LIVE_CHECK_CLEAN_ENV:-}" ]]; then
  exec env -u TMUX -u TMUX_TMPDIR LIVE_CHECK_CLEAN_ENV=1 bash "$0" "$@"
fi

set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
REPO_DIR=$(cd "$HERE/.." && pwd)
source "$HERE/test-daemon-lib.sh"

REPORT="$REPO_DIR/tests/live-smoke/decision-records-report.md"
TLS_PORT=18443
PLAIN_PORT=18444
BASE="https://127.0.0.1:$TLS_PORT"
TOK=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')

mkdir -p "$REPO_DIR/tmp-sandbox"
ROOT=$(mktemp -d "$REPO_DIR/tmp-sandbox/decision-live.XXXXXX")
BIN="$ROOT/bin/datawatch"
DOCS="$ROOT/docs"; FOLDER="$ROOT/folder"; GATEPROJ="$ROOT/gate-project"
SNAP="$ROOT/daemon-snapshot.txt"

# ---- step bookkeeping ------------------------------------------------------
STEP_IDS=(); STEP_NAMES=(); STEP_RESULTS=(); STEP_OBS=()
CUR_ID=""; CUR_NAME=""; CUR_FAIL=0; CUR_SKIP=""; CUR_OBS=""
RECS=(); GATE_PRD=""; CLEANUP_RESULT="not run"; VERSION_LINE="unknown"

step() { CUR_ID=$1; CUR_NAME=$2; CUR_FAIL=0; CUR_SKIP=""; CUR_OBS=""; printf '\n== step %s: %s ==\n' "$1" "$2"; }
ok()   { CUR_OBS+="ok: $*; "; printf '  ok:   %s\n' "$*"; }
bad()  { CUR_FAIL=1; CUR_OBS+="FAILED: $*; "; printf '  FAIL: %s\n' "$*"; }
skip() { CUR_SKIP="$*"; printf '  SKIPPED: %s\n' "$*"; }
step_end() {
  local r=PASS
  if (( CUR_FAIL )); then r=FAIL; elif [[ -n "$CUR_SKIP" ]]; then r=SKIPPED; CUR_OBS="$CUR_SKIP; $CUR_OBS"; fi
  STEP_IDS+=("$CUR_ID"); STEP_NAMES+=("$CUR_NAME"); STEP_RESULTS+=("$r"); STEP_OBS+=("$CUR_OBS")
  printf '  -> %s\n' "$r"
}
any_failed() { local r; for r in "${STEP_RESULTS[@]:-}"; do [[ "$r" == FAIL ]] && return 0; done; return 1; }

# ---- report ----------------------------------------------------------------
write_report() {
  local result=PASS i obs
  any_failed && result=FAIL
  # A run that never reached the end of the steps is not a pass.
  [[ ${#STEP_RESULTS[@]} -lt 8 ]] && result=FAIL
  mkdir -p "$(dirname "$REPORT")"
  {
    echo "# Decision records: sandbox live check"
    echo
    echo "- Date: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo "- Sandbox port: $TLS_PORT (TLS, own data directory and config)"
    echo "- Daemon version: $VERSION_LINE"
    echo
    echo "| Step | Check | Result | Observation |"
    echo "|------|-------|--------|-------------|"
    for i in "${!STEP_IDS[@]}"; do
      obs=${STEP_OBS[$i]//|/\\|}; obs=${obs//$'\n'/ }
      printf '| %s | %s | %s | %s |\n' "${STEP_IDS[$i]}" "${STEP_NAMES[$i]}" "${STEP_RESULTS[$i]}" "${obs:0:700}"
    done
    echo
    echo "## Cleanup"
    echo
    echo "$CLEANUP_RESULT"
    echo
    echo "RESULT: $result"
  } > "$REPORT"
}

# ---- API helpers -----------------------------------------------------------
# api <METHOD> <path> [json]  -> API_CODE, API_BODY
api() {
  local m=$1 p=$2 d=${3:-} out
  local args=(-sk --max-time 60 -X "$m" -w $'\n%{http_code}' -H "Authorization: Bearer $TOK")
  [[ -n "$d" ]] && args+=(-H 'Content-Type: application/json' --data-binary "$d")
  out=$(curl "${args[@]}" "$BASE$p" 2>/dev/null) || out=$'\n000'
  API_CODE=${out##*$'\n'}; API_BODY=${out%$'\n'*}
}
jg() { jq -r "$1 // empty" <<<"$API_BODY" 2>/dev/null; }
# cli <args...> -> CLI_CODE, CLI_OUT (the real binary, against the sandbox)
cli() {
  CLI_OUT=$(cd "$ROOT" && DATAWATCH_TOKEN="$TOK" DW_BASE_INSECURE=1 "$BIN" --url "$BASE" "$@" 2>&1); CLI_CODE=$?
}
cfg_put() { api PUT /api/config "$(jq -cn --arg k "$1" --argjson v "$2" '{($k):$v}')"; }
# wait_for <seconds> <function>: poll once a second.
wait_for() { local end=$(( $(date +%s) + $1 )); shift; while :; do "$@" && return 0; (( $(date +%s) < end )) || return 1; sleep 1; done; }
new_rec() { RECS+=("$1"); }
# question_of <qid>: sets API_BODY to the question (tolerates a wrapper).
get_question() { api GET "/api/questions/$1"; API_BODY=$(jq -c '(.question // .)' <<<"$API_BODY" 2>/dev/null || echo "$API_BODY"); }
rec_json() { # rec_json <label> <title> <situation-suffix> [doc]
  jq -cn --arg l "$1" --arg t "$2" --arg s "$3" --arg d "${4:-}" '
   {label:$l, title:$t,
    situation:("The sandbox live check files this decision so the daemon builds one question from it and the operator can read exactly what is being decided. " + $s),
    deciding:"Pick which of the two options below the sandbox should treat as the chosen one when the live check answers this question later on.",
    options:[{id:"fast",label:"Take the fast option",tradeoff:"Quicker to finish, at the cost of less checking"},
             {id:"slow",label:"Take the careful option",tradeoff:"Slower to finish, but every step is checked"}],
    recommendation:{option_id:"fast",reason:"The sandbox is isolated and short-lived, so speed wins"},
    default:"hold"} + (if $d=="" then {} else {links:{doc:$d}} end)'
}

# ---- cleanup: every exit path ----------------------------------------------
pre_stop_cleanup() {
  local n=0 closed=0 id
  if [[ -n "${BASE:-}" ]] && curl -sk --max-time 3 "$BASE/api/health" >/dev/null 2>&1; then
    for id in "${RECS[@]:-}"; do
      [[ -n "$id" ]] || continue
      api POST "/api/autonomous/decision-records/$id/withdraw" '{"reason":"live check cleanup"}'
      if [[ "$API_CODE" == 200 ]]; then n=$((n+1)); else closed=$((closed+1)); fi
    done
    if [[ -n "$GATE_PRD" ]]; then
      api POST "/api/autonomous/prds/$GATE_PRD/cancel" '{}'
      api DELETE "/api/autonomous/prds/$GATE_PRD?hard=true"
    fi
    echo "Records withdrawn: $n; already answered or withdrawn: $closed. Automaton removed: ${GATE_PRD:-none}." > "$ROOT/cleanup.txt"
  else
    echo "The sandbox daemon was not reachable at cleanup; its data directory is removed with it." > "$ROOT/cleanup.txt"
  fi
}
final_cleanup() {
  local left
  CLEANUP_RESULT=$(cat "$ROOT/cleanup.txt" 2>/dev/null || echo "No pre-stop cleanup result was recorded.")
  [[ "${KEEP:-0}" == 1 ]] || rm -rf "$ROOT"
  if bash "$HERE/check-no-leftover-test-daemons.sh" check "$SNAP" >"$REPO_DIR/tmp-sandbox/leftover.out" 2>&1; then
    left="no leftover test daemon"
  else
    left="LEFTOVER test daemon: $(head -c 300 "$REPO_DIR/tmp-sandbox/leftover.out" | tr '\n' ' ')"
  fi
  CLEANUP_RESULT="$CLEANUP_RESULT Sandbox daemon ended by its recorded PID. Temporary folders removed. Leftover check: $left."
  write_report
  rm -f "$REPO_DIR/tmp-sandbox/leftover.out"; rmdir "$REPO_DIR/tmp-sandbox" 2>/dev/null
}
# The snapshot is taken before the traps so the leftover check has a baseline.
bash "$HERE/check-no-leftover-test-daemons.sh" snapshot "$SNAP" >/dev/null 2>&1 || : > "$SNAP"
TD_PRE_STOP_CLEANUP=pre_stop_cleanup
TD_EXTRA_CLEANUP=final_cleanup
td_install_traps "$@"

die() { printf 'setup failed: %s\n' "$*" >&2; step 0 "setup"; bad "$*"; step_end; exit 2; }
for t in curl jq go git od; do command -v "$t" >/dev/null || die "required tool missing: $t"; done
for p in $TLS_PORT $PLAIN_PORT; do
  ss -ltn 2>/dev/null | grep -q ":$p " && die "sandbox port $p is already in use"
done

# ---- build, config, daemon ---------------------------------------------------
echo "building the binary from this tree into $ROOT/bin"
(cd "$REPO_DIR" && go build -o "$BIN" ./cmd/datawatch/) || die "go build failed"
VERSION_LINE=$("$BIN" --version 2>/dev/null | head -1)
[[ -n "$VERSION_LINE" ]] || VERSION_LINE=$("$BIN" version 2>/dev/null | head -1)
[[ -n "$VERSION_LINE" ]] || VERSION_LINE=unknown

mkdir -p "$DOCS" "$FOLDER" "$GATEPROJ"
(cd "$GATEPROJ" && git init -q -b main && echo seed > README.md && git add README.md \
  && git -c user.email=live-check@local -c user.name=live-check commit -qm seed)
printf '# Proposal one\n\nSome text.\n\nConfirmed: pending the live check\n\nMore text.\n' > "$DOCS/one.md"
printf '# Proposal two\n\nSome text.\n\nConfirmed: pending the live check\n\nMore text.\n' > "$DOCS/two.md"

cat > "$ROOT/config.yaml" <<EOF
data_dir: $ROOT/data
hostname: dw-decision-live
session:
  llm_backend: shell
  default_project_dir: $GATEPROJ
  auto_git_init: false
  auto_git_commit: false
server:
  enabled: true
  host: 127.0.0.1
  port: $PLAIN_PORT
  token: $TOK
  tls_enabled: true
  tls_auto_generate: true
  tls_port: $TLS_PORT
  allowed_project_roots:
    - $ROOT
mcp:
  enabled: false
memory:
  enabled: false
autonomous:
  enabled: true
  poll_interval_seconds: 3
  max_concurrent_tasks: 1
  scan:
    enabled: false
  questions:
    enabled: true
    decision_gate: true
    decision_gate_mode: on
  decisions:
    enabled: true
    clarity_mode: warn
    doc_root: $DOCS
    doc_status_mode: report
    follow_ups:
      - name: kick
        kind: start_template
        target: no-such-template
EOF

echo "starting the sandbox daemon on $BASE"
td_start "$BIN" "$ROOT/config.yaml" "$ROOT/daemon.log" || { tail -20 "$ROOT/daemon.log" >&2; die "daemon did not start"; }
DPID=$TD_LAST_PID
td_set_api "$DPID" "$BASE" "$TOK" || true
healthy() { curl -sk --max-time 3 "$BASE/api/health" >/dev/null 2>&1; }
wait_for 240 healthy || { tail -20 "$ROOT/daemon.log" >&2; die "daemon not healthy within 240 s"; }
echo "daemon healthy (PID $DPID, $VERSION_LINE)"

# ============================================================================
step 1 "create a record over REST and read back its question"
api POST /api/autonomous/decision-records "$(rec_json live-one 'Which option does the sandbox take?' 'First record.')"
R1=$(jg '.record.id'); Q1=$(jg '.question_id')
if [[ "$API_CODE" == 201 && -n "$R1" && -n "$Q1" ]]; then new_rec "$R1"; ok "created $R1, question $Q1"; else bad "create gave $API_CODE: ${API_BODY:0:200}"; fi
if [[ -n "${Q1:-}" ]]; then
  get_question "$Q1"
  QT=$(jg '.text'); NOPT=$(jq '.options|length' <<<"$API_BODY"); NTRADE=$(jq '[.options[]|select((.tradeoffs//"")!="")]|length' <<<"$API_BODY")
  QREC=$(jg '.recommendation.option_id // .recommendation.option'); QDEF=$(jg '.default')
  [[ "$QT" == *"Which option does the sandbox take?"* ]] && ok "question text built from the record" || bad "question text: ${QT:0:120}"
  [[ "$NOPT" == 2 && "$NTRADE" == 2 ]] && ok "2 options, both with trade-offs" || bad "options=$NOPT with tradeoff=$NTRADE"
  [[ "$QREC" == fast ]] && ok "recommendation fast" || bad "recommendation=${QREC:-none}"
  [[ "$QDEF" == hold ]] && ok "default hold" || bad "default=${QDEF:-none}"
fi
step_end

# ============================================================================
step 2 "one question per record (REST, CLI list, comm-style path)"
api POST /api/autonomous/decision-records "$(rec_json live-two 'Which option does the sandbox take, again?' 'Second record.')"
R2=$(jg '.record.id'); Q2=$(jg '.question_id')
[[ "$API_CODE" == 201 && -n "$R2" ]] && { new_rec "$R2"; ok "second record $R2 got its own question $Q2"; } || bad "second create: $API_CODE ${API_BODY:0:160}"
[[ -n "${Q1:-}" && "$Q1" != "${Q2:-}" ]] && ok "two records, two different questions" || bad "the records share a question"
api GET /api/questions
for pair in "$R1 ${Q1:-}" "$R2 ${Q2:-}"; do
  set -- $pair
  n=$(jq --arg r "$1" '[(.questions // .)[]? | select(.decision_record_id==$r)] | length' <<<"$API_BODY" 2>/dev/null)
  [[ "$n" == 1 ]] && ok "record $1 has exactly 1 question in the question list" || bad "record $1 has ${n:-?} questions"
done
cli autonomous decisions --state open --json
if [[ $CLI_CODE -eq 0 ]] && grep -q "${Q1:-none}" <<<"$CLI_OUT" && grep -q "${Q2:-none}" <<<"$CLI_OUT"; then ok "CLI 'autonomous decisions' lists both questions"; else bad "CLI list code=$CLI_CODE: ${CLI_OUT:0:160}"; fi
api GET "/api/autonomous/decisions?limit=50"   # the path the chat verb reads
if [[ "$API_CODE" == 200 ]] && grep -q "${Q1:-none}" <<<"$API_BODY"; then
  c=$(grep -o "${Q1}" <<<"$API_BODY" | wc -l)
  ok "comm-style path /api/autonomous/decisions answers 200 and names ${Q1} ($c time(s))"
else bad "comm-style path: $API_CODE ${API_BODY:0:160}"; fi
api GET /api/autonomous/decision-records
n=$(jq --arg q "${Q1:-x}" '[.decision_records[]|select(.question_id==$q)]|length' <<<"$API_BODY" 2>/dev/null)
[[ "$n" == 1 ]] && ok "record list holds one record for question ${Q1}" || bad "record list holds ${n:-?} records for ${Q1}"
step_end

# ============================================================================
step 3 "clarity: refusal in refuse mode, warning in warn mode"
UNCLEAR='{"label":"go","title":"Pick one","situation":"short.","deciding":"pick one.","options":[{"id":"a","label":"option a","tradeoff":"a bit faster"},{"id":"b","label":"option b","tradeoff":""}],"default":"hold"}'
cfg_put autonomous.decisions.clarity_mode '"refuse"'; [[ "$API_CODE" == 200 ]] && ok "PUT clarity_mode=refuse" || bad "PUT refuse: $API_CODE ${API_BODY:0:120}"
api GET /api/autonomous/decision-records; before=$(jq '.decision_records|length' <<<"$API_BODY")
api POST /api/autonomous/decision-records "$UNCLEAR"
if [[ "$API_CODE" == 422 ]] && jq -e '.findings|length>0' <<<"$API_BODY" >/dev/null 2>&1; then
  ok "refuse mode: 422 with findings $(jq -r '[.findings[].code]|join(",")' <<<"$API_BODY")"
else bad "refuse mode expected 422, got $API_CODE ${API_BODY:0:160}"; fi
api GET /api/autonomous/decision-records; after=$(jq '.decision_records|length' <<<"$API_BODY")
[[ "$before" == "$after" ]] && ok "the refused record was not stored" || bad "record count went $before -> $after"
cfg_put autonomous.decisions.clarity_mode '"warn"'; [[ "$API_CODE" == 200 ]] || bad "PUT warn: $API_CODE"
api POST /api/autonomous/decision-records "$UNCLEAR"
R3=$(jg '.record.id')
if [[ "$API_CODE" == 201 ]] && jq -e '.warnings|length>=1' <<<"$API_BODY" >/dev/null 2>&1; then
  new_rec "$R3"; ok "warn mode: stored $R3 with warnings $(jq -r '[.warnings[].code]|join(",")' <<<"$API_BODY")"
else bad "warn mode expected 201 with warnings, got $API_CODE ${API_BODY:0:160}"; fi
step_end

# ============================================================================
step 4 "edit an open record: question text changes on the same question id"
get_question "$Q1"; TXT_BEFORE=$(jg '.text')
EDIT=$(rec_json live-one 'Which option does the sandbox take?' 'An operator tightened this wording afterwards, which must reach the same question.')
api PUT "/api/autonomous/decision-records/$R1" "$EDIT"
[[ "$API_CODE" == 200 && "$(jg '.question_id')" == "$Q1" ]] && ok "PUT answered 200, question id still $Q1" || bad "PUT: $API_CODE ${API_BODY:0:160}"
changed() { get_question "$Q1"; [[ "$(jg '.text')" == *"tightened this wording"* ]]; }
if wait_for 30 changed; then ok "question text now carries the edit (was different before)"; [[ "$TXT_BEFORE" != "$(jg '.text')" ]] || bad "text unchanged"; else bad "question text did not change"; fi
step_end

# ============================================================================
step 5 "answer with a multi-line quote; unconfigured follow_up is refused"
api POST /api/autonomous/decision-records "$(rec_json live-five 'Which option closes the answer check?' 'No Automaton is linked.')"
R5=$(jg '.record.id'); Q5=$(jg '.question_id'); new_rec "$R5"
[[ "$(jg '.record.links.automaton')" == "" ]] && ok "record $R5 links no Automaton" || bad "record links an Automaton"
QUOTE=$'first line of the reason\nsecond line — with an em dash\nthird line'
ANS=$(jq -cn --arg q "$QUOTE" '{chosen_option_id:"slow",actor:"live-operator",surface:"cli",quote:$q,follow_up:"not-configured"}')
api POST "/api/questions/$Q5/answer" "$ANS"
[[ "$API_CODE" == 400 ]] && ok "follow_up not configured -> 400" || bad "unconfigured follow_up gave $API_CODE ${API_BODY:0:140}"
api GET "/api/autonomous/decision-records/$R5"
[[ "$(jg '.record.question_state')" == open ]] && ok "question still open after the refused answer" || bad "question state $(jg '.record.question_state')"
api POST "/api/questions/$Q5/answer" "$(jq -c 'del(.follow_up)' <<<"$ANS")"
[[ "$API_CODE" =~ ^2 ]] && ok "answer without follow_up accepted ($API_CODE)" || bad "answer: $API_CODE ${API_BODY:0:160}"
applied() { api GET "/api/autonomous/decision-records/$R5"; [[ -n "$(jg '.record.answer.quote')" ]]; }
wait_for 30 applied
GOTQ=$(jq -r '.record.answer.quote' <<<"$API_BODY")
[[ "$GOTQ" == "$QUOTE" ]] && ok "read-back quote is the 3-line quote, byte for byte" || bad "quote differs: ${GOTQ:0:100}"
[[ "$(jg '.record.answer.actor')" == live-operator ]] && ok "actor live-operator" || bad "actor=$(jg '.record.answer.actor')"
[[ "$(jg '.record.answer.surface')" == cli ]] && ok "surface cli" || bad "surface=$(jg '.record.answer.surface')"
[[ "$(jg '.record.state')" == answered && "$(jg '.record.question_state')" == answered ]] && ok "record answered, question state answered" || bad "state=$(jg '.record.state') question_state=$(jg '.record.question_state')"
api GET /api/autonomous/decision-records
LIST_ROW=$(jq -c --arg r "$R5" '.decision_records[]|select(.id==$r)|{a:.answer.actor,s:.answer.surface,q:.answer.quote,qs:.question_state}' <<<"$API_BODY")
if [[ "$(jq -r .a <<<"$LIST_ROW")" == live-operator && "$(jq -r .s <<<"$LIST_ROW")" == cli && "$(jq -r .qs <<<"$LIST_ROW")" == answered && "$(jq -r .q <<<"$LIST_ROW")" == "$QUOTE" ]]; then
  ok "the list shows the quote, actor, surface and question state"; else bad "list row: ${LIST_ROW:0:200}"; fi
# a configured follow-up on its own record
api POST /api/autonomous/decision-records "$(rec_json live-kick 'Which option starts the follow-up?' 'This one requests the configured follow-up.')"
RK=$(jg '.record.id'); QK=$(jg '.question_id'); new_rec "$RK"
api POST "/api/questions/$QK/answer" '{"chosen_option_id":"fast","actor":"live-operator","quote":"go","follow_up":"kick"}'
[[ "$API_CODE" =~ ^2 ]] && ok "configured follow_up 'kick' accepted" || bad "kick answer: $API_CODE ${API_BODY:0:140}"
fu() { api GET "/api/autonomous/decision-records/$RK"; [[ "$(jg '.record.follow_up.status')" != "" ]]; }
if wait_for 30 fu; then ok "follow-up ran: status=$(jg '.record.follow_up.status') result=$(jg '.record.follow_up.result' | head -c 80)"; else bad "follow-up never reported a result"; fi
step_end

# ============================================================================
step 6 "document status line: report mode leaves the file, on mode rewrites one line"
H1=$(sha256sum "$DOCS/one.md" | cut -d' ' -f1)
api POST /api/autonomous/decision-records "$(rec_json live-doc-one 'Does the first proposal keep its scope?' 'Linked to the first document.' one.md)"
R6=$(jg '.record.id'); Q6=$(jg '.question_id'); new_rec "$R6"
api POST "/api/questions/$Q6/answer" '{"chosen_option_id":"fast","actor":"live-operator","quote":"keep it small"}'
dc() { api GET "/api/autonomous/decision-records/$R6"; [[ -n "$(jg '.record.doc_change.status')" ]]; }
wait_for 30 dc
H1b=$(sha256sum "$DOCS/one.md" | cut -d' ' -f1)
[[ "$H1" == "$H1b" ]] && ok "report mode: file byte-identical" || bad "report mode changed the file"
[[ -n "$(jg '.record.doc_change.after')" ]] && ok "report mode shows the change: '$(jg '.record.doc_change.before' | head -c 40)' -> '$(jg '.record.doc_change.after' | head -c 80)' (status $(jg '.record.doc_change.status'))" || bad "no change shown: ${API_BODY:0:200}"
cfg_put autonomous.decisions.doc_status_mode '"on"'; [[ "$API_CODE" == 200 ]] || bad "PUT doc_status_mode on: $API_CODE"
cp "$DOCS/two.md" "$ROOT/two.before"
api POST /api/autonomous/decision-records "$(rec_json live-doc-two 'Does the second proposal keep its scope?' 'Linked to the second document.' two.md)"
R7=$(jg '.record.id'); Q7=$(jg '.question_id'); new_rec "$R7"
api POST "/api/questions/$Q7/answer" '{"chosen_option_id":"fast","actor":"live-operator","quote":"apply it now"}'
rewritten() { ! cmp -s "$DOCS/two.md" "$ROOT/two.before"; }
if wait_for 30 rewritten; then
  nd=$(diff "$ROOT/two.before" "$DOCS/two.md" | grep -c '^>')
  [[ "$nd" == 1 ]] && ok "exactly one line rewritten" || bad "$nd lines changed"
  grep -q '^Confirmed:.*apply it now' "$DOCS/two.md" && ok "line now reads: $(grep '^Confirmed:' "$DOCS/two.md" | head -c 100)" || bad "quoted words missing in the file"
else bad "on mode never rewrote the file"; fi
step_end

# ============================================================================
folder_case() { # folder_case <tag> <delimiter> <separator>
  local tag=$1 d=$2 s=$3 f="$FOLDER/$1.md" rid qid h1 h2 q2 st
  write_file() { printf '%s\n' "$d" "label${s} $tag" "title${s} Which folder option does the daemon keep for $tag?" \
    "situation${s} The operator files a decision as a plain file in a folder the daemon watches, and the daemon turns it into one open question. $1" \
    "deciding${s} Pick which of the two folder options the daemon treats as chosen when the operator eventually answers this question." \
    "option${s} fa | File option A | the sandbox prefers speed" "option${s} fb | File option B | the sandbox prefers quiet" \
    "recommendation${s} fa | speed wins here" "default${s} hold" "$d" "Free text that is ignored." > "$f"; }
  write_file "Original."
  found() { api GET /api/autonomous/decision-records; rid=$(jq -r --arg t "$tag" '[.decision_records[]|select(.label==$t)][0].id // empty' <<<"$API_BODY"); [[ -n "$rid" ]]; }
  if wait_for 60 found; then
    new_rec "$rid"; api GET "/api/autonomous/decision-records/$rid"; qid=$(jg '.question_id'); h1=$(jg '.record.content_hash')
    [[ -n "$qid" ]] && ok "[$tag] file raised record $rid and question $qid" || bad "[$tag] record without a question"
  else bad "[$tag] no record within 60 s: $(curl -sk -H "Authorization: Bearer $TOK" "$BASE/api/autonomous/decision-records" | jq -c .folder 2>/dev/null | head -c 300)"; return; fi
  write_file "An operator edited the file."
  edited() { api GET "/api/autonomous/decision-records/$rid"; h2=$(jg '.record.content_hash'); [[ -n "$h2" && "$h2" != "$h1" ]]; }
  if wait_for 60 edited; then q2=$(jg '.question_id'); [[ "$q2" == "$qid" ]] && ok "[$tag] edit updated the record, same question" || bad "[$tag] question changed $qid -> $q2"; else bad "[$tag] edit not picked up; folder status: $(curl -sk -H "Authorization: Bearer $TOK" "$BASE/api/autonomous/decision-records" | jq -c '.folder|{errors,notes}' 2>/dev/null | head -c 400); hashes $h1 / $h2"; fi
  rm -f "$f"
  gone() { api GET "/api/autonomous/decision-records/$rid"; [[ "$(jg '.record.state')" == withdrawn ]]; }
  wait_for 60 gone && ok "[$tag] deleting the file withdrew the record" || bad "[$tag] state after delete: $(jg '.record.state')"
}
step 7 "folder watch: raise, edit, delete; then another delimiter and separator"
cfg_put autonomous.decisions.folder "$(jq -Rn --arg p "$FOLDER" '$p')"; [[ "$API_CODE" == 200 ]] && ok "folder configured" || bad "PUT folder: $API_CODE ${API_BODY:0:140}"
folder_case folder-default '---' ':'
cfg_put autonomous.decisions.front_matter_delimiter '"+++"'; d1=$API_CODE
cfg_put autonomous.decisions.front_matter_separator '"="'; [[ "$d1" == 200 && "$API_CODE" == 200 ]] && ok "delimiter +++ and separator = configured" || bad "PUT delimiter/separator: $d1 $API_CODE"
folder_case folder-custom '+++' '='
step_end

# ============================================================================
step 8 "a task spec naming the question id waits, then starts after the answer"
api POST /api/autonomous/decision-records "$(rec_json live-gate 'Which option releases the waiting task?' 'A task spec names this question.')"
RG=$(jg '.record.id'); QG=$(jg '.question_id'); new_rec "$RG"
[[ -n "$QG" ]] && ok "gate question $QG raised" || bad "no gate question"
api POST /api/autonomous/prds "$(jq -cn --arg d "$GATEPROJ" '{spec:"Live check: one small task that waits on an operator decision.",project_dir:$d}')"
GATE_PRD=$(jg '.id')
if [[ -z "$GATE_PRD" ]]; then
  skip "could not create an Automaton in the sandbox ($API_CODE ${API_BODY:0:160})"
else
  ok "Automaton $GATE_PRD created"
  # Tasks come from the planning step, which needs a planning backend.
  api POST "/api/autonomous/prds/$GATE_PRD/decompose" '{}'
  planned() { api GET "/api/autonomous/prds/$GATE_PRD"; PS=$(jq -r '.status // empty' <<<"$API_BODY"); NT=$(jq '[.stories[]?.tasks[]?]|length' <<<"$API_BODY" 2>/dev/null); [[ "$PS" == needs_review && "${NT:-0}" -ge 1 ]]; }
  if ! wait_for 120 planned; then
    skip "the sandbox has no planning backend, so the Automaton stays $PS with ${NT:-0} tasks and cannot hold a task (decompose answered $API_CODE)"
  else
    api GET "/api/autonomous/prds/$GATE_PRD"
    TID=$(jq -r '[.stories[]?.tasks[]?]|.[0].id' <<<"$API_BODY")
    api POST "/api/autonomous/prds/$GATE_PRD/edit_task" "$(jq -cn --arg t "$TID" --arg q "$QG" '{task_id:$t,new_spec:("Decision: "+$q+" - write note.txt with the single line released."),actor:"live-operator"}')"
    A1=$API_CODE
    api POST "/api/autonomous/prds/$GATE_PRD/approve" '{"actor":"live-operator","note":"live check"}'; A2=$API_CODE
    api POST "/api/autonomous/prds/$GATE_PRD/run" '{}'; A3=$API_CODE
    if [[ ! "$A1" =~ ^2 || ! "$A2" =~ ^2 || ! "$A3" =~ ^2 ]]; then
      skip "the Automaton could not be prepared or run in the sandbox (edit_task $A1, approve $A2, run $A3: ${API_BODY:0:140})"
    else
      tstat() { api GET "/api/autonomous/prds/$GATE_PRD"; TS=$(jq -r --arg t "$TID" '[.stories[]?.tasks[]?|select(.id==$t)]|.[0].status // empty' <<<"$API_BODY"); }
      waiting() { tstat; [[ "$TS" == waiting_decision ]]; }
      if wait_for 90 waiting; then
        ok "task $TID shows the waiting status ($TS) while the question is open"
        api POST "/api/questions/$QG/answer" '{"chosen_option_id":"fast","actor":"live-operator","quote":"release it"}'
        [[ "$API_CODE" =~ ^2 ]] && ok "question answered" || bad "answer: $API_CODE"
        started() { tstat; [[ -n "$TS" && "$TS" != waiting_decision && "$TS" != pending && "$TS" != queued ]]; }
        wait_for 240 started && ok "task left the waiting status and started (now $TS)" || bad "task still $TS 240 s after the answer"
      else
        tstat; bad "task never showed waiting_decision (status ${TS:-none})"
      fi
    fi
  fi
fi
step_end

# ---- verdict -----------------------------------------------------------------
echo
for i in "${!STEP_IDS[@]}"; do printf 'step %s %-8s %s\n' "${STEP_IDS[$i]}" "${STEP_RESULTS[$i]}" "${STEP_NAMES[$i]}"; done
any_failed && exit 1
exit 0
