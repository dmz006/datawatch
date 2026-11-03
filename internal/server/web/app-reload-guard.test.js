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

// ── Per-server version tracking: switching the picker must never look
// like a daemon update of the server serving this page; only a version
// change observed from the SAME server takes the update path. ────────────
function spyUpdatePath(env) {
  const calls = { loadUi: 0, updates: [] };
  env.sandbox._loadUiConfig = () => { calls.loadUi++; };
  env.sandbox._onDaemonVersionChange = v => calls.updates.push(v);
  return calls;
}

function seedLocal(env, version) {
  run(env, `state._daemonVersion = ${JSON.stringify(version)};
           state._daemonVersionByServer.local = ${JSON.stringify(version)};`);
}

test('first local frame only records (boot-fetch fallback); repeated frames never fire', () => {
  const env = load();
  const calls = spyUpdatePath(env);
  run(env, 'state.activeServer = null'); // local socket at boot
  env.sandbox._daemonVersionNoteFrame('v1');
  assert.equal(calls.loadUi, 0);
  assert.equal(calls.updates.length, 0);
  assert.equal(run(env, 'state._daemonVersion'), 'v1', 'fallback filled state._daemonVersion');
  assert.equal(run(env, 'state._daemonVersionByServer.local'), 'v1');
  env.sandbox._daemonVersionNoteFrame('v1'); // next frame, same version
  assert.equal(calls.loadUi, 0);
  assert.equal(calls.updates.length, 0);
  env.sandbox._daemonVersionNoteFrame('v2'); // genuine local update
  assert.equal(calls.loadUi, 1);
  assert.deepEqual(calls.updates, ['v2']);
  env.sandbox._daemonVersionNoteFrame('v2'); // once-per-version guard
  env.sandbox._daemonVersionNoteFrame('v2');
  assert.equal(calls.loadUi, 1);
  assert.equal(calls.updates.length, 1);
});

test('switching to a server whose recorded version differs records it with no banner and no reload', () => {
  const env = load();
  const calls = spyUpdatePath(env);
  seedLocal(env, 'v1');
  run(env, "state.activeServer = 'peerA';"); // operator picked a federated peer
  env.sandbox._daemonVersionNoteFrame('v9'); // peerA's first frame: different build, new key
  assert.equal(calls.loadUi, 0, 'no _loadUiConfig on a picker switch');
  assert.equal(calls.updates.length, 0, 'no _onDaemonVersionChange on a picker switch');
  assert.equal(env.reloads.length, 0, 'no reload');
  assert.equal(env.appended.length, 0, 'no banner');
  assert.equal(run(env, "state._daemonVersionByServer.peerA"), 'v9', 'peer version recorded under its own key');
  assert.equal(run(env, 'state._daemonVersion'), 'v1', "the page's own daemon version is untouched");
  env.sandbox._daemonVersionNoteFrame('v9'); // peer re-sends the same version
  assert.equal(calls.loadUi, 0);
  assert.equal(calls.updates.length, 0);
});

test('a later different version from that same server does take the update path exactly once', () => {
  const env = load();
  const calls = spyUpdatePath(env);
  seedLocal(env, 'v1');
  run(env, "state.activeServer = 'peerA'; state._daemonVersionByServer.peerA = 'v9';");
  env.sandbox._daemonVersionNoteFrame('v9'); // same version: no-op (baseline)
  env.sandbox._daemonVersionNoteFrame('v10'); // peerA actually updated
  assert.equal(calls.loadUi, 1);
  assert.deepEqual(calls.updates, ['v10']);
  assert.equal(run(env, "state._daemonVersionByServer.peerA"), 'v10');
  env.sandbox._daemonVersionNoteFrame('v10'); // repeated frames, same new version
  env.sandbox._daemonVersionNoteFrame('v10');
  assert.equal(calls.loadUi, 1, 'update path fired exactly once for that');
  assert.equal(calls.updates.length, 1);
  assert.equal(run(env, 'state._daemonVersion'), 'v1', "the page's own daemon version is untouched");
});

test("'all' mode frames belong to the local key, not their own picker value", () => {
  const env = load();
  const calls = spyUpdatePath(env);
  seedLocal(env, 'v1');
  run(env, "state.activeServer = 'all';"); // 'all' stays on the local socket
  env.sandbox._daemonVersionNoteFrame('v1');
  assert.equal(calls.loadUi, 0);
  assert.equal(calls.updates.length, 0);
  assert.equal(run(env, "state._daemonVersionByServer.local"), 'v1');
  env.sandbox._daemonVersionNoteFrame('v2'); // local daemon updated while in 'all' mode
  assert.equal(calls.loadUi, 1);
  assert.deepEqual(calls.updates, ['v2']);
});

test('_bootDaemonVersion records the /api/health version as page-local', () => {
  const env = load();
  const calls = spyUpdatePath(env);
  env.sandbox.fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve({ version: 'boot-v3' }) });
  env.sandbox._bootDaemonVersion();
  return new Promise(resolve => setTimeout(resolve, 0)).then(() => {
    assert.equal(run(env, 'state._daemonVersion'), 'boot-v3');
    assert.equal(run(env, 'state._daemonVersionByServer.local'), 'boot-v3');
    assert.equal(calls.loadUi, 0);
    assert.equal(calls.updates.length, 0);
  });
});

test('_bootDaemonVersion keeps the first local frame that beat it in', () => {
  const env = load();
  const calls = spyUpdatePath(env);
  run(env, 'state.activeServer = null');
  env.sandbox._daemonVersionNoteFrame('v1'); // frame recorded first
  env.sandbox.fetch = () => Promise.resolve({ ok: true, json: () => Promise.resolve({ version: 'boot-v3' }) });
  env.sandbox._bootDaemonVersion(); // late fetch, different version: must not clobber
  return new Promise(resolve => setTimeout(resolve, 0)).then(() => {
    assert.equal(run(env, 'state._daemonVersion'), 'v1');
    assert.equal(run(env, 'state._daemonVersionByServer.local'), 'v1');
    assert.equal(calls.loadUi, 0);
    assert.equal(calls.updates.length, 0);
  });
});
