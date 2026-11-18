// e73d0006 — Alerts list freshness: stale marker only (no age counter).
//
// The Alerts list loader runs `ageDisplay: 'stale-only'`:
//   * no "Last updated Ns ago" line and no per-second age ticker for
//     this list (it polls every 30 s; an age counter only churns),
//   * no `data-list-age` span in the painted HTML,
//   * instead a single stale marker once the data is past the 2-minute
//     stale-only window, or a failed refresh,
//   * a failed refresh with cached data still paints the cached rows +
//     the failure banner (Retry) + the stale marker.
//
// Other lists keep the default loader behavior (5-minute stale window;
// the cached/refreshing meta spans) — covered in the last test.
// The clock comes from listLoader's `now` option (for the generic
// loader) or from `state.at` (alerts loader, whose clock is Date.now)
// so the tests control freshness deterministically; no real timers.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const {
  vm, buildSandbox, loadScript, makeStubElement, flushAsync,
} = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE = (code) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', code + '.json'), 'utf8'));
const EN = LOCALE('en');

const SESSION = { id: 'local-st', name: 'Local', state: 'running' };
const SAMPLE = [
  { id: 'al-st-1', level: 'warn', title: 'disk filling up', body: 'usage 91% on /data', session_id: 'local-st', created_at: '2026-01-01T00:00:05Z' },
  { id: 'al-st-2', level: 'error', title: 'build failed <b title=x>', body: '', session_id: 'local-st', created_at: '2026-01-01T00:00:06Z' },
];

function boot(bundle) {
  const view = makeStubElement();
  const list = makeStubElement();
  const sandbox = buildSandbox({ byId: { view, alertsList: list } });
  const api = {
    // Flip this to simulate a failed refresh; the first load succeeds so
    // the loader has cached data to fall back to.
    fail: false,
    fetch(url) {
      const u = String(url);
      if (api.fail) return Promise.reject(new Error('upstream 503'));
      const body = u.includes('/api/alerts') ? SAMPLE
        : u.includes('/api/sessions') ? { sessions: [SESSION] }
        : u.includes('/api/commands') ? []
        : [];
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
    },
  };
  sandbox.fetch = (url, o) => api.fetch(url);
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);   // real top-level code runs to completion
  sandbox._i18n.bundle = bundle;
  sandbox._alertsTabSel = 'active';
  return { sandbox, list, api };
}

test('no "Last updated" text, no data-list-age span, no tick timer in the Alerts loader', async () => {
  const { sandbox, list, api } = boot(EN);
  const ctl = sandbox._alertsLoader('local');
  ctl.load();
  await flushAsync();
  const html = String(list.innerHTML);
  assert.ok(html.includes('disk filling up'), 'the sample alert painted');
  assert.ok(!html.includes('Last updated'), 'no "Last updated Ns ago" line');
  assert.ok(!html.toUpperCase().includes('UPDATED'), 'no "updated" wording of any size');
  assert.ok(!html.includes('data-list-age'), 'no per-second age counter span');
  assert.ok(!html.includes('list-state-age'), 'no age element in the Alerts markup');
  assert.ok(!('tickTimer' in ctl.state), 'no age tick timer on the loader state');
  assert.ok(!('ageTimer' in ctl.state), 'no age timer on the loader state');
  assert.ok(!ctl.state.pollTimer, 'no pending tick timer id at rest (stub timers are inert)');
  assert.ok(!api.fail);
});

test('fresh data → no stale marker', async () => {
  const { sandbox, list } = boot(EN);
  const ctl = sandbox._alertsLoader('local');
  ctl.load();
  await flushAsync();
  const html = String(list.innerHTML);
  assert.ok(html.includes('disk filling up'), 'the alert list is rendered');
  assert.ok(!html.includes('list-state-stale'), 'no stale marker on fresh data');
  assert.ok(!html.includes(EN.stale_data), 'no stale wording either');
});

