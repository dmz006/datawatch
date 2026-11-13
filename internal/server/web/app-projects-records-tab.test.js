// Records sub-tab — browses every registry row and finding of a project.
//
// The tab reads GET /api/plan-records/browse (summaries, paged with Load more)
// with a search box, a kind filter and a state filter. Each row shows its
// plan and Automata as links; a link opens the Plans sub-tab with that plan
// expanded. The Registry and findings card sits below the browser. While the
// feature is off the browser says so. Every value is escaped.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-projects-records-tab.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ESCAPED = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) { return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8')); }

const ALL = [
  { title: 'Passkey registry row', id: 'r1', uid: 'x1', kind: 'registry_row', project: 'acme', state: 'shipped', legacy_number: 412, plan_id: 'login-flow', plan_title: 'Ship the login flow', automaton_ids: ['a1', 'a2'] },
  { title: 'Search misses aliases', id: 'f1', uid: 'x2', kind: 'finding', project: 'acme', state: 'open', plan_id: 'login-flow', plan_title: 'Ship the login flow', automaton_ids: [] },
  { title: 'Loose finding', id: 'f2', uid: 'x3', kind: 'finding', project: 'acme', state: 'triaged', automaton_ids: ['z9'] },
];

function load(opts = {}) {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const list = { innerHTML: '' };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view, recordsTabList: list } });
  sandbox.__calls = [];
  sandbox.fetch = (url, o) => {
    const u = String(url);
    const ok = b => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) });
    if (u.startsWith('/api/plan-records/browse')) {
      sandbox.__calls.push(u);
      if (opts.fail) return Promise.resolve({ ok: false, status: opts.fail, statusText: 'x', text: () => Promise.resolve(opts.failText || 'broken'), json: () => Promise.resolve({}) });
      const g = k => decodeURIComponent((u.match(new RegExp('[?&]' + k + '=([^&]*)')) || [])[1] || '');
      const q = g('q').toLowerCase(), kind = g('kind'), state = g('state');
      const off = Number(g('offset') || 0), lim = Number(g('limit') || 50);
      const rows = (opts.rows || ALL).filter(r => (!q || r.title.toLowerCase().includes(q)) && (!kind || r.kind === kind) && (!state || r.state === state));
      return ok({ records: rows.slice(off, off + lim), page: { total: rows.length } });
    }
    if (u.startsWith('/api/plans') || u.includes('/plans?')) {
      sandbox.__calls.push(u);
      if (u.includes('/plans?')) return ok({ plans: [{ title: 'Ship the login flow', id: 'login-flow', state: 'in_progress', automaton_ids: ['a1'] }], page: { total: 1 }, backlog: { plans: [], page: { total: 0 } } });
      return ok({ automata: [], page: { total: 0 } });
    }
    if (u.startsWith('/api/plan-records')) return ok({ records: [], total: 0 });
    if (u.includes('/api/projects') && !u.includes('/rollup') && !u.includes('/history')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    return ok({});
  };
  sandbox.__timers = [];
  sandbox.setTimeout = fn => { sandbox.__timers.push(fn); return sandbox.__timers.length; };
  sandbox.clearTimeout = id => { if (id) sandbox.__timers[id - 1] = null; };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, list };
}

const pr = (sb, e) => vm.runInContext(e, sb);
const browseCalls = sb => sb.__calls.filter(u => u.startsWith('/api/plan-records/browse'));
async function open(sb) {
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  await sb._recordsTabLoad(false);
  await flushAsync();
}
const html = sb => sb.recordsTabListHTML(pr(sb, '_recordsTab'));

test('the tab lists every row and finding from the browse route, title first', async () => {
  const { sb } = load();
  await open(sb);
  const c = browseCalls(sb)[0];
  assert.ok(c.includes('project=acme') && c.includes('limit=50') && c.includes('offset=0'));
  const h = html(sb);
  assert.ok(h.includes('Passkey registry row') && h.includes('Search misses aliases') && h.includes('Loose finding'));
  assert.ok(h.includes('row 412'));
  assert.ok(h.includes('3 records; 3 shown.'));
  assert.ok(h.indexOf('Passkey registry row') < h.indexOf('Search misses aliases'));
});

test('plan and Automaton links jump to the Plans sub-tab with the plan expanded', async () => {
  const { sb } = load();
  await open(sb);
  const h = html(sb);
  assert.equal((h.match(/records-tab-plan-link/g) || []).length, 2);
  assert.equal((h.match(/records-tab-automaton-link/g) || []).length, 2, 'a1 and a2 link to the plan');
  assert.ok(h.includes('recordsTabGoPlan'));
  assert.ok(h.includes('z9') && !h.includes('data-plan="z9"'), 'an Automaton with no plan is plain text');
  await sb._plansBrowserLoadSelected();
  await sb.recordsTabGoPlan('login-flow');
  await flushAsync();
  assert.equal(pr(sb, '_projectsState.subtab'), 'plans');
  assert.equal(pr(sb, "_plansIsOpen('acme', 'login-flow')"), true);
  assert.ok(sb.__calls.some(u => u.startsWith('/api/plans/login-flow/automata')), 'its Automata are read');
});

