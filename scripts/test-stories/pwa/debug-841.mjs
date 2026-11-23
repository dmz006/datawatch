#!/usr/bin/env node
// debug-841 — replicate TS-841's opening steps and dump page state.
import { launchBrowser, env } from './lib.mjs';

const log = (m) => console.log(`[${((Date.now() - T0) / 1000).toFixed(1)}s] ${m}`);
const T0 = Date.now();

const browser = await launchBrowser();
try {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 800 } });
  await context.route('**/sw.js', (route) => route.abort());
  const page = await context.newPage();
  const errors = [];
  page.on('console', (m) => { if (m.type() === 'error') { errors.push(m.text()); log('CONSOLE-ERR: ' + m.text().slice(0, 200)); } });
  page.on('pageerror', (e) => { errors.push(e.message); log('PAGE-ERR: ' + e.message.slice(0, 200)); });
  page.on('requestfailed', (r) => log(`REQFAIL: ${r.url().slice(0, 120)} ${r.failure()?.errorText || ''}`));

  log('goto ' + env.http);
  await page.goto(env.http, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await page.evaluate(() => localStorage.setItem('cs_settings_tab', 'automata'));
  await page.evaluate((t) => localStorage.setItem('cs_token', t), env.token);
  await page.reload({ waitUntil: 'domcontentloaded', timeout: 60000 });
  log('reload ok, waiting for splash...');
  const splash = await page.waitForFunction(
    () => {
      const el = document.getElementById('splash');
      return !el || el.style.display === 'none' || el.classList.contains('hidden') ||
             window.getComputedStyle(el).display === 'none' || window.getComputedStyle(el).opacity === '0';
    }, null, { timeout: 30000 }).catch((e) => 'splash-wait-failed: ' + e.message.slice(0, 100));
  log('splash: ' + splash);

  const state = await page.evaluate(() => ({
    url: location.href,
    title: document.title,
    bodyLen: document.body ? document.body.innerHTML.length : -1,
    navButtons: Array.from(document.querySelectorAll('.nav-btn')).map(b => b.dataset.view + (b.classList.contains('active') ? '*' : '')),
    settingsBtn: !!document.querySelector('[data-view="settings"]'),
    viewEl: document.getElementById('view') ? document.getElementById('view').className : null,
    splashEl: (() => { const el = document.getElementById('splash'); return el ? { display: el.style.display, cls: el.className, computed: getComputedStyle(el).display } : 'absent'; })(),
    currentView: window.__dwCurrentView || null,
    navigateFn: typeof window.navigate,
    ws: (window.__wsState) || null,
  }));
  log('STATE: ' + JSON.stringify(state, null, 1));

  // Probe the settings button's actionability.
  const btn = await page.evaluate(() => {
    const el = document.querySelector('[data-view="settings"]');
    if (!el) return { present: false };
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    return { present: true, rect: { x: r.x, y: r.y, w: r.width, h: r.height }, offsetParent: el.offsetParent ? true : false, display: cs.display, visibility: cs.visibility, pointerEvents: cs.pointerEvents, innerW: window.innerWidth, innerH: window.innerHeight, disabled: el.disabled };
  });
  log('BTN: ' + JSON.stringify(btn));

  const hit = await page.evaluate(() => {
    const el = document.querySelector('[data-view="settings"]');
    const r = el.getBoundingClientRect();
    const t = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return { at: t ? (t.tagName + '#' + t.id + '.' + t.className) : null, isSelf: t === el || el.contains(t) };
  });
  log('HIT: ' + JSON.stringify(hit));

  try {
    await page.click('[data-view="settings"]', { timeout: 8000 });
    log('CLICK ok');
    await page.waitForFunction(() => document.querySelector('[data-view="settings"]')?.classList.contains('active'), null, { timeout: 5000 });
    log('ACTIVE ok');
  } catch (e) {
    log('CLICK-FAIL: ' + e.message.split('\n').slice(0, 8).join(' | '));
  }

  const shot = 'saved-to-' + '/tmp/dw-pwa-debug/dbg841.png';
  await page.screenshot({ path: '/tmp/dw-pwa-debug/dbg841.png' }).catch(() => {});
  log('screenshot: ' + shot);
  await page.close();
} finally {
  await browser.close().catch(() => {});
}
log('done');
