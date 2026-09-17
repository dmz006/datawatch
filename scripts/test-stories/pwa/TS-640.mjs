// TS-640 — Settings Compute tab: LLM/backend list visible
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Click the Compute tab (data-tab="compute") which contains LLM/backend sections
  const computeTab = await page.$('[data-tab="compute"]');
  if (computeTab) {
    await computeTab.click();
    await page.waitForTimeout(800);
  } else {
    // Fallback: try llm or backends tabs
    const llmTab = await page.$('[data-tab="llm"], [data-tab="backends"], [data-section="llm"], #llmTab');
    if (llmTab) {
      await llmTab.click();
      await page.waitForTimeout(800);
    }
  }

  // Compute tab has settings sections with data-group="compute"
  await assertVisible(page, '[data-group="compute"], [id*="llm"], .settings-section', 'LLM/backends section');

  await screenshot(page, '02-compute-tab');
});
