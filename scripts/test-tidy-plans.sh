#!/usr/bin/env bash
#
# test-tidy-plans.sh — self-contained tests for scripts/tidy-plans.sh,
# scripts/plans_links.py, and scripts/check-plans-links.sh.
#
# Every case builds its own throwaway git repository under a mktemp
# directory, copies the code under test in, exercises one piece of the
# contract, and cleans up on exit.  The real docs/plans/ is never
# touched (each tool resolves its repo root from the temp repo, so
# commands are always run with that temp repo as the working directory).
#
# Covered:
#   * a plan linked from an open registry row stays
#   * a plan linked from a script, a rules file, or a doc stays
#   * an unreferenced closed plan is archived
#   * links to a moved file are rewritten
#   * the links check fails on a broken link and passes when clean
#   * tidy output contains counts only (never a file list)
#
# Usage:
#   scripts/test-tidy-plans.sh            run all cases (default)
#   scripts/test-tidy-plans.sh all        same
#   scripts/test-tidy-plans.sh hygiene    run only the registry hygiene cases
#
# Exit 0 when every case passes, 1 otherwise.

set -euo pipefail

# never attach to a tmux session while testing
unset TMUX TMUX_TMPDIR

SELF_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_DIR=$(cd "$SELF_DIR/.." && pwd)

MODE="${1:-all}"
case "$MODE" in
  all|hygiene) ;;
  *) printf 'usage: test-tidy-plans.sh [all|hygiene]\n' >&2; exit 2 ;;
esac

FAILED=0
ok()   { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n' "$1" >&2; FAILED=1; }

# ── assertions ───────────────────────────────────────────────────────────
assert_line() { # $1=description  $2=haystack  $3=ERE some line must match
  printf '%s\n' "$2" | grep -Eq "$3" \
    || { printf '     want line matching: %s\n     got:\n%s\n' "$3" "$2" >&2; fail "$1"; }
  ok "$1"
}
assert_contains() { # $1=description  $2=haystack  $3=literal substring
  case "$2" in
    *"$3"*) ok "$1" ;;
    *) printf '     want substring: %s\n     got:\n%s\n' "$3" "$2" >&2; fail "$1" ;;
  esac
}
assert_lacks() { # $1=description  $2=haystack  $3=ERE that must not appear
  if printf '%s\n' "$2" | grep -Eq "$3"; then printf '     found: %s\n' "$3" >&2; fail "$1"; else ok "$1"; fi
}
assert_file()  { [ -f "$2" ] && ok "$1" || fail "$1 (missing $2: $(basename -- "$2"))"; }
assert_no_file() { [ ! -e "$2" ] && ok "$1" || fail "$1 (unexpected: $(basename -- "$2"))"; }
# count-only output: no line may mention a .md filename
assert_counts_only() { # $1=description  $2=haystack
  if printf '%s\n' "$2" | grep -qE '\.md\b'; then
    printf '     got:\n%s\n' "$2" >&2; fail "$1"
  else
    ok "$1"
  fi
}

# ── scratch repos ────────────────────────────────────────────────────────
TMPDIRS=()
cleanup() {
  local d
  for d in "${TMPDIRS[@]:-}"; do
    if [ -n "$d" ]; then rm -rf "$d"; fi
  done
  return 0
}
trap cleanup EXIT

# Build a fresh throwaway git repo with the code under test copied in;
# sets NEW_REPO and registers the dir for cleanup.
new_repo() {
  local d
  d=$(mktemp -d)
  TMPDIRS+=("$d")
  (
    cd "$d"
    git init -q .
    git config user.name "tidy-test"
    git config user.email "tidy-test@localhost"
    mkdir -p scripts docs/plans
    cp "$REPO_DIR/scripts/tidy-plans.sh"        scripts/
    cp "$REPO_DIR/scripts/plans_links.py"       scripts/
    cp "$REPO_DIR/scripts/check-plans-links.sh" scripts/
  )
  NEW_REPO="$d"
  return 0
}

# Run the tools with the temp repo as working directory so their
# `git rev-parse --show-toplevel` resolves to the temp repo, never the
# one this file lives in.
run_tidy()    { ( cd "$1" && exec bash scripts/tidy-plans.sh ); }
run_checker() { ( cd "$1" && exec bash scripts/check-plans-links.sh ); }

# Old date: far past the default 7-day age cutoff.
OLD=2020-01-01

