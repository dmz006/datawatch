// TS-748 — Autonomous task row: error, verif, retry-btn, session-link DOM elements
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';
import path from 'node:path';

const APP_JS = new URL('../../web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  // Structural check: verify class names exist in app.js source
  let appSrc = '';
  try {
    appSrc = await readFile(APP_JS, 'utf8');
  } catch {
    // Try alternate path
    try {
      appSrc = await readFile(path.join(path.dirname(new URL(import.meta.url).pathname), '../../../internal/server/web/app.js'), 'utf8');
    } catch { /* will check via loaded page script */ }
  }

  if (appSrc) {
    const classCheck = {
      prdTaskError:   appSrc.includes('prd-task-error'),
      prdTaskVerif:   appSrc.includes('prd-task-verif'),
      prdTaskRetry:   appSrc.includes('prd-task-retry-btn'),
      prdTaskSession: appSrc.includes('prd-task-session-link'),
    };
    await saveLog('source-check', JSON.stringify(classCheck));

    const missing = Object.entries(classCheck).filter(([,v]) => !v).map(([k]) => k);
    if (missing.length > 0) {
      throw new Error(`Task row DOM class names missing in app.js: ${missing.join(', ')}`);
    }
    await saveLog('result', `structural OK: all task row class names present in app.js source`);
    return;
  }

  // Fallback: load PWA and create PRD to get live DOM elements
  await connectToPWA(page);
  await navigateTo(page, 'autonomous');
  await page.waitForTimeout(2000);
  await screenshot(page, '01-autonomous');

  const prdId = await page.evaluate(async () => {
    try {
      const r = await fetch('/api/autonomous/prds', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ spec: 'TS-748 test', effort: 'low', project_dir: '/tmp' }),
      });
      const d = await r.json();
      return d.id || null;
    } catch { return null; }
  });

  if (!prdId) {
    throw new Error('Could not create PRD and app.js not readable for structural check');
  }

  await page.waitForTimeout(2000);
  await screenshot(page, '02-prd-created');

  // Check app.js was loaded by the browser — verify class names via script source
  const scriptCheck = await page.evaluate(() => {
    const scripts = [...document.querySelectorAll('script[src]')].map(s => s.src);
    return scripts;
  });
  await saveLog('scripts', JSON.stringify(scriptCheck));

  // Check via the loaded page's innerHTML for class names from any PRD rows
  const domCheck = await page.evaluate((id) => {
    const allHtml = document.getElementById('appContainer')?.innerHTML || document.body.innerHTML;
    return {
      prdTaskError:   allHtml.includes('prd-task-error'),
      prdTaskRetry:   allHtml.includes('prd-task-retry-btn'),
      prdTaskVerif:   allHtml.includes('prd-task-verif'),
      prdTaskSession: allHtml.includes('prd-task-session-link'),
      prdRowRendered: allHtml.includes(id),
    };
  }, prdId);

  await saveLog('dom-check', JSON.stringify(domCheck));
  await screenshot(page, '03-dom-check');

  if (!domCheck.prdTaskError && !domCheck.prdTaskRetry) {
    throw new Error(`Task row DOM classes not rendered (PRD ${prdId} may need tasks): ${JSON.stringify(domCheck)}`);
  }

  await saveLog('result', `live DOM check: ${JSON.stringify(domCheck)}`);
});
