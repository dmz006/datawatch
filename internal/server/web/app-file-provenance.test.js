// File provenance on the Automata task/story cards: planned (files),
// touched (files_touched) and generated (files_generated +
// verification_script) render as three distinct labelled rows, and
// verification output never shows up under the touched label.

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

const PRD = { id: 'p1', status: 'running', project_dir: '/repo' };

const TASK = {
  id: 't1',
  title: 'task one',
  status: 'running', // default-expanded
  files: ['src/planned.go'],
  files_touched: ['src/touched.go', '.datawatch/verify/t1.sh'],
  files_generated: ['.datawatch/verify/t1.sh', '.datawatch/verify/t1.log'],
  verification_script: '.datawatch/verify/t1.sh',
};

test('renderTask shows planned, touched and generated as three distinct rows', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, TASK, false);

  const planned = rowHTML(html, 'prd-task-files');
  const touched = rowHTML(html, 'prd-task-touched');
  const generated = rowHTML(html, 'prd-task-generated');
  assert.ok(planned, 'planned row present');
  assert.ok(touched, 'touched row present');
  assert.ok(generated, 'generated row present');

  assert.ok(planned.includes('prd-task-files-label'));
  assert.ok(touched.includes('prd-task-touched-label'));
  assert.ok(generated.includes('prd-task-generated-label'));
  assert.ok(!html.includes('Output files:'), 'old ambiguous label is gone');

  assert.ok(planned.includes('/repo/src/planned.go'));
  assert.ok(touched.includes('src/touched.go'));
  assert.ok(generated.includes('/repo/.datawatch/verify/t1.sh'));
  assert.ok(generated.includes('/repo/.datawatch/verify/t1.log'));
  assert.ok(generated.includes('prd-verification-script-tag'));
  // Script listed once even though it is also in files_generated.
  assert.equal(generated.split('title="/repo/.datawatch/verify/t1.sh"').length - 1, 1);

  // Generated paths never appear under the touched label.
  assert.ok(!touched.includes('verify/t1.sh'));
  assert.ok(!touched.includes('verify/t1.log'));
  assert.ok(!planned.includes('verify/'));
});

test('renderTask shows the verification script even with empty files_generated, read-only', () => {
  const sandbox = loadAppJS();
  const task = { id: 't2', status: 'running', verification_script: '.datawatch/verify/t2.sh' };
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, task, true);
  const generated = rowHTML(html, 'prd-task-generated');
  assert.ok(generated);
  assert.ok(generated.includes('/repo/.datawatch/verify/t2.sh'));
  assert.ok(generated.includes('prd-verification-script-tag'));
  assert.ok(!generated.includes('prd-story-edit-icon'), 'no edit affordance on generated row');
});

test('renderTask omits the generated row when there is no verification output', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, 'renderTask', PRD, { id: 's1' }, { id: 't3', status: 'running', files_touched: ['a.go'] }, false);
  assert.equal(rowHTML(html, 'prd-task-generated'), null);
});

test('renderTask escapes generated paths', () => {
  const sandbox = loadAppJS();
  const task = { id: 't4', status: 'running', files_generated: ['<img src=x onerror=alert(1)>.sh'] };
  const html = call(sandbox, 'renderTask', { id: 'p1', status: 'running' }, { id: 's1' }, task, false);
  assert.ok(!html.includes('<img'));
});

function storyFixture() {
  return {
    id: 's1',
    title: 'story one',
    files: ['src/planned.go'],
    files_generated: ['.datawatch/verify/s1.sh'],
    verification_script: '.datawatch/verify/s1.sh',
    tasks: [{ ...TASK }],
  };
}

for (const [label, prd] of [
  ['read-only (running)', { ...PRD, status: 'running' }],
  ['editable (needs_review)', { ...PRD, status: 'needs_review' }],
]) {
  test(`renderStory shows a separate generated row on the ${label} card`, () => {
    const sandbox = loadAppJS();
    const story = storyFixture();
    prd.stories = [story];
    vm.runInContext("state._prdStoryExpanded = {}; state._prdStoryExpanded['s1'] = true;", sandbox);
    const html = call(sandbox, 'renderStory', prd, story);

    const planned = rowHTML(html, 'prd-story-files');
    const generated = rowHTML(html, 'prd-story-generated-row');
    assert.ok(planned, 'planned row present');
    assert.ok(generated, 'generated row present');
    assert.ok(generated.includes('prd-story-generated-label'));
    assert.ok(planned.includes('/repo/src/planned.go'));
    assert.ok(!planned.includes('verify/s1.sh'));
    assert.ok(generated.includes('/repo/.datawatch/verify/s1.sh'));
    assert.ok(generated.includes('prd-verification-script-tag'));
    assert.ok(!generated.includes('prd-story-edit-icon'));

    const touched = rowHTML(html, 'prd-story-touched-row');
    if (prd.status === 'running') {
      assert.ok(touched, 'read-only card has the touched aggregate');
      assert.ok(touched.includes('src/touched.go'));
      assert.ok(!touched.includes('verify/'), 'generated paths excluded from touched aggregate');
    } else {
      assert.equal(touched, null, 'editable card has no touched aggregate');
    }
  });
}
