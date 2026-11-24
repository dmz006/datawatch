// TS-857.mjs
// PWA story: an installed web app (service worker established, no release
// change) must not navigate on its own. Before the fix every page load
// registered the bare /sw.js and then /sw.js?v=<release>; each swap claimed the
// page, raised the "Update available" bar about a second after load and reloaded
// the page 30 seconds later, over and over.
//
// Steps: open the PWA cold, wait until a worker controls the page (first
// install), then reload once (now an established install) and watch for
// WATCH_MS. Contract: zero main-frame navigations after the settle, no update
// bar, one worker script URL seen and it carries the release (/sw.js?v=...),
// and the page's own release equals the daemon's.
// The release-change half (exactly one navigation after a real release change)
// needs two daemon builds, so it is covered by node tests and a manual run.

import { runPerfStory, coldPage, openPWA, check } from './perf-lib.mjs';

const WATCH_MS = 40000; // longer than the 30 second idle reload the defect used

runPerfStory(async (page) => {
  const { context, page: p } = await coldPage(page.context().browser());
  const workerUrls = new Set();
  context.on('serviceworker', (w) => workerUrls.add(new URL(w.url()).pathname + new URL(w.url()).search));

  await openPWA(p);
  await p.waitForFunction(() => !!(navigator.serviceWorker && navigator.serviceWorker.controller), null, { timeout: 30000 });

  // Established install: reload once, then count navigations from here on.
  await p.reload({ waitUntil: 'domcontentloaded' });
  await p.waitForFunction(() => typeof window.navigate === 'function', null, { timeout: 30000 });
  await p.waitForTimeout(3000); // let any claim / controllerchange settle
  const navs = [];
  p.on('framenavigated', (f) => { if (f === p.mainFrame()) navs.push(Date.now()); });
  let bannerSeen = false;
  const end = Date.now() + WATCH_MS;
  while (Date.now() < end) {
    try { if (await p.$('#updateAvailableBanner')) bannerSeen = true; } catch (_) { /* navigating */ }
    await p.waitForTimeout(500);
  }

  const info = await p.evaluate(async () => {
    const regs = await navigator.serviceWorker.getRegistrations();
    return {
      page: window.__DW_VERSION__,
      daemon: (state._daemonVersionByServer && state._daemonVersionByServer.local) || state._daemonVersion,
      active: regs.map((r) => r.active && r.active.scriptURL),
    };
  });
  check(navs.length === 0, `the established install navigated ${navs.length} time(s) in ${WATCH_MS / 1000}s with no release change`);
  check(!bannerSeen, 'the "Update available" bar appeared with no release change');
  check(info.page && info.daemon && info.page === info.daemon, `page release ${info.page} should equal daemon release ${info.daemon}`);
  check(info.active.length === 1 && /\/sw\.js\?v=/.test(info.active[0] || ''), `one versioned worker should be active, got ${JSON.stringify(info.active)}`);
  check(![...workerUrls].some((u) => u === '/sw.js'), `the bare /sw.js worker must never be registered (saw ${JSON.stringify([...workerUrls])})`);
  await context.close();
});
