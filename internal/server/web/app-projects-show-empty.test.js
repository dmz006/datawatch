// Projects tab — the show-empty checkbox in the picker. It is a viewer
// preference: unchecked by default, remembered, and it repaints on change.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function load() {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { view } });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

test('the picker has an unchecked show-empty checkbox by default', () => {
  const sb = load();
  const html = sb._projectsPickerHTML([{ name: 'acme' }], '', '');
  assert.ok(html.includes('id="projectsShowEmpty"'), 'checkbox present');
  assert.ok(!html.includes(' checked '), 'unchecked by default');
  assert.ok(html.includes('toggleProjectsShowEmpty(this.checked)'), 'onchange wired');
  assert.ok(html.includes('Show empty lines and sections'), 'label from locale');
});

test('toggleProjectsShowEmpty remembers the choice and repaints', () => {
  const sb = load();
  let painted = 0;
  vm.runInContext('_projectsPaint = function () { globalThis.__painted = (globalThis.__painted || 0) + 1; }', sb);
  sb.toggleProjectsShowEmpty(true);
  assert.equal(vm.runInContext('_projectsShowEmpty()', sb), true);
  assert.equal(vm.runInContext('globalThis.__painted', sb), 1);
  assert.ok(sb._projectsPickerHTML([], '', '').includes(' checked '), 'checked once remembered');
  sb.toggleProjectsShowEmpty(false);
  assert.equal(vm.runInContext('_projectsShowEmpty()', sb), false);
});

// ---- show-empty behaviour on the project page ----

function loadWith(storage, showEmpty) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { view } });
  if (storage) sandbox.localStorage = storage;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  if (showEmpty !== undefined) vm.runInContext(`_projectsShowEmpty = function () { return ${showEmpty}; }`, sandbox);
  return sandbox;
}

const BARE = { name: 'acme', arcs: [{ automation_id: 'a1' }] };
const BARE_ROLL = { arcs: [{ automation_id: 'a1', priority: 1, tasks: {} }] };
const BARE_POLICY = { policy: {
  weekly_share_percent: { source: 'default' },
  branch_retention_days: { value: null, source: 'default' },
  major_trigger: { value: '', source: 'default' },
} };
const EMPTY_LABELS = ['Owner:', 'Repository:', 'Default branch:', 'Weekly share:'];

// stream renders the three link sections for a project with no data.
function sections(sb) {
  return sb._projectEventStreamHTML({ events: [] }, '') +
    sb._projectMergePipelineHTML({ definition: { steps: [] } }, '') +
    sb._projectPolicyEffectiveHTML(BARE_POLICY, '');
}

test('preference off: a project with no data shows no none-recorded line and no empty sections', () => {
  const sb = loadWith(null, false);
  const page = sb._projectPageHTML(BARE, BARE_ROLL, '', null);
  assert.ok(!page.includes('none recorded'), 'no none-recorded line');
  for (const l of EMPTY_LABELS) assert.ok(!page.includes(l), l + ' hidden');
  assert.equal(sb._projectEventStreamHTML({ events: [] }, ''), '', 'no empty event stream section');
  assert.ok(!sections(sb).includes('project-event-stream'));
  const pol = sb._projectPolicyEffectiveHTML(BARE_POLICY, '');
  assert.ok(!pol.includes('weekly_share_percent'), 'no empty weekly share');
  assert.ok(!pol.includes('branch_retention_days'), 'no empty branch retention');
  assert.ok(!pol.includes('major_trigger'), 'no empty major trigger');
  assert.ok(!pol.includes('none recorded'));
  const docs = sb._projectArcDocsHTML({ arcs: [{ automation_id: 'a1' }] }, { automation_id: 'a1' });
  assert.ok(!docs.includes('none recorded'), 'arc documents have no none-recorded line');
});

test('preference on: the same project shows every empty line and section with plain labels', () => {
  const sb = loadWith(null, true);
  const page = sb._projectPageHTML(BARE, BARE_ROLL, '', null);
  for (const l of EMPTY_LABELS) assert.ok(page.includes(l), l + ' shown');
  assert.ok(page.includes('none recorded'));
  assert.ok(page.includes('Plan documents:') || page.includes('none recorded'));
  assert.ok(sb._projectEventStreamHTML({ events: [] }, '').includes('project-event-stream'));
  assert.ok(sb._projectMergePipelineHTML({ definition: { steps: [] } }, '').includes('project-merge-pipeline'));
  const pol = sb._projectPolicyEffectiveHTML(BARE_POLICY, '');
  for (const k of ['weekly_share_percent:', 'branch_retention_days:', 'major_trigger:']) assert.ok(pol.includes(k), k);
  assert.ok(pol.includes('none recorded'));
  const docs = sb._projectArcDocsHTML({ arcs: [{ automation_id: 'a1' }] }, { automation_id: 'a1' });
  assert.ok(docs.includes('none recorded'));
});

