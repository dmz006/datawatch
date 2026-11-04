// Task card — the Read-only label and the Read-only toggle in the add-task and
// edit-task dialogs. fetch is stubbed and records every request.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadApp(byId) {
  const sandbox = buildSandbox({ byId });
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push({ url, method: (opts && opts.method) || 'GET', body: opts && opts.body ? JSON.parse(opts.body) : undefined });
    return Promise.resolve({
      ok: true, status: 200, statusText: '200',
      json: () => Promise.resolve({}),
      text: () => Promise.resolve('{}'),
    });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__calls = calls;
  sandbox.showToast = () => {};
  return sandbox;
}

const prd = { id: 'p1', status: 'needs_review' };
const story = { id: 's1', title: 'Story' };

test('the task card shows a read-only label only for a read-only task', () => {
  const sb = loadApp({});
  const ro = sb.renderTask(prd, story, { id: 't1', title: 'Run gate', spec: 'x', read_only: true }, true);
  assert.match(ro, /prd-task-readonly-badge/);
  assert.match(ro, /read-only/);
  const rw = sb.renderTask(prd, story, { id: 't2', title: 'Write code', spec: 'x' }, true);
  assert.doesNotMatch(rw, /prd-task-readonly-badge/);
});

test('the edit button hands the current read-only state to the dialog', () => {
  const sb = loadApp({});
  const html = sb.renderTask(prd, story, { id: 't1', title: 'Run gate', spec: 'x', read_only: true }, true);
  const decoded = html.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, '&');
  assert.match(decoded, /openPRDEditTaskModal\([^)]*,true(,"[^"]*")?\)/);
  const plain = sb.renderTask(prd, story, { id: 't2', title: 'Write', spec: 'x' }, true).replace(/&quot;/g, '"');
  assert.match(plain, /openPRDEditTaskModal\([^)]*,false(,"[^"]*")?\)/);
});

test('the read-only field is a labelled checkbox that reflects the current state', () => {
  const sb = loadApp({});
  const on = sb._prdTaskReadOnlyFieldHTML('x', true);
  assert.match(on, /type="checkbox" id="x" checked/);
  assert.match(on, /Read-only/);
  assert.doesNotMatch(sb._prdTaskReadOnlyFieldHTML('x', false), /checked/);
});

test('the add-task dialog sends read_only only when the toggle is on', async () => {
  const title = { value: 'Run the gate' };
  const spec = { value: 'READ-ONLY: runs the suite' };
  const toggle = { checked: true };
  const sb = loadApp({ prdAddTaskTitle: title, prdAddTaskSpec: spec, prdAddTaskReadOnly: toggle });
  let html, submit;
  vm.runInContext('_prdMountModal = function (h, f) { __mounted = { h, f }; }', sb);
  sb.openPRDAddTaskModal('p1', 's1');
  const mounted = vm.runInContext('__mounted', sb);
  html = mounted.h; submit = mounted.f;
  assert.match(html, /id="prdAddTaskReadOnly"/);
  submit();
  await new Promise(r => setImmediate(r));
  const on = sb.__calls.find(c => c.url.endsWith('/add_task'));
  assert.equal(on.body.read_only, true);
  assert.equal(on.body.title, 'Run the gate');

  toggle.checked = false;
  sb.__calls.length = 0;
  submit();
  await new Promise(r => setImmediate(r));
  const off = sb.__calls.find(c => c.url.endsWith('/add_task'));
  assert.equal('read_only' in off.body, false);
});

test('the edit-task dialog posts edit_task with read_only when only the toggle changed', async () => {
  const els = {
    prdEditSpec: { value: 'same spec' }, prdEditBackend: { value: '' }, prdEditEffort: { value: '' },
    prdEditModelInner: makeStubElement(), prdEditProjectDir: { value: '' },
    prdEditReadOnly: { checked: true },
    prdEditVerifBackend: { value: '' }, prdEditVerifEffort: { value: '' }, prdEditVerifModelInner: makeStubElement(),
  };
  const sb = loadApp(els);
  vm.runInContext('_prdMountModal = function (h, f) { __mounted = { h, f }; }; ensureLLMModelLists = function () { return Promise.resolve(); }; refreshLLMModelField = function () {};', sb);
  sb.openPRDEditTaskModal('p1', 't1', 'same spec', '', '', '', {}, '', false);
  await new Promise(r => setImmediate(r));
  const mounted = vm.runInContext('__mounted', sb);
  assert.match(mounted.h, /id="prdEditReadOnly"/);
  mounted.f();
  await new Promise(r => setImmediate(r));
  const edit = sb.__calls.find(c => c.url.endsWith('/edit_task'));
  assert.ok(edit, 'edit_task was not called');
  assert.equal(edit.body.read_only, true);
  assert.equal('new_spec' in edit.body, false);

  // Unchanged toggle and spec: no edit_task call at all.
  sb.__calls.length = 0;
  els.prdEditReadOnly.checked = false;
  mounted.f();
  await new Promise(r => setImmediate(r));
  assert.equal(sb.__calls.some(c => c.url.endsWith('/edit_task')), false);
});
