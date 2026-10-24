// TS-637 — Sessions view renders list or empty state; the main nav has no Automation button
import { runStory, connectToPWA, navigateTo, assertVisible, assertCount, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  await navigateTo(page, 'sessions');
  // When sessions exist: .session-list; when empty: .empty-state or .sessions-watermark
  await assertVisible(page, '.session-list, .sessions-watermark, .empty-state, .view-content', 'sessions container');
  // Sessions, Alerts, Observer and Settings always exist; Automata and Dashboard
  // appear when their subsystems are on. Automation is a sub-tab of Automata,
  // so the main nav never carries a button of its own for it.
  await assertCount(page, '#nav [data-view], nav [data-view]', 4);
  const navViews = await page.evaluate(() => [...document.querySelectorAll('#nav [data-view]')].map((el) => el.dataset.view));
  for (const v of ['sessions', 'alerts', 'observer', 'settings']) {
    if (!navViews.includes(v)) throw new Error(`main nav has no ${v} button (found: ${navViews.join(', ')})`);
  }
  if (navViews.includes('automation')) throw new Error('main nav still has an Automation button; it is a sub-tab of Automata');

  await screenshot(page, '01-sessions-view');
});
