#!/usr/bin/env node
/**
 * TS-812 — a dropped and restored connection recovers with a small, bounded
 * refresh, and repeats do not cause a reconnect storm
 * tags: surface:pwa feature:load-performance feature:reconnect
 *
 * Needs a daemon started on the seeded fixture (SeedPerfServer in
 * internal/server/perf_seed_test.go); seedLargeSet() skips (exit 2) when the
 * large set is not there. With Sessions and Automata loaded the connection is
 * dropped (context offline plus the page's websocket closed), held down past
 * the socket watchdog threshold (PERF_OUTAGE_MS, default 40 s), then restored.
 * From the restore until the UI is live again (socket open, view showing
 * content) plus a short settle window, the requests and bytes are measured:
 *   - at most PERF_BUDGET_RECONNECT_REQUESTS requests (default 6);
 *   - at most PERF_BUDGET_RECONNECT_BYTES bytes (default 256 KB);
 *   - no full-form list request (full=1, or a list without view=summary).
 * The drop and restore is then repeated; the second pass must stay inside the
 * same budgets, so repeated reconnects do not reload everything again.
 */
import { screenshot, saveLog } from './lib.mjs';
import {
  runPerfStory, TABS, coldPage, openPWA, waitForState, viewState, recordNetwork,
  fullFormReason, sumBytes, check, saveJson, seedLargeSet, cleanupSeed, BUDGET,
} from './perf-lib.mjs';

// Window after the UI is live in which late requests are still counted.
const SETTLE_MS = 5000;
// The app backs off up to 30 s between reconnect attempts.
const LIVE_TIMEOUT_MS = 90000;

const socketOpen = () => typeof state !== 'undefined' && state.connected === true && !!state.ws && state.ws.readyState === 1;

await runPerfStory(async (page) => {
  const seed = await seedLargeSet({ log: (m) => console.error(m) });
  await saveLog('seed', JSON.stringify(seed));
  const { context, page: p } = await coldPage(page.context().browser());
  const rec = recordNetwork(p);
  const passes = [];
  try {
    await openPWA(p, { timeout: 120000 });
    await p.waitForFunction(socketOpen, null, { timeout: 30000 });
    // Sessions first, Automata last: the view on screen when the link drops.
    for (const name of ['sessions', 'automata']) {
      const tab = TABS.find((t) => t.name === name);
      await p.evaluate((v) => window.navigate(v), tab.view);
      await waitForState(p, ['content', 'empty'], 120000);
    }
    await p.waitForTimeout(SETTLE_MS);

    for (const pass of [1, 2]) {
      await rec.flush();
      // drop: offline, and close the page's socket so the app sees the loss
      await context.setOffline(true);
      await p.evaluate(() => { try { if (state.ws) state.ws.close(); } catch { /* already closed */ } });
      const outageStart = rec.mark();
      await p.waitForTimeout(BUDGET.outageMs);
      const offlineAttempts = rec.since(outageStart).length;

      // restore, then measure until the UI is live again plus the settle window
      const pos = rec.mark();
      const t0 = Date.now();
      await context.setOffline(false);
      await p.waitForFunction(socketOpen, null, { timeout: LIVE_TIMEOUT_MS, polling: 100 })
        .catch(() => { throw new Error(`pass ${pass}: socket did not reopen within ${LIVE_TIMEOUT_MS} ms of the restore`); });
      await waitForState(p, ['content', 'empty', 'failed'], LIVE_TIMEOUT_MS);
      const liveMs = Date.now() - t0;
      const stateNow = await viewState(p);
      await p.waitForTimeout(SETTLE_MS);
      await rec.flush();

      const reqs = rec.since(pos);
      const violations = reqs
        .map((e) => ({ url: e.url, reason: fullFormReason(e.url, e.method) }))
        .filter((v) => v.reason);
      const bytes = sumBytes(reqs);
      passes.push({
        pass, liveMs, state: stateNow, requests: reqs.length, bytes, offlineAttempts,
        requestsSeen: reqs.map((e) => ({ method: e.method, url: e.url, status: e.status, bytes: e.bytes, failed: e.failed })),
        violations,
      });
      await screenshot(p, `reconnect-${pass}`).catch(() => {});

      check(stateNow !== 'failed', `pass ${pass}: view ended in the failed state after the restore`);
      check(violations.length === 0,
        `pass ${pass}: full-form list request(s) after reconnect: ${violations.map((v) => `${v.url} (${v.reason})`).join(' | ')}`);
      check(reqs.length <= BUDGET.reconnectRequests,
        `pass ${pass}: ${reqs.length} requests after reconnect, budget ${BUDGET.reconnectRequests}: ${reqs.map((e) => e.url).join(' | ')}`);
      check(bytes <= BUDGET.reconnectBytes,
        `pass ${pass}: ${bytes} bytes after reconnect, budget ${BUDGET.reconnectBytes}`);
    }
  } finally {
    await saveJson('reconnect', { seed, budget: BUDGET, passes }).catch(() => {});
    await context.setOffline(false).catch(() => {});
    await context.close().catch(() => {});
    if (seed.created > 0) await cleanupSeed();
  }
});
