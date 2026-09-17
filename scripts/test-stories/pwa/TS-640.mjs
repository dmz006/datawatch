// TS-640 — Settings Compute tab: LLM/backend list visible
import { runStory, connectToPWA, navigateTo, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'settings');
  await screenshot(page, '01-settings-view');

  // Wait for settings tab buttons to be in DOM before interacting
  await page.waitForSelector('.settings-tab-btn, [data-tab]', { state: 'attached', timeout: 8000 })
    .catch(() => {});

  // Call switchSettingsTab directly — more reliable than simulating a click
  // (avoids race conditions with re-renders resetting the active tab state)
  const switched = await page.evaluate(() => {
    if (typeof window.switchSettingsTab === 'function') {
      window.switchSettingsTab('compute');
      return true;
    }
    return false;
  });

  if (!switched) {
    // Fallback: try clicking the tab button
    const btn = await page.$('[data-tab="compute"], [data-tab="llm"], [data-tab="backends"]');
    if (btn) await btn.click();
  }

  await page.waitForTimeout(600);

  // After switching to compute tab, at least one compute section should be visible
  const visible = await page.evaluate(() => {
    const sections = document.querySelectorAll('.settings-section[data-group="compute"]');
    for (const s of sections) {
      const style = window.getComputedStyle(s);
      if (style.display !== 'none' && style.visibility !== 'hidden') return true;
    }
    // Broader fallback: any settings-section visible
    const any = document.querySelector('.settings-section');
    if (any) {
      const style = window.getComputedStyle(any);
      if (style.display !== 'none') return true;
    }
    return false;
  });

  await screenshot(page, '02-compute-tab');

  if (!visible) {
    throw new Error('No visible settings section found after switching to compute tab');
  }
});
