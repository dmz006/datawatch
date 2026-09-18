// TS-746 — PWA filter chip change clears state.selectedSessions
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);
  await navigateTo(page, 'sessions');
  await page.waitForTimeout(1500);
  await screenshot(page, '01-sessions-loaded');

  const hasState = await page.evaluate(() => typeof state !== 'undefined' && 'selectedSessions' in state);
  if (!hasState) throw new Error('state.selectedSessions not found in PWA global state');

  const sizeBefore = await page.evaluate(() => {
    if (typeof selectAllInactive === 'function') selectAllInactive();
    return state.selectedSessions instanceof Set ? state.selectedSessions.size : (state.selectedSessions?.size ?? 0);
  });
  await saveLog('before-filter', `selectedSessions size before chip change: ${sizeBefore}`);

  const sizeAfter = await page.evaluate(() => {
    if (typeof setSessionStateChip !== 'function') return null;
    setSessionStateChip('running');
    return state.selectedSessions instanceof Set ? state.selectedSessions.size : (state.selectedSessions?.size ?? -1);
  });

  await screenshot(page, '02-after-chip-change');
  await saveLog('after-chip', `selectedSessions size after setSessionStateChip: ${sizeAfter}`);

  if (sizeAfter === null) throw new Error('setSessionStateChip function not found — filter-clear logic missing');
  if (sizeAfter !== 0) throw new Error(`Expected selectedSessions size=0 after chip change, got ${sizeAfter}`);

  await saveLog('result', `PASS: selectedSessions cleared (size=0) after setSessionStateChip (was ${sizeBefore})`);
});