test('the kind and state filters go to the server and reset to the first page', async () => {
  const { sb } = load();
  await open(sb);
  await sb.recordsTabSetKind('finding');
  await flushAsync();
  assert.ok(browseCalls(sb).pop().includes('kind=finding'));
  assert.ok(!html(sb).includes('Passkey registry row'));
  await sb.recordsTabSetState('triaged');
  await flushAsync();
  const last = browseCalls(sb).pop();
  assert.ok(last.includes('kind=finding') && last.includes('state=triaged') && last.includes('offset=0'));
  assert.ok(html(sb).includes('Loose finding') && !html(sb).includes('Search misses aliases'));
});

test('search waits for a pause in typing and uses the one window timer', async () => {
  const { sb } = load();
  await open(sb);
  const before = browseCalls(sb).length;
  const timers = sb.__timers.filter(Boolean).length;
  sb.recordsTabSearchInput('pass');
  sb.recordsTabSearchInput('passkey');
  assert.equal(browseCalls(sb).length, before, 'nothing sent while typing');
  assert.equal(sb.__timers.filter(Boolean).length, timers + 1, 'the first timer was cleared');
  sb.__timers.splice(0).filter(Boolean).forEach(fn => fn());
  await flushAsync();
  const last = browseCalls(sb).pop();
  assert.ok(last.includes('q=passkey'));
  assert.equal(pr(sb, '_recordsTab.rows.length'), 1);
  assert.equal(sb._recordsTabSearchTimer, null);
});

test('Load more appends the next page', async () => {
  const rows = Array.from({ length: 60 }, (_, i) => ({ title: 'Row ' + (i + 1), id: 'r' + i, kind: 'registry_row', state: 'open', automaton_ids: [] }));
  const { sb } = load({ rows });
  await open(sb);
  assert.equal(pr(sb, '_recordsTab.rows.length'), 50);
  assert.ok(html(sb).includes('records-tab-more'));
  await sb.recordsTabMore();
  await flushAsync();
  assert.ok(browseCalls(sb).pop().includes('offset=50'));
  assert.equal(pr(sb, '_recordsTab.rows.length'), 60);
  assert.ok(!html(sb).includes('records-tab-more'));
});

test('an empty result, a failure and a feature that is off each say so', async () => {
  let t = load({ rows: [] });
  await open(t.sb);
  assert.ok(html(t.sb).includes('No registry row or finding matches.'));
  t = load({ fail: 500, failText: 'boom' });
  await open(t.sb);
  assert.ok(html(t.sb).includes('Could not load records'));
  t = load({ fail: 404, failText: 'plan records are turned off' });
  await open(t.sb);
  assert.ok(html(t.sb).includes('Records are turned off for this server.'));
});

test('record values are escaped', async () => {
  const { sb } = load({ rows: [{ title: HOSTILE, id: 'h"1', kind: 'finding', state: 'open', plan_id: HOSTILE, plan_title: HOSTILE, automaton_ids: [HOSTILE] }] });
  await open(sb);
  const h = html(sb);
  assert.ok(!h.includes(HOSTILE));
  assert.ok(h.includes(ESCAPED));
});

test('the Records sub-tab draws the browser above the Registry and findings card', async () => {
  const { sb } = load();
  await open(sb);
  vm.runInContext("_projectsState.subtab = 'records'", sb);
  const page = sb._projectPageHTML({ name: 'acme' }, {}, '', null);
  assert.ok(page.includes('class="records-tab"'));
  assert.ok(page.includes('id="recordsTabList"'));
  assert.ok(page.indexOf('class="records-tab"') < page.indexOf('id="planRecordsSlot"'));
  vm.runInContext("_projectsState.subtab = 'plans'", sb);
  assert.ok(!sb._projectPageHTML({ name: 'acme' }, {}, '', null).includes('class="records-tab"'));
});

test('selecting the project loads the browser', async () => {
  const { sb } = load();
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  await sb._projectsLoadSelected();
  await flushAsync();
  assert.ok(browseCalls(sb).some(u => u.includes('limit=50')), 'the Records browser read');
});

test('every new locale key exists in all five files', () => {
  const keys = ['records_tab_search_label', 'records_tab_search_placeholder', 'records_tab_filter_kind', 'records_tab_kind_all', 'records_tab_loading',
    'records_tab_off', 'records_tab_failed', 'records_tab_empty', 'records_tab_status', 'records_tab_plan', 'records_tab_automata'];
  const src = fs.readFileSync(APP_JS, 'utf8');
  for (const k of keys) assert.ok(src.includes("'" + k + "'"), k + ' is used by app.js');
  for (const l of LANGS) {
    const b = bundle(l);
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].length > 0, `${l} has ${k}`);
    assert.ok(b.records_tab_failed.includes('%1$s') && b.records_tab_status.includes('%2$s'), l + ' keeps its placeholders');
  }
});
