// Projects page — cache-first: the page paints the last good roll-up from the
// cache at once and marks it refreshing, refetches in the background with
// If-None-Match, repaints only the cards whose data changed, gives every card
// its own error with Retry, renders with a storage that throws, never asks for
// the full-records form, and expands an Automaton through the paged task route.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const EN = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));

const arc = (id, done, total) => ({ automation_id: id, priority: 1, state: 'active', automata: [], tasks: { done, total }, points: 1, throughput: {} });
const ROLL = (a, b) => ({
  project: 'acme', window_days: 7, points_priced: true,
  arcs: [arc('arcA', a, 10), arc('arcB', b, 10)],
  unassigned: { automata: [], tasks: { done: 0, total: 0 }, points: 0, throughput: {} },
  total: { tasks: { done: a + b, total: 20 }, points: 2, throughput: { automata_finished: 1, tasks_finished: 2, automata_per_day: 0.1, tasks_per_day: 0.2 } },
});

// A DOM stand-in that keeps one element per id and counts the writes to each.
function load({ route, store, hash = '#projects/acme', throwingStorage = false } = {}) {
  const els = {};
  const sandbox = buildSandbox({ initialHash: hash });
  sandbox.document.getElementById = id => {
    if (!els[id]) {
      const e = makeStubElement(() => { e.writes = (e.writes || 0) + 1; });
      e.attrs = {};
      e.insertAdjacentHTML = () => {};
      e.setAttribute = (k, v) => { e.attrs[k] = v; };
      els[id] = e;
    }
    return els[id];
  };
  sandbox.AbortController = AbortController;
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
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = EN;
  if (throwingStorage) {
    const boom = () => { throw new Error('storage blocked'); };
    sandbox.localStorage = { getItem: boom, setItem: boom, removeItem: boom, key: boom, get length() { throw new Error('storage blocked'); } };
  } else if (store) {
    Object.keys(store).forEach(k => sandbox.localStorage.setItem(k, JSON.stringify(store[k])));
  }
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects';", sandbox);
  const panel = () => els.automataPanel;
  return { sb: sandbox, calls, els, panel, run: code => vm.runInContext(code, sandbox) };
}

const PROJECTS = [{ name: 'acme' }, { name: 'beta' }];
function standard(over = {}) {
  return (u, h) => {
    if (over.all) { const r = over.all(u, h); if (r !== undefined) return r; }
    if (u.includes('/rollup')) return over.rollup ? over.rollup(u, h) : { body: ROLL(3, 4) };
    if (u.includes('/timeline')) return { body: { rows: [] } };
    if (u.includes('/history')) return { body: { items: [] } };
    if (u.startsWith('/api/projects')) return { body: PROJECTS };
    return { body: {} };
  };
}
const cacheOf = (a, b, tag) => ({
  cs_cf_local_projects: { tag: '', data: PROJECTS, at: Date.now() },
  'cs_cf_local_rollup_acme@7': { tag: tag || '"r1"', data: ROLL(a, b), at: Date.now() },
});

test('a cache hit paints the roll-up before the fetch resolves and marks it refreshing', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const h = load({ store: cacheOf(3, 4), route: standard({ rollup: () => gate.then(() => ({ body: ROLL(5, 4) })) }) });
  const p = h.run('renderProjectsView()');
  // Synchronous first paint, nothing has resolved yet.
  const html = h.panel().innerHTML;
  assert.match(html, /id="pcard_totals"[^>]*data-state="refreshing"/, 'cache-rendered card is marked refreshing');
  assert.match(html, /7 \/ 20|7\/20|7 of 20/, 'cached totals are on the page: ' + html.slice(0, 400));
  assert.doesNotMatch(html, /pc-skeleton/, 'no skeleton when the cache holds the data');
  release();
  await p; await flushAsync(20);
  assert.equal(h.run('_projectsState.rollupStatus'), 'ready');
});

test('with no cache the cards show their own skeletons until the roll-up arrives', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const h = load({ route: standard({ rollup: () => gate.then(() => ({ body: ROLL(1, 1) })) }) });
  const p = h.run('renderProjectsView()');
  await flushAsync(20);
  const html = h.panel().innerHTML;
  assert.match(html, /id="pcard_totals"[^>]*data-state="loading"/);
  assert.match(html, /id="pcard_throughput"[^>]*data-state="loading"/);
  assert.match(html, /pc-skeleton/);
  release();
  await p; await flushAsync(20);
  assert.match(h.panel().innerHTML, /id="pcard_totals"[^>]*data-state="ready"/);
});

