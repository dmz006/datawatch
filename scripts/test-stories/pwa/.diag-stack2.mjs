import { launchBrowser } from './lib.mjs';
import { installSeed, clearSeed } from './mobile-fit-seeds.mjs';
const BASE = 'http://127.0.0.1:18481';
const TOKEN = 'dw-diag-token';
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
const browser = await launchBrowser();
const ctx = await browser.newContext({ viewport: { width: 320, height: 568 }, deviceScaleFactor: 1 });
await ctx.addInitScript(([tok]) => { try { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', tok); } catch {} }, [TOKEN]);
const page = await ctx.newPage();
const errs = [];
page.on('pageerror', e => errs.push('pageerror: ' + e.message));
const statsReq = [];
page.on('request', (req) => { const u = req.url(); if (u.includes('/api/stats')) statsReq.push({ t: Date.now(), u }); });
await page.goto(BASE, { waitUntil: 'domcontentloaded', timeout: 20000 }).catch(e => console.log('goto err', e.message));
await sleep(1000);
try { await clearSeed(page); } catch {}
await installSeed(page, 'ten-peers');
await (async () => {
  for (let a = 1; a <= 3; a++) {
    const ok = await page.evaluate(() => {
      try { if (typeof window.navigate !== 'function') return false; window.navigate('observer'); } catch { return false; }
      return !!document.getElementById('perSystemGrid');
    }).catch(e => console.log('bootVerify err', e.message));
    console.log('bootVerify attempt', a, ok);
    if (ok) break;
    await page.reload({ waitUntil: 'domcontentloaded' }).catch(()=>{});
    await sleep(800);
    try { await clearSeed(page); await installSeed(page, 'ten-peers'); } catch {}
  }
})();
await sleep(1500);
console.log('has perSystemGrid:', await page.evaluate(() => !!document.getElementById('perSystemGrid')));
console.log('view html len:', await page.evaluate(() => (document.getElementById('view')||{}).innerHTML?.length));
const t0 = Date.now();
await sleep(17000);
const R1 = statsReq.filter(x => x.t >= t0).length;
console.log('R1 (17s) =', R1);
for (let i = 0; i < 10; i++) {
  await page.evaluate(() => { window.navigate('sessions'); });
  await sleep(150);
  await page.evaluate(() => { window.navigate('observer'); });
  await sleep(2000);
}
console.log('after rerenders, has perSystemGrid:', await page.evaluate(() => !!document.getElementById('perSystemGrid')));
const t1 = Date.now();
await sleep(17000);
const w2 = statsReq.filter(x => x.t > t1);
console.log('R2 window =', w2.length);
const c = {}; for (const x of w2) c[x.u] = (c[x.u]||0)+1; console.log('urls:', JSON.stringify(c));
console.log('page errors:', errs.slice(0,6));
await browser.close();
