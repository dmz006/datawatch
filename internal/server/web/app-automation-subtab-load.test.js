// Automation sub-tab loading and phone fit.
//
// The Automation sub-tab loads its list (Automations), capacity card
// (Resources) and progress card (Progress) only while their second-row
// sub-tab is the selected one; switching sub-tabs reuses the Automata
// list state it already holds instead of requesting it again; leaving the
// Automation sub-tab ends its polling. The phone-fit test reads the stylesheet.

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
  for (const id of ['view', 'automationPanel', 'automationCapacityCard', 'progressCard', 'automataPanel', 'prdCapacityCard']) {
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
  return { sb: sandbox, calls, els };
}
const run = (sb, code) => vm.runInContext(code, sb);
const count = (calls, re) => calls.filter(u => re.test(u)).length;
const AUTOMATION = /^\/api\/automations(\?|$)/;
const AUTOMATA_LIST = /^\/api\/autonomous\/prds\?limit=/;
const CAPACITY = /^\/api\/capacity(\?|$)/;
const PROGRESS = /^\/api\/autonomous\/progress(\?|$)/;

test('the Automation endpoint is not fetched until the sub-tab is selected', async () => {
  const { sb, calls } = load();
  run(sb, "_automataState.tab = 'automata'");
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATA_LIST), 1, 'the Automata list loads for the visible sub-tab');
  assert.equal(count(calls, AUTOMATION), 0, 'no Automation request while another sub-tab shows');
  assert.equal(count(calls, PROGRESS), 0, 'no progress card load while another sub-tab shows');
  assert.equal(run(sb, '!!(window._listLoaders.automation || window._listLoaders.automation_capacity)'), false, 'no Automation loader exists yet');

  sb.switchAutomataTab('automation');
  await flushAsync(30);
  assert.equal(run(sb, '_automationState.subtab'), 'inbox', 'the sub-tab opens on Inbox');
  assert.equal(count(calls, AUTOMATION), 0, 'the Inbox does not load the Automations list');
  assert.equal(count(calls, PROGRESS), 0, 'the progress card waits for the Progress sub-tab');
  // The Automata list fetch carries its own best-effort /api/capacity call,
  // so the capacity card's load is told apart by the count moving.
  const capBefore = count(calls, CAPACITY);

  sb.switchAutomationSubtab('automations');
  await flushAsync(30);
  assert.equal(count(calls, AUTOMATION), 1, 'selecting Automations loads the Automations');
  assert.equal(count(calls, PROGRESS), 0, 'Automations requests no progress');
  assert.equal(count(calls, CAPACITY), capBefore, 'Inbox and Automations request no capacity card');
  assert.ok(run(sb, 'window._listLoaders.automation'), 'the Automations loader starts with its sub-tab');

  sb.switchAutomationSubtab('resources');
  await flushAsync(30);
  assert.ok(count(calls, CAPACITY) > capBefore, 'selecting Resources loads the capacity card');
  assert.equal(count(calls, PROGRESS), 0, 'Resources requests no progress');
  assert.ok(run(sb, 'window._listLoaders.automation'), 'the Automations loader keeps its state after the switch');
  assert.ok(run(sb, 'window._listLoaders.automation_capacity'), 'the Resources loader starts with its sub-tab');

  sb.switchAutomationSubtab('progress');
  await flushAsync(30);
  assert.ok(count(calls, PROGRESS) >= 1, 'selecting Progress loads the progress card');
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
  // The Automations loader starts when the operator selects Automations.
  sb.switchAutomationSubtab('automations');
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation.state.stopped'), false);
  // The Resources loaders start when the operator selects Resources.
  sb.switchAutomationSubtab('resources');
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation_capacity.state.stopped'), false, 'the Resources loader runs while Resources is selected');
  assert.equal(run(sb, 'window._listLoaders.automation.state.stopped'), true, 'the Automations loader stops when its sub-tab is left');
  sb.switchAutomataTab('automata');
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation.state.stopped'), true);
  assert.equal(run(sb, 'window._listLoaders.automation_capacity.state.stopped'), true);
  assert.equal(run(sb, 'window._listLoaders.automation.state.pollTimer'), null, 'no refresh timer outside the sub-tab');
  const before = count(calls, AUTOMATION);
  sb.switchAutomataTab('automation');
  sb.switchAutomationSubtab('automations');
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

// The Automata and Templates sub-tabs never request the progress endpoint.
// (The Automata list fetch does carry a best-effort /api/capacity call: it is
// the budget and admission data for the list cards, not the capacity card,
// which lives on the Automation sub-tab only. The progress card has no such
// second consumer and must therefore load with the Automation sub-tab alone.)
test('the progress card loads with the Progress sub-tab only', async () => {
  const { sb, calls } = load();
  run(sb, "_automataState.tab = 'automata'");
  sb.renderAutonomousView();
  await flushAsync(30);
  assert.equal(count(calls, PROGRESS), 0, 'the Automata sub-tab requests no progress');
  sb.switchAutomataTab('templates');
  await flushAsync(30);
  assert.equal(count(calls, PROGRESS), 0, 'the Templates sub-tab requests no progress');
  sb.switchAutomataTab('automation');
  await flushAsync(30);
  assert.equal(count(calls, PROGRESS), 0, 'the Inbox sub-tab requests no progress');
  sb.switchAutomationSubtab('automations');
  await flushAsync(30);
  assert.equal(count(calls, PROGRESS), 0, 'the Automations sub-tab requests no progress');
  sb.switchAutomationSubtab('resources');
  await flushAsync(30);
  assert.equal(count(calls, PROGRESS), 0, 'the Resources sub-tab requests no progress');
  assert.ok(count(calls, CAPACITY) >= 1, 'the capacity card loads with the Resources sub-tab');
  sb.switchAutomationSubtab('progress');
  await flushAsync(30);
  assert.ok(count(calls, PROGRESS) >= 1, 'selecting Progress requests the progress card');
});

