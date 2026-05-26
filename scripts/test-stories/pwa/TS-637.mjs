// TS-637 — Sessions view renders list
import { runStory, connectToPWA, navigateTo, assertVisible, assertCount, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'sessions');
  await assertVisible(page, '#sessionsList, [id*="session"], .session-item, .session-row', 'sessions container');
  await assertCount(page, '#nav [data-view], nav [data-view]', 4);

  await screenshot(page, '01-sessions-view');
});
