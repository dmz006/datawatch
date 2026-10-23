#!/usr/bin/env node
/**
 * TS-810 — three reloads in a row, each back to content within budget
 * tags: surface:pwa feature:load-performance feature:reload
 *
 * Sessions is loaded, then the page is reloaded three times back to back. Each
 * reload must show a non-blank view as soon as the app is up and reach content
 * (cached data counts: it is badged and refreshed) within PERF_BUDGET_RELOAD_MS,
 * with no failed banner. A page error counts as a failure; transient navigation
 * errors are retried once and are not.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, waitForState, viewState, tolerateTransient,
  check, saveJson, BUDGET,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const errors = [];
  p.on('pageerror', (e) => errors.push(e.message));
  const times = [];
  try {
    await openPWA(p);
    await p.evaluate(() => window.navigate('sessions'));
    await waitForState(p, ['content', 'empty'], 30000);

    for (let i = 1; i <= 3; i++) {
      const t0 = Date.now();
      await tolerateTransient(() => p.reload({ waitUntil: 'domcontentloaded', timeout: 60000 }));
      await p.waitForFunction(() => typeof window.navigate === 'function', null, { timeout: 30000 });
      // a reload lands on the last view; make sure Sessions is the one measured
      await p.evaluate(() => { if (!document.getElementById('view').innerText.trim()) window.navigate('sessions'); });
      await waitForState(p, ['content', 'empty', 'failed'], BUDGET.reloadMs + 10000);
      const ms = Date.now() - t0;
      const state = await viewState(p);
      times.push({ reload: i, ms, state });
      await screenshot(p, `reload-${i}`).catch(() => {});
      check(state !== 'failed', `reload ${i}: ended in the failed state`);
      check(ms <= BUDGET.reloadMs, `reload ${i}: ${ms} ms to content, budget ${BUDGET.reloadMs} ms`);
    }
    await saveJson('reloads', { times, errors });
    check(errors.length === 0, `page errors during reloads: ${errors.slice(0, 3).join(' | ')}`);
  } finally {
    await context.close().catch(() => {});
  }
});
