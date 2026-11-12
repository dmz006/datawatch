'use strict';
// Server dropdown option lookup — PWA side.
//
// _updatePickerOptionReachability(name) marks the #serverDropdown options
// whose value equals the server name. It matches on the option's own value,
// not through a CSS attribute selector, so a name holding quotes or
// backslashes can neither break out of a selector nor miss its own option.
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

function opt(name) {
  return { value: name, dataset: { baseLabel: name, reachability: 'unknown' }, textContent: name };
}

// The document stub only answers getElementById('serverDropdown'), whose
// querySelectorAll returns the options for the 'option' selector and
// nothing else. The point of this test is "builds no CSS from the name":
// we record every selector the lookup passes and assert at the end that
// none of them embeds a hostile server name.
function run(sandbox, options, name) {
  const selectors = [];
  const select = {
    querySelectorAll: (sel) => {
      selectors.push(sel);
      return sel === 'option' ? options : [];
    },
  };
  sandbox.document.getElementById = (id) => (id === 'serverDropdown' ? select : null);
  sandbox.__name = name;
  vm.runInContext('_serverReachability[__name] = false; _updatePickerOptionReachability(__name)', sandbox);
  delete sandbox.__name;
  return selectors;
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
  test(`marks only the option named ${JSON.stringify(name)}`, () => {
    const sandbox = loadApp();
    const target = opt(name);
    const other = opt('other');
    const lookalike = opt(name + '\\');
    const selectors = run(sandbox, [other, target, lookalike], name);

    assert.equal(target.dataset.reachability, 'down', 'the target option must be marked down');
    assert.notEqual(target.textContent, name, 'the target option text must gain the unreachable marker');
    assert.equal(other.dataset.reachability, 'unknown', 'an unrelated option must be untouched');
    assert.equal(lookalike.dataset.reachability, 'unknown', 'a near-identical name must not be hit');
    for (const sel of selectors) assert.ok(!sel.includes(name), `selector embeds the name: ${sel}`);
  });
}

test('a reachable server leaves its option alone', () => {
  const sandbox = loadApp();
  const target = opt('ok"name');
  const select = { querySelectorAll: (sel) => (sel === 'option' ? [target] : []) };
  sandbox.document.getElementById = (id) => (id === 'serverDropdown' ? select : null);
  sandbox.__name = 'ok"name';
  vm.runInContext('_serverReachability[__name] = true; _updatePickerOptionReachability(__name)', sandbox);
  assert.equal(target.dataset.reachability, 'unknown', 'a reachable option must be marked unknown, not down');
});
