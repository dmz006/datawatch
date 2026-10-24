#!/usr/bin/env node
/**
 * TS-813 — at 320 px wide the Automata tab bar fits: nothing clips, every sub-tab is tappable
 * tags: surface:pwa feature:responsive feature:automata-subtabs
 *
 * The Automata tab holds three sub-tabs (Automation, Automata, Templates) and the
 * action buttons of the selected one. At a 320 px viewport, for each sub-tab in
 * turn: the bar stays inside the viewport and does not scroll or clip its own
 * content, every tab and visible action button is at least 40 px tall and wide
 * enough to tap, and the page does not scroll horizontally
 * (document.documentElement.scrollWidth <= the viewport width).
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, openTab, noHorizontalOverflow, skipStory, check, saveJson,
} from './perf-lib.mjs';

const W = 320;
const MIN_TAP = 40;
const SUBTABS = ['automation', 'automata', 'templates'];

// Runs in the page: geometry of the sub-tab bar and everything it holds.
const BAR_GEOMETRY = () => {
  const bar = document.querySelector('.automata-tabs-bar');
  if (!bar) return null;
  const rect = (el) => {
    const b = el.getBoundingClientRect();
    return { left: b.left, right: b.right, width: b.width, height: b.height };
  };
  const shown = (el) => el.getClientRects().length > 0 && getComputedStyle(el).visibility !== 'hidden';
  return {
    bar: { ...rect(bar), scrollWidth: bar.scrollWidth, clientWidth: bar.clientWidth, overflowX: getComputedStyle(bar).overflowX },
    tabs: [...bar.querySelectorAll('.output-tab')].filter(shown)
      .map((el) => ({ id: el.dataset.tab || el.textContent.trim(), ...rect(el) })),
    actions: [...bar.querySelectorAll('button:not(.output-tab)')].filter(shown)
      .map((el) => ({ id: el.id || el.textContent.trim(), ...rect(el) })),
  };
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser(), { viewport: { width: W, height: 640 } });
  const report = {};
  const failures = [];
  try {
    await openPWA(p);
    await openTab(p, { view: 'autonomous', subtab: 'automation' });
    const hasBar = await p.waitForSelector('.automata-tabs-bar', { timeout: 15000 }).then(() => true).catch(() => false);
    if (!hasBar) await skipStory('the Automata tab is not available on this daemon (autonomous subsystem off)');

    for (const sub of SUBTABS) {
      const label = `${W}px/${sub}`;
      try {
        await openTab(p, { view: 'autonomous', subtab: sub });
        await p.waitForFunction((s) => {
          const b = document.querySelector(`.automata-tabs-bar .output-tab[data-tab="${s}"]`);
          return !!b && b.classList.contains('active');
        }, sub, { timeout: 10000 });
        const g = await p.evaluate(BAR_GEOMETRY);
        report[label] = g;
        check(g, `${label}: no sub-tab bar`);
        check(g.tabs.length >= 2, `${label}: ${g.tabs.length} sub-tab button(s) visible, expected the sub-tabs`);

        // The bar sits inside the viewport and neither scrolls nor clips its content.
        check(g.bar.left >= -1 && g.bar.right <= W + 1, `${label}: bar outside the viewport (${Math.round(g.bar.left)}..${Math.round(g.bar.right)})`);
        check(g.bar.scrollWidth <= g.bar.clientWidth + 1, `${label}: bar content wider than the bar (scrollWidth ${g.bar.scrollWidth} > ${g.bar.clientWidth})`);
        for (const el of [...g.tabs, ...g.actions]) {
          check(el.left >= g.bar.left - 1 && el.right <= g.bar.right + 1,
            `${label}: ${el.id} clipped by the bar (${Math.round(el.left)}..${Math.round(el.right)} in ${Math.round(g.bar.left)}..${Math.round(g.bar.right)})`);
          check(el.height >= MIN_TAP - 0.5, `${label}: ${el.id} is ${Math.round(el.height)} px tall, minimum ${MIN_TAP}`);
          check(el.width >= MIN_TAP - 0.5, `${label}: ${el.id} is ${Math.round(el.width)} px wide, minimum ${MIN_TAP}`);
        }

        // No sideways page scroll: documentElement.scrollWidth stays within the viewport.
        const o = await noHorizontalOverflow(p);
        check(o.scrollWidth <= o.innerWidth, `${label}: page scrolls sideways (scrollWidth ${o.scrollWidth} > ${o.innerWidth})`);
        await screenshot(p, `${W}-${sub}`).catch(() => {});
      } catch (e) {
        failures.push(e.message);
        await screenshot(p, `${W}-${sub}-fail`).catch(() => {});
      }
    }
    await saveJson('subtab-bar-320', report);
  } finally {
    await context.close().catch(() => {});
  }
  if (failures.length) throw new Error(failures.join('\n'));
});
