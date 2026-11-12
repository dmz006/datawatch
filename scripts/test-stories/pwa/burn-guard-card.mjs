#!/usr/bin/env node
/**
 * Burn guard card story — the usage calculator view shows the burn guard (state, rung,
 * projection, ladder, settings) and the Monitor stats panel shows its stats card
 * tags: surface:pwa feature:burn-guard group:usage
 *
 * Runs against the runner's sandbox daemon (never the production daemon). Flow:
 *   1. open the Automata tab, Automation sub-tab; the burn guard card appears under the
 *      usage calculator and reads the state line from GET /api/usage/burn-guard;
 *   2. the state the card draws equals the state the REST body reports;
 *   3. "Run now" posts /api/usage/burn-guard/run and the card shows the confirmation;
 *   4. the card never scrolls sideways at phone width (390x844);
 *   5. when GET /api/stats carries burn_guard_state, the Monitor stats panel shows the
 *      Burn guard stat card with the same state.
 * The settings the story reads are not changed.
 */
import { screenshot } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const CARD = '#burnGuardCard';

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true });
  const p = await context.newPage();
  const errors = [];
  p.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  try {
    await openPWA(p, { timeout: 60000 });
    const probe = await p.evaluate(async () => {
      const r = await fetch('/api/usage/burn-guard', { credentials: 'same-origin' });
      return { status: r.status, body: r.ok ? await r.json() : null };
    });
    if (probe.status === 404) await skipStory('the daemon has no /api/usage/burn-guard; start a sandbox daemon built from this tree');
    check(probe.status === 200, `GET /api/usage/burn-guard -> ${probe.status}`);

    await p.evaluate(() => {
      window.navigate('autonomous');
      const btn = document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]');
      if (btn && !btn.classList.contains('active')) btn.click();
    });
    await p.waitForSelector(`${CARD} [data-burn-guard="state"]`, { state: 'attached', timeout: 60000 });
    await p.locator(CARD).scrollIntoViewIfNeeded();
    const drawn = await p.$eval(`${CARD} [data-burn-guard="state"]`, (el) => el.getAttribute('data-state'));
    const live = await p.evaluate(async () => (await (await fetch('/api/usage/burn-guard', { credentials: 'same-origin' })).json()).state);
    check(drawn === live, `the card draws state "${drawn}" but the REST body reports "${live}"`);
    const text = await (await p.$(CARD)).innerText();
    check(text.includes('Burn guard'), `card title missing (got: ${text.slice(0, 200)})`);
    check(text.includes('State:'), `card state line missing (got: ${text.slice(0, 300)})`);
    await screenshot(p, 'burn-guard-card');

    // Run now: a daemon without the guard answers 503 and the card shows the error plainly.
    const run = p.waitForResponse((r) => r.url().includes('/api/usage/burn-guard/run') && r.request().method() === 'POST', { timeout: 20000 });
    await p.locator(`${CARD} [data-burn-guard="run"]`).click();
    const res = await run;
    check([200, 503].includes(res.status()), `POST /api/usage/burn-guard/run -> ${res.status()}`);
    await p.waitForFunction((sel) => {
      const c = document.querySelector(sel);
      return c && (c.querySelector('[data-burn-guard="message"]') || c.querySelector('[data-burn-guard="error"]'));
    }, CARD, { timeout: 15000 });

    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `burn guard card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);

    // Monitor stats panel: the stat card appears when the daemon reports a guard state.
    const stats = await p.evaluate(async () => {
      const r = await fetch('/api/stats', { credentials: 'same-origin' });
      return r.ok ? (await r.json()).burn_guard_state || '' : '';
    });
    let monitor = 'not reported';
    if (stats) {
      await p.evaluate(() => window.navigate('observer'));
      await p.waitForSelector('[data-stat="burn-guard"]', { state: 'attached', timeout: 30000 });
      const mtext = await p.$eval('[data-stat="burn-guard"]', (el) => el.innerText);
      check(mtext.includes('Burn guard') && mtext.includes(stats), `Monitor stat card missing the state "${stats}" (got: ${mtext.slice(0, 200)})`);
      monitor = 'shown';
      await screenshot(p, 'burn-guard-monitor-card');
    }
    check(!errors.length, `page errors: ${errors.join('; ')}`);
    await saveJson('burn-guard-card', { state: live, monitor });
  } finally {
    await context.close().catch(() => {});
  }
});
