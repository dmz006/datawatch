'use strict';
// Server picker chip lookup — PWA side.
//
// _updatePickerChipReachability(name) finds the picker-bar buttons whose
// data-server-name equals the server name. It matches on the dataset value,
// not through a CSS attribute selector, so a name holding quotes or
// backslashes can neither break out of a selector nor miss its own chip.
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

function chip(name) {
  return { dataset: { serverName: name }, style: {}, title: '', textContent: name };
}

// The document stub only answers the one selector the lookup may use: the
// picker-bar buttons. Any selector that embeds the server name throws, which
// is what keeps this test honest about "builds no CSS from the name".
function run(sandbox, buttons, name) {
  const selectors = [];
  sandbox.document.querySelectorAll = (sel) => {
    selectors.push(sel);
    return sel === '.server-picker-bar button' ? buttons : [];
  };
  sandbox.__name = name;
  vm.runInContext('_serverReachability[__name] = false; _updatePickerChipReachability(__name)', sandbox);
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
  test(`marks only the chip named ${JSON.stringify(name)}`, () => {
    const sandbox = loadApp();
    const target = chip(name);
    const other = chip('other');
    const lookalike = chip(name + '\\');
    const selectors = run(sandbox, [other, target, lookalike], name);

    assert.equal(target.style.opacity, '0.45');
    assert.ok(target.textContent.endsWith('⚠'));
    assert.equal(other.style.opacity, undefined);
    assert.equal(lookalike.style.opacity, undefined);
    for (const sel of selectors) assert.ok(!sel.includes(name), `selector embeds the name: ${sel}`);
  });
}

test('a reachable server leaves its chip alone', () => {
  const sandbox = loadApp();
  const target = chip('ok"name');
  sandbox.document.querySelectorAll = () => [target];
  sandbox.__name = 'ok"name';
  vm.runInContext('_serverReachability[__name] = true; _updatePickerChipReachability(__name)', sandbox);
  assert.equal(target.style.opacity, undefined);
});
