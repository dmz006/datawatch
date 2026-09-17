// TS-645 — Dashboard stat strip visible
import { runStory, connectToPWA, navigateTo, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // Try dedicated dashboard nav first, fall back to sessions (dashboard may be embedded there)
  const dashNav = await page.$('[data-view="dashboard"]');
  if (dashNav) {
    await navigateTo(page, 'dashboard');
  } else {
    // Dashboard stat strip may live on the sessions/main view
    await navigateTo(page, 'sessions');
  }

  await screenshot(page, '01-view');

  // Look for dashboard stat strip elements
  const statStripSelector = '#dashStatBurnRate, [id*="dash"], .stat-strip, .dash-stats, [id*="stat"]';
  let found = null;
  try {
    found = await page.waitForSelector(statStripSelector, { state: 'attached', timeout: 8000 });
  } catch {
    // waitForSelector can throw on navigation or timeout
    found = null;
  }

  if (!found) {
    // Try a JS-level check in case of navigation/context race
    const inDom = await page.evaluate((sel) => !!document.querySelector(sel), statStripSelector)
      .catch(() => false);
    if (!inDom) {
      throw new Error('Dashboard stat strip element not found in DOM');
    }
  }

  await screenshot(page, '02-stat-strip');
});
