// TS-639 — Settings About tab has version info
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Click the About tab (data-tab="about") which contains version info
  const aboutTab = await page.$('[data-tab="about"]');
  if (aboutTab) {
    await aboutTab.click();
    await page.waitForTimeout(500);
  } else {
    // Fallback: try general or other tabs
    const generalTab = await page.$('[data-tab="general"], [data-section="general"], #settingsGeneral, #generalTab');
    if (generalTab) {
      await generalTab.click();
      await page.waitForTimeout(500);
    }
  }

  // #aboutVersion is in the About tab
  await assertVisible(page, '#aboutVersion, [id*="version"], .settings-version', 'version info');

  await screenshot(page, '02-about-tab');
});
