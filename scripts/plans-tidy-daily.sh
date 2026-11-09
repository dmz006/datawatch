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
#   6. Counts only are printed to stdout.  Alerts go to stderr and to a
#      file under .datawatch/reports/.
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
SIM_DIR=""

alert() {
  local msg="$1" file
  mkdir -p "$REPORT_DIR" 2>/dev/null || true
  file="$REPORT_DIR/plans-tidy-alert-$(date '+%Y%m%d-%H%M%S').txt"
  printf 'plans-tidy-daily alert at %s\n%s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$msg" > "$file" 2>/dev/null || true
  echo "ALERT: $msg" >&2
  echo "alert written to $file" >&2
}

remove_sim() {
  if [ -n "$SIM_DIR" ] && [ -e "$SIM_DIR" ]; then
    git -C "$ROOT" worktree remove --force "$SIM_DIR" >/dev/null 2>&1 || rm -rf "$SIM_DIR"
    git -C "$ROOT" worktree prune >/dev/null 2>&1 || true
  fi
  SIM_DIR=""
}
trap remove_sim EXIT

# ── settings sanity ─────────────────────────────────────────────────────
if ! [[ "$PLANS_TIDY_DAYS" =~ ^[0-9]+$ ]]; then
  alert "PLANS_TIDY_DAYS must be a whole number of days, got '$PLANS_TIDY_DAYS'"
  exit 1
fi

# ── 1. preflight ────────────────────────────────────────────────────────
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  alert "project is not a git repository: $ROOT"
  exit 1
fi

if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=all -- docs/plans)" ]; then
  alert "docs/plans has uncommitted or untracked changes; tidy not run"
  exit 1
fi

PLANS_TIDY_BRANCH=${PLANS_TIDY_BRANCH:-$(git -C "$ROOT" branch --show-current)}

# ── 2. simulate in a throwaway worktree of HEAD ─────────────────────────
mkdir -p "$SCRATCH_DIR"
SIM_DIR="$SCRATCH_DIR/sim-$(date '+%Y%m%d%H%M%S')-$$"
if ! git -C "$ROOT" worktree add --detach "$SIM_DIR" HEAD >/dev/null 2>&1; then
  alert "could not create the simulation worktree under .datawatch/scratch/"
  exit 1
fi

if ! (cd "$SIM_DIR" && scripts/tidy-plans.sh --days "$PLANS_TIDY_DAYS" >/dev/null 2>&1); then
  alert "simulated tidy-plans.sh failed"
  exit 1
fi

# ── 3. simulated link check ─────────────────────────────────────────────
if ! (cd "$SIM_DIR" && scripts/check-plans-links.sh --quiet >/dev/null 2>&1); then
  alert "simulated tree has broken plan links after tidy; real tree not changed"
  exit 1
fi

remove_sim

# ── 4. real tidy and link check ─────────────────────────────────────────
if ! TIDY_OUT=$(cd "$ROOT" && scripts/tidy-plans.sh --days "$PLANS_TIDY_DAYS" 2>&1); then
  # tidy failed part-way: put docs/plans back the same way as a failed check
  git -C "$ROOT" reset -q -- docs/plans >/dev/null 2>&1 || true
  git -C "$ROOT" checkout -- docs/plans >/dev/null 2>&1 || true
  git -C "$ROOT" ls-files --others --exclude-standard -z -- docs/plans | xargs -0 -r rm -f --
  alert "tidy-plans.sh failed in the real tree; docs/plans restored"
  exit 1
fi

if ! (cd "$ROOT" && scripts/check-plans-links.sh --quiet >/dev/null 2>&1); then
  git -C "$ROOT" reset -q -- docs/plans >/dev/null 2>&1 || true
  git -C "$ROOT" checkout -- docs/plans >/dev/null 2>&1 || true
  # preflight guarantees nothing untracked existed under docs/plans, so
  # every untracked file left there was created by this tidy
  git -C "$ROOT" ls-files --others --exclude-standard -z -- docs/plans | xargs -0 -r rm -f --
  alert "plan links broken after tidy in the real tree; docs/plans restored"
  exit 1
fi

# ── counts from the tidy summary ────────────────────────────────────────
count_of() {
  printf '%s\n' "$TIDY_OUT" | sed -n "s/^ *$1:[[:space:]]*//p" | tail -n 1
}
plans_archived=$(count_of "plans archived");           plans_archived=${plans_archived:-0}
reports_archived=$(count_of "reports archived");       reports_archived=${reports_archived:-0}
parity_archived=$(count_of "parity issues archived");  parity_archived=${parity_archived:-0}
release_moved=$(count_of "release notes moved");       release_moved=${release_moved:-0}
links_rewritten=$(count_of "links rewritten");         links_rewritten=${links_rewritten:-0}
rows_archived=$(count_of "hygiene closed rows archived"); rows_archived=${rows_archived:-0}
unclassified=$(count_of "unclassified \(manual review needed\)"); unclassified=${unclassified:-0}
moved=$(( plans_archived + reports_archived + parity_archived + release_moved ))

# ── 5. commit and push when docs/plans changed ──────────────────────────
result="no changes"
if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=all -- docs/plans)" ]; then
  git -C "$ROOT" add --all -- docs/plans

  bad=$(git -C "$ROOT" diff --cached --name-only -z | tr '\0' '\n' | grep -v '^docs/plans/' || true)
  if [ -n "$bad" ]; then
    git -C "$ROOT" reset -q -- docs/plans >/dev/null 2>&1 || true
    alert "staged paths outside docs/plans; commit refused"
    exit 1
  fi

  COUNTS="moved=$moved links_rewritten=$links_rewritten rows_archived=$rows_archived unclassified=$unclassified days=$PLANS_TIDY_DAYS"
  if ! git -C "$ROOT" commit -q -m "docs: daily plans tidy" -m "$COUNTS" >/dev/null 2>&1; then
    alert "git commit of the plans tidy failed"
    exit 1
  fi
  result="committed"

  if [ -z "$PLANS_TIDY_BRANCH" ]; then
    alert "no branch to push to (detached HEAD); local commit kept"
    exit 1
  fi
  if ! PUSH_ERR=$(git -C "$ROOT" push "$PLANS_TIDY_REMOTE" "HEAD:refs/heads/$PLANS_TIDY_BRANCH" 2>&1); then
    alert "push to $PLANS_TIDY_REMOTE $PLANS_TIDY_BRANCH failed; local commit kept. $(printf '%s' "$PUSH_ERR" | tail -n 3)"
    exit 1
  fi
  result="committed and pushed"
fi

# ── 6. counts only on stdout ────────────────────────────────────────────
echo "moved: $moved"
echo "links rewritten: $links_rewritten"
echo "rows archived: $rows_archived"
echo "unclassified: $unclassified"
echo "result: $result"
exit 0
