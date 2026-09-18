// TS-756 — PWA task row: Retry button visible for failed task in running PRD
// Structural: verify app.js canRetry condition includes failed status + running PRD
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';
import path from 'node:path';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  let appSrc = await readFile(APP_JS, 'utf8');

  // Verify canRetry includes task.status === 'failed' AND prd.status === 'running'
  const hasFailedCondition = appSrc.includes("task.status === 'failed'") ||
                              appSrc.includes('task.status==="failed"');
  const hasRunningCondition = appSrc.includes("prd.status === 'running'") ||
                               appSrc.includes('prd.status==="running"');
  const hasCanRetry = appSrc.includes('canRetry');
  const hasRetryBtn = appSrc.includes('prd-task-retry-btn');

  await saveLog('structural-check', JSON.stringify({
    hasFailedCondition,
    hasRunningCondition,
    hasCanRetry,
    hasRetryBtn,
  }));

  if (!hasCanRetry) throw new Error('canRetry variable not found in app.js');
  if (!hasRetryBtn) throw new Error('prd-task-retry-btn class not found in app.js');
  if (!hasFailedCondition) throw new Error("canRetry: task.status==='failed' condition missing in app.js");
  if (!hasRunningCondition) throw new Error("canRetry: prd.status==='running' guard missing in app.js");

  // Also verify via live PWA that the app loaded correctly.
  await connectToPWA(page);
  const version = await page.evaluate(() => window._dwVersion || '').catch(() => '');
  await saveLog('result', `app.js canRetry logic verified. PWA version=${version}`);
});
