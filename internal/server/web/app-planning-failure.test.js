// Planning failure on the Automaton detail and the Automata list card: reason,
// category, rule, phrases and retry counts render escaped; the list badge
// shows only for a draft with a planning failure.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

const failed = () => ({
  id: 'p1',
  status: 'draft',
  planning_failure: {
    reason: 'spec holds a fixed version number "v9.1.0" </script><img src=x onerror=alert(1)>',
    category: 'scope',
    source: 'release-version-fixed',
    phrases: ['v9.1.0', '<b>bad</b>'],
    retryable: false,
    retry_counts: { scope: 2, infra: 1 },
    retry_budget: 3,
  },
});

test('PlanningFailure detail shows reason, category, rule, phrases and retry counts', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDDecomposeFailure', failed());
  assert.match(html, /prd-planning-failure/);
  assert.match(html, /fixed version number/);
  assert.match(html, / scope<\/div>/);
  assert.match(html, /release-version-fixed/);
  assert.match(html, /<code class="prd-planning-failure-phrase">v9\.1\.0<\/code>/);
  assert.match(html, /2 \/ 3 \(infra=1, scope=2\)/);
  assert.match(html, /planning_failure_not_retryable/);
});

test('PlanningFailure detail escapes every string', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDDecomposeFailure', failed());
  assert.doesNotMatch(html, /<\/script>/);
  assert.doesNotMatch(html, /<img/);
  assert.doesNotMatch(html, /<b>bad/);
  assert.match(html, /&lt;b&gt;bad&lt;\/b&gt;/);
});

test('PlanningFailure detail shows the pending question through the question row', () => {
  const sandbox = loadAppJS();
  vm.runInContext(`window._planningQuestionByPrd = { p1: { id: 'q1', asked_by: 'planning', prd_id: 'p1', text: 'Reword?', options: [{ id: 'fix_wording', label: 'Fix the wording and plan again' }, { id: 'leave_it', label: 'Leave it' }] } };`, sandbox);
  const html = call(sandbox, 'renderPRDDecomposeFailure', failed());
  assert.match(html, /questions-queue-row/);
  assert.match(html, /data-option-id="fix_wording"/);
});

test('PlanningFailure falls back to the decompose line without a planning failure object', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPRDDecomposeFailure', { id: 'p2', status: 'draft', last_decompose_failure: { category: 'infra', detail: 'boom' } });
  assert.match(html, /prd-decompose-failure/);
  assert.doesNotMatch(html, /prd-planning-failure/);
});

test('PlanningFailure badge shows for a draft with a failure only', () => {
  const sandbox = loadAppJS();
  assert.match(call(sandbox, 'renderPlanningFailureBadge', failed()), /planning-failure-badge/);
  assert.equal(call(sandbox, 'renderPlanningFailureBadge', { id: 'p3', status: 'draft' }), '');
  const running = failed(); running.status = 'running';
  assert.equal(call(sandbox, 'renderPlanningFailureBadge', running), '');
});

test('PlanningFailure badge escapes the reason in its title', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderPlanningFailureBadge', failed());
  assert.doesNotMatch(html, /<img/);
});
