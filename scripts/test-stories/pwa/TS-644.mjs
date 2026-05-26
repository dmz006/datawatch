// TS-644 — Autonomous/Automata view renders
import { runStory, connectToPWA, navigateTo, assertVisible, screenshot } from './lib.mjs';

await runStory(async (page) => {
  await connectToPWA(page);

  // Detect which nav attribute name is used for the autonomous/automata view
  const view = await page.evaluate(() => {
    const el = document.querySelector('[data-view="autonomous"], [data-view="automata"]');
    return el?.getAttribute('data-view') || 'autonomous';
  });

  await navigateTo(page, view);

  await assertVisible(page, '[id*="prd"], [id*="automata"], [id*="autonomous"], #view', 'autonomous view');

  await screenshot(page, '01-autonomous-view');
});
