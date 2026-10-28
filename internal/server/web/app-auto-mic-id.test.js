// Auto-attached mic buttons give an id-less textarea a DOM id. That id is a UI
// identifier only, so it comes from a module-level counter and must never touch
// Math.random (CodeQL js/insecure-randomness, alert 714).
// Tests _autoMicTextareaId and the autoAttachMics call site.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._autoMicTextareaId, 'function', '_autoMicTextareaId is not defined -- did it get renamed?');
  return sandbox;
}

test('_autoMicTextareaId returns an id-safe string with the _autoMicTa_ prefix', () => {
  const sandbox = loadAppJS();
  const id = vm.runInContext('_autoMicTextareaId()', sandbox);
  assert.match(id, /^_autoMicTa_[0-9]+$/);
});

test('_autoMicTextareaId differs across calls', () => {
  const sandbox = loadAppJS();
  const ids = [];
  for (let i = 0; i < 50; i++) ids.push(vm.runInContext('_autoMicTextareaId()', sandbox));
  assert.equal(new Set(ids).size, ids.length);
});

test('_autoMicTextareaId does not use Math.random', () => {
  const sandbox = loadAppJS();
  vm.runInContext("Math.random = function () { throw new Error('Math.random must not be called'); }", sandbox);
  const a = vm.runInContext('_autoMicTextareaId()', sandbox);
  const b = vm.runInContext('_autoMicTextareaId()', sandbox);
  assert.notEqual(a, b);
});

test('autoAttachMics assigns ids through the helper, not an inline Math.random', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('window.autoAttachMics = function');
  assert.ok(start > 0, 'autoAttachMics not found');
  const end = src.indexOf('\n};', start);
  const body = src.slice(start, end);
  assert.ok(body.includes('_autoMicTextareaId()'), 'autoAttachMics should call _autoMicTextareaId()');
  assert.ok(!body.includes('Math.random'), 'autoAttachMics must not call Math.random');
});
