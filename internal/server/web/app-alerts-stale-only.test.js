// ageDisplay "stale-only" (the Alerts list's display mode, shared
// listLoader in app.js) — the stale marker contract.
//
// Every listLoader paints one small stale marker (`.list-state-stale`)
// while its data is out of date. The marker is up when
//   * the last refresh failed (error !== ''), or
//   * the data (the last successful refresh) is older than the
//     loader's threshold:
//       - default mode: LIST_STALE_MS = 5 minutes (unchanged).
//       - ageDisplay "stale-only": LIST_STALE_ONLY_MS = a fixed
//         2 minutes, deliberately independent of the loader's pollMs
//         (Alerts polls every 30 s; a poll-derived threshold would flag
//         data "stale" while its own scheduled refresh is merely due).
// There is no ticking "Last updated N s ago" line and no per-second
// repaint in either mode.
//
// Contracts under test:
//   * stale-only: marker appears strictly after 120 s of data age, is
//     absent at 120 s and under, and appears on a failed refresh while
//     the data is still seconds old;
//   * default: marker absent well under 5 min (including the
//     120–300 s band stale-only flags), present past 5 min — the
//     pre-Alerts contract is unchanged;
//   * _alertsLoader opts into stale-only on top of its 30 s poll, is
//     cached per server in window._listLoaders, and its render carries
//     the stale marker once the data is 2+ min old;
//   * the Alerts topBar refresh button's alertsRefresh() drives a real
//     refetch through the cached loader (not just a repaint);
//   * the topBar button is wired to alertsRefresh() with the
//     alerts_refresh_btn title and fallback text.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));

// Load the REAL app.js top level into a fresh sandbox (same approach as
// app-list-loader-meta-extra.test.js); resolve t() through the en bundle.
function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = EN;
  return sandbox;
}

// mkLoader wires one listLoader into a fresh sandbox with a controllable
// clock (opts.now = _clock, starting at 0) and a stub element that
// records what paint() writes. ageDisplay may be undefined (default
// mode) or 'stale-only'.
function mkLoader(ageDisplay) {
  const sandbox = loadApp();
  const el = makeStubElement(() => {});
  let clock = 0;
  sandbox._clock = () => clock;
  sandbox._el = el;
  sandbox._data = { n: 7 };
  const opts = [
    'key: "ageonly-test' + (ageDisplay ? '-so' : '-def') + '"',
    'url: "/api/age-test"',
    'el: _el',
    'fetch: () => Promise.resolve(_data)',
    'render: d => "body-" + d.n',
    'now: _clock',
  ];
  if (ageDisplay) opts.push('ageDisplay: "' + ageDisplay + '"');
  sandbox._ctl = vm.runInContext('listLoader({ ' + opts.join(', ') + ' })', sandbox);
  return {
    sandbox,
    el,
    ctl: sandbox._ctl,
    setClock: (ms) => { clock = ms; },
  };
}

test('stale-only: stale marker appears strictly past 120 s, absent at 120 s and under', async () => {
  const m = mkLoader('stale-only');
  await m.ctl.load();
  await flushAsync();
  assert.ok(m.el.innerHTML.includes('body-7'), 'data renders');
  assert.ok(!m.el.innerHTML.includes('list-state-stale'), 'fresh data: marker absent');
  m.setClock(119_000);
  m.ctl.paint();
  assert.ok(!m.el.innerHTML.includes('list-state-stale'), '119 s: still within the 120 s window');
  m.setClock(120_000);
  m.ctl.paint();
  assert.ok(!m.el.innerHTML.includes('list-state-stale'), 'exactly 120 s: boundary not stale (strictly older)');
  m.setClock(120_001);
  m.ctl.paint();
  assert.ok(m.el.innerHTML.includes('list-state-stale'), '120.001 s: marker appears');
  m.ctl.stop();
});

test('stale-only flags 3–5 min old data that default mode still treats as fresh', async () => {
  const so = mkLoader('stale-only');
  const def = mkLoader(undefined);
  await so.ctl.load();
  await def.ctl.load();
  await flushAsync();
  for (const [ms, soStale, defStale] of [[130_000, true, false], [299_000, true, false], [301_000, true, true]]) {
    so.setClock(ms);
    def.setClock(ms);
    so.ctl.paint();
    def.ctl.paint();
    const soHas = so.el.innerHTML.includes('list-state-stale');
    const defHas = def.el.innerHTML.includes('list-state-stale');
    assert.equal(soHas, soStale, `stale-only at ${ms} ms expected ${soStale}`);
    assert.equal(defHas, defStale, `default at ${ms} ms expected ${defStale}`);
  }
  so.ctl.stop();
  def.ctl.stop();
});

test('default mode keeps the 5 minute threshold exactly (regression guard)', async () => {
  const m = mkLoader(undefined);
  await m.ctl.load();
  await flushAsync();
  m.setClock(299_999);
  m.ctl.paint();
  assert.ok(!m.el.innerHTML.includes('list-state-stale'), 'just under 5 min: not stale');
  m.setClock(300_001);
  m.ctl.paint();
  assert.ok(m.el.innerHTML.includes('list-state-stale'), 'just past 5 min: stale');
  m.ctl.stop();
});

