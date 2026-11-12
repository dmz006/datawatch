// Story: the Alerts tab's meta row — the list's state text and the round help
// link share one row at a 390 px phone viewport, with the help link on the
// right and no horizontal overflow. The ticking "Last updated N s ago" age
// text and its one-second interval are gone: the meta row shows only the
// Cached / Loading / "Data may be out of date" badges, and there is no
// [data-list-age] span and no age ticker.
// tags: surface:pwa feature:alerts-header conflict:pwa-pwa
//
// The help link is the round "?" anchor (aria-label="Help", class
// .alerts-help-link) the loader renders through its metaExtra option on the
// meta row (`.list-state-meta-row`), to the right of the state-text span
// (`.list-state-meta-main`). On 390 px the link must stay on that one row and
// the page must not overflow horizontally.
//
// Runs against the SANDBOX daemon only (TEST_BASE / TEST_TLS / TEST_TOKEN set
// by run_pwa_story in scripts/test-stories/lib.sh — never start a daemon here).
// The /api/alerts GET is fulfilled with an empty list so the story does not
// depend on sandbox alert state; the meta row renders either way because the
// Alerts loader always passes its metaExtra help link.

import { screenshot } from './lib.mjs';
import {
  coldPage, openPWA, check, fulfillEndpoint, noHorizontalOverflow, saveJson, runPerfStory,
} from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };

runPerfStory(async (browser) => {
  const { page: p } = await coldPage(browser, { viewport: PHONE });
  await openPWA(p);

  // Deterministic list content: the loader's meta row (help link) renders on
  // success regardless of how many alerts exist.
  const aCtl = await fulfillEndpoint(p, '/api/alerts', []);
  try {
    await p.evaluate(() => window.navigate('alerts'));
    await p.waitForFunction(() => !!document.querySelector('#alertsList .list-state-meta-row a[aria-label="Help"]'), null, { timeout: 30000, polling: 100 });

    // The help link is on the meta row (the one row that holds the state
    // text), and that row is a single line.
    const geo = await p.evaluate(() => {
      const row = document.querySelector('#alertsList .list-state-meta-row');
      const main = document.querySelector('#alertsList .list-state-meta-main');
      const help = document.querySelector('#alertsList .list-state-meta-row a[aria-label="Help"]');
      if (!row || !help) return null;
      const r = row.getBoundingClientRect();
      const h = help.getBoundingClientRect();
      const m = main ? main.getBoundingClientRect() : null;
      return {
        rowTop: r.top, rowHeight: r.height, rowRight: r.right,
        mainRight: m ? m.right : null,
        helpTop: h.top, helpLeft: h.left,
      };
    });
    check(geo, 'help link rendered in the Alerts meta row');
    check(Math.abs(geo.helpTop - geo.rowTop) < geo.rowHeight,
      `help link sits on the meta row: |Δtop|=${Math.abs(geo.helpTop - geo.rowTop).toFixed(1)} px, row height=${geo.rowHeight.toFixed(1)} px`);
    check(geo.rowHeight < 60, `the meta row is a single line: height=${geo.rowHeight.toFixed(1)} px`);
    if (geo.mainRight != null) {
      check(geo.helpLeft >= geo.mainRight,
        `help link is to the right of the state text: help.left=${geo.helpLeft.toFixed(1)} < main.right=${geo.mainRight.toFixed(1)}`);
    }
    await saveJson('geometry', { viewport: PHONE, ...geo });

    // 390 px viewport: no horizontal overflow.
    const o = await noHorizontalOverflow(p);
    check(o.ok, `no horizontal overflow at 390 px (scrollWidth=${o.scrollWidth}, innerWidth=${o.innerWidth})`);
    await screenshot(p, 'pwa-390-alerts-header');

    // The ticking "last updated" age text and its one-second interval are
    // gone from the loader meta row.
    const noAge = await p.evaluate(() => {
      const age = document.querySelector('#alertsList [data-list-age]');
      return age == null;
    });
    check(noAge, 'no [data-list-age] ticking age span in the Alerts meta row');
    await screenshot(p, 'pwa-390-alerts-header');
  } finally {
    if (aCtl && aCtl.stop) await aCtl.stop();
  }
});
