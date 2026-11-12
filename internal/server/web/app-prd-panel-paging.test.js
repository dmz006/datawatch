// The Automata panel (loadPRDPanel) pages the summary list instead of
// fetching every PRD in full: the first load asks for view=summary with a
// limit, "Load more" sends the cursor and appends, a failed load shows Retry,
// and opening a row fetches that one PRD by id.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));

function harness(route, byId = {}) {
  const panel = makeStubElement();
  const sandbox = buildSandbox({ byId: Object.assign({ prdPanel: panel }, byId) });
  sandbox.setInterval = () => 0;
  sandbox.clearInterval = () => {};
  sandbox.AbortController = AbortController;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n = sandbox._i18n || {};
  sandbox._i18n.bundle = EN;
  const calls = [];
  sandbox.fetch = (url) => {
    calls.push(String(url));
    return Promise.resolve(route(String(url))).then(r => {
      if (r instanceof Error) throw r;
      const status = r.status || 200;
      const text = typeof r.body === 'string' ? r.body : JSON.stringify(r.body === undefined ? {} : r.body);
      return { ok: status >= 200 && status < 300, status, headers: { get() { return null; } }, text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)) };
    });
  };
  const run = code => vm.runInContext(code, sandbox);
  return { sandbox, panel, calls, run };
}

const ROW = (id, extra) => Object.assign({ id, title: 'Auto ' + id, status: 'running', rev: 1, updated_at: '2026-10-23T01:00:00Z', story_count: 1, task_count: 1, decision_count: 0, task_status_counts: { pending: 1 } }, extra);
const FULL = id => Object.assign(ROW(id), { spec: 'the spec', stories: [{ id: 's1', title: 'Story one', status: 'pending', tasks: [{ id: 't1', title: 'Task one', status: 'pending' }] }] });

function routes(over = {}) {
  return (url) => {
    if (/^\/api\/autonomous\/prds\/[^/?]+\/children/.test(url)) return { body: { children: [] } };
    if (/^\/api\/autonomous\/prds\/[^/?]+$/.test(url)) return over.detail ? over.detail(url) : { body: FULL(decodeURIComponent(url.split('/').pop())) };
    if (url.startsWith('/api/autonomous/prds')) return over.list ? over.list(url) : { body: { prds: [ROW('p1'), ROW('p2')], total: 2 } };
    if (url.startsWith('/api/llms')) return { body: { llms: [] } };
    return { body: {} };
  };
}
const listCalls = h => h.calls.filter(u => u.startsWith('/api/autonomous/prds?'));

test('first load requests the summary view with a limit and never the full form', async () => {
  const h = harness(routes());
  h.run('loadPRDPanel()');
  await flushAsync(30);
  const lists = listCalls(h);
  assert.equal(lists.length, 1, lists.join());
  assert.match(lists[0], /[?&]view=summary(&|$)/);
  assert.match(lists[0], /[?&]limit=\d+(&|$)/);
  assert.doesNotMatch(lists[0], /cursor=/);
  assert.ok(!h.calls.some(u => u.includes('full=1')), 'never the full list: ' + h.calls.join());
  assert.match(h.panel.innerHTML, /Auto p1/);
  assert.match(h.panel.innerHTML, /Auto p2/);
  // A collapsed row shows counts from the summary and loads no PRD by id.
  assert.match(h.panel.innerHTML, /1 stories &middot; 1 tasks/);
  assert.ok(!h.calls.some(u => /^\/api\/autonomous\/prds\/p\d/.test(u)), h.calls.join());
});

test('Load more sends next_cursor and appends the rows', async () => {
  const h = harness(routes({
    list: (url) => url.includes('cursor=CUR1')
      ? { body: { prds: [ROW('p3')], total: 3 } }
      : { body: { prds: [ROW('p1'), ROW('p2')], total: 3, next_cursor: 'CUR1' } },
  }));
  h.run('loadPRDPanel()');
  await flushAsync(30);
  assert.match(h.panel.innerHTML, /prdPanelLoadMore/);
  assert.doesNotMatch(h.panel.innerHTML, /Auto p3/);
  h.run('prdPanelLoadMore()');
  await flushAsync(30);
  const more = listCalls(h).filter(u => u.includes('cursor='));
  assert.equal(more.length, 1);
  assert.match(more[0], /view=summary/);
  assert.match(more[0], /cursor=CUR1(&|$)/);
  assert.match(h.panel.innerHTML, /Auto p1/);
  assert.match(h.panel.innerHTML, /Auto p3/);
  assert.doesNotMatch(h.panel.innerHTML, /prdPanelLoadMore/, 'no control once the last page is in');
});

