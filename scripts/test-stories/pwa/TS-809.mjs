#!/usr/bin/env node
/**
 * TS-809 — phone and tablet widths: no horizontal overflow, indicators visible
 * tags: surface:pwa feature:load-performance feature:responsive
 *
 * Widths 360, 390, 412 (phones) and 768 (tablet). For every main tab: the list
 * endpoint is held for ~700 ms so the loading indicator is observable; it must
 * appear within PERF_INDICATOR_MS, sit inside the viewport, and the page must
 * not scroll horizontally while loading and after content arrives.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, openPWA, delayEndpoint, measureTab, assertMeasure,
  noHorizontalOverflow, check, saveJson,
} from './perf-lib.mjs';

const WIDTHS = [
  { w: 360, h: 740 }, { w: 390, h: 844 }, { w: 412, h: 915 }, { w: 768, h: 1024 },
];

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const report = {};
  const failures = [];
  for (const { w, h } of WIDTHS) {
    const { context, page: p } = await coldPage(browser, { viewport: { width: w, height: h } });
    try {
      await openPWA(p);
      for (const tab of TABS) {
        const label = `${w}px/${tab.name}`;
        const hold = await delayEndpoint(p, tab.endpoint, 700);
        // clear the loader cache so the indicator is the skeleton, not cached data
        await p.evaluate(() => { try { Object.keys(localStorage).filter((k) => k.startsWith('cs_list_')).forEach((k) => localStorage.removeItem(k)); } catch {} });
        let overflowWhileLoading = null;
        const poll = (async () => {
          for (let i = 0; i < 12; i++) {
            const o = await noHorizontalOverflow(p).catch(() => null);
            if (o && !o.ok && !overflowWhileLoading) overflowWhileLoading = o;
            await p.waitForTimeout(60);
          }
        })();
        const r = await measureTab(p, tab.view, { subtab: tab.subtab, timeout: 30000 });
        await poll;
        try {
          assertMeasure(label, r);
          check(!overflowWhileLoading, `${label}: horizontal overflow while loading (scrollWidth ${overflowWhileLoading && overflowWhileLoading.scrollWidth} > ${w})`);
          const o = await noHorizontalOverflow(p);
          check(o.ok, `${label}: horizontal overflow after load (scrollWidth ${o.scrollWidth} > ${o.innerWidth})`);
          const ind = await p.evaluate(() => {
            const el = document.querySelector('#view .list-state-skeleton, #view .list-state-body, #view .list-state-failed, #view .list-state-empty');
            if (!el) return null;
            const b = el.getBoundingClientRect();
            return { left: b.left, right: b.right, width: b.width };
          });
          if (ind) check(ind.left >= -1 && ind.right <= w + 1, `${label}: indicator/content outside the viewport (${Math.round(ind.left)}..${Math.round(ind.right)})`);
        } catch (e) {
          failures.push(e.message);
          await screenshot(p, `${w}-${tab.name}-fail`).catch(() => {});
        }
        await hold.stop();
        report[label] = r;
      }
      await screenshot(p, `${w}-final`).catch(() => {});
    } finally {
      await context.close().catch(() => {});
    }
  }
  await saveJson('widths', report);
  if (failures.length) throw new Error(failures.join('\n'));
});
