// Pins the daemon-version-change reload policy: never reload under an active
// user; show a single tap-to-reload banner instead.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

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

// ── service-worker controllerchange path (b42f0003 audit) ──────────────
// The service worker is only ever served by the origin that serves THIS
// page — federated peers are reached through the local /api/proxy — so
// switching state.activeServer can never change which SW controls the page.
// controllerchange therefore always means a real new-asset swap of this
// page's assets, and every one of those must still respect _canAutoReload.
// These tests bind the real listener from registerServiceWorker and drive
// it directly, the way a browser would on a take-over.
function loadSw({ hadController = true, activeServer = null, lastActivityMsAgo = 60000 } = {}) {
  const env = load();
  if (activeServer) run(env, `state.activeServer = ${JSON.stringify(activeServer)};`);
  run(env, `state._lastUserActivityAt = Date.now() - ${lastActivityMsAgo};`);
  const ccHandlers = [];
  env.sandbox.navigator.serviceWorker = {
    controller: hadController ? { scriptURL: 'http://localhost/sw.js' } : null,
    addEventListener: (type, fn) => { if (type === 'controllerchange') ccHandlers.push(fn); },
    register: () => Promise.resolve({
      scope: '/',
      update: () => Promise.resolve(),
      addEventListener: () => {},
    }),
  };
  run(env, 'registerServiceWorker();');
  return { ...env, ccHandlers };
}

test('controllerchange (real new-asset swap) reloads immediately when fully idle, no banner', async () => {
  const env = loadSw({ hadController: true, lastActivityMsAgo: 60000 });
  await flushAsync(6);
  assert.equal(env.ccHandlers.length, 1, 'controllerchange listener bound exactly once');
  env.ccHandlers[0]();
  assert.equal(env.reloads.length, 1, 'real swap takes the page over immediately when idle');
  assert.equal(env.appended.length, 0, 'no banner needed on the immediate path');
  env.ccHandlers[0](); // late duplicate event
  assert.equal(env.reloads.length, 1, '_reloadGuard: a duplicate event must not reload again');
});

test('controllerchange while the user is active: no reload, banner + recheck; recheck reloads once idle', async () => {
  const env = loadSw({ hadController: true, lastActivityMsAgo: 1 });
  await flushAsync(6);
  const timersBefore = env.timers.length;
  env.ccHandlers[0]();
  assert.equal(env.reloads.length, 0, '_canAutoReload blocks a reload under an active user');
  assert.equal(env.appended.length, 1, 'banner shown instead');
  assert.equal(env.appended[0].id, 'updateAvailableBanner');
  assert.equal(run(env, 'state._updateAvailable'), true);
  assert.ok(env.timers.length > timersBefore, 'recheck timer scheduled');
  run(env, 'state._lastUserActivityAt = Date.now() - 60000'); // user goes idle
  const recheck = env.timers[env.timers.length - 1];
  assert.equal(recheck.ms, 5000, 'recheck fires after the 5s grace');
  recheck.fn();
  assert.equal(env.reloads.length, 1, 'recheck takes the page over once idle');
});

test('controllerchange on first install (clients.claim) is not an update; the next real swap is', async () => {
  const env = loadSw({ hadController: false, lastActivityMsAgo: 60000 });
  await flushAsync(6);
  env.ccHandlers[0](); // initial claim at install time
  assert.equal(env.reloads.length, 0, 'the install-time claim must not reload the page');
  assert.equal(env.appended.length, 0, '…and it is not an update, so no banner either');
  env.ccHandlers[0](); // a real new-build swap later
  assert.equal(env.reloads.length, 1, 'the second swap is a real new-asset signal and reloads');
});

test('a second registerServiceWorker() must not stack another controllerchange handler', async () => {
  const env = loadSw({ hadController: true, lastActivityMsAgo: 60000 });
  await flushAsync(6);
  assert.equal(env.ccHandlers.length, 1);
  assert.equal(env.reloads.length, 0);
  run(env, 'registerServiceWorker();'); // retry path re-running registration
  await flushAsync(6);
  assert.equal(env.ccHandlers.length, 1, 'no second controllerchange listener is attached');
  assert.equal(env.reloads.length, 0);
});

