// Automation sub-tab routes.
//
// Automation is a sub-tab of the Automata view, so every route that used to
// open the Automation main view must land on the Automata tab with the
// Automation sub-tab selected, and a link to one Automation must open that
// item. This test covers the old view name, the link helper, the saved-link
// parser, the disabled gate, the first-visit default and the remember switch.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadView() {
  const els = {};
  const out = { view: '' };
  els.view = makeStubElement(h => { out.view = h; });
  const sandbox = buildSandbox({ byId: els });
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, out, els };
}
const run = (sb, code) => vm.runInContext(code, sb);
const tabState = sb => run(sb, '_automataState.tab');

test("navigate('automation') selects the Automation sub-tab on the Automata view", () => {
  const { sb, out } = loadView();
  sb.switchAutomataTab('templates');
  sb.navigate('automation');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  assert.equal(tabState(sb), 'automation');
  assert.match(out.view, /class="output-tab active" data-tab="automation"/);
  assert.match(out.view, /id="automationPanel"/);
  assert.equal(sb.localStorage.getItem('cs_active_view'), 'autonomous');
});

test("navigate('automation') resets the open Automation like the Automata branch", () => {
  const { sb } = loadView();
  run(sb, "_automationState.detailId = 'a1'");
  sb.navigate('automation');
  assert.equal(run(sb, '_automationState.detailId'), null);
});

test('a history entry that holds the old view name restores the Automation sub-tab', () => {
  const { sb } = loadView();
  sb.switchAutomataTab('automata');
  sb.navigate('automation', null, true);
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  assert.equal(tabState(sb), 'automation');
});

test('opening Automata from the nav button lands on the default sub-tab', () => {
  const { sb } = loadView();
  sb.navigate('autonomous');
  assert.equal(tabState(sb), run(sb, '_AUTOMATA_DEFAULT_TAB'));
  assert.equal(tabState(sb), 'automation');
});

test('a link that targets one Automation selects the sub-tab and opens the item', () => {
  const { sb } = loadView();
  sb.switchAutomataTab('templates');
  run(sb, "globalThis.__opened = []; const _o = openAutomationDetail; openAutomationDetail = function (id) { __opened.push(id + ':' + _automataState.tab + ':' + state.activeView); return _o(id); };");
  sb.openAutomationLink('a7');
  assert.deepEqual(JSON.parse(JSON.stringify(run(sb, '__opened'))), ['a7:automation:autonomous'], 'the sub-tab is selected before the item opens');
  assert.equal(run(sb, '_automationState.detailId'), 'a7');
});

test('a link with no id lands on the Automation list', () => {
  const { sb } = loadView();
  sb.switchAutomataTab('automata');
  sb.openAutomationLink('');
  assert.equal(tabState(sb), 'automation');
  assert.equal(run(sb, '_automationState.detailId'), null);
});

test('the back control of a member page returns to the Automation that holds it', () => {
  const { sb } = loadView();
  sb.switchAutomataTab('automata');
  sb.window._automationReturnId = 'a9';
  sb._backToAutomataList();
  assert.equal(tabState(sb), 'automation');
  assert.equal(run(sb, '_automationState.detailId'), 'a9');
});

test('saved links parse to the list, one Automation, or nothing', () => {
  const { sb } = loadView();
  const route = loc => sb._automationRouteFromLocation(loc);
  assert.equal(route({ hash: '#automation', search: '' }), '');
  assert.equal(route({ hash: '#automation/a%201', search: '' }), 'a 1');
  assert.equal(route({ hash: '', search: '?automation=a2' }), 'a2');
  assert.equal(route({ hash: '#automata', search: '' }), null);
  assert.equal(route({ hash: '', search: '' }), null);
  assert.equal(route({ hash: '' }), null);
});

test('a disabled subsystem sends every Automation route to the Automata sub-tab', () => {
  const { sb, out } = loadView();
  run(sb, 'state._autonomousEnabled = false');
  sb.navigate('automation');
  assert.equal(tabState(sb), 'automata');
  assert.doesNotMatch(out.view, /data-tab="automation"/);
  assert.match(out.view, /id="automataPanel"/);
  sb.openAutomationLink('a1');
  assert.equal(tabState(sb), 'automata');
});

