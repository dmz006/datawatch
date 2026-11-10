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
