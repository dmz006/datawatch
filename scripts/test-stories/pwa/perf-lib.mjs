// scripts/test-stories/pwa/perf-lib.mjs — shared helpers for the load-performance
// PWA stories (TS-802 .. TS-812).
//
// Built on lib.mjs. Provides: network throttle profiles (Chrome DevTools
// Protocol), time-to-first-content and "loading indicator within 200 ms"
// measurement, a "never blank without an indicator" watcher, route helpers to
// fail / delay / reorder one endpoint, and a seeding helper for the large data
// set. Budgets are constants here and can be overridden by environment:
//
//   PERF_BUDGET_FAST_MS     time to first content, unthrottled      (default 1500)
//   PERF_BUDGET_SLOW4G_MS   time to first content, slow 4G          (default 3000,
//                           same default as scripts/perf-budget.sh PERF_TAB_BUDGET_MS)
//   PERF_BUDGET_RELOAD_MS   a full reload back to content           (default 4000)
//   PERF_INDICATOR_MS       loading indicator after navigation      (default 200)
//   PERF_SEED_SESSIONS      large set: sessions                     (default 3000)
//   PERF_SEED_AUTOMATA      large set: heavy Automata               (default 300)
//   PERF_LOADER_TIMEOUT_MS  the list loader's request timeout       (default 15000)
//   PERF_BUDGET_TAB_BYTES   data bytes for one tab's first load      (default 524288)
//   PERF_BUDGET_RECONNECT_REQUESTS  requests after a reconnect      (default 6)
//   PERF_BUDGET_RECONNECT_BYTES     bytes after a reconnect         (default 262144)
//   PERF_OUTAGE_MS          how long the connection stays dropped   (default 40000,
//                           past the 35 s socket watchdog threshold)
//   PERF_BUDGET_WIRE_FRAME_BYTES         largest single websocket frame    (default 307200)
//   PERF_BUDGET_WIRE_COLD_BYTES          websocket bytes, cold connect     (default 524288)
//   PERF_BUDGET_WIRE_RECONNECT_BYTES     websocket bytes, 10 s after reconnect (default 524288)
//   PERF_BUDGET_WIRE_IDLE_BYTES_PER_MIN  websocket bytes per minute, one idle session open
//                                        (default 262144)
//
// The large set is normally supplied by a daemon started on the seeded fixture
// (SeedPerfServer in internal/server/perf_seed_test.go). seedLargeSet() checks
// the counts through the daemon API and tops Automata up through the API when
// it can; sessions have no bulk-create endpoint, so a daemon without the
// fixture makes the story skip (exit 2) instead of failing.

import { writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { env, storyEvidenceDir } from './lib.mjs';

const num = (name, dflt) => {
  const v = Number(process.env[name]);
  return Number.isFinite(v) && v > 0 ? v : dflt;
};

export const BUDGET = {
  fastMs: num('PERF_BUDGET_FAST_MS', 1500),
  slow4gMs: num('PERF_BUDGET_SLOW4G_MS', 3000),
  reloadMs: num('PERF_BUDGET_RELOAD_MS', 4000),
  indicatorMs: num('PERF_INDICATOR_MS', 200),
  seedSessions: num('PERF_SEED_SESSIONS', 3000),
  seedAutomata: num('PERF_SEED_AUTOMATA', 300),
  loaderTimeoutMs: num('PERF_LOADER_TIMEOUT_MS', 15000),
  tabBytes: num('PERF_BUDGET_TAB_BYTES', 512 * 1024),
  reconnectRequests: num('PERF_BUDGET_RECONNECT_REQUESTS', 6),
  reconnectBytes: num('PERF_BUDGET_RECONNECT_BYTES', 256 * 1024),
  outageMs: num('PERF_OUTAGE_MS', 40000),
  // Websocket byte budgets (TS-816). The frame budget is the largest single
  // message a client may receive: a summary-row frame stays well under it, and
  // a single oversized frame is what blocks the UI thread on a phone.
  wireFrameBytes: num('PERF_BUDGET_WIRE_FRAME_BYTES', 307200),
  // Everything received from the first socket until the sessions list renders.
  wireColdBytes: num('PERF_BUDGET_WIRE_COLD_BYTES', 524288),
  // Everything received in the 10 s after a 2.5 s outage reconnects.
  wireReconnectBytes: num('PERF_BUDGET_WIRE_RECONNECT_BYTES', 524288),
  // Received bytes per minute with one running session's detail view idle.
  wireIdleBytesPerMin: num('PERF_BUDGET_WIRE_IDLE_BYTES_PER_MIN', 262144),
};

// Throttle profiles for Network.emulateNetworkConditions. slow-4G is the
// DevTools preset: 1.6 Mbps down, 750 Kbps up, 150 ms RTT.
export const PROFILES = {
  'slow-4g': {
    offline: false,
    latency: 150,
    downloadThroughput: (1.6 * 1024 * 1024) / 8,
    uploadThroughput: (750 * 1024) / 8,
  },
  fast: { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 },
};

export function budgetFor(profileName) {
  return profileName === 'slow-4g' ? BUDGET.slow4gMs : BUDGET.fastMs;
}

// Main tabs and the list endpoint each one loads first. Automata and Automation
// share one main tab (view 'autonomous'); `subtab` is the sub-tab button the
// story selects after opening it (see openTabSrc / openTab).
export const TABS = [
  { name: 'sessions', view: 'sessions', endpoint: '/api/sessions' },
  { name: 'automata', view: 'autonomous', subtab: 'automata', endpoint: '/api/autonomous/prds' },
  { name: 'automation', view: 'autonomous', subtab: 'automation', endpoint: '/api/automations' },
  { name: 'observer', view: 'observer', endpoint: '/api/observer/stats' },
  { name: 'settings', view: 'settings', endpoint: '/api/config' },
];

// --- skip / assertion plumbing ----------------------------------------------

// Skip the story (exit 2, reason in result.txt) when a precondition is missing,
// e.g. no Chrome or no seeded fixture. run_pwa_story maps exit 2 to skip.
export async function skipStory(reason) {
  try {
    const dir = storyEvidenceDir();
    await mkdir(dir, { recursive: true });
    await writeFile(path.join(dir, 'result.txt'), reason);
  } catch { /* best-effort */ }
  console.error('SKIP:', reason);
  process.exit(2);
}

// A story's failures throw; runStory turns them into a non-zero exit.
export function check(cond, message) {
  if (!cond) throw new Error(message);
}

export async function saveJson(name, obj) {
  const dir = storyEvidenceDir();
  await mkdir(dir, { recursive: true });
  await writeFile(path.join(dir, `${name}.json`), JSON.stringify(obj, null, 2));
}

// Chromium navigation errors that are not product failures.
export function isTransient(err) {
  const m = (err && err.message) || '';
  return /ERR_CERT_VERIFIER_CHANGED|ERR_NETWORK_CHANGED|ERR_CONNECTION_RESET|Cannot find context with specified id|Execution context was destroyed/.test(m);
}

// Retry fn once when it hit a transient navigation error.
export async function tolerateTransient(fn) {
  try { return await fn(); } catch (e) {
    if (!isTransient(e)) throw e;
    await new Promise((r) => setTimeout(r, 1000));
    return fn();
  }
}

// --- throttling --------------------------------------------------------------

// Apply a profile ('slow-4g' | 'fast') to the page's context. Returns the CDP
// session; call setProfile again to change it.
export async function throttle(page, profileName) {
  const profile = PROFILES[profileName];
  check(profile, `unknown throttle profile ${profileName}`);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', profile);
  return cdp;
}

export async function setProfile(cdp, profileName) {
  await cdp.send('Network.emulateNetworkConditions', PROFILES[profileName]);
}

// --- in-page view classifier -------------------------------------------------

// Runs in the page. #view always holds exactly one of:
//   failed   a .list-state-failed banner
//   loading  a skeleton / loading label with no loaded body yet
//   empty    an empty-state block
//   content  rendered content (a loaded body or any non-empty text)
//   blank    nothing — the state that must never be observed
// Kept as a function source string so it can be injected into evaluate calls.
export const CLASSIFY_SRC = `(() => {
  const el = document.getElementById('view');
  if (!el) return 'blank';
  if (el.querySelector('.list-state-failed')) return 'failed';
  const hasBody = !!el.querySelector('.list-state-body');
  if (!hasBody && el.querySelector('.list-state-skeleton, .list-skel-row, .skeleton, .loading, [aria-busy="true"]')) return 'loading';
  if (el.querySelector('.list-state-empty, .automation-empty-state, .empty-state')) return 'empty';
  const txt = (el.innerText || '').trim();
  if (!txt || el.children.length === 0) return 'blank';
  if (!hasBody && /^\\s*(loading|…|\\.\\.\\.)/i.test(txt)) return 'loading';
  return 'content';
})()`;

export function viewState(page) {
  return page.evaluate(CLASSIFY_SRC).catch(() => 'unknown');
}

// Wait until #view classifies as one of `states`. Returns elapsed ms.
export async function waitForState(page, states, timeout = 30000) {
  const t0 = Date.now();
  await page.waitForFunction(
    ({ src, want }) => want.includes(eval(src)),
    { src: CLASSIFY_SRC, want: states },
    { timeout, polling: 50 },
  );
  return Date.now() - t0;
}

// Runs in the page: open a main tab and, when it has one, select its sub-tab by
// clicking the sub-tab button (the same path an operator takes). The button is
// found by its data-tab value; window.switchAutomataTab is the fallback when the
// bar has not rendered yet.
export const OPEN_TAB_SRC = `((view, subtab) => {
  window.navigate(view);
  if (!subtab) return;
  const btn = document.querySelector('.automata-tabs-bar .output-tab[data-tab="' + subtab + '"]');
  if (btn) { if (!btn.classList.contains('active')) btn.click(); }
  else if (window.switchAutomataTab) window.switchAutomataTab(subtab);
})`;

// Open `tab` (an entry of TABS) in the page. `clear` empties #view first so the
// next state seen is the one the open produced.
export function openTab(page, tab, { clear = false } = {}) {
  return page.evaluate(({ src, view, subtab, clear }) => {
    if (clear) document.getElementById('view').innerHTML = '';
    return eval(src)(view, subtab);
  }, { src: OPEN_TAB_SRC, view: tab.view, subtab: tab.subtab || null, clear });
}

// --- measurement -------------------------------------------------------------

// Navigate to `view` inside the page and measure, in-page and so unaffected by
// protocol latency:
//   indicatorMs  navigation -> first non-blank state (loading/content/...)
//   firstContentMs  navigation -> first 'content' (or 'empty'/'failed') state
//   blankAfterIndicator  number of samples that were blank AFTER the indicator
//   states       ordered list of distinct states seen
// `watchMs` keeps sampling after first content so a late blank is caught.
export function measureTab(page, view, { timeout = 60000, watchMs = 500, subtab = null } = {}) {
  return page.evaluate(({ view, subtab, timeout, watchMs, src, openSrc }) => new Promise((resolve) => {
    const classify = () => eval(src);
    const el = document.getElementById('view');
    const res = { indicatorMs: null, firstContentMs: null, blankAfterIndicator: 0, states: [], timedOut: false };
    el.innerHTML = '';
    const t0 = performance.now();
    let contentAt = null;
    let finished = false;
    let iv = null;
    const mo = new MutationObserver(() => sample());
    const finish = () => {
      if (finished) return;
      finished = true;
      clearInterval(iv);
      mo.disconnect();
      resolve(res);
    };
    function sample() {
      if (finished) return;
      const s = classify();
      const now = performance.now() - t0;
      if (res.states[res.states.length - 1] !== s) res.states.push(s);
      if (s !== 'blank' && res.indicatorMs === null) res.indicatorMs = Math.round(now);
      if (s === 'blank' && res.indicatorMs !== null) res.blankAfterIndicator++;
      if ((s === 'content' || s === 'empty' || s === 'failed') && res.firstContentMs === null) {
        res.firstContentMs = Math.round(now);
        res.firstState = s;
        contentAt = now;
      }
      if (contentAt !== null && now - contentAt >= watchMs) finish();
      if (now >= timeout) { res.timedOut = true; finish(); }
    }
    mo.observe(el, { childList: true, subtree: true, characterData: true, attributes: true });
    iv = setInterval(sample, 20);
    eval(openSrc)(view, subtab);
    sample();
  }), { view, subtab, timeout, watchMs, src: CLASSIFY_SRC, openSrc: OPEN_TAB_SRC });
}

// Poll the tab container for `ms` and return every sample's state. Used by the
// "never blank" check when something other than navigate() drives the view.
export async function sampleStates(page, ms, everyMs = 40) {
  const seen = [];
  const t0 = Date.now();
  while (Date.now() - t0 < ms) {
    seen.push({ t: Date.now() - t0, s: await viewState(page) });
    await page.waitForTimeout(everyMs);
  }
  return seen;
}

// Throw if a measureTab result breaks the indicator / blank / budget rules.
export function assertMeasure(label, r, { budgetMs, indicatorMs = BUDGET.indicatorMs } = {}) {
  check(!r.timedOut, `${label}: no content within the timeout (states: ${r.states.join(' > ')})`);
  check(r.indicatorMs !== null && r.indicatorMs <= indicatorMs,
    `${label}: loading indicator after ${r.indicatorMs} ms, limit ${indicatorMs} ms`);
  check(r.blankAfterIndicator === 0,
    `${label}: view went blank ${r.blankAfterIndicator} time(s) after the indicator (states: ${r.states.join(' > ')})`);
  if (budgetMs) {
    check(r.firstContentMs !== null && r.firstContentMs <= budgetMs,
      `${label}: time to first content ${r.firstContentMs} ms, budget ${budgetMs} ms`);
  }
}

// Stricter "never blank without an indicator" check: every sample of `samples`
// (from sampleStates) must be a non-blank state.
export function assertNeverBlank(label, samples) {
  const blank = samples.filter((x) => x.s === 'blank');
  check(blank.length === 0, `${label}: ${blank.length} blank sample(s), first at ${blank[0] && blank[0].t} ms`);
}

// --- route interception ------------------------------------------------------

// Match API calls by pathname (query string ignored). `method` defaults to GET.
export function endpointMatcher(pathname, method = 'GET') {
  return (url, req) => {
    try {
      const u = new URL(url);
      if (u.pathname !== pathname) return false;
      return !method || !req || req.method() === method;
    } catch { return false; }
  };
}

async function routeEndpoint(page, pathname, handler, method) {
  const match = endpointMatcher(pathname, method);
  const fn = (route) => (match(route.request().url(), route.request()) ? handler(route) : route.fallback());
  await page.route('**/api/**', fn);
  return () => page.unroute('**/api/**', fn).catch(() => {});
}

// Fail `pathname` with `status` (default 500) for the next `times` calls
// (default: until the returned stop() is called). Returns { stop, hits }.
export async function failEndpoint(page, pathname, { status = 500, times = Infinity, method = 'GET' } = {}) {
  const ctl = { hits: 0 };
  ctl.stop = await routeEndpoint(page, pathname, (route) => {
    if (ctl.hits >= times) return route.fallback();
    ctl.hits++;
    return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify({ error: 'injected failure' }) });
  }, method);
  return ctl;
}

