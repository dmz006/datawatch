// GH#182 — regression test for parseAlertDeepLinkId (app.js), the
// extraction step behind the alert deep link (web+datawatch://alert/<id>
// via the Web App Manifest protocol_handlers, or a plain ?alert=<id> link
// from e.g. a push notification's click-action).
//
// The two input shapes it must distinguish:
//  1. A bare id ("?alert=abc123") -- a plain link passes this directly.
//  2. A full escaped URI ("?alert=web%2Bdatawatch%3A%2F%2Falert%2Fabc123")
//     -- the protocol_handlers spec replaces its url template's %s with
//     the full URI that was navigated to, already decoded once by
//     URLSearchParams.get() by the time it reaches this function, so what
//     this function actually sees is "web+datawatch://alert/abc123".

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.parseAlertDeepLinkId, 'function', 'parseAlertDeepLinkId is not defined -- did it get renamed?');
  return sandbox;
}

function callStr(sandbox, arg) {
  sandbox.__arg = arg;
  const result = vm.runInContext('parseAlertDeepLinkId(__arg)', sandbox);
  delete sandbox.__arg;
  return result;
}

test('parseAlertDeepLinkId returns a bare id unchanged', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'abc123'), 'abc123');
});

test('parseAlertDeepLinkId extracts the id from a protocol_handlers full URI', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'web+datawatch://alert/abc123'), 'abc123');
});

test('parseAlertDeepLinkId decodes a percent-encoded id segment', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'web+datawatch://alert/abc%20123'), 'abc 123');
});

test('parseAlertDeepLinkId stops the id at a trailing query or fragment', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'web+datawatch://alert/abc123?x=1'), 'abc123');
  assert.equal(callStr(sandbox, 'web+datawatch://alert/abc123#frag'), 'abc123');
});

test('parseAlertDeepLinkId returns "" for empty input', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, ''), '');
  assert.equal(callStr(sandbox, null), '');
});

test('parseAlertDeepLinkId returns "" for a URI with no /alert/ path', () => {
  const sandbox = loadAppJS();
  assert.equal(callStr(sandbox, 'web+datawatch://session/abc123'), '');
});
