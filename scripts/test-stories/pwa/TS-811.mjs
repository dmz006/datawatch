#!/usr/bin/env node
/**
 * TS-811 — the first load of the Automata and Sessions tabs requests summary
 * lists only, within a byte budget
 * tags: surface:pwa feature:load-performance feature:payload
 *
 * Needs a daemon started on the seeded fixture (SeedPerfServer in
 * internal/server/perf_seed_test.go); seedLargeSet() skips (exit 2) when the
 * large set is not there. Every network response is recorded with its encoded
 * body size. Each tab is then opened cold (a fresh browser context) and:
 *   - no request URL contains full=1, and no list endpoint (sessions, Automata)
 *     is requested without view=summary;
 *   - the data bytes (everything under /api/) of the first load stay under
 *     PERF_BUDGET_TAB_BYTES (default 512 KB). The static app shell is recorded
 *     in the evidence but is not part of the data budget.
 */
import { screenshot, saveLog } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, openPWA, waitForState, viewState, recordNetwork,
  fullFormReason, sumBytes, check, saveJson, seedLargeSet, cleanupSeed, BUDGET,
} from './perf-lib.mjs';

// Time to let requests that start right after first content finish, so a late
// full-form fetch is still caught.
const SETTLE_MS = 2500;

const pathOf = (url) => { try { return new URL(url).pathname; } catch { return url; } };

await runPerfStory(async (page) => {
  const seed = await seedLargeSet({ log: (m) => console.error(m) });
  await saveLog('seed', JSON.stringify(seed));
  const browser = page.context().browser();
  const report = {};
  const failures = [];
  try {
    for (const tab of TABS.filter((t) => ['automata', 'sessions'].includes(t.name))) {
      const { context, page: p } = await coldPage(browser);
      try {
        const rec = recordNetwork(p);
        await openPWA(p, { timeout: 120000 });
        await p.evaluate((v) => window.navigate(v), tab.view);
        await waitForState(p, ['content', 'empty', 'failed'], 120000);
        const state = await viewState(p);
        await p.waitForTimeout(SETTLE_MS);
        await rec.flush();

        const all = rec.entries;
        const data = all.filter((e) => pathOf(e.url).startsWith('/api/'));
        const violations = all
          .map((e) => ({ url: e.url, reason: fullFormReason(e.url, e.method) }))
          .filter((v) => v.reason);
        const dataBytes = sumBytes(data);
        const biggest = [...data].sort((a, b) => b.bytes - a.bytes).slice(0, 5)
          .map((e) => ({ url: e.url, bytes: e.bytes }));
        report[tab.name] = {
          state, requests: all.length, dataRequests: data.length, dataBytes,
          totalBytes: sumBytes(all), budget: BUDGET.tabBytes, biggest, violations,
        };
        await saveJson(`first-load-${tab.name}`, { ...report[tab.name], requestsSeen: all.map((e) => ({ method: e.method, url: e.url, status: e.status, bytes: e.bytes })) });
        await screenshot(p, `first-load-${tab.name}`).catch(() => {});

        try {
          check(state !== 'failed', `${tab.name}: first load ended in the failed state`);
          check(violations.length === 0,
            `${tab.name}: full-form list request(s): ${violations.map((v) => `${v.url} (${v.reason})`).join(' | ')}`);
          check(dataBytes < BUDGET.tabBytes,
            `${tab.name}: ${dataBytes} data bytes on first load, budget ${BUDGET.tabBytes}`);
        } catch (e) {
          failures.push(e.message);
        }
      } finally {
        await context.close().catch(() => {});
      }
    }
  } finally {
    if (seed.created > 0) await cleanupSeed();
  }
  await saveJson('first-load-summary', { seed, budget: BUDGET, report });
  if (failures.length) throw new Error(failures.join('\n'));
});
