// The Inbox badge counts only items the operator can act on.
//
// Pins, against the REAL app.js loaded into the shared stub browser:
//   • open questions count; held questions in the open list count,
//     the shadow would-be list never counts,
//   • the security-failure report and the supervisor watch findings render
//     but do not count (read-only cards),
//   • a triage record counts only while its card carries an action button
//     (Retry, Resolve review or Cancel): failed, blocked and merge_pending
//     records count, the read-only merge_blocked card does not,
//   • the badge is hidden at 0 even when read-only cards are loaded,
//   • with a project selected the count and the scoped lists agree.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._i18n.fallback = bundle('en');
  return sandbox;
}

const run = (s, code) => vm.runInContext(code, s);

const PROJECTS = [
  { name: 'alpha', dir: '/work/alpha' },
  { name: 'beta', dir: '/work/beta' },
];

function setup(scope) {
  const sandbox = loadAppSandbox();
  vm.runInContext(`
    state.activeView = 'autonomous';
    _automataState.tab = ${scope === 'project' ? "'projects'" : "'automata'"};
    _automataState.allPrds = [
      { id: 'prd-a', system_tags: ['project:alpha'] },
      { id: 'prd-b', project_dir: '/work/beta' },
    ];
    _projectsState.list = ${JSON.stringify(PROJECTS)};
    _projectsState.selected = ${scope === 'project' ? "'alpha'" : "''"};
  `, sandbox);
  return sandbox;
}

test('open questions count toward the badge', () => {
  const s = setup('overview');
  const data = { questions: [{ id: 'q1', state: 'open' }, { id: 'q2', state: 'open' }] };
  assert.equal(run(s, `_automationInboxCountOf('automation_questions', ${JSON.stringify(data)})`), 2);
});

test('held questions in the open list count; the shadow would-be list never counts', () => {
  const s = setup('overview');
  const open = [{ id: 'h1' }, { id: 'h2' }];
  const shadow = [{ id: 's1' }, { id: 's2' }, { id: 's3' }];
  assert.equal(run(s, `_automationInboxCountOf('automation_held_questions', { open: ${JSON.stringify(open)}, shadow: ${JSON.stringify(shadow)} })`), 2);
  assert.equal(run(s, `_automationInboxCountOf('automation_held_questions', { open: [], shadow: ${JSON.stringify(shadow)} })`), 0, 'shadow only: nothing counts');
});

test('the security-failure report and the supervisor watch findings render but do not count', () => {
  const s = setup('overview');
  assert.equal(run(s, `_automationInboxCountOf('automation_security_failure', [{ id: 'f1' }, { id: 'f2' }])`), 0);
  assert.equal(run(s, `_automationInboxCountOf('supervisor', { findings: [{ id: 's1' }, { id: 's2' }] })`), 0);
});

test('a triage record counts only while its card carries an action button', () => {
  const s = setup('overview');
  for (const [status, actionable] of [
    ['failed', true],
    ['blocked', true],
    ['merge_pending', true],
    ['merge_blocked', false],
  ]) {
    assert.equal(run(s, `_triageRecordActionable({ status: ${JSON.stringify(status)} })`), actionable, status);
  }
  assert.equal(run(s, '_triageRecordActionable(null)'), false);
  const data = {
    records: [
      { id: 't1', status: 'failed' },
      { id: 't2', status: 'blocked' },
      { id: 't3', status: 'merge_pending' },
      { id: 't4', status: 'merge_blocked' },
      { id: 't5', status: 'merge_blocked' },
    ],
  };
  assert.equal(run(s, `_automationInboxCountOf('automation_triage', ${JSON.stringify(data)})`), 3);
  assert.equal(run(s, `_automationInboxBadgeCount()`), 0, 'counts start empty');
  run(s, `_automationInboxCountSet('automation_triage', ${JSON.stringify(data)})`);
  assert.equal(run(s, '_automationInboxBadgeCount()'), 3);
});

test('the badge is hidden at 0 even when read-only cards are loaded', () => {
  const s = setup('overview');
  const attrs = {};
  s.__badge = { style: {}, textContent: '', setAttribute: (k, v) => { attrs[k] = v; } };
  vm.runInContext(`
    document.querySelector = function () { return __badge; };
    _automationInboxCountSet('automation_security_failure', [{ id: 'f1' }, { id: 'f2' }], function () {});
    _automationInboxCountSet('supervisor', { findings: [{ id: 's1' }] }, function () {});
    _automationUpdateInboxBadge();
  `, s);
  assert.equal(s.__badge.style.display, 'none', 'zero is hidden');
  assert.equal(s.__badge.textContent, '0');
  assert.match(String(attrs['aria-label']), /waiting on you to act/);
  s.__badge.style.display = '';
  run(s, `_automationInboxCountSet('automation_triage', { records: [{ id: 't1', status: 'failed' }] }, function () {})`);
  assert.equal(s.__badge.style.display, '', 'one actionable record makes it visible');
  assert.equal(s.__badge.textContent, '1');
});

test('with a project selected the count and the scoped lists agree', () => {
  const s = setup('project');
  assert.equal(run(s, '_inboxScope().kind'), 'project');
  const triage = { records: [
    { id: 'a', status: 'failed', system_tags: ['project:alpha'] },
    { id: 'b', status: 'merge_blocked', system_tags: ['project:alpha'] },
    { id: 'c', status: 'failed', system_tags: ['project:beta'] },
    { id: 'd', status: 'merge_pending' },
  ] };
  const questions = { questions: [
    { id: 'q1', system_tags: ['project:alpha'] },
    { id: 'q2', system_tags: ['project:beta'] },
  ] };
  const held = { open: [{ id: 'h1', prd_id: 'prd-a' }] };
  const sec = [{ id: 'f1', repo: 'acme/alpha' }, { id: 'f2', repo: 'acme/beta' }];
  const findings = { findings: [{ id: 's1', system_tags: ['project:alpha'] }] };

  s.__d = triage;
  assert.equal(run(s, `_inboxScopeData('automation_triage', __d).records.length`), 2, 'alpha keeps its two triage records');
  assert.equal(run(s, `_automationInboxCountOf('automation_triage', __d)`), 1, 'only the failed one of them is actionable');
  assert.equal(run(s, `_automationInboxCountOf('automation_security_failure', ${JSON.stringify(sec)})`), 0, 'security failures still do not count in scope');
  assert.equal(run(s, `_inboxScopeData('supervisor', ${JSON.stringify(findings)}).findings.length`), 1, 'the read-only supervisor list still renders in scope');
  assert.equal(run(s, `_automationInboxCountOf('supervisor', ${JSON.stringify(findings)})`), 0);

  run(s, `_automationInboxCountSet('automation_triage', __d, function () {})`);
  run(s, `_automationInboxCountSet('automation_questions', ${JSON.stringify(questions)}, function () {})`);
  run(s, `_automationInboxCountSet('automation_held_questions', ${JSON.stringify(held)}, function () {})`);
  run(s, `_automationInboxCountSet('automation_security_failure', ${JSON.stringify(sec)}, function () {})`);
  run(s, `_automationInboxCountSet('supervisor', ${JSON.stringify(findings)}, function () {})`);
  assert.equal(run(s, '_automationInboxBadgeCount()'), 3, '1 actionable triage + 1 question + 1 held question');
});
