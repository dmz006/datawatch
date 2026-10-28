// The session detail header no longer carries a watch button. The watch toggle
// stays on each session list card, the toggle itself still persists the watched
// set, and the sessions list still narrows to watched sessions when the watch
// filter is on.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const WEB = __dirname;
const STORAGE_KEY = 'cs_sessions_watched';

function loadApp() {
  const writes = [];
  const view = makeStubElement(html => writes.push(html));
  const sandbox = buildSandbox();
  sandbox.document.getElementById = id => (id === 'view' ? view : makeStubElement());
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  // An open socket, so subscribing for the session output does not raise the
  // connection error toast.
  vm.runInContext('state.ws = { readyState: 1, send() {} }; WebSocket.OPEN = 1;', sandbox);
  sandbox.__writes = writes;
  return sandbox;
}

function run(sb, code) { return vm.runInContext(code, sb); }

function sess(id, extra) {
  return Object.assign({
    full_id: `host-${id}`, id, state: 'running', task: `task ${id}`,
    updated_at: new Date().toISOString(),
  }, extra || {});
}

function openDetail(sb, s) {
  run(sb, `state.sessions = ${JSON.stringify([s])}; state.activeSession = '${s.full_id}'; state.activeView = 'session-detail';`);
  sb.renderSessionDetail(s.full_id);
}

function openList(sb, sessions) {
  run(sb, `state.sessions = ${JSON.stringify(sessions)}; state.activeView = 'sessions'; state.sessionStateChip = 'all';`);
  sb.renderSessionsView();
}

test('the detail header of an open session has no button calling toggleSessionWatch', () => {
  const sb = loadApp();
  openDetail(sb, sess('aaaa'));
  const html = sb.__writes.join('\n');
  assert.match(html, /detail-state-badge/, 'the detail header was drawn');
  assert.doesNotMatch(html, /toggleSessionWatch\(/);
});

test('a session list card still renders the watch button', () => {
  const sb = loadApp();
  const s = sess('aaaa');
  run(sb, `state.sessions = ${JSON.stringify([s])};`);
  const html = sb.sessionCard(s, 0, 1);
  assert.ok(html.includes(`toggleSessionWatch('${s.full_id}')`), 'the card calls toggleSessionWatch with its id');
});

test('toggleSessionWatch adds and removes the id and writes the watched key', () => {
  const sb = loadApp();
  run(sb, "state.activeView = 'dashboard';");
  const id = 'host-aaaa';
  const stored = () => JSON.parse(sb.localStorage.getItem(STORAGE_KEY));

  sb.toggleSessionWatch(id);
  assert.equal(run(sb, `state.watchedSessions.has('${id}')`), true);
  assert.deepEqual(stored(), [id]);

  sb.toggleSessionWatch(id);
  assert.equal(run(sb, `state.watchedSessions.has('${id}')`), false);
  assert.deepEqual(stored(), []);
});

test('with the watch filter on, the sessions list shows only watched sessions', () => {
  const sb = loadApp();
  const watched = sess('aaaa');
  const other = sess('bbbb');
  run(sb, `state.watchedSessions = new Set(['${watched.full_id}']);`);

  run(sb, 'state.sessionWatchFilter = false;');
  sb.__writes.length = 0;
  openList(sb, [watched, other]);
  let html = sb.__writes.join('\n');
  assert.ok(html.includes(`data-full-id="${watched.full_id}"`));
  assert.ok(html.includes(`data-full-id="${other.full_id}"`));

  run(sb, 'state.sessionWatchFilter = true;');
  sb.__writes.length = 0;
  sb.renderSessionsView();
  html = sb.__writes.join('\n');
  assert.ok(html.includes(`data-full-id="${watched.full_id}"`), 'the watched session is listed');
  assert.ok(!html.includes(`data-full-id="${other.full_id}"`), 'the unwatched session is hidden');
});
