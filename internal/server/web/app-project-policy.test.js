// Project policy panel — PWA side.
//
// window.projectPolicyPanelHtml(state) draws one row per policy setting of the
// project tier: the effective value, the precedence badge (Automaton /
// Project / Daemon) and a control to save a value or clear the key back to the
// tier below. The panel parses nothing server-side and changes no tier except
// the project tier; these tests check the drawing, the save it sends, the
// clear-to-inherit write and the client-side validation that refuses a value
// before it reaches the daemon.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

const PROJECTS = [{
  name: 'alpha', dir: '/srv/alpha', description: '', default_backend: 'ollama',
  policy: { max_concurrent_tasks: 2 },
}];

function effective(policy) {
  return { policy: policy };
}

function render(s, state) {
  s.ST = state;
  return vm.runInContext('window.projectPolicyPanelHtml(ST)', s);
}

function stubApi(s, replies) {
  s.__calls = [];
  s.__replies = replies || {};
  vm.runInContext('apiFetch = (u, i) => { __calls.push([u, i || {}]); return Promise.resolve(__replies[u] || {}); }', s);
  return s.__calls;
}

test('shows loading until projects arrive, then the picker, the legend and every group', () => {
  const s = loadApp();
  assert.ok(render(s, { projects: [] }).includes('No projects registered'));
  assert.ok(render(s, { projects: PROJECTS, effective: null }).includes('Loading project policy…'));
  const html = render(s, { projects: PROJECTS, projectName: 'alpha', effective: effective({}) });
  assert.ok(html.includes('value="alpha" selected'), 'project picker');
  for (const g of ['Plan checks', 'Retry &amp; escalation', 'Git defaults', 'Lane limits']) {
    assert.ok(html.includes('>' + g + '</div>'), 'group: ' + g);
  }
  for (const key of ['plan_review_mode', 'retry_policy', 'default_auto_pr', 'max_concurrent_tasks', 'burn_guard_ask_on_change']) {
    assert.ok(html.includes('id="ppf_' + key + '"'), 'control: ' + key);
    assert.ok(html.includes('onclick="projectPolicyApply(\'' + key + '\')"'), 'save: ' + key);
  }
});

test('each row shows the effective value with its precedence badge', () => {
  const s = loadApp();
  const html = render(s, {
    projects: PROJECTS,
    effective: effective({
      plan_review_mode: { value: 'block', source: 'automaton' },
      max_concurrent_tasks: { value: 2, source: 'project' },
      retry_policy: { value: { infra: 3, content: 1 }, source: 'daemon' },
      failure_escalation: { value: { enabled: true, max_failures: 4, steps: [] }, source: 'daemon' },
      burn_guard_ask_on_change: { value: false, source: 'daemon' },
    }),
  });
  assert.ok(html.includes('block') && html.includes('>Automaton<'), 'automaton badge');
  assert.ok(html.includes('>2') && html.includes('>Project<'), 'project badge');
  assert.ok(html.includes('content=1, infra=3') && html.includes('>Daemon<'), 'daemon badge + map format');
  assert.ok(html.includes('on, max 4, 0 steps'), 'escalation format');
  assert.ok(html.includes('off'), 'bool format');
});

test('the help link points at the project-automation-policy howto and the legend states the precedence', () => {
  const html = render(loadApp(), { projects: PROJECTS, effective: effective({}) });
  assert.ok(html.includes('howto/project-automation-policy.md'), 'howto link');
  assert.ok(html.includes('Precedence: Automaton overrides project overrides daemon'), 'legend');
});

