// The Setup and policy sub-tab is gone from the Automata view: no sub-tab
// entry, a stored 'setup' id falls back to the default, supervisor loop runs
// and counts render on the Events tab, and no setup card id is left in the
// Automata page markup.
//
//   node --test app-automata-setup-tab-removed.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadApp() {
  const sandbox = buildSandbox({ byId: {}, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

test('the sub-tab list has no setup entry', () => {
  const sandbox = loadApp();
  const ids = vm.runInContext('_AUTOMATION_SUBTABS.map(s => s.id)', sandbox);
  assert.ok(!ids.includes('setup'), ids.join(','));
  assert.ok(ids.includes('inbox'));
});

test('a stored setup id falls back to the default sub-tab', () => {
  const sandbox = loadApp();
  vm.runInContext("_automationState.subtab = 'setup'", sandbox);
  const body = vm.runInContext('_automationBodyHTML()', sandbox);
  assert.equal(vm.runInContext('_automationState.subtab', sandbox), 'inbox');
  assert.ok(!body.includes('data-subtab="setup"'));
});

test('no setup card id remains in the Automata sub-tab markup', () => {
  const sandbox = loadApp();
  const ids = vm.runInContext('_AUTOMATION_SUBTABS.map(s => s.id)', sandbox);
  const cards = ['automationOnboardingReportCard', 'automationMergePipelineCard', 'automationMergeShadowCard',
    'automationReleaseReadinessCard', 'automationRequestWatchCard', 'automationProjectPolicyCard',
    'automationDecomposeLintCard', 'automationPriorityByKindCard', 'automationGroupingTagsCard',
    'automationSupervisorLoopCard'];
  for (const sub of ids) {
    const body = vm.runInContext(`_AUTOMATION_SUBTABS.find(s => s.id === '${sub}').bodyHTML()`, sandbox);
    for (const c of cards) assert.ok(!body.includes(c), `${c} in ${sub}`);
  }
});

test('supervisor loop runs and counts render for the Events tab', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(`supervisorLoopCardHtml({enabled:true,jobs:[{name:'a',style:'s',backend:'b',model:'m',last_outcome:'ok'}]},{runs:[{job:'a',started:'t',outcome:'ok',model:'m',escalations:3}]})`, sandbox);
  assert.ok(html.includes('supervisor-loop-run'));
  assert.ok(html.includes('>3<'));
  const panel = vm.runInContext('automataEventsStatusHtml()', sandbox);
  assert.ok(panel.includes('automationSupervisorLoopCard'));
  assert.ok(panel.includes('automationReleaseReadinessCard'));
});
