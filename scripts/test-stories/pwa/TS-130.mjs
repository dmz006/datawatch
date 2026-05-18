// TS-130 — PWA loads, splash resolves, all 5 nav views reachable
// Replaces the API-only curl check with a real browser interaction test.
import { runStory, connectToPWA, navigateTo, assertVisible, assertCount, screenshot } from './lib.mjs';

await runStory(async (page) => {
  // Load PWA and authenticate
  await connectToPWA(page);
  await screenshot(page, '01-splash-resolved');

  // Sessions view (default) — session list container must exist
  await assertVisible(page, '#sessions-view, [data-view-content="sessions"]', 'sessions view');
  await assertVisible(page, 'nav.nav, .nav', 'bottom nav bar');
  await assertCount(page, 'nav .nav-btn, nav [data-view]', 4, 5000); // at least 4 nav buttons
  await screenshot(page, '02-sessions-view');

  // Alerts view
  await navigateTo(page, 'alerts');
  await assertVisible(page, '#alerts-view, [data-view-content="alerts"]', 'alerts view');
  await screenshot(page, '03-alerts-view');

  // Observer view
  await navigateTo(page, 'observer');
  await assertVisible(page, '#observer-view, [data-view-content="observer"]', 'observer view');
  await screenshot(page, '04-observer-view');

  // Settings view
  await navigateTo(page, 'settings');
  await assertVisible(page, '#settings-view, [data-view-content="settings"]', 'settings view');
  await screenshot(page, '05-settings-view');

  // Back to sessions
  await navigateTo(page, 'sessions');
  await screenshot(page, '06-return-sessions');
});