test('the preference defaults to off and is remembered across a fresh load with the same storage', () => {
  const first = loadWith(null);
  assert.equal(vm.runInContext('_projectsShowEmpty()', first), false, 'default off');
  const storage = first.localStorage;
  first._projectsSetShowEmpty(true);
  const second = loadWith(storage);
  assert.equal(vm.runInContext('_projectsShowEmpty()', second), true, 'remembered');
  assert.ok(second._projectsPickerHTML([], '', '').includes(' checked '), 'checkbox checked after reload');
  second._projectsSetShowEmpty(false);
  assert.equal(vm.runInContext('_projectsShowEmpty()', loadWith(storage)), false, 'off again');
  assert.equal(vm.runInContext('_projectsShowEmpty()', loadWith(null)), false, 'fresh storage defaults off');
});

test('a project with data in every field renders the same with the preference off or on', () => {
  const full = {
    name: 'acme', dir: '/work/acme', default_backend: 'claude-code', owner: 'dana', repository: 'https://example.com/acme',
    default_branch: 'main', description: 'hello', weekly_share: 20,
    target: { name: 'v2', definition_of_done: 'all green' },
    lane_defaults: { lane: 'claude', backend: 'claude-code', model: 'opus' },
    arcs: [{ automation_id: 'a1', plan_docs: ['https://example.com/plan'], design_docs: ['https://example.com/design'], issues: ['7'] }],
  };
  const roll = { arcs: [{ automation_id: 'a1', priority: 1, state: 'active', tasks: { done: 1, total: 2 },
    automata: [{ id: 'm1', title: 'First step', status: 'running', session_ids: ['s-1'] }] }],
    unassigned: { automata: [{ id: 'm2', title: 'Loose step', status: 'queued', session_ids: ['s-2'] }] } };
  const policy = { policy: {
    weekly_share_percent: { value: 20, source: 'project' },
    branch_retention_days: { value: 14, source: 'daemon' },
    major_trigger: { value: 'manual', source: 'project' },
  } };
  const events = { events: [{ at: '2026-11-09T10:00:00Z', kind: 'automaton_started', level: 'info', automation_id: 'a1' }] };
  const off = loadWith(null, false);
  const on = loadWith(null, true);
  const page = off._projectPageHTML(full, roll, '', null);
  assert.equal(page, on._projectPageHTML(full, roll, '', null), 'page identical');
  for (const s of ['dana', '/work/acme', 'claude-code', 'main', 'hello', '20%', 'v2', 'all green', 'First step', 'Loose step', 'https://example.com/plan', 'https://example.com/design', 'datawatch-app #7'])
    assert.ok(page.includes(s), s);
  assert.ok(!page.includes('none recorded'));
  assert.ok(!page.includes('is not set'), 'no unset events');
  assert.equal(off._projectPolicyEffectiveHTML(policy, ''), on._projectPolicyEffectiveHTML(policy, ''));
  const pol = off._projectPolicyEffectiveHTML(policy, '');
  for (const s of ['weekly_share_percent:', ' 20 ', 'branch_retention_days:', ' 14 ', 'major_trigger:', 'manual']) assert.ok(pol.includes(s), s);
  const ev = off._projectEventStreamHTML(events, '');
  assert.equal(ev, on._projectEventStreamHTML(events, ''));
  assert.ok(ev.includes('automaton_started') && ev.includes('project-event-row'));
});

test('a missing weekly share is an Events entry only when the project has an arc', () => {
  for (const showEmpty of [false, true]) {
    const sb = loadWith(null, showEmpty);
    const withArc = sb._projectPageHTML({ name: 'acme', arcs: [{ automation_id: 'a1' }] }, BARE_ROLL, '', null);
    const ev = withArc.slice(withArc.indexOf('id="projectEvents"'));
    assert.ok(withArc.includes('id="projectEvents"'), 'events block present');
    assert.ok(ev.includes('Weekly share is not set.'), 'entry for the missing share');
    const noArcs = sb._projectPageHTML({ name: 'acme' }, { arcs: [] }, '', null);
    assert.ok(!noArcs.includes('Weekly share is not set.'), 'no entry without arcs');
    const withShare = sb._projectPageHTML({ name: 'acme', weekly_share: 10 }, BARE_ROLL, '', null);
    assert.ok(!withShare.includes('Weekly share is not set.'), 'no entry when set');
  }
});