# ── cases ────────────────────────────────────────────────────────────────
case_open_row_kept() {
  local repo out
  new_repo; repo=$NEW_REPO
  printf '# open-row plan\n' > "$repo/docs/plans/${OLD}-open-row.md"
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [open-row plan](${OLD}-open-row.md) | in progress |
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "open row: tidy crashed: $out"; return; fi
  assert_line "open row: plan kept"                "$out" 'plans kept: 1'
  assert_line "open row: nothing archived"         "$out" 'plans archived: 0'
  assert_file   "open row: plan still top-level"   "$repo/docs/plans/${OLD}-open-row.md"
  assert_no_file "open row: no historical copy"    "$repo/docs/plans/historical-plans/${OLD}-open-row.md"
  assert_counts_only "open row: count-only output" "$out"
}

case_script_ref_kept() {
  local repo out
  new_repo; repo=$NEW_REPO
  printf '# scripted plan\n' > "$repo/docs/plans/${OLD}-script-ref.md"
  # no registry row for this plan: only the script reference keeps it
  cat > "$repo/scripts/tool.sh" <<EOF
#!/usr/bin/env bash
# uses the flow in docs/plans/${OLD}-script-ref.md
echo ok
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "script ref: tidy crashed: $out"; return; fi
  assert_line  "script ref: plan kept"        "$out" 'plans kept: 1'
  assert_line  "script ref: nothing archived" "$out" 'plans archived: 0'
  assert_file  "script ref: plan still top-level" "$repo/docs/plans/${OLD}-script-ref.md"
}

case_rules_ref_kept() {
  local repo out
  new_repo; repo=$NEW_REPO
  printf '# rules-linked plan\n' > "$repo/docs/plans/${OLD}-rule-ref.md"
  # root rules file (no registry row for this plan)
  cat > "$repo/AGENT.md" <<EOF
# rules
- the flow from [rule ref](docs/plans/${OLD}-rule-ref.md) applies
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "rules ref: tidy crashed: $out"; return; fi
  assert_line  "rules ref: plan kept"        "$out" 'plans kept: 1'
  assert_line  "rules ref: nothing archived" "$out" 'plans archived: 0'
  assert_file  "rules ref: plan still top-level" "$repo/docs/plans/${OLD}-rule-ref.md"
}

case_docs_ref_kept() {
  local repo out
  new_repo; repo=$NEW_REPO
  printf '# doc-linked plan\n' > "$repo/docs/plans/${OLD}-doc-ref.md"
  # a file under docs/ outside docs/plans/ (no registry row for this plan)
  cat > "$repo/docs/guide.md" <<EOF
# guide
See [doc ref](plans/${OLD}-doc-ref.md) for details.
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "doc ref: tidy crashed: $out"; return; fi
  assert_line  "doc ref: plan kept"        "$out" 'plans kept: 1'
  assert_line  "doc ref: nothing archived" "$out" 'plans archived: 0'
  assert_file  "doc ref: plan still top-level" "$repo/docs/plans/${OLD}-doc-ref.md"
}

case_closed_unreferenced_archived() {
  local repo out
  new_repo; repo=$NEW_REPO
  printf '# stale plan\n' > "$repo/docs/plans/${OLD}-stale.md"
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [stale plan](${OLD}-stale.md) | done, shipped |
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "archived: tidy crashed: $out"; return; fi
  assert_line  "archived: exactly one archived"       "$out" 'plans archived: 1'
  assert_line  "archived: none kept"                  "$out" 'plans kept: 0'
  assert_file  "archived: plan in historical-plans/"  "$repo/docs/plans/historical-plans/${OLD}-stale.md"
  assert_no_file "archived: gone from plans top level" "$repo/docs/plans/${OLD}-stale.md"
  assert_counts_only "archived: count-only output" "$out"
}

