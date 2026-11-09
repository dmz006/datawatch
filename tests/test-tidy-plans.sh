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
#   * registry hygiene: duplicate sections folded, closed tables merged,
#     duplicate rows dropped, verbose closed rows cut to a one-line
#     summary, stale draft rows corrected from a --status-file list,
#     hygiene output is counts only, and a second hygiene run changes
#     nothing (and --dry-run changes nothing)
#
# Usage:
#   tests/test-tidy-plans.sh            run all cases (default)
#   tests/test-tidy-plans.sh all        same
#   tests/test-tidy-plans.sh hygiene    run only the registry hygiene cases
#
# Exit 0 when every case passes, 1 otherwise.

set -euo pipefail
# A clean CI runner has no git identity: every commit the fixtures make needs one from the environment.
export GIT_AUTHOR_NAME="${GIT_AUTHOR_NAME:-tidy-test}" GIT_AUTHOR_EMAIL="${GIT_AUTHOR_EMAIL:-tidy-test@localhost}"
export GIT_COMMITTER_NAME="${GIT_COMMITTER_NAME:-tidy-test}" GIT_COMMITTER_EMAIL="${GIT_COMMITTER_EMAIL:-tidy-test@localhost}"

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
    cp "$REPO_DIR/scripts/plans_registry_hygiene.py" scripts/
    cp "$REPO_DIR/scripts/plans_monthly_archive.py" scripts/
    cp "$REPO_DIR/scripts/check-plans-links.sh" scripts/
  )
  NEW_REPO="$d"
  return 0
}

# Run the tools with the temp repo as working directory so their
# `git rev-parse --show-toplevel` resolves to the temp repo, never the
# one this file lives in.
run_tidy()    { local repo=$1; shift; ( cd "$repo" && exec bash scripts/tidy-plans.sh "$@" ); }
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
case_hygiene_registry_cleaned() {
  local repo out reg
  new_repo; repo=$NEW_REPO
  # a duplicate section ("## done" twice), duplicate row 1 kept in planned
  # and repeated inside the second "done" table, plus two all-closed tables
  cat > "$repo/docs/plans/README.md" <<'EOF'
# Registry

## planned

| # | Item | Status |
|---|------|--------|
| 1 | [one](one.md) | done, archived |
| 2 | [two](two.md) | in progress |

## done

| # | Item | Status |
|---|------|--------|
| 3 | [three](three.md) | shipped |

## done

| # | Item | Status |
|---|------|--------|
| 1 | [one](one.md) | done, archived |
| 4 | [four](four.md) | fixed |
EOF
  printf '# one\n'   > "$repo/docs/plans/one.md"
  printf '# two\n'   > "$repo/docs/plans/two.md"
  printf '# three\n' > "$repo/docs/plans/three.md"
  printf '# four\n'  > "$repo/docs/plans/four.md"
  if ! out=$(run_tidy "$repo" 2>&1); then fail "hygiene cleaned: tidy crashed: $out"; return; fi
  assert_line  "hygiene cleaned: duplicate section removed" "$out" 'duplicate sections removed: 1'
  assert_line  "hygiene cleaned: closed tables merged"      "$out" 'completed tables merged: 1'
  assert_line  "hygiene cleaned: duplicate row dropped"     "$out" 'duplicate rows removed: 1'
  reg=$(cat "$repo/docs/plans/README.md")
  assert_line  "hygiene cleaned: single 'done' section"     "$reg" '^## done$'
  [ "$(printf '%s\n' "$reg" | grep -c '^## done$')" -eq 1 ] \
    && ok "hygiene cleaned: exactly one 'done' section" \
    || fail "hygiene cleaned: expected exactly one 'done' section"
  [ "$(printf '%s\n' "$reg" | grep -c 'one\.md')" -eq 1 ] \
    && ok "hygiene cleaned: entry one kept exactly once" \
    || fail "hygiene cleaned: entry one must appear exactly once"
  assert_contains "hygiene cleaned: all four entries survive" "$reg" '[four](four.md)'
  assert_contains "hygiene cleaned: live entry untouched"     "$reg" 'in progress'
  assert_counts_only "hygiene cleaned: count-only output"     "$out"
}