// Answer `pathname` with a fixed JSON body (e.g. an empty list).
export async function fulfillEndpoint(page, pathname, body, { method = 'GET' } = {}) {
  const ctl = { hits: 0 };
  ctl.stop = await routeEndpoint(page, pathname, (route) => {
    ctl.hits++;
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  }, method);
  return ctl;
}

// Delay every call to `pathname` by `ms` before it reaches the daemon.
export async function delayEndpoint(page, pathname, ms, { method = 'GET', times = Infinity } = {}) {
  const ctl = { hits: 0 };
  ctl.stop = await routeEndpoint(page, pathname, async (route) => {
    if (ctl.hits >= times) return route.fallback();
    ctl.hits++;
    await new Promise((r) => setTimeout(r, ms));
    try { await route.fallback(); } catch { /* page closed or request aborted */ }
  }, method);
  return ctl;
}

// Reorder responses: the FIRST call to `pathname` is fetched immediately but
// held back (its JSON body passed through `markStale`) until release() is
// called; later calls pass straight through. Lets a story make an older
// request return after a newer one. Returns { release, firstHeld(), stop }.
export async function reorderEndpoint(page, pathname, { markStale = (b) => b, method = 'GET' } = {}) {
  const ctl = { hits: 0, held: null, released: false };
  let releaseFn = null;
  const gate = new Promise((r) => { releaseFn = r; });
  ctl.release = () => { ctl.released = true; releaseFn(); };
  ctl.firstHeld = () => ctl.held !== null;
  ctl.stop = await routeEndpoint(page, pathname, async (route) => {
    ctl.hits++;
    if (ctl.hits !== 1) return route.fallback();
    ctl.held = true;
    let resp;
    try { resp = await route.fetch(); } catch { return route.abort().catch(() => {}); }
    await gate;
    let body = await resp.text();
    try { body = JSON.stringify(markStale(JSON.parse(body))); } catch { /* not JSON: pass through */ }
    return route.fulfill({ response: resp, body }).catch(() => {});
  }, method);
  return ctl;
}