test('the background fetch sends If-None-Match and a 304 repaints nothing', async () => {
  const h = load({ store: cacheOf(3, 4, '"r1"'), route: standard({ rollup: (u, hd) => (hd['If-None-Match'] === '"r1"' ? { status: 304, body: '' } : { body: ROLL(9, 9) }) }) });
  await h.run('renderProjectsView()');
  await flushAsync(20);
  const rollCalls = h.calls.filter(c => c.url.includes('/rollup'));
  assert.equal(rollCalls.length, 1);
  assert.equal(rollCalls[0].headers['If-None-Match'], '"r1"');
  assert.equal(h.run('_projectsState.rollupStatus'), 'ready');
  assert.equal(h.run('_projectsState.cardsPainted || 0'), 0, 'no card body was rewritten');
  assert.match(h.panel().innerHTML, /7 \/ 20|7\/20|7 of 20/, 'cached figures stay');
  assert.equal(h.els.pcard_totals.attrs['data-state'], 'ready', 'the refreshing marker is cleared');
});

test('a refresh that changes one arc repaints only that arc card', async () => {
  const h = load({ store: cacheOf(3, 4), hash: '#projects/acme/arcs', route: standard({ rollup: () => ({ body: ROLL(3, 8), headers: { ETag: '"r2"' } }) }) });
  await h.run('renderProjectsView()');
  await flushAsync(20);
  assert.match(h.panel().innerHTML, /id="pcard_arc_arcA"/);
  const a = h.els.pcard_arc_arcA_body, b = h.els.pcard_arc_arcB_body;
  assert.ok(b && /8\s*\/\s*10|8 of 10|8\/10/.test(b.innerHTML), 'changed card carries the new figure: ' + b.innerHTML.slice(0, 300));
  assert.equal(b.writes, 1, 'the changed arc was rewritten once');
  assert.ok(!a || !a.writes, 'the unchanged arc was not rewritten');
  assert.equal(h.els.pcard_arc_arcA.attrs['data-state'], 'ready');
  // The new tag is held for the next conditional request.
  const held = JSON.parse(h.sb.localStorage.getItem('cs_cf_local_rollup_acme@7'));
  assert.equal(held.tag, '"r2"');
});

test('a failing roll-up shows its own error with Retry while the Gantt still renders', async () => {
  let fail = true;
  const h = load({
    route: standard({
      rollup: () => (fail ? { status: 500, body: 'roll-up exploded' } : { body: ROLL(2, 2) }),
      all: u => (u.includes('/timeline') ? { body: { rows: [{ kind: 'arc', id: 'a1', title: 'Arc a1', state: 'active', first: new Date(Date.now() - 864e5).toISOString(), last: null, running: true, has_children: false, children_count: 0 }] } } : undefined),
    }),
  });
  await h.run('renderProjectsView()');
  await flushAsync(30);
  const html = h.panel().innerHTML;
  assert.match(html, /id="pcard_totals"[^>]*data-state="error"/);
  assert.match(html, /roll-up exploded/);
  assert.match(html, /pc-retry/);
  assert.match(html, /projectsCardRetry\(&quot;totals&quot;\)/);
  assert.equal(h.run('_projectsState.gantt.rows.length'), 1, 'the Gantt fetch was not blocked by the roll-up failure');
  fail = false;
  h.run("projectsCardRetry('totals')");
  await flushAsync(30);
  assert.match(h.panel().innerHTML, /id="pcard_totals"[^>]*data-state="ready"/);
});

test('a failing Gantt shows its own error with Retry and the roll-up cards render', async () => {
  const h = load({ route: standard({ all: u => (u.includes('/timeline') ? { status: 500, body: 'gantt down' } : undefined) }), hash: '#projects/acme/gantt' });
  await h.run('renderProjectsView()');
  await flushAsync(30);
  assert.equal(h.run('_projectsState.rollupStatus'), 'ready');
  const g = h.run('_projectGanttInnerHTML(_projectsState.gantt)');
  assert.match(g, /data-card="gantt" data-state="error"/);
  assert.match(g, /gantt down/);
  assert.match(g, /projectsCardRetry\(&quot;gantt&quot;\)/);
});

