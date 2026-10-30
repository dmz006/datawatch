#!/usr/bin/env node
/**
 * Supervisor watch card story — the Automation view shows the supervisor
 *                               watch: mode, last poll, open findings, the
 *                               comparison with the supervisor script
 * tags: surface:pwa feature:supervisor-watch
 *
 * Route fulfilment (no daemon data): GET /api/autonomous/supervisor-watch
 * answers a shadow-mode read view with two open findings (one shadow-marked
 * would-alert, one whose text is hostile HTML), one resolved finding that must
 * not be listed, and a comparison that differs from the script. The card in
 * the Automation view's Chains sub-tab must show the mode in words, the age of
 * the last poll, the open-finding count, each open finding's kind and line,
 * the would-alert mark and the comparison line, with every value rendered as
 * text (the hostile record must never ship a live <img>/<script>), and the
 * page must not scroll sideways at phone width. Then the read view answers
 * with no findings and no comparison (the card says so), and finally the
 * route fails — the card must be hidden, never a stale state.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, failEndpoint, measureTab,
  assertMeasure, check, noHorizontalOverflow, saveJson,
} from './perf-lib.mjs';

const now = Date.now();
const iso = (msAgo) => new Date(now - msAgo).toISOString();

// Wire shape of internal/autonomous SupervisorWatchStatus
// (supervisor_watch_status.go).
const populated = {
  mode: 'shadow',
  last_poll_at: iso(90 * 1000),
  findings: [
    {
      kind: 'task_failed', subject: 'p1', text: 'Automaton p1: a task failed',
      first_seen_at: iso(40 * 60 * 1000), last_seen_at: iso(60 * 1000),
      would_alert: true, resolved: false,
    },
    {
      kind: 'ci_red', subject: '4242', text: '<img src=x onerror=alert(1)> CI run 4242 is red',
      first_seen_at: iso(5 * 60 * 1000), last_seen_at: iso(60 * 1000),
      would_alert: false, resolved: false,
    },
    {
      kind: 'worktree_silent', subject: 'p9', text: 'Automaton p9 went silent',
      first_seen_at: iso(3 * 3600 * 1000), last_seen_at: iso(2 * 3600 * 1000),
      resolved: true,
    },
  ],
  comparison: {
    last: { at: iso(60 * 1000), agreed: false, only_watch: ['task_failed/p1'], only_script: [] },
    agreed_days: 2,
    only_watch: ['task_failed/p1'],
    only_script: [],
  },
};
const quiet = {
  mode: 'shadow',
  last_poll_at: '0001-01-01T00:00:00Z',
  findings: [],
  comparison: { last: null, agreed_days: 0, only_watch: [], only_script: [] },
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    const ctl = await fulfillEndpoint(p, '/api/autonomous/supervisor-watch', populated);

    const r = await measureTab(p, 'autonomous', { subtab: 'automation', timeout: 30000, watchMs: 800 });
    assertMeasure('automation-supervisor-watch', r);

    await p.waitForFunction(() => {
      const c = document.getElementById('automationSupervisorWatchCard');
      return c && c.style.display !== 'none' && c.querySelector('.supervisor-watch-card');
    }, null, { timeout: 15000 });

    const slot = await p.$('#automationSupervisorWatchCard');
    check(slot, 'no #automationSupervisorWatchCard slot in the Automation sub-tab');
    const text = await slot.innerText();
    const html = await slot.innerHTML();

    for (const needle of [
      'Supervisor watch',
      'Mode: shadow (reports only, never acts)',
      'Last poll 1 m. ago',
      'Open findings: 2',
      'task_failed',
      'Automaton p1: a task failed',
      '(would alert)',
      'ci_red',
      'CI run 4242 is red',
      'Comparison: differs from the supervisor script. Only the watch: task_failed/p1. Only the script: nothing. (2 days of full agreement)',
    ]) check(text.includes(needle), `supervisor watch card missing "${needle}" (got: ${text.slice(0, 700)})`);

    check(!text.includes('went silent'), 'a resolved finding is listed as open');
    check((await p.$$eval('#automationSupervisorWatchCard .supervisor-watch-finding', els => els.length)) === 2,
      'expected exactly the two open findings');

    // Hostile values are inert text, not markup; nothing ran.
    check(!html.includes('<img src=x'), 'hostile finding text rendered as a live <img>');
    check(!dialogs.length, `dialog(s) fired by a hostile finding: ${dialogs.join('; ')}`);
    await screenshot(p, 'automation-supervisor-watch');

    // Phone width: the card (hostile and long text included) must never push
    // the page or the card wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `supervisor watch card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'automation-supervisor-watch-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // Quiet daemon: no findings, never polled, no comparison.
    ctl.stop();
    const quietCtl = await fulfillEndpoint(p, '/api/autonomous/supervisor-watch', quiet);
    await p.evaluate(() => window.supervisorWatchRefreshCard());
    await p.waitForFunction(() => {
      const c = document.getElementById('automationSupervisorWatchCard');
      return c && c.innerText.includes('No open findings');
    }, null, { timeout: 15000 });
    const quietText = await (await p.$('#automationSupervisorWatchCard')).innerText();
    for (const needle of ['Last poll never', 'Comparison: no supervisor script record compared yet']) {
      check(quietText.includes(needle), `quiet card missing "${needle}" (got: ${quietText.slice(0, 400)})`);
    }
    await screenshot(p, 'automation-supervisor-watch-quiet');

    // Endpoint unavailable: the card is hidden, never a stale state.
    quietCtl.stop();
    await failEndpoint(p, '/api/autonomous/supervisor-watch', { status: 503 });
    await p.evaluate(() => window.supervisorWatchRefreshCard());
    await p.waitForFunction(() => {
      const c = document.getElementById('automationSupervisorWatchCard');
      return c && c.style.display === 'none' && c.innerHTML === '';
    }, null, { timeout: 15000 });

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
    await saveJson('supervisor-watch', { states: r.states, indicatorMs: r.indicatorMs });
  } finally {
    await context.close().catch(() => {});
  }
});