// --- network recording -------------------------------------------------------

// List endpoints that have a small (summary) form. A GET of one of these
// without view=summary is a full-form list request.
export const SUMMARY_LIST_PATHS = ['/api/sessions', '/api/autonomous/prds'];

// Why `url` is a full-form list request, or '' when it is not. A request
// through a federation proxy is judged on the path behind the proxy prefix.
export function fullFormReason(url, method = 'GET') {
  if (method !== 'GET') return '';
  let u;
  try { u = new URL(url, 'http://localhost'); } catch { return ''; }
  const full = u.searchParams.get('full');
  if (full === '1' || full === 'true') return 'full=1';
  const p = u.pathname.replace(/^\/api\/proxy\/[^/]+/, '');
  if (SUMMARY_LIST_PATHS.includes(p) && u.searchParams.get('view') !== 'summary') {
    return 'list endpoint without view=summary';
  }
  return '';
}

// Record every request the page makes, with the encoded size of its response
// body (page.on('response') + Request.sizes()). mark() returns a position;
// since(pos) returns the entries recorded after it. Call flush() before
// reading sizes so bodies still in flight are counted.
export function recordNetwork(page) {
  const entries = [];
  const byRequest = new Map();
  const pending = [];
  page.on('request', (req) => {
    const u = req.url();
    if (u.startsWith('data:') || u.startsWith('blob:')) return;
    const e = { url: u, method: req.method(), status: null, bytes: 0, failed: false, at: Date.now() };
    entries.push(e);
    byRequest.set(req, e);
  });
  page.on('response', (resp) => {
    const req = resp.request();
    const e = byRequest.get(req);
    if (!e) return;
    e.status = resp.status();
    pending.push(
      req.sizes()
        .then((s) => { e.bytes = Math.max(0, s.responseBodySize); })
        .catch(() => {
          const cl = Number(resp.headers()['content-length']);
          e.bytes = Number.isFinite(cl) ? cl : 0;
        }),
    );
  });
  page.on('requestfailed', (req) => {
    const e = byRequest.get(req);
    if (e) e.failed = true;
  });
  return {
    entries,
    mark: () => entries.length,
    since: (pos) => entries.slice(pos),
    flush: () => Promise.all(pending.splice(0)).then(() => {}),
  };
}

