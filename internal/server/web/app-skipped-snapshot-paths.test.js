// Skipped snapshot paths on the Automata task card: task.skipped_snapshot_paths
// (array of {path, reason}) renders as a labelled row of path + reason chips
// below the touched row, all text escaped, and the row is omitted entirely
// when the list is absent or empty.

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

// Returns the inner HTML of the first <div class="<cls>"> ... </div> row.
// Rows contain no nested divs, so the first closing </div> ends the row.
function rowHTML(html, cls) {
  const start = html.indexOf(`<div class="${cls}">`);
  if (start < 0) return null;
  const end = html.indexOf('</div>', start);
  return html.slice(start, end);
}

const PRD = { id: 'p1', status: 'running' };

test('renderTask shows skipped snapshot paths as path + reason chips', () => {
  const sandbox = loadAppJS();
  const task = {
    id: 't1',
    status: 'running', // default-expanded
    files_touched: ['src/touched.go'],
    skipped_snapshot_paths: [
      { path: 'secrets/.env', reason: 'never-stage' },
      { path: 'build/out.bin', reason: 'ignored' },
    ],
  };
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, task, false);

  const skipped = rowHTML(html, 'prd-task-skipped-snapshot');
  assert.ok(skipped, 'skipped-snapshot row present');
  assert.ok(skipped.includes('prd-task-skipped-snapshot-label'));
  // Label comes from the new i18n key (the stubbed t() returns the key itself);
  // in the browser it resolves through locales, 'Left unstaged:' is the
  // in-code fallback when the key is missing.
  assert.ok(skipped.includes('prd_skipped_snapshot_label'));
  assert.ok(!skipped.includes('prd_files_touched_label'), 'not the touched label');

  assert.equal(skipped.split('prd-task-skipped-chip').length - 1, 2, 'one chip per entry');
  assert.ok(skipped.includes('secrets/.env'));
  assert.ok(skipped.includes('build/out.bin'));
  assert.ok(skipped.includes('prd-task-skipped-reason'));
  assert.ok(skipped.match(/never-stage/g).length >= 2, 'reason in chip text and title');
  assert.ok(skipped.match(/ignored/g).length >= 2, 'reason in chip text and title');

  // The skipped row sits between the touched and generated rows.
  const touched = rowHTML(html, 'prd-task-touched');
  assert.ok(touched, 'touched row present');
  assert.ok(html.indexOf('prd-task-touched') < html.indexOf('prd-task-skipped-snapshot'));

  // Skip chips are plain escaped text, not clickable file-chip affordances.
  assert.ok(!skipped.includes('prd-file-chip'), 'no file-chip view/download buttons');
});

test('renderTask shows reason-less entries as bare path chips', () => {
  const sandbox = loadAppJS();
  const task = { id: 't2', status: 'running', skipped_snapshot_paths: [{ path: 'a.log' }] };
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, task, false);
  const skipped = rowHTML(html, 'prd-task-skipped-snapshot');
  assert.ok(skipped, 'skipped-snapshot row present');
  assert.ok(skipped.includes('a.log'));
  assert.ok(!skipped.includes('prd-task-skipped-reason'), 'no reason span without a reason');
});

test('renderTask omits the skipped-snapshot row when absent or empty', () => {
  const sandbox = loadAppJS();
  const plain = call(sandbox, 'renderTask', PRD, { id: 's1' }, { id: 't3', status: 'running', files_touched: ['a.go'] }, false);
  assert.equal(rowHTML(plain, 'prd-task-skipped-snapshot'), null, 'absent list -> no row');

  const empty = call(sandbox, 'renderTask', PRD, { id: 's1' }, { id: 't4', status: 'running', skipped_snapshot_paths: [] }, false);
  assert.equal(rowHTML(empty, 'prd-task-skipped-snapshot'), null, 'empty list -> no row');
});

test('renderTask drops malformed skipped-snapshot entries', () => {
  const sandbox = loadAppJS();
  const task = {
    id: 't5',
    status: 'running',
    skipped_snapshot_paths: ['just-a-string', { reason: 'no path' }, { path: 'ok.log', reason: 'ignored' }],
  };
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, task, false);
  const skipped = rowHTML(html, 'prd-task-skipped-snapshot');
  assert.ok(skipped, 'row present for the one valid entry');
  assert.equal(skipped.split('prd-task-skipped-chip').length - 1, 1, 'only the entry with a path renders');
  assert.ok(skipped.includes('ok.log'));
  assert.ok(!skipped.includes('just-a-string'));
});

test('renderTask escapes skipped snapshot paths and reasons', () => {
  const sandbox = loadAppJS();
  const task = {
    id: 't6',
    status: 'running',
    skipped_snapshot_paths: [
      { path: '<img src=x onerror=alert(1)>', reason: '" onclick="evil()' },
    ],
  };
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, task, false);
  assert.ok(!html.includes('<img'), 'malicious path not injected');
  assert.ok(!html.includes('" onclick="evil'), 'malicious reason not injected into attributes');
  const skipped = rowHTML(html, 'prd-task-skipped-snapshot');
  assert.ok(skipped.includes('&lt;img'), 'path escaped in text content');
});
