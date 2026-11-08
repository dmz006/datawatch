// Story: the Alerts tab's header row — the age text and the help link share
// one row at a 390 px phone viewport, with the help link to the right of the
// age text and no horizontal overflow. The one-second age ticker then advances
// the age text in place without re-creating the help link node (no meta-row
// repaint).
// tags: surface:pwa feature:alerts-header conflict:pwa-pwa
//
// The age text is the [data-list-age] span in the loader's meta row (the
// "last updated … ago" line above the list body); the help link is the round
// "?" anchor (aria-label="Help", class .alerts-help-link) the loader renders
// through its metaExtra option on the same row. On 390 px the two must stay
// on one row with the help link to the right, and the page must not overflow
// horizontally. The loader's age ticker (listLoader tick()) rewrites the span
// textContent each second without re-painting the row, so the help link keeps
// its DOM node.
//
// Runs against the SANDBOX daemon only (TEST_BASE / TEST_TLS / TEST_TOKEN set
// by run_pwa_story in scripts/test-stories/lib.sh — never start a daemon here).
// The /api/alerts GET is fulfilled with an empty list so the story does not
// depend on sandbox alert state; the meta row renders either way.

import { screenshot } from './lib.mjs';
import {
  coldPage, openPWA, check, fulfillEndpoint, noHorizontalOverflow, saveJson, runPerfStory,
} from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };

runPerfStory(async (browser) => {
  const { page: p } = await coldPage(browser, { viewport: PHONE });
  await openPWA(p);

  // Deterministic list content: the loader's meta row (age + help link)
  // renders on success regardless of how many alerts exist.
  const aCtl = await fulfillEndpoint(p, '/api/alerts', []);
  try {
    await p.evaluate(() => window.navigate('alerts'));
    await p.waitForFunction(() => !!document.querySelector('#alertsList [data-list-age]'), null, { timeout: 30000, polling: 100 });
    await p.waitForFunction(() => !!document.querySelector('#alertsList .list-state-meta-row a[aria-label="Help"]'), null, { timeout: 10000, polling: 100 });

    // Both elements exist, share one row (|Δtop| < age span height), and the
    // help link sits to the right of the age text.
    const geo = await p.evaluate(() => {
      const age = document.querySelector('#alertsList [data-list-age]');
      const help = document.querySelector('#alertsList .list-state-meta-row a[aria-label="Help"]');
      if (!age || !help) return null;
      const a = age.getBoundingClientRect();
      const h = help.getBoundingClientRect();
      return {
        ageTop: a.top, ageRight: a.right, ageHeight: a.height,
        helpTop: h.top, helpLeft: h.left,
      };
    });
    check(geo, 'age span and help link both rendered in the Alerts meta row');
    check(Math.abs(geo.helpTop - geo.ageTop) < geo.ageHeight,
      `age text and help link share one row: |Δtop|=${Math.abs(geo.helpTop - geo.ageTop).toFixed(1)} px, age span height=${geo.ageHeight.toFixed(1)} px`);
    check(geo.helpLeft >= geo.ageRight,
      `help link is to the right of the age text: help.left=${geo.helpLeft.toFixed(1)} < age.right=${geo.ageRight.toFixed(1)}`);
    await saveJson('geometry', { viewport: PHONE, ...geo });

    // 390 px viewport: no horizontal overflow.
    const o = await noHorizontalOverflow(p);
    check(o.ok, `no horizontal overflow at 390 px (scrollWidth=${o.scrollWidth}, innerWidth=${o.innerWidth})`);
    await screenshot(p, 'pwa-390-alerts-header');

    // Age ticker: wait for the age text to advance (the loader rewrites the
    // span every second). The help link must be the very same DOM node
    // afterwards — an in-place text update, not a meta-row repaint.
    const first = await p.evaluate(() => document.querySelector('#alertsList [data-list-age]').textContent);
    await p.evaluate(() => {
      window.__alertsHelp = document.querySelector('#alertsList .list-state-meta-row a[aria-label="Help"]');
    });
    await p.waitForFunction(
      (init) => { const s = document.querySelector('#alertsList [data-list-age]'); return !!s && s.textContent !== init; },
      first, { timeout: 15000, polling: 250 });
    const after = await p.evaluate(() => {
      const s = document.querySelector('#alertsList [data-list-age]');
      const help = document.querySelector('#alertsList .list-state-meta-row a[aria-label="Help"]');
      return { text: s && s.textContent, sameHelp: !!(help && help === window.__alertsHelp) };
    });
    check(!!after.text && after.text !== first,
      `age text advanced with the ticker (was ${JSON.stringify(first)}, now ${JSON.stringify(after.text)})`);
    check(after.sameHelp, 'help link is the same DOM node after the age ticker advanced (no meta-row repaint)');
    await screenshot(p, 'pwa-390-alerts-header-age-advanced');
  } finally {
    if (aCtl && aCtl.stop) await aCtl.stop();
  }
});
