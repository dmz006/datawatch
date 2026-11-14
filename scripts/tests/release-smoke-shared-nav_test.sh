#!/bin/bash
# Test: the shared navigation (79a) and load (79) sections of
# scripts/release-smoke.sh.
#
# The section bodies and the H/ok/ko/skip helpers are extracted from the real
# script and evaluated in a harness whose PATH carries a fake `node` (and a
# fake `timeout`) that records its environment and arguments and exits with a
# status chosen by the test, so no case needs a browser, a daemon or the
# network. Each case runs in its own subshell with a fresh `mktemp -d`
# directory that is removed on EXIT.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SMOKE="$HERE/../release-smoke.sh"

if [[ ! -f "$SMOKE" ]]; then
  echo "FAIL: $SMOKE not found" >&2
  exit 1
fi

FAILURES=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; FAILURES=$((FAILURES + 1)); }

# --- extraction from release-smoke.sh --------------------------------------

# extract_body <section-id> — the section's executable body: everything after
# its `H "<id>. ..."` line up to the next section separator (`# ---...`).
extract_body() {
  awk -v id="$1" '
    $0 ~ ("^H \"" id "\\. ") {grab=1; next}
    grab && /^# -/ {exit}
    grab {print}
  ' "$SMOKE"
}

# extract_helpers — the SMOKE_ONLY/SECTION_SKIP initialisation, the H()
# function and the ok/ko/skip one-liners, verbatim from the script.
extract_helpers() {
  awk '
    /^SMOKE_ONLY="\$SMOKE_ONLY/ {print; next}
    /^SECTION_SKIP=0/ {print; next}
    /^H\(\) \{/ {inblock=1}
    inblock {print; if ($0 == "}") inblock=0; next}
    /^ok\(\) \{/ {print; next}
    /^ko\(\) \{/ {print; next}
    /^skip\(\) \{/ {print; next}
  ' "$SMOKE"
}

SHARED_NAV_BODY="$(extract_body "79a")"
LOAD_BODY="$(extract_body "79")"
HELPERS="$(extract_helpers)"

if [[ -n "$SHARED_NAV_BODY" ]] && grep -qF -- '--group shared-nav' <<<"$SHARED_NAV_BODY" \
   && [[ -n "$LOAD_BODY" ]] && grep -qF -- '--group load' <<<"$LOAD_BODY" \
   && grep -qF 'H() {' <<<"$HELPERS" \
   && grep -qF 'ok() {' <<<"$HELPERS" \
   && grep -qF 'ko() {' <<<"$HELPERS" \
   && grep -qF 'skip() {' <<<"$HELPERS"; then
  pass "extraction: 79a and 79 section bodies and H/ok/ko/skip pulled from release-smoke.sh"
else
  fail "extraction: could not pull the sections or the helpers from release-smoke.sh"
fi

# --- harness ---------------------------------------------------------------

# case_setup — fresh scratch dir (removed on EXIT), fake node + timeout on
# PATH, the script globals the sections read, and the extracted helpers.
# FAKE_NODE_RC / FAKE_NODE_STDOUT may be exported by a case to steer the fake
# node; every node call is recorded under $FAKE_NODE_DIR as inv-*.
case_setup() {
  CASE_DIR="$(mktemp -d)"
  # shellcheck disable=SC2064 # intentional: bake the path in now
  trap "rm -rf -- $(printf '%q' "$CASE_DIR")" EXIT

  mkdir -p "$CASE_DIR/bin" "$CASE_DIR/fakenode" "$CASE_DIR/repo" "$CASE_DIR/tmpd"

  cat > "$CASE_DIR/bin/node" <<'EOF'
#!/bin/bash
# fake node: record args + environment, print FAKE_NODE_STDOUT, exit FAKE_NODE_RC
d="${FAKE_NODE_DIR:?FAKE_NODE_DIR not set}"
f="$d/inv-$$"
while [[ -e "$f" ]]; do f="${f}x"; done
{
  printf 'args:\n'
  for a in "$@"; do printf 'arg: %s\n' "$a"; done
  printf 'env:\n'
  env
} > "$f"
if [[ -n "${FAKE_NODE_STDOUT:-}" ]]; then
  printf '%s\n' "$FAKE_NODE_STDOUT"
fi
exit "${FAKE_NODE_RC:-0}"
EOF
  chmod 0755 "$CASE_DIR/bin/node"

  cat > "$CASE_DIR/bin/timeout" <<'EOF'
#!/bin/bash
# fake timeout: drop the duration argument and run the command unchanged
shift
exec "$@"
EOF
  chmod 0755 "$CASE_DIR/bin/timeout"

  FAKE_NODE_DIR="$CASE_DIR/fakenode"
  REPO_DIR="$CASE_DIR/repo"
  TMPD="$CASE_DIR/tmpd"
  BASE="http://127.0.0.1:1"
  TOK="smoke-test-token"
  export FAKE_NODE_DIR REPO_DIR TMPD BASE TOK
  export FAKE_NODE_RC="${FAKE_NODE_RC:-0}"
  export FAKE_NODE_STDOUT="${FAKE_NODE_STDOUT:-}"

  PATH="$CASE_DIR/bin:$PATH"
  export PATH

  # counters the ok/ko/skip helpers bump
  PASS=0
  FAIL=0
  SKIP=0

  # H() calls these two; neither does anything in the harness.
  _smoke_close_sec() { :; }
  _smoke_write_progress() { :; }

  eval "$HELPERS"
}

# run_nav_section <out-file> / run_load_section <out-file> — run H() plus the
# extracted body in the current shell, output captured, the section's own
# non-zero statements must not abort the case (ko/ok are assertions, not
# errors).
run_nav_section() {
  local out="$1"
  set +e
  {
    H "79a. Shared navigation bar fits phone widths in light and dark"
    eval "$SHARED_NAV_BODY"
  } >"$out" 2>&1
  set -e
}

run_load_section() {
  local out="$1"
  set +e
  {
    H "79. Web UI slow-network load scenarios - cold load, empty, failed, stale, offline, reload"
    eval "$LOAD_BODY"
  } >"$out" 2>&1
  set -e
}

invocation_count() { find "$FAKE_NODE_DIR" -name 'inv-*' 2>/dev/null | wc -l; }

inv_files_have_arg() { # <arg>
  grep -qxF "arg: $1" "$FAKE_NODE_DIR"/inv-*
}

recorded_var() { # <VAR> — value of the recorded env line, empty if absent
  awk -F'=' -v v="$1" '$1 == v {print; exit}' "$FAKE_NODE_DIR"/inv-*
}

# --- cases -----------------------------------------------------------------

# (a) SMOKE_ONLY set to another section id: the fake node is never called and
# the section prints its skip line.
case_a_nav_skipped_for_other_section_id() {
  export SMOKE_ONLY="1"
  case_setup
  local out="$CASE_DIR/out.txt"
  run_nav_section "$out"

  if [[ "$(invocation_count)" -eq 0 ]]; then
    pass "A: SMOKE_ONLY=1 (another section) — fake node never called"
  else
    fail "A: fake node called $(invocation_count) time(s) though SMOKE_ONLY=1"
  fi
  if grep -qF 'not in SMOKE_ONLY=1' "$out"; then
    pass "A: section printed its SMOKE_ONLY skip line"
  else
    fail "A: no SMOKE_ONLY skip line in section output: $(tr -d '\n' < "$out" | head -c 200)"
  fi
  if [[ "$FAIL" -eq 0 ]]; then
    pass "A: no FAIL counted for the skipped section"
  else
    fail "A: FAIL=$FAIL counted although the section was skipped"
  fi
}

# (b) Section selected by its own id (and by empty SMOKE_ONLY): node is
# called once with --group shared-nav, the four probe variables are non-empty
# in its environment, and the throwaway dirs are gone afterwards.
nav_selected_case() {
  local only="$1"
  export SMOKE_ONLY="$only"
  case_setup
  export FAKE_NODE_RC=0
  export FAKE_NODE_STDOUT="summary: 2 passed 0 failed 0 skipped"
  local out="$CASE_DIR/out.txt"
  run_nav_section "$out"

  local n
  n="$(invocation_count)"
  if [[ "$n" -eq 1 ]]; then
    pass "B($only): fake node called exactly once"
  else
    fail "B($only): fake node called $n time(s), want 1"
    return 0
  fi
  if inv_files_have_arg "--group" && inv_files_have_arg "shared-nav"; then
    pass "B($only): node called with --group shared-nav"
  else
    fail "B($only): node args missing --group shared-nav: $(tr -d '\n' < "$FAKE_NODE_DIR"/inv-* | head -c 300)"
  fi

  local bad="" v
  for v in PROBE_OUT_DIR PROBE_CHROME_PROFILE TEST_TLS TEST_TOKEN; do
    if [[ -z "$(recorded_var "$v")" ]]; then bad="$bad $v"; fi
  done
  if [[ -z "$bad" ]]; then
    pass "B($only): PROBE_OUT_DIR, PROBE_CHROME_PROFILE, TEST_TLS, TEST_TOKEN all non-empty in the node env"
  else
    fail "B($only): missing or empty env variable(s):$bad"
  fi

  local profile evd
  profile="$(recorded_var PROBE_CHROME_PROFILE)"
  evd="$(recorded_var PROBE_OUT_DIR)"
  if [[ -n "$profile" && -n "$evd" && ! -d "$profile" && ! -d "$evd" ]]; then
    pass "B($only): throwaway profile and evidence dirs removed afterwards"
  else
    fail "B($only): leftover dir(s): profile='$profile' evd='$evd'"
  fi

  if [[ "$FAIL" -eq 0 && "$PASS" -ge 1 ]]; then
    pass "B($only): passing run recorded as PASS (PASS=$PASS, FAIL=$FAIL)"
  else
    fail "B($only): counters wrong for a passing run: PASS=$PASS FAIL=$FAIL SKIP=$SKIP"
  fi
}

case_b1_nav_selected_by_own_id() { nav_selected_case "79a"; }
case_b2_nav_selected_with_empty_smoke_only() { nav_selected_case ""; }

# (c) Fake node simulating a missing browser: SKIP, never FAIL.
case_c1_nav_skip_exit0_all_skipped() {
  export SMOKE_ONLY="79a"
  case_setup
  export FAKE_NODE_RC=0
  export FAKE_NODE_STDOUT="summary: 0 passed 0 failed 1 skipped"
  local out="$CASE_DIR/out.txt"
  run_nav_section "$out"

  if [[ "$SKIP" -ge 1 && "$FAIL" -eq 0 ]]; then
    pass "C1: 0-passed run (exit 0) recorded as SKIP, no FAIL"
  else
    fail "C1: counters SKIP=$SKIP FAIL=$FAIL (want SKIP>=1, FAIL=0)"
  fi
  if grep -qF 'SKIP' "$out"; then
    pass "C1: section output shows the SKIP line"
  else
    fail "C1: no SKIP line in section output: $(tr -d '\n' < "$out" | head -c 200)"
  fi
}

case_c2_nav_skip_exit2() {
  export SMOKE_ONLY="79a"
  case_setup
  export FAKE_NODE_RC=2
  export FAKE_NODE_STDOUT="no browser at DW_CHROME_PATH"
  local out="$CASE_DIR/out.txt"
  run_nav_section "$out"

  if [[ "$SKIP" -ge 1 && "$FAIL" -eq 0 ]]; then
    pass "C2: exit-2 run recorded as SKIP, no FAIL"
  else
    fail "C2: counters SKIP=$SKIP FAIL=$FAIL (want SKIP>=1, FAIL=0)"
  fi
  if grep -qF 'runner exited 2' "$out"; then
    pass "C2: section output names the exit-2 skip reason"
  else
    fail "C2: no exit-2 skip line in section output: $(tr -d '\n' < "$out" | head -c 200)"
  fi
}

# (d) A failing run is a FAIL.
case_d_nav_failure_is_fail() {
  export SMOKE_ONLY="79a"
  case_setup
  export FAKE_NODE_RC=1
  export FAKE_NODE_STDOUT="summary: 1 passed 1 failed 0 skipped"
  local out="$CASE_DIR/out.txt"
  run_nav_section "$out"

  if [[ "$FAIL" -ge 1 ]]; then
    pass "D: failing run recorded as FAIL (FAIL=$FAIL)"
  else
    fail "D: no FAIL counted for a failing run (PASS=$PASS SKIP=$SKIP)"
  fi
  if grep -qF 'FAIL' "$out"; then
    pass "D: section output shows the FAIL line"
  else
    fail "D: no FAIL line in section output: $(tr -d '\n' < "$out" | head -c 200)"
  fi
}

# Load section: the same SECTION_SKIP guarantee — excluded by SMOKE_ONLY it
# never calls node and never fails.
case_e_load_skipped_for_other_section_id() {
  export SMOKE_ONLY="1"
  case_setup
  local out="$CASE_DIR/out.txt"
  run_load_section "$out"

  if [[ "$(invocation_count)" -eq 0 && "$FAIL" -eq 0 ]]; then
    pass "E: load section under SMOKE_ONLY=1 — no node call, no FAIL"
  else
    fail "E: load section node calls=$(invocation_count) FAIL=$FAIL under SMOKE_ONLY=1"
  fi
  if grep -qF 'not in SMOKE_ONLY=1' "$out"; then
    pass "E: load section printed its SMOKE_ONLY skip line"
  else
    fail "E: no SMOKE_ONLY skip line in load section output: $(tr -d '\n' < "$out" | head -c 200)"
  fi
}

# --- run -------------------------------------------------------------------

run_case() {
  local rc=0
  (
    FAILURES=0
    "$1"
    exit "$FAILURES"
  ) || rc=$?
  if (( rc > 0 )); then
    FAILURES=$((FAILURES + rc))
  fi
}

run_case case_a_nav_skipped_for_other_section_id
run_case case_b1_nav_selected_by_own_id
run_case case_b2_nav_selected_with_empty_smoke_only
run_case case_c1_nav_skip_exit0_all_skipped
run_case case_c2_nav_skip_exit2
run_case case_d_nav_failure_is_fail
run_case case_e_load_skipped_for_other_section_id

echo ""
if (( FAILURES > 0 )); then
  echo "test failed: $FAILURES assertion(s) failed"
  exit 1
fi
echo "test passed: shared navigation (79a) + load (79) section behaviour"