test('a storage that throws still renders the page', async () => {
  const h = load({ throwingStorage: true, route: standard() });
  await h.run('renderProjectsView()');
  await flushAsync(30);
  assert.match(h.panel().innerHTML, /id="pcard_totals"[^>]*data-state="ready"/);
  assert.match(h.panel().innerHTML, /7 \/ 20|7\/20|7 of 20/);
  // And the pure cache helpers are safe to call.
  assert.equal(h.run("_cfGet('x')"), null);
  h.run("_cfPut('x', 't', {a: 1}); _cfDrop();");
});

test('no request of the page carries full=true', async () => {
  const h = load({ store: cacheOf(3, 4), route: standard() });
  await h.run('renderProjectsView()');
  await flushAsync(30);
  h.run("selectProjectsView('beta')");
  await flushAsync(30);
  assert.ok(h.calls.length > 3);
  assert.deepEqual(h.calls.filter(c => /full=true/.test(c.url)), []);
});

test('Gantt expand fetches the Automaton task page route, never the full record', async () => {
  const day = 864e5;
  const iso = d => new Date(Date.now() - d * day).toISOString();
  const route = standard({
    all: u => {
      if (u.includes('level=arcs')) return { body: { rows: [{ kind: 'arc', id: 'a1', title: 'A1', state: 'active', first: iso(9), last: iso(1), running: false, has_children: true, children_count: 1 }] } };
      if (u.includes('level=automata&parent=a1')) return { body: { rows: [{ kind: 'automaton', id: 'm1', title: 'M1', state: 'completed', first: iso(9), last: iso(2), running: false, has_children: true, children_count: 2 }] } };
      if (/\/api\/autonomous\/prds\/m1\/tasks/.test(u)) return { body: { prd_id: 'm1', total: 2, next_cursor: 'c1', tasks: [{ id: 't1', title: 'T1', status: 'completed', started_at: iso(8), completed_at: iso(5) }] } };
      return undefined;
    },
  });
  const h = load({ route, hash: '#projects/acme/gantt' });
  await h.run('renderProjectsView()');
  await flushAsync(30);
  await h.run("projectsGanttToggle('arc:a1')");
  await flushAsync(10);
  const keyOf = h.run("_projectsGanttKey(_projectsState.gantt.kids['arc:a1'] ? _projectsState.gantt.kids['arc:a1'].rows[0] : null)");
  await h.run(`projectsGanttToggle(${JSON.stringify(keyOf)})`);
  await flushAsync(10);
  const taskCalls = h.calls.filter(c => /\/api\/autonomous\/prds\/m1\/tasks/.test(c.url));
  assert.equal(taskCalls.length, 1, h.calls.map(c => c.url).join('\n'));
  assert.match(taskCalls[0].url, /[?&]limit=\d+/);
  assert.ok(!h.calls.some(c => /\/api\/autonomous\/prds\/m1(\?|$)/.test(c.url)), 'the full record is not read');
  // Load more sends the cursor.
  await h.run(`projectsGanttMore(${JSON.stringify(keyOf)})`);
  await flushAsync(10);
  assert.ok(h.calls.some(c => /\/prds\/m1\/tasks\?.*cursor=c1/.test(c.url)));
});

test('a project change drops the left project cache; an auth reset drops everything', async () => {
  const h = load({ store: Object.assign(cacheOf(3, 4), { 'cs_cf_local_rollup_beta@7': { tag: '"b"', data: ROLL(1, 1), at: Date.now() } }), route: standard() });
  await h.run('renderProjectsView()');
  await flushAsync(30);
  assert.ok(h.sb.localStorage.getItem('cs_cf_local_rollup_acme@7'));
  await h.run("selectProjectsView('beta')");
  await flushAsync(30);
  assert.equal(h.sb.localStorage.getItem('cs_cf_local_rollup_acme@7'), null, 'acme cache dropped on the project change');
  assert.ok(h.sb.localStorage.getItem('cs_cf_local_rollup_beta@7'), 'the selected project keeps its cache');
  h.run('_cfDrop()');
  assert.equal(h.sb.localStorage.getItem('cs_cf_local_rollup_beta@7'), null);
  assert.equal(h.sb.localStorage.getItem('cs_cf_local_projects'), null);
});

test('the new strings exist in every locale', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of ['projects_card_failed', 'projects_card_refreshing', 'projects_card_plan_loading', 'projects_card_plan_failed']) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ':' + k);
    }
  }
});
