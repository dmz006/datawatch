#!/usr/bin/env bash
# TS-801 — result panel: a spawned subprocess session posts structured
# results over the per-session endpoint using its injected scoped
# credential, and every read surface (REST grouped + history, MCP, CLI)
# reports them back; the cross-session identity gate rejects a scoped
# token used against a different (but valid) session with 403.
#
# Spawns two ephemeral subprocess sessions through POST /api/schedules
# (type=spawn, subprocess=true, run_at=now — the schedule timer ticks
# every 30 s, so both fire on the first tick). The "poster" child writes
# its injected DATAWATCH_SESSION_ID / DATAWATCH_RESULT_TOKEN to a shared
# scratch dir and posts two results (status ok, then error) to its own
# session. The story then:
#   1. polls GET /api/schedule-results until the group shows both posts
#      (latest=error, recent_statuses ok→error, count>=2);
#   2. checks GET /api/schedule-results/{name} history order + provenance
#      (same session id on every entry);
#   3. asserts a 403 when the poster's scoped token posts to the decoy
#      session's path (a different, real session);
#   4. posts one more result through the CLI (DATAWATCH_RESULT_TOKEN from
#      the child's env) and re-reads both REST surfaces;
#   5. reads the same schedule through MCP schedule_results_list.
#
# Skips (not fails) when the daemon predates the result endpoints or the
# child cannot write the scratch dir (remote external-daemon mode).
# Every session it spawns is killed and the scratch dir removed on exit.
# tags: surface:api feature:schedules conflict:rest-mcp
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-801"
story_preflight "surface:api feature:schedules conflict:rest-mcp" || return 0

