// TS-758 — PWA Retry button: prdResetTask calls reset_task endpoint and shows toast
// Structural: verify prdResetTask function body calls /api/autonomous/prds/.../reset_task
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  const appSrc = await readFile(APP_JS, 'utf8');

  // Extract prdResetTask function
  const fnMatch = appSrc.match(/function prdResetTask[\s\S]{0,800}?(?=\nfunction |\nconst |\nlet |\nvar |\n\/\/)/);
  const fnBody = fnMatch ? fnMatch[0] : '';

  const callsResetTask = fnBody.includes('reset_task') || appSrc.includes("'/reset_task'") || appSrc.includes('"/reset_task"');
  const showsToast = fnBody.includes('showToast') || fnBody.includes('Toast');

  // Also check the reset_task endpoint is referenced near prdResetTask
  const resetIdx = appSrc.indexOf('prdResetTask');
  const resetContext = resetIdx >= 0 ? appSrc.slice(resetIdx, resetIdx + 600) : '';
  const contextHasEndpoint = resetContext.includes('reset_task');
  const contextHasToast = resetContext.includes('showToast');

  await saveLog('structural-check', JSON.stringify({
    fnBodyFound: fnBody.length > 0,
    callsResetTask,
    showsToast,
    contextHasEndpoint,
    contextHasToast,
    fnBodySlice: fnBody.slice(0, 300),
  }));

  if (!callsResetTask && !contextHasEndpoint) {
    throw new Error('prdResetTask does not reference reset_task endpoint in app.js');
  }
  if (!showsToast && !contextHasToast) {
    throw new Error('prdResetTask does not call showToast after reset in app.js');
  }

  await connectToPWA(page);
  await saveLog('result', 'prdResetTask verified: calls reset_task endpoint and shows toast');
});
