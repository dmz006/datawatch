#!/usr/bin/env bash
#
# test-plans-tidy-daily.sh — end-to-end tests for scripts/plans-tidy-daily.sh.
#
# Every case builds a throwaway git repository under mktemp with a bare
# repository as origin, copies the tools in, and runs the daily runner
# there.  PLANS_TIDY_DAYS is small so the dated fixture files are old
# enough.  The real repository is never touched.
#
# Cases:
#   clean      old closed plan: docs/plans changes, one docs-only commit,
#              pushed to the bare origin, stamp result ok, report lists
#              unclassified names, stdout is counts only
#   linkfail   a broken link makes the simulated link check fail: tree and
#              remote unchanged, alert file written, exit 1
#   dirty      dirty docs/plans: runner refuses before changing anything
#   rerun      a second run changes nothing
#
# Usage: tests/test-plans-tidy-daily.sh [all|clean|linkfail|dirty|rerun]
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
  all|clean|linkfail|dirty|rerun) ;;
  *) printf 'usage: test-plans-tidy-daily.sh [all|clean|linkfail|dirty|rerun]\n' >&2; exit 2 ;;
esac

export PLANS_TIDY_DAYS=2
OLD=2020-01-01
FAILED=0
ok()   { printf 'ok   - %s\n' "$1"; }
fail() { printf 'FAIL - %s\n' "$1" >&2; FAILED=1; }

check() { # $1=description  then a command that must succeed
  local d=$1; shift
  if "$@"; then ok "$d"; else fail "$d"; fi
}
has_line() { printf '%s\n' "$1" | grep -Eq "$2"; }
no_md_names() { ! printf '%s\n' "$1" | grep -qE '\.md\b'; }

TMPDIRS=()
cleanup() {
  local d
  for d in "${TMPDIRS[@]:-}"; do
    if [ -n "$d" ]; then rm -rf "$d"; fi
  done
  return 0
}
trap cleanup EXIT

# new_fixture: sets WORK (clone) and ORIGIN (bare); one pushed commit holds
# an old closed plan, an unfiled old parity issue and the registry.
new_fixture() {
  local base
  base=$(mktemp -d)
  TMPDIRS+=("$base")
  ORIGIN="$base/origin.git"
  WORK="$base/work"
  git init -q --bare -b main "$ORIGIN"
  git init -q -b main "$WORK"
  (
    cd "$WORK"
    git config user.name "tidy-test"
    git config user.email "tidy-test@localhost"
    mkdir -p scripts docs/plans/parity-issues
    local f
    for f in tidy-plans.sh plans_links.py plans_registry_hygiene.py \
             plans_monthly_archive.py check-plans-links.sh plans-tidy-daily.sh; do
      cp "$REPO_DIR/scripts/$f" scripts/
    done
    printf '# stale plan\n' > "docs/plans/${OLD}-stale.md"
    printf '# open parity gap\n\nNothing filed yet.\n' > "docs/plans/parity-issues/${OLD}-gap.md"
    cat > docs/plans/README.md <<EOT
# Registry

| # | Item | Status |
|---|------|--------|
| 1 | [stale plan](${OLD}-stale.md) | done, shipped |
EOT
    printf '.datawatch/\n__pycache__/\n' > .gitignore
    git add -A
    git commit -q -m "fixture"
    git remote add origin "$ORIGIN"
    git push -q origin main
  )
}

run_daily() { # $1=repo; sets OUT and RC
  set +e
  OUT=$(cd "$1" && env -u TMUX -u TMUX_TMPDIR bash scripts/plans-tidy-daily.sh 2>&1)
  RC=$?
  set -e
}

head_of()   { git -C "$1" rev-parse HEAD; }
origin_of() { git -C "$ORIGIN" rev-parse refs/heads/main; }
stamp()     { cat "$WORK/.datawatch/reports/plans-tidy-last-run.json" 2>/dev/null || true; }

case_clean() {
  new_fixture
  local before after
  before=$(head_of "$WORK")
  run_daily "$WORK"
  after=$(head_of "$WORK")
  check "clean: exit 0" [ "$RC" -eq 0 ]
  check "clean: plan archived" [ -f "$WORK/docs/plans/historical-plans/${OLD}-stale.md" ]
  check "clean: plan gone from top level" [ ! -e "$WORK/docs/plans/${OLD}-stale.md" ]
  check "clean: exactly one new commit" [ "$(git -C "$WORK" rev-list --count "$before..$after")" -eq 1 ]
  check "clean: commit is docs-only" \
    [ -z "$(git -C "$WORK" diff --name-only "$before" "$after" | grep -v '^docs/plans/' || true)" ]
  check "clean: commit message" [ "$(git -C "$WORK" log -1 --format=%s)" = "docs: daily plans tidy" ]
  check "clean: pushed to the bare remote" [ "$(origin_of)" = "$after" ]
  check "clean: stamp result ok" has_line "$(stamp)" '"result": "ok"'
  check "clean: stamp carries commit" has_line "$(stamp)" "\"commit\": \"$after\""
  check "clean: report lists unclassified name" \
    grep -q "${OLD}-gap.md" "$WORK/.datawatch/reports/plans-tidy-daily-report.md"
  check "clean: stdout reports unclassified count" has_line "$OUT" '^unclassified: 1$'
  check "clean: stdout reports committed and pushed" has_line "$OUT" '^result: committed and pushed$'
  check "clean: stdout is counts only" no_md_names "$OUT"
  check "clean: no alert file" [ ! -e "$WORK/.datawatch/reports/plans-tidy-alert.md" ]
  check "clean: working tree clean" [ -z "$(git -C "$WORK" status --porcelain)" ]
}