case_hygiene_trim_and_dry_run() {
  local repo out before after long
  new_repo; repo=$NEW_REPO
  long=$(printf '%s ' $(for i in $(seq 1 40); do printf 'verbose-token-%03d' "$i"; done))
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

## backlog

| # | Item | Status |
|---|------|--------|
| 1 | [verbose](verbose.md) | done, shipped in v9.9.9. ${long}|
EOF
  printf '# verbose\n' > "$repo/docs/plans/verbose.md"
  before=$(md5sum "$repo/docs/plans/README.md")
  if ! out=$(run_tidy "$repo" --dry-run 2>&1); then fail "hygiene trim: tidy crashed: $out"; return; fi
  assert_line    "hygiene trim: dry-run reports trim"   "$out" 'closed rows trimmed: 1'
  [ "$(md5sum "$repo/docs/plans/README.md")" = "$before" ] \
    && ok "hygiene trim: dry-run changed nothing" \
    || fail "hygiene trim: dry-run must not change the registry"
  if ! out=$(run_tidy "$repo" 2>&1); then fail "hygiene trim: apply crashed: $out"; return; fi
  after=$(md5sum "$repo/docs/plans/README.md")
  [ "$before" != "$after" ] \
    && ok "hygiene trim: apply rewrote the registry" \
    || fail "hygiene trim: apply must change the registry"
  assert_line    "hygiene trim: counted"                "$out" 'closed rows trimmed: 1'
  reg=$(cat "$repo/docs/plans/README.md")
  case "$reg" in *'…'*) ok "hygiene trim: ellipsis present" ;; *) fail "hygiene trim: ellipsis present" ;; esac
  case "$reg" in *'verbose-token-040'*) fail "hygiene trim: tail text removed" ;; *) ok "hygiene trim: tail text removed" ;; esac
  case "$reg" in *'verbose-token-001'*) ok "hygiene trim: head text kept" ;; *) fail "hygiene trim: head text kept" ;; esac
  # the trimmed row must be exactly one table row (a one-line summary):
  # one line that is a row, mentions the head token, and ends with the ellipsis cell
  local one_line
  one_line=$(printf '%s\n' "$reg" | grep -cE '^\| *1 *\|.*verbose-token-001.*…' ) || true
  [ "${one_line:-0}" -eq 1 ] \
    && ok "hygiene trim: closed row became a one-line summary" \
    || fail "hygiene trim: closed row became a one-line summary (matching rows: $one_line)"
}

case_hygiene_stale_corrected() {
  local repo out reg
  new_repo; repo=$NEW_REPO
  cat > "$repo/statuses.json" <<'EOF'
[{"id":"deadbeef","status":"completed"},{"id":"feedface","status":"in_progress"}]
EOF
  cat > "$repo/docs/plans/README.md" <<'EOF'
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [drafted](drafted.md) | not started (draft PRD `deadbeef`) |
| 2 | [moving](moving.md) | in progress (PRD `feedface`) |
EOF
  printf '# drafted\n' > "$repo/docs/plans/drafted.md"
  printf '# moving\n'  > "$repo/docs/plans/moving.md"
  if ! out=$(run_tidy "$repo" 2>&1); then fail "hygiene stale: tidy crashed: $out"; return; fi
  assert_line  "hygiene stale: list unused without --status-file" "$out" 'stale rows corrected: 0'
  if ! out=$(run_tidy "$repo" --status-file statuses.json 2>&1); then fail "hygiene stale: tidy crashed: $out"; return; fi
  assert_line  "hygiene stale: exactly one corrected"    "$out" 'stale rows corrected: 1'
  reg=$(cat "$repo/docs/plans/README.md")
  assert_contains "hygiene stale: draft row now corrected"   "$reg" 'corrected (list: PRD `deadbeef` completed)'
  assert_contains "hygiene stale: live row left alone"       "$reg" 'in progress (PRD `feedface`)'
  [ "$(printf '%s\n' "$reg" | grep -c 'corrected')" -eq 1 ] \
    && ok "hygiene stale: only the stale row got a corrected prefix" \
    || fail "hygiene stale: only the stale row got a corrected prefix"
}

