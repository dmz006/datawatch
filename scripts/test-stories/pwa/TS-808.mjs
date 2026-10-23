#!/usr/bin/env node
/**
 * TS-808 — offline then back online recovers
 * tags: surface:pwa feature:load-performance feature:offline
 *
 * With the PWA loaded the browser goes offline. Opening Sessions, Automata and
 * Automation while offline must show cached content or the failed state with
 * Retry — never a blank view. Back online, Retry (or the next poll) must bring
 * each tab back to content/empty with no failed banner.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, openPWA, waitForState, sampleStates, assertNeverBlank,
  viewState, check, saveJson,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const report = {};
  try {
    await openPWA(p);
    const tabs = TABS.filter((t) => ['sessions', 'automata', 'automation'].includes(t.name));

    // Warm each tab once online so the list cache exists for the offline pass.
    for (const tab of tabs) {
      await p.evaluate((v) => window.navigate(v), tab.view);
      await waitForState(p, ['content', 'empty'], 30000);
    }

    await context.setOffline(true);
    for (const tab of tabs) {
      await p.evaluate((v) => { document.getElementById('view').innerHTML = ''; window.navigate(v); }, tab.view);
      const samples = await sampleStates(p, 2500);
      assertNeverBlank(`offline/${tab.name}`, samples);
      const last = samples[samples.length - 1].s;
      check(['content', 'failed', 'loading'].includes(last), `offline/${tab.name}: unexpected state ${last}`);
      report[tab.name] = { offline: last };
      await screenshot(p, `${tab.name}-offline`);
    }

    await context.setOffline(false);
    for (const tab of tabs) {
      await p.evaluate((v) => window.navigate(v), tab.view);
      const retry = await p.$('#view .list-state-retry').catch(() => null);
      if (retry) await retry.click().catch(() => {});
      await waitForState(p, ['content', 'empty'], 30000)
        .catch(() => { throw new Error(`online/${tab.name}: did not recover (state ${'see screenshot'})`); });
      // the failed banner clears once a load succeeds
      await p.waitForFunction(() => !document.querySelector('#view .list-state-failed'), null, { timeout: 20000 })
        .catch(() => { throw new Error(`online/${tab.name}: failed banner still shown after reconnect`); });
      report[tab.name].online = await viewState(p);
      await screenshot(p, `${tab.name}-online`);
    }
    await saveJson('offline-online', report);
  } finally {
    await context.setOffline(false).catch(() => {});
    await context.close().catch(() => {});
  }
});