export const sumBytes = (entries) => entries.reduce((n, e) => n + e.bytes, 0);

// --- seeding -----------------------------------------------------------------

async function apiJson(pathAndQuery, init = {}) {
  const r = await fetch(env.http + pathAndQuery, {
    ...init,
    headers: { Authorization: `Bearer ${env.token}`, 'Content-Type': 'application/json', ...(init.headers || {}) },
  });
  if (!r.ok) throw new Error(`${init.method || 'GET'} ${pathAndQuery} -> ${r.status}`);
  const text = await r.text();
  return text ? JSON.parse(text) : null;
}

const listOf = (d, ...keys) => {
  if (Array.isArray(d)) return d;
  for (const k of keys) if (d && Array.isArray(d[k])) return d[k];
  return [];
};

export async function countSessions() {
  const d = await apiJson('/api/sessions?view=summary&limit=100000');
  return listOf(d, 'sessions', 'items').length;
}

export async function countAutomata() {
  const d = await apiJson('/api/autonomous/prds');
  return listOf(d, 'prds', 'items').length;
}

// Ensure the large data set exists: BUDGET.seedSessions sessions and
// BUDGET.seedAutomata Automata. Automata are topped up through the API when
// short; sessions can only come from the seeded fixture, so a shortfall there
// skips the story. Returns the counts. Created Automata are titled
// "perf-seed-N" so cleanupSeed() can remove them.
export async function seedLargeSet({ log = () => {} } = {}) {
  const sessions = await countSessions();
  if (sessions < BUDGET.seedSessions) {
    await skipStory(`large set needs ${BUDGET.seedSessions} sessions, daemon has ${sessions}; start it on the SeedPerfServer fixture (internal/server/perf_seed_test.go)`);
  }
  const automata = await countAutomata();
  let created = 0;
  while (automata + created < BUDGET.seedAutomata) {
    const n = automata + created;
    try {
      await apiJson('/api/autonomous/prds', {
        method: 'POST',
        body: JSON.stringify({ spec: `perf-seed automaton ${n}: ${'lorem ipsum '.repeat(200)}`, project_dir: '.', title: `perf-seed-${n}` }),
      });
      created++;
    } catch (e) {
      await skipStory(`cannot create Automata through the API (${e.message}); ${automata + created} of ${BUDGET.seedAutomata} present`);
    }
  }
  log(`seed: sessions=${sessions} automata=${automata}+${created}`);
  return { sessions, automata: automata + created, created };
}

