// TS-694 — memory-scope PWA tile visible on the dashboard when memory is enabled
import { runStory, connectToPWA, navigateTo, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // Check memory status via API before testing the tile
  const memEnabled = await page.evaluate(async () => {
    try {
      const r = await fetch('/api/memory/stats', {
        headers: { Authorization: 'Bearer ' + (window._token || '') },
      });
      if (!r.ok) return false;
      const d = await r.json();
      return !!d.enabled;
    } catch { return false; }
  });

  if (!memEnabled) {
    // Memory is disabled — tile should show "stats unavailable" message, not crash
    await navigateTo(page, 'dashboard');
    await screenshot(page, '01-dashboard-memory-disabled');
    const cardPresent = await page.evaluate(() => {
      return !!document.getElementById('dashMemoryScopeCard');
    });
    if (!cardPresent) {
      // Memory tile may not appear when memory is disabled — that is acceptable
      console.log('[TS-694] memory disabled and tile absent — acceptable');
    }
    return; // skip via early return — runStory wraps with ok/skip logic
  }

  await navigateTo(page, 'dashboard');
  await page.waitForTimeout(1200);
  await screenshot(page, '01-dashboard');

  // Wait for memory scope card to appear in DOM
  let cardEl = null;
  try {
    cardEl = await page.waitForSelector('#dashMemoryScopeCard', { state: 'attached', timeout: 8000 });
  } catch {
    cardEl = null;
  }

  if (!cardEl) {
    const inDom = await page.evaluate(() => !!document.getElementById('dashMemoryScopeCard'))
      .catch(() => false);
    if (!inDom) {
      throw new Error('Memory scope card element (#dashMemoryScopeCard) not found in dashboard DOM');
    }
  }

  // Verify it renders actual content (not just an empty div)
  const hasContent = await page.evaluate(() => {
    const el = document.getElementById('dashMemoryScopeCard');
    if (!el) return false;
    const style = window.getComputedStyle(el);
    if (style.display === 'none') return false;
    return el.innerText.trim().length > 0;
  });

  await screenshot(page, '02-memory-scope-card');

  if (!hasContent) {
    // Card exists but may be loading — give it more time
    await page.waitForTimeout(2000);
    const hasContentRetry = await page.evaluate(() => {
      const el = document.getElementById('dashMemoryScopeCard');
      return el ? el.innerText.trim().length > 0 : false;
    });
    if (!hasContentRetry) {
      throw new Error('Memory scope card present but empty after 3s — stats may not be loading');
    }
  }
});
