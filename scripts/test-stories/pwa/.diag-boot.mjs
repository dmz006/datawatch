import { launchBrowser } from './lib.mjs';
import { installSeed, clearSeed } from './mobile-fit-seeds.mjs';
const BASE = 'http://127.0.0.1:18481';
const TOKEN = 'dw-diag-token';
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
const browser = await launchBrowser();
const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 });
await ctx.addInitScript(([tok]) => { try { localStorage.setItem('cs_theme','dark'); localStorage.setItem('cs_token', tok); } catch {} }, [TOKEN]);
const page = await ctx.newPage();
const errs = [];
page.on('pageerror', e => errs.push('pageerror: ' + e.message));
page.on('console', m => { if (m.type() === 'error') errs.push('console: ' + m.text().slice(0,200)); });
await page.goto(BASE, { waitUntil: 'domcontentloaded', timeout: 20000 });
await sleep(2000);
console.log('title:', await page.title().catch(() => '?'));
console.log('body len:', await page.evaluate(() => document.body ? document.body.innerHTML.length : -1).catch(e => 'ERR ' + e.message));
console.log('window keys sample:', await page.evaluate(() => {
  const out = [];
  for (const k of ['state','navigate','renderObserverView','apiFetch']) {
    try { out.push(k + '=' + typeof window[k]); } catch (e) { out.push(k + ' THROWS ' + e.message); }
  }
  return out;
}).catch(e => 'ERR ' + e.message));
console.log('errors:', errs.slice(0,8));
await browser.close();
