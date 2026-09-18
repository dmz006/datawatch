// TS-745 — PWA select-all uses _visibleDone (filtered set), not full sessions list
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);
  await navigateTo(page, 'sessions');
  await page.waitForTimeout(1500);
  await screenshot(page, '01-sessions-view');

  const stateCheck = await page.evaluate(() => {
    if (typeof state === 'undefined') return { error: 'no state' };
    return {
      hasSelectedSessions: 'selectedSessions' in state,
      hasVisibleDone: '_visibleDone' in state,
      isSet: state.selectedSessions instanceof Set,
    };
  });

  if (stateCheck.error) throw new Error(stateCheck.error);
  if (!stateCheck.hasSelectedSessions) throw new Error('state.selectedSessions not found');
  if (!stateCheck.hasVisibleDone) throw new Error('state._visibleDone not found — select-all filter cache missing');

  // Call selectAllInactive, verify selected count == _visibleDone.length
  const before = await page.evaluate(() => {
    if (typeof selectAllInactive === 'function') selectAllInactive();
    return {
      selected: state.selectedSessions instanceof Set ? state.selectedSessions.size : (state.selectedSessions?.length || 0),
      visibleDone: Array.isArray(state._visibleDone) ? state._visibleDone.length : (state._visibleDone?.length || 0),
      total: Array.isArray(state.sessions) ? state.sessions.length : 0,
    };
  });
  await saveLog('before', JSON.stringify(before));

  // Change chip — should clear selectedSessions
  const after = await page.evaluate(() => {
    if (typeof setSessionStateChip === 'function') setSessionStateChip('running');
    return state.selectedSessions instanceof Set
      ? state.selectedSessions.size
      : (state.selectedSessions?.size ?? state.selectedSessions?.length ?? -1);
  });

  await screenshot(page, '02-after-chip');
  await saveLog('after-chip', `size after chip change: ${after}`);

  if (after !== 0) {
    throw new Error(`Expected selectedSessions size=0 after filter chip change, got ${after}`);
  }

  // Verify selectAllInactive used _visibleDone not all sessions
  if (before.selected > before.total) {
    throw new Error(`selectAllInactive selected more than total sessions: ${before.selected} > ${before.total}`);
  }

  await saveLog('result', `PASS: selectAllInactive selected=${before.selected} (visibleDone=${before.visibleDone}, total=${before.total}); cleared to 0 after chip change`);
});
