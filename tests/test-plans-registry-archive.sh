#!/usr/bin/env bash
#
# test-plans-registry-archive.sh — self-contained tests for the closed-row
# archive pass of scripts/plans_registry_hygiene.py (the registry archive).
#
# Each run builds a throwaway git repository under mktemp, copies the code
# under test in, and removes it on exit.  The real docs/plans/ is never
# touched.  --archive-month fixes the month so results do not depend on date.
#
# Fixture registry: Stage A (closed + open rows, the current stage), Stage B
# (closed rows only), and a Rules section.
#
# Asserted:
#   * closed rows of the non-current stage land in roadmap-archive-YYYY-MM.md
#     with their numbers and row-N anchors
#   * open rows and current-stage rows stay in the live file
#   * the live file has exactly one month line linking to the archive
#   * row numbers are unchanged
#   * a second run changes nothing
#   * --dry-run changes nothing
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

MONTH=2026-10
ARCHIVE="roadmap-archive-${MONTH}.md"

FAILED=0
ok()   { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n' "$1" >&2; FAILED=1; }

has()  { grep -Fq -- "$2" "$1"; }   # $1=file $2=literal
count_lines() { grep -Fc -- "$2" "$1" || true; }

TMPDIRS=()
cleanup() {
  local d
  for d in "${TMPDIRS[@]:-}"; do
    if [ -n "$d" ]; then rm -rf "$d"; fi
  done
  return 0
}
trap cleanup EXIT

new_repo() {
  local d
  d=$(mktemp -d)
  TMPDIRS+=("$d")
  (
    cd "$d"
    git init -q .
    git config user.name "archive-test"
    git config user.email "archive-test@localhost"
    mkdir -p scripts docs/plans
    cp "$REPO_DIR/scripts/plans_registry_hygiene.py" scripts/
    cat > docs/plans/README.md <<'REG'
# Registry

## Active order

Work the stages in order.

## Stage A

| # | Item | Status |
|---|------|--------|
| 1 | Alpha closed | done in v1.0 |
| 2 | Beta open | in progress |

## Stage B

| # | Item | Status |
|---|------|--------|
| 3 | Gamma closed | shipped in v1.1 |
| 4 | Delta closed | done in v1.2 |

## Rules

| # | Item | Status |
|---|------|--------|
| 5 | Rule closed | done |
| 6 | Rule open | in progress |
REG
  )
  NEW_REPO="$d"
}

run_hygiene() { local repo=$1; shift; ( cd "$repo" && python3 scripts/plans_registry_hygiene.py "$repo" --archive-month "$MONTH" "$@" ); }

snapshot() { ( cd "$1/docs/plans" && find . -type f -print0 | sort -z | xargs -0 sha256sum ); }

case_archive_moves_closed_rows() {
  local repo live arc
  new_repo; repo=$NEW_REPO
  live="$repo/docs/plans/README.md"; arc="$repo/docs/plans/$ARCHIVE"
  run_hygiene "$repo" >/dev/null || { fail "archive: hygiene crashed"; return; }

  [ -f "$arc" ] && ok "archive: archive file created" || { fail "archive: $ARCHIVE missing"; return; }

  for n in 3 4; do
    has "$arc" "<a id=\"row-$n\"></a>$n" && ok "archive: row $n in archive with anchor" \
      || fail "archive: row $n anchor missing in archive"
    has "$live" "| $n | " && fail "archive: row $n still in live file" \
      || ok "archive: row $n gone from live file"
  done
  has "$arc" "Gamma closed" && has "$arc" "Delta closed" \
    && ok "archive: closed Stage B row text kept" || fail "archive: row text lost"

  for n in 1 2; do
    has "$live" "| $n | " && ok "archive: current-stage row $n stays live" \
      || fail "archive: current-stage row $n missing from live file"
    has "$arc" "row-$n\"" && fail "archive: row $n wrongly archived" \
      || ok "archive: row $n not in archive"
  done
  has "$live" "Beta open" && ok "archive: open row stays live" || fail "archive: open row lost"

  has "$live" "## Rules" && has "$live" "| 5 | Rule closed" \
    && ok "archive: Rules section row untouched" || fail "archive: Rules row moved or lost"
  has "$arc" "Rule closed" && fail "archive: Rules row archived" || ok "archive: Rules row not archived"

  local n_month
  n_month=$(count_lines "$live" "](roadmap-archive-${MONTH}.md)")
  [ "$n_month" = 1 ] && ok "archive: exactly one month line" \
    || fail "archive: expected 1 month line, got $n_month"
  has "$live" "- [Closed rows archived for $MONTH](roadmap-archive-${MONTH}.md), rows 3 to 4" \
    && ok "archive: month line text and row range" || fail "archive: month line text wrong"

  # row numbers across live + archive are exactly 1..6, each once
  local nums
  nums=$(cat "$live" "$arc" | grep -oE '^\| (<a id="row-[0-9]+"></a>)?[0-9]+ ' \
         | grep -oE '[0-9]+ $' | tr -d ' ' | sort -n | tr '\n' ' ')
  [ "$nums" = "1 2 3 4 5 6 " ] && ok "archive: row numbers unchanged (1-6, once each)" \
    || fail "archive: row numbers changed: $nums"
}

case_archive_second_run_noop() {
  local repo before after
  new_repo; repo=$NEW_REPO
  run_hygiene "$repo" >/dev/null || { fail "second run: first run crashed"; return; }
  before=$(snapshot "$repo")
  run_hygiene "$repo" >/dev/null || { fail "second run: crashed"; return; }
  after=$(snapshot "$repo")
  [ "$before" = "$after" ] && ok "second run: nothing changed" || fail "second run: files changed"
  [ "$(count_lines "$repo/docs/plans/README.md" "](roadmap-archive-${MONTH}.md)")" = 1 ] \
    && ok "second run: still one month line" || fail "second run: month line duplicated"
}

case_archive_dry_run_noop() {
  local repo before after
  new_repo; repo=$NEW_REPO
  before=$(snapshot "$repo")
  run_hygiene "$repo" --dry-run >/dev/null || { fail "dry-run: crashed"; return; }
  after=$(snapshot "$repo")
  [ "$before" = "$after" ] && ok "dry-run: nothing changed" || fail "dry-run: files changed"
  [ ! -e "$repo/docs/plans/$ARCHIVE" ] && ok "dry-run: no archive file written" \
    || fail "dry-run: archive file created"
}

case_archive_moves_closed_rows
case_archive_second_run_noop
case_archive_dry_run_noop

echo
if [ "$FAILED" -eq 0 ]; then
  echo "test-plans-registry-archive: all cases passed"
else
  echo "test-plans-registry-archive: FAILURES (see above)" >&2
  exit 1
fi