test('controllerchange while viewing a federated peer is still a real local build change', async () => {
  const env = loadSw({ hadController: true, activeServer: 'peerA', lastActivityMsAgo: 60000 });
  run(env, "state._daemonVersionByServer.peerA = 'v9';"); // peer's version is recorded
  await flushAsync(6);
  env.ccHandlers[0]();
  assert.equal(env.reloads.length, 1, 'the SW is served by this page’s own origin — a take-over is a real asset change even while browsing a peer');
  assert.equal(env.appended.length, 0);
  assert.equal(run(env, "state._daemonVersionByServer.peerA"), 'v9', 'peer bookkeeping untouched');
});

// ── picker switch must never reload (b42f0003 audit) ─────────────────
// selectServer() and _checkFederatedConnection() may reconnect and probe;
// they must never reload the page or touch the update banner.

test('selectServer: switching to a peer and back to local never reloads or banners', () => {
  const env = load();
  env.sandbox.loadServers = () => {};
  env.sandbox.connect = () => {};
  env.sandbox._checkFederatedConnection = () => {};
  env.sandbox._loadAllServersSessions = () => {};
  env.sandbox.showToast = () => {};
  run(env, "selectServer('peerA');");
  assert.equal(run(env, 'state.activeServer'), 'peerA');
  assert.equal(env.reloads.length, 0, 'picker switch must not reload');
  assert.equal(env.appended.length, 0, 'picker switch must not banner');
  assert.equal(run(env, 'typeof state._updateAvailable'), 'undefined', 'no update flag from a picker switch');
  run(env, 'selectServer(null);');
  assert.equal(run(env, 'state.activeServer'), null);
  assert.equal(env.reloads.length, 0);
  run(env, "selectServer('peerA'); selectServer('peerA');"); // re-selecting the active peer is a no-op
  assert.equal(run(env, 'state.activeServer'), 'peerA');
  assert.equal(env.reloads.length, 0, 'repeated selection still never reloads');
  assert.equal(env.appended.length, 0);
});

test('_checkFederatedConnection: a successful probe records data, never reloads or banners', async () => {
  const env = load();
  const fetches = [];
  env.sandbox.fetch = (url) => {
    fetches.push(String(url));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ sessions: [{ full_id: 's1' }] }) });
  };
  run(env, "state.activeServer = 'peerA'; state.activeView = 'settings';");
  run(env, "_checkFederatedConnection('peerA');");
  await flushAsync(8);
  assert.ok(fetches.some(u => u.startsWith('/api/proxy/peerA/api/sessions')), 'probes the peer proxy prefix: ' + JSON.stringify(fetches));
  assert.equal(env.reloads.length, 0, 'a successful peer probe must not reload');
  assert.equal(env.appended.length, 0, '…and not banner');
  assert.equal(run(env, 'state._fedConnStatus'), null, 'settles back to no error status with data in hand');
  assert.equal(run(env, 'state.sessions.length'), 1, 'first page recorded');
});

test('_checkFederatedConnection: a 401 failure surfaces the error status, still never reloads', async () => {
  const env = load();
  env.sandbox.fetch = () => Promise.resolve({ status: 401, text: () => Promise.resolve('federation peer lacks capability: sessions:list') });
  run(env, "state.activeServer = 'peerA'; state.activeView = 'settings';");
  run(env, "_checkFederatedConnection('peerA');");
  await flushAsync(8);
  assert.equal(env.reloads.length, 0, 'a peer auth failure must not reload');
  assert.equal(env.appended.length, 0, '…and not banner');
  const status = run(env, 'state._fedConnStatus');
  assert.ok(status && status.phase === 'error' && String(status.server) === 'peerA', 'error phase recorded for peerA: ' + JSON.stringify(status));
});