test('a failed load shows the failed state with Retry, and Retry recovers', async () => {
  let fail = true;
  const h = harness(routes({ list: () => (fail ? { status: 500, body: 'boom' } : { body: { prds: [ROW('p1')], total: 1 } }) }));
  h.run('loadPRDPanel()');
  await flushAsync(30);
  assert.match(h.panel.innerHTML, /list-state-failed/);
  assert.match(h.panel.innerHTML, /list-state-retry/);
  assert.match(h.panel.innerHTML, />Retry</);
  fail = false;
  h.run('window._listLoaders.prdpanel.retry()');
  await flushAsync(30);
  assert.doesNotMatch(h.panel.innerHTML, /list-state-failed/);
  assert.match(h.panel.innerHTML, /Auto p1/);
});

test('opening a row requests the by-id route exactly once and renders its stories', async () => {
  const h = harness(routes());
  h.run('loadPRDPanel()');
  await flushAsync(30);
  h.run("prdPanelToggle('p1', { open: true })");
  await flushAsync(30);
  const byId = h.calls.filter(u => u === '/api/autonomous/prds/p1');
  assert.equal(byId.length, 1, h.calls.join());
  assert.match(h.panel.innerHTML, /Story one/);
  // Close, open again and refresh: still the one fetch, the revision is unchanged.
  h.run("prdPanelToggle('p1', { open: false })");
  h.run("prdPanelToggle('p1', { open: true })");
  h.run('loadPRDPanel()');
  await flushAsync(30);
  assert.equal(h.calls.filter(u => u === '/api/autonomous/prds/p1').length, 1, h.calls.join());
  assert.equal(h.calls.filter(u => u === '/api/autonomous/prds/p1/children').length, 1, h.calls.join());
  assert.ok(!h.calls.some(u => u === '/api/autonomous/prds/p2'), 'a collapsed row loads nothing');
});

test('a refresh refetches the summary pages already loaded, never the full form', async () => {
  const h = harness(routes({
    list: (url) => url.includes('cursor=CUR1')
      ? { body: { prds: [ROW('p3')], total: 3 } }
      : { body: { prds: [ROW('p1'), ROW('p2')], total: 3, next_cursor: 'CUR1' } },
  }));
  h.run('loadPRDPanel()');
  await flushAsync(30);
  h.run('prdPanelLoadMore()');
  await flushAsync(30);
  h.calls.length = 0;
  h.run('loadPRDPanel()');
  await flushAsync(30);
  const lists = listCalls(h);
  assert.equal(lists.length, 1, lists.join());
  assert.match(lists[0], /view=summary/);
  assert.match(lists[0], /limit=\d+/);
  assert.doesNotMatch(lists[0], /cursor=/);
  assert.ok(!h.calls.some(u => u.includes('full=1')));
});

test('the status filter goes to the server and templates are filtered over the loaded pages', async () => {
  const h = harness(routes({
    list: () => ({ body: { prds: [ROW('p1'), ROW('t1', { is_template: true, title: 'Template one' })], total: 2 } }),
  }), { prdFilterStatus: { value: 'running' }, prdIncludeTemplates: { checked: false } });
  h.run('loadPRDPanel()');
  await flushAsync(30);
  assert.match(listCalls(h)[0], /status=running/);
  assert.match(h.panel.innerHTML, /Auto p1/);
  assert.doesNotMatch(h.panel.innerHTML, /Template one/);
});

test('a summary row keeps its actions; Edit fetches the full PRD first', async () => {
  const h = harness(routes({ list: () => ({ body: { prds: [ROW('p1', { status: 'needs_review' })], total: 1 } }) }));
  h.run('loadPRDPanel()');
  await flushAsync(30);
  const html = h.panel.innerHTML;
  for (const label of ['Approve', 'Reject', 'Revise', 'Edit', 'LLM', 'Permission', 'Delete', 'Repair deps']) {
    assert.ok(html.includes('>' + label + '<'), 'missing action ' + label);
  }
  assert.match(html, /openDashExpand/);
  assert.ok(!h.calls.some(u => u === '/api/autonomous/prds/p1'), 'rendering a row fetches nothing by id');
  h.run("window.__edit = null; openPRDEditModal = (id, title, spec) => { window.__edit = [id, title, spec]; }");
  h.run("prdPanelOpenFull('p1', 'edit')");
  await flushAsync(30);
  assert.deepEqual(Array.from(h.run('window.__edit')), ['p1', 'Auto p1', 'the spec']);
  assert.equal(h.calls.filter(u => u === '/api/autonomous/prds/p1').length, 1);
});
