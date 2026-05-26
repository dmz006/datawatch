// TS-640 — Settings LLM tab: backend list visible
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Find and click the LLM settings tab
  const llmTabSelector = '[data-tab="llm"], [data-section="llm"], #llmTab, [data-tab="backends"], #backendsTab';
  const llmTab = await page.$(llmTabSelector);
  if (llmTab) {
    await llmTab.click();
    await page.waitForTimeout(800);
  }

  // Wait for LLM section to appear
  await assertVisible(page, '[id*="llm"], [data-tab="llm"], #llmSettingsPanel, #llmList, [id*="backend"]', 'LLM/backends section');

  await screenshot(page, '02-llm-tab');
});
