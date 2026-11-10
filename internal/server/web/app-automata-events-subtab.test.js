// Automata view — Events sub-tab order and validity.
//
// Checks that the Events sub-tab sits between Automata and Templates in the
// output-tab button row, and that the tab normaliser accepts 'events' as a
// valid sub-tab. The button row is read from the app.js source; the normaliser
// runs in the real app loaded into a sandbox.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const appSource = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

test('Automata tab row lists Events between Automata and Templates', () => {
  const start = appSource.indexOf('<div class="output-tabs automata-tabs-bar">');
  assert.ok(start >= 0, 'automata tab bar is present in app.js');
  const bar = appSource.slice(start, appSource.indexOf('<div style="flex:1;"></div>', start));
  const tabs = [...bar.matchAll(/data-tab="([a-z]+)"/g)].map(m => m[1]);
  assert.deepEqual(tabs, ['automation', 'automata', 'events', 'templates']);
});

test('events is a valid Automata sub-tab for the normaliser', () => {
  const sandbox = loadAppSandbox();
  assert.deepEqual(
    [...vm.runInContext('_AUTOMATA_TABS', sandbox)],
    ['automation', 'automata', 'events', 'templates']
  );
  vm.runInContext("_automataState.tab = 'events'; state._autonomousEnabled = true;", sandbox);
  assert.equal(vm.runInContext('_automataNormalizeTab()', sandbox), 'events');
});
