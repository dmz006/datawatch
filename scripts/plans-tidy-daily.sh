#!/usr/bin/env bash
#
# plans-tidy-daily.sh — run scripts/tidy-plans.sh once a day, safely.
#
# Steps:
#   1. Preflight: the project is a git repository and docs/plans/ has no
#      uncommitted or untracked changes.  Either failure raises an alert
#      and exits 1 without touching anything.
#   2. Simulate: the tidy and the plans link check run in a detached,
#      throwaway worktree of HEAD under .datawatch/scratch/.  The worktree
#      is removed afterwards on every exit path.
#   3. A failing simulated link check raises an alert and exits 1; the
#      real tree is not changed.
#   4. The tidy runs in the real tree, then the link check runs again.
#      If that check fails, docs/plans is restored (index and worktree),
#      only the files the tidy created under docs/plans are removed, an
#      alert is raised and the script exits 1.
#   5. If docs/plans changed, only docs/plans is staged (a staged path
#      outside docs/plans refuses the commit), committed as
#      'docs: daily plans tidy' with a counts line, and pushed to
#      PLANS_TIDY_REMOTE / PLANS_TIDY_BRANCH.  A push failure raises an
#      alert and exits 1 but keeps the local commit.
#   6. Counts only are printed to stdout.  On failure stdout carries one
#      ALERT line instead.
#
# Outputs, all under .datawatch/reports/ and written on every run:
#   plans-tidy-last-run.json     run stamp: ran_at (UTC), result, counts, commit
#   plans-tidy-daily-report.md   counts and the unclassified file names
#                                (file names appear here only, never on stdout)
#   plans-tidy-alert.md          written on failure (result name and counts,
#                                no file names); removed by a successful run
#
# Result names (the "result" field of the stamp):
#   ok                 tidy committed and pushed
#   nothing_to_do      tidy changed nothing in docs/plans
#   preflight_failed   settings, git, preflight, simulation or real tidy failed
#   link_check_failed  plan links broken after the simulated or the real tidy
#   push_failed        commit kept locally, push failed
#
# Settings (environment, with defaults):
#   PLANS_TIDY_DAYS    plan-age threshold passed as --days   (7)
#   PLANS_TIDY_REMOTE  push remote                           (origin)
#   PLANS_TIDY_BRANCH  push branch                           (current branch)
#
# Usage:
#   scripts/plans-tidy-daily.sh
#
# The version fields in cmd/datawatch/main.go and internal/server/api.go
# are never touched.

set -euo pipefail

PLANS_TIDY_DAYS=${PLANS_TIDY_DAYS:-7}
PLANS_TIDY_REMOTE=${PLANS_TIDY_REMOTE:-origin}

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
REPORT_DIR="$ROOT/.datawatch/reports"
SCRATCH_DIR="$ROOT/.datawatch/scratch"
STAMP_FILE="$REPORT_DIR/plans-tidy-last-run.json"
DAILY_REPORT="$REPORT_DIR/plans-tidy-daily-report.md"
ALERT_FILE="$REPORT_DIR/plans-tidy-alert.md"
SIM_DIR=""

RUN_AT=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
RESULT="preflight_failed"
COMMIT=""
MSG=""
MOVED=0
LINKS_REWRITTEN=0
ROWS_ARCHIVED=0
UNCLASSIFIED=0
DAYS_USED="$PLANS_TIDY_DAYS"

remove_sim() {
  if [ -n "$SIM_DIR" ] && [ -e "$SIM_DIR" ]; then
    git -C "$ROOT" worktree remove --force "$SIM_DIR" >/dev/null 2>&1 || rm -rf "$SIM_DIR"
    git -C "$ROOT" worktree prune >/dev/null 2>&1 || true
  fi
  SIM_DIR=""
}
trap remove_sim EXIT

