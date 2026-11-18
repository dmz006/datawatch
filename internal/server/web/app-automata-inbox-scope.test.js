// Inbox scope — counts follow the selected project.
//
// Loads the real app.js into a sandbox and drives _automationInboxCountOf, the
// project-item helper and the badge label. With a project selected only that
// project's items count (project link first, directory fallback); items with
// no project count only under the Overview.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const PROJECTS = [
  { name: 'alpha', dir: '/work/alpha' },
  { name: 'beta', dir: '/work/beta/' },
];

function setup(selected) {
  const sandbox = loadAppSandbox();
  vm.runInContext(`
    state.activeView = 'autonomous';
    _automataState.tab = 'projects';
    _automataState.allPrds = [
      { id: 'prd-a', system_tags: ['project:alpha'] },
      { id: 'prd-b', project_dir: '/work/beta' },
    ];
    _projectsState.list = ${JSON.stringify(PROJECTS)};
    _projectsState.selected = ${JSON.stringify(selected)};
  `, sandbox);
  return sandbox;
}

function select(sandbox, name) {
  vm.runInContext(`_projectsState.selected = ${JSON.stringify(name)};`, sandbox);
}

function count(sandbox, key, data) {
  sandbox.__d = data;
  return vm.runInContext(`_automationInboxCountOf(${JSON.stringify(key)}, __d)`, sandbox);
}

const questions = () => [
  { id: 'q1', system_tags: ['project:alpha'] },
  { id: 'q2', project_dir: '/work/beta' },
  { id: 'q3', prd_id: 'prd-a' },
  { id: 'q4' },
];

test('questions: project link first, directory fallback, no-project only in Overview', () => {
  const s = setup('alpha');
  assert.equal(count(s, 'automation_questions', { questions: questions() }), 2); // q1 + q3 (via prd)
  select(s, 'beta');
  assert.equal(count(s, 'automation_questions', { questions: questions() }), 1); // q2 by directory
});

test('a project tag wins over a conflicting directory', () => {
  const s = setup('beta');
  const data = [{ id: 'x', system_tags: ['project:alpha'], project_dir: '/work/beta' }];
  assert.equal(count(s, 'automation_questions', data), 0);
});

test('triage records are scoped to the project', () => {
  const s = setup('alpha');
  const data = { records: [{ id: 't1', system_tags: ['project:alpha'] }, { id: 't2', project_dir: '/work/beta' }, { id: 't3' }] };
  assert.equal(count(s, 'automation_triage', data), 1);
});

test('held questions are scoped to the project', () => {
  const s = setup('beta');
  const data = { open: [{ id: 'h1', project_dir: '/work/beta' }, { id: 'h2', system_tags: ['project:alpha'] }, { id: 'h3' }] };
  assert.equal(count(s, 'automation_held_questions', data), 1);
});

test('security failures are scoped, with the repo name as a directory fallback', () => {
  const s = setup('alpha');
  const data = [{ id: 's1', repo: 'github.com/acme/alpha' }, { id: 's2', repo: 'acme/beta' }, { id: 's3' }];
  assert.equal(count(s, 'automation_security_failure', data), 1);
});

test('supervisor findings are scoped and resolved ones do not count', () => {
  const s = setup('alpha');
  const data = { findings: [
    { id: 'f1', system_tags: ['project:alpha'] },
    { id: 'f2', system_tags: ['project:alpha'], resolved: true },
    { id: 'f3', project_dir: '/work/beta' },
    { id: 'f4' },
  ] };
  assert.equal(count(s, 'supervisor', data), 1);
});

test('items with no project count only under the Overview', () => {
  const s = setup('');
  assert.equal(count(s, 'automation_questions', { questions: questions() }), 4);
  assert.equal(count(s, 'automation_security_failure', [{ id: 'a' }, { id: 'b' }]), 2);
  select(s, 'alpha');
  assert.equal(count(s, 'automation_security_failure', [{ id: 'a' }, { id: 'b' }]), 0);
});

test('an unknown selected name falls back to the Overview scope', () => {
  const s = setup('ghost');
  assert.equal(count(s, 'automation_questions', { questions: questions() }), 4);
});

test('the project scope applies only while the Projects tab is shown', () => {
  const s = setup('alpha');
  vm.runInContext("_automataState.tab = 'automata';", s);
  assert.equal(count(s, 'automation_questions', { questions: questions() }), 4);
});

test('Overview badge label uses the all-projects key; a project uses the plain key', () => {
  const s = setup('');
  s.__calls = [];
  vm.runInContext(`
    var __origT = t;
    t = function (k, a) { __calls.push(k); return __origT.apply(this, arguments); };
  `, s);
  vm.runInContext('_automationBadgeLabel(3)', s);
  assert.equal(s.__calls.pop(), 'automation_subtab_inbox_badge_label_all');
  select(s, 'alpha');
  vm.runInContext('_automationBadgeLabel(3)', s);
  assert.equal(s.__calls.pop(), 'automation_subtab_inbox_badge_label');
});

test('badge element aria-label follows the all-projects key in the Overview', () => {
  const s = setup('');
  const attrs = {};
  s.__badge = { textContent: '', style: {}, title: '', setAttribute(k, v) { attrs[k] = v; } };
  vm.runInContext(`
    document.querySelector = function () { return __badge; };
    _automationInboxCountSet('automation_questions', { questions: ${JSON.stringify(questions())} });
  `, s);
  assert.equal(s.__badge.textContent, '4');
  assert.match(String(attrs['aria-label']), /automation_subtab_inbox_badge_label_all|across all projects/);
});

test('switching the selection recomputes the count from held data', () => {
  const s = setup('');
  s.__badge = { textContent: '', style: {}, title: '', setAttribute() {} };
  vm.runInContext(`
    document.querySelector = function () { return __badge; };
    _automationInboxCountSet('automation_questions', { questions: ${JSON.stringify(questions())} });
  `, s);
  assert.equal(vm.runInContext('_automationInboxBadgeCount()', s), 4);
  select(s, 'alpha');
  vm.runInContext('_automationInboxRescope()', s);
  assert.equal(vm.runInContext('_automationInboxBadgeCount()', s), 2);
  assert.equal(s.__badge.textContent, '2');
  select(s, 'beta');
  vm.runInContext('_automationInboxRescope()', s);
  assert.equal(vm.runInContext('_automationInboxBadgeCount()', s), 1);
  select(s, '');
  vm.runInContext('_automationInboxRescope()', s);
  assert.equal(vm.runInContext('_automationInboxBadgeCount()', s), 4);
});
