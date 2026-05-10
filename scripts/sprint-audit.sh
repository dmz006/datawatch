#!/usr/bin/env bash
# Per-sprint rules audit (operator-flagged 2026-05-09 #251).
# Run before commit/tag of every alpha release.
#
# Checks:
#   1. Every t() key referenced in app.js exists in all 5 locale bundles
#      (en/fr/de/es/ja).
#   2. Every new /api/* path declared in internal/server/server.go has at
#      least one curl call in scripts/release-smoke.sh (best-effort grep).
#   3. node --check internal/server/web/app.js
#   4. go build ./...
#
# Exits 0 = all checks pass; 1 = at least one failure.
#
# Usage:
#   bash scripts/sprint-audit.sh             # full audit, prints findings
#   bash scripts/sprint-audit.sh --fix-locales  # also extracts missing keys to /tmp/missing-locales-*.txt for backfill

set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

ERRORS=0
WARNINGS=0
report_err()  { echo "  ✗ $1"; ERRORS=$((ERRORS + 1)); }
report_warn() { echo "  ⚠ $1"; WARNINGS=$((WARNINGS + 1)); }
report_ok()   { echo "  ✓ $1"; }

H() { echo ""; echo "== $* =="; }

# ── 1. Locale parity ─────────────────────────────────────────────────────
H "1. Locale parity (t() keys must exist in all 5 bundles)"
APP_JS="internal/server/web/app.js"
LOCALES_DIR="internal/server/web/locales"

# Extract every t('foo') / t("foo") key referenced in app.js.
KEYS=$(grep -oE "t\(['\"][a-z0-9_]+['\"]" "$APP_JS" | sed -E "s/t\(['\"]([a-z0-9_]+)['\"].*/\1/" | sort -u)
TOTAL_KEYS=$(echo "$KEYS" | wc -l | tr -d ' ')
echo "  $TOTAL_KEYS unique t() keys in app.js"

MISSING_FILE="/tmp/missing-locales-$(date +%s).txt"
: > "$MISSING_FILE"
TOTAL_MISSING=0
for locale in en fr de es ja; do
    f="$LOCALES_DIR/$locale.json"
    if [[ ! -f "$f" ]]; then
        report_err "locale file missing: $f"
        continue
    fi
    miss_count=0
    while IFS= read -r key; do
        [[ -z "$key" ]] && continue
        if ! grep -q "\"$key\"\s*:" "$f"; then
            echo "$locale  $key" >> "$MISSING_FILE"
            miss_count=$((miss_count + 1))
        fi
    done <<< "$KEYS"
    if [[ "$miss_count" -gt 0 ]]; then
        report_warn "$locale: missing $miss_count key(s) — see $MISSING_FILE"
        TOTAL_MISSING=$((TOTAL_MISSING + miss_count))
    else
        report_ok "$locale: all $TOTAL_KEYS keys present"
    fi
done
if [[ "$TOTAL_MISSING" -gt 0 ]]; then
    echo "  → To backfill: see $MISSING_FILE (locale + key on each line)"
fi

# ── 2. Smoke coverage for new endpoints ──────────────────────────────────
H "2. Smoke coverage (every apiMux.HandleFunc path probed by smoke)"
SMOKE="scripts/release-smoke.sh"
if [[ ! -f "$SMOKE" ]]; then
    report_err "smoke script not found: $SMOKE"
else
    PATHS=$(grep -oE 'apiMux.HandleFunc\("[^"]+"' internal/server/server.go | sed -E 's/apiMux.HandleFunc\("([^"]+)".*/\1/' | grep -E '^/api/' | sort -u)
    PATH_COUNT=$(echo "$PATHS" | wc -l | tr -d ' ')
    not_covered=0
    while IFS= read -r path; do
        [[ -z "$path" ]] && continue
        # Strip trailing slash for grep — smoke uses both forms.
        base=$(echo "$path" | sed 's|/$||')
        if ! grep -q "$base" "$SMOKE"; then
            report_warn "smoke does not reference $path"
            not_covered=$((not_covered + 1))
        fi
    done <<< "$PATHS"
    if [[ "$not_covered" -eq 0 ]]; then
        report_ok "all $PATH_COUNT REST paths referenced in smoke"
    else
        report_warn "$not_covered/$PATH_COUNT REST paths not in smoke — extend release-smoke.sh"
    fi
fi

# ── 3. JS syntax ─────────────────────────────────────────────────────────
H "3. JS syntax (node --check app.js)"
if command -v node >/dev/null 2>&1; then
    if node --check "$APP_JS" 2>/dev/null; then
        report_ok "node --check $APP_JS"
    else
        report_err "node --check failed for $APP_JS"
    fi
else
    report_warn "node not on PATH; skipping JS syntax check"
fi

# ── 4. Go build ──────────────────────────────────────────────────────────
H "4. Go build (./...)"
if go build ./... >/dev/null 2>&1; then
    report_ok "go build ./..."
else
    report_err "go build ./... failed"
fi

# ── 5. GitHub Actions runner status ──────────────────────────────────────
# operator 2026-05-09: each release must check that GH workflows on main
# are green. Failed runs become release-block items unless explicitly
# discussed. Skip when offline / not in a git repo / gh not installed.
H "5. GitHub Actions runner status (last 5 runs on main)"
if ! command -v gh >/dev/null 2>&1; then
    report_warn "gh CLI not on PATH; skipping runner check"
elif ! gh auth status >/dev/null 2>&1; then
    report_warn "gh not authenticated; skipping runner check"
else
    RUNS_JSON=$(gh run list --branch main --limit 5 --json status,conclusion,name,databaseId,createdAt 2>/dev/null || echo "[]")
    if [[ "$RUNS_JSON" == "[]" || -z "$RUNS_JSON" ]]; then
        report_warn "no recent GH workflow runs found on main"
    else
        FAILED=$(echo "$RUNS_JSON" | python3 -c "
import json, sys
runs = json.load(sys.stdin)
fails = [r for r in runs if r.get('conclusion') == 'failure']
if not fails:
    print('OK')
else:
    for r in fails:
        print('  X ' + str(r.get('name')) + ' (run ' + str(r.get('databaseId')) + ', ' + str(r.get('createdAt')) + ')')
" 2>/dev/null)
        if [[ "$FAILED" == "OK" ]]; then
            report_ok "no failed GH runs in last 5 on main"
        else
            report_err "GH runner failures detected — fix before tag, or document why not:"
            echo "$FAILED"
        fi
    fi
fi

# ── Summary ──────────────────────────────────────────────────────────────
H "Sprint audit summary"
echo "  Errors:   $ERRORS"
echo "  Warnings: $WARNINGS"
echo ""
if [[ "$ERRORS" -gt 0 ]]; then
    echo "✗ FAIL — fix errors above before commit/tag"
    exit 1
fi
if [[ "$WARNINGS" -gt 0 ]]; then
    echo "⚠ PASS WITH WARNINGS — backfill warnings before next sprint"
    exit 0
fi
echo "✓ PASS — all checks green"
exit 0
