'use strict';
// selectServer() re-renders the active view after a real picker switch, so
// the picker bar re-injects with the new active chip highlighted.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-select-server-rerender.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

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
