// Burn guard card — PWA side.
//
// window.burnGuardCardHtml(status, config, opts) draws the state and rung, the projection
// against the pace line, the last decision and the settings controls. The card computes
// nothing; these tests check the drawing, the writes it sends and the interval handling.

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

test('the settings controls are all present', () => {
  const html = card(loadApp(), STATUS, CONFIG);
  for (const id of ['burnGuardEnabled', 'burnGuardMargin', 'burnGuardLadder', 'burnGuardStepBack', 'burnGuardAskOnChange', 'burnGuardRetryLimit']) {
    assert.ok(html.includes('id="' + id + '"'), id);
  }
  assert.ok(html.includes('data-ceiling-kind="docs"'));
  assert.ok(html.includes('data-burn-guard="run"'), 'run now button');
  assert.ok(html.includes('/diagrams.html#docs'), 'help link');
});

test('ask_on_change is unchecked by default, checked when set, and links to the howto Settings section', () => {
  const off = card(loadApp(), STATUS, CONFIG);
  assert.ok(off.includes('id="burnGuardAskOnChange"'));
  assert.ok(!off.includes('id="burnGuardAskOnChange" checked'), 'default is off');
  const on = card(loadApp(), STATUS, Object.assign({}, CONFIG, { ask_on_change: true }));
  assert.ok(on.includes('id="burnGuardAskOnChange" checked'));
  for (const html of [off, on]) {
    assert.ok(html.includes('/diagrams.html#docs/howto/burn-guard.md#settings'), 'docs link to the howto Settings section');
    assert.ok(html.includes('Open the burn guard howto'), 'help text is the localized title');
  }
});

test('toggling ask_on_change sends a PUT of the checkbox state', async () => {
  const s = loadApp();
  const calls = stubApi(s, { enabled: true });
  s.document.getElementById = id => (id === 'burnGuardAskOnChange' ? { checked: false } : null);
  await vm.runInContext("window.burnGuardSaveField('ask_on_change')", s);
  const put = calls.find(c => c[1] && c[1].method === 'PUT');
  assert.ok(put, 'a PUT was sent');
  assert.equal(put[0], '/api/usage/burn-guard/config');
  assert.deepEqual(JSON.parse(put[1].body), { ask_on_change: false });
});

test('the ladder lists the configured rungs first, in order, with unused rungs unchecked', () => {
  const html = card(loadApp(), STATUS, CONFIG);
  const a = html.indexOf('data-rung="lower_inflight"');
  const b = html.indexOf('data-rung="route_pi_prompt"');
  const c = html.indexOf('data-rung="hold_claude"');
  assert.ok(a > 0 && a < b && b < c);
  assert.ok(html.includes('data-rung-on="lower_inflight" checked'));
  assert.ok(!html.includes('data-rung-on="hold_claude" checked'));
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

function stubApi(s, reply) {
  s.__calls = [];
  s.__reply = reply || {};
  vm.runInContext('apiFetch = (u, i) => { __calls.push([u, i]); return Promise.resolve(__reply); }', s);
  return s.__calls;
}

test('saving the margin sends a PUT of the bare key; a bad retry limit sends nothing', async () => {
  const s = loadApp();
  const calls = stubApi(s);
  s.document.getElementById = id => (id === 'burnGuardMargin' ? { value: '7' } : id === 'burnGuardRetryLimit' ? { value: '99' } : null);
  await vm.runInContext("window.burnGuardSaveField('margin_points')", s);
  const put = calls.find(c => c[1] && c[1].method === 'PUT');
  assert.ok(put, 'a PUT was sent');
  assert.equal(put[0], '/api/usage/burn-guard/config');
  assert.deepEqual(JSON.parse(put[1].body), { margin_points: 7 });
  const before = calls.length;
  await vm.runInContext("window.burnGuardSaveField('retry_limit_claude')", s);
  assert.equal(calls.length, before, 'retry limit over 20 is rejected locally');
});

test('Run now posts to the run endpoint', async () => {
  const s = loadApp();
  const calls = stubApi(s, { status: null });
  await vm.runInContext('window.burnGuardRunNow()', s);
  const run = calls.find(c => c[0] === '/api/usage/burn-guard/run');
  assert.ok(run && run[1].method === 'POST');
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

test('the max projection gap control shows the configured value and links to the howto Settings section', () => {
  const html = card(loadApp(), STATUS, Object.assign({}, CONFIG, { max_projection_gap_points: 30 }));
  assert.ok(html.includes('id="burnGuardMaxProjectionGap"'));
  assert.ok(html.includes('value="30"'));
  assert.ok(html.includes("burnGuardSaveField('max_projection_gap_points')"));
  assert.ok(html.includes('/diagrams.html#docs/howto/burn-guard.md#settings'), 'help link to the howto Settings section');
  assert.ok(html.includes('Maximum projection gap'), 'help title is the localized text');
  const unset = card(loadApp(), STATUS, CONFIG);
  assert.ok(unset.includes('id="burnGuardMaxProjectionGap"'));
  assert.ok(unset.includes('value=""'), 'no value when the config has none');
});

test('saving the max projection gap sends a PUT of the bare key', async () => {
  const s = loadApp();
  const calls = stubApi(s);
  s.document.getElementById = id => (id === 'burnGuardMaxProjectionGap' ? { value: '40' } : null);
  await vm.runInContext("window.burnGuardSaveField('max_projection_gap_points')", s);
  const put = calls.find(c => c[1] && c[1].method === 'PUT');
  assert.ok(put, 'a PUT was sent');
  assert.equal(put[0], '/api/usage/burn-guard/config');
  assert.deepEqual(JSON.parse(put[1].body), { max_projection_gap_points: 40 });
});

test('a max projection gap outside 0 to 100 or not a number is refused locally and sends nothing', async () => {
  for (const value of ['101', '-1', 'abc', '']) {
    const s = loadApp();
    const calls = stubApi(s);
    s.document.getElementById = id => (id === 'burnGuardMaxProjectionGap' ? { value } : null);
    await vm.runInContext("window.burnGuardSaveField('max_projection_gap_points')", s);
    assert.equal(calls.length, 0, 'no request for ' + JSON.stringify(value));
    assert.ok(vm.runInContext('_burnGuard.error', s).length > 0, 'an error is shown for ' + JSON.stringify(value));
  }
  const edge = loadApp();
  const calls = stubApi(edge);
  edge.document.getElementById = id => (id === 'burnGuardMaxProjectionGap' ? { value: '0' } : null);
  await vm.runInContext("window.burnGuardSaveField('max_projection_gap_points')", edge);
  assert.equal(calls.filter(c => c[1] && c[1].method === 'PUT').length, 1, '0 is a valid value (gap check off)');
});

test('the settings field for the max projection gap is a number from 0 to 100 with a locale key', () => {
  const s = loadApp();
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const line = src.split('\n').find(l => l.includes("key: 'usage.burn_guard.max_projection_gap_points'"));
  assert.ok(line, 'settings field present');
  assert.ok(line.includes("type: 'number'") && line.includes('min: 0') && line.includes('max: 100'));
  assert.ok(line.includes("labelKey: 'settings_usage_burn_guard_max_projection_gap'"));
  assert.ok(vm.runInContext("_i18n.bundle.settings_usage_burn_guard_max_projection_gap", s));
});
