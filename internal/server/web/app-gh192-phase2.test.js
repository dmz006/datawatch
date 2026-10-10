// GH#192 Phase 2 — #2 per-Automaton memory section. Reuses the existing
// BL386 Phase 4 (GET /api/autonomous/prds/{id}/memory-report) and Phase 5
// (GET /api/memory/scopes/recall) REST endpoints -- no backend change.
// See docs/plans/historical-plans/2026-10-07-gh192-parity-batch.md.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(byId) {
  const sandbox = buildSandbox({ byId });
  sandbox.URLSearchParams = URLSearchParams;
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

// makeStubElement()'s `value` property is a fixed no-op getter/setter
// (always reads back ''), so a text-input stub needs its own writable
// `value` instead.
function makeInputStub(initialValue) {
  return { value: initialValue };
}

function mockFetchJSON(sandbox, byUrl) {
  sandbox.fetch = (url) => {
    const body = byUrl[url];
    if (body === undefined) return Promise.resolve({ ok: false, status: 404, text: () => Promise.resolve('not found') });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
}

test('_renderDetailTabStrip includes an always-visible Memory tab', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, '_renderDetailTabStrip', { id: 'p1' }, 'overview');
  assert.ok(/switchAutomataDetailTab\('memory'\)/.test(html), 'expected a Memory tab button');
});

test('_renderDetailMemoryTab renders the stats/report/recall mount points for the given PRD id', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, '_renderDetailMemoryTab', { id: 'prd-42' });
  assert.ok(html.includes('prdMemoryStats_prd-42'));
  assert.ok(html.includes('prdMemoryReport_prd-42'));
  assert.ok(html.includes('prdMemoryRecall_prd-42'));
  assert.ok(html.includes('prdMemoryRecallQuery_prd-42'));
});

