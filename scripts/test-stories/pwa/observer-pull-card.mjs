#!/usr/bin/env node
/**
 * Observer pull card story — the Observer view's Monitor stats panel shows the
 * Federation — Observer Pull card (daemon-wide gate, peer targets, stale peers)
 * tags: surface:pwa feature:observer-pull group:federation
 *
 * Runs against the runner's sandbox daemon (never the production daemon). Flow:
 *   1. GET /api/observer/pull must answer 200 (pull protocol present) and
 *      GET /api/stats must carry the observer_pull_* fields — otherwise the
 *      daemon is older than the pull protocol and the story skips;
 *   2. the Monitor card draws the same gate/target/stale values the REST
 *      bodies report (the card is a view over /api/stats, so the drawn state
 *      must equal the body state);
 *   3. every /api/observer/pull row is a pull-mode peer (source "pull") with a
 *      stale_reason the howto lists (none|timeout|error|forbidden|not_permitted|bad_payload|unknown);
 *   4. the card never scrolls sideways at phone width (390x844).
 */
import { screenshot } from './lib.mjs';
import { runPerfStory, openPWA, check, saveJson, skipStory, noHorizontalOverflow } from './perf-lib.mjs';

const PHONE = { width: 390, height: 844 };
const REASONS = new Set(['none','timeout','error','forbidden','not_permitted','bad_payload','unknown']);

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: PHONE, locale: 'en-US', isMobile: true, hasTouch: true });
  const p = await context.newPage();
  const errors = [];
  p.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`));
  try {
    await openPWA(p, { timeout: 60000 });
    const probe = await p.evaluate(async () => {
      const pull = await fetch('/api/observer/pull', { credentials: 'same-origin' });
      const pullBody = pull.ok ? await pull.json() : null;
      const stats = await fetch('/api/stats', { credentials: 'same-origin' });
      const statsBody = stats.ok ? await stats.json() : null;
      return {
        pullStatus: pull.status,
        pullBody,
        statsStatus: stats.status,
        hasStats: !!statsBody && 'observer_pull_enabled' in statsBody,
        statsBody,
      };
    });
    if (probe.pullStatus === 404) await skipStory('the daemon has no /api/observer/pull; start a sandbox daemon built from this tree');
    if (!probe.hasStats) await skipStory('the daemon /api/stats predates the observer_pull_* fields (Monitor card not supported)');
    check(probe.pullStatus === 200, `GET /api/observer/pull -> ${probe.pullStatus}`);

    const rows = (probe.pullBody && (probe.pullBody.peers || probe.pullBody.pull || probe.pullBody)) || [];
    const list = Array.isArray(rows) ? rows : (rows.peers || []);
    check(list.length === (probe.statsBody.observer_pull_targets || 0) || probe.statsBody.observer_pull_targets === 0,
      `the pull target set (${list.length} rows) disagrees with /api/stats observer_pull_targets (${probe.statsBody.observer_pull_targets})`);
    for (const row of list) {
      check(row.source === 'pull', `pull row for "${row.name}" has source "${row.source}"`);
      check(REASONS.has(row.stale_reason), `pull row for "${row.name}" has an unknown stale_reason "${row.stale_reason}"`);
    }

    // Observer view → Monitor stats card: the drawn state equals the REST body.
    await p.evaluate(() => window.navigate('observer'));
    await p.waitForFunction(() => {
      const body = document.querySelector('#statsBody');
      return body && body.innerText.includes('Observer Pull');
    }, null, { timeout: 30000 }).catch(async () => {
      // The card is only painted once /api/stats is polled again on the
      // loaded panel — trigger the loader one more time.
      await p.evaluate(() => {
        const l = window._listLoaders && window._listLoaders['observer-stats:' + (window.state?.activeServer || 'local')];
        if (l && l.load) l.load();
      });
    });
    await p.waitForFunction(() => {
      const body = document.querySelector('#statsBody');
      return body && body.innerText.includes('Observer Pull');
    }, null, { timeout: 30000 });

    const bodyText = await p.$eval('#statsBody', (el) => el.innerText);
    const cardStart = bodyText.indexOf('Observer Pull');
    const card = bodyText.slice(cardStart, cardStart + 200);
    const on = !!probe.statsBody.observer_pull_enabled;
    check(on ? card.includes('Enabled') : card.includes('Disabled'),
      `the Monitor card does not draw the gate state (${on ? 'Enabled' : 'Disabled'}) (got: ${card.slice(0, 160)})`);
    check(card.includes(String(probe.statsBody.observer_pull_targets)),
      `the Monitor card is missing the peer target count (${probe.statsBody.observer_pull_targets}) (got: ${card.slice(0, 160)})`);
    check(card.includes(String(probe.statsBody.observer_pull_stale)),
      `the Monitor card is missing the stale count (${probe.statsBody.observer_pull_stale}) (got: ${card.slice(0, 160)})`);
    await screenshot(p, 'observer-pull-card');

    const fit = await noHorizontalOverflow(p);
    check(fit.ok, `observer pull card scrolls sideways at phone width (scrollWidth ${fit.scrollWidth}, viewport ${fit.innerWidth})`);
    await screenshot(p, 'observer-pull-card-phone');

    check(!errors.length, `page errors: ${errors.join('; ')}`);
    await saveJson('observer-pull-card', {
      gate: on,
      targets: probe.statsBody.observer_pull_targets,
      stale: probe.statsBody.observer_pull_stale,
      rows: list.length,
    });
  } finally {
    await context.close().catch(() => {});
  }
});
