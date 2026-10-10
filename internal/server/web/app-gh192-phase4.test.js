// GH#192 Phase 4 — #4 memory tags, PWA side. The REST/MCP/Store end of
// this (full end-to-end, per operator decision) is tested in
// internal/memory, internal/server, internal/mcp. This covers
// addMemoryQuick()'s new tags input and the memory browser's tag-chip
// display. See docs/plans/historical-plans/2026-10-07-gh192-parity-batch.md.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function makeInputStub(initialValue) {
  return { value: initialValue };
}

function loadAppJS(byId) {
  const sandbox = buildSandbox({ byId });
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

function mockFetchJSON(sandbox, byUrl) {
  sandbox.fetch = (url, opts) => {
    const key = url;
    const entry = byUrl[key];
    if (entry === undefined) return Promise.resolve({ ok: false, status: 404, text: () => Promise.resolve('not found') });
    if (typeof entry === 'function') entry(opts);
    const body = typeof entry === 'function' ? {} : entry;
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
}

test('_renderMemoryTagChips renders one chip per tag, trimmed', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, '_renderMemoryTagChips', 'work, bug , urgent');
  assert.equal((html.match(/<span/g) || []).length, 3);
  assert.ok(html.includes('work'));
  assert.ok(html.includes('bug'));
  assert.ok(html.includes('urgent'));
});

test('_renderMemoryTagChips returns an empty string for no tags (absent or empty)', () => {
  const sandbox = loadAppJS();
  assert.equal(call(sandbox, '_renderMemoryTagChips', ''), '');
  assert.equal(call(sandbox, '_renderMemoryTagChips', undefined), '');
  assert.equal(call(sandbox, '_renderMemoryTagChips', null), '');
});

test('_renderMemoryTagChips escapes tag content (no raw HTML injection)', () => {
  const sandbox = loadAppJS();
  const html = call(sandbox, '_renderMemoryTagChips', '<img src=x onerror=alert(1)>');
  assert.ok(!html.includes('<img'));
  assert.ok(html.includes('&lt;img'));
});

test('addMemoryQuick sends tags in the request body when the tags field is filled in', async () => {
  const contentInput = makeInputStub('a memory');
  const tagsInput = makeInputStub('work, bug');
  const sandbox = loadAppJS({
    memoryQuickAddInput: contentInput,
    memoryQuickAddTags: tagsInput,
    memoryBrowserList: makeStubElement(),
  });
  let sentBody = null;
  mockFetchJSON(sandbox, {
    '/api/memory/save': (opts) => { sentBody = JSON.parse(opts.body); },
    '/api/memory/list?n=50': [],
  });
  await call(sandbox, 'addMemoryQuick');
  await new Promise(r => setImmediate(r));
  assert.deepEqual(sentBody, { content: 'a memory', tags: 'work, bug' });
});

test('addMemoryQuick omits the tags field entirely when the tags input is empty', async () => {
  const contentInput = makeInputStub('a memory');
  const tagsInput = makeInputStub('');
  const sandbox = loadAppJS({
    memoryQuickAddInput: contentInput,
    memoryQuickAddTags: tagsInput,
    memoryBrowserList: makeStubElement(),
  });
  let sentBody = null;
  mockFetchJSON(sandbox, {
    '/api/memory/save': (opts) => { sentBody = JSON.parse(opts.body); },
    '/api/memory/list?n=50': [],
  });
  await call(sandbox, 'addMemoryQuick');
  await new Promise(r => setImmediate(r));
  assert.deepEqual(sentBody, { content: 'a memory' });
  assert.ok(!('tags' in sentBody));
});

test('addMemoryQuick clears both the content and tags inputs after a successful save', async () => {
  const contentInput = makeInputStub('a memory');
  const tagsInput = makeInputStub('work');
  const sandbox = loadAppJS({
    memoryQuickAddInput: contentInput,
    memoryQuickAddTags: tagsInput,
    memoryBrowserList: makeStubElement(),
  });
  mockFetchJSON(sandbox, { '/api/memory/save': {}, '/api/memory/list?n=50': [] });
  await call(sandbox, 'addMemoryQuick');
  await new Promise(r => setImmediate(r));
  assert.equal(contentInput.value, '');
  assert.equal(tagsInput.value, '');
});

test('listMemories shows a tag-chip row for a memory that has tags, and none for one that does not', async () => {
  const listEl = makeStubElement();
  const sandbox = loadAppJS({ memoryBrowserList: listEl });
  mockFetchJSON(sandbox, {
    '/api/memory/list?n=50': [
      { id: 1, role: 'manual', content: 'tagged one', tags: 'work,bug' },
      { id: 2, role: 'manual', content: 'untagged one' },
    ],
  });
  await call(sandbox, 'listMemories');
  await new Promise(r => setImmediate(r));
  assert.ok(listEl.innerHTML.includes('work'));
  assert.ok(listEl.innerHTML.includes('bug'));
  assert.ok(listEl.innerHTML.includes('tagged one'));
  assert.ok(listEl.innerHTML.includes('untagged one'));
});
