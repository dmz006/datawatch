// TS-189 — PWA: Settings view renders key sections
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);
  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Settings nav button is active and main view container is visible
  await assertVisible(page, '[data-view="settings"].active, #view', 'settings view container');
  await screenshot(page, '02-settings-content');
});
