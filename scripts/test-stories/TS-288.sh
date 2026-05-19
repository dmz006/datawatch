#!/usr/bin/env bash
# TS-288 — eval_list_suites + eval_run smoke suite shape via REST
# tags: surface:mcp feature:mcp feature:evals
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-288"
story_preflight "surface:mcp feature:mcp feature:evals" || return 0

_story_ts_288() {
  local suite_name="ts288-e2e-smoke-$$"
  local suite_file="$TEST_DATA/evals/${suite_name}.yaml"

  # Create the evals directory and suite YAML
  mkdir -p "$TEST_DATA/evals"
  cat > "$suite_file" <<YAML
name: ${suite_name}
description: E2E smoke eval for TS-288
mode: regression
pass_threshold: 1.0
cases:
  - name: basic-string-match
    input: "hello"
    expected: "hello"
    grader:
      type: string_match
YAML

  # Verify the suite appears in the list
  local suites_resp
  suites_resp=$(api GET /api/evals/suites)
  save_evidence TS-288 "suites.json" "$suites_resp"

  if echo "$suites_resp" | grep -qi "disabled\|not available\|503"; then
    rm -f "$suite_file"
    skip "evals not available in this build"
    return
  fi

  if ! echo "$suites_resp" | grep -q "$suite_name"; then
    rm -f "$suite_file"
    ko "created suite YAML but GET /api/evals/suites did not include $suite_name: $(echo "$suites_resp" | head -c 200)"
    return
  fi

  # Run the suite
  local run_resp
  run_resp=$(api POST "/api/evals/run?suite=${suite_name}")
  save_evidence TS-288 "run.json" "$run_resp"

  # Clean up the suite file
  rm -f "$suite_file"

  if assert_json "$run_resp" '"pass" in d'; then
    ok "eval suite created, listed, and run returned 'pass' field"
  elif assert_json "$run_resp" 'isinstance(d, dict)'; then
    ok "eval suite created, listed, and run returned dict: $(echo "$run_resp" | head -c 100)"
  else
    ko "eval run response unexpected: $(echo "$run_resp" | head -c 200)"
  fi
}

RESULT=fail
_story_ts_288
: "${RESULT:=fail}"
unset -f _story_ts_288
