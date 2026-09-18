// TS-744 — PWA Monitor tab: web search stats card visible when web_search.enabled
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // Verify via API first
  const wsEnabled = await page.evaluate(async () => {
    try {
      const r = await fetch('/api/stats');
      if (!r.ok) return false;
      const d = await r.json();
      return !!d.web_search_enabled;
    } catch { return false; }
  });

  if (!wsEnabled) {
    await saveLog('skip', 'web_search.enabled=false — card only appears when web_search is enabled');
    return;
  }

  // Navigate to Observer (Monitor) view
  await navigateTo(page, 'observer');
  await page.waitForTimeout(2500);
  await screenshot(page, '01-observer-loaded');

  const cardText = await page.evaluate(() => {
    const cards = document.querySelectorAll('.stat-card');
    for (const c of cards) {
      if (c.innerText.includes('Web Search')) return c.innerText;
    }
    return null;
  });

  await screenshot(page, '02-stats-cards');

  if (!cardText) {
    throw new Error('Web Search stat-card not found in Observer view (web_search_enabled=true)');
  }

  await saveLog('result', `Web Search card found: ${cardText.replace(/\n/g, ' ')}`);
});
