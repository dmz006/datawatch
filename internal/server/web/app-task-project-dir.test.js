// BL447 — per-task project dir override: badge on the task card, edit
// field in the task modal (escaped, placeholder when inherited).

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

const PRD = { id: 'p1', status: 'needs_review', project_dir: '/repo' };

test('renderTask shows a project dir badge only when overridden', () => {
  const sandbox = loadAppJS();
  const withDir = call(sandbox, 'renderTask', PRD, { id: 's1' }, { id: 't1', title: 'x', status: 'pending', project_dir: '/repo/sub' }, false);
  assert.match(withDir, /prd-task-projdir-badge/);
  assert.match(withDir, /dir: \/repo\/sub/);
  const without = call(sandbox, 'renderTask', PRD, { id: 's1' }, { id: 't2', title: 'y', status: 'pending' }, false);
  assert.doesNotMatch(without, /prd-task-projdir-badge/);
});

test('renderTask escapes the project dir badge', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, { id: 't1', title: 'x', status: 'pending', project_dir: '/repo/<img src=x>' }, false);
  assert.doesNotMatch(html, /<img src=x>/);
});

test('project dir field shows value or inherits placeholder, escaped', () => {
  const sandbox = loadAppJS();
  const set = call(sandbox, '_prdTaskProjectDirFieldHTML', '/repo/"sub"');
  assert.match(set, /id="prdEditProjectDir"/);
  assert.match(set, /value="\/repo\/&quot;sub&quot;"/);
  const empty = call(sandbox, '_prdTaskProjectDirFieldHTML', '');
  assert.match(empty, /value=""/);
  assert.match(empty, /inherits Automaton dir|prd_task_projdir_placeholder/);
});
