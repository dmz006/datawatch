#!/usr/bin/env node
/**
 * TS-806 — a request slower than the timeout shows failed, never a stale warning;
 * the stale warning appears only when data is older than the polling interval
 * tags: surface:pwa feature:load-performance feature:stale-state
 *
 * Uses the Automation capacity card (/api/capacity, pollMs 5000, loader timeout
 * PERF_LOADER_TIMEOUT_MS, default 15000) inside the Automation tab.
 *   A. First load takes longer than the timeout: the card goes loading -> failed
 *      with Retry. .list-state-stale must never be present, at any sample.
 *   B. A slower-than-normal but successful first load: no stale warning while
 *      loading or right after it finished.
 *   C. After a good load the endpoint starts failing: no stale warning inside the
 *      first polling interval; it must appear once the data is older than that.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, failEndpoint, delayEndpoint, check, saveJson, BUDGET,
} from './perf-lib.mjs';

const CARD = '#automationCapacityCard';
const POLL_MS = 5000; // pollMs of the automation_capacity loader

const cardHas = (p, sel) => p.$(`${CARD} ${sel}`).then((e) => !!e).catch(() => false);

async function sample(p, ms, onSample) {
  const t0 = Date.now();
  while (Date.now() - t0 < ms) {
    await onSample(Date.now() - t0);
    await p.waitForTimeout(100);
  }
}

await runPerfStory(async (page) => {
  const browser = page.context().browser();
  const report = {};

  // A. slower than the timeout
  {
    const { context, page: p } = await coldPage(browser);
    try {
      await openPWA(p);
      const slow = await delayEndpoint(p, '/api/capacity', BUDGET.loaderTimeoutMs + 4000);
      await p.evaluate(() => { document.getElementById('view').innerHTML = ''; window.navigate('automation'); });
      let sawLoading = false, sawStale = false, sawFailed = false;
      await sample(p, BUDGET.loaderTimeoutMs + 3000, async () => {
        if (await cardHas(p, '.list-state-skeleton')) sawLoading = true;
        if (await cardHas(p, '.list-state-stale')) sawStale = true;
        if (await cardHas(p, '.list-state-failed')) sawFailed = true;
      });
      await screenshot(p, 'timeout-failed');
      check(sawLoading, 'A: no loading indicator during the slow first load');
      check(sawFailed, `A: no failed state after the request exceeded the ${BUDGET.loaderTimeoutMs} ms timeout`);
      check(!sawStale, 'A: stale warning shown during/after a timed-out first load');
      check(await cardHas(p, '.list-state-retry'), 'A: failed state has no Retry button');
      await slow.stop();
      report.timeout = { sawLoading, sawFailed, sawStale };
    } finally {
      await context.close().catch(() => {});
    }
  }

  // B + C. slow-but-successful first load, then a failing endpoint
  {
    const { context, page: p } = await coldPage(browser);
    try {
      await openPWA(p);
      const slow = await delayEndpoint(p, '/api/capacity', 2500, { times: 1 });
      await p.evaluate(() => { document.getElementById('view').innerHTML = ''; window.navigate('automation'); });
      let staleDuringFirst = false;
      let loaded = false;
      await sample(p, 9000, async () => {
        if (await cardHas(p, '.list-state-stale')) staleDuringFirst = true;
        if (await cardHas(p, '.list-state-age')) loaded = true;
        // stop sampling early only after the first poll could have changed things
      });
      await slow.stop();
      check(loaded, 'B: card never finished its first load');
      check(!staleDuringFirst, 'B: stale warning during the first load or inside the first polling interval of fresh data');

      // C. fresh data, then the endpoint fails
      const down = await failEndpoint(p, '/api/capacity', { status: 503 });
      const t0 = Date.now();
      let staleEarly = false, staleAt = null;
      await sample(p, POLL_MS * 4, async (elapsed) => {
        const stale = await cardHas(p, '.list-state-stale');
        if (stale && staleAt === null) staleAt = elapsed;
        if (stale && elapsed < POLL_MS / 2 && Date.now() - t0 < POLL_MS / 2) staleEarly = true;
      });
      await screenshot(p, 'stale-after-failures');
      await down.stop();
      check(!staleEarly, `C: stale warning at ${staleAt} ms, before the data was older than ${POLL_MS} ms`);
      check(staleAt !== null, `C: no stale warning although the data aged past ${POLL_MS} ms while the endpoint failed`);
      report.stale = { staleDuringFirst, staleAt };
    } finally {
      await context.close().catch(() => {});
    }
  }
  await saveJson('timeout-stale', report);
});