case_linkfail() {
  new_fixture
  # a committed doc whose plan link is broken: the simulated check fails
  (
    cd "$WORK"
    mkdir -p docs
    printf '# guide\nSee [gone](plans/does-not-exist.md).\n' > docs/guide.md
    git add -A
    git commit -q -m "broken link"
    git push -q origin main
  )
  local before remote_before
  before=$(head_of "$WORK")
  remote_before=$(origin_of)
  run_daily "$WORK"
  check "linkfail: exit 1" [ "$RC" -eq 1 ]
  check "linkfail: HEAD unchanged" [ "$(head_of "$WORK")" = "$before" ]
  check "linkfail: remote unchanged" [ "$(origin_of)" = "$remote_before" ]
  check "linkfail: tree unchanged" [ -z "$(git -C "$WORK" status --porcelain)" ]
  check "linkfail: plan still top-level" [ -f "$WORK/docs/plans/${OLD}-stale.md" ]
  check "linkfail: alert file written" [ -s "$WORK/.datawatch/reports/plans-tidy-alert.md" ]
  check "linkfail: stamp result" has_line "$(stamp)" '"result": "link_check_failed"'
  check "linkfail: stdout is one ALERT line" has_line "$OUT" '^ALERT: plans tidy link_check_failed'
  check "linkfail: no simulation worktree left" \
    [ -z "$(git -C "$WORK" worktree list --porcelain | grep -c 'sim-' | grep -v '^0$' || true)" ]
}

case_dirty() {
  new_fixture
  printf 'scratch\n' > "$WORK/docs/plans/untracked-note.md"
  printf '\nedited\n' >> "$WORK/docs/plans/README.md"
  local before remote_before status_before readme_before
  before=$(head_of "$WORK")
  remote_before=$(origin_of)
  status_before=$(git -C "$WORK" status --porcelain --untracked-files=all)
  readme_before=$(cat "$WORK/docs/plans/README.md")
  run_daily "$WORK"
  check "dirty: exit 1" [ "$RC" -eq 1 ]
  check "dirty: HEAD unchanged" [ "$(head_of "$WORK")" = "$before" ]
  check "dirty: remote unchanged" [ "$(origin_of)" = "$remote_before" ]
  check "dirty: status unchanged" \
    [ "$(git -C "$WORK" status --porcelain --untracked-files=all)" = "$status_before" ]
  check "dirty: README untouched" [ "$(cat "$WORK/docs/plans/README.md")" = "$readme_before" ]
  check "dirty: plan not moved" [ -f "$WORK/docs/plans/${OLD}-stale.md" ]
  check "dirty: untracked file kept" [ -f "$WORK/docs/plans/untracked-note.md" ]
  check "dirty: stamp result" has_line "$(stamp)" '"result": "preflight_failed"'
  check "dirty: alert file written" [ -s "$WORK/.datawatch/reports/plans-tidy-alert.md" ]
  check "dirty: stdout is one ALERT line" has_line "$OUT" '^ALERT: plans tidy preflight_failed'
}

case_rerun() {
  new_fixture
  run_daily "$WORK"
  [ "$RC" -eq 0 ] || fail "rerun: first run failed: $OUT"
  local head1 remote1 tree1
  head1=$(head_of "$WORK")
  remote1=$(origin_of)
  tree1=$(git -C "$WORK" ls-files -s | sha1sum)
  run_daily "$WORK"
  check "rerun: exit 0" [ "$RC" -eq 0 ]
  check "rerun: no new commit" [ "$(head_of "$WORK")" = "$head1" ]
  check "rerun: remote unchanged" [ "$(origin_of)" = "$remote1" ]
  check "rerun: index unchanged" [ "$(git -C "$WORK" ls-files -s | sha1sum)" = "$tree1" ]
  check "rerun: tree clean" [ -z "$(git -C "$WORK" status --porcelain)" ]
  check "rerun: stamp nothing_to_do" has_line "$(stamp)" '"result": "nothing_to_do"'
  check "rerun: stdout says no changes" has_line "$OUT" '^result: no changes$'
  check "rerun: stdout is counts only" no_md_names "$OUT"
}

if [ "$MODE" = all ] || [ "$MODE" = clean ];    then case_clean;    fi
if [ "$MODE" = all ] || [ "$MODE" = linkfail ]; then case_linkfail; fi
if [ "$MODE" = all ] || [ "$MODE" = dirty ];    then case_dirty;    fi
if [ "$MODE" = all ] || [ "$MODE" = rerun ];    then case_rerun;    fi

if [ "$FAILED" -ne 0 ]; then
  printf 'test-plans-tidy-daily: FAILED\n' >&2
  exit 1
fi
printf 'test-plans-tidy-daily: all cases passed\n'
