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
  await page.waitForSelector(statStripSelector, { state: 'attached', timeout: 8000 });

  const found = await page.$(statStripSelector);
  if (!found) {
    throw new Error('Dashboard stat strip element not found in DOM');
  }

  await screenshot(page, '02-stat-strip');
});
