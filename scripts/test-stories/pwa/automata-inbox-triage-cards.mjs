#!/usr/bin/env node
/**
 * Automata Inbox triage cards — the triage card sits below the open and held
 *                              questions cards, opens its detail panel and carries no repo CI line
 * tags: surface:pwa feature:triage feature:automata-inbox group:automata
 *
 * Runs against the runner's sandbox daemon (TEST_HTTP, never the production daemon).
 * The triage list is answered by a route fulfilment (GET /api/autonomous/triage) with
 * one failed record and one blocked record, so the card text is known. Flow:
 *   1. open the Automata view and the Automation sub-tab's Inbox;
 *   2. a triage card shows a headline that starts with a status word, the Automaton
 *      label and a waiting-time line;
 *   3. the open questions card and the held questions card come before the triage
 *      card in DOM order;
 *   4. clicking a card body (not a button) opens the detail panel with the full reason;
 *   5. no element with the repo CI status line id exists under the sub-tab bar.
 *      The repo CI watch state is recorded for the evidence; the assertion holds
 *      whether the watch is on or off because the status line is removed from the view.
 * Nothing on the daemon is started, stopped or changed.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, check, saveJson,
} from './perf-lib.mjs';

const REPO_CI_LINE_ID = 'repoCIWatchStatusLine';

const failedRecord = {
  prd_id: 'p1',
  prd_title: 'Refresh indexes',
  automation_id: 'a1',
  automation_name: 'Nightly pipeline',
  member_id: 'm1',
  status: 'failed',
  task_id: 't1',
  task_title: 'Build migration script',
  error: 'migration step 3 timed out after 600s; the retry budget for the scope category is spent and no further automatic retry will run',
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

// A status word at the start of the headline: Stopped (failed), Blocked, Ready to merge.
const STATUS_HEADLINE = /^(Stopped|Blocked|Ready to merge)\b/;

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    const triageCtl = await fulfillEndpoint(p, '/api/autonomous/triage', {
      records: [failedRecord, blockedRecord],
      alert_minutes: 30,
    });

    // The repo CI watch state is recorded only; the absence check does not depend on it.
    const repoCI = await p.evaluate(async () => {
      try {
        const r = await fetch('/api/autonomous/repo-ci-watch', { credentials: 'same-origin' });
        return { status: r.status, body: r.ok ? await r.json() : null };
      } catch (err) {
        return { status: 0, error: String(err) };
      }
    });

    // 1. Open the Automata view and the Inbox sub-tab.
    await p.evaluate(() => window.navigate('autonomous'));
    await p.waitForSelector('.automation-subtabs [data-subtab="inbox"]', { state: 'visible', timeout: 30000 });
    await p.click('.automation-subtabs [data-subtab="inbox"]');
    await p.waitForSelector('#automationTriageCard .triage-card', { state: 'attached', timeout: 30000 });

    const cards = p.locator('#automationTriageCard .triage-card');
    check((await cards.count()) === 2, `expected two triage cards, found ${await cards.count()}`);
    const failedCard = cards.nth(0);
    const failedText = await failedCard.innerText();

    // 2. Headline starts with a status word; Automaton label and waiting time are on the card.
    const headline = (await failedCard.locator('.triage-headline').innerText()).trim();
    check(STATUS_HEADLINE.test(headline), `failed card headline does not start with a status word: "${headline}"`);
    check(failedText.includes('Refresh indexes') || failedText.includes('Nightly pipeline'),
      `card is missing the Automaton label (got: ${failedText.slice(0, 300)})`);
    const wait = (await failedCard.locator('.triage-wait').innerText().catch(() => '')).trim();
    check(/^waiting\s+\S+/.test(wait), `card is missing a waiting-time line (got: "${wait}")`);

    const blockedHeadline = (await cards.nth(1).locator('.triage-headline').innerText()).trim();
    check(STATUS_HEADLINE.test(blockedHeadline), `blocked card headline does not start with a status word: "${blockedHeadline}"`);

    // 3. DOM order: open questions, then held questions, then the triage card.
    const order = await p.evaluate(() => {
      const q = document.getElementById('automationQuestionsCard');
      const h = document.getElementById('automationHeldQuestionsCard');
      const t = document.getElementById('automationTriageCard');
      if (!q || !h || !t) return { missing: [!q && 'automationQuestionsCard', !h && 'automationHeldQuestionsCard', !t && 'automationTriageCard'].filter(Boolean) };
      const before = (a, b) => (a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;
      return { missing: [], questionsBeforeTriage: before(q, t), heldBeforeTriage: before(h, t) };
    });
    check(!order.missing.length, `Inbox is missing card(s): ${order.missing.join(', ')}`);
    check(order.questionsBeforeTriage, 'the open questions card does not come before the triage card');
    check(order.heldBeforeTriage, 'the held questions card does not come before the triage card');

    // 4. Clicking the card body (the reason, not a button) opens the detail panel.
    await failedCard.locator('.triage-reason').click();
    await p.waitForSelector('#triageDetailDrawer .triage-detail-text', { state: 'visible', timeout: 10000 });
    const detail = await p.$eval('#triageDetailDrawer', (el) => el.innerText);
    check(detail.includes('migration step 3 timed out after 600s'),
      `detail panel missing the start of the full reason (got: ${detail.slice(0, 300)})`);
    check(detail.includes('no further automatic retry will run'),
      `detail panel shows a clamped reason, not the full text (got: ${detail.slice(0, 400)})`);
    await screenshot(p, 'inbox-triage-detail');
    await p.evaluate(() => window.closeTriageDetail && window.closeTriageDetail());

    // 5. No repo CI status line under the sub-tab bar.
    const repoLine = await p.evaluate((id) => {
      const bar = document.querySelector('.automation-subtabs');
      if (!bar) return { barFound: false, inDocument: !!document.getElementById(id), afterBar: false };
      let afterBar = false;
      for (let el = bar.nextElementSibling; el; el = el.nextElementSibling) {
        if (el.id === id || el.querySelector(`#${id}`)) { afterBar = true; break; }
      }
      return { barFound: true, inDocument: !!document.getElementById(id), afterBar };
    }, REPO_CI_LINE_ID);
    check(repoLine.barFound, 'no .automation-subtabs bar in the Automata view');
    check(!repoLine.afterBar, `element #${REPO_CI_LINE_ID} exists under the sub-tab bar`);
    check(!repoLine.inDocument, `element #${REPO_CI_LINE_ID} exists in the document`);

    await screenshot(p, 'inbox-triage-cards');
    check(!dialogs.length, `dialog(s) fired: ${dialogs.join('; ')}`);
    triageCtl.stop();
    await saveJson('automata-inbox-triage-cards', {
      headlines: [headline, blockedHeadline],
      wait,
      repoCI,
      repoLineAbsent: !repoLine.inDocument && !repoLine.afterBar,
    });
  } finally {
    await context.close().catch(() => {});
  }
});
