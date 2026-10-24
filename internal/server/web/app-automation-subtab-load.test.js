// Automation sub-tab loading and phone fit.
//
// The Automation sub-tab loads its list and capacity card only while it is the
// selected sub-tab; switching sub-tabs reuses the Automata list state it
// already holds instead of requesting it again; leaving the Automation
// sub-tab ends its polling. The phone-fit test reads the stylesheet.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

// load builds a sandbox whose fetch answers every request with an empty list
// and records each requested URL in calls.
function load() {
  const calls = [];
  const els = {};
  for (const id of ['view', 'automationPanel', 'automationCapacityCard', 'automataPanel', 'prdCapacityCard']) {
    els[id] = makeStubElement();
  }
  const sandbox = buildSandbox({ byId: els });
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.fetch = url => {
    calls.push(String(url));
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  // The server list is already known, so the picker does not re-render the
  // view when its own request answers; the tests count only sub-tab loads.
  vm.runInContext('state.servers = []', sandbox);
  return { sb: sandbox, calls };
}
const run = (sb, code) => vm.runInContext(code, sb);
const count = (calls, re) => calls.filter(u => re.test(u)).length;
const AUTOMATION = /^\/api\/automations(\?|$)/;
const AUTOMATA_LIST = /^\/api\/autonomous\/prds\?limit=/;

test('the Automation endpoint is not fetched until the sub-tab is selected', async () => {
  const { sb, calls } = load();
  run(sb, "_automataState.tab = 'automata'");
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 1, 'the Automata list loads for the visible sub-tab');
  assert.equal(count(calls, AUTOMATION), 0, 'no Automation request while another sub-tab shows');
  assert.equal(run(sb, '!!(window._listLoaders.automation || window._listLoaders.automation_capacity)'), false, 'no Automation loader exists yet');

  sb.switchAutomataTab('automation');
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATION), 1, 'selecting the sub-tab loads the Automations');
  assert.ok(run(sb, 'window._listLoaders.automation_capacity') && run(sb, 'window._listLoaders.automation'), 'both Automation loaders start with the sub-tab');
});

test('the Templates sub-tab does not fetch the Automation endpoint either', async () => {
  const { sb, calls } = load();
  run(sb, "_automataState.tab = 'templates'");
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATION), 0);
  assert.equal(count(calls, /^\/api\/autonomous\/templates/), 1);
});

test('a tab switch does not call the Automata list endpoint again', async () => {
  const { sb, calls } = load();
  run(sb, "_automataState.tab = 'automata'");
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 1);

  sb.switchAutomataTab('automation');
  await flushAsync(30);
  sb.switchAutomataTab('automata');
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 1, 'Automata -> Automation -> Automata reuses the list');

  sb.switchAutomataTab('templates');
  await flushAsync(30);
  sb.switchAutomataTab('automata');
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 1, 'Automata -> Templates -> Automata reuses the list');
});

test('a switch repaints the Automata list from the cached state', async () => {
  const { sb } = load();
  run(sb, "_automataState.tab = 'automata'");
  sb.renderAutonomousView();
  await flushAsync(30);
  run(sb, "globalThis.__paints = 0; const _p = window._listLoaders.automata.paint; window._listLoaders.automata.paint = function () { __paints++; return _p.apply(this, arguments); };");
  sb.switchAutomataTab('templates');
  await flushAsync(30);
  const before = run(sb, '__paints');
  sb.switchAutomataTab('automata');
  assert.equal(run(sb, '__paints') - before, 1, 'one repaint, no new load');
});

test('the Automata list loads when no list was fetched yet', async () => {
  const { sb, calls } = load();
  // The default sub-tab is Automation, so the Automata list was never fetched.
  assert.equal(run(sb, '_automataState.tab'), 'automation');
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 0);
  sb.switchAutomataTab('automata');
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 1);
});

test('leaving the Automation sub-tab stops its polling and selecting it starts it again', async () => {
  const { sb, calls } = load();
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation.state.stopped'), false);
  sb.switchAutomataTab('automata');
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation.state.stopped'), true);
  assert.equal(run(sb, 'window._listLoaders.automation_capacity.state.stopped'), true);
  assert.equal(run(sb, 'window._listLoaders.automation.state.pollTimer'), null, 'no refresh timer outside the sub-tab');
  const before = count(calls, AUTOMATION);
  sb.switchAutomataTab('automation');
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATION) - before, 1);
  assert.equal(run(sb, 'window._listLoaders.automation.state.stopped'), false);
});

test('a refresh while another sub-tab shows does not touch the Automation endpoint', async () => {
  const { sb, calls } = load();
  run(sb, "_automataState.tab = 'automata'; state.activeView = 'autonomous'");
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(run(sb, '_automationTabActive()'), false);
  run(sb, "if (_automationTabActive()) refreshAutomationView(); _refreshOverdueLoaders();");
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATION), 0);
});

test('phone fit: the tab bar wraps and shrinks, with 40 px tap targets and no page scroll', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const start = css.indexOf('@media (max-width: 600px), (pointer: coarse) {\n  .automata-tabs-bar {');
  assert.ok(start > 0, 'phone block for .automata-tabs-bar exists');
  const block = css.slice(start, css.indexOf('\n}\n', start) + 3);
  assert.match(block, /\.automata-tabs-bar \{[^}]*flex-wrap: wrap;/, 'action buttons wrap instead of scrolling');
  assert.match(block, /\.automata-tabs-bar \{[^}]*overflow: visible;/, 'nothing is clipped or scrolled in the bar');
  assert.match(block, /\.automata-tabs-bar \{[^}]*max-width: 100%;/, 'the bar never exceeds the page width');
  const tab = block.match(/\.automata-tabs-bar \.output-tab \{([^}]*)\}/);
  assert.ok(tab, 'tab rule');
  assert.match(tab[1], /min-height: 40px;/);
  assert.match(tab[1], /min-width: 0;/);
  assert.match(tab[1], /flex: 0 1 auto;/, 'tabs may shrink');
  assert.match(tab[1], /text-overflow: ellipsis;/);
  const btn = block.match(/\.automata-tabs-bar \.automata-action-btn \{([^}]*)\}/);
  assert.ok(btn, 'action button rule');
  assert.match(btn[1], /min-width: 40px;/);
  assert.match(btn[1], /min-height: 40px;/);
  assert.match(block, /\.automata-tabs-bar \.btn-primary \{[^}]*min-height: 40px;/, 'the New button reaches 40 px');
});

test('phone fit: three tabs and the action buttons fit 304 px of bar width by their rules', () => {
  // The tab group needs about 250 px at 12 px type with 8 px side padding; the
  // rest of the 304 px bar (320 px less 2 x 8 px padding) cannot hold a 40 px
  // button row, so the buttons wrap. This guards the padding the estimate uses.
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.match(css, /\.automata-tabs-bar \.output-tab \{[^}]*padding: 6px 8px;/);
});
