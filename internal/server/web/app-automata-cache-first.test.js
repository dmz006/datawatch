// Automata tab — cache-first list: the last first page paints before the fetch
// resolves and is marked refreshing, the background fetch is conditional
// (If-None-Match) and repaints only the rows whose data changed, a failed fetch
// keeps the rows and shows its own error with Retry, a storage that throws still
// renders, no request carries full=true, and an opened Automaton lists its
// tasks from the paged task route with a Load more control.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales/en.json'), 'utf8'));

function harness(route, { store, throwingStorage = false } = {}) {
  const els = {};
  const sandbox = buildSandbox();
  sandbox.document.getElementById = id => {
    if (!els[id]) {
      const e = makeStubElement(() => { e.writes = (e.writes || 0) + 1; });
      e.attrs = {};
      e.setAttribute = (k, v) => { e.attrs[k] = v; };
      els[id] = e;
    }
    return els[id];
  };
  sandbox.AbortController = AbortController;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n = sandbox._i18n || {};
  sandbox._i18n.bundle = EN;
  if (throwingStorage) {
    const boom = () => { throw new Error('storage blocked'); };
    sandbox.localStorage = { getItem: boom, setItem: boom, removeItem: boom, key: boom, get length() { throw new Error('storage blocked'); } };
  } else if (store) {
    Object.keys(store).forEach(k => sandbox.localStorage.setItem(k, JSON.stringify(store[k])));
  }
  const calls = [];
  sandbox.fetch = (url, opts) => {
    const u = String(url);
    calls.push({ url: u, headers: (opts && opts.headers) || {} });
    return Promise.resolve(route(u, (opts && opts.headers) || {})).then(r => {
      if (r instanceof Error) throw r;
      const status = r.status || 200;
      const text = typeof r.body === 'string' ? r.body : JSON.stringify(r.body === undefined ? {} : r.body);
      const hdrs = r.headers || {};
      return { ok: status >= 200 && status < 300, status, statusText: '', headers: { get: k => hdrs[k] || null }, text: () => Promise.resolve(text), json: () => Promise.resolve(JSON.parse(text)) };
    });
  };
  return { sb: sandbox, els, calls, panel: () => els.prdPanel, run: code => vm.runInContext(code, sandbox) };
}

const ROW = (id, extra) => Object.assign({ id, title: 'Auto ' + id, status: 'running', rev: 1, updated_at: '2026-10-23T01:00:00Z', story_count: 1, task_count: 2, decision_count: 0, task_status_counts: { pending: 1 } }, extra);
const FULL = id => Object.assign(ROW(id), { spec: 'the spec', stories: [{ id: 's1', title: 'Story one', status: 'pending', tasks: [{ id: 't1', title: 'Task one', status: 'pending' }] }] });
const FIRST = (rows, extra) => Object.assign({ prds: rows, total: rows.length }, extra);

function routes(over = {}) {
  return (url, hd) => {
    if (over.all) { const r = over.all(url, hd); if (r !== undefined) return r; }
    if (/^\/api\/autonomous\/prds\/[^/?]+\/children/.test(url)) return { body: { children: [] } };
    if (/^\/api\/autonomous\/prds\/[^/?]+\/tasks/.test(url)) return over.tasks ? over.tasks(url) : { body: { prd_id: 'p1', total: 1, tasks: [{ id: 't1', title: 'Task one', status: 'pending', story: 's1' }] } };
    if (/^\/api\/autonomous\/prds\/[^/?]+$/.test(url)) return { body: FULL(decodeURIComponent(url.split('/').pop())) };
    if (url.startsWith('/api/autonomous/prds')) return over.list ? over.list(url, hd) : { body: FIRST([ROW('p1'), ROW('p2')]) };
    if (url.startsWith('/api/llms')) return { body: { llms: [] } };
    return { body: {} };
  };
}
// An update to one row lands in that row's body element, not in the whole list.
const rowBody = (h, id) => (h.els['prdRow_' + id + '_body'] || { innerHTML: '' }).innerHTML;
const listCalls = h => h.calls.filter(c => c.url.startsWith('/api/autonomous/prds?'));
const seeded = (rows, tag) => ({
  cs_list_prdpanel: { t: Date.now(), data: { prds: rows, total: rows.length, next: '' } },
  'cs_cf_local_automata_first_all': { tag: tag || '"a1"', data: FIRST(rows), at: Date.now() },
});

