#!/usr/bin/env node
/**
 * Settings nav clear of peer alert — at a 390 px viewport nothing (such as a
 * peer-stale badge or connectivity alert overlay) sits over the Settings nav
 * button's label, except the button itself and its descendants.
 * tags: surface:pwa group:nav
 *
 * Runs against the runner's sandbox daemon (never the production daemon).
 */
import { runStory, connectToPWA, screenshot, saveLog } from './lib.mjs';

await runStory(async (page) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await connectToPWA(page);
  await page.waitForTimeout(1500);
  await screenshot(page, '01-nav-390');

  const btn = page.locator('[data-view="settings"]').first();
  await btn.waitFor({ state: 'visible', timeout: 10000 });
  const label = btn.locator('.nav-label').first();
  await label.waitFor({ state: 'visible', timeout: 5000 });

  const result = await page.evaluate(() => {
    const b = document.querySelector('[data-view="settings"]');
    const l = b && b.querySelector('.nav-label');
    if (!b || !l) return { error: 'settings nav button or nav-label missing' };
    const r = l.getBoundingClientRect();
    const x = r.left + r.width / 2;
    const y = r.top + r.height / 2;
    const describe = (e) => `${e.tagName.toLowerCase()}${e.id ? '#' + e.id : ''}${e.className && typeof e.className === 'string' ? '.' + e.className.trim().split(/\s+/).join('.') : ''}`;
    const stack = document.elementsFromPoint(x, y);
    const intruders = stack.filter((e) => e !== b && !b.contains(e)).map(describe);
    // Only elements stacked above the button matter; ones beneath are fine.
    const above = [];
    for (const e of stack) {
      if (e === b || b.contains(e)) break;
      above.push(describe(e));
    }
    return { x, y, width: window.innerWidth, above, intruders };
  });

  await saveLog('hit-test', JSON.stringify(result));
  if (result.error) throw new Error(result.error);
  if (result.width !== 390) throw new Error(`viewport width ${result.width}, expected 390`);
  if (result.above.length > 0) {
    throw new Error(`element(s) over the Settings nav label centre (${result.x},${result.y}): ${result.above.join(', ')}`);
  }
  await saveLog('result', 'PASS: nothing but the Settings nav button covers its label at 390 px');
});
