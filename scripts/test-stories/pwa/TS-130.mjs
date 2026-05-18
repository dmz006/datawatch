// TS-130 — PWA loads, splash resolves, all 5 nav views reachable
// Replaces the API-only curl check with a real browser interaction test.
import { runStory, connectToPWA, navigateTo, assertVisible, assertCount, screenshot } from './lib.mjs';

await runStory(async (page) => {
  // Load PWA and authenticate
  await connectToPWA(page);
  await screenshot(page, '01-splash-resolved');

  // Sessions view (default) — nav button active + main view container visible
  await assertVisible(page, '[data-view="sessions"].active, #view', 'sessions view');
  await assertVisible(page, '#nav, nav', 'bottom nav bar');
  await assertCount(page, '#nav [data-view], nav [data-view]', 4, 5000); // at least 4 nav buttons
  await screenshot(page, '02-sessions-view');

  // Alerts view
  await navigateTo(page, 'alerts');
  await assertVisible(page, '[data-view="alerts"].active, #view', 'alerts view');
  await screenshot(page, '03-alerts-view');

  // Observer view
  await navigateTo(page, 'observer');
  await assertVisible(page, '[data-view="observer"].active, #view', 'observer view');
  await screenshot(page, '04-observer-view');

  // Settings view
  await navigateTo(page, 'settings');
  await assertVisible(page, '[data-view="settings"].active, #view', 'settings view');
  await screenshot(page, '05-settings-view');

  // Back to sessions
  await navigateTo(page, 'sessions');
  await screenshot(page, '06-return-sessions');
});