test('boot restores a stored automation view into the sub-tab, and falls back to sessions when disabled', () => {
  const js = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(js, /_initView === 'automation'\)\s*\{\s*_automataState\.tab = 'automation';\s*_initView = state\._autonomousEnabled === false \? 'sessions' : 'autonomous';/);
});

test('the remember switch is off: a chosen sub-tab is not stored and a first visit is Automation', () => {
  const { sb } = loadView();
  assert.equal(run(sb, '_AUTOMATA_REMEMBER_TAB'), false);
  sb.localStorage.setItem('cs_automata_tab', 'templates');
  sb.switchAutomataTab('automata');
  assert.equal(sb.localStorage.getItem('cs_automata_tab'), 'templates', 'switching writes nothing');
  assert.equal(run(sb, '_automataInitialTab()'), 'automation', 'a stored value is ignored');
});

test('the default sub-tab on a first visit is Automation', () => {
  const { sb } = loadView();
  assert.equal(run(sb, '_AUTOMATA_DEFAULT_TAB'), 'automation');
  assert.equal(tabState(sb), 'automation');
});

// Member page back control: an Automaton opened from a member strip carries
// the Automation id through _backToAutomataList and back returns to that
// Automation, while plain list-to-detail navigation still goes to the list.

test('a back from the member Automaton page returns to the Automation, not the list', () => {
  const { sb } = loadView();
  run(sb, "_automationState.detailId = 'a3'");
  const log = [];
  sb.renderPRDDetailView = id => { log.push(['detail', id]); };
  sb.openAutomationMemberPRD('m2aaaaaaaaaaaa');
  assert.deepEqual(log, [['detail', 'm2aaaaaaaaaaaa']]);
  assert.equal(run(sb, 'window._automationReturnId'), 'a3', 'the Automation is remembered after the member page opens');
  // Now the operator taps the back control on the Automaton page.
  const back = [];
  sb.openAutomationDetail = id => { run(sb, '_automationState.detailId = ' + JSON.stringify(id)); back.push(['open', id]); };
  sb._backToAutomataList();
  assert.deepEqual(back, [['open', 'a3']], 'back opens the Automation again');
  assert.equal(run(sb, '_automataState.tab'), 'automation');
  assert.equal(run(sb, '_automationState.detailId'), 'a3');
  assert.equal(run(sb, '_automataDetailId'), null, 'the PRD detail state is cleared');
});

test('the back control of a plain list to detail navigation goes back to the list', () => {
  const { sb } = loadView();
  run(sb, '_automataDetailId = "p7"'); // as renderPRDDetailView would leave it
  assert.equal(run(sb, 'window._automationReturnId || null'), null, 'no Automation was the entry point');
  const opened = [];
  sb.openAutomationDetail = id => { opened.push(id); };
  sb._backToAutomataList();
  assert.deepEqual(opened, [], 'no Automation is reopened');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  assert.equal(run(sb, '_automataDetailId'), null);
});

test('leaving the Automata view clears the member return id', () => {
  const { sb } = loadView();
  run(sb, "_automationState.detailId = 'a5'");
  sb.window._automationReturnId = 'a5'; // as openAutomationMemberPRD sets it
  assert.equal(run(sb, 'window._automationReturnId'), 'a5');
  sb.navigate('sessions');
  assert.equal(run(sb, 'window._automationReturnId'), null, 'a later back cannot resurrect the Automation');
});

test('with no Automation open, a member to Automaton trip is plain navigation', () => {
  const { sb } = loadView();
  assert.equal(run(sb, '_automationState.detailId'), null);
  const log = [];
  sb.renderPRDDetailView = id => { log.push(['detail', id]); };
  sb.openAutomationMemberPRD('m2aaaaaaaaaaaa');
  assert.deepEqual(log, [['detail', 'm2aaaaaaaaaaaa']]);
  assert.equal(run(sb, 'window._automationReturnId'), null, 'nothing to return to');
  const opened = [];
  sb.openAutomationDetail = id => { opened.push(id); };
  sb._backToAutomataList();
  assert.deepEqual(opened, [], 'back goes to the list exactly like plain navigation');
  assert.equal(run(sb, 'state.activeView'), 'autonomous');
  assert.equal(run(sb, '_automataDetailId'), null);
});
