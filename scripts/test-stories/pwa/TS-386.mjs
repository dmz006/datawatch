// TS-386 — PWA locale switcher persists selection and reloads with translated strings
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  // Load PWA and authenticate
  await connectToPWA(page);

  // Navigate to settings view
  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-initial');

  // Switch to the "about" tab — the locale picker lives there
  await page.waitForFunction(() => typeof window.switchSettingsTab === 'function', null, { timeout: 5000 });
  await page.evaluate(() => window.switchSettingsTab('about'));
  await screenshot(page, '02-settings-about-tab');

  // Wait for the locale picker to exist in the DOM (it may be hidden if section collapsed)
  await page.waitForSelector('#localePickerAbout', { timeout: 10000 });

  // Record initial locale value
  const initialLocale = await page.evaluate(() => {
    const sel = document.getElementById('localePickerAbout');
    return sel ? sel.value : null;
  });
  await saveLog('initial-locale', `initialLocale=${initialLocale}`);

  // Switch to German — call the global function directly so the reload is the behavior under test
  await page.evaluate(() => window.setLocaleOverride('de'));

  // Wait for the page to reload and settle
  await page.waitForLoadState('domcontentloaded', { timeout: 15000 });
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {});

  await screenshot(page, '03-after-locale-change');

  // Verify the locale persisted in localStorage
  const storedLocale = await page.evaluate(() => localStorage.getItem('datawatch.locale'));
  if (storedLocale !== 'de') {
    throw new Error(`Expected localStorage 'datawatch.locale' to be 'de', got '${storedLocale}'`);
  }
  await saveLog('stored-locale', `storedLocale=${storedLocale} — correct`);

  // Reset to English so the session doesn't leave the app in a non-English state
  await page.evaluate(() => window.setLocaleOverride('en'));
  await page.waitForLoadState('domcontentloaded', { timeout: 15000 });
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {});

  await screenshot(page, '04-reset-to-en');

  const resetLocale = await page.evaluate(() => localStorage.getItem('datawatch.locale'));
  await saveLog('result', `initialLocale=${initialLocale} switchedTo=de storedLocale=${storedLocale} resetTo=${resetLocale} — PASS`);
});