test('leaving the Automation sub-tab stops its loaders and starts no 1-second tick', async () => {
  const { sb, els } = load();
  const intervals = [];
  sb.setInterval = (fn, ms) => { intervals.push(ms); return intervals.length; };
  sb.clearInterval = () => {};
  sb.renderAutonomousView(); // the default sub-tab is Inbox
  await flushAsync(30);
  // The list cards poll on a setTimeout chain, not a ticking interval: none
  // of the intervals the sub-tab started is the per-second "last updated" tick.
  assert.ok(intervals.every(ms => ms !== 1000), 'no 1 s tick interval started: ' + JSON.stringify(intervals));
  // The Inbox loaders exist and are meant to poll while the tab is shown.
  assert.ok(run(sb, 'window._listLoaders.automation_triage'), 'the Inbox triage loader starts with its sub-tab');
  assert.equal(run(sb, 'window._listLoaders.automation_triage.state.stopped'), false, 'the loader is active while its tab shows');
  sb.switchAutomataTab('automata'); // leaving the Automation tab ends its polling
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation_triage.state.stopped'), true, 'leaving the tab stops the loader');
  sb.switchAutomataTab('templates');
  await flushAsync(30);
  assert.equal(run(sb, 'window._listLoaders.automation_triage.state.stopped'), true, 'the loader stays stopped off the tab');
  assert.doesNotMatch(els.view.innerHTML, /id="progressCard"/, 'no progress slot off the Automation sub-tab');
  assert.doesNotMatch(els.view.innerHTML, /id="automationCapacityCard"/, 'no capacity slot off the Automation sub-tab');
});

test('a progress fetch that lands after the operator left the Automation sub-tab paints nothing', async () => {
  const { sb, els } = load();
  let resolveProgress;
  const pending = new Promise(res => { resolveProgress = res; });
  const done = () => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  sb.fetch = url => {
    const u = String(url);
    if (u.startsWith('/api/autonomous/progress') && !u.includes('trend')) return pending;
    return done();
  };
  sb.renderAutonomousView(); // default sub-tab is Inbox, so the fetch does not start yet
  sb.switchAutomationSubtab('progress'); // Progress's mount starts it while it is pending
  await flushAsync(30);
  const paintedAtStart = els.progressCard.innerHTML;
  assert.notEqual(paintedAtStart, '', 'the slot showed a state on the Progress sub-tab');
  sb.switchAutomataTab('automata');
  await flushAsync(30);
  // The late answer carries a body the loader rejects (not an object); the point
  // is what happens to the slot after, not which state the machine lands in.
  resolveProgress({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(null), text: () => Promise.resolve('null') });
  await flushAsync(30);
  assert.equal(els.progressCard.innerHTML, paintedAtStart, 'the late fetch must not write into the slot');
  assert.equal(els.progressCard.style.display, 'none', 'the slot is hidden off the Automation sub-tab');
});

test('phone fit: the tab bar scrolls sideways, with 40 px tap targets and no page scroll', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const start = css.indexOf('@media (max-width: 600px), (pointer: coarse) {\n  .automata-tabs-bar,');
  assert.ok(start > 0, 'phone block for .automata-tabs-bar exists');
  const block = css.slice(start, css.indexOf('\n}\n', start) + 3);
  assert.match(block, /flex-wrap: nowrap;/, 'the tabs never wrap');
  assert.match(block, /overflow-x: auto;/, 'the strip scrolls sideways');
  assert.match(block, /max-width: 100%;/, 'the bar never exceeds the page width');
  assert.match(block, /mask-image: linear-gradient/, 'an edge fade shows more tabs exist');
  assert.match(block, /\.header-title \{[^}]*font-size: 14px;/, 'the header title shrinks on phones');
  const tab = block.match(/\.output-tab,?\s*\.projects-tabs-bar \.output-tab \{([^}]*)\}/);
  assert.ok(tab, 'tab rule');
  assert.match(tab[1], /min-height: 40px;/);
  assert.match(tab[1], /flex: 0 0 auto;/, 'tabs keep their width and the strip scrolls');
  const btn = block.match(/\.automata-tabs-bar \.automata-action-btn \{([^}]*)\}/);
  assert.ok(btn, 'action button rule');
  assert.match(btn[1], /min-width: 40px;/);
  assert.match(btn[1], /min-height: 40px;/);
  assert.match(block, /\.automata-tabs-bar \.btn-primary \{[^}]*min-height: 40px;/, 'the New button reaches 40 px');
});

test('tab strip helper: one shared reveal is guarded for the node stub', () => {
  const js = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(js, /function _tabStripReveal\(stripEl\)/);
  assert.match(js, /function _automationSubtabsReveal\(\) \{\s*try \{ _tabStripReveal\(/);
});
