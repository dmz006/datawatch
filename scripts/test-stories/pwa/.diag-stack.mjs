// Temp diagnostic: find what stacks /api/stats polling after Observer re-renders.
import { launchBrowser } from './lib.mjs';
import { installSeed, clearSeed } from './mobile-fit-seeds.mjs';
const BASE = process.env.DIAG_HTTP || 'http://127.0.0.1:18481';
const TOKEN = 'dw-diag-token';
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
const browser = await launchBrowser();
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 });
await ctx.addInitScript(([tok]) => { try { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', tok); } catch {} }, [TOKEN]);
await ctx.addInitScript(() => {
  const live = new Set();
  window.__liveIntervals = live;
  const origSet = window.setInterval.bind(window);
  const origClear = window.clearInterval.bind(window);
  window.__intervalLog = [];
  window.setInterval = (...args) => {
    const id = origSet(...args);
    if (id !== undefined && id !== null) {
      live.add(id);
      try {
        const fn = args[0];
        window.__intervalLog.push({ at: Date.now(), period: args[1], src: (fn && fn.toString ? fn.toString().slice(0,160).replace(/\n/g,' ') : String(fn)) });
      } catch {}
    }
    return id;
  };
  window.clearInterval = (id) => { live.delete(id); return origClear(id); };
});
const page = await ctx.newPage();
const urls = [];
page.on('request', (req) => { const u = req.url(); if (u.includes('/api/stats')) urls.push({ t: Date.now(), u }); });
await page.goto(BASE, { waitUntil: 'domcontentloaded', timeout: 20000 });
await sleep(800);
try { await clearSeed(page); } catch {}
await installSeed(page, 'ten-peers');
await page.evaluate(() => { window.navigate('observer'); });
await sleep(4000);
const n1 = await page.evaluate(() => window.__liveIntervals.size);
const t0 = Date.now();
await sleep(9000);
const win1 = urls.filter(x => x.t >= t0 && x.t <= Date.now()).length;
console.log('LIVE N1 =', n1, ' win1 /api/stats =', win1);
for (let i = 0; i < 10; i++) {
  await page.evaluate(() => { window.navigate('sessions'); });
  await sleep(200);
  await page.evaluate(() => { window.navigate('observer'); });
  await sleep(1200);
}
const n2 = await page.evaluate(() => window.__liveIntervals.size);
console.log('LIVE N2 =', n2);
const t1 = Date.now();
await sleep(9000);
const win = urls.filter(x => x.t > t1);
console.log('win2 /api/stats =', win.length);
const counts = {};
for (const x of win) { counts[x.u] = (counts[x.u]||0)+1; }
console.log(JSON.stringify(counts, null, 1));
const ivs = await page.evaluate(() => window.__intervalLog.map(l => ({ at: l.at, period: l.period, src: l.src })));
// show intervals created in the last 15s (i.e., during re-render loop / after)
const recent = ivs.filter(l => (Date.now() - l.at) < 20000);
console.log('recent intervals:', recent.length);
for (const r of recent.slice(-10)) console.log('  ', r.period, r.src);
// Also: how many LIVE intervals fire fetch of /api/stats? check each live interval's source is impossible after the fact; instead, log current state flags.
console.log('state flags:', await page.evaluate(() => ({
  activeView: window.state?.activeView,
  _statsTabPoll: !!window.state?._statsTabPoll,
  _statusTabPoll: !!window.state?._statusTabPoll,
  _sessionStatsInterval: !!window._sessionStatsInterval,
  activeSession: window.state?.activeSession,
  activeOutputTab: window.state?.activeOutputTab,
})));
await browser.close();
