// Automata tabs after the Events stream took over the retry, check and
// maintenance rows:
//
//   - the infrastructure retries card is gone from the Resources sub-tab
//     (its data comes through the Events stream, kind retry)
//   - the Automata checks card is gone from the Resources sub-tab
//     (its data comes through the Events stream, kind check)
//   - the safe-to-delete report card and the cleanup status card are gone
//     from the Housekeeping sub-tab (their data comes through the Events
//     stream, kinds cleanup); the uncommitted-work and protected-paths cards
//     stay where they were
//
//   node --test app-automata-events-moved-maintenance.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

const MOVED = {
  automationInfraRetryCard: 'resources',
  automationChecksCard: 'resources',
  automationSafeDeleteCard: 'housekeeping',
  automationCleanupCard: 'housekeeping',
};

function loadApp() {
  const sandbox = buildSandbox({ byId: {}, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function bodyOfSubtab(sandbox, subtab) {
  return vm.runInContext(`_AUTOMATION_SUBTABS.find(s => s.id === '${subtab}').bodyHTML()`, sandbox);
}

test('the four moved cards are not in the body of any Automata sub-tab', () => {
  const sandbox = loadApp();
  const subtabs = vm.runInContext('_AUTOMATION_SUBTABS.map(s => s.id)', sandbox);
  for (const subtab of subtabs) {
    const body = bodyOfSubtab(sandbox, subtab);
    for (const id of Object.keys(MOVED)) {
      assert.ok(!body.includes(`id="${id}"`), `${id} must be gone from ${subtab}`);
    }
  }
});

test('the four moved cards are not on their old tabs', () => {
  const sandbox = loadApp();
  for (const [id, subtab] of Object.entries(MOVED)) {
    const body = bodyOfSubtab(sandbox, subtab);
    assert.ok(!body.includes(id), `${id} must be gone from ${subtab}`);
  }
});

test('the Resources and Housekeeping tabs keep their other cards', () => {
  const sandbox = loadApp();
  const resources = bodyOfSubtab(sandbox, 'resources');
  for (const id of ['automationCapacityCard', 'automationLaneFillCard', 'automationUsageCard', 'automationShipCard']) {
    assert.ok(resources.includes(`id="${id}"`), `${id} must stay on Resources`);
  }
  const housekeeping = bodyOfSubtab(sandbox, 'housekeeping');
  for (const id of ['automationUncommittedWorkCard', 'automationProtectedPathsCard']) {
    assert.ok(housekeeping.includes(`id="${id}"`), `${id} must stay on Housekeeping`);
  }
});

// The safe-to-delete report and the cleanup status render again as cards, but
// on the Automata Events sub-tab (card slots under the event stream), never on
// Housekeeping. Their loaders are kept and poll only while Events is shown.
function eventsPanelSandbox() {
  const panel = { el: null };
  const sandbox = buildSandbox({ byId: {}, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const stub = sandbox.document.getElementById('x');
  panel.el = stub;
  const orig = sandbox.document.getElementById;
  sandbox.document.getElementById = id => (id === 'automataPanel' ? stub : orig(id));
  return { sandbox, panel: stub };
}

test('the safe-to-delete report and the cleanup status are Events card slots in order', () => {
  const sandbox = loadApp();
  const slots = [...vm.runInContext("_automationSlotsOf('events').map(s => s.id)", sandbox)];
  assert.deepEqual(slots, ['automationSafeDeleteCard', 'automationCleanupCard']);
  const html = vm.runInContext("_automationSlotsHTML('events')", sandbox);
  assert.ok(html.includes('id="automationSafeDeleteCard"'));
  assert.ok(html.includes('id="automationCleanupCard"'));
  assert.ok(!html.includes('display:none'), 'the two cards are visible slots, not hidden placeholders');
});

test('the Events panel renders both cards under the event stream', () => {
  const { sandbox, panel } = eventsPanelSandbox();
  vm.runInContext('window.listLoaderAll = () => {}; renderAutomataEventsPanel()', sandbox);
  const html = panel.innerHTML;
  const list = html.indexOf('id="automataEventsList"');
  const cards = html.indexOf('id="automataEventsCards"');
  assert.ok(list >= 0 && cards > list, 'cards sit under the stream');
  assert.ok(html.indexOf('id="automationSafeDeleteCard"') > cards);
  assert.ok(html.indexOf('id="automationCleanupCard"') > html.indexOf('id="automationSafeDeleteCard"'));
});

test('entering Events starts the two loaders and leaving Events stops them', () => {
  const { sandbox } = eventsPanelSandbox();
  const log = [];
  sandbox._log = log;
  vm.runInContext(`
    _AUTOMATION_CARD_SLOTS.events.forEach(sl => {
      sl.mount = () => _log.push('mount:' + sl.id);
      sl.stop = () => _log.push('stop:' + sl.id);
    });
    renderAutomataEventsPanel();
  `, sandbox);
  assert.deepEqual(log, ['mount:automationSafeDeleteCard', 'mount:automationCleanupCard']);
  vm.runInContext("state._autonomousEnabled = true; _automataState.tab = 'events'; switchAutomataTab('automata')", sandbox);
  assert.deepEqual(log.slice(2), ['stop:automationSafeDeleteCard', 'stop:automationCleanupCard']);
  vm.runInContext("switchAutomataTab('templates')", sandbox);
  assert.equal(log.length, 4, 'leaving a tab other than Events stops nothing more');
});

test('the Events slots mount the real loaders and stop them by key', () => {
  const sandbox = loadApp();
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf("registerAutomationCardSlot('events', {");
  const end = src.indexOf('// The Supervisor loop card fills the Setup placeholder', start);
  const block = src.slice(start, end);
  assert.ok(block.includes('_automationSafeDeleteLoader()') && block.includes("_automationStopLoaders(['automation_safe_delete'])"));
  assert.ok(block.includes('_automationCleanupLoader()') && block.includes("_automationStopLoaders(['automation_cleanup'])"));
  const keys = [...vm.runInContext('Object.keys(_AUTOMATION_CARD_SLOTS)', sandbox)];
  assert.ok(keys.includes('events'), 'the slot registry has an events tab');
});

test('the Resources and Housekeeping mount and stop no longer start or end the moved loaders', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const resourcesStart = src.indexOf("id: 'resources',");
  const progressStart = src.indexOf("id: 'progress',");
  const housekeepingStart = src.indexOf("id: 'housekeeping',");
  const sectionEnd = src.indexOf('];', housekeepingStart);
  const resources = src.slice(resourcesStart, progressStart);
  const housekeeping = src.slice(housekeepingStart, sectionEnd);
  for (const fn of ['_automationInfraRetryLoader()', '_automationChecksLoader()']) {
    assert.ok(!resources.includes(fn), `${fn} must not be mounted on Resources`);
  }
  for (const fn of ['_automationSafeDeleteLoader()', '_automationCleanupLoader()']) {
    assert.ok(!housekeeping.includes(fn), `${fn} must not be mounted on Housekeeping`);
  }
  for (const key of ['automation_infra_retry', 'automation_checks']) {
    assert.ok(!resources.includes(`'${key}'`), `${key} must not be stopped on Resources`);
  }
  for (const key of ['automation_safe_delete', 'automation_cleanup']) {
    assert.ok(!housekeeping.includes(`'${key}'`), `${key} must not be stopped on Housekeeping`);
  }
});
