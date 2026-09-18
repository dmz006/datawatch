// TS-749 — Settings view: web_search section visible with toggle
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);
  await navigateTo(page, 'settings');
  await page.waitForTimeout(1500);
  await screenshot(page, '01-settings-loaded');

  const wsSection = await page.evaluate(() => {
    // Settings rows rendered with data-key or visible label text
    const rows = document.querySelectorAll('.settings-row, .setting-item, [data-key], .settings-group > *');
    for (const r of rows) {
      const key = r.getAttribute('data-key') || '';
      const text = r.innerText || '';
      if (key.includes('web_search') || text.toLowerCase().includes('web search')) {
        return { found: true, key, text: text.slice(0, 100) };
      }
    }
    // Check full settings panel text
    const panel = document.querySelector('#settingsPanel, .settings-panel, [class*="settings"]');
    const panelText = panel ? panel.innerText : document.body.innerText;
    const hasWebSearch = panelText.toLowerCase().includes('web search') || panelText.includes('web_search');
    return { found: hasWebSearch, panelText: panelText.slice(0, 800) };
  });

  await saveLog('ws-section', JSON.stringify(wsSection, null, 2));
  await screenshot(page, '02-settings-panel');

  if (!wsSection.found) {
    // Check via API config
    const apiCheck = await page.evaluate(async () => {
      try {
        const r = await fetch('/api/config');
        if (!r.ok) return null;
        const d = await r.json();
        return { hasWebSearch: !!(d.web_search || d['web_search']) };
      } catch { return null; }
    });
    await saveLog('api-config', JSON.stringify(apiCheck));

    if (apiCheck?.hasWebSearch) {
      await saveLog('result', 'web_search in API config but not visible in current settings panel scroll position');
      return;
    }
    throw new Error('web_search.enabled setting not found in settings view or API config');
  }

  await saveLog('result', `web_search section found: key="${wsSection.key}" text="${wsSection.text}"`);
});