test('a cache hit paints the rows before the fetch resolves and marks them refreshing', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const h = harness(routes({ list: () => gate.then(() => ({ body: FIRST([ROW('p1'), ROW('p2')]) })) }), { store: seeded([ROW('p1'), ROW('p2')]) });
  const p = h.run('loadPRDPanel()');
  const html = h.panel().innerHTML;
  assert.match(html, /Auto p1/);
  assert.match(html, /Auto p2/);
  assert.match(html, /id="prdRow_p1"[^>]*data-state="refreshing"/, 'cache-rendered rows are marked refreshing');
  assert.match(html, /list-state-cached/);
  assert.doesNotMatch(html, /list-skel-row/, 'no skeleton when the cache holds the page');
  release();
  await p; await flushAsync(20);
  assert.equal(h.els.prdRow_p1.attrs['data-state'], 'ready', 'the marker clears when the fetch settles');
});

test('with no cache the list shows its own skeleton until the first page arrives', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const h = harness(routes({ list: () => gate.then(() => ({ body: FIRST([ROW('p1')]) })) }));
  const p = h.run('loadPRDPanel()');
  assert.match(h.panel().innerHTML, /list-state-skeleton/);
  release();
  await p; await flushAsync(20);
  assert.match(h.panel().innerHTML, /id="prdRow_p1"[^>]*data-state="ready"/);
});

test('the background fetch is conditional and a 304 repaints no row', async () => {
  const h = harness(routes({ list: (u, hd) => (hd['If-None-Match'] === '"a1"' ? { status: 304, body: '' } : { body: FIRST([ROW('p1'), ROW('p2')]), headers: { ETag: '"a2"' } }) }), { store: seeded([ROW('p1'), ROW('p2')], '"a1"') });
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  const lists = listCalls(h);
  assert.equal(lists.length, 1);
  assert.equal(lists[0].headers['If-None-Match'], '"a1"');
  assert.equal(h.run('_prdPanelState.rowsPainted || 0'), 0);
  assert.match(h.panel().innerHTML, /Auto p1/);
});

test('a refresh repaints only the rows whose data changed', async () => {
  let n = 0;
  const h = harness(routes({
    list: () => {
      n++;
      return { body: FIRST(n === 1 ? [ROW('p1'), ROW('p2')] : [ROW('p1'), ROW('p2', { title: 'Renamed p2', rev: 2 })]), headers: { ETag: '"e' + n + '"' } };
    },
  }));
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  assert.match(h.panel().innerHTML, /Auto p2/);
  const before = h.panel().writes;
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  assert.equal(h.panel().writes, before, 'the list element was not rewritten as a whole');
  assert.match(h.els.prdRow_p2_body.innerHTML, /Renamed p2/);
  assert.equal(h.els.prdRow_p2_body.writes, 1, 'the changed row was rewritten once');
  assert.ok(!h.els.prdRow_p1_body || !h.els.prdRow_p1_body.writes, 'the unchanged row was not rewritten');
  assert.equal(h.run('_prdPanelState.rowsPainted'), 1);
});

test('a new row set repaints the whole list', async () => {
  let n = 0;
  const h = harness(routes({ list: () => { n++; return { body: FIRST(n === 1 ? [ROW('p1')] : [ROW('p1'), ROW('p3')]) }; } }));
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  assert.match(h.panel().innerHTML, /Auto p3/);
});

test('a failed fetch keeps the cached rows and shows its own error with Retry', async () => {
  let fail = true;
  const h = harness(routes({ list: () => (fail ? { status: 500, body: 'list down' } : { body: FIRST([ROW('p1'), ROW('p2')]) }) }), { store: seeded([ROW('p1'), ROW('p2')]) });
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  const html = h.panel().innerHTML;
  assert.match(html, /list-state-failed/);
  assert.match(html, /list down/);
  assert.match(html, />Retry</);
  assert.match(html, /Auto p1/, 'the rows stay beside the error');
  fail = false;
  h.run('window._listLoaders.prdpanel.retry()');
  await flushAsync(20);
  assert.doesNotMatch(h.panel().innerHTML, /list-state-failed/);
});

