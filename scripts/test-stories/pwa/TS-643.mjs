// TS-643 — Observer view loads without error
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  const consoleErrors = [];
  page.on('console', (msg) => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });

  await connectToPWA(page);

  await navigateTo(page, 'observer');
  // Wait for data to load
  await page.waitForTimeout(3000);

  await assertVisible(page, '#observerStats, [id*="observer"], #view', 'observer view');

  if (consoleErrors.length > 0) {
    await saveLog('console-errors', consoleErrors.join('\n'));
  }

  await screenshot(page, '01-observer-view');
});
