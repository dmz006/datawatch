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

test('summary line is one row with tappable chips that set or clear the state filter', () => {
  const sb = loadApp();
  vm.runInContext(`state.sessionStateChip = 'running';`, sb);
  const html = sb.renderSessionsSummaryBar([{ state: 'running' }, { state: 'running' }, { state: 'waiting_input' }]);
  assert.match(html, /setSessionStateChip\('all'\)/);
  assert.match(html, /setSessionStateChip\('waiting_input'\)/);
  assert.match(html, />sessions 3</);
});

test('the sheet carries exactly one Group by control, offering discovered placement axes', () => {
  const sb = loadApp();
  vm.runInContext(`
    state.activeView = 'sessions';
    state.sessions = [{ id: 'a', state: 'running', system_tags: ['compute:gpu-1'] }];
    state._sessionsSheetOpen = true;
  `, sb);
  const html = sb._sessionsSheetHTML({ groupBy: null, stateBadges: '', llmBadges: '', tagPanel: '', tagHelp: '' });
  assert.match(html, /<select id="sessions-group-select"/);
  assert.equal((html.match(/<select/g) || []).length, 1, 'exactly one <select> in the sheet (the Group by control)');
  assert.ok(!html.includes('group-axis-control'), 'the removed second placement control is gone');
  const sel = html.match(/<select id="sessions-group-select"[\s\S]*?<\/select>/)[0];
  assert.ok(sel.includes('value="tag|compute:"'), 'placement axis the sessions carry is offered in the one picker');
  // The one picker is the shared control: it reads state.sessionGroupBy.
  vm.runInContext(`state.sessionGroupBy = { axis: 'tag', tagPrefix: 'compute:' };`, sb);
  const cur = sb.renderSessionsGroupPicker(vm.runInContext('state.sessionGroupBy', sb));
  assert.ok(cur.includes('value="tag|compute:" selected'), 'current placement grouping selected inside the same dropdown');
});
