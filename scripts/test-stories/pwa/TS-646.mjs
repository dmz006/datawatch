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

  // Navigate to the About tab where the theme picker (#themePickerAbout) lives
  const aboutTab = await page.$('[data-tab="about"]');
  if (aboutTab) {
    await aboutTab.click();
    await page.waitForTimeout(500);
  }

  // Find theme toggle — #themePickerAbout is a <select> in the About tab
  const themeToggleSelector = '#themePickerAbout, #themeToggle, [id*="theme"], .theme-toggle, [data-action="toggle-theme"]';
  const themeToggle = await page.$(themeToggleSelector);
  if (!themeToggle) {
    throw new Error(`Theme toggle not found with selector: ${themeToggleSelector}`);
  }

  // For a select element, change the value; for a checkbox/button, click it
  const tagName = await themeToggle.evaluate(el => el.tagName.toLowerCase());
  if (tagName === 'select') {
    // Select a different value than current
    const currentVal = await themeToggle.evaluate(el => el.value);
    const newVal = currentVal === 'dark' ? 'light' : 'dark';
    await themeToggle.selectOption(newVal);
  } else {
    await themeToggle.click();
  }
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
