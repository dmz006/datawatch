// TS-149 — PWA: fullscreen toggle button present and clickable (BL315)
import { runStory, connectToPWA, assertVisible, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // BL315 — headerFullscreenBtn must exist in the header
  await assertVisible(page, '#headerFullscreenBtn', 'fullscreen toggle button');
  await screenshot(page, '01-fullscreen-btn-present');

  // Button click toggles _pwaExpanded; check it doesn't throw
  await page.click('#headerFullscreenBtn');
  const expanded = await page.evaluate(() => typeof _pwaExpanded !== 'undefined' && _pwaExpanded);
  await screenshot(page, '02-after-toggle');

  // Toggle back
  await page.click('#headerFullscreenBtn');
  await screenshot(page, '03-restored');

  await saveLog('result', `expanded after first click: ${expanded} — PASS`);
});