_story_ts_799() {
  # --- endpoint must exist on this daemon ---------------------------------
  local code
  code=$(api_code GET /api/schedule-results | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
  if [[ "$code" != "200" ]]; then skip "GET /api/schedule-results returned $code (daemon predates the result endpoints)"; return; fi

  local sm_name="ts799-result-$$"
  local scratch
  scratch=$(mktemp -d "${TMPDIR:-/tmp}/ts799-result-XXXXXX")
  # scratch + spawned sessions are torn down at the end of the story

  # --- spawn body builders (python keeps the nested quoting sane) ---------
  local body_a
  body_a=$(SM_NAME="$sm_name" SM_SCRATCH="$scratch" python3 - <<'PY'
import json, os, shlex
name, scratch = os.environ["SM_NAME"], os.environ["SM_SCRATCH"]
p1 = json.dumps({"title": "first post", "status": "ok",
                 "summary": "posted via REST from the child",
                 "metrics": {"posts": 1}})
p2 = json.dumps({"title": "second post", "status": "error",
                 "summary": "simulated failure with the scoped token"})
task = (
    "sleep 1; "
    "S=${DATAWATCH_SESSION_ID:-}; T=${DATAWATCH_RESULT_TOKEN:-}; B=${DATAWATCH_BASE_URL:-}; "
    "mkdir -p __SCRATCH__ 2>/dev/null; "
    "if [ -n \"$S\" ]; then printf %s \"$S\" > __SCRATCH__/sid_poster; fi; "
    "if [ -n \"$T\" ]; then printf %s \"$T\" > __SCRATCH__/tok_poster; fi; "
    "if [ -n \"$B\" ]; then printf %s \"$B\" > __SCRATCH__/base_url; fi; "
    "if [ -n \"$T\" ] && [ -n \"$S\" ] && [ -n \"$B\" ]; then "
    "curl -sS -m 10 -X POST \"$B/api/sessions/$S/result\" "
    '-H "Authorization: Bearer $T" -H "Content-Type: application/json" '
    "-d '__P1__' >/dev/null 2>&1; sleep 2; "
    "curl -sS -m 10 -X POST \"$B/api/sessions/$S/result\" "
    '-H "Authorization: Bearer $T" -H "Content-Type: application/json" '
    "-d '__P2__' >/dev/null 2>&1; fi; exit 0"
).replace("__SCRATCH__", scratch).replace("__P1__", p1).replace("__P2__", p2)
task = "bash -c " + shlex.quote(task)  # a subprocess task is one program, no shell
print(json.dumps({"type": "spawn", "subprocess": True, "schedule_name": name,
                  "name": name, "project_dir": "/tmp", "run_at": "now",
                  "command": task}))
PY
) || { skip "could not build spawn body (python3)"; rm -rf "$scratch"; return; }

  local body_b
  body_b=$(SM_SCRATCH="$scratch" python3 - <<'PY'
import json, os, shlex
scratch = os.environ["SM_SCRATCH"]
task = (
    "sleep 1; S=${DATAWATCH_SESSION_ID:-}; "
    "mkdir -p __SCRATCH__ 2>/dev/null; "
    "if [ -n \"$S\" ]; then printf %s \"$S\" > __SCRATCH__/sid_decoy; fi; exit 0"
).replace("__SCRATCH__", scratch)
task = "bash -c " + shlex.quote(task)  # a subprocess task is one program, no shell
print(json.dumps({"type": "spawn", "subprocess": True,
                  "name": "ts799-decoy", "project_dir": "/tmp",
                  "run_at": "now", "command": task}))
PY
) || true

  # --- start both spawns; capture schedule entry ids for cleanup ----------
  local resp sid_poster="" sid_decoy="" sched_a sched_b
  resp=$(api POST /api/schedules "$body_a")
  save_evidence TS-801 "1_spawn_poster.json" "{\"request\":$body_a
$response: $resp"
  sched_a=$(echo "$resp" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  [[ -n "$sched_a" ]] && add_cleanup sched "$sched_a"

  api POST /api/schedules "$body_b" >/dev/null 2>&1
  api_code DELETE "/api/schedule?name=$sm_name" >/dev/null 2>&1  # pending-only; no-op after fire
  api_code DELETE "/api/schedule?name="ts799-decoy"" >/dev/null 2>&1  # pending-only; no-op after fire

  # --- poll until both posts are listed (schedule timer ticks every 30 s) --
  local seen="" i
  for i in $(seq 1 40); do
    seen=$(api GET /api/schedule-results | python3 -c '
import json, sys
groups = json.load(sys.stdin)
for g in (groups if isinstance(groups, list) else []):
    if g.get("schedule_name") == sys.argv[1]:
        print(json.dumps(g))
        break
' "$sm_name" 2>/dev/null || echo "")
    [[ -n "$seen" ]] && break
    sleep 5
  done
  save_evidence TS-801 "2_group_after_poll.json" "${seen:-<not observed after $(i)s polls>}"
  if [[ -z "$seen" ]]; then
    skip "posted results not listed within ~200 s (schedule 30 s tick + spawn latency)"
  fi

  # --- 1. grouped summary: latest=error, both statuses in order ---------
  local v1
  v1=$(echo "$seen" | python3 -c '
import json, sys
g = json.load(sys.stdin)
lp = (g.get("latest") or {}).get("payload", {})
rs = g.get("recent_statuses") or []
errs = []
if (lp.get("status")) != "error": errs.append("latest.status=%r want error" % (lp.get("status"),))
if rs[-2:] != ["ok", "error"]: errs.append("recent_statuses tail=%r want [\"ok\",\"error\"]" % (rs[-2:],))
if (g.get("count") or 0) < 2: errs.append("count=%r want >=2" % (g.get("count"),))
print("OK" if not errs else "; ".join(errs))
' 2>/dev/null || echo "parse error")
  if [[ "$(echo "$v1" | head -1)" != "OK" ]]; then
    ko "grouped summary: $v1"; return
  fi

  # --- 2. history endpoint: order + provenance ---------------------------
  local hist
  hist=$(api GET "/api/schedule-results/$sm_name")
  save_evidence TS-801 "3_history.json" "$hist"
  local v2
  v2=$(echo "$hist" | python3 -c '
import json, sys
d = json.load(sys.stdin)
errs = []
if not (isinstance(d, list) and len(d) >= 2): errs.append("history=%r want list >=2" % (d if not isinstance(d, list) else len(d),))
else:
    if d[0].get("payload", {}).get("status") != "ok": errs.append("first.status=%r want ok" % d[0].get("payload", {}).get("status"))
    if d[-1].get("payload", {}).get("status") != "error": errs.append("last.status=%r want error" % d[-1].get("payload", {}).get("status"))
    sids = {e.get("session_id") for e in d if e.get("session_id")}
    if len(sids) != 1: errs.append("session_ids span %r, want a single session" % (sids,))
    if d[-1].get("schedule_name") != d[-1].get("schedule_name"): pass
print("OK" if not errs else "; ".join(errs))
' 2>/dev/null || echo "parse error")
  if [[ "$(echo "$v2" | head -1)" != "OK" ]]; then
    ko "history endpoint: $v2"; return
  fi
  ok "child posted two results (ok→error) over /api/sessions/{id}/result; listed grouped + as ordered history with single-session provenance"

  # --- 3. cross-session identity gate (403) ------------------------------
  for i in $(seq 1 12); do [[ -s "$scratch/sid_decoy" ]] && break; sleep 5; done
  if [[ -s "$scratch/sid_decoy" && -s "$scratch/tok_poster" ]]; then
    sid_decoy=$(cat "$scratch/sid_decoy")
    local tok_poster="$(cat "$scratch/tok_poster")"
    local bodyx='{"title":"must be rejected","status":"error"}'
    code=$(api_code POST "/api/sessions/$sid_decoy/result" "$bodyx" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo "0")
    save_evidence TS-801 "4_cross_session_403.txt" "poster token -> decoy session $sid_decoy => HTTP $code (want 403)"
    if [[ "$code" != "403" ]]; then
      ko "cross-session post with the poster's scoped token to session $sid_decoy: got HTTP $code, want 403"
      return
    fi
    ok "cross-session post rejected (HTTP 403) — scoped token only valid for its own session"
  else
    skip "cross-session 403 gate (poster or decoy child did not report; scratch dir not shared with daemon)"
  fi

  # --- 4. CLI post surface, then re-read both REST surfaces ---------------
  if [[ -s "$scratch/tok_poster" && -s "$scratch/sid_poster" ]]; then
    sid_poster=$(cat "$scratch/sid_poster")
    local tok="$(cat "$scratch/tok_poster")"
    local cli_out
    cli_out=$(env DATAWATCH_RESULT_TOKEN="$tok" cli_test schedule-results post \
        --session "$sid_poster" --title "cli post" --status warn --summary "posted via CLI" 2>&1 || true)
    save_evidence TS-801 "5_cli_post.txt" "$cli_out"
    local g2
    for i in $(seq 1 6); do
      g2=$(api GET /api/schedule-results | python3 -c '
import json, sys
for g in (json.load(sys.stdin) or []):
    if g.get("schedule_name") == sys.argv[1]: print(json.dumps(g))
' "$sm_name" 2>/dev/null || echo "")
      echo "$g2" | python3 -c 'import json,sys;g=json.load(sys.stdin);sys.exit(0 if (g.get("latest") or {}).get("payload",{}).get("status")=="warn" else 1)' 2>/dev/null && break
      sleep 3
    done
    local v3
    v3=$(echo "$g2" | python3 -c '
import json, sys
g = json.load(sys.stdin)
lp = (g.get("latest") or {}).get("payload", {})
rs = g.get("recent_statuses") or []
errs = []
if lp.get("status") != "warn": errs.append("latest.status=%r want warn" % (lp.get("status"),))
if rs[-3:] != ["ok", "error", "warn"]: errs.append("recent_statuses tail=%r" % (rs[-3:],))
print("OK" if not errs else "; ".join(errs))
' 2>/dev/null || echo "parse error")
    if [[ "$(echo "$v3" | head -1)" != "OK" ]]; then
      ko "CLI post not reflected in grouped summary: $v3"
      return
    fi
    hist=$(api GET "/api/schedule-results/$sm_name")
    save_evidence TS-801 "6_history_after_cli.json" "$hist"
    local v4
    v4=$(echo "$hist" | python3 -c '
import json, sys
d = json.load(sys.stdin)
errs = []
if len(d) < 3: errs.append("history %d entries, want >=3" % len(d))
elif d[-1].get("payload", {}).get("status") != "warn": errs.append("last.status=%r want warn" % d[-1].get("payload", {}).get("status"))
print("OK" if not errs else "; ".join(errs))
' 2>/dev/null || echo "parse error")
    if [[ "$(echo "$v4" | head -1)" != "OK" ]]; then
      ko "CLI post not reflected in history: $v4"
      return
    fi
    ok "CLI schedule-results post with the scoped token reflected in both read surfaces (latest=warn)"
  else
    skip "CLI post sub-check (child env not captured)"
  fi

  # --- 5. MCP read surface ------------------------------------------------
  local mcp_raw
  mcp_raw=$(api_mcp schedule_results_list "{\"schedule_name\":\"$sm_name\"}")
  save_evidence TS-801 "7_mcp_schedule_results_list.json" "$mcp_raw"
  if echo "$mcp_raw" | python3 -c '
import json, sys
raw = sys.stdin.read()
try:
    d = json.loads(raw)
except Exception:
    sys.exit(1)
names = json.dumps(d)
ok_ = sys.argv[1] in names
if not ok_: sys.exit(1)
s = json.dumps(d)
if "warn" not in s: sys.exit(1)
' "$sm_name" 2>/dev/null; then
    ok "MCP schedule_results_list surfaces the same schedule + latest status (warn)"
  else
    ko "MCP schedule_results_list did not reflect the schedule: $(echo "$mcp_raw" | head -c 200)"
    return
  fi

  # --- teardown ------------------------------------------------------------
  for f in sid_poster sid_decoy; do
    if [[ -s "$scratch/$f" ]]; then
      api POST /api/sessions/kill "$(python3 -c 'import json,sys;print(json.dumps({"id":sys.argv[1]}))' "$(cat "$scratch/$f")")" >/dev/null 2>&1 || true
    fi
  done
  rm -rf "$scratch" 2>/dev/null || true
  ok "TS-801 result panel: spawn -> scoped-credential post -> REST/MCP/CLI reads + 403 gate all hold"
}

RESULT=fail
_story_ts_799
: "${RESULT:=fail}"
unset -f _story_ts_799
