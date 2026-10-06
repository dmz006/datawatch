// GH#172 D79 — regression test for _flattenConfigForPatch, the function
// the Config Viewer's "Save" path uses to turn the nested JSON GET
// /api/config returns into the flat dotted-key shape PUT /api/config
// expects. The critical safety property: a leaf still holding the
// server's "***" redaction placeholder must be dropped entirely, never
// sent back as a literal value — PUT /api/config leaves an omitted key's
// existing value untouched, so dropping it is what keeps an unedited
// secret from being overwritten with the mask string itself. Verified
// live against a real daemon during implementation (edited an unrelated
// field, confirmed the real secret survived on disk); this test pins
// the same property so a future refactor can't silently regress it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._flattenConfigForPatch, 'function', '_flattenConfigForPatch is not defined -- did it get renamed?');
  return sandbox;
}

// Returned objects are vm-sandbox-realm values; compare by JSON shape
// rather than assert.deepEqual, which treats cross-realm primitives
// (e.g. the sandbox's own `true`/`null`) as not reference-equal even
// when structurally identical.
function flattenJSON(sandbox, obj) {
  sandbox.__obj = obj;
  const result = vm.runInContext('JSON.stringify(_flattenConfigForPatch(__obj, ""))', sandbox);
  delete sandbox.__obj;
  return JSON.parse(result);
}

test('a masked secret ("***") is dropped entirely, not sent back as a literal', () => {
  const sandbox = loadAppJS();
  const out = flattenJSON(sandbox, { ntfy: { enabled: true, token: '***', topic: 'x' } });
  assert.deepEqual(out, { 'ntfy.enabled': true, 'ntfy.topic': 'x' });
  assert.equal('ntfy.token' in out, false, 'a masked field must never appear in the outgoing patch');
});

test('a masked secret nested several levels deep is also dropped', () => {
  const sandbox = loadAppJS();
  const out = flattenJSON(sandbox, { a: { b: { c: { secret: '***', keep: 'y' } } } });
  assert.deepEqual(out, { 'a.b.c.keep': 'y' });
});

test('nested objects flatten to dotted keys', () => {
  const sandbox = loadAppJS();
  const out = flattenJSON(sandbox, { server: { host: '127.0.0.1', port: 8080 } });
  assert.deepEqual(out, { 'server.host': '127.0.0.1', 'server.port': 8080 });
});

test('arrays are kept as a single leaf value, not recursed into', () => {
  const sandbox = loadAppJS();
  const out = flattenJSON(sandbox, { detection: { prompt_patterns: ['a', 'b', 'c'] } });
  assert.deepEqual(out, { 'detection.prompt_patterns': ['a', 'b', 'c'] });
});

test('a non-masked string identical in shape but different value passes through unchanged', () => {
  const sandbox = loadAppJS();
  // Confirms the check is an exact-match on the literal "***" string,
  // not a fuzzy "looks redacted" heuristic that could false-positive on
  // a legitimate three-char value.
  const out = flattenJSON(sandbox, { x: { y: '**' }, z: { w: '****' } });
  assert.deepEqual(out, { 'x.y': '**', 'z.w': '****' });
});

test('booleans, numbers, and null pass through unchanged', () => {
  const sandbox = loadAppJS();
  const out = flattenJSON(sandbox, { a: { b: true, c: 0, d: null, e: false } });
  assert.deepEqual(out, { 'a.b': true, 'a.c': 0, 'a.d': null, 'a.e': false });
});
