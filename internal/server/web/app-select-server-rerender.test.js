'use strict';
// selectServer() re-renders the active view after a real picker switch, so
// the picker bar re-injects with the new active chip highlighted.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-select-server-rerender.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const PEER = 'pi-node';
const VIEW_RENDERERS = [
  'renderSessionsView', 'renderAutonomousView', 'renderAlertsView',
  'renderDashboardView', 'renderSettingsView', 'renderObserverView',
];

function load(activeView, activeServer = null) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined');

  // Neutralise side effects; keep the real state mutation.
  sandbox.loadServers = () => {};
  sandbox.connect = () => {};
  sandbox.showToast = () => {};
  sandbox._ensureLocalHostname = () => {};
  sandbox._checkFederatedConnection = () => {};
  sandbox._loadAllServersSessions = () => {};

  // Count calls to each view render function. selectServer's re-render goes
  // through the global name, so replacing the property intercepts it.
  sandbox.__renders = [];
  for (const name of VIEW_RENDERERS) {
    sandbox[name] = () => { sandbox.__renders.push(name); };
  }

  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, label: ${JSON.stringify(PEER)}, enabled: true, url: 'https://x' }] };` +
    `state.activeServer = ${activeServer === null ? 'null' : JSON.stringify(activeServer)};` +
    `state.activeView = ${JSON.stringify(activeView)}; state.sessions = []; state.ws = null;`,
    sandbox,
  );
  return sandbox;
}

test('switching to a peer re-renders the Sessions view', () => {
  const s = load('sessions');
  s.selectServer(PEER);
  assert.deepEqual(s.__renders, ['renderSessionsView']);
});

test('switching to a peer re-renders each picker-bar view', () => {
  const cases = {
    autonomous: 'renderAutonomousView',
    alerts: 'renderAlertsView',
    dashboard: 'renderDashboardView',
    settings: 'renderSettingsView',
    observer: 'renderObserverView',
  };
  for (const [view, fn] of Object.entries(cases)) {
    const s = load(view);
    s.selectServer(PEER);
    assert.deepEqual(s.__renders, [fn], `view ${view}`);
  }
});

test('switching back to local re-renders the active view', () => {
  const s = load('settings', PEER);
  s.selectServer(null);
  assert.deepEqual(s.__renders, ['renderSettingsView']);
});

test('All selection re-renders Sessions, which shows All', () => {
  const s = load('sessions');
  s.selectServer('all');
  assert.deepEqual(s.__renders, ['renderSessionsView']);
});

test('All selection is skipped on Settings and Observer (they hide All)', () => {
  for (const view of ['settings', 'observer']) {
    const s = load(view);
    s.selectServer('all');
    assert.deepEqual(s.__renders, [], `view ${view}`);
  }
});

test('views without a picker bar are not re-rendered', () => {
  for (const view of ['session-detail', 'new', 'unknown-view']) {
    const s = load(view);
    s.selectServer(PEER);
    assert.deepEqual(s.__renders, [], `view ${view}`);
  }
});

test('re-selecting the already-active server is a no-op (no re-render)', () => {
  const s = load('sessions', PEER);
  s.selectServer(PEER);
  assert.deepEqual(s.__renders, []);
});

test('the re-render uses the state.activeView set by navigate()', () => {
  const s = load('sessions');
  vm.runInContext(`state.activeView = 'alerts';`, s);
  s.selectServer(PEER);
  assert.deepEqual(s.__renders, ['renderAlertsView']);
});

// Records every fetch() URL so a test can assert which path a request took.
function recordFetches(s) {
  const urls = [];
  s.fetch = (url) => {
    urls.push(String(url));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ version: '1.0.0' }) });
  };
  return urls;
}

test('About showing a peer: selecting local re-renders About with the local chip active and reads the version locally', async () => {
  const s = load('settings', PEER);
  vm.runInContext(`_settingsTab = 'about';`, s);
  const urls = recordFetches(s);

  s.selectServer(null);
  assert.deepEqual(s.__renders, ['renderSettingsView'], 'About (the Settings render) must re-render');

  // The "local" option (value="") is the selected one; the peer option is not.
  const bar = s._serverPickerBar({});
  const optFor = (name) => {
    const marker = `value="${name == null ? '' : name}" data-base-label="`;
    const start = bar.indexOf(marker);
    assert.ok(start >= 0, `option for "${name}" must be rendered`);
    return bar.slice(bar.lastIndexOf('<option ', start), bar.indexOf('</option>', start));
  };
  const localOpt = optFor('');
  assert.ok(localOpt.includes('selected'), `local option must be selected: ${localOpt}`);
  assert.ok(localOpt.includes('aria-current="true"'), 'active local option must be aria-current');
  assert.ok(!optFor(PEER).includes('selected'), 'peer option must not be selected');

  // About's version read goes to the local path, not /api/proxy/<peer>/.
  s.loadVersionInfo();
  await flushAsync(30);
  assert.ok(urls.includes('/api/health'), `version read must hit /api/health: ${JSON.stringify(urls)}`);
  assert.ok(urls.every(u => !u.startsWith('/api/proxy/')), `no request may use the proxy prefix: ${JSON.stringify(urls)}`);
});

test('re-selecting the peer that is already active makes no re-render and no request', async () => {
  const s = load('settings', PEER);
  vm.runInContext(`_settingsTab = 'about';`, s);
  const urls = recordFetches(s);

  s.selectServer(PEER);
  await flushAsync(30);
  assert.deepEqual(s.__renders, [], 'no view may re-render');
  assert.deepEqual(urls, [], `no request may be made: ${JSON.stringify(urls)}`);
});

test('switching to a peer while Sessions is shown resets the Sessions list before the re-render, and the re-render still runs', () => {
  const s = load('sessions');
  // A populated Sessions list from the previous server.
  vm.runInContext(`state.sessions = [{ id: 'stale-local-session' }];`, s);
  // Record what the Sessions list looks like at the moment it is redrawn.
  const seen = [];
  s.renderSessionsView = () => {
    s.__renders.push('renderSessionsView');
    seen.push(vm.runInContext(`JSON.stringify({ active: state.activeServer, sessions: state.sessions })`, s));
  };

  s.selectServer(PEER);

  assert.equal(vm.runInContext('state.activeServer', s), PEER, 'state.activeServer must equal the peer');
  assert.deepEqual(s.__renders, ['renderSessionsView'], 'Sessions must be re-rendered once');
  assert.deepEqual(JSON.parse(seen[0]), { active: PEER, sessions: [] }, 'Sessions list must be reset before the redraw');
});
