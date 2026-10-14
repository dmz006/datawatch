#!/usr/bin/env bash
# TS-796 — per-task verification LLM override, proven end-to-end on a
# real run (v9.0.67–v9.0.72). An Automaton runs two tiny tasks in a
# fresh temp git repo; one task gets set_task_verification_llm pointing
# at an ask-compatible registry backend that differs from both the
# global autonomous verification_backend and the Automaton's execution
# backend. Asserts from the task's OWN recorded verification metadata
# (not a config echo) that the overridden task was judged by the
# override with verifier_source "task", and that the sibling task with
# no override records "global" (or "pool").
#
# How it reaches a runnable state: add_story/add_task are rejected
# while an Automaton is "draft", so it decomposes first (planner on
# ollama, a file-free placeholder spec so the decompose
# verification_script coverage gate has nothing to demand), then
# replaces the planned story with its own story + two tasks. The
# execution backend is switched to claude-code afterwards: ollama has
# no file tools, and a task that changes no files is rejected before
# the LLM verifier ever runs. testdata's llms: block is not loaded into
# the inference registry, so the override LLM is registered inline
# (compute node + LLM entry, address taken from testdata) when absent.
# tags: surface:api feature:automata conflict:llm
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-796"
story_preflight "surface:api feature:automata conflict:llm" || return 0