// Cancel Automata a story created (best-effort).
export async function cleanupSeed() {
  try {
    const d = await apiJson('/api/autonomous/prds');
    for (const p of listOf(d, 'prds', 'items')) {
      if (String(p.title || p.name || '').startsWith('perf-seed-')) {
        await apiJson('/api/autonomous/prds/' + encodeURIComponent(p.id) + '/cancel', { method: 'POST', body: '{}' }).catch(() => {});
      }
    }
  } catch { /* best-effort */ }
}

// --- context helpers ---------------------------------------------------------

// A fresh browser context (cold: empty cache, empty localStorage).
export async function coldPage(browser, { viewport = { width: 1280, height: 800 } } = {}) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport });
  const page = await context.newPage();
  return { context, page };
}

// Open the PWA with the token injected and wait for the app and splash. Used
// where a story controls its own timing instead of calling connectToPWA.
export async function openPWA(page, { timeout = 60000 } = {}) {
  await tolerateTransient(async () => {
    await page.goto(env.http, { waitUntil: 'domcontentloaded', timeout });
    await page.evaluate((t) => localStorage.setItem('cs_token', t), env.token);
    await page.reload({ waitUntil: 'domcontentloaded', timeout });
  });
  await page.waitForFunction(() => typeof window.navigate === 'function', null, { timeout });
  await page.waitForFunction(() => {
    const el = document.getElementById('splash');
    return !el || getComputedStyle(el).display === 'none' || getComputedStyle(el).opacity === '0' || el.classList.contains('hidden');
  }, null, { timeout });
}

// Horizontal overflow at the current viewport.
export function noHorizontalOverflow(page) {
  return page.evaluate(() => {
    const de = document.documentElement;
    return {
      ok: de.scrollWidth <= window.innerWidth + 1 && document.body.scrollWidth <= window.innerWidth + 1,
      scrollWidth: de.scrollWidth,
      innerWidth: window.innerWidth,
    };
  });
}

// --- story runner ------------------------------------------------------------

// runStory plus the skip rule: exit 2 when Chrome cannot be launched.
export async function runPerfStory(fn) {
  const { launchBrowser, runStory } = await import('./lib.mjs');
  try {
    const b = await launchBrowser();
    await b.close();
  } catch (e) {
    await skipStory(`Chrome unavailable: ${(e && e.message || e).toString().split('\n')[0]}`);
  }
  await runStory(fn);
}