# write_report: the run stamp and the daily report, written on every run.
write_report() {
  mkdir -p "$REPORT_DIR" 2>/dev/null || return 0
  printf '{\n  "ran_at": "%s",\n  "result": "%s",\n  "moved": %s,\n  "links_rewritten": %s,\n  "rows_archived": %s,\n  "unclassified": %s,\n  "commit": "%s"\n}\n' \
    "$RUN_AT" "$RESULT" "$MOVED" "$LINKS_REWRITTEN" "$ROWS_ARCHIVED" "$UNCLASSIFIED" "$COMMIT" \
    > "$STAMP_FILE" 2>/dev/null || true

  local names=""
  if [ -f "$ROOT/scripts/plans_monthly_archive.py" ]; then
    names=$(cd "$ROOT" && python3 scripts/plans_monthly_archive.py "$ROOT" \
      --list-unclassified --days "$DAYS_USED" 2>/dev/null || true)
  fi
  {
    echo "# Plans tidy daily report"
    echo
    echo "ran_at: $RUN_AT"
    echo "result: $RESULT"
    echo "moved: $MOVED"
    echo "links rewritten: $LINKS_REWRITTEN"
    echo "rows archived: $ROWS_ARCHIVED"
    echo "unclassified: $UNCLASSIFIED"
    if [ -n "$COMMIT" ]; then
      echo "commit: $COMMIT"
    fi
    echo
    echo "## Unclassified files"
    echo
    if [ -n "$names" ]; then
      printf '%s\n' "$names"
    else
      echo "(none)"
    fi
  } > "$DAILY_REPORT" 2>/dev/null || true
}

# finish: write the reports, then print counts (success) or one ALERT line
# (failure) and exit with the matching status.
finish() {
  remove_sim
  write_report
  if [ "$RESULT" = "ok" ] || [ "$RESULT" = "nothing_to_do" ]; then
    rm -f "$ALERT_FILE" 2>/dev/null || true
    echo "moved: $MOVED"
    echo "links rewritten: $LINKS_REWRITTEN"
    echo "rows archived: $ROWS_ARCHIVED"
    echo "unclassified: $UNCLASSIFIED"
    if [ "$RESULT" = "ok" ]; then
      echo "result: committed and pushed"
    else
      echo "result: no changes"
    fi
    exit 0
  fi
  mkdir -p "$REPORT_DIR" 2>/dev/null || true
  {
    echo "# Plans tidy alert"
    echo
    echo "ran_at: $RUN_AT"
    echo "result: $RESULT"
    echo "detail: $MSG"
    echo "moved: $MOVED"
    echo "links rewritten: $LINKS_REWRITTEN"
    echo "rows archived: $ROWS_ARCHIVED"
    echo "unclassified: $UNCLASSIFIED"
  } > "$ALERT_FILE" 2>/dev/null || true
  echo "ALERT: plans tidy $RESULT: $MSG"
  exit 1
}

# fail RESULT MESSAGE: record the failure and finish.
fail() {
  RESULT="$1"
  MSG="$2"
  finish
}

# ── settings sanity ─────────────────────────────────────────────────────
if ! [[ "$PLANS_TIDY_DAYS" =~ ^[0-9]+$ ]]; then
  fail preflight_failed "PLANS_TIDY_DAYS must be a whole number of days"
fi

# ── 1. preflight ────────────────────────────────────────────────────────
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  fail preflight_failed "project is not a git repository"
fi

if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=all -- docs/plans)" ]; then
  fail preflight_failed "docs/plans has uncommitted or untracked changes; tidy not run"
fi

PLANS_TIDY_BRANCH=${PLANS_TIDY_BRANCH:-$(git -C "$ROOT" branch --show-current)}

# ── 2. simulate in a throwaway worktree of HEAD ─────────────────────────
mkdir -p "$SCRATCH_DIR"
SIM_DIR="$SCRATCH_DIR/sim-$(date '+%Y%m%d%H%M%S')-$$"
if ! git -C "$ROOT" worktree add --detach "$SIM_DIR" HEAD >/dev/null 2>&1; then
  fail preflight_failed "could not create the simulation worktree under .datawatch/scratch/"
fi

if ! (cd "$SIM_DIR" && scripts/tidy-plans.sh --days "$PLANS_TIDY_DAYS" >/dev/null 2>&1); then
  fail preflight_failed "simulated tidy-plans.sh failed"
fi

# ── 3. simulated link check ─────────────────────────────────────────────
if ! (cd "$SIM_DIR" && scripts/check-plans-links.sh --quiet >/dev/null 2>&1); then
  fail link_check_failed "simulated tree has broken plan links after tidy; real tree not changed"
fi

remove_sim

