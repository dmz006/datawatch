// TS-644 — Automata tab renders and opens on its Automation sub-tab
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

  // The Automation view is the first sub-tab of this tab, not a tab of its own.
  await assertVisible(page, '.automata-tabs-bar .output-tab[data-tab="automation"]', 'Automation sub-tab button');
  const order = await page.evaluate(() => [...document.querySelectorAll('.automata-tabs-bar .output-tab')].map((b) => b.dataset.tab));
  if (order[0] !== 'automation') throw new Error(`Automation is not the first sub-tab (order: ${order.join(', ')})`);
  if (await page.$('#nav [data-view="automation"]')) throw new Error('main nav still has an Automation button');
  await page.click('.automata-tabs-bar .output-tab[data-tab="automata"]');
  await page.waitForFunction(() => document.querySelector('.automata-tabs-bar .output-tab[data-tab="automata"]')?.classList.contains('active'), null, { timeout: 5000 });
  await page.click('.automata-tabs-bar .output-tab[data-tab="automation"]');
  await page.waitForFunction(() => document.querySelector('.automata-tabs-bar .output-tab[data-tab="automation"]')?.classList.contains('active'), null, { timeout: 5000 });

  await screenshot(page, '01-autonomous-view');
});
