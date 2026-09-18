// TS-760 — PWA chip "(no change since last refresh)" renders on no_change response
import { runStory, connectToPWA, navigateTo, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // Intercept the current-status API to return no_change: true
  await page.route('**/api/sessions/*/current-status', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ no_change: true }),
    });
  });

  await navigateTo(page, 'sessions');
  await page.waitForTimeout(1500);
  await screenshot(page, '01-sessions');

  // Inject a fake session into state so fetchCurrentStatus has something to work on.
  const fakeId = 'fake-session-ts760';
  await page.evaluate((id) => {
    // Add a minimal session to the state so the sessions view renders it.
    const fakeSession = {
      id, full_id: id, state: 'completed',
      llm_backend: 'shell', created_at: new Date().toISOString(),
    };
    if (!state.sessions) state.sessions = [];
    state.sessions.unshift(fakeSession);
    if (!state._visibleDone) state._visibleDone = [];
    state._visibleDone.unshift(fakeSession);
    state.selectMode = false;
    renderSessionsView();
  }, fakeId);

  await page.waitForTimeout(500);

  // Call fetchCurrentStatus on the fake session — API will return no_change: true.
  await page.evaluate((id) => {
    if (typeof fetchCurrentStatus === 'function') {
      fetchCurrentStatus(id);
    }
  }, fakeId);

  await page.waitForTimeout(1000);
  await screenshot(page, '02-after-fetch');

  // Verify state.currentStatus has the no_change chip text.
  const chipText = await page.evaluate((id) => {
    const cs = state.currentStatus && state.currentStatus[id];
    return cs ? cs.text : null;
  }, fakeId);

  await saveLog('chip-text', chipText || 'null');

  if (!chipText) {
    throw new Error('state.currentStatus not set after fetchCurrentStatus with no_change response');
  }
  if (!chipText.includes('no change')) {
    throw new Error(`Expected "no change" in chip text, got: "${chipText}"`);
  }
});
