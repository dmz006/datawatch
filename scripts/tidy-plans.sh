#!/usr/bin/env bash
#
# tidy-plans.sh — keep docs/plans/ from drowning in stale design docs.
#
# Operator-directed 2026-05-05 (datawatch session): "create a rule and
# script for the build process: keep the plans folder clean — create
# folders for historical-plans and historical-releasenotes and move any
# plan > 1 week old into historical-plans and any release line not in
# the current minor version branch into historical-releasenotes".
#
# Policy:
#   1. Files matching `YYYY-MM-DD-*.md` with date older than --days
#      (default 7) → docs/plans/historical-plans/
#   2. Files matching `RELEASE-NOTES-vMAJOR.MINOR.PATCH*.md` whose
#      MAJOR.MINOR doesn't match the current minor (read from
#      cmd/datawatch/main.go's `var Version`) → docs/plans/historical-releasenotes/
#   3. README.md and any other unclassified files are listed but NOT
#      moved (manual review). Operator rule: "review anything else in
#      that folder to see if it can be similarly archived to keep the
#      folder clean".
#
# Usage:
#   scripts/tidy-plans.sh             # do the moves (uses git mv)
#   scripts/tidy-plans.sh --dry-run   # show what would move, no changes
#   scripts/tidy-plans.sh --days 14   # change the plan-age threshold
#
# Idempotent: safe to run repeatedly. Files already inside the historical
# subdirectories are skipped.

set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
PLANS_DIR="$ROOT/docs/plans"
HIST_PLANS="$PLANS_DIR/historical-plans"
HIST_RN="$PLANS_DIR/historical-releasenotes"

DRY_RUN=0
CHECK=0
DAYS=7

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --check)   CHECK=1; DRY_RUN=1; shift ;;
    --days) DAYS="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1"; exit 2 ;;
  esac
done

if [ ! -d "$PLANS_DIR" ]; then
  echo "no docs/plans/ — nothing to tidy"
  exit 0
fi

# Read current minor (e.g. "6.11") from cmd/datawatch/main.go.
VERSION=$(grep -E '^var Version = "' "$ROOT/cmd/datawatch/main.go" | sed -E 's/.*"([^"]+)".*/\1/')
CURRENT_MINOR=$(echo "$VERSION" | awk -F. '{print $1"."$2}')

# Cutoff date for plans (epoch seconds).
if date -d "@0" >/dev/null 2>&1; then
  CUTOFF_EPOCH=$(date -d "$DAYS days ago" +%s)
else
  # macOS fallback
  CUTOFF_EPOCH=$(date -v-"${DAYS}"d +%s)
fi

mkdir -p "$HIST_PLANS" "$HIST_RN"

moved_plans=0
moved_rn=0
unclassified=()

for f in "$PLANS_DIR"/*.md; do
  [ -f "$f" ] || continue
  base=$(basename "$f")

  # README + anything in the historical/ subdirs is never touched here.
  if [ "$base" = "README.md" ]; then
    continue
  fi

  # Dated plan?
  if [[ "$base" =~ ^([0-9]{4})-([0-9]{2})-([0-9]{2})- ]]; then
    plan_date="${BASH_REMATCH[1]}-${BASH_REMATCH[2]}-${BASH_REMATCH[3]}"
    if date -d "@0" >/dev/null 2>&1; then
      plan_epoch=$(date -d "$plan_date" +%s 2>/dev/null || echo 0)
    else
      plan_epoch=$(date -j -f "%Y-%m-%d" "$plan_date" +%s 2>/dev/null || echo 0)
    fi
    if [ "$plan_epoch" -gt 0 ] && [ "$plan_epoch" -lt "$CUTOFF_EPOCH" ]; then
      if [ $DRY_RUN -eq 1 ]; then
        echo "[plan] would move: $base → historical-plans/"
      else
        git mv -f "$f" "$HIST_PLANS/$base" >/dev/null
        echo "[plan] moved: $base → historical-plans/"
      fi
      moved_plans=$((moved_plans + 1))
    fi
    continue
  fi

  # Release-notes file?
  if [[ "$base" =~ ^RELEASE-NOTES-v([0-9]+)\.([0-9]+)\. ]]; then
    file_minor="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}"
    if [ "$file_minor" != "$CURRENT_MINOR" ]; then
      if [ $DRY_RUN -eq 1 ]; then
        echo "[rn] would move: $base → historical-releasenotes/  (file v$file_minor.x ≠ current v$CURRENT_MINOR.x)"
      else
        git mv -f "$f" "$HIST_RN/$base" >/dev/null
        echo "[rn] moved: $base → historical-releasenotes/"
      fi
      moved_rn=$((moved_rn + 1))
    fi
    continue
  fi

  unclassified+=("$base")
done

echo
echo "── tidy-plans summary ──"
echo "current minor: v$CURRENT_MINOR.x  (plan-age cutoff: $DAYS days)"
echo "plans moved:   $moved_plans"
echo "release notes moved: $moved_rn"
if [ ${#unclassified[@]} -gt 0 ]; then
  echo "unclassified (please review — manual decision needed):"
  for u in "${unclassified[@]}"; do
    echo "  $u"
  done
fi
if [ $DRY_RUN -eq 1 ]; then
  echo "(dry run — no files actually moved)"
fi

# --check mode: non-zero exit if anything would have moved. Used by
# release-smoke.sh to fail-fast when the operator hasn't tidied between
# releases (per the operator's "build process" rule, 2026-05-05).
if [ $CHECK -eq 1 ] && [ $((moved_plans + moved_rn)) -gt 0 ]; then
  echo
  echo "ERR: $((moved_plans + moved_rn)) file(s) need to move out of docs/plans/." >&2
  echo "     Run \`scripts/tidy-plans.sh\` (no flags) to perform the moves," >&2
  echo "     then commit before re-running release-smoke." >&2
  exit 1
fi
