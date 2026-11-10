// Repo CI watch in the PWA (Automaton view):
//
//   - the read-only repo CI status line is removed from the Automata view:
//     its element is absent from the rendered markup, no repo CI poll timer
//     starts when the view opens, and its helper functions are gone
//   - the server endpoint GET /api/autonomous/repo-ci-watch and the eight
//     repo_ci_watch config keys sit in GENERAL_CONFIG_FIELDS and stay as they are
//
//   node --test app-repo-ci-watch.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

// Loads the real app.js into a fresh sandbox. `intervals` records every
// setInterval the script or the view render starts, and `fetches` records
// every apiFetch URL, so a test can prove no repo CI poll was started.
function loadApp() {
  const intervals = [];
  const fetches = [];
  const byId = { view: { innerHTML: '', style: {}, querySelector() { return null; }, classList: { add() {}, remove() {}, contains() { return false; } } } };
  const sandbox = buildSandbox({ byId, localStorage: new Map() });
  sandbox.setInterval = (fn, ms) => { intervals.push({ fn, ms }); return intervals.length; };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.apiFetch = (url) => { fetches.push(String(url)); return Promise.resolve({}); };
  return { sandbox, intervals, fetches, view: byId.view };
}

const REMOVED_FUNCTIONS = [
  'repoCIWatchAgeText',
  'repoCIWatchStatusText',
  'repoCIWatchRenderLine',
  'repoCIWatchRefreshStatusLine',
  'repoCIWatchStartPolling',
  'repoCIWatchStopPolling',
];

test('repo CI watch — the status line element is absent from the Automata view markup', () => {
  const { sandbox, view } = loadApp();
  sandbox.renderAutonomousView();
  assert.equal(typeof view.innerHTML, 'string');
  assert.ok(view.innerHTML.length > 0, 'the Automata view renders');
  assert.equal(view.innerHTML.includes('repoCIWatchStatusLine'), false,
    'the rendered Automata markup must not contain the repo CI status line element');
  assert.equal(view.innerHTML.includes('Repo CI watch'), false,
    'the rendered Automata markup must not contain the repo CI status line comment or text');
});

test('repo CI watch — opening the Automata view starts no repo CI poll timer', () => {
  const { sandbox, intervals, fetches } = loadApp();
  sandbox.renderAutonomousView();
  assert.equal(sandbox.window._repoCIWatchInterval, undefined,
    'no repo CI poll interval is stored on window');
  assert.equal(fetches.some(u => u.includes('/api/autonomous/repo-ci-watch')), false,
    'the repo CI watch endpoint is not polled when the view opens');
  // The Automata list keeps its own 30s loader timer; the repo CI watch
  // timer (window._repoCIWatchInterval) must never be among the timers set.
  assert.equal(intervals.some(i => i.fn && i.fn.name === 'repoCIWatchRefreshStatusLine'), false);
});

test('repo CI watch — the repo CI status helper functions are gone', () => {
  const { sandbox } = loadApp();
  for (const name of REMOVED_FUNCTIONS) {
    assert.equal(typeof sandbox[name], 'undefined', `${name} must be removed from app.js`);
  }
  assert.equal(vm.runInContext("typeof REPO_CI_WATCH_STATUS_ELEMENT_ID", sandbox), 'undefined',
    'the status element id constant must be removed');
  assert.equal(vm.runInContext("typeof REPO_CI_WATCH_POLL_MS", sandbox), 'undefined',
    'the status poll cadence constant must be removed');
});

test('repo CI watch — the eight repo_ci_watch config keys sit in GENERAL_CONFIG_FIELDS', () => {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox);
  const section = fields.find(s => s.id === 'autonomous');
  assert.ok(section, 'autonomous section must exist in GENERAL_CONFIG_FIELDS');
  const keys = new Set(section.fields.map(f => f.key));
  for (const k of [
    'autonomous.automation.repo_ci_watch.mode',
    'autonomous.automation.repo_ci_watch.repos',
    'autonomous.automation.repo_ci_watch.poll_seconds',
    'autonomous.automation.repo_ci_watch.alert_minutes',
    'autonomous.automation.repo_ci_watch.tag_count',
    'autonomous.automation.repo_ci_watch.hung_after_minutes',
    'autonomous.automation.repo_ci_watch.security_workflows',
    'autonomous.automation.repo_ci_watch.delete_superseded',
  ]) {
    assert.ok(keys.has(k), `GENERAL_CONFIG_FIELDS must contain ${k}`);
  }
});
