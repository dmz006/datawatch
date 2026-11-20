'use strict';
// Server picker row lookup — PWA side.
//
// _updatePickerOptionReachability(name) marks the open picker modal's rows
// whose data-server-name equals the server name. It matches on the row's own
// attribute value, not through a CSS attribute selector, so a name holding
// quotes or backslashes can neither break out of a selector nor miss its own
// row. Rows are only patched (data-reachability + the "(unreachable)" suffix),
// never disabled.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-server-picker-selector.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// A picker row: a button carrying data-server-name, with a suffix span child.
function row(name, selectors) {
  const attrs = { 'data-server-name': name, 'data-reachability': 'unknown' };
  const suffix = { textContent: '' };
  return {
    suffix,
    getAttribute: (k) => (k in attrs ? attrs[k] : null),
    setAttribute: (k, v) => { attrs[k] = String(v); },
    hasAttribute: (k) => k in attrs,
    querySelector: (sel) => { selectors.push(sel); return sel === '[data-role="suffix"]' ? suffix : null; },
  };
}

// The document stub answers getElementById('serverPickerModalList') with a
// list whose querySelectorAll returns the rows for the 'button' selector and
// nothing else. The point of this test is "builds no CSS from the name": we
// record every selector the lookup passes and assert at the end that none of
// them embeds a hostile server name.
function run(sandbox, rows, name, selectors) {
  const list = {
    querySelectorAll: (sel) => {
      selectors.push(sel);
      return sel === 'button' ? rows : [];
    },
  };
  const real = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => (id === 'serverPickerModalList' ? list : real.call(sandbox.document, id));
  sandbox.__name = name;
  vm.runInContext('_serverReachability[__name] = false; _updatePickerOptionReachability(__name)', sandbox);
  delete sandbox.__name;
}

const NAMES = [
  'plain',
  'say "hi"',
  'back\\slash',
  'trail\\',
  'quote"\\"mix',
  `a"]; x[b="`,
  "it's",
];

for (const name of NAMES) {
  test(`marks only the row named ${JSON.stringify(name)}`, () => {
    const sandbox = loadApp();
    const selectors = [];
    const target = row(name, selectors);
    const other = row('other', selectors);
    const lookalike = row(name + '\\', selectors);
    run(sandbox, [other, target, lookalike], name, selectors);

    assert.equal(target.getAttribute('data-reachability'), 'down', 'the target row must be marked down');
    assert.ok(target.suffix.textContent.includes('(unreachable)') || target.suffix.textContent.includes('server_dropdown_unreachable'),
      'the target row must gain the unreachable suffix: ' + target.suffix.textContent);
    assert.ok(!target.hasAttribute('disabled'), 'an unreachable row is never disabled');
    assert.equal(other.getAttribute('data-reachability'), 'unknown', 'an unrelated row must be untouched');
    assert.equal(other.suffix.textContent, '');
    assert.equal(lookalike.getAttribute('data-reachability'), 'unknown', 'a near-identical name must not be hit');
    assert.equal(lookalike.suffix.textContent, '');
    assert.ok(selectors.length > 0, 'the lookup went through querySelector(All)');
    for (const sel of selectors) assert.ok(!sel.includes(name), `selector embeds the name: ${sel}`);
  });
}

test('a reachable server clears its row mark', () => {
  const sandbox = loadApp();
  const selectors = [];
  const target = row('ok"name', selectors);
  target.setAttribute('data-reachability', 'down');
  target.suffix.textContent = ' (unreachable)';
  const list = { querySelectorAll: (sel) => (sel === 'button' ? [target] : []) };
  const real = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => (id === 'serverPickerModalList' ? list : real.call(sandbox.document, id));
  sandbox.__name = 'ok"name';
  vm.runInContext('_serverReachability[__name] = true; _updatePickerOptionReachability(__name)', sandbox);
  assert.equal(target.getAttribute('data-reachability'), 'unknown', 'a reachable row must be marked unknown, not down');
  assert.equal(target.suffix.textContent, '', 'the suffix is cleared');
});

test('with no modal open the lookup is a no-op and never throws', () => {
  const sandbox = loadApp();
  const real = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => (id === 'serverPickerModalList' ? null : real.call(sandbox.document, id));
  assert.doesNotThrow(() => vm.runInContext('_serverReachability["x"] = false; _updatePickerOptionReachability("x")', sandbox));
});