_story_ts_796() {
  if [[ "$(t3_check_autonomous)" != "yes" ]]; then skip "autonomous disabled"; return; fi

  local exec_backend="claude-code"
  local exec_ok
  exec_ok=$(api GET /api/backends | python3 -c '
import json,sys
d=json.load(sys.stdin)
print("yes" if any(b.get("name")==sys.argv[1] and b.get("enabled") and b.get("available") for b in d.get("llm",[])) else "no")
' "$exec_backend" 2>/dev/null || echo "no")
  if [[ "$exec_ok" != "yes" ]]; then skip "$exec_backend not available on this daemon (needed to execute a file-writing task)"; return; fi

  # ---- pick an override that differs from the global verifier and the executor ----
  local global_vb override="" cand
  global_vb=$(api GET /api/autonomous/config | python3 -c 'import json,sys;print(json.load(sys.stdin).get("verification_backend") or "")' 2>/dev/null || echo "")
  local testdata_llms
  testdata_llms=$(python3 -c '
import sys
in_llms=False
for line in open(sys.argv[1]):
    if line.strip() and not line.startswith((" ", "#")):
        in_llms=line.startswith("llms:")
    elif in_llms and line.strip().startswith("- name:"):
        print(line.split(":",1)[1].strip())
' "$REPO_ROOT/testdata/datawatch.yaml" 2>/dev/null | tr '\n' ' ')
  for cand in ollama-localhost $testdata_llms; do
    if [[ "$cand" != "$global_vb" && "$cand" != "$exec_backend" ]]; then override="$cand"; break; fi
  done
  local override_model override_url
  read -r override_model override_url < <(python3 - "$REPO_ROOT/testdata/datawatch.yaml" "$override" <<'PY'
import sys
path, name = sys.argv[1], sys.argv[2]
cur, model, url, in_llms = None, "", "", False
for line in open(path):
    s = line.rstrip("\n")
    if s and not s.startswith(" ") and not s.startswith("#"):
        in_llms = s.startswith("llms:")
        continue
    if not in_llms:
        continue
    t = s.strip()
    if t.startswith("- name:"):
        cur = t.split(":", 1)[1].strip()
    elif cur == name and t.startswith("model:") and not model:
        model = t.split(":", 1)[1].strip()
    elif cur == name and t.startswith("url:") and not url:
        url = t.split(":", 1)[1].strip()
print(model or "qwen3:1.7b", url)
PY
)
  save_evidence TS-796 "0_override_choice.txt" "global_verification_backend='$global_vb' exec_backend=$exec_backend override=$override model=$override_model url=$override_url"

  local code
  code=$(api_code GET "/api/llms/$override" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then
    if [[ -z "$override_url" ]]; then skip "no url for $override in testdata/datawatch.yaml llms:"; return; fi
    local node="ts796-node-$$"
    code=$(api_code POST /api/compute/nodes "{\"name\":\"$node\",\"kind\":\"ollama\",\"address\":\"$override_url\"}" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
    [[ "$code" == "200" || "$code" == "201" ]] && add_cleanup compute_node "$node"
    code=$(api_code POST /api/llms "{\"name\":\"$override\",\"kind\":\"ollama\",\"model\":\"$override_model\",\"compute_nodes\":[\"$node\"]}" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
    if [[ "$code" != "200" && "$code" != "201" ]]; then skip "could not register override LLM $override ($code)"; return; fi
    add_cleanup llm "$override"
  fi
  # The override must actually answer /api/ask, the path the verifier uses.
  local probe
  probe=$(curl "${curl_args[@]}" --max-time 120 -X POST -H "Content-Type: application/json" \
    -d "{\"question\":\"Reply with the single word OK.\",\"backend\":\"ollama\",\"llm\":\"$override\",\"model\":\"$override_model\"}" \
    "$TEST_BASE/api/ask")
  save_evidence TS-796 "1_ask_probe.json" "$probe"
  if ! echo "$probe" | python3 -c 'import json,sys;assert json.load(sys.stdin).get("answer")' 2>/dev/null; then
    skip "override LLM $override does not answer /api/ask: $(echo "$probe" | head -c 200)"; return
  fi

  # ---- fresh temp git repo with one trivial file ----
  local repo
  repo=$(mktemp -d "${TMPDIR:-/tmp}/ts796-repo-XXXXXX")
  git -C "$repo" init -q -b main
  echo "# ts796 fixture" > "$repo/README.md"
  git -C "$repo" add README.md
  git -C "$repo" -c user.email=ts796@example.invalid -c user.name=ts796 commit -qm init

  # ---- Automaton: decompose a file-free placeholder to reach needs_review ----
  local spec="Placeholder plan used only to reach review. Produce exactly one story with one task titled Review that reads the README and writes nothing. Do not plan any files."
  local atm atm_id status="" attempt i
  atm=$(api POST /api/autonomous/prds "{\"spec\":\"$spec\",\"project_dir\":\"$repo\",\"backend\":\"ollama\",\"effort\":\"low\"}")
  atm_id=$(echo "$atm" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("id",""))' 2>/dev/null || echo "")
  if [[ -z "$atm_id" ]]; then ko "could not create Automaton: $(echo "$atm" | head -c 200)"; return; fi
  add_cleanup automaton "$atm_id"
  for attempt in 1 2 3; do
    api POST "/api/autonomous/prds/$atm_id/decompose" '{}' >/dev/null
    for i in $(seq 1 100); do
      status=$(api GET "/api/autonomous/prds/$atm_id/decompose/status" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
      [[ "$status" == "pending" || "$status" == "in_progress" ]] || break
      sleep 3
    done
    status=$(api GET "/api/autonomous/prds/$atm_id" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("status",""))' 2>/dev/null || echo "")
    [[ "$status" == "needs_review" ]] && break
  done
  if [[ "$status" != "needs_review" ]]; then
    skip "decompose did not reach needs_review after 3 attempts (status=$status): $(api GET "/api/autonomous/prds/$atm_id/decompose/status" | head -c 200)"
    return
  fi

  # ---- replace the planned story with one story + two tiny tasks ----
  local story_resp story_id old_ids
  story_resp=$(api POST "/api/autonomous/prds/$atm_id/add_story" '{"title":"Notes","description":"Append single lines to two files."}')
  story_id=$(echo "$story_resp" | python3 -c 'import json,sys;print(json.load(sys.stdin)["stories"][-1]["id"])' 2>/dev/null || echo "")
  old_ids=$(echo "$story_resp" | python3 -c 'import json,sys;print(" ".join(s["id"] for s in json.load(sys.stdin)["stories"][:-1]))' 2>/dev/null || echo "")
  if [[ -z "$story_id" ]]; then ko "add_story returned no story id: $(echo "$story_resp" | head -c 200)"; return; fi
  for i in $old_ids; do api POST "/api/autonomous/prds/$atm_id/remove_story" "{\"story_id\":\"$i\"}" >/dev/null; done
  local t_resp task_override task_sibling
  t_resp=$(api POST "/api/autonomous/prds/$atm_id/add_task" "{\"story_id\":\"$story_id\",\"title\":\"hello\",\"spec\":\"Append the line hello to NOTES.md (create the file if missing). Do nothing else.\"}")
  task_override=$(echo "$t_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print([s for s in d["stories"] if s["id"]==sys.argv[1]][0]["tasks"][-1]["id"])' "$story_id" 2>/dev/null || echo "")
  t_resp=$(api POST "/api/autonomous/prds/$atm_id/add_task" "{\"story_id\":\"$story_id\",\"title\":\"world\",\"spec\":\"Append the line world to WORLD.md (create the file if missing). Do nothing else.\"}")
  task_sibling=$(echo "$t_resp" | python3 -c 'import json,sys;d=json.load(sys.stdin);print([s for s in d["stories"] if s["id"]==sys.argv[1]][0]["tasks"][-1]["id"])' "$story_id" 2>/dev/null || echo "")
  if [[ -z "$task_override" || -z "$task_sibling" ]]; then ko "add_task did not return two task ids"; return; fi

  # ---- execution backend + the override under test ----
  api POST "/api/autonomous/prds/$atm_id/set_llm" "{\"backend\":\"$exec_backend\",\"effort\":\"low\"}" >/dev/null
  local set_body="{\"task_id\":\"$task_override\",\"verification_backend\":\"$override\",\"verification_model\":\"$override_model\"}"
  local set_resp
  set_resp=$(api_code POST "/api/autonomous/prds/$atm_id/set_task_verification_llm" "$set_body")
  save_evidence TS-796 "2_set_task_verification_llm.txt" "POST /api/autonomous/prds/$atm_id/set_task_verification_llm $set_body
$(echo "$set_resp" | tail -1)"
  code=$(echo "$set_resp" | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then ko "set_task_verification_llm returned $code: $(echo "$set_resp" | head -c 200)"; return; fi

  code=$(api_code POST "/api/autonomous/prds/$atm_id/approve" '{}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" ]]; then ko "approve returned $code"; return; fi
  code=$(api_code POST "/api/autonomous/prds/$atm_id/run" '{}' | grep -oP '__HTTP_CODE_\K[0-9]+' || echo 0)
  if [[ "$code" != "200" && "$code" != "202" ]]; then ko "run returned $code"; return; fi

  # ---- poll until both tasks carry a stamped verification result (max 1800s) ----
  local snap done_both
  for i in $(seq 1 360); do
    snap=$(api GET "/api/autonomous/prds/$atm_id")
    done_both=$(echo "$snap" | python3 -c '
import json,sys
d=json.load(sys.stdin)
ts={t["id"]:t for s in d.get("stories") or [] for t in s.get("tasks") or []}
want=sys.argv[1:]
stamped=all((ts.get(w,{}).get("verification") or {}).get("verifier_source") for w in want)
print("yes" if stamped or d.get("status") in ("completed","failed","cancelled","blocked") else "no")
' "$task_override" "$task_sibling" 2>/dev/null || echo "no")
    [[ "$done_both" == "yes" ]] && break
    sleep 5
  done
  save_evidence TS-796 "3_automaton_final.json" "$snap"

  local verdict
  verdict=$(echo "$snap" | python3 -c '
import json,sys
d=json.load(sys.stdin)
ts={t["id"]:t for s in d.get("stories") or [] for t in s.get("tasks") or []}
ov=ts.get(sys.argv[1],{}).get("verification") or {}
sb=ts.get(sys.argv[2],{}).get("verification") or {}
want=sys.argv[3]
errs=[]
if ov.get("verifier_backend")!=want: errs.append("override task verifier_backend=%r want %r" % (ov.get("verifier_backend"), want))
if ov.get("verifier_source")!="task": errs.append("override task verifier_source=%r want task" % ov.get("verifier_source"))
if sb.get("verifier_source") not in ("global","pool"): errs.append("sibling task verifier_source=%r want global|pool" % sb.get("verifier_source"))
print("OK" if not errs else "; ".join(errs))
print("override=" + json.dumps(ov))
print("sibling=" + json.dumps(sb))
' "$task_override" "$task_sibling" "$override" 2>/dev/null || echo "parse error")
  save_evidence TS-796 "4_verdict.txt" "$verdict"
  echo "  [TS-796] automaton=$atm_id task_override=$task_override task_sibling=$task_sibling"
  echo "$verdict" | sed 's/^/  [TS-796] /'

  rm -rf "$repo"
  if [[ "$(echo "$verdict" | head -1)" == "OK" ]]; then
    ok "task $task_override judged by override $override (verifier_source=task); sibling $task_sibling verifier_source global/pool (v9.0.72)"
  else
    ko "$(echo "$verdict" | head -1)"
  fi
}

RESULT=fail
_story_ts_796
: "${RESULT:=fail}"
unset -f _story_ts_796
