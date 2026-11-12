#!/usr/bin/env bash
# scripts/perf-budget.sh — web UI load performance budget.
#
# 1. Runs the Go budget test (TestPerfBudget*): first-page list bodies must stay
#    under 256 KB uncompressed, a repeat with If-None-Match must answer 304, and
#    the explicit full form must still return complete objects.
# 2. When TEST_HTTP and TEST_TOKEN are set and node is available, runs
#    scripts/perf-measure-tabs.mjs on a throttled slow-4G profile and fails if a
#    tab's time to first content exceeds PERF_TAB_BUDGET_MS (default 3000).
#
# Exits non-zero on any failure.
set -u

cd "$(dirname "$0")/.." || exit 1
BUDGET_MS="${PERF_TAB_BUDGET_MS:-3000}"
case "$BUDGET_MS" in ''|*[!0-9]*) echo "perf-budget: PERF_TAB_BUDGET_MS must be an integer, got '$BUDGET_MS'" >&2; exit 2 ;; esac

GO=$(command -v go) || { echo "perf-budget: go not found" >&2; exit 2; }
fail=0

echo "== Go budget test =="
"$GO" test ./internal/server/ -run 'TestPerfBudget' -count=1 || fail=1

if [ -n "${TEST_HTTP:-}" ] && [ -n "${TEST_TOKEN:-}" ]; then
  if NODE=$(command -v node); then
    echo "== Tab time to first content, slow 4G, limit ${BUDGET_MS} ms =="
    out=$(mktemp) || exit 2
    trap 'rm -f "$out"' EXIT
    if ! "$NODE" scripts/perf-measure-tabs.mjs --slow-4g-only >"$out"; then
      echo "perf-budget: perf-measure-tabs.mjs failed" >&2
      cat "$out" >&2
      fail=1
    else
      "$NODE" -e '
        const budget = Number(process.argv[2]);
        const doc = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
        let bad = 0;
        for (const p of doc.passes) {
          for (const [tab, r] of Object.entries(p.tabs || {})) {
            if (r.skipped) { console.log(`${tab}: skipped`); continue; }
            const over = r.timed_out || r.error || r.ms > budget;
            console.log(`${tab}: ${r.ms} ms${r.timed_out ? " (timed out)" : ""} ${over ? "FAIL" : "ok"}`);
            if (over) bad++;
          }
        }
        process.exit(bad ? 1 : 0);
      ' "$out" "$BUDGET_MS" || fail=1
    fi
  else
    echo "perf-budget: node not found, skipping the tab timing pass"
  fi
else
  echo "perf-budget: TEST_HTTP/TEST_TOKEN not set, skipping the tab timing pass"
fi

if [ "$fail" -ne 0 ]; then echo "perf-budget: FAILED" >&2; exit 1; fi
echo "perf-budget: OK"
