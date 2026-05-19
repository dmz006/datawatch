// TS-589 — Observer tab shows Federation Peers card
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  // Load PWA and authenticate
  await connectToPWA(page);
  await screenshot(page, '01-connected');

  // Navigate to the observer view
  await navigateTo(page, 'observer');
  await screenshot(page, '02-observer-view');

  // Wait for any observer-related element to appear
  await page.waitForSelector('#observerPeersList, [id*="peer"], [id*="observer"]', {
    state: 'visible',
    timeout: 10000,
  });

  // Assert the federated peers list panel is visible
  await assertVisible(page, '#observerPeersList', 'federated peers list panel');
  await screenshot(page, '03-peers-panel');

  // Wait for the peers list to finish loading (no longer showing "Loading…")
  await page.waitForFunction(
    () => !document.getElementById('observerPeersList')?.textContent?.includes('Loading…'),
    null,
    { timeout: 10000 },
  );
  await screenshot(page, '04-peers-loaded');

  // Capture peers list text for evidence
  const peersText = await page.evaluate(() =>
    document.getElementById('observerPeersList')?.textContent?.trim() ?? '(not found)',
  );
  await saveLog('result', `observerPeersList content: ${peersText.slice(0, 200)} — PASS`);
});
