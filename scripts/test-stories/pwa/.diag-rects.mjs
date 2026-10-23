// Measure rects of overlap-flagged spans.
import { launchBrowser } from './lib.mjs';
import { installSeed, clearSeed } from './mobile-fit-seeds.mjs';

const BASE = process.env.DIAG_HTTP || 'http://127.0.0.1:18591';
const TOKEN = 'dw-diag-token';
const [state, W, H] = [process.argv[2] || 'no-peers', Number(process.argv[3] || 360), Number(process.argv[4] || 740)];
const sleep = (ms) => new Promise(r => setTimeout(r, ms));

const browser = await launchBrowser();
const ctx = await browser.newContext({ viewport: { width: W, height: H }, deviceScaleFactor: 1, ignoreHTTPSErrors: true });
await ctx.addInitScript(([tok]) => { try { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', tok); } catch {} }, [TOKEN]);
const page = await ctx.newPage();
try { await page.goto(BASE, { waitUntil: 'domcontentloaded', timeout: 20000 }); } catch {}
await sleep(800);
try { await clearSeed(page); } catch {}
await installSeed(page, state);
let boot = 'fail';
for (let a = 1; a <= 4 && boot !== 'ok'; a++) {
  boot = await page.evaluate(() => {
    if (typeof window.navigate !== 'function') return 'no-fn';
    try { window.navigate('observer'); } catch (e) { return 'threw'; }
    return document.getElementById('perSystemGrid') ? 'ok' : 'no-grid';
  }).catch(() => 'threw');
  if (boot !== 'ok') { await sleep(1200); await page.reload({ waitUntil: 'domcontentloaded' }).catch(()=>{}); try { await clearSeed(page); await installSeed(page, state); } catch {} }
}
console.log('boot:', boot, W + 'x' + H);
if (state === 'section-collapse-expand') {
  // interaction scenario: collapse then expand the channel diagnostics section per TS
  await page.evaluate(() => { try { window.navigate('observer'); } catch(e){} });
  await sleep(800);
}
await sleep(1500);
const m = await page.evaluate(() => {
  const rr = (el) => { if (!el) return null; const r = el.getBoundingClientRect(); return { x: Math.round(r.left), y: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height) }; };
  const cs = document.getElementById('channelDiagStatus');
  const out = { channelRows: [] };
  if (cs) for (const d of Array.from(cs.children).slice(0, 4)) {
    if (d.tagName !== 'DIV') { out.channelRows.push(d.tagName + ' ' + (d.textContent||'').trim().slice(0,30)); continue; }
    out.channelRows.push({ display: getComputedStyle(d).display, kids: Array.from(d.children).map(c => ({ t: (c.textContent||'').trim().slice(0,25), r: rr(c), disp: getComputedStyle(c).display })) });
  }
  const rows = document.querySelector('#memoryBrowserList');
  if (rows) {
    const row0 = rows.children[0];
    const meta = row0 && row0.children[0];
    const chips = meta && meta.children[1];
    out.chipContainer = chips && { display: getComputedStyle(chips).display, dir: getComputedStyle(chips).direction, text: (chips.textContent||'').trim().slice(0,40), kids: Array.from(chips.children).map(c => ({ t: (c.textContent||'').trim(), r: rr(c), ml: getComputedStyle(c).marginLeft, mrg: getComputedStyle(c).marginRight, disp: getComputedStyle(c).display })) };
    out.metaKids = meta && Array.from(meta.children).map(c => rr(c) && { r: rr(c), t: (c.textContent||'').trim().slice(0,25) });
  }
  return out;
}).catch(e => 'ERR ' + e.message);
console.log(JSON.stringify(m, null, 1));
await browser.close();
