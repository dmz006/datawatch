// TS-642 — Settings Automata tab reachable
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Find and click the Automata tab
  const automataTabSelector = '[data-tab="automata"], [data-section="automata"], #automataTab, [data-group="automata"]';
  const automataTab = await page.$(automataTabSelector);
  if (automataTab) {
    await automataTab.click();
    await page.waitForTimeout(800);
  }

  await assertVisible(page, '[data-group="automata"], [id*="automata"], #automataPanel, [data-tab="automata"]', 'automata section');

  await screenshot(page, '02-automata-tab');
});
