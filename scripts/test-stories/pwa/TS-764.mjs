// TS-764 — PWA Settings: planning backend picker accepts all configured LLMs
// Verifies autonomous.planning_backend config key present in settings panel
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  // Structural: verify planning_backend is in settings config keys.
  const appSrc = await readFile(APP_JS, 'utf8');
  const hasPlanningBackendKey  = appSrc.includes("'autonomous.planning_backend'") ||
                                  appSrc.includes('"autonomous.planning_backend"');
  const hasLLMBackendType      = appSrc.includes("type: 'llm_backend'") ||
                                  appSrc.includes('type:"llm_backend"');

  await saveLog('structural-check', JSON.stringify({ hasPlanningBackendKey, hasLLMBackendType }));

  if (!hasPlanningBackendKey) throw new Error("autonomous.planning_backend key not in settings config");
  if (!hasLLMBackendType)     throw new Error("llm_backend type field not found in settings config");

  // Live: navigate to settings and verify planning backend field is in the HTML (may be collapsed).
  await connectToPWA(page);
  await navigateTo(page, 'settings');
  await page.waitForTimeout(2000);
  await screenshot(page, '01-settings');

  // Check innerHTML (includes collapsed/hidden elements) for planning backend text.
  const inHtml = await page.evaluate(() => {
    const html = document.body.innerHTML;
    return {
      hasPlanningBackendLabel: html.includes('Planning backend') || html.includes('planning_backend'),
      hasPlanningBackendInput: html.includes('autonomous.planning_backend'),
    };
  });

  await saveLog('html-check', JSON.stringify(inHtml));

  if (!inHtml.hasPlanningBackendLabel && !inHtml.hasPlanningBackendInput) {
    // Try expanding settings sections by clicking them.
    await page.evaluate(() => {
      document.querySelectorAll('.settings-section-toggle').forEach(el => {
        const toggleId = el.getAttribute('onclick')?.match(/getElementById\('([^']+)'\)/)?.[1];
        if (toggleId) {
          const target = document.getElementById(toggleId);
          if (target) target.style.display = '';
        }
      });
    });
    await page.waitForTimeout(500);
    await screenshot(page, '02-expanded');

    const expandedCheck = await page.evaluate(() => {
      const html = document.body.innerHTML;
      return html.includes('Planning backend') || html.includes('planning_backend') ||
             html.includes('autonomous.planning_backend');
    });

    await saveLog('expanded-check', `found after expand: ${expandedCheck}`);
    if (!expandedCheck) {
      throw new Error('planning backend setting not found in settings view (even after expanding sections)');
    }
  }

  await screenshot(page, '03-planning-backend-found');
  await saveLog('result', `planning backend picker verified in settings HTML (collapsed ok)`);
});
