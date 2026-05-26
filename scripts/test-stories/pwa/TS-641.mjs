// TS-641 — Settings Comms tab reachable
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Find and click the Comms tab
  const commsTabSelector = '[data-tab="comms"], [data-section="comms"], #commsTab, [data-group="comms"]';
  const commsTab = await page.$(commsTabSelector);
  if (commsTab) {
    await commsTab.click();
    await page.waitForTimeout(800);
  }

  await assertVisible(page, '[data-group="comms"], [id*="comms"], #commsPanel, [data-tab="comms"]', 'comms section');

  await screenshot(page, '02-comms-tab');
});
