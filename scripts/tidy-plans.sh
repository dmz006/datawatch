#!/usr/bin/env bash
#
# tidy-plans.sh — keep docs/plans/ clean and link-consistent.
#
# Dated plans (YYYY-MM-DD-<slug>.md) stay in docs/plans/ as long as
# anything still references them:
#   * an unclosed registry row in docs/plans/README.md,
#   * any file under scripts/,
#   * a root rules file (AGENT.md, CLAUDE.md, CONTEXT.md, CONTRIBUTING.md,
#     DATAWATCH-CONTEXT.md, README.md, SECURITY.md),
#   * any file under docs/ (outside docs/plans/).
# Only plans past the age cutoff (--days, default 7) with no such
# references and a closed registry row move to
# docs/plans/historical-plans/.  On the move, every relative and absolute
# link pointing at the plan -- markdown link targets and path mentions
# alike, in the registry and elsewhere -- is rewritten to the new
# location.  All plan-side logic lives in scripts/plans_links.py
# (python3, standard library only).
#
# Release notes (RELEASE-NOTES-vMAJOR.MINOR.PATCH*.md) whose minor version
# does not match the current one (cmd/datawatch/main.go's `var Version`)
# are grouped into docs/plans/historical-releasenotes/<minor-version>/.
#
# README.md, HISTORY.md, and any other unclassified files are counted
# but NOT moved (manual review).
#
# Registry hygiene (docs/plans/README.md) runs on every tidy via
# scripts/plans_registry_hygiene.py (python3, standard library only): it
# removes duplicate sections (first occurrence wins, later copies folded
# in), merges all-closed tables into one, drops duplicate rows, trims
# verbose closed status cells, and -- with --status-file PATH -- corrects
# stale draft/not-started rows from an Automaton list export (JSON, read
# as-is; no daemon, no gh).
#
# Reports (docs/plans/reports/) and parity issues (docs/plans/parity-issues/)
# are archived monthly by scripts/plans_monthly_archive.py, which runs after
# the dated-plan step with the same --days and --dry-run values; its link
# and unclassified counts are folded into the summary totals.
#
# Output is counts only (kept, archived, links rewritten, ...) -- never a
# list of files.
#
# Usage:
#   scripts/tidy-plans.sh                do the moves + registry hygiene
#   scripts/tidy-plans.sh --dry-run      report counts, change nothing
#   scripts/tidy-plans.sh --check        report counts, exit 1 if anything would move
#   scripts/tidy-plans.sh --days N       plan age threshold in days (default 7)
#   scripts/tidy-plans.sh --no-hygiene   skip the registry README hygiene pass
#   scripts/tidy-plans.sh --status-file PATH
#                                        Automaton list export (JSON) used to
#                                        correct stale draft rows (off by default)
#
# Idempotent: safe to run repeatedly.

set -euo pipefail

ROOT=$(git rev-parse --show-toplevel)
PLANS_DIR="$ROOT/docs/plans"

DRY_RUN=0
CHECK=0
DAYS=7
RELEASES=1
HYGIENE=1
STATUS_FILE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --check)   CHECK=1; DRY_RUN=1; shift ;;
    --days) DAYS="$2"; shift 2 ;;
    --no-releases) RELEASES=0; shift ;;
    --no-hygiene) HYGIENE=0; shift ;;
    --status-file)
      if [ $# -lt 2 ]; then echo "--status-file needs a PATH"; exit 2; fi
      STATUS_FILE="$2"; shift 2 ;;
    -h|--help) awk 'NR>1 && /^# Idempotent/{exit} NR>1' "$0"; exit 0 ;;
    *) echo "unknown arg: $1"; exit 2 ;;
  esac
done

if [ ! -d "$PLANS_DIR" ]; then
  echo "no docs/plans/ — nothing to tidy"
  exit 0
fi

# ── registry hygiene: duplicate sections / closed tables / dedupe / trim ─
# Runs first so the tidy steps see a cleaned registry.  Counts only.
HYGIENE_OUT=""
if [ "$HYGIENE" -eq 1 ] && [ -f "$PLANS_DIR/README.md" ]; then
  HY_ARGS=()
  [ "$DRY_RUN" -eq 1 ] && HY_ARGS+=(--dry-run)
  [ -n "$STATUS_FILE" ] && HY_ARGS+=(--status-file "$STATUS_FILE")
  HYGIENE_OUT=$(python3 "$ROOT/scripts/plans_registry_hygiene.py" "$ROOT" ${HY_ARGS[@]+"${HY_ARGS[@]}"})
fi

