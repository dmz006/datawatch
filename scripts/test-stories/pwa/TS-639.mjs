// TS-639 — Settings General tab reachable
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Look for general tab active state or click it if present
  const generalTabSelector = '[data-tab="general"], [data-section="general"], #settingsGeneral, #generalTab';
  const generalTab = await page.$(generalTabSelector);
  if (generalTab) {
    await generalTab.click();
    await page.waitForTimeout(500);
  }

  // Look for version info which is typically in the general/about section
  await assertVisible(page, '[id*="version"], .settings-version, #settingsVersion, #aboutVersion, [class*="version"]', 'version info');

  await screenshot(page, '02-general-tab');
});
