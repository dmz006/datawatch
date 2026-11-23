// debug-802.mjs — step-by-step instrumentation of TS-802's first tab.
import { chromium } from 'playwright';
import { env } from './lib.mjs';
import { coldPage, throttle, openPWA, ensurePerfHelpers, viewState, TABS } from './perf-lib.mjs';

const t0 = Date.now();
const log = (m) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${m}`);

const browser = await chromium.launch({
  executablePath: process.env.DW_CHROME_PATH || '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage', '--disable-gpu', '--ignore-certificate-errors', ...(process.env.CHROME_LOG ? ['--enable-logging=stderr', '--v=1', '--log-file=/tmp/dw-pwa-debug/chrome.log'] : [])],
});
try {
  log('browser launched');
  const { context, page } = await coldPage(browser);
  browser.on('page', (p) => log('BROWSER-OPENED-NEW-PAGE ' + p.url()));
  page.on('crash', () => log('PAGE CRASHED'));
  page.on('close', () => log('PAGE CLOSED'));
  await context.addInitScript(() => {
    const mark = (op) => { try { console.error('NAV-TRIGGER ' + op + ' STACK:' + new Error().stack); } catch (_) {}
      try { localStorage.setItem('nav_trigger_log', (localStorage.getItem('nav_trigger_log') || '') + '\n' + op + ' @' + Date.now()); } catch (_) {}
    };
    try {
      const r0 = location.reload.bind(location);
      location.reload = function (...a) { mark('location.reload'); return r0(...a); };
      const a0 = location.assign.bind(location);
      location.assign = function (...a) { mark('location.assign ' + a[0]); return a0(...a); };
      const s0 = location.replace.bind(location);
      location.replace = function (...a) { mark('location.replace ' + a[0]); return s0(...a); };
    } catch (e) { try { console.error('NAV-HOOK-ERR ' + e.message); } catch (_) {}
    }
    addEventListener('beforeunload', () => mark('beforeunload-firing'));
    addEventListener('pagehide', () => mark('pagehide-firing'));
  });
  // CDP trace: log every document navigation with its initiator, plus frameStartedLoading.
  const cdp = await context.newCDPSession(page);
  await cdp.send('Network.enable');
  cdp.on('Network.requestWillBeSent', (p) => {
    if (p.type === 'Document' || p.type === 'Fetch' && /\/$/.test(p.request.url)) {
      log('CDP req [' + p.type + '] ' + p.request.url + ' initiator=' + JSON.stringify(p.initiator));
    }
  });
  await cdp.send('Page.enable');
  cdp.on('Page.frameStartedLoading', () => log('CDP frameStartedLoading'));
  cdp.on('Page.frameNavigated', (p) => log('CDP frameNavigated ' + (p.frame && p.frame.url)));
  cdp.on('Page.frameClearedDocument', () => log('CDP frameClearedDocument'));
  cdp.on('Page.navigatedWithinDocument', (p) => log('CDP navigatedWithinDocument ' + p.url));
  const cdp2 = await context.newCDPSession(page);
  await cdp2.send('Runtime.enable');
  cdp2.on('Runtime.executionContextDestroyed', (p) => log('CDP execContextDestroyed ' + JSON.stringify(p)));
  if (process.env.BLOCK_SW === '1') {
    await context.route('**/sw.js*', (r) => r.fulfill({ status: 404, body: 'blocked' }));
    log('SW blocked');
  }
  log('coldPage ok');
  page.on('console', (m) => log('console:' + m.type() + ' ' + m.text().slice(0, 220)));
  page.on('pageerror', (e) => log('pageerror: ' + e.message.slice(0, 200)));
  page.on('framenavigated', (f) => log('FRAME-NAVIGATED: ' + f.url()));
  page.on('request', (r) => log('req ' + r.method() + ' ' + r.url().slice(0, 120) + '  ' + r.resourceType()));
  await throttle(page, 'slow-4g');
  log('throttle ok');
  log('goto...');
  await page.goto(env.http, { waitUntil: 'domcontentloaded', timeout: 20000 });
  log('goto ok, url=' + page.url() + ' title=' + (await page.title().catch(() => '?')));
  await page.evaluate((t) => localStorage.setItem('cs_token', t), env.token);
  log('token set');
  await page.reload({ waitUntil: 'domcontentloaded', timeout: 20000 });
  log('reload ok');
  try {
    await page.waitForFunction(() => typeof window.navigate === 'function', null, { timeout: 30000 });
    log('window.navigate present');
  } catch { log('window.navigate NEVER appeared in 30s'); }
  try {
    await page.waitForFunction(() => {
      const el = document.getElementById('splash');
      return !el || getComputedStyle(el).display === 'none' || getComputedStyle(el).opacity === '0' || el.classList.contains('hidden');
    }, null, { timeout: 30000 });
    log('splash resolved');
  } catch { log('splash STILL showing after 30s'); }
  log('openPWA-equivalent done; now measureTab(sessions)...');
  await ensurePerfHelpers(page);
  const p = page.evaluate(({ view }) => new Promise((resolve) => {
    const el = document.getElementById('view');
    const t0 = performance.now();
    const res = { states: [], timedOut: false };
    if (!el) { resolve({ noViewEl: true }); return; }
    el.innerHTML = '';
    let iv = setInterval(() => {
      const s = window.__dwPerf.classify();
      if (res.states[res.states.length - 1] !== s) res.states.push(s);
      if (performance.now() - t0 >= 45000) { clearInterval(iv); res.timedOut = true; resolve(res); }
    }, 100);
    window.__dwPerf.openTab(view, null, null);
  }), { view: TABS[0].view });
  log('in-page measure done: ' + JSON.stringify(p));
  log('viewState: ' + (await viewState(page).catch((e) => 'ERR ' + e.message)));
  log('final screenshot...');
  await page.screenshot({ path: '/tmp/dw-pwa-debug/dbg.png', timeout: 15000 });
  log('screenshot ok');
} catch (e) {
  log('EXCEPTION: ' + (e.message || e).slice(0, 400));
  try { await page.screenshot({ path: '/tmp/dw-pwa-debug/dbg-fail.png', timeout: 5000 }); log('failure screenshot ok'); } catch { log('failure screenshot failed'); }
} finally {
  await browser.close().catch(() => {});
}
log('done, exiting');