case_links_rewritten() {
  local repo out reg
  new_repo; repo=$NEW_REPO
  printf '# moved plan\n' > "$repo/docs/plans/${OLD}-moved.md"
  # three closed registry rows referencing the plan three different ways
  # (row links, bare name, absolute path).  Closed rows never *keep* the
  # plan, but every reference is rewritten when it moves.
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [moved plan](${OLD}-moved.md) | merged |
| 2 | follow-up in plans/${OLD}-moved.md | cancelled |
| 3 | absolute mention docs/plans/${OLD}-moved.md | done |
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "rewritten: tidy crashed: $out"; return; fi
  assert_line "rewritten: plan archived"   "$out" 'plans archived: 1'
  assert_line "rewritten: none kept"       "$out" 'plans kept: 0'
  assert_line "rewritten: all 3 links rewritten" "$out" 'links rewritten: 3'
  assert_file  "rewritten: plan in historical-plans/" "$repo/docs/plans/historical-plans/${OLD}-moved.md"
  assert_no_file "rewritten: plan moved out of plans/" "$repo/docs/plans/${OLD}-moved.md"
  reg=$(cat "$repo/docs/plans/README.md")
  assert_contains "rewritten: row link now historical"      "$reg" "](historical-plans/${OLD}-moved.md)"
  assert_lacks    "rewritten: no old row link remains"      "$reg" '\]\(2020-01-01-moved\.md\)'
  assert_contains "rewritten: bare path now historical"     "$reg" "plans/historical-plans/${OLD}-moved.md"
  assert_lacks    "rewritten: no old bare path remains"     "$reg" 'in plans/2020-01-01-moved'
  assert_contains "rewritten: absolute path now historical" "$reg" "docs/plans/historical-plans/${OLD}-moved.md"
  assert_lacks    "rewritten: no old absolute path remains" "$reg" 'docs/plans/2020-01-01-moved'
}

case_links_check() {
  local repo out
  new_repo; repo=$NEW_REPO
  # every plan the registry links to exists -> clean
  printf '# kept one\n' > "$repo/docs/plans/${OLD}-kept-one.md"
  printf '# kept two\n' > "$repo/docs/plans/${OLD}-kept-two.md"
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [open plan](${OLD}-kept-one.md) | in progress |
| 2 | [done plan](${OLD}-kept-two.md) | done |
EOF

  # clean: checker passes and prints only the zero count
  if out=$(run_checker "$repo" 2>/dev/null); then
    ok "links check: passes when clean"
  else
    fail "links check: expected clean exit on a link-clean repo (out: $out)"
  fi
  assert_line "links check: count is 0"                    "$out" '^0$'
  assert_counts_only "links check: counts only, no names" "$out"

  # broken: a doc links at a plan that does not exist anywhere
  cat > "$repo/docs/broken.md" <<EOF
# broken
See [ghost](plans/2019-05-05-ghost-plan.md).
EOF
  if out=$(run_checker "$repo" 2>/dev/null); then
    fail "links check: expected non-zero exit with a broken link"
  else
    ok "links check: fails on a broken link"
  fi
  assert_line "links check: counts the broken link" "$out" '^[1-9][0-9]*$'
  assert_counts_only "links check: counts only, no names" "$out"

  # back to clean: removing the broken link passes again
  rm "$repo/docs/broken.md"
  if ! out=$(run_checker "$repo" 2>/dev/null); then
    fail "links check: expected clean exit after removing the bad link (out: $out)"
  else
    ok "links check: passes again after fix"
  fi
  assert_line "links check: count back to 0" "$out" '^0$'
}

case_counts_only_output() {
  local repo out
  new_repo; repo=$NEW_REPO
  # mixed registry: one closed row (archived), one open row (kept)
  printf '# stale mixed\n' > "$repo/docs/plans/${OLD}-stale-mixed.md"
  printf '# live mixed\n'  > "$repo/docs/plans/${OLD}-live-mixed.md"
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [stale](${OLD}-stale-mixed.md) | cancelled |
| 2 | [live](${OLD}-live-mixed.md) | in progress |
EOF
  if ! out=$(run_tidy "$repo" 2>&1); then fail "counts: tidy crashed: $out"; return; fi
  assert_line        "counts: kept and archived both reported"  "$out" 'plans kept: 1'
  assert_line        "counts: "                                    "$out" 'plans archived: 1'
  assert_counts_only "counts: no .md filenames in output"         "$out"
  assert_lacks       "counts: no absolute paths in output"        "$out" '^/'
}

# ── registry hygiene cases ──────────────────────────────────────────────
# (populated by the registry hygiene task)
case_hygiene_placeholder() {
  echo "hygiene: no registry hygiene cases registered yet"
}

# ── dispatch ─────────────────────────────────────────────────────────────
echo "── test-tidy-plans: mode=$MODE ──"
case "$MODE" in
  hygiene)
    case_hygiene_placeholder
    ;;
  *)
    case_open_row_kept
    case_script_ref_kept
    case_rules_ref_kept
    case_docs_ref_kept
    case_closed_unreferenced_archived
    case_links_rewritten
    case_links_check
    case_counts_only_output
    case_hygiene_placeholder
    ;;
esac

echo
if [ "$FAILED" -eq 0 ]; then
  echo "test-tidy-plans: all cases passed"
else
  echo "test-tidy-plans: FAILURES (see above)" >&2
  exit 1
fi
