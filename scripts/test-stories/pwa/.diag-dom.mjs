// Live DOM inspector: runs the mobile-fit checks and dumps details + relevant markup.
import { launchBrowser } from './lib.mjs';
import { installSeed, clearSeed } from './mobile-fit-seeds.mjs';
import { collectFindings } from './mobile-fit-lib.mjs';

const BASE = process.env.DIAG_HTTP || 'http://127.0.0.1:18481';
const TOKEN = 'dw-diag-token';
const [state, W, H, ZOOM] = [process.argv[2] || 'three-peers', Number(process.argv[3] || 360), Number(process.argv[4] || 740), Number(process.argv[5] || 2)];
const sleep = (ms) => new Promise(r => setTimeout(r, ms));

const browser = await launchBrowser();
const ctx = await browser.newContext({ viewport: { width: W, height: H }, deviceScaleFactor: ZOOM, ignoreHTTPSErrors: true });
await ctx.addInitScript(([tok]) => { try { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', tok); } catch {} }, [TOKEN]);
const page = await ctx.newPage();
page.on('pageerror', e => console.log('PAGE ERR:', e.message.slice(0,200)));
try { await page.goto(BASE, { waitUntil: 'domcontentloaded', timeout: 20000 }); } catch {}
await sleep(800);
try { await clearSeed(page); } catch {}
await installSeed(page, state);
for (let a = 1; a <= 5; a++) {
  const r = await page.evaluate(() => {
    if (typeof window.navigate !== 'function') return 'no navigate fn yet';
    try { window.navigate('observer'); } catch (e) { return 'nav threw ' + e.message; }
    return document.getElementById('perSystemGrid') ? 'ok' : 'no grid';
  }).catch(e => 'threw ' + e.message);
  if (r === 'ok') { console.log('boot: ok (attempt ' + a + ')'); break; }
  if (r === 'no navigate fn yet') await sleep(1000);
  if (r.startsWith('nav threw')) console.log('boot attempt', a, r); 
  if (a < 5) { await page.reload({ waitUntil: 'domcontentloaded' }).catch(()=>{}); await sleep(1000); try { await clearSeed(page); await installSeed(page, state); } catch {}
  }
}
const bootState = await page.evaluate(() => ({ nav: typeof window.navigate, grid: !!document.getElementById('perSystemGrid'), bodyLen: document.body.innerHTML.length }));
console.log('boot final:', JSON.stringify(bootState), 'viewport:', await page.evaluate(() => window.innerWidth + 'x' + window.innerHeight + ' dpr' + window.devicePixelRatio));
await sleep(2500);

const findings = await collectFindings(page, { maxPerCheck: 2000 });
const seen = new Map();
for (const f of findings) {
  const k = f.check + '|' + f.selector;
  if (!seen.has(k)) seen.set(k, []);
  const arr = seen.get(k);
  const dk = f.detail + '|' + (f.text || '');
  if (!arr.some(x => x.d === dk)) arr.push({ d: f.detail, t: f.text || null, r: f.rect });
}
for (const [k, arr] of seen) {
  console.log('\n### ' + k + '  x' + arr.length + ' detail-shapes');
  for (const a of arr.slice(0, 4)) console.log('   ', a.r.x + '@' + a.r.y, a.r.width + 'x' + a.r.height, '—', a.d, a.t ? 'text="' + a.t + '"' : '');
}

// Dump key markup
const dump = await page.evaluate(() => {
  const out = [];
  const pick = (label, el) => { if (!el) { out.push(label + ': (not found)'); return; }
    out.push(label + ': ' + (el.outerHTML || '').replace(/\s+/g, ' ').slice(0, 700)); };
  const q = (s) => document.querySelector(s);
  pick('.docs-link first', q('.docs-link'));
  pick('.docs-link parent chain', q('.docs-link')?.closest('a')?.parentElement?.parentElement ? q('.docs-link').parentElement.parentElement.outerHTML.replace(/\s+/g,' ').slice(0,400) : null);
  pick('#channelDiagStatus', q('#channelDiagStatus'));
  pick('#memoryBrowserList (first 1200 ch)', q('#memoryBrowserList'));
  pick('#memoryQuickAddInput parent', q('#memoryQuickAddInput')?.closest('div,form')?.outerHTML.replace(/\s+/g,' ').slice(0,500));
  pick('#peerStaleBadge chain', (() => { const el = q('#peerStaleBadge'); let s=''; let n=el; for(let i=0;i<4 && n;i++){ s += '\n  ['+i+'] '+ (n.outerHTML||'').replace(/\s+/g,' ').slice(0,300); n = n.parentElement; } return s; })());
  pick('.btn-icon first + parent', q('.btn-icon') ? q('.btn-icon').outerHTML + ' || parent: ' + (q('.btn-icon').parentElement.outerHTML||'').replace(/\s+/g,' ').slice(0,300) : null);
  pick('.settings-section-title', q('.settings-section-title'));
  pick('.toast-error', q('.toast-error') || q('[class*=toast]'));
  const memRow = q('#memoryBrowserList > div');
  pick('#memoryBrowserList row', memRow ? memRow.outerHTML.replace(/\s+/g,' ').slice(0,500) : null);
  return out.join('\n');
}).catch(e => 'DUMP ERR ' + e.message);
console.log('\n\n===== MARKUP =====\n' + dump);
await browser.close();