case_hygiene_idempotent() {
  local repo out1 out2 before
  new_repo; repo=$NEW_REPO
  cat > "$repo/docs/plans/README.md" <<'EOF'
# Registry

## backlog

| # | Item | Status |
|---|------|--------|
| 1 | [one](one.md) | done, archived |

## backlog

| # | Item | Status |
|---|------|--------|
| 2 | [two](two.md) | fixed |
EOF
  printf '# one\n' > "$repo/docs/plans/one.md"
  printf '# two\n' > "$repo/docs/plans/two.md"
  if ! out1=$(run_tidy "$repo" 2>&1); then fail "hygiene idem: first run crashed: $out1"; return; fi
  before=$(md5sum "$repo/docs/plans/README.md")
  if ! out2=$(run_tidy "$repo" 2>&1); then fail "hygiene idem: second run crashed: $out2"; return; fi
  [ "$(md5sum "$repo/docs/plans/README.md")" = "$before" ] \
    && ok "hygiene idem: second run changes nothing" \
    || fail "hygiene idem: second run changes nothing"
  assert_line "hygiene idem: second run all zero" "$out2" 'duplicate sections removed: 0'
}

# ── dispatch ─────────────────────────────────────────────────────────────
echo "── test-tidy-plans: mode=$MODE ──"
# ── reports / parity-issues monthly archive cases ────────────────────────
# Fixture: old filed + unfiled parity drafts, an old report with an open
# registry row, an old report with no reference (no name date prefix, dated
# by a git committer date 30 days ago), and an old report (name prefix)
# with only closed-row / plan-doc links.  Sets NEW_REPO and AGE_MONTH.
AGE_MONTH=""
build_archive_fixture() {
  local repo pd rp d30
  new_repo; repo=$NEW_REPO
  pd="$repo/docs/plans/parity-issues"; rp="$repo/docs/plans/reports"
  mkdir -p "$pd" "$rp"
  d30=$(date -d '30 days ago' '+%Y-%m-%dT12:00:00')
  AGE_MONTH=$(date -d '30 days ago' '+%Y-%m')
  printf '# filed draft\n\n## Filed issue\n\nhttps://github.com/example/repo/issues/55\n' > "$pd/${OLD}-filed.md"
  printf '# unfiled draft\n\n## Filed issue\n\nNot filed yet.\n' > "$pd/${OLD}-unfiled.md"
  printf '# open report\n' > "$rp/${OLD}-open-rep.md"
  printf '# unreferenced report\n' > "$rp/loose-report.md"
  printf '# linked report\n' > "$rp/${OLD}-linked-rep.md"
  printf '# linker plan\n\nSee [the report](reports/%s-linked-rep.md) and docs/plans/reports/%s-linked-rep.md.\n' "$OLD" "$OLD" > "$repo/docs/plans/${OLD}-linker.md"
  cat > "$repo/docs/plans/README.md" <<EOF
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [open report](reports/${OLD}-open-rep.md) | in progress |
| 2 | [linked report](reports/${OLD}-linked-rep.md) | done |
| 3 | [linker plan](${OLD}-linker.md) | in progress |
EOF
  (
    cd "$repo"
    git add -A
    GIT_AUTHOR_DATE="$d30" GIT_COMMITTER_DATE="$d30" git commit -q -m fixture
  )
  return 0
}

# hash of every file outside .git plus the porcelain status
repo_state() { ( cd "$1" && find . -path ./.git -prune -o -type f -print0 | sort -z | xargs -0 sha256sum; git status --porcelain ); }

case_archive_parity_filed_moves() {
  local repo out
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "parity filed: tidy crashed: $out"; return; fi
  assert_file    "parity filed: filed draft archived" "$repo/docs/plans/parity-issues/archive/2020-01/${OLD}-filed.md"
  assert_no_file "parity filed: filed draft left top-level" "$repo/docs/plans/parity-issues/${OLD}-filed.md"
  assert_line    "parity filed: counted" "$out" 'parity issues archived: 1'
}

case_archive_parity_unfiled_stays() {
  local repo out
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "parity unfiled: tidy crashed: $out"; return; fi
  assert_file    "parity unfiled: draft stays" "$repo/docs/plans/parity-issues/${OLD}-unfiled.md"
  assert_no_file "parity unfiled: not archived" "$repo/docs/plans/parity-issues/archive/2020-01/${OLD}-unfiled.md"
}

