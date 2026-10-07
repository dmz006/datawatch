// BL317 — per-row server attribution. The audit (2026-10-07) found this
// badge existed only on Sessions rows (`.server-badge`, keyed on
// `sess.server`) despite PRDs and Alerts both having real aggregated
// "all servers" endpoints (`/api/autonomous/prds/aggregated`,
// `/api/alerts/aggregated`) that already tag each item with a `server`
// field server-side (internal/server/bl312_aggregated.go). This pins the
// PWA-side gap being closed: renderPRDRow and the (newly hoisted,
// directly-testable) renderAlertCard now render the same badge.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.renderPRDRow, 'function', 'renderPRDRow is not defined -- did it get renamed?');
  assert.equal(typeof sandbox.renderAlertCard, 'function', 'renderAlertCard is not defined -- did it get renamed or stay a closure?');
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

test('renderPRDRow shows no server badge for a local PRD', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDRow', { id: 'p1', title: 'local prd', status: 'draft', server: 'local' });
  assert.ok(!html.includes('server-badge'), 'local PRD should not get a server badge');
});

test('renderPRDRow shows no server badge when server is absent (single-server mode response)', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDRow', { id: 'p1', title: 'no server field', status: 'draft' });
  assert.ok(!html.includes('server-badge'));
});

test('renderPRDRow shows the server badge with the server name when aggregated from a remote peer', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDRow', { id: 'p1', title: 'remote prd', status: 'running', server: 'pi-node' });
  assert.ok(html.includes('server-badge'), 'expected a server-badge span');
  assert.ok(html.includes('pi-node'), 'expected the server name to appear in the badge');
});

test('renderAlertCard shows no server badge for a local alert', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderAlertCard', { id: 'a1', level: 'info', title: 't', body: 'b', created_at: new Date().toISOString(), server: 'local' }, '', false, []);
  assert.ok(!html.includes('server-badge'));
});

test('renderAlertCard shows the server badge for an alert aggregated from a remote peer', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderAlertCard', { id: 'a2', level: 'warn', title: 't', body: 'b', created_at: new Date().toISOString(), server: 'pi-node' }, '', false, []);
  assert.ok(html.includes('server-badge'));
  assert.ok(html.includes('pi-node'));
});

test('renderAlertCard still renders the quick-reply dropdown via the explicit cmds param (not a closure capture anymore)', () => {
  const sandbox = loadAppJS();
  const cmds = [{ name: 'Yes', command: 'yes' }];
  const html = call(sandbox, 'renderAlertCard',
    { id: 'a3', level: 'info', title: 't', body: 'b', created_at: new Date().toISOString(), session_id: 'sess-1' },
    'waiting_input', true, cmds);
  assert.ok(html.includes('quick-cmd-select'), 'expected the quick-reply dropdown to still render with an explicit cmds array');
  assert.ok(html.includes('Yes'));
});
