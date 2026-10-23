import { launchBrowser, env } from './lib.mjs';
import { installSeed } from './mobile-fit-seeds.mjs';
const b = await launchBrowser();
const ctx = await b.newContext({ viewport: { width: 320, height: 568 }, ignoreHTTPSErrors: true });
await ctx.addInitScript(([t]) => { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', t); }, [env.token]);
const p = await ctx.newPage();
await p.goto(env.http, { waitUntil: 'domcontentloaded', timeout: 20000 });
await new Promise(r => setTimeout(r, 500));
await p.evaluate((t) => localStorage.setItem('cs_token', t), env.token);
await installSeed(p, 'ten-peers');
await p.evaluate(() => window.navigate('observer'));
await new Promise(r => setTimeout(r, 2000));

// Expand diagnostics section
const before = await p.evaluate(() => {
  const el = document.querySelector('[onclick*="channelDiagBody"]');
  if (!el) return 'no toggle';
  el.click(); return 'clicked: ' + el.tagName;
});
await new Promise(r => setTimeout(r, 1000));

const dump1 = await p.evaluate(() => {
  const row = document.querySelector('#memoryBrowserList .settings-row');
  if (!row) return 'no row';
  const all = row.querySelectorAll('span');
  return Array.from(all).slice(0, 8).map(s => {
    const r = s.getBoundingClientRect(); const cs = getComputedStyle(s);
    return { txt: s.textContent.slice(0,20), x:Math.round(r.x), y:Math.round(r.y), w:+r.width.toFixed(1), h:+r.height.toFixed(1),
      display: cs.display, position: cs.position, float: cs.cssFloat, overflow: cs.overflow };
  });
});

const dump2 = await p.evaluate(() => {
  const st = document.getElementById('channelDiagStatus');
  if (!st) return 'no status';
  const cs = getComputedStyle(st); const r0 = st.getBoundingClientRect();
  const divs = Array.from(st.children).map(d => {
    const r = d.getBoundingClientRect(); const c = getComputedStyle(d);
    return { txt: d.textContent.slice(0,40), x:Math.round(r.x), y:Math.round(r.y), w:+r.width.toFixed(1), h:+r.height.toFixed(1),
      display: c.display, position: c.position, flexWrap: c.flexWrap, whiteSpace: c.whiteSpace };
  });
  const spans = Array.from(st.querySelectorAll('span')).map(s => {
    const r = s.getBoundingClientRect(); const c = getComputedStyle(s);
    return { txt: s.textContent.slice(0,24), x:Math.round(r.x), y:Math.round(r.y), w:+r.width.toFixed(1), h:+r.height.toFixed(1),
      display: c.display, position: c.position, maxWidth: c.maxWidth, whiteSpace: c.whiteSpace };
  });
  return { status: { display: cs.display, x:Math.round(r0.x), y:Math.round(r0.y), w:+r0.width.toFixed(1), h:+r0.height.toFixed(1) }, divs, spans };
});
console.log('MEMROW:', JSON.stringify(dump1, null, 1));
console.log('DIAG:', JSON.stringify(dump2, null, 1));
// also toast: find .toast-error-close
const close = await p.evaluate(() => { const e = document.querySelector('.toast-error-close'); if (!e) return null; const r = e.getBoundingClientRect(); const c = getComputedStyle(e); return {x:Math.round(r.x),y:Math.round(r.y),w:r.width,h:r.height, padding:c.padding, fontSize:c.fontSize}; });
console.log('TOAST_CLOSE:', JSON.stringify(close));
await b.close();