case_archive_report_open_row_stays() {
  local repo out
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "report open row: tidy crashed: $out"; return; fi
  assert_file    "report open row: report stays" "$repo/docs/plans/reports/${OLD}-open-rep.md"
  assert_no_file "report open row: not archived" "$repo/docs/plans/reports/archive/2020-01/${OLD}-open-rep.md"
}

case_archive_report_unreferenced_moves() {
  local repo out
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "report loose: tidy crashed: $out"; return; fi
  assert_file    "report loose: archived under its commit month" "$repo/docs/plans/reports/archive/${AGE_MONTH}/loose-report.md"
  assert_no_file "report loose: left top-level" "$repo/docs/plans/reports/loose-report.md"
  assert_line    "report loose: counted" "$out" 'reports archived: 2'
}

case_archive_links_rewritten() {
  local repo out reg doc
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "report links: tidy crashed: $out"; return; fi
  assert_file "report links: linked report archived" "$repo/docs/plans/reports/archive/2020-01/${OLD}-linked-rep.md"
  reg=$(cat "$repo/docs/plans/README.md")
  doc=$(cat "$repo/docs/plans/${OLD}-linker.md")
  assert_contains "report links: registry row rewritten" "$reg" "](reports/archive/2020-01/${OLD}-linked-rep.md)"
  assert_lacks    "report links: registry old link gone" "$reg" '\]\(reports/2020-01-01-linked-rep\.md\)'
  assert_contains "report links: doc link rewritten" "$doc" "](reports/archive/2020-01/${OLD}-linked-rep.md)"
  assert_contains "report links: doc path mention rewritten" "$doc" "docs/plans/reports/archive/2020-01/${OLD}-linked-rep.md"
  assert_lacks    "report links: doc old mention gone" "$doc" 'docs/plans/reports/2020-01-01-linked-rep'
  assert_contains "report links: open-row link untouched" "$reg" "](reports/${OLD}-open-rep.md)"
}

case_archive_link_check_passes() {
  local repo out n=""
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "link check: tidy crashed: $out"; return; fi
  if n=$(run_checker "$repo" 2>&1); then
    ok "link check: passes after the move"
  else
    printf '     checker said: %s\n' "$n" >&2; fail "link check: passes after the move"
  fi
  assert_line "link check: zero broken targets" "$n" '^0$'
}

case_archive_second_run_noop() {
  local repo before after out
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "second run: first tidy crashed: $out"; return; fi
  before=$(repo_state "$repo")
  if ! out=$(run_tidy "$repo" 2>&1); then fail "second run: tidy crashed: $out"; return; fi
  after=$(repo_state "$repo")
  if [ "$before" = "$after" ]; then ok "second run: file hashes and git status unchanged"
  else
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") >&2 || true
    fail "second run: file hashes and git status unchanged"
  fi
  assert_line "second run: nothing archived" "$out" 'reports archived: 0'
  assert_line "second run: no parity archived" "$out" 'parity issues archived: 0'
  assert_line "second run: no links rewritten" "$out" 'links rewritten: 0'
}

case_archive_counts_only() {
  local repo out
  build_archive_fixture; repo=$NEW_REPO
  if ! out=$(run_tidy "$repo" 2>&1); then fail "archive counts: tidy crashed: $out"; return; fi
  assert_counts_only "archive counts: no .md file name in output" "$out"
  assert_lacks "archive counts: no fixture names in output" "$out" 'filed|open-rep|loose-report|linked-rep|linker'
}

case "$MODE" in
  hygiene)
    case_hygiene_registry_cleaned
    case_hygiene_trim_and_dry_run
    case_hygiene_stale_corrected
    case_hygiene_idempotent
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
    case_archive_parity_filed_moves
    case_archive_parity_unfiled_stays
    case_archive_report_open_row_stays
    case_archive_report_unreferenced_moves
    case_archive_links_rewritten
    case_archive_link_check_passes
    case_archive_second_run_noop
    case_archive_counts_only
    case_hygiene_registry_cleaned
    case_hygiene_trim_and_dry_run
    case_hygiene_stale_corrected
    case_hygiene_idempotent
    ;;
esac

echo
if [ "$FAILED" -eq 0 ]; then
  echo "test-tidy-plans: all cases passed"
else
  echo "test-tidy-plans: FAILURES (see above)" >&2
  exit 1
fi
