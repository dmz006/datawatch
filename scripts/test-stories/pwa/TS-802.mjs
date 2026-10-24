#!/usr/bin/env node
/**
 * TS-802 — cold load of every main tab meets the time-to-first-content budget
 * tags: surface:pwa feature:load-performance
 *
 * Sessions, Automata, Automation, Observer and Settings are each opened cold
 * (fresh browser context: empty cache, empty localStorage) on slow-4G and on
 * fast. Per tab: a loading indicator within PERF_INDICATOR_MS of navigation,
 * never blank afterwards, and time to first content under the budget
 * (PERF_BUDGET_SLOW4G_MS / PERF_BUDGET_FAST_MS, see perf-lib.mjs).
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, throttle, openPWA, measureTab, assertMeasure,
  budgetFor, saveJson, tolerateTransient,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const report = {};
  const failures = [];

  for (const profile of ['slow-4g', 'fast']) {
    const { context, page: p } = await coldPage(browser);
    try {
      await throttle(p, profile);
      await tolerateTransient(() => openPWA(p));
      report[profile] = {};
      for (const tab of TABS) {
        const r = await measureTab(p, tab.view, { subtab: tab.subtab, timeout: 60000 });
        report[profile][tab.name] = r;
        try {
          assertMeasure(`${profile}/${tab.name}`, r, { budgetMs: budgetFor(profile) });
        } catch (e) {
          failures.push(e.message);
          await screenshot(p, `${profile}-${tab.name}-fail`).catch(() => {});
        }
      }
      await screenshot(p, `${profile}-last-tab`).catch(() => {});
    } finally {
      await context.close().catch(() => {});
    }
  }

  await saveJson('cold-load', report);
  if (failures.length) throw new Error(failures.join('\n'));
});
