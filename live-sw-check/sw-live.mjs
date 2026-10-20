#!/usr/bin/env node
// Live check for the sw.js activate no-op guard (GATE).
// Drives /usr/bin/google-chrome headless over raw CDP (Node 24 native WebSocket).
//
// Setup invariants (verified separately):
//   * index.html baked version (9.5.31) === daemon /api/health version (9.5.31),
//     so the PWA's own one-shot version-mismatch reload CANNOT fire.
//   * Single Page.navigate into an about:blank tab → exactly one intended
//     document navigation; any further document load is attributable.
//   * Reload attribution: addScriptToEvaluateOnNewDocument hooks
//     location.reload / location.assign / location.href / history.replace.
//     A page-script navigation leaves a stack trace in sessionStorage;
//     a SW-side WebWindow.navigate does not.
//
// Expected by phase (baseline CACHE_NAME = 'datawatch-v7-0-0-alpha-51'):
//   A — fresh profile (install + activate, "no caches" no-op):
//       1 document, final nav entry type 'navigate', no SW-navigate.
//   B — restarted profile (re-activate, current-cache-only no-op):
//       1 document, final nav entry type 'navigate', no SW-navigate.
//   C — daemon rebuilt with bumped CACHE_NAME (real upgrade):
//       2 documents: original 'navigate' + one SW-driven 'reload' after the
//       new SW activates; old cache key purged, caches = [new key].
//
// Usage: node sw-live.mjs <A|B|C>

import { spawn } from 'node:child_process';

const PORT = 18499;
const BASE = 'http://127.0.0.1:18491';
const PROFILE = new URL('./profile', import.meta.url).pathname;
const CHROME = '/usr/bin/google-chrome';
const phase = process.argv[2] || 'A';

const HOOK = '(function(){' +
  'if (window.__dwHook) return; window.__dwHook = 1;' +
  "function rec(v){ try { var s = sessionStorage.getItem('__dw_nav') || '';" +
  " sessionStorage.setItem('__dw_nav', s + JSON.stringify(v) + String.fromCharCode(10)); } catch(e){} }" +
  'try {' +
  'var oR = History.prototype.replace;' +
  'History.prototype.replace = function(){ rec({via:"history.replace", to:String(arguments[0]), stack:(new Error()).stack}); return oR.apply(this, arguments); };' +
  'var la = Location.prototype.assign;' +
  'Location.prototype.assign = function(){ rec({via:"location.assign", to:String(arguments[0]), stack:(new Error()).stack}); return la.apply(this, arguments); };' +
  'var lr = Location.prototype.reload;' +
  'if (lr) Location.prototype.reload = function(){ rec({via:"location.reload", stack:(new Error()).stack}); return lr.apply(this, arguments); };' +
  'var d = Object.getOwnPropertyDescriptor(Location.prototype, "href");' +
  'if (d && d.set && d.get) Object.defineProperty(Location.prototype, "href", { configurable: true,' +
  '  set(v){ rec({via:"location.href=", to:String(v), stack:(new Error()).stack}); return d.set.call(this, v); },' +
  '  get(){ return d.get.call(this); } });' +
  '} catch(e) { rec({hookError: String(e)}); }' +
  '})();';

let chromeProc = null;
async function launchChrome() {
  const p = spawn(CHROME, [
    '--headless=new', '--no-sandbox', '--disable-dev-shm-usage',
    `--user-data-dir=${PROFILE}`, `--remote-debugging-port=${PORT}`,
    '--no-first-run', '--no-default-browser-check', '--disable-extensions',
    'about:blank',
  ], { stdio: ['ignore', 'pipe', 'pipe'] });
  chromeProc = p;
  for (let i = 0; i < 80; i++) {
    await new Promise(r => setTimeout(r, 250));
    try {
      const r = await fetch(`http://127.0.0.1:${PORT}/json/version`);
      if (r.ok) { const v = await r.json(); console.log(`[sw-live] chrome: ${v.Browser}`); return p; }
    } catch {}
  }
  throw new Error('chrome DevTools endpoint never came up');
}

