// TS-637 — Sessions view renders list or empty state
import { runStory, connectToPWA, navigateTo, assertVisible, assertCount, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'sessions');
  // When sessions exist: .session-list; when empty: .empty-state or .sessions-watermark
  await assertVisible(page, '.session-list, .sessions-watermark, .empty-state, .view-content', 'sessions container');
  await assertCount(page, '#nav [data-view], nav [data-view]', 4);

  await screenshot(page, '01-sessions-view');
});