test('_renderPRDMemoryStats counts memories per scope (prd-shared/story-shared/session-local)', () => {
  const sandbox = loadAppJS();
  const el = makeStubElement();
  call(sandbox, '_renderPRDMemoryStats', el, [
    { scope: 'prd-shared', memories: [{ id: 1 }, { id: 2 }] },
    { scope: 'story-shared', scope_id: 'story-1', memories: [{ id: 3 }] },
    { scope: 'story-shared', scope_id: 'story-2', memories: [{ id: 4 }, { id: 5 }] },
    { scope: 'session-local', memories: [{ id: 6 }] },
  ]);
  // 2 prd-shared, 1+2=3 story-shared (summed across both story entries), 1 session-local
  const matches = el.innerHTML.match(/stat-value">(\d+)</g).map(s => s.match(/\d+/)[0]);
  assert.deepEqual(matches, ['2', '3', '1']);
});

test('_renderPRDMemoryReportList flattens memories across scopes and tags each with its scope', () => {
  const sandbox = loadAppJS();
  const el = makeStubElement();
  call(sandbox, '_renderPRDMemoryReportList', el, [
    { scope: 'prd-shared', memories: [{ id: 1, content: 'alpha' }] },
    { scope: 'story-shared', scope_id: 'story-9', memories: [{ id: 2, content: 'beta' }] },
  ]);
  assert.ok(el.innerHTML.includes('alpha'));
  assert.ok(el.innerHTML.includes('beta'));
  assert.ok(el.innerHTML.includes('prd-shared'));
  assert.ok(el.innerHTML.includes('story-shared'));
  assert.ok(el.innerHTML.includes('story-9'));
});

test('_renderPRDMemoryReportList shows the empty-state message when no scope has any memories', () => {
  const sandbox = loadAppJS();
  const el = makeStubElement();
  call(sandbox, '_renderPRDMemoryReportList', el, [{ scope: 'prd-shared', memories: [] }]);
  assert.ok(!el.innerHTML.includes('settings-row'));
});

test('_renderPRDMemoryRecallList renders rows, or a "no results" message when the scope walk is empty', () => {
  const sandbox = loadAppJS({ 'prdMemoryRecall_p1': makeStubElement() });
  call(sandbox, '_renderPRDMemoryRecallList', 'p1', [{ id: 7, content: 'gamma', scope: 'prd-shared' }]);
  const el = vm.runInContext("document.getElementById('prdMemoryRecall_p1')", sandbox);
  assert.ok(el.innerHTML.includes('gamma'));

  call(sandbox, '_renderPRDMemoryRecallList', 'p1', []);
  assert.ok(!el.innerHTML.includes('gamma'));
});

test('_filterPRDMemoryRecall filters the cached recall list by content substring, case-insensitively', () => {
  const queryInput = makeInputStub('ALPHA');
  const recallEl = makeStubElement();
  const sandbox = loadAppJS({ 'prdMemoryRecallQuery_p1': queryInput, 'prdMemoryRecall_p1': recallEl });
  sandbox.__cache = { 'p1': [{ id: 1, content: 'has alpha in it' }, { id: 2, content: 'no match here' }] };
  vm.runInContext('_prdMemoryRecallCache = __cache', sandbox);
  call(sandbox, '_filterPRDMemoryRecall', 'p1');
  assert.ok(recallEl.innerHTML.includes('has alpha in it'));
  assert.ok(!recallEl.innerHTML.includes('no match here'));
});

test('_filterPRDMemoryRecall with an empty query shows every cached entry', () => {
  const queryInput = makeInputStub('');
  const recallEl = makeStubElement();
  const sandbox = loadAppJS({ 'prdMemoryRecallQuery_p1': queryInput, 'prdMemoryRecall_p1': recallEl });
  sandbox.__cache = { 'p1': [{ id: 1, content: 'one' }, { id: 2, content: 'two' }] };
  vm.runInContext('_prdMemoryRecallCache = __cache', sandbox);
  call(sandbox, '_filterPRDMemoryRecall', 'p1');
  assert.ok(recallEl.innerHTML.includes('one'));
  assert.ok(recallEl.innerHTML.includes('two'));
});

test('_loadPRDMemoryTab fetches the memory-report and scoped-recall endpoints, locked to this PRD\'s id + project_dir', async () => {
  const statsEl = makeStubElement();
  const reportEl = makeStubElement();
  const recallEl = makeStubElement();
  const sandbox = loadAppJS({
    'prdMemoryStats_prd-7': statsEl,
    'prdMemoryReport_prd-7': reportEl,
    'prdMemoryRecall_prd-7': recallEl,
  });
  let recallUrl = null;
  mockFetchJSON(sandbox, {
    '/api/autonomous/prds/prd-7/memory-report': [
      { scope: 'prd-shared', memories: [{ id: 1, content: 'report entry' }] },
    ],
  });
  const realFetch = sandbox.fetch;
  sandbox.fetch = (url, opts) => {
    if (url.startsWith('/api/memory/scopes/recall')) {
      recallUrl = url;
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ results: [{ id: 2, content: 'recalled entry', scope: 'prd-shared' }] }) });
    }
    return realFetch(url, opts);
  };

  await call(sandbox, '_loadPRDMemoryTab', { id: 'prd-7', project_dir: '/home/dmz/workspace/datawatch' });
  // Both fetches are fire-and-forget promises inside _loadPRDMemoryTab; give them a tick.
  await new Promise(r => setImmediate(r));

  assert.ok(reportEl.innerHTML.includes('report entry'));
  assert.ok(recallEl.innerHTML.includes('recalled entry'));
  assert.ok(recallUrl.includes('prd_id=prd-7'), `expected prd_id in recall URL, got: ${recallUrl}`);
  assert.ok(recallUrl.includes('project='), `expected project in recall URL, got: ${recallUrl}`);
});

test('_loadPRDMemoryTab shows the unavailable message when the memory-report fetch fails', async () => {
  const statsEl = makeStubElement();
  const reportEl = makeStubElement();
  const sandbox = loadAppJS({
    'prdMemoryStats_prd-8': statsEl,
    'prdMemoryReport_prd-8': reportEl,
    'prdMemoryRecall_prd-8': makeStubElement(),
  });
  mockFetchJSON(sandbox, {}); // nothing matches -> 404 for both calls
  await call(sandbox, '_loadPRDMemoryTab', { id: 'prd-8' });
  await new Promise(r => setImmediate(r));
  assert.ok(reportEl.innerHTML.toLowerCase().includes('unavailable') || reportEl.innerHTML.length > 0);
});
