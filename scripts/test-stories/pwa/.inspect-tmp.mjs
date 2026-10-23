import { launchBrowser } from './lib.mjs';
import { installSeed } from './mobile-fit-seeds.mjs';

const base = process.env.TEST_HTTP || 'http://127.0.0.1:18080';
const tok  = process.env.TEST_TOKEN || 'dw-test-token-12345';
const b = await launchBrowser();
const ctx = await b.newContext({ viewport: { width: 320, height: 568 }, ignoreHTTPSErrors: true });
await ctx.addInitScript(([t]) => { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', t); }, [tok]);
const p = await ctx.newPage();
await p.goto(base, { waitUntil: 'domcontentloaded', timeout: 20000 });
await new Promise(r => setTimeout(r, 500));
await p.evaluate((t) => localStorage.setItem('cs_token', t), tok);
await installSeed(p, 'ten-peers');
await p.evaluate(() => window.navigate('observer'));
await new Promise(r => setTimeout(r, 2500));

const sel = [
  '#peerStaleBadge',
  '#nav > button.nav-btn:nth-child(7) > span.nav-icon',
  '.toast-error-close',
  '#headerHelpLink', '#headerAlertPill', '#headerFullscreenBtn',
  '#memoryBrowserList > .settings-row',
  '#auditEntries > div:nth-child(1)',
  '#channelDiagStatus',
  '#observerPeersPanel > div:nth-child(4)',
  '.settings-section-title.settings-section-toggle',
  '#memoryQuickAddInput',
];
const dump = await p.evaluate((sels) => {
  const out = [];
  for (const s of sels) {
    const el = document.querySelector(s);
    if (!el) { out.push({ s, missing: true }); continue; }
    const cs = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    out.push({
      s,
      tag: el.tagName.toLowerCase(),
      cls: el.className && el.className.toString().slice(0, 120),
      style: el.getAttribute('style'),
      rect: { x: Math.round(r.x), y: Math.round(r.y), w: +r.width.toFixed(1), h: +r.height.toFixed(1) },
      scrollW: el.scrollWidth, clientW: el.clientWidth, scrollH: el.scrollHeight, clientH: el.clientHeight,
      display: cs.display, whiteSpace: cs.whiteSpace, overflow: cs.overflow, fontSize: cs.fontSize,
      padding: cs.padding, maxH: cs.maxHeight, width: cs.width, position: cs.position,
      html: el.outerHTML.slice(0, 400),
    });
  }
  return out;
}, sel);
console.log(JSON.stringify(dump, null, 1));
await b.close();
