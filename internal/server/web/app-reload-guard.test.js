// Pins the daemon-version-change reload policy: never reload under an active
// user; show a single tap-to-reload banner instead.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function load({ activeElement = null, inputValue = '' } = {}) {
  const sandbox = buildSandbox();
  const appended = [];
  const reloads = [];
  const timers = [];
  sandbox.location.reload = () => reloads.push(1);
  sandbox.setTimeout = (fn, ms) => { timers.push({ fn, ms }); return timers.length; };
  sandbox.clearTimeout = () => {};
  sandbox.document.activeElement = activeElement;
  sandbox.document.body.appendChild = el => appended.push(el);
  sandbox.document.createElement = () => {
    const el = makeStubElement();
    el.listeners = {};
    el.addEventListener = (n, f) => { el.listeners[n] = f; };
    el.children = [];
    el.appendChild = c => el.children.push(c);
    el.remove = () => { const i = appended.indexOf(el); if (i >= 0) appended.splice(i, 1); };
    return el;
  };
  const inputEl = { value: inputValue };
  sandbox.document.getElementById = id => (id === 'sessionInput' ? inputEl : makeStubElement());
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return { sandbox, appended, reloads, timers };
}

function run(env, code) { return vm.runInContext(code, env.sandbox); }

const base = { activeElementIsInput: false, terminalFocused: false, hasUnsentDraft: false, idleMs: 60000 };

test('_canAutoReload: true only when fully idle and unfocused', () => {
  const { sandbox } = load();
  assert.equal(sandbox._canAutoReload(base), true);
  assert.equal(sandbox._canAutoReload({ ...base, activeElementIsInput: true }), false);
  assert.equal(sandbox._canAutoReload({ ...base, terminalFocused: true }), false);
  assert.equal(sandbox._canAutoReload({ ...base, hasUnsentDraft: true }), false);
  assert.equal(sandbox._canAutoReload({ ...base, idleMs: 29999 }), false);
  assert.equal(sandbox._canAutoReload({ ...base, idleMs: 30000 }), true);
});

test('no reload when an input is focused', () => {
  const env = load({ activeElement: { tagName: 'TEXTAREA' } });
  run(env, 'state._lastUserActivityAt = Date.now() - 60000');
  env.sandbox._onDaemonVersionChange('v2');
  assert.equal(env.reloads.length, 0);
  assert.equal(env.appended.length, 1);
});

test('no reload when the terminal is focused', () => {
  const el = { tagName: 'TEXTAREA', closest: sel => (sel === '.xterm' ? {} : null) };
  const env = load({ activeElement: el });
  run(env, 'state._lastUserActivityAt = Date.now() - 60000');
  env.sandbox._onDaemonVersionChange('v2');
  assert.equal(env.reloads.length, 0);
  assert.equal(env.appended.length, 1);
});

test('no reload when a draft exists in the session input', () => {
  const env = load({ inputValue: 'half typed' });
  run(env, 'state._lastUserActivityAt = Date.now() - 60000');
  env.sandbox._onDaemonVersionChange('v2');
  assert.equal(env.reloads.length, 0);
  assert.equal(env.appended.length, 1);
});

test('no reload when idle under 30s', () => {
  const env = load();
  run(env, 'state._lastUserActivityAt = Date.now() - 5000');
  env.sandbox._onDaemonVersionChange('v2');
  assert.equal(env.reloads.length, 0);
  assert.equal(env.appended.length, 1);
});

test('reloads immediately when fully idle, no banner', () => {
  const env = load();
  run(env, 'state._lastUserActivityAt = Date.now() - 60000');
  env.sandbox._onDaemonVersionChange('v2');
  assert.equal(env.reloads.length, 1);
  assert.equal(env.appended.length, 0);
});

test('banner created once with required id/text; second change does not stack', () => {
  const env = load();
  run(env, 'state._lastUserActivityAt = Date.now()');
  env.sandbox._onDaemonVersionChange('v2');
  env.sandbox._onDaemonVersionChange('v3');
  assert.equal(env.appended.length, 1);
  assert.equal(env.appended[0].id, 'updateAvailableBanner');
  assert.equal(run(env, 'state._updateAvailable'), 'v3');
  assert.ok(env.timers.some(t => t.ms >= 5000), 're-evaluate timer must be >= 5s');
});

test('banner reloads only on explicit tap', () => {
  const env = load();
  run(env, 'state._lastUserActivityAt = Date.now()');
  env.sandbox._onDaemonVersionChange('v2');
  assert.equal(env.reloads.length, 0);
  const bar = env.appended[0];
  assert.ok(bar, 'banner appended');
  assert.equal(bar.id, 'updateAvailableBanner');
  const btn = bar.children && bar.children[1];
  assert.ok(btn, 'banner has a button');
  btn.listeners.click();
  assert.equal(env.reloads.length, 1);
});
