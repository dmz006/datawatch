#!/usr/bin/env node
/**
 * TS-803 — the large seeded set (3,000 sessions, 300 heavy Automata) still loads
 * within budget on slow-4G
 * tags: surface:pwa feature:load-performance
 *
 * Needs a daemon started on the seeded fixture (SeedPerfServer in
 * internal/server/perf_seed_test.go); seedLargeSet() verifies the counts through
 * the API, tops Automata up through the API, and skips (exit 2) when the
 * sessions are not there. Sessions, Automata and Automation are then opened cold
 * on slow-4G: indicator within PERF_INDICATOR_MS, never blank, first content
 * under PERF_BUDGET_SLOW4G_MS.
 */
import { screenshot, saveLog } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, throttle, openPWA, measureTab, assertMeasure,
  budgetFor, saveJson, seedLargeSet, cleanupSeed, BUDGET,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const seed = await seedLargeSet({ log: (m) => console.error(m) });
  await saveLog('seed', JSON.stringify(seed));
  const browser = page.context().browser();
  const { context, page: p } = await coldPage(browser);
  const report = {};
  const failures = [];
  try {
    await throttle(p, 'slow-4g');
    await openPWA(p, { timeout: 120000 });
    for (const tab of TABS.filter((t) => ['sessions', 'automata', 'automation'].includes(t.name))) {
      const r = await measureTab(p, tab.view, { timeout: 120000 });
      report[tab.name] = r;
      try {
        assertMeasure(`large/${tab.name}`, r, { budgetMs: budgetFor('slow-4g') });
      } catch (e) {
        failures.push(e.message);
        await screenshot(p, `large-${tab.name}-fail`).catch(() => {});
      }
    }
    await screenshot(p, 'large-set');
  } finally {
    await context.close().catch(() => {});
    if (seed.created > 0) await cleanupSeed();
  }
  await saveJson('large-set', { seed, budget: BUDGET, report });
  if (failures.length) throw new Error(failures.join('\n'));
});