function cdpClient(wsUrl) {
  const ws = new WebSocket(wsUrl);
  let idc = 0; const pending = new Map(); const handlers = [];
  ws.onmessage = (ev) => {
    let msg; try { msg = JSON.parse(ev.data.toString()); } catch { return; }
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id); pending.delete(msg.id);
      msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
    } else if (msg.method) {
      for (const h of handlers) { try { h(msg.method, msg.params || {}); } catch {} }
    }
  };
  const ready = new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = () => rej(new Error('ws error'));
    setTimeout(() => rej(new Error('ws open timeout')), 15000);
  });
  return {
    ws, ready,
    send(method, params = {}) {
      const id = ++idc;
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject });
        ws.send(JSON.stringify({ id, method, params }));
        setTimeout(() => { if (pending.has(id)) { pending.delete(id); reject(new Error(`cdp timeout: ${method}`)); } }, 30000);
      });
    },
    on(fn) { handlers.push(fn); },
    close() { try { ws.close(); } catch {} },
  };
}

async function evalJSON(cdp, expression) {
  const st = await cdp.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
  if (st.exceptionDetails) {
    const ex = st.exceptionDetails.exception || {};
    return { error: String(ex.description || ex.value || st.exceptionDetails.text || '').slice(0, 400) };
  }
  try { return JSON.parse(String(st.result.value)); }
  catch { return { raw: st.result && st.result.type, value: String(st.result.value).slice(0, 300) }; }
}

