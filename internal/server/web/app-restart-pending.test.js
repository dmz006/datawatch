// A config save whose reply lists requires_restart keys marks those keys as
// waiting for a daemon restart; keys applied live add nothing to the badge.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function call(sandbox, expr, arg) {
  sandbox.__arg = arg;
  const result = vm.runInContext(expr, sandbox);
  delete sandbox.__arg;
  return result;
}

test('restartKeysFromPut reads requires_restart from a PUT /api/config reply', () => {
  const sb = loadAppJS();
  assert.equal(typeof sb.restartKeysFromPut, 'function', 'restartKeysFromPut is not defined');
  assert.deepEqual(Array.from(call(sb, 'restartKeysFromPut(__arg)', { status: 'ok', requires_restart: ['server.port'] })), ['server.port']);
  assert.deepEqual(Array.from(call(sb, 'restartKeysFromPut(__arg)', { status: 'ok', applied: ['a.b'] })), []);
  assert.deepEqual(Array.from(call(sb, 'restartKeysFromPut(__arg)', null)), []);
});

test('noteRestartPending keeps a de-duplicated set and labels the badge with its size', () => {
  const sb = loadAppJS();
  assert.equal(typeof sb.noteRestartPending, 'function', 'noteRestartPending is not defined');
  assert.equal(call(sb, 'noteRestartPending(__arg)', ['server.port']), 1);
  assert.equal(call(sb, 'noteRestartPending(__arg)', ['server.port', 'mcp.sse_port']), 2);
  assert.equal(call(sb, 'noteRestartPending(__arg)', []), 2);
});
