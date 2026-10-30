// Search-before-create overlap block: the render helper must cover each
// warning kind, render nothing for an empty or missing list, and both create
// paths must hand the create response's overlap_warnings to it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const EN_BUNDLE = path.join(__dirname, 'locales', 'en.json');

// loadAppJS loads the real app.js into a fresh sandbox. The English bundle is
// installed so t() returns the English text, which lets the tests read the
// rendered wording rather than the key names.
function loadAppJS(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(EN_BUNDLE, 'utf8'));
  vm.runInContext('window._i18n.bundle = ' + JSON.stringify(en), sandbox);
  return sandbox;
}

function call(sandbox, fnName, ...args) {
  sandbox.__args = args;
  const result = vm.runInContext(`${fnName}(...__args)`, sandbox);
  delete sandbox.__args;
  return result;
}

const AUTOMATON_WARNING = {
  kind: 'automaton', id: 'a-old', status: 'running', score: 0.42,
  shared_terms: ['backup'], reason: 'overlaps an earlier Automaton',
};
const REGISTRY_WARNING = {
  kind: 'registry', row: 7, score: 0.31, shared_terms: ['backup'],
  reason: 'matches a registry row',
};
const BRANCH_WARNING = {
  kind: 'branch', branch: 'feat/backup-job', score: 0.27, shared_terms: ['backup'],
  reason: 'matches an unmerged branch',
};

test('renders an automaton line with its id, status, score and read hint', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderOverlapWarnings', [AUTOMATON_WARNING]);
  assert.match(html, /Automaton/);
  assert.match(html, /a-old \(running\)/);
  assert.match(html, /0\.42/);
  assert.match(html, /Read this item before you start new work/);
});

test('renders a registry line with its row number', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderOverlapWarnings', [REGISTRY_WARNING]);
  assert.match(html, /Registry row/);
  assert.match(html, />\s*7\s*</);
  assert.match(html, /0\.31/);
});

test('renders a branch line with the branch name', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderOverlapWarnings', [BRANCH_WARNING]);
  assert.match(html, /Unmerged branch/);
  assert.match(html, /feat\/backup-job/);
  assert.match(html, /0\.27/);
});

test('renders every kind in one block with one line per warning', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderOverlapWarnings', [AUTOMATON_WARNING, REGISTRY_WARNING, BRANCH_WARNING]);
  assert.equal((html.match(/<li/g) || []).length, 3);
});

test('renders nothing for an empty, missing or non-array list', () => {
  const sandbox = loadAppJS();
  assert.equal(call(sandbox, 'renderOverlapWarnings', []), '');
  assert.equal(call(sandbox, 'renderOverlapWarnings', undefined), '');
  assert.equal(call(sandbox, 'renderOverlapWarnings', null), '');
  assert.equal(call(sandbox, 'renderOverlapWarnings', 'not a list'), '');
});

test('skips a warning of an unknown kind and renders nothing when none is left', () => {
  const sandbox = loadAppJS();
  const unavailable = { kind: 'unavailable', score: 0, reason: 'source failed' };
  assert.equal(call(sandbox, 'renderOverlapWarnings', [unavailable]), '');
  const html = call(sandbox, 'renderOverlapWarnings', [unavailable, BRANCH_WARNING]);
  assert.equal((html.match(/<li/g) || []).length, 1);
});

test('escapes server-supplied values', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderOverlapWarnings', [
    { kind: 'automaton', id: '<img src=x onerror=alert(1)>', status: 'running', score: 0.5 },
  ]);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
});

test('the only control in the block is the close control', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderOverlapWarnings', [AUTOMATON_WARNING]);
  const buttons = html.match(/<button[^>]*>/g) || [];
  assert.equal(buttons.length, 1);
  assert.match(buttons[0], /onclick="closeOverlapWarnings\(\)"/);
  assert.equal((html.match(/onclick=/g) || []).length, 1);
});

test('wizard create path hands the response warnings to the render helper', async () => {
  const byId = {
    wizardIntent: { value: 'Add a backup job' },
    selectedDirDisplay: { textContent: '/work/proj' },
  };
  const sandbox = loadAppJS(byId);
  const warnings = [BRANCH_WARNING];
  const seen = [];
  sandbox.renderOverlapWarnings = w => { seen.push(w); return ''; };
  sandbox.apiFetch = url => Promise.resolve(
    url === '/api/autonomous/prds' ? { id: 'a-new', overlap_warnings: warnings } : {}
  );
  sandbox._prdCloseModal = () => {};
  sandbox.renderPRDDetailView = () => {};
  sandbox.showToast = () => {};
  vm.runInContext('_wizardSubmit()', sandbox);
  await flushAsync();
  assert.equal(seen.length, 1);
  assert.deepEqual(seen[0], warnings);
});

test('PRD modal create path hands the response warnings to the render helper', async () => {
  const submitHandlers = [];
  const form = {
    addEventListener(type, fn) { if (type === 'submit') submitHandlers.push(fn); },
  };
  const byId = {
    prdModalForm: form,
    prdNewSpec: { value: 'Add a backup job' },
    prdNewProfile: { value: '__dir__' },
    selectedDirDisplay: { textContent: '/work/proj' },
  };
  const sandbox = loadAppJS(byId);
  const warnings = [REGISTRY_WARNING];
  const seen = [];
  sandbox.renderOverlapWarnings = w => { seen.push(w); return ''; };
  sandbox.apiFetch = (url, opts) => Promise.resolve(
    opts && opts.method === 'POST' ? { id: 'a-new', overlap_warnings: warnings } : []
  );
  sandbox.showToast = () => {};
  sandbox._refreshAutomataOrPRD = () => {};
  call(sandbox, 'openPRDCreateModal');
  // The modal mounts after its option lists settle, so wait for those first.
  await flushAsync(20);
  assert.equal(submitHandlers.length, 1);
  submitHandlers[0]({ preventDefault() {} });
  await flushAsync();
  assert.equal(seen.length, 1);
  assert.deepEqual(seen[0], warnings);
});