# ── dated plans: link-aware, via scripts/plans_links.py ─────────────────
PY_ARGS=(--days "$DAYS")
[ "$DRY_RUN" -eq 1 ] && PY_ARGS+=(--dry-run)
PLAN_OUT=$(python3 "$ROOT/scripts/plans_links.py" "$ROOT" "${PY_ARGS[@]}")
plans_kept=$(printf '%s\n' "$PLAN_OUT"  | sed -n 's/^plans kept:[[:space:]]*//p')
plans_archived=$(printf '%s\n' "$PLAN_OUT" | sed -n 's/^plans archived:[[:space:]]*//p')
links_rewritten=$(printf '%s\n' "$PLAN_OUT" | sed -n 's/^links rewritten:[[:space:]]*//p')

# ── reports and parity issues: monthly archive helper ───────────────────
MA_OUT=$(python3 "$ROOT/scripts/plans_monthly_archive.py" "$ROOT" "${PY_ARGS[@]}")
ma_get() { printf '%s\n' "$MA_OUT" | sed -n "s/^$1:[[:space:]]*//p"; }
reports_archived=$(ma_get "reports archived")
parity_archived=$(ma_get "parity issues archived")
ma_links=$(ma_get "links rewritten")
ma_unclassified=$(ma_get "unclassified")
links_rewritten=$(( ${links_rewritten:-0} + ${ma_links:-0} ))

# ── release notes: group by minor version ────────────────────────────────
moved_rn=0
if [ "$RELEASES" -eq 1 ] && [ -f "$ROOT/cmd/datawatch/main.go" ]; then
  VERSION=$(grep -E '^var Version = "' "$ROOT/cmd/datawatch/main.go" | sed -E 's/.*"([^"]+)".*/\1/' || true)
  if [ -n "$VERSION" ]; then
    MINOR_PROG=$'{print $1"."$2}'
    CURRENT_MINOR=$(printf '%s\n' "$VERSION" | awk -F. "$MINOR_PROG")
    for f in "$PLANS_DIR"/RELEASE-NOTES-v*.md; do
      [ -f "$f" ] || continue
      base=$(basename "$f")
      case "$base" in
        RELEASE-NOTES-v[0-9]*.[0-9]*.*) ;;
        *) continue ;;
      esac
      numpart=${base#RELEASE-NOTES-v}
      file_minor=$(printf '%s\n' "$numpart" | awk -F. "$MINOR_PROG")
      if [ "$file_minor" != "$CURRENT_MINOR" ]; then
        dest="$PLANS_DIR/historical-releasenotes/$file_minor"
        if [ "$DRY_RUN" -eq 1 ]; then
          moved_rn=$((moved_rn + 1))
        else
          mkdir -p "$dest"
          if git -C "$ROOT" ls-files -- "$f" >/dev/null 2>&1; then
            git -C "$ROOT" mv -f "$f" "$dest/$base" >/dev/null
          else
            mv -f "$f" "$dest/$base"
          fi
          moved_rn=$((moved_rn + 1))
        fi
      fi
    done
  fi
fi

# ── unclassified top-level files: counted only, never moved ────────────
unclassified=0
for f in "$PLANS_DIR"/*.md; do
  [ -f "$f" ] || continue
  base=$(basename "$f")
  case "$base" in
    README.md|HISTORY.md|roadmap-archive-*.md) continue ;;
    [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-*.md) continue ;;
    RELEASE-NOTES-v[0-9]*.[0-9]*.*) continue ;;
  esac
  unclassified=$((unclassified + 1))
done
unclassified=$((unclassified + ${ma_unclassified:-0}))

echo
echo "── tidy-plans summary ──"
echo "current minor: v${CURRENT_MINOR:-?}.x  (plan-age cutoff: $DAYS days)"
echo "plans kept: $plans_kept"
echo "plans archived: $plans_archived"
echo "links rewritten: $links_rewritten"
echo "reports archived: ${reports_archived:-0}"
echo "parity issues archived: ${parity_archived:-0}"
echo "release notes moved: $moved_rn"
if [ -n "$HYGIENE_OUT" ]; then
  echo "registry hygiene:"
  printf '%s\n' "$HYGIENE_OUT" | sed 's/^/  /'
else
  echo "registry hygiene: skipped"
fi
if [ "$unclassified" -gt 0 ]; then
  echo "unclassified (manual review needed): $unclassified"
fi
if [ "$DRY_RUN" -eq 1 ]; then
  echo "(dry run — no files actually moved)"
fi

# --check: non-zero exit if anything would have moved.  Used by
# release-smoke.sh to fail-fast when the operator hasn't tidied between
# releases (per the operator's "build process" rule, 2026-05-05).
if [ "$CHECK" -eq 1 ] && [ $((plans_archived + moved_rn + ${reports_archived:-0} + ${parity_archived:-0})) -gt 0 ]; then
  echo
  echo "ERR: $((plans_archived + moved_rn + ${reports_archived:-0} + ${parity_archived:-0})) file(s) need to move out of docs/plans/." >&2
  echo "     Run \`scripts/tidy-plans.sh\` (no flags) to perform the moves," >&2
  echo "     then commit before re-running release-smoke." >&2
  exit 1
fi