test('saving writes the project tier with the chosen value and the other keys unchanged', async () => {
  const s = loadApp();
  const calls = stubApi(s, {
    '/api/projects': { name: 'alpha', policy: { max_concurrent_tasks: 2, plan_review_mode: 'warn' } },
    '/api/projects/alpha/policy/effective': effective({
      max_concurrent_tasks: { value: 2, source: 'project' },
      plan_review_mode: { value: 'warn', source: 'project' },
    }),
  });
  s.ST = { projects: PROJECTS, projectName: 'alpha', effective: effective({}) };
  vm.runInContext('Object.assign(_projectPolicy, ST)', s);
  s.document.getElementById = id => (id === 'ppf_plan_review_mode' ? { value: 'block' } : null);
  await vm.runInContext('window.projectPolicyApply("plan_review_mode")', s);
  const post = calls.find(c => c[0] === '/api/projects' && c[1].method === 'POST');
  assert.ok(post, 'a POST to /api/projects was sent');
  const body = JSON.parse(post[1].body);
  assert.equal(body.name, 'alpha');
  assert.equal(body.policy.max_concurrent_tasks, 2, 'other stored key untouched');
  assert.equal(body.policy.plan_review_mode, 'block', 'chosen value written');
  assert.ok(calls.some(c => c[0] === '/api/projects/alpha/policy/effective'), 're-read effective after save');
});

test('clearing a key sends null for it — inherit, not the daemon value', async () => {
  const s = loadApp();
  const calls = stubApi(s, {
    '/api/projects': { name: 'alpha', policy: {} },
    '/api/projects/alpha/policy/effective': effective({ max_concurrent_tasks: { value: 1, source: 'daemon' } }),
  });
  vm.runInContext('Object.assign(_projectPolicy, { projects: [{ name: "alpha", dir: "/srv/alpha", description: "", default_backend: "", policy: { max_concurrent_tasks: 1 } }], projectName: "alpha" })', s);
  s.document.getElementById = id => (id === 'ppf_max_concurrent_tasks' ? { value: '' } : null);
  await vm.runInContext('window.projectPolicyApply("max_concurrent_tasks")', s);
  const post = calls.find(c => c[0] === '/api/projects' && c[1].method === 'POST');
  assert.ok(post, 'a POST was sent for a clear');
  const body = JSON.parse(post[1].body);
  assert.ok('max_concurrent_tasks' in body.policy);
  assert.equal(body.policy.max_concurrent_tasks, null, 'null means the tier below is in play again');
});

test('an out-of-vocabulary value is refused client-side: error shown, nothing written', async () => {
  const s = loadApp();
  const calls = stubApi(s, {});
  vm.runInContext('Object.assign(_projectPolicy, { projects: [{ name: "alpha", dir: "/srv/alpha", description: "", default_backend: "", policy: {} }], projectName: "alpha", effective: { policy: {} } })', s);
  s.document.getElementById = id => (id === 'ppf_plan_review_mode' ? { value: 'sometimes' } : null);
  await vm.runInContext('window.projectPolicyApply("plan_review_mode")', s);
  assert.ok(!calls.some(c => c[0] === '/api/projects' && c[1].method === 'POST'), 'no write');
  assert.ok(s._projectPolicy.error.includes('Plan review mode') && s._projectPolicy.error.includes('sometimes'), s._projectPolicy.error);
  assert.ok(render(s, s._projectPolicy).includes('sometimes'), 'the error is drawn');
});

test('a map value with an unknown category is refused client-side', async () => {
  const s = loadApp();
  const calls = stubApi(s, {});
  vm.runInContext('Object.assign(_projectPolicy, { projects: [{ name: "alpha", dir: "/srv/alpha", description: "", default_backend: "", policy: {} }], projectName: "alpha", effective: { policy: {} } })', s);
  s.document.getElementById = id => (id === 'ppf_retry_policy' ? { value: 'flaky=3' } : null);
  await vm.runInContext('window.projectPolicyApply("retry_policy")', s);
  assert.ok(!calls.some(c => c[0] === '/api/projects' && c[1].method === 'POST'), 'no write');
  assert.ok(s._projectPolicy.error.includes('flaky=3'), s._projectPolicy.error);
});

test('values and errors are escaped, never raw', () => {
  const s = loadApp();
  const html = render(s, {
    projects: [{ name: 'x<b', dir: '/x', policy: {} }],
    projectName: 'x<b',
    effective: effective({}),
    error: '<script>bad</script>',
  });
  assert.ok(html.includes('&lt;script&gt;bad&lt;/script&gt;'), 'error escaped');
  assert.ok(!html.includes('<b</b'), 'project name not raw');
});
