#!/usr/bin/env node
// debug-841b — EXACT TS-841 opening flow (lib connectToPWA) + click diagnostics.
import { launchBrowser, connectToPWA, env } from './lib.mjs';

const log = (m) => console.log(`[${((Date.now() - T0) / 1000).toFixed(1)}s] ${m}`);
const T0 = Date.now();

const browser = await launchBrowser();
try {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 800 } });
  await context.route('**/sw.js', (route) => route.abort());
  const page = await context.newPage();
  page.on('console', (m) => { log('CONSOLE[' + m.type() + ']: ' + m.text().slice(0, 250)); });
  page.on('pageerror', (e) => log('PAGE-ERR: ' + e.message.slice(0, 200)));
  page.on('framenavigated', (f) => { if (f === page.mainFrame()) log('NAVIGATED: ' + f.url()); });

  // === exact TS-841 sequence ===
  await page.goto(env.http, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'automata'));
  await connectToPWA(page);
  log('connectToPWA done');

  // === diagnostics ===
  for (let i = 0; i < 8; i++) {
    const st = await page.evaluate(() => {
      const el = document.querySelector('[data-view="settings"]');
      const splash = document.getElementById('splash');
      let hit = null;
      if (el) {
        const r = el.getBoundingClientRect();
        const t = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
        hit = { rect: [r.x, r.y, r.width, r.height], top: t ? t.tagName + '#' + t.id : null, self: t === el || el.contains(t) };
      }
      return { url: location.href, elPresent: !!el, hit, splash: splash ? { cls: splash.className, computed: getComputedStyle(splash).display, opacity: getComputedStyle(splash).opacity } : null, bodyLen: document.body.innerHTML.length };
    });
    log('T' + i + ': ' + JSON.stringify(st));
    try {
      await page.click('[data-view="settings"]', { timeout: 1000 });
      log('CLICK OK at T' + i);
      break;
    } catch (e) {
      log('click wait T' + i + ' failed: ' + e.message.split('\n')[0]);
    }
  }
  await page.close();
} finally {
  await browser.close().catch(() => {});
}
log('done');
