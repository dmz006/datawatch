// TS-815 — PWA Automation sub-tab: the Progress card renders its table and completion
// range, shows the failed state with a Retry button when the fetch fails, no text in
// the card looks like a tracker ID or uses the internal three-letter term, and the
// Automata and Templates sub-tabs carry no card.
//
// Runs against the sandbox daemon (TEST_HTTP / TEST_TLS from run_pwa_story), never the
// production daemon. It reads whatever Automata the sandbox holds; the card must render
// for an empty store too, so nothing is created and nothing is skipped.
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

const CARD = '#progressCard .progress-card';

// Text a user can read or hear in the card: every text node plus the title,
// aria-label and placeholder attributes.
async function cardTexts(page) {
  return page.evaluate((sel) => {
    const root = document.querySelector(sel);
    if (!root) return [];
    const out = [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const s = n.nodeValue.trim();
      if (s) out.push(s);
    }
    for (const el of root.querySelectorAll('*')) {
      for (const a of ['title', 'aria-label', 'placeholder']) {
        const v = el.getAttribute(a);
        if (v && v.trim()) out.push(v.trim());
      }
    }
    return out;
  }, CARD);
}

// A tracker ID is a short capital prefix followed by digits (BL449, GH#128, B87, TS-811).
const TRACKER_ID = /\b(?:BL|GH|PR|TS|B)[-# ]?\d+\b/;
// The internal three-letter term, as a whole word, in any case.
const INTERNAL_TERM = /\bprds?\b/i;

async function assertNoInternalText(page, label) {
  const texts = await cardTexts(page);
  const bad = texts.filter((s) => TRACKER_ID.test(s) || INTERNAL_TERM.test(s));
  if (bad.length) {
    throw new Error(`${label}: card text matches a tracker ID or the internal term: ${JSON.stringify(bad.slice(0, 5))}`);
  }
  return texts.length;
}

await runStory(async (page) => {
  await connectToPWA(page);
  await navigateTo(page, 'autonomous');

  // --- the card lives on the Automation sub-tab ------------------------------
  await page.evaluate(({ subtab }) => switchAutomataTab(subtab), { subtab: 'automation' });

  // --- dashboard card -------------------------------------------------------
  await page.waitForSelector(`${CARD}[data-progress-status]`, { state: 'visible', timeout: 20000 });
  await page.waitForSelector(`${CARD}[data-progress-status="ok"]`, { state: 'visible', timeout: 20000 })
    .catch(async () => {
      const status = await page.getAttribute(CARD, 'data-progress-status');
      throw new Error(`Progress card did not reach the ok state (status: ${status})`);
    });
  await screenshot(page, '01-card-ok');

  // --- table ---------------------------------------------------------------
  const table = await page.evaluate((sel) => {
    const t = document.querySelector(`${sel} .progress-card-table`);
    if (!t) return null;
    return {
      headCells: t.querySelectorAll('thead th').length,
      rows: [...t.querySelectorAll('tbody tr')].map((r) => (r.querySelector('th')?.innerText || '').trim()),
    };
  }, CARD);
  if (!table) throw new Error('Progress card has no table');
  if (table.headCells < 12) throw new Error(`table head has ${table.headCells} cells, expected a label cell and 11 columns`);
  if (table.rows.length < 3 || table.rows.some((r) => !r)) {
    throw new Error(`table needs the feature, bug and total rows, got: ${JSON.stringify(table.rows)}`);
  }

  // --- completion range -----------------------------------------------------
  const completion = await page.evaluate((sel) => {
    const title = typeof t === 'function' ? t('progress_card_completion_title') : 'Completion range';
    const sections = [...document.querySelectorAll(`${sel} .progress-card-section`)];
    const sec = sections.find((s) => (s.firstElementChild?.innerText || '').trim() === title);
    if (!sec) return null;
    return {
      title,
      lines: sec.querySelectorAll('.progress-card-line').length,
      methods: sec.querySelectorAll('.progress-card-method').length,
      text: sec.innerText,
    };
  }, CARD);
  if (!completion) throw new Error('completion-range area not found in the Progress card');
  if (completion.methods < 4) throw new Error(`completion range lists ${completion.methods} methods, expected the four`);
  if (completion.lines < 5) throw new Error('completion range has no range line');
  await saveLog('completion', completion.text);

  const okTexts = await assertNoInternalText(page, 'ok state');

  // --- forced fetch failure -------------------------------------------------
  // The failed state shows only when nothing was ever loaded, so reset the
  // loader state, answer the progress routes with a server error and ask again.
  await page.route('**/api/autonomous/progress**', (route) =>
    route.fulfill({ status: 500, contentType: 'text/plain', body: 'forced failure' }));
  await page.evaluate(() => {
    window._progressCardState = { status: 'loading', data: null, error: '', fetchedAt: 0, trend: null, attemptedAt: 0, inflight: null };
    return window.loadProgressCard(true);
  });
  await page.waitForSelector(`${CARD}[data-progress-status="failed"] .progress-card-retry`, { state: 'visible', timeout: 10000 });
  const alertText = await page.innerText(`${CARD}[data-progress-status="failed"] [role="alert"]`);
  if (!alertText.trim()) throw new Error('failed state shows no message');
  await screenshot(page, '02-card-failed');
  const failedTexts = await assertNoInternalText(page, 'failed state');

  // --- retry recovers -------------------------------------------------------
  await page.unroute('**/api/autonomous/progress**');
  await page.click(`${CARD}[data-progress-status="failed"] .progress-card-retry`);
  await page.waitForSelector(`${CARD}[data-progress-status="ok"] .progress-card-table`, { state: 'visible', timeout: 15000 });
  await screenshot(page, '03-card-recovered');

  // --- the Automata and Templates sub-tabs carry no card ---------------------
  for (const other of ['automata', 'templates']) {
    await page.evaluate((subtab) => switchAutomataTab(subtab), other);
    const visible = await page.evaluate(() => {
      const el = document.getElementById('progressCard');
      return !!el && el.style.display !== 'none';
    });
    if (visible) throw new Error(`the Progress card must not show on the ${other} sub-tab`);
  }
  await page.evaluate(({ subtab }) => switchAutomataTab(subtab), { subtab: 'automation' });

  await saveLog('result', `card ok (${okTexts} text items), failed state with Retry (${failedTexts} text items), recovered; table rows: ${table.rows.join(', ')}`);
});