test('a failed task page shows its own error with Retry and the rest of the row renders', async () => {
  let fail = true;
  const h = harness(routes({ tasks: () => (fail ? { status: 500, body: 'tasks down' } : { body: { prd_id: 'p1', total: 1, tasks: [{ id: 't1', title: 'Task one', status: 'pending' }] } }) }));
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  h.run("prdPanelToggle('p1', { open: true })");
  await flushAsync(40);
  const html = rowBody(h, 'p1');
  assert.match(html, /Story one/, 'the stories render');
  assert.match(html, /tasks down/);
  assert.match(html, /projectsCardRetry\(&quot;prdtasks:p1&quot;\)/);
  fail = false;
  h.run("projectsCardRetry('prdtasks:p1')");
  await flushAsync(40);
  assert.match(rowBody(h, 'p1'), /Task one/);
  assert.doesNotMatch(rowBody(h, 'p1'), /tasks down/);
});

test('a storage that throws still renders the list', async () => {
  const h = harness(routes(), { throwingStorage: true });
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  assert.match(h.panel().innerHTML, /Auto p1/);
  assert.match(h.panel().innerHTML, /id="prdRow_p1"/);
});

test('no request of the tab carries full=true', async () => {
  const h = harness(routes({ list: u => (u.includes('cursor=C1') ? { body: FIRST([ROW('p3')]) } : { body: FIRST([ROW('p1'), ROW('p2')], { total: 3, next_cursor: 'C1' }) }) }), { store: seeded([ROW('p1'), ROW('p2')]) });
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  h.run('prdPanelLoadMore()');
  await flushAsync(20);
  h.run("prdPanelToggle('p1', { open: true })");
  await flushAsync(30);
  h.run('loadPRDPanel()');
  await flushAsync(20);
  assert.ok(h.calls.length > 4);
  assert.deepEqual(h.calls.filter(c => /full=true/.test(c.url)), []);
  assert.ok(listCalls(h).every(c => /view=summary/.test(c.url)));
});

test('opening an Automaton reads its record once and lists tasks from the paged route with Load more', async () => {
  const h = harness(routes({
    tasks: u => (u.includes('cursor=T1')
      ? { body: { prd_id: 'p1', total: 3, tasks: [{ id: 't3', title: 'Third task', status: 'pending' }] } }
      : { body: { prd_id: 'p1', total: 3, next_cursor: 'T1', tasks: [{ id: 't1', title: 'First task', status: 'completed' }, { id: 't2', title: 'Second task', status: 'pending' }] } }),
  }));
  await h.run('loadPRDPanel()');
  await flushAsync(20);
  h.run("prdPanelToggle('p1', { open: true })");
  await flushAsync(40);
  assert.equal(h.calls.filter(c => c.url === '/api/autonomous/prds/p1').length, 1);
  const tcalls = h.calls.filter(c => /\/prds\/p1\/tasks/.test(c.url));
  assert.equal(tcalls.length, 1);
  assert.match(tcalls[0].url, /limit=50/);
  assert.match(rowBody(h, 'p1'), /First task/);
  assert.match(rowBody(h, 'p1'), /prdPanelTasksMore\(&quot;p1&quot;\)/);
  assert.match(rowBody(h, 'p1'), /Load more tasks \(2 of 3\)/);
  h.run("prdPanelTasksMore('p1')");
  await flushAsync(40);
  assert.ok(h.calls.some(c => /\/prds\/p1\/tasks\?.*cursor=T1/.test(c.url)));
  assert.match(rowBody(h, 'p1'), /Third task/);
  assert.doesNotMatch(rowBody(h, 'p1'), /Load more tasks/);
});

test('the new strings exist in every locale', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of ['automaton_tasks_heading', 'automaton_tasks_loading', 'automaton_tasks_failed', 'automaton_tasks_load_more', 'projects_card_refreshing']) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ':' + k);
    }
    assert.match(b.automaton_tasks_load_more, /%1\$s/);
    assert.match(b.automaton_tasks_load_more, /%2\$s/);
  }
});