async function main() {
  await launchChrome();
  const r0 = await fetch(`http://127.0.0.1:${PORT}/json/new?about:blank`, { method: 'PUT' });
  const tab = await r0.json();
  const cdp = cdpClient(tab.webSocketDebuggerUrl);
  await cdp.ready;

  const t0 = Date.now();
  const events = [];
  const evt = (what, extra) => { const e = { t: Date.now() - t0, what }; Object.assign(e, extra || {}); events.push(e);
     if (what === 'load') loadCount++; };
  let loadCount = 0;
  const loadWaiters = [];
  const setLoad = (w) => { loadWaiters.push(w); w(); };
  cdp.on((method, params) => {
    if (method === 'Network.requestWillBeSent' && params.request && params.request.resourceType === 'document')
      evt('document', { url: params.request.url });
    if (method === 'Page.frameNavigated')
      evt('frameNavigated', { url: params.frame && params.frame.url, loader: params.frame && params.frame.loaderId });
    if (method === 'Page.loadEventFired')
      evt('load');
      // notify waiters handled below via loadCount poll
    if (method === 'ServiceWorker.statusUpdated')
      evt('sw.status', { s: (params.statusUpdates || []).map(s => `${s.registrationId.slice(0, 3)}:${s.workerId.slice(0, 3)}:${s.status}`).join(',') });
    if (method === 'ServiceWorker.workerVersionUpdated')
      evt('sw.version', { n: (params.versionUpdates || []).length });
  });

  await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: HOOK });
  await cdp.send('Network.enable');
  await cdp.send('Page.enable');
  await cdp.send('Runtime.enable');
  await cdp.send('ServiceWorker.enable');
  evt('mark: navigate-sent');
  const nav = await cdp.send('Page.navigate', { url: `${BASE}/` });
  if (nav.errorText) console.log(`[sw-live] navigate errorText: ${nav.errorText}`);

  // Wait for initial load complete + SW activated.
  const tStart = Date.now();
  const waitLoadSettled = async (timeoutMs) => {
    const dl = Date.now() + timeoutMs;
    while (Date.now() < dl) {
      const loads = events.filter(e => e.what === 'load').length;
      if (loads >= 1 && Date.now() - t0 > 500) break;
      await new Promise(r => setTimeout(r, 200));
    }
    const loadsNow = events.filter(e => e.what === 'load').length;
    if (loadsNow < 1) throw new Error('no Page.loadEventFired observed');
    // brief pause so post-load microtasks settle
    await new Promise(r => setTimeout(r, 600));
  };
  let state = null;
  while (Date.now() - tStart < 25000) {
    try {
      if (!state) await waitLoadSettled(25000);
      state = await evalJSON(cdp, `(() => { return (async () => {
      const out = { readyState: document.readyState };
      try { const reg = await navigator.serviceWorker.getRegistration();
        out.sw = reg ? { active: !!reg.active, state: reg.active && reg.active.state,
                         script: reg.active && reg.active.scriptURL, status: reg.status } : null;
      } catch (e) { out.swErr = String(e); }
      return JSON.stringify(out); })(); })()`);
      if (state.error) throw new Error(state.error);
    } catch (e) {
      state = null;
      await new Promise(r => setTimeout(r, 300));
      continue;
    }
    if (!state.error && state.readyState === 'complete' && state.sw && state.sw.status === 'activated') break;
    await new Promise(r => setTimeout(r, 250));
  }
  evt('mark: settled');
  console.log(`[sw-live] settled: ${JSON.stringify(state)}`);

  // Settle window: allow SW install/activate/navigate to complete.
  const deadline = tStart + (phase === 'C' ? 30000 : 12000);
  while (Date.now() < deadline) {
    await new Promise(r => setTimeout(r, 400));
    try {
      const si = await evalJSON(cdp, `(() => { try { const n = performance.getEntriesByType('navigation');
        return JSON.stringify({ n: n.length, types: n.map(x => x.type) }); } catch (e) { return JSON.stringify({ err: String(e) }); })()`);
      if (si && !si.err && !si.error) console.log(`[sw-live] settle probe: nav entries=${si.n} types=${JSON.stringify(si.types)} loads=${events.filter(e => e.what === 'load').length}`);
    } catch {}
  }

  // Wait for load quiescence (no new Page.loadEventFired for >1.5s) before reading final state.
  let lastLoads = -1, lastChange = Date.now();
  const dq = Date.now() + 12000;
  while (Date.now() < dq) {
    const l = events.filter(e => e.what === 'load').length;
    if (l !== lastLoads) { lastLoads = l; lastChange = Date.now(); }
    if (lastLoads >= 1 && Date.now() - lastChange > 1500) break;
    await new Promise(r => setTimeout(r, 300));
  }
  await new Promise(r => setTimeout(r, 500));

  const finalInfo = async () => {
    const r = await evalJSON(cdp, `(() => { return (async () => {
      const out = {};
      try { out.navigations = performance.getEntriesByType('navigation').map(n => ({ type: n.type, navType: n.navigationType || '' })); } catch (e) { out.err = String(e); }
      try { out.reloadCount = (out.navigations || []).filter(n => n.type === 'reload').length; } catch (e) {}
      try { out.caches = await caches.keys(); } catch (e) { out.cacheErr = String(e); }
      try { out.controller = navigator.serviceWorker.controller && navigator.serviceWorker.controller.scriptURL; } catch (e) {}
      try { out.sessionNav = sessionStorage.getItem('__dw_nav') || ''; } catch (e) {}
      return JSON.stringify(out); })(); })()`);
    return r;
  };
  let info = null;
  for (let i = 0; i < 3 && !info; i++) {
    try { info = await finalInfo(); if (info.error) info = null; } catch {}
    if (!info) await new Promise(r => setTimeout(r, 1500));
  }
  if (!info) info = { error: 'final evaluation failed' };

  cdp.close();
  if (chromeProc) await new Promise(res => { chromeProc.kill('SIGKILL', () => res()); setTimeout(res, 4000); });

  const docs = events.filter(e => e.what === 'document');
  const loads = events.filter(e => e.what === 'load');
  const pageNavs = info.sessionNav ? info.sessionNav.trim().split('\n').filter(Boolean) : [];
  console.log(`\n[sw-live] === PHASE ${phase} RESULT ===`);
  console.log(`[sw-live] document requests: ${docs.length}`);
  docs.forEach(d => console.log(`[sw-live]   +${d.t}ms ${d.url}`));
  console.log(`[sw-live] Page.loadEventFired: ${loads.length}`);
  console.log(`[sw-live] SW+frame events: ${JSON.stringify(events.filter(e => e.what === 'document' || e.what === 'load' || e.what === 'mark' || e.what.startsWith('sw.') || e.what === 'frameNavigated'))}`);
  console.log(`[sw-live] navigation entries: ${JSON.stringify(info.navigations)}`);
  console.log(`[sw-live] reloadCount: ${info.reloadCount}`);
  console.log(`[sw-live] caches: ${JSON.stringify(info.caches)}${info.cacheErr ? ` (${info.cacheErr})` : ''}`);
  console.log(`[sw-live] controller: ${info.controller}`);
  console.log(`[sw-live] page-script navigations: ${pageNavs.length ? JSON.stringify(pageNavs, null, 1) : 'none'}`);
  const extra = docs.length - 1;
  if (extra > 0) {
    console.log(`[sw-live] EXTRA document loads: ${extra} → ${pageNavs.length ? 'page-script-driven (hook hit)' : 'SW-driven (no page-script hook hit)'}`);
  } else {
    console.log(`[sw-live] EXTRA document loads: 0 (no reload)`);
  }
  console.log(`[sw-live] PASS ${phase}: ${phase === 'C' ? (extra === 1 && info.reloadCount === 1) : (extra === 0 && info.reloadCount === 0)}`);
}

main().catch((e) => { console.error(`[sw-live] FATAL: ${e.message}`); if (chromeProc) chromeProc.kill('SIGKILL'); process.exit(1); });
