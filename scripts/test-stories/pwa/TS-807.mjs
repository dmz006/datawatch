#!/usr/bin/env node
/**
 * TS-807 — a response that arrives after a newer one does not overwrite it
 * tags: surface:pwa feature:load-performance feature:request-ordering
 *
 * Two loads of the Automation capacity loader are started back to back. The
 * first request is held by the test (its body marked "__stale") and released
 * only after the second one has completed. The loader must keep the newer data:
 * state.data has no __stale marker, and the card shows no failed banner.
 */
import { screenshot } from './lib.mjs';
import {
  runPerfStory, coldPage, openPWA, reorderEndpoint, check, saveJson,
} from './perf-lib.mjs';

await runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  try {
    await openPWA(p);
    await p.evaluate(() => window.navigate('automation'));
    await p.waitForFunction(() => window._listLoaders && window._listLoaders.automation_capacity
      && window._listLoaders.automation_capacity.state.loadedOnce, null, { timeout: 30000 })
      .catch(() => { throw new Error('automation_capacity loader never loaded'); });

    const order = await reorderEndpoint(p, '/api/capacity', {
      markStale: (b) => (b && typeof b === 'object' && !Array.isArray(b) ? { ...b, __stale: true } : b),
    });
    // First load: held. Second load: supersedes it and completes.
    const first = p.evaluate(() => window._listLoaders.automation_capacity.load().then(() => 'done'));
    await p.waitForTimeout(300);
    check(order.firstHeld(), 'first request was not intercepted');
    await p.evaluate(() => window._listLoaders.automation_capacity.load());
    const afterSecond = await p.evaluate(() => {
      const s = window._listLoaders.automation_capacity.state;
      return { loading: s.loading, stale: !!(s.data && s.data.__stale), at: s.at };
    });
    check(!afterSecond.loading, 'second load did not complete');
    check(!afterSecond.stale, 'second (newer) response carried the stale marker');

    order.release();                 // the older response now arrives late
    await first;
    await p.waitForTimeout(500);
    const final = await p.evaluate(() => {
      const s = window._listLoaders.automation_capacity.state;
      return { stale: !!(s.data && s.data.__stale), at: s.at, error: s.error, loading: s.loading };
    });
    await screenshot(p, 'reordered');
    check(!final.stale, 'late older response overwrote the newer data');
    check(final.at === afterSecond.at, 'late older response changed the data timestamp');
    check(!final.error, `late older response set an error: ${final.error}`);
    check(!(await p.$('#automationCapacityCard .list-state-failed').catch(() => null)), 'failed banner after a late response');
    await saveJson('reorder', { afterSecond, final });
    await order.stop();
  } finally {
    await context.close().catch(() => {});
  }
});
