#!/usr/bin/env node
/**
 * Question decision link story — an Automaton's decision log shows the answer
 *                                 id of an answered question as a link that
 *                                 opens the verbatim quote
 * tags: surface:pwa feature:questions
 *
 * Route fulfilment (no daemon data): GET /api/autonomous/prds/p1 answers an
 * Automaton whose decision log holds one operator_decision entry and one
 * operator_answer entry for the same question, and GET
 * /api/questions/q-1/answers answers that question's answer log with one
 * answer whose quote is hostile HTML. The decision log must carry exactly one
 * answer link (on the operator_answer entry only); clicking it must open a
 * panel with the quote exactly as stored, the chosen option, the actor and the
 * time, with the quote rendered as text (no live <img>, no dialog); a second
 * click must close the panel. A question whose answer log is gone must show
 * the not-on-record line, and the page must not scroll sideways at phone
 * width.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, fulfillEndpoint, failEndpoint,
  check, viewState, noHorizontalOverflow,
} from './perf-lib.mjs';

const QUOTE = '<img src=x onerror=alert(1)> Use postgres,\nnot sqlite.';

// Wire shape of internal/autonomous Decision (models.go) and Answer
// (questions.go).
const prd = {
  id: 'p1',
  title: 'Refresh indexes',
  status: 'running',
  stories: [],
  decisions: [
    {
      kind: 'operator_decision', actor: 'planner', at: '2026-10-29T10:00:00Z',
      note: 'Which database should the importer target?',
      question_id: 'q-1', decision_state: 'open',
    },
    {
      kind: 'operator_answer', actor: 'dmz', at: '2026-10-29T10:05:00Z',
      note: 'answered',
      question_id: 'q-1', answer_id: 'a-1', decision_state: 'answered',
    },
  ],
};
const answerLog = {
  answers: [{
    id: 'a-1', question_id: 'q-1', actor: 'dmz', surface: 'pwa',
    chosen_option_id: 'postgres', quote: QUOTE,
    created_at: '2026-10-29T10:05:00Z', superseded: false,
  }],
  current_answer_id: 'a-1',
};

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const dialogs = [];
  try {
    p.on('dialog', async (d) => { dialogs.push(d.message()); await d.dismiss().catch(() => {}); });

    await openPWA(p);
    await fulfillEndpoint(p, '/api/autonomous/prds/p1', prd);
    const answersCtl = await fulfillEndpoint(p, '/api/questions/q-1/answers', answerLog);

    await p.evaluate(() => window.renderPRDDetailView('p1'));
    await p.waitForFunction(() => !!document.querySelector('#automataDetailBody .prd-detail-tab-body'),
      null, { timeout: 15000 });
    await p.evaluate(() => window.switchAutomataDetailTab('decisions'));
    await p.waitForFunction(() => document.querySelectorAll('.prd-decision-card').length === 2,
      null, { timeout: 15000 });

    // One link, on the answer entry only; the panel starts closed.
    const links = await p.$$('.decision-answer-link');
    check(links.length === 1, `expected one answer link, found ${links.length}`);
    check((await links[0].innerText()).trim() === 'Answer a-1', 'link text is not "Answer a-1"');
    check(await links[0].evaluate(el => !!el.closest('.prd-decision-card').querySelector('.prd-decision-kind')
      && el.closest('.prd-decision-card').querySelector('.prd-decision-kind').textContent === 'operator_answer'),
    'the link is not on the operator_answer entry');
    check(await p.$eval('.decision-answer-panel', el => el.hidden), 'panel is open before any click');
    check(answersCtl.hits === 0, 'answers were fetched before the link was clicked');

    // Open: verbatim quote as text, chosen option, actor, time.
    await links[0].click();
    await p.waitForSelector('.decision-answer-quote', { timeout: 15000 });
    check(answersCtl.hits === 1, `expected one answers fetch, saw ${answersCtl.hits}`);
    const panel = await p.$('.decision-answer-panel');
    const text = await panel.innerText();
    const html = await panel.innerHTML();
    check(await p.$eval('.decision-answer-quote', el => el.textContent) === QUOTE,
      'the quote is not shown exactly as stored');
    for (const needle of ['Chosen option:', 'postgres', 'Answered by:', 'dmz', 'Answered at:']) {
      check(text.includes(needle), `answer panel missing "${needle}" (got: ${text.slice(0, 300)})`);
    }
    check(!html.includes('<img src=x'), 'hostile quote rendered as a live <img>');
    check((await p.$$('.decision-answer-panel img')).length === 0, 'an <img> element exists in the panel');
    check(await links[0].getAttribute('aria-expanded') === 'true', 'aria-expanded not true while open');
    check(!dialogs.length, `dialog(s) fired by the hostile quote: ${dialogs.join('; ')}`);
    await screenshot(p, 'decision-answer-open');

    // Phone width: the panel must not push the page wider than the viewport.
    await p.setViewportSize({ width: 360, height: 740 });
    await p.waitForTimeout(300);
    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `decision log scrolls sideways at phone width (doc ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'decision-answer-phone');
    await p.setViewportSize({ width: 1280, height: 800 });

    // Second click closes the panel.
    await links[0].click();
    check(await p.$eval('.decision-answer-panel', el => el.hidden), 'second click did not close the panel');
    check(await links[0].getAttribute('aria-expanded') === 'false', 'aria-expanded not false once closed');

    // An answer log that is gone: the panel says so instead of staying empty.
    answersCtl.stop();
    await failEndpoint(p, '/api/questions/q-1/answers', { status: 404 });
    await links[0].click();
    await p.waitForSelector('.decision-answer-failed, .decision-answer-missing', { timeout: 15000 });
    check((await (await p.$('.decision-answer-panel')).innerText()).trim().length > 0,
      'panel is empty after a failed answers fetch');
    await screenshot(p, 'decision-answer-failed');

    check((await viewState(p)) !== 'blank', 'detail view blank after the decision log rendered');
    check(!dialogs.length, `dialog(s) fired somewhere in the story: ${dialogs.join('; ')}`);
  } finally {
    await context.close().catch(() => {});
  }
});
