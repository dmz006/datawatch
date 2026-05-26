// TS-638 — Alerts view renders
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'alerts');
  await assertVisible(page, '[id*="alert"], #alertsList, .alert-item, .alerts-container, #view', 'alerts view');

  await screenshot(page, '01-alerts-view');
});
