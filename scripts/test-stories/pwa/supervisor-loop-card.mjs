#!/usr/bin/env node
/**
 * Supervisor loop card story — the Automation view shows the supervisor loop:
 *                              its jobs with last outcome and the recent runs
 * tags: surface:pwa feature:supervisor-loop
 *
 * Route fulfilment (no daemon data): GET /api/supervisor-loop/jobs answers
 * the loop's jobs (one mechanical step, two model jobs, one whose name is
 * hostile HTML) and GET /api/supervisor-loop/runs answers two runs. The card
 * in the Automation view must show the title, the Jobs and Recent runs
 * headings, one row per job with the model job's style, backend and model and
 * the last outcome in words, one row per run, with every value rendered as
 * text (the hostile record must never ship a live <img>/<script>), and the
 * page must not scroll sideways at phone width. Then the jobs route answers
 * with the loop switched off (the card says so), and finally the route fails —
 * the card must be hidden, never a stale state.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, failEndpoint, measureTab,
  assertMeasure, check, noHorizontalOverflow, saveJson,
} from './perf-lib.mjs';

const now = Date.now();
const iso = (msAgo) => new Date(now - msAgo).toISOString();

// Wire shape of internal/server supervisor_loop.go (jobs and runs bodies).
const jobsOn = {
  enabled: true,
  jobs: [
    { name: 'cleanup_sweep', kind: 'mechanical', enabled: true, last_outcome: 'ok', last_run: iso(60 * 1000) },
    { name: 'draft_spec', kind: 'model', style: 'oneshot', backend: 'ollama', model: 'qwen-test', enabled: true, last_outcome: 'escalated', last_run: iso(5 * 60 * 1000) },
    { name: 'pick_backend', kind: 'model', style: 'session', backend: 'claude-code', model: 'sonnet-test', enabled: true },
    { name: '<img src=x onerror=alert(1)>', kind: 'mechanical', enabled: true, last_outcome: 'error' },
  ],
};
const runs = {
  runs: [
    { job: 'draft_spec', style: 'oneshot', backend: 'ollama', model: 'qwen-test', outcome: 'escalated', result: 'asked the operator', validator_failures: ['no stories'], retries: 2, escalations: 1, duration_ms: 1800, started: iso(5 * 60 * 1000) },
    { job: 'cleanup_sweep', outcome: 'ok', result: 'swept 0', validator_failures: [], retries: 0, escalations: 0, duration_ms: 12, started: iso(60 * 1000) },
  ],
};
const jobsOff = { enabled: false, jobs: jobsOn.jobs.slice(0, 2) };

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    const jobsCtl = await fulfillEndpoint(p, '/api/supervisor-loop/jobs', jobsOn);
    const runsCtl = await fulfillEndpoint(p, '/api/supervisor-loop/runs', runs);

    const r = await measureTab(p, 'autonomous', { subtab: 'automation', timeout: 30000, watchMs: 800 });
    assertMeasure('automation-supervisor-loop', r);

    // The card lives on the Setup sub-tab of the Automation view.
    await p.evaluate(() => window.switchAutomationSubtab('setup'));

    await p.waitForFunction(() => {
      const c = document.getElementById('automationSupervisorLoopCard');
      return c && c.style.display !== 'none' && c.querySelector('.supervisor-loop-card');
    }, null, { timeout: 15000 });

    const slot = await p.$('#automationSupervisorLoopCard');
    check(slot, 'no #automationSupervisorLoopCard slot in the Setup sub-tab');
    const text = await slot.innerText();
    const html = await slot.innerHTML();

    for (const needle of ['Supervisor loop', 'Jobs', 'Recent runs', 'qwen-test', 'ollama', 'oneshot', 'sonnet-test', 'escalated']) {
      check(text.includes(needle), `supervisor loop card missing "${needle}" (got: ${text.slice(0, 700)})`);
    }
    check((await p.$$eval('#automationSupervisorLoopCard .supervisor-loop-job', els => els.length)) === 4,
      'expected one row per job (4)');
    check((await p.$$eval('#automationSupervisorLoopCard .supervisor-loop-run', els => els.length)) === 2,
      'expected one row per run (2)');

    // Hostile values are inert text, not markup; nothing ran.
    check(!html.includes('<img src=x'), 'hostile job name rendered as a live <img>');
    check(!dialogs.length, `dialog(s) fired by a hostile job name: ${dialogs.join('; ')}`);
    await screenshot(p, 'automation-supervisor-loop');

    // Phone width: the card must never push the page wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `supervisor loop card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'automation-supervisor-loop-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // Loop switched off: the card says so.
    jobsCtl.stop();
    const offCtl = await fulfillEndpoint(p, '/api/supervisor-loop/jobs', jobsOff);
    await p.evaluate(() => window.supervisorLoopRefreshCard());
    await p.waitForFunction(() => {
      const c = document.getElementById('automationSupervisorLoopCard');
      return c && c.innerText.includes('switched off');
    }, null, { timeout: 15000 });
    await screenshot(p, 'automation-supervisor-loop-off');

    // Endpoint unavailable: the card is hidden, never a stale state.
    offCtl.stop();
    runsCtl.stop();
    await failEndpoint(p, '/api/supervisor-loop/jobs', { status: 503 });
    await p.evaluate(() => window.supervisorLoopRefreshCard());
    await p.waitForFunction(() => {
      const c = document.getElementById('automationSupervisorLoopCard');
      return c && c.style.display === 'none' && c.innerHTML === '';
    }, null, { timeout: 15000 });

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
    await saveJson('supervisor-loop', { states: r.states, indicatorMs: r.indicatorMs });
  } finally {
    await context.close().catch(() => {});
  }
});
