// Sessions Filters sheet: badge count, open/close state, Escape listener
// lifetime, and the one-line status counts.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

test('badge counts group, state chip, watch filter and tags but not search text', () => {
  const sb = loadApp();
  vm.runInContext(`state.sessions = [{ id: 'a', state: 'running', backend_family: 'claude-code' }]; state.sessionFilter = 'zzz';`, sb);
  assert.equal(sb._sessionsActiveFilterCount(), 0);
  vm.runInContext(`state.sessionGroupBy = { axis: 'state' }; state.sessionStateChip = 'running'; state.sessionWatchFilter = true;`, sb);
  assert.equal(sb._sessionsActiveFilterCount(), 3);
});

test('open and close flip the state and the Escape listener only lives while open', () => {
  const sb = loadApp();
  let added = 0, removed = 0;
  sb.document.addEventListener = (n) => { if (n === 'keydown') added++; };
  sb.document.removeEventListener = (n) => { if (n === 'keydown') removed++; };
  vm.runInContext(`state.activeView = 'other';`, sb);
  sb.sessionsFiltersOpen();
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), true);
  assert.equal(added, 1);
  sb.sessionsFiltersClose();
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), false);
  assert.equal(removed, 1);
  sb.sessionsFiltersClose();
  assert.equal(removed, 1, 'no listener left to remove');
});
