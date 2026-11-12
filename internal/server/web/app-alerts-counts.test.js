// e73d0004 — regression test for the pure alerts-tab counting helpers
// in app.js: _alertCategory (one categorization used by the heading,
// the category chips and the row badges) and _alertsCounts (the shared
// per-tab alert/session counting). The disagreement being fixed: tab
// buttons counted one thing (sessions) while the "N alerts" heading and
// the category chips counted alerts, with each call site computing its
// total a different way. Now heading and "all" chip both read
// counts.all from _alertsCounts, and the session count lives in
// counts.sessions for the tab buttons.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._alertCategory, 'function', '_alertCategory is not defined — did it get renamed or moved inside the view closure?');
  assert.equal(typeof sandbox._alertsCounts, 'function', '_alertsCounts is not defined — did it get renamed or moved inside the view closure?');
  return sandbox;
}

function counts(sandbox, entries, system) {
  sandbox.__entries = entries;
  sandbox.__system = system;
  const result = vm.runInContext('_alertsCounts(__entries, __system)', sandbox);
  delete sandbox.__entries;
  delete sandbox.__system;
  return result;
}

function cat(sandbox, a, sessState) {
  sandbox.__a = a;
  sandbox.__s = sessState;
  const result = vm.runInContext('_alertCategory(__a, __s)', sandbox);
  delete sandbox.__a;
  delete sandbox.__s;
  return result;
}

test('_alertCategory: waiting_input session is a prompt regardless of level', () => {
  const s = loadAppJS();
  assert.equal(cat(s, { level: 'info', title: 'x' }, 'waiting_input'), 'prompt');
  assert.equal(cat(s, { level: 'error', title: 'x' }, 'waiting_input'), 'prompt');
});

test('_alertCategory: prompt-ish titles are prompts on any session state', () => {
  const s = loadAppJS();
  assert.equal(cat(s, { level: 'info', title: 'needs input' }, ''), 'prompt');
  assert.equal(cat(s, { level: 'warn', title: 'Agent is waiting for you' }, ''), 'prompt');
  assert.equal(cat(s, { level: 'info', title: '' }, ''), 'info'); // no prompt wording + level decides
});

test('_alertCategory: plain levels map to their category', () => {
  const s = loadAppJS();
  assert.equal(cat(s, { level: 'error', title: 'boom' }, 'running'), 'error');
  assert.equal(cat(s, { level: 'warn', title: 'slow' }, 'running'), 'warn');
  assert.equal(cat(s, { level: 'info', title: 'ok' }, 'running'), 'info');
});

test('_alertCategory: null alert does not throw', () => {
  const s = loadAppJS();
  assert.equal(cat(s, null, ''), 'info');
});

test('_alertsCounts: session alerts counted per category with sessions counted', () => {
  const s = loadAppJS();
  const entries = [
    { sessID: 'a', sessState: 'running', alerts: [
      { level: 'error', title: 'boom' },
      { level: 'warn', title: 'slow' },
      { level: 'info', title: 'ok' },
    ]},
    { sessID: 'b', sessState: 'waiting_input', alerts: [
      { level: 'info', title: 'idle' }, // prompt by state, not level
    ]},
  ];
  const c = counts(s, entries, []);
  assert.equal(c.all, 4);
  assert.equal(c.prompt, 1);
  assert.equal(c.error, 1);
  assert.equal(c.warn, 1);
  assert.equal(c.info, 1);
  assert.equal(c.sessions, 2);
});

test('_alertsCounts: system tab counts the system list, no sessions', () => {
  const s = loadAppJS();
  const c = counts(s, [], [
    { level: 'error', title: 'pipeline failed' },
    { level: 'warn', title: 'disk 90%' },
    { level: 'warn', title: 'eBPF dropped' },
  ]);
  assert.equal(c.all, 3);
  assert.equal(c.error, 1);
  assert.equal(c.warn, 2);
  assert.equal(c.sessions, 0);
});

test('_alertsCounts: heading total always equals the "all" chip total', () => {
  const s = loadAppJS();
  const entries = [
    { sessID: 'a', sessState: 'running', alerts: [
      { level: 'error', title: 'boom' },
      { level: 'error', title: 'bang' },
      { level: 'info', title: 'ok' },
    ]},
  ];
  const c = counts(s, entries, []);
  assert.equal(c.prompt + c.error + c.warn + c.info, c.all,
    'category buckets must partition the tab total (heading == "all" chip)');
});

test('_alertsCounts: tolerant of null/undefined input', () => {
  const s = loadAppJS();
  const c = counts(s, null, undefined);
  assert.equal(c.all, 0);
  assert.equal(c.sessions, 0);
  const d = counts(s, [ { alerts: undefined }, null ], [null]);
  assert.ok(d.all >= 1); // the null system alert still counts as one alert
});

test('_alertsCounts: empty alert list per session still counts the session', () => {
  const s = loadAppJS();
  const c = counts(s, [{ sessID: 'a', sessState: 'running', alerts: [] }], []);
  assert.equal(c.all, 0);
  assert.equal(c.sessions, 1);
});
