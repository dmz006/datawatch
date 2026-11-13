// Burn guard card — PWA side (read-only status tile).
//
// window.burnGuardCardHtml(status, config, opts) now draws a compact, read-only
// status tile: the guard's on/off state, when it last checked, the state/rung,
// the projection against the pace line, and the last decision. It links into the
// burn guard section of the Settings page rather than hosting config controls.
// The card computes nothing; these tests check the drawing, the tile, the
// run-now action, refresh and the interval handling. The settings link, help
// entry and the "no editable controls" contract are pinned separately in
// app-automata-burn-guard-link.test.js.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

const STATUS = {
  enabled: true, state: 'guarding', level: 1, rung: 'lower_inflight', line: 60, margin: 5,
  applied: ['lower_inflight'],
  last_projection: { projected: 72.5, line: 60, margin: 5, over_by: 7.5 },
  last_decision: { direction: 'climb', rung: 'lower_inflight', level: 1, previous: 0, reason: 'over the line' },
};
// config is accepted for backward compatibility but no longer drawn; pass one to
// prove the card ignores it.
const CONFIG = {
  enabled: true, margin_points: 5, ladder: ['lower_inflight', 'route_pi_prompt'], step_back: true,
  retry_limit_claude: 2, effort_ceiling: { docs: 'low', final_gate: 'low' },
};

function card(s, status, config, opts) {
  s._st = status; s._cfg = config; s._opts = opts || {};
  return vm.runInContext('window.burnGuardCardHtml(_st, _cfg, _opts)', s);
}

test('the card shows the state, rung, projection, margin and the last decision', () => {
  const html = card(loadApp(), STATUS, CONFIG);
  assert.ok(html.includes('data-state="guarding"'));
  assert.ok(html.includes('guarding'));
  assert.ok(html.includes('Lower Claude inflight cap'));
  assert.ok(html.includes('Projected 72.5% at the reset against a pace line of 60.0% (margin 5.0, over by 7.5)'));
  assert.ok(html.includes('Last decision: climb'));
  assert.ok(html.includes('over the line'));
});

test('the tile shows the enabled state and the last check time', () => {
  // With a last tick: the relative time is shown (timeAgo of a recent instant).
  const recent = Object.assign({}, STATUS, { last_tick_at: new Date(Date.now() - 40000).toISOString() });
  const html = card(loadApp(), recent, CONFIG);
  assert.ok(html.includes('data-burn-guard="tile"'), 'tile element');
  assert.ok(html.includes('data-enabled="true"'), 'enabled state');
  assert.ok(html.includes('data-burn-guard="enabled"'), 'enabled label');
  assert.ok(html.includes('data-burn-guard="last-check"'), 'last check shown');
  assert.ok(html.includes('Enabled'), 'localized enabled state');

  // Disabled guard: the tile reports it.
  const off = card(loadApp(), Object.assign({}, STATUS, { enabled: false }), CONFIG);
  assert.ok(off.includes('data-enabled="false"'), 'disabled state');
  assert.ok(off.includes('Disabled'), 'localized disabled state');

  // No last tick yet: the localized "never" is shown.
  const never = card(loadApp(), Object.assign({}, STATUS, { last_tick_at: null }), CONFIG);
  assert.ok(never.includes('Last check'), 'last check label');
  assert.ok(never.includes('never'), 'localized never value');
});

test('a missing status shows loading and an error shows plainly; values are escaped', () => {
  const s = loadApp();
  assert.ok(card(s, null, {}).includes('data-burn-guard="loading"'));
  const err = card(s, null, {}, { error: '<b>bad</b>' });
  assert.ok(err.includes('&lt;b&gt;bad&lt;/b&gt;'));
  assert.ok(!err.includes('<b>bad</b>'));
  const st = Object.assign({}, STATUS, { last_error: '<i>x</i>' });
  assert.ok(!card(s, st, CONFIG).includes('<i>x</i>'));
});

test('with no projection or decision the card says so', () => {
  const html = card(loadApp(), { state: 'waiting', level: 0 }, CONFIG);
  assert.ok(html.includes('No projection yet'));
  assert.ok(html.includes('No decision yet'));
});

test('Run now posts to the run endpoint and re-draws from the status', async () => {
  const s = loadApp();
  s.__calls = [];
  s.__reply = Object.assign({}, STATUS);
  vm.runInContext('apiFetch = (u, i) => { __calls.push([u, i]); return Promise.resolve(__reply); }', s);
  await vm.runInContext('window.burnGuardRunNow()', s);
  const run = s.__calls.find(c => c[0] === '/api/usage/burn-guard/run');
  assert.ok(run && run[1].method === 'POST', 'the run action POSTs');
});

test('refresh fetches only the status body, not the config body', async () => {
  const s = loadApp();
  s.__calls = [];
  s.__reply = Object.assign({}, STATUS);
  vm.runInContext('apiFetch = (u, i) => { __calls.push(u); return Promise.resolve(__reply); }', s);
  await vm.runInContext('window.burnGuardRefresh()', s);
  assert.deepEqual(s.__calls, ['/api/usage/burn-guard'], 'one status GET, no config GET');
});

test('polling uses a window-level interval variable and stops cleanly', () => {
  const s = loadApp();
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const block = src.slice(src.indexOf('// ── Burn guard card'), src.indexOf("// The usage card's loader"));
  assert.ok(block.includes('window._burnGuardInterval = setInterval('));
  assert.ok(!/\.\w*[Ii]nterval\w*\s*=\s*setInterval/.test(block.replace(/window\._burnGuardInterval/g, '')), 'no DOM-held interval');
  s.setInterval = () => 7;
  vm.runInContext('window.burnGuardStartPolling(); window.burnGuardStartPolling();', s);
  assert.equal(vm.runInContext('window._burnGuardInterval', s), 7);
  vm.runInContext('window.burnGuardStopPolling()', s);
  assert.equal(vm.runInContext('window._burnGuardInterval', s), null);
});
