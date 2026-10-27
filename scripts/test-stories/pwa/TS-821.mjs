#!/usr/bin/env node
/**
 * TS-821 — a stuck Automaton's triage card renders on the Automation sub-tab
 *         and above every tab of its detail view
 * tags: surface:pwa feature:triage
 *
 * Route fulfilment (no daemon data): GET /api/autonomous/triage answers one
 * failed record, one blocked record and one record whose fields are hostile
 * HTML. The Automation sub-tab must show one card per record in its triage
 * slot — status chip, task, error, blocked reason, session end reason, retry
 * budget per category with the ceiling, first seen and the alerted state —
 * with every value rendered as text (the hostile record must never ship a
 * live <img>/<script>). The same failed Automaton, opened by id, must carry
 * the card on its detail view. Finally the list must settle on the empty
 * state when the server reports no Automata waiting for triage.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, measureTab,
  assertMeasure, check, viewState, saveJson,
} from './perf-lib.mjs';

// Wire shape of internal/autonomous TriageRecord (triage_record.go).
const failedRecord = {
  prd_id: 'p1',
  prd_title: 'Refresh indexes',
  automation_id: 'a1',
  automation_name: 'Nightly pipeline',
  member_id: 'm1',
  status: 'failed',
  task_id: 't1',
  task_title: 'Build migration script',
  error: 'migration step 3 timed out after 600s',
  session_end_reason: 'retry budget exhausted',
  first_seen_at: '2026-10-24T14:00:00Z',
  updated_at: '2026-10-24T15:29:00Z',
  alerted_at: '2026-10-24T15:30:00Z',
  retry_budget: {
    infra: { limit: 3, used: 1, left: 2 },
    content: { limit: 5, used: 0, left: 5 },
    scope: { limit: 2, used: 2, left: 0 },
    policy: { limit: 0, used: 0, left: 0 },
    ceiling_total: 30,
    ceiling_left: 12,
  },
};
const blockedRecord = {
  prd_id: 'p2',
  prd_title: 'Ingest partner feed',
  automation_id: 'a2',
  automation_name: 'Partner sync',
  status: 'blocked',
  blocked_reason: 'waiting for partner API keys',
  first_seen_at: '2026-10-24T09:00:00Z',
  updated_at: '2026-10-24T09:00:00Z',
  retry_budget: {
    infra: { limit: 0, used: 0, left: 0 },
    content: { limit: 0, used: 0, left: 0 },
    scope: { limit: 0, used: 0, left: 0 },
    policy: { limit: 0, used: 0, left: 0 },
    ceiling_total: 0,
    ceiling_left: 0,
  },
};
// Hostile field values: must come back as inert text, never as markup.
const hostileRecord = {
  prd_id: 'pX',
  prd_title: '<img src=x onerror=alert(1)>',
  automation_name: 'onmouseover=alert(1)',
  status: 'failed',
  error: '<script>alert(1)</script>',
  first_seen_at: 'not a date',
  retry_budget: {
    infra: { limit: 1, used: 0, left: 1 },
    content: { limit: 0, used: 0, left: 0 },
    scope: { limit: 0, used: 0, left: 0 },
    policy: { limit: 0, used: 0, left: 0 },
    ceiling_total: 1,
    ceiling_left: 1,
  },
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    const triageCtl = await fulfillEndpoint(p, '/api/autonomous/triage', { records: [failedRecord, blockedRecord, hostileRecord], alert_minutes: 30 });

    const r = await measureTab(p, 'autonomous', { subtab: 'automation', timeout: 30000, watchMs: 800 });
    assertMeasure('automation-triage', r);
    check(r.firstState === 'content', `first settled state was "${r.firstState}", expected content (${r.states.join(' > ')})`);

    await p.waitForFunction(() => {
      const c = document.getElementById('automationTriageCard');
      return c && c.querySelectorAll('.triage-card').length === 3;
    }, null, { timeout: 15000 });

    const slot = await p.$('#automationTriageCard');
    check(slot, 'no #automationTriageCard slot in the Automation sub-tab');
    const text = await slot.innerText();
    const html = await slot.innerHTML();

    // One card per record: failed, blocked.
    check((await p.$$eval('#automationTriageCard .triage-status-failed', els => els.length)) === 2,
      'expected two failed chips (failed + hostile record)');
    check((await p.$$eval('#automationTriageCard .triage-status-blocked', els => els.length)) === 1,
      'expected one blocked chip');

    // Failed record: every field the operator needs on one card.
    for (const needle of [
      'Refresh indexes',
      'Automation: Nightly pipeline',
      '(member m1)',
      'Task: Build migration script',
      'migration step 3 timed out after 600s',
      'Sessions ended: retry budget exhausted',
      'First seen',
      'Alerted',
      'Retry budget',
      'Infra', 'Content', 'Scope', 'Policy',
      '1 of 3 used · 2 left',
      '2 of 2 used · 0 left',
      'no auto-retry allowed',
      'Ceiling: 12 left of 30',
    ]) check(text.includes(needle), `triage card missing "${needle}" (got: ${text.slice(0, 600)})`);

    // Blocked record: the block reason and the never-alerted state.
    check(text.includes('Ingest partner feed'), 'blocked card missing the Automaton name');
    check(text.includes('Blocked: waiting for partner API keys'), 'blocked card missing the block reason');
    check(text.includes('Not yet alerted'), 'blocked card missing the "Not yet alerted" state');

    // Hostile values are inert text, not markup; nothing ran.
    check(!html.includes('<img src=x'), 'hostile prd_title rendered as a live <img>');
    check(!html.includes('<script>alert'), 'hostile error rendered as a live <script>');
    check(!dialogs.length, `dialog(s) fired by hostile records: ${dialogs.join('; ')}`);
    await screenshot(p, 'automation-triage-list');

    // Phone width: the cards (hostile and long text included) must never push
    // the page or a card wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await p.evaluate(() => ({
      inner: window.innerWidth,
      doc: document.documentElement.scrollWidth,
      body: document.body.scrollWidth,
      wide: Array.from(document.querySelectorAll('#automationTriageCard .triage-card'))
        .filter(c => c.scrollWidth > c.clientWidth + 1 || c.getBoundingClientRect().right > window.innerWidth + 1).length,
    }));
    check(fit.doc <= fit.inner + 1 && fit.body <= fit.inner + 1,
      `triage list scrolls sideways at phone width (doc ${fit.doc}, body ${fit.body}, viewport ${fit.inner})`);
    check(fit.wide === 0, `${fit.wide} triage card(s) overflow at phone width`);
    await screenshot(p, 'automation-triage-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // Empty state: the same slot settles on "nothing waiting" once the
    // server reports zero records.
    triageCtl.stop();
    const emptyCtl = await fulfillEndpoint(p, '/api/autonomous/triage', { records: [], alert_minutes: 30 });
    await p.waitForFunction(() => !!window._listLoaders && !!window._listLoaders.automation_triage,
      null, { timeout: 5000 });
    check((await p.evaluate(() => !!window._listLoaders.automation_triage)), 'automation_triage loader not registered');
    await p.evaluate(() => window._listLoaders.automation_triage.retry());
    await p.waitForFunction(() => {
      const c = document.getElementById('automationTriageCard');
      return c && c.querySelector('.triage-list-empty') && c.querySelectorAll('.triage-card').length === 0;
    }, null, { timeout: 15000 });
    const emptyText = await (await p.$('#automationTriageCard')).innerText();
    check(emptyText.includes('No Automata are waiting for triage.'),
      `empty state text unexpected: "${emptyText.slice(0, 200)}"`);
    await screenshot(p, 'automation-triage-empty');

    // Detail view: the same failed Automaton carries the above its tabs.
    emptyCtl.stop();
    await fulfillEndpoint(p, '/api/autonomous/prds/p1', {
      id: 'p1', title: 'Refresh indexes', status: 'failed',
      stories: [], decisions: [], triage: failedRecord,
    });
    await p.evaluate(() => window.renderPRDDetailView('p1'));
    await p.waitForFunction(() => {
      const c = document.getElementById('prdTriageCard');
      return c && c.querySelector('.triage-card');
    }, null, { timeout: 15000 });
    const dtext = await (await p.$('#prdTriageCard')).innerText();
    for (const needle of ['Refresh indexes', 'failed', 'Task: Build migration script',
      'migration step 3 timed out after 600s', 'Retry budget', 'Ceiling: 12 left of 30']) {
      check(dtext.includes(needle), `detail triage card missing "${needle}" (got: ${dtext.slice(0, 400)})`);
    }
    await screenshot(p, 'detail-triage-card');
    check((await viewState(p)) !== 'blank', 'detail view blank after the triage card rendered');

    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
    await saveJson('triage', { states: r.states, indicatorMs: r.indicatorMs });
  } finally {
    await context.close().catch(() => {});
  }
});
