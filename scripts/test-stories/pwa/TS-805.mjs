#!/usr/bin/env node
/**
 * TS-805 — a failing endpoint shows the failed state with Retry, and Retry recovers
 * tags: surface:pwa feature:load-performance feature:failed-state
 *
 * For Sessions, Automata and Automation: the tab's list endpoint answers 500.
 * The tab must show the failed state (.list-state-failed) with a Retry button
 * and never go blank. After the fault is removed, clicking Retry must replace
 * the failed state with content (or the empty state).
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, openPWA, failEndpoint, waitForState, viewState,
  check, saveJson,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const report = {};
  try {
    await openPWA(p);
    for (const tab of TABS.filter((t) => ['sessions', 'automata', 'automation'].includes(t.name))) {
      // Start from an empty loader cache so the failure is a first-load failure.
      await p.evaluate(() => { try { Object.keys(localStorage).filter((k) => k.startsWith('cs_list_')).forEach((k) => localStorage.removeItem(k)); } catch {} });
      const fault = await failEndpoint(p, tab.endpoint, { status: 500 });
      await p.evaluate((v) => { document.getElementById('view').innerHTML = ''; window.navigate(v); }, tab.view);
      await waitForState(p, ['failed'], 20000).catch(() => {
        throw new Error(`${tab.name}: no failed state after ${tab.endpoint} returned 500 (state: ${'see screenshot'})`);
      });
      const retry = await p.$('#view .list-state-retry').catch(() => null);
      check(retry, `${tab.name}: failed state has no Retry button`);
      check(await retry.isVisible(), `${tab.name}: Retry button not visible`);
      check((await viewState(p)) !== 'blank', `${tab.name}: blank view while failed`);
      await screenshot(p, `${tab.name}-failed`);
      report[tab.name] = { hits: fault.hits };

      await fault.stop();
      await retry.click();
      await waitForState(p, ['content', 'empty'], 30000).catch(() => {
        throw new Error(`${tab.name}: Retry did not recover to content`);
      });
      const still = await p.$('#view .list-state-failed').catch(() => null);
      check(!still, `${tab.name}: failed banner still present after a successful Retry`);
      await screenshot(p, `${tab.name}-recovered`);
    }
    await saveJson('failed-retry', report);
  } finally {
    await context.close().catch(() => {});
  }
});
