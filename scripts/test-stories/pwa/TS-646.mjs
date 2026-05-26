// TS-646 — Dark/light theme toggle persists
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // Read current theme
  const themeBefore = await page.evaluate(() =>
    document.documentElement.getAttribute('data-theme') ||
    document.body.getAttribute('data-theme') ||
    document.documentElement.className.match(/theme-(\w+)/)?.[1] ||
    'unknown'
  );

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-before-toggle');

  // Find theme toggle button/checkbox
  const themeToggleSelector = '#themeToggle, [id*="theme"], .theme-toggle, [data-action="toggle-theme"], input[type="checkbox"][id*="dark"], input[type="checkbox"][id*="theme"]';
  const themeToggle = await page.$(themeToggleSelector);
  if (!themeToggle) {
    throw new Error(`Theme toggle not found with selector: ${themeToggleSelector}`);
  }
  await themeToggle.click();
  await page.waitForTimeout(500);

  // Read new theme
  const themeAfter = await page.evaluate(() =>
    document.documentElement.getAttribute('data-theme') ||
    document.body.getAttribute('data-theme') ||
    document.documentElement.className.match(/theme-(\w+)/)?.[1] ||
    'unknown'
  );

  await screenshot(page, '02-after-toggle');
  await saveLog('result', `theme before: ${themeBefore} / after: ${themeAfter}`);

  if (themeBefore === themeAfter) {
    throw new Error(`Theme did not change after toggle: still "${themeAfter}"`);
  }
});
