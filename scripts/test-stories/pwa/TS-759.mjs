// TS-759 — Toggle "None" deselects only the visible filtered set (not non-visible selected sessions)
// Structural: verify selectAllInactive operates on _visibleDone not all selectedSessions
import { runStory, connectToPWA, saveLog } from './lib.mjs';
import { readFile } from 'node:fs/promises';

const APP_JS = new URL('../../../internal/server/web/app.js', import.meta.url).pathname;

await runStory(async (page) => {
  // Structural check: verify selectAllInactive only operates on _visibleDone.
  const appSrc = await readFile(APP_JS, 'utf8');

  const fnMatch = appSrc.match(/function selectAllInactive\(\)[\s\S]{0,800}?(?=\nfunction |\nwindow\.)/);
  const fnBody = fnMatch ? fnMatch[0] : '';

  const usesVisibleDone    = fnBody.includes('_visibleDone');
  const notAllSessions     = !fnBody.includes('state.sessions.filter') &&
                              !fnBody.includes('state.sessions.forEach');
  const togglesOnAllSelect = fnBody.includes('allSelected') && fnBody.includes('delete') && fnBody.includes('add');

  await saveLog('structural-check', JSON.stringify({
    fnBodyFound: fnBody.length > 0,
    usesVisibleDone,
    notAllSessions,
    togglesOnAllSelect,
    fnBodySlice: fnBody.slice(0, 400),
  }));

  if (!fnBody) throw new Error('selectAllInactive function not found in app.js');
  if (!usesVisibleDone) throw new Error('selectAllInactive does not use _visibleDone — scope bug: would affect non-visible sessions');
  if (!notAllSessions) throw new Error('selectAllInactive iterates state.sessions instead of _visibleDone — wrong scope');
  if (!togglesOnAllSelect) throw new Error('selectAllInactive missing toggle logic (allSelected → delete, else add)');

  // Live check: verify _visibleDone is a subset of all sessions.
  await connectToPWA(page);

  const liveCheck = await page.evaluate(() => {
    const totalSessions = state.sessions ? state.sessions.length : 0;
    const visibleDoneLen = state._visibleDone ? state._visibleDone.length : 0;
    const selectedSize   = state.selectedSessions ? state.selectedSessions.size : 0;
    return { totalSessions, visibleDoneLen, selectedSize,
             visibleDoneSubset: visibleDoneLen <= totalSessions };
  });

  await saveLog('live-check', JSON.stringify(liveCheck));
  if (!liveCheck.visibleDoneSubset) {
    throw new Error(`_visibleDone (${liveCheck.visibleDoneLen}) > sessions (${liveCheck.totalSessions}) — filtering broken`);
  }

  await saveLog('result', 'selectAllInactive confirmed to operate on _visibleDone only');
});