# ── 4. real tidy and link check ─────────────────────────────────────────
if ! TIDY_OUT=$(cd "$ROOT" && scripts/tidy-plans.sh --days "$PLANS_TIDY_DAYS" 2>&1); then
  # tidy failed part-way: put docs/plans back the same way as a failed check
  git -C "$ROOT" reset -q -- docs/plans >/dev/null 2>&1 || true
  git -C "$ROOT" checkout -- docs/plans >/dev/null 2>&1 || true
  git -C "$ROOT" ls-files --others --exclude-standard -z -- docs/plans | xargs -0 -r rm -f --
  fail preflight_failed "tidy-plans.sh failed in the real tree; docs/plans restored"
fi

# ── counts from the tidy summary ────────────────────────────────────────
count_of() {
  printf '%s\n' "$TIDY_OUT" | sed -n "s/^ *$1:[[:space:]]*//p" | tail -n 1
}
plans_archived=$(count_of "plans archived");           plans_archived=${plans_archived:-0}
reports_archived=$(count_of "reports archived");       reports_archived=${reports_archived:-0}
parity_archived=$(count_of "parity issues archived");  parity_archived=${parity_archived:-0}
release_moved=$(count_of "release notes moved");       release_moved=${release_moved:-0}
LINKS_REWRITTEN=$(count_of "links rewritten");         LINKS_REWRITTEN=${LINKS_REWRITTEN:-0}
ROWS_ARCHIVED=$(count_of "hygiene closed rows archived"); ROWS_ARCHIVED=${ROWS_ARCHIVED:-0}
UNCLASSIFIED=$(count_of "unclassified (manual review needed)"); UNCLASSIFIED=${UNCLASSIFIED:-0}
MOVED=$(( plans_archived + reports_archived + parity_archived + release_moved ))

if ! (cd "$ROOT" && scripts/check-plans-links.sh --quiet >/dev/null 2>&1); then
  git -C "$ROOT" reset -q -- docs/plans >/dev/null 2>&1 || true
  git -C "$ROOT" checkout -- docs/plans >/dev/null 2>&1 || true
  # preflight guarantees nothing untracked existed under docs/plans, so
  # every untracked file left there was created by this tidy
  git -C "$ROOT" ls-files --others --exclude-standard -z -- docs/plans | xargs -0 -r rm -f --
  MOVED=0; LINKS_REWRITTEN=0; ROWS_ARCHIVED=0; UNCLASSIFIED=0
  fail link_check_failed "plan links broken after tidy in the real tree; docs/plans restored"
fi

# ── 5. commit and push when docs/plans changed ──────────────────────────
if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=all -- docs/plans)" ]; then
  git -C "$ROOT" add --all -- docs/plans

  bad=$(git -C "$ROOT" diff --cached --name-only -z | tr '\0' '\n' | grep -v '^docs/plans/' || true)
  if [ -n "$bad" ]; then
    git -C "$ROOT" reset -q -- docs/plans >/dev/null 2>&1 || true
    fail preflight_failed "staged paths outside docs/plans; commit refused"
  fi

  COUNTS="moved=$MOVED links_rewritten=$LINKS_REWRITTEN rows_archived=$ROWS_ARCHIVED unclassified=$UNCLASSIFIED days=$PLANS_TIDY_DAYS"
  if ! git -C "$ROOT" commit -q -m "docs: daily plans tidy" -m "$COUNTS" >/dev/null 2>&1; then
    fail preflight_failed "git commit of the plans tidy failed"
  fi
  COMMIT=$(git -C "$ROOT" rev-parse HEAD)

  if [ -z "$PLANS_TIDY_BRANCH" ]; then
    fail push_failed "no branch to push to (detached HEAD); local commit kept"
  fi
  if ! PUSH_ERR=$(git -C "$ROOT" push "$PLANS_TIDY_REMOTE" "HEAD:refs/heads/$PLANS_TIDY_BRANCH" 2>&1); then
    fail push_failed "push to $PLANS_TIDY_REMOTE $PLANS_TIDY_BRANCH failed; local commit kept. $(printf '%s' "$PUSH_ERR" | tail -n 3 | tr '\n' ' ')"
  fi
  RESULT="ok"
else
  RESULT="nothing_to_do"
fi

# ── 6. stamp, report and counts only on stdout ──────────────────────────
finish
