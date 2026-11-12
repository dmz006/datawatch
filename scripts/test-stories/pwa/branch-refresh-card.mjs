#!/usr/bin/env node
/**
 * Branch refresh card story — the Automaton page's Overview tab shows the
 *                             branch refresh state and sets the override
 * tags: surface:pwa feature:branch-refresh
 *
 * Route fulfilment (no daemon data): GET /api/autonomous/prds/{id} answers an
 * Automaton whose last refresh hit a conflict and stopped refreshing, with a
 * hostile value in the base commit field. The Branch refresh card must show
 * the commits behind, the result in words (Conflict) and that refreshing is
 * stopped by a conflict, with every value rendered as text (no live
 * <img>/<script>). Choosing "Always refresh" in the override select must POST
 * {"enabled": true} to /api/autonomous/prds/{id}/branch-refresh, and choosing
 * the daemon default must POST {"enabled": null}. Then a clean, not-stopped
 * record shows the clean result and the active state. The page must not scroll
 * sideways at phone width.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, check, noHorizontalOverflow, saveJson,
} from './perf-lib.mjs';

const PRD_ID = 'branch-refresh-1';
const now = Date.now();
const iso = (msAgo) => new Date(now - msAgo).toISOString();

const base = {
  id: PRD_ID,
  title: 'Branch refresh card',
  spec: 'Seeded Automaton for the branch refresh card story.',
  status: 'running',
  stories: [{ id: 's1', title: 'First story', status: 'pending', tasks: [{ id: 't1', title: 'First task', status: 'pending', spec: 'Do it.' }] }],
  decisions: [],
  branch_refresh_override: null,
  branch_refresh_enabled: true,
};
const conflicted = {
  ...base,
  branch_refresh: {
    last_refresh_at: iso(5 * 60 * 1000), commits_behind: 7, last_result: 'conflict',
    last_main_commit: '<img src=x onerror=alert(1)>', stopped: true,
  },
};
const clean = {
  ...base,
  branch_refresh: { last_refresh_at: iso(60 * 1000), commits_behind: 5, last_result: 'clean', last_main_commit: 'abc123' },
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });
    await openPWA(p);

    const posts = [];
    await p.route('**/api/autonomous/prds/' + PRD_ID + '/branch-refresh', (route) => {
      posts.push(route.request().postDataJSON());
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(conflicted) });
    });
    const detail = await fulfillEndpoint(p, `/api/autonomous/prds/${PRD_ID}`, conflicted);
    await fulfillEndpoint(p, '/api/autonomous/prds', { prds: [conflicted] });

    await p.evaluate(() => window.navigate('autonomous'));
    await p.waitForTimeout(300);
    await p.evaluate((id) => window.renderPRDDetailView(id), PRD_ID);
    await p.waitForSelector('#automataDetailBody .branch-refresh-panel', { timeout: 15000 });

    const card = await p.$('#automataDetailBody .branch-refresh-panel');
    const text = await card.innerText();
    const html = await card.innerHTML();
    for (const needle of ['Branch refresh', 'Conflict', '7', 'Stopped by a conflict']) {
      check(text.includes(needle), `branch refresh card missing "${needle}" (got: ${text.slice(0, 500)})`);
    }
    check(!html.includes('<img src=x'), 'hostile value rendered as a live <img>');
    check(!dialogs.length, `dialog(s) fired by a hostile value: ${dialogs.join('; ')}`);
    await screenshot(p, 'branch-refresh-card-stopped');

    // Choosing the override posts true; choosing the daemon default posts null.
    await p.selectOption('#branchRefreshOverride', 'on');
    await p.waitForTimeout(400);
    await p.waitForSelector('#automataDetailBody .branch-refresh-panel', { timeout: 15000 });
    await p.selectOption('#branchRefreshOverride', '');
    await p.waitForTimeout(400);
    check(posts.length === 2, `expected two override posts, got ${posts.length}`);
    check(posts[0] && posts[0].enabled === true, `first post must carry enabled true, got ${JSON.stringify(posts[0])}`);
    check(posts[1] && posts[1].enabled === null, `second post must carry enabled null, got ${JSON.stringify(posts[1])}`);

    // A clean, not-stopped record shows the clean result and the active state.
    detail.stop();
    await fulfillEndpoint(p, `/api/autonomous/prds/${PRD_ID}`, clean);
    await p.evaluate((id) => window.renderPRDDetailView(id), PRD_ID);
    await p.waitForFunction(() => {
      const c = document.querySelector('#automataDetailBody .branch-refresh-panel');
      return c && c.innerText.includes('Clean') && c.innerText.includes('Active');
    }, null, { timeout: 15000 });
    const cleanText = await (await p.$('#automataDetailBody .branch-refresh-panel')).innerText();
    check(!cleanText.includes('Stopped by a conflict'), 'a record that is not stopped must not say it is stopped');

    // Phone width: the card must never push the page wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `branch refresh card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'branch-refresh-card-phone');

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
    await saveJson('branch-refresh-card', { posts });
  } finally {
    await context.close().catch(() => {});
  }
});