test('stale marker appears once the data is past the 120 s stale-only window', async () => {
  const { sandbox, list } = boot(EN);
  const ctl = sandbox._alertsLoader('local');
  ctl.load();
  await flushAsync();
  assert.ok(!String(list.innerHTML).includes('list-state-stale'), 'fresh: no marker');
  ctl.state.at = Date.now() - 115_000;   // 1 min 55 s old — inside the window
  ctl.paint();
  assert.ok(!String(list.innerHTML).includes('list-state-stale'), 'just inside 120 s: still no marker');
  ctl.state.at = Date.now() - 121_000;   // past the 2-minute stale-only window
  ctl.paint();
  const html = String(list.innerHTML);
  assert.ok(html.includes('list-state-stale'), 'past 120 s: stale marker present');
  assert.ok(html.includes(EN.stale_data), 'marker wording localized: "Data may be out of date"');
});

test('failed refresh: banner + Retry, cached rows stay, stale marker joins', async () => {
  const { sandbox, list, api } = boot(EN);
  const ctl = sandbox._alertsLoader('local');
  ctl.load();
  await flushAsync();
  assert.ok(String(list.innerHTML).includes('disk filling up'), 'first load painted the alerts');
  api.fail = true;
  ctl.load();
  await flushAsync();
  const html = String(list.innerHTML);
  assert.ok(html.includes('list-state-failed'), 'failure banner shown');
  assert.ok(html.includes(EN.retry), 'Retry button offered');
  assert.ok(html.includes('disk filling up'), 'cached rows still visible under the banner');
  assert.ok(html.includes('list-state-stale'), 'stale marker joins the failed refresh');
  assert.ok(html.includes(EN.stale_data), 'stale wording localized');
});

test('a FAILED first load (no cache) shows the banner without a stale marker', async () => {
  const { sandbox, list, api } = boot(EN);
  api.fail = true;
  const ctl = sandbox._alertsLoader('local');
  ctl.load();
  await flushAsync();
  const html = String(list.innerHTML);
  assert.ok(html.includes('list-state-failed'), 'banner on empty load');
  assert.ok(html.includes(EN.retry), 'Retry offered');
  assert.ok(!html.includes('list-state-stale'), 'no stale marker — there is no cached data to be stale about');
});

test('default (other) lists keep their 5-minute window — boundary differs from Alerts 2-minute window', async () => {
  const { sandbox } = boot(EN);
  const list2 = makeStubElement();
  // Generic list on the default loader (ageDisplay unset): same paint
  // pipeline, old 5-minute stale window, driven by an injected clock.
  sandbox.__list2 = list2;
  vm.runInContext(`
    window.__now = Date.now();
    window.__other = listLoader({
      key: 'other:boundary',
      el: __list2,
      url: '/api/other',
      fetch: () => Promise.resolve({ n: 3 }),
      render: (d) => '<div class="list-state-body"><span>BODY ' + d.n + '</span></div>',
      now: () => window.__now,
    });
  `, sandbox);
  sandbox.__other.load();
  await flushAsync();
  let html = String(list2.innerHTML);
  assert.ok(html.includes('BODY 3'), 'generic list renders its data');
  assert.ok(!html.includes('list-state-stale'), 'fresh: no stale marker (default list)');

  sandbox.__now += 3 * 60 * 1000;   // 3 min old — an Alerts list would already be stale
  sandbox.__other.paint();
  html = String(list2.innerHTML);
  assert.ok(html.includes('BODY 3'), '3 min: default list still fine (window is 5 min, not 2)');
  assert.ok(!html.includes('list-state-stale'), 'just inside 5 min: no marker on the default list');

  sandbox.__now += 3 * 60 * 1000;   // 6 min old — past the default 5-minute window
  sandbox.__other.paint();
  html = String(list2.innerHTML);
  assert.ok(html.includes('list-state-stale'), 'past 5 min (injected clock): default list shows its stale marker too');
  assert.ok(html.includes(EN.stale_data), 'stale wording localized');
});

test('no "Last updated" / data-list-age machinery in app.js for this list; app.js is untouched', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  // The generic loader never writes an age counter or a "last updated"
  // timestamp; the only freshness span is the .list-state-stale marker.
  assert.ok(!src.includes('data-list-age'), 'no data-list-age counter element anywhere in app.js');
  assert.ok(!/Last updated \d/.test(src), 'no live "Last updated Ns ago" counter rendering');
  const i = src.indexOf('ageDisplay: \'stale-only\'');
  assert.ok(i >= 0, 'the Alerts loader opts into ageDisplay: stale-only');
  assert.ok(src.includes('LIST_STALE_ONLY_MS'), 'the 2-minute stale-only window constant is defined');
});