test('stale marker is up on a failed refresh even while data is seconds old (both modes)', async () => {
  for (const mode of ['stale-only', undefined]) {
    const sandbox = loadApp();
    const el = makeStubElement(() => {});
    let clock = 0;
    sandbox._clock = () => clock;
    sandbox._el = el;
    sandbox._data = { n: 1 };
    sandbox._reject = false;
    const opts = [
      'key: "failmode-' + (mode || 'def') + '"',
      'url: "/api/failmode-test"',
      'el: _el',
      'fetch: () => _reject ? Promise.reject(new Error("boom")) : Promise.resolve(_data)',
      'render: d => "body-" + d.n',
      'now: _clock',
    ];
    if (mode) opts.push('ageDisplay: "' + mode + '"');
    const ctl = vm.runInContext('listLoader({ ' + opts.join(', ') + ' })', sandbox);
    await ctl.load();
    await flushAsync();
    clock = 20_000; // 20 s old — well inside both thresholds
    sandbox._reject = true;
    await ctl.load(); // fails; cached data remains
    await flushAsync();
    assert.ok(el.innerHTML.includes('list-state-stale'), `${mode || 'default'}: failed refresh marks data stale at 20 s`);
    assert.ok(el.innerHTML.includes('body-1'), `${mode || 'default'}: cached data still rendered`);
    ctl.stop();
  }
});

test('no ticking "Last updated" line and no per-second repaint in either mode', async () => {
  const so = mkLoader('stale-only');
  const def = mkLoader(undefined);
  await so.ctl.load();
  await def.ctl.load();
  await flushAsync();
  for (const m of [so, def]) {
    assert.ok(!m.el.innerHTML.includes('list-state-age'), 'no age span rendered');
    assert.ok(!m.el.innerHTML.includes('Last updated'), 'no "Last updated" line rendered');
    assert.equal('tickTimer' in m.ctl.state, false, 'no tick timer in state');
    m.ctl.stop();
  }
});

test('_alertsLoader: stale-only on top of the 30 s poll, cached per server, marker appears at 2 min', async () => {
  const alertsEl = makeStubElement(() => {});
  const sandbox = buildSandbox({ byId: { alertsList: alertsEl } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = EN;
  sandbox.fetch = (url) => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve([]) });
  const local = sandbox._alertsLoader('local');
  assert.equal(sandbox._alertsLoader('local'), local, 'second call returns the cached local loader');
  const remote = sandbox._alertsLoader('host-a');
  assert.notEqual(remote, local, 'a different server gets its own loader');

  assert.equal(local.state.opts.ageDisplay, 'stale-only', 'Alerts opts into stale-only');
  assert.equal(local.state.opts.pollMs, 30000, 'Alerts still polls every 30 s');
  assert.equal(sandbox._listLoaders['alerts:local'], local, 'registered under alerts:local');

  await local.load();
  await flushAsync();
  assert.ok(alertsEl.innerHTML.length > 0, 'alerts view rendered into #alertsList');
  assert.ok(!alertsEl.innerHTML.includes('list-state-stale'), 'just-loaded alerts data: not stale');

  // 90 s old: still within the 2 min window.
  local.state.at = Date.now() - 90_000;
  local.paint();
  assert.ok(!alertsEl.innerHTML.includes('list-state-stale'), '90 s old: within stale-only window');

  // 130 s old: past the 2 min window → marker.
  local.state.at = Date.now() - 130_000;
  local.paint();
  assert.ok(alertsEl.innerHTML.includes('list-state-stale'), '130 s old: past the 2 min stale-only window');
  local.state.error = 'refused';
  local.paint();
  assert.ok(alertsEl.innerHTML.includes('list-state-stale'), 'failed refresh: marker up regardless of age');

  local.stop();
  remote.stop();
});

test('alertsRefresh drives a real refetch through the cached loader', async () => {
  const sandbox = loadApp();
  let fetches = 0;
  sandbox.fetch = (url) => {
    fetches++;
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve([]) });
  };
  const ctl = sandbox._alertsLoader('local');
  await ctl.load();
  await flushAsync();
  assert.ok(fetches > 0, 'initial load hit the network');
  const before = fetches;
  sandbox.alertsRefresh();
  await flushAsync();
  assert.ok(fetches > before, 'alertsRefresh refetched through the cached loader');
  ctl.stop();
});

test('Alerts topBar refresh button is wired to alertsRefresh (not a plain repaint)', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('function _alertsBuildHtml');
  assert.ok(start >= 0, '_alertsBuildHtml present');
  const stop = src.indexOf('window.alertsRefresh', start);
  assert.ok(stop > start, 'alertsRefresh defined after _alertsBuildHtml');
  const region = src.slice(start, stop);
  const btn = region.match(/<button[^>]*onclick="alertsRefresh\(\)"[^>]*title="[^"]*"[^>]*>/); 
  assert.ok(btn, 'refresh button calls alertsRefresh()');
  assert.ok(!!region.match(/t\('alerts_refresh_btn'\)/), 'button title uses the alerts_refresh_btn locale key');
  assert.ok(btn[0].includes("t('alerts_refresh_btn')"), 'button title keeps the alerts_refresh_btn key with literal fallback');
  assert.ok(btn[0].includes("'Refresh'"), 'button title falls back to the literal "Refresh" text');
  assert.ok(!/<button[^>]*on(click|change)="renderAlertsView\(\)"/.test(region), 'no full-view repaint button in the toolbar');
});
