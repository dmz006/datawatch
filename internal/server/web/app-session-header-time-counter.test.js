// The session detail header no longer carries a running "time since last output"
// counter: no markup in the header of an active session, a live session update for
// the open session that runs without that element, the two helpers that drove it
// gone from the app's global scope, and its title string gone from every locale.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const WEB = __dirname;
const LOCALES = ['de', 'en', 'es', 'fr', 'ja'];
const REMOVED_KEY = 'session_last_activity_title';

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

function sess(extra) {
  return Object.assign({
    full_id: 'host-aaaa', id: 'aaaa', state: 'running', task: 'a task',
    updated_at: new Date().toISOString(),
    last_channel_event_at: new Date().toISOString(),
  }, extra || {});
}

function openDetail(sb, s) {
  run(sb, `state.sessions = ${JSON.stringify([s])}; state.activeSession = '${s.full_id}'; state.activeView = 'session-detail';`);
  sb.renderSessionDetail(s.full_id);
}

test('the detail header of an active session has no last-activity markup', () => {
  const sb = loadApp();
  openDetail(sb, sess());
  const html = sb.__writes.join('\n');
  assert.match(html, /detail-state-badge/, 'the detail header was drawn');
  assert.doesNotMatch(html, /session-last-activity/);
  assert.doesNotMatch(html, /data-last-activity/);
});

test('a session update for the open session runs without the last-activity element', () => {
  const sb = loadApp();
  const s = sess();
  openDetail(sb, s);
  assert.doesNotThrow(() => {
    sb.handleMessage({ type: 'session_state', data: { session: Object.assign({}, s, { last_channel_event_at: new Date().toISOString() }) } });
  });
  assert.equal(run(sb, 'state.sessions.length'), 1);
});

test('the clock and indicator helpers are not defined in the global scope', () => {
  const sb = loadApp();
  assert.equal(run(sb, "typeof updateLastActivityClocks"), 'undefined');
  assert.equal(run(sb, "typeof refreshLastActivityIndicator"), 'undefined');
  assert.equal(run(sb, "typeof timeAgo"), 'function', 'the shared relative-time helper stays');
});

test('the removed title key is absent from every locale bundle', () => {
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', `${loc}.json`), 'utf8'));
    assert.ok(!(REMOVED_KEY in bundle), `${loc}.json still has ${REMOVED_KEY}`);
  }
});
