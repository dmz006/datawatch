// Projects page — fields and lines with no data are hidden unless the
// show-empty preference is on.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function load(showEmpty) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`_projectsShowEmpty = function () { return ${showEmpty}; }`, sandbox);
  return sandbox;
}

const EMPTY = { name: 'acme', arcs: [{ automation_id: 'a1' }] };
const ROLL = { arcs: [{ automation_id: 'a1', priority: 1, tasks: {} }] };
const POLICY = { policy: {
  weekly_share_percent: { source: 'default' },
  branch_retention_days: { value: null, source: 'default' },
  major_trigger: { value: '', source: 'default' },
  other: { value: 'x', source: 'project' },
} };

test('empty fields and lines are hidden by default', () => {
  const sb = load(false);
  const html = sb._projectPageHTML(EMPTY, ROLL, '', null);
  assert.ok(!html.includes('Weekly share:'));
  assert.ok(!html.includes('none recorded'));
  const pol = sb._projectPolicyEffectiveHTML(POLICY, '');
  assert.ok(!pol.includes('weekly_share_percent'));
  assert.ok(!pol.includes('branch_retention_days'));
  assert.ok(!pol.includes('major_trigger'));
  assert.ok(pol.includes('other:'));
  assert.ok(!sb._projectPolicyEffectiveHTML({ policy: {} }, '').includes('No policy values'));
});

test('empty fields and lines show with labels when the control is on', () => {
  const sb = load(true);
  const html = sb._projectPageHTML(EMPTY, ROLL, '', null);
  assert.ok(html.includes('Weekly share:'));
  assert.ok(html.includes('none recorded'));
  const pol = sb._projectPolicyEffectiveHTML(POLICY, '');
  assert.ok(pol.includes('weekly_share_percent:'));
  assert.ok(pol.includes('major_trigger:'));
  assert.ok(sb._projectPolicyEffectiveHTML({ policy: {} }, '').includes('No policy values'));
});

test('a project with data renders its values', () => {
  const sb = load(false);
  const html = sb._projectPageHTML({ name: 'acme', owner: 'dana', description: 'hello', weekly_share: 20 }, ROLL, '', null);
  assert.ok(html.includes('dana'));
  assert.ok(html.includes('hello'));
  assert.ok(html.includes('20%'));
});

test('empty event stream, merge pipeline and arc sections are hidden unless show-empty is on', () => {
  const off = load(false);
  const on = load(true);
  assert.equal(off._projectEventStreamHTML({ events: [] }, ''), '');
  assert.equal(off._projectMergePipelineHTML({ definition: { steps: [] } }, ''), '');
  assert.ok(off._projectEventStreamHTML(null, '').includes('project-event-stream'));
  assert.ok(off._projectEventStreamHTML({ events: [] }, 'boom').includes('boom'));
  assert.ok(off._projectMergePipelineHTML(null, '').includes('project-merge-pipeline'));
  assert.ok(off._projectEventStreamHTML({ events: [{ kind: 'k' }] }, '').includes('project-event-row'));
  assert.ok(on._projectEventStreamHTML({ events: [] }, '').includes('project-event-stream'));
  assert.ok(on._projectMergePipelineHTML({ definition: { steps: [] } }, '').includes('project-merge-pipeline'));
  const page = (sb) => sb._projectPageHTML({ name: 'acme', arcs: [] }, { arcs: [] }, '', null);
  assert.ok(!page(off).includes('>Arcs<'));
  assert.ok(page(on).includes('>Arcs<'));
});
