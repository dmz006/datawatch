// Projects pages follow the header scope.
//
// Loads the real app.js into a sandbox (no daemon, no tmux) and drives the
// project page with a project scope, a plan scope and the scope All: the banner
// on every sub-tab body, the plan's own roll-up group, plan-browser entry,
// records, timeline rows, history rows, events and the unassigned empty state,
// the client-side paging of a filtered list, and the scope token that drops a
// late reply.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');
const { installFakeDom } = require('./testutil_fake_dom');

const WEB = __dirname;
const run = (sb, code) => vm.runInContext(code, sb);

function json(body) {
  return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
}

const PROJECTS = [{ name: 'acme', dir: '/work/acme', arcs: [{ automation_id: 'au-1' }, { automation_id: 'au-2' }] }];
const ROLL = {
  window_days: 7, points_priced: true,
  total: { tasks: { done: 6, total: 10 }, points: 20, throughput: {} },
  arcs: [
    { automation_id: 'au-1', priority: 1, state: 'active', tasks: { done: 1, total: 4 }, points: 3, automata: [{ id: 'p-1', title: 'First', status: 'completed', points: 1 }, { id: 'p-2', title: 'Second', status: 'running', points: 2 }] },
    { automation_id: 'au-2', priority: 2, state: 'planned', tasks: { done: 5, total: 6 }, points: 17, automata: [{ id: 'p-3', title: 'Third', status: 'completed' }] },
  ],
  unassigned: { tasks: { done: 0, total: 0 }, points: 0, automata: [{ id: 'p-9', title: 'Loose', status: 'planned' }] },
};

function load({ fetches = {} } = {}) {
  const sandbox = buildSandbox({});
  const dom = installFakeDom(sandbox, { statics: ['headerScopeBtn', 'headerScopeBadge', 'view', 'automataPanel'] });
  ['view', 'automataPanel'].forEach(id => { dom.el(id).insertAdjacentHTML = () => {}; });
  const calls = [];
  sandbox.fetch = (url) => {
    const u = String(url);
    calls.push(u);
    for (const k of Object.keys(fetches)) {
      if (u.includes(k)) { const r = fetches[k]; return typeof r === 'function' ? r(u) : json(r); }
    }
    return json([]);
  };
  sandbox.history = { pushState() {}, replaceState() {} };
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  run(sandbox, `state.activeView = 'autonomous'; _automataState.tab = 'projects';
    _projectsState.list = ${JSON.stringify(PROJECTS)}; _projectsState.error = '';`);
  sandbox.renderAutonomousView = () => {};
  sandbox.__calls = calls;
  return sandbox;
}

// page(sb, subtab) draws the project page with the sub-tab and returns the body.
function page(sb, subtab, extra) {
  run(sb, `_projectsState.selected = 'acme'; _projectsState.subtab = ${JSON.stringify(subtab)};
    _projectsState.rollup = ${JSON.stringify(ROLL)}; _projectsState.rollupFor = 'acme'; _projectsState.rollupError = '';
    ${extra || ''}`);
  sb.__out = run(sb, `_projectPageHTML(_projectsState.list[0], _projectsState.rollup, '', _projectsState.hist)`);
  const m = /<div id="projectsSubtabBody"[^>]*>([\s\S]*)<\/div><\/div><\/div>$/.exec(sb.__out);
  assert.ok(m, 'sub-tab body present');
  return m[1];
}
function planReady(sb, ids) {
  run(sb, `_scopePlanCache = { key: 'acme\\nau-1', ids: new Set(${JSON.stringify(ids)}), status: 'ready' };`);
}
const SUBTABS = ['overview', 'plans', 'records', 'gantt', 'unassigned', 'history'];

test('scope All: no banner on any sub-tab and the unfiltered pages', () => {
  const sb = load();
  run(sb, `setScope('', '', { silent: true, noHash: true });`);
  SUBTABS.forEach(k => assert.ok(!/scope-banner/.test(page(sb, k)), k));
  const plans = page(sb, 'plans');
  assert.match(plans, /data-automation="au-1"/);
  assert.match(plans, /data-automation="au-2"/);
  assert.match(page(sb, 'unassigned'), /Loose/);
});

test('project scope: every sub-tab body starts with the banner naming the project', () => {
  const sb = load();
  run(sb, `setScope('acme', '', { silent: true, noHash: true });`);
  SUBTABS.forEach(k => {
    const body = page(sb, k);
    assert.ok(body.startsWith('<div class="scope-banner"'), k + ' starts with the banner');
    assert.match(body.slice(0, 400), /Filtered to acme/);
  });
  // a project scope alone keeps every plan
  assert.match(page(sb, 'plans'), /data-automation="au-2"/);
});

test('plan scope: the banner names the plan and the Plans tab holds only its roll-up group', () => {
  const sb = load();
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });`);
  planReady(sb, ['p-1', 'p-2']);
  const body = page(sb, 'plans');
  assert.match(body, /Filtered to acme \/ /);
  assert.match(body, /data-automation="au-1"/);
  assert.ok(!/data-automation="au-2"/.test(body), 'the other plan is gone');
  // the roll-up block counts the plan's tasks only: 1/4, not 6/10
  assert.match(body, /1\/4 /);
  assert.ok(!/6\/10/.test(body));
});

test('plan scope: the Plans tab keeps only the plan-browser entry that lists the plan', () => {
  const sb = load();
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });
    _plansBrowser = _plansBrowserFresh('acme');
    _plansBrowser.status = 'ready'; _plansBrowser.total = 2;
    _plansBrowser.plans = [
      { id: 'pl-1', title: 'Plan one', state: 'active', automation_ids: ['au-1'], progress: {} },
      { id: 'pl-2', title: 'Plan two', state: 'planned', automation_ids: ['au-2'], progress: {} }];`);
  planReady(sb, []);
  const list = run(sb, `plansBrowserListHTML(_plansBrowser)`);
  assert.match(list, /data-plan="pl-1"/);
  assert.ok(!/data-plan="pl-2"/.test(list));
  run(sb, `setScope('acme', '', { silent: true, noHash: true });`);
  const all = run(sb, `plansBrowserListHTML(_plansBrowser)`);
  assert.match(all, /data-plan="pl-2"/);
});

test('plan scope: the Overview numbers come from the plan roll-up group', () => {
  const sb = load();
  const summary = { header: {}, progress: { automata: { done: 9, total: 9, percent: 100 }, points: 99 }, active_plans: [{ id: 'x', title: 'Other', state: 'active', progress: {} }], next: null, counts: { plans: { total: 7 }, records: { total: 5 }, rows: { total: 3 }, findings: { total: 2 } } };
  run(sb, `_projectsSummary = { name: 'acme', data: ${JSON.stringify(summary)}, error: '' }; setScope('acme', 'au-1', { silent: true, noHash: true });`);
  planReady(sb, ['p-1', 'p-2']);
  const body = page(sb, 'overview');
  assert.match(body, /1\/2 Automata/); // one of the two Automata of au-1 finished
  assert.match(body, /3 points/);       // the group's points, not 99
  assert.ok(!/99/.test(body));
  assert.ok(!/Other/.test(body));
  assert.match(body, /Second/);         // the next unfinished Automaton of the plan
  run(sb, `setScope('acme', '', { silent: true, noHash: true });`);
  const proj = page(sb, 'overview');
  assert.match(proj, /9\/9 Automata/);
  assert.match(proj, /99 points/);
});

test('plan scope: Unassigned says unassigned Automata are not part of a plan', () => {
  const sb = load();
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });`);
  planReady(sb, ['p-1']);
  const body = page(sb, 'unassigned');
  assert.match(body, /scope-empty-unassigned/);
  assert.match(body, /Unassigned Automata are not part of a plan/);
  assert.ok(!/Loose/.test(body));
  assert.ok(body.startsWith('<div class="scope-banner"'));
});

test('plan scope: Records keep rows under the plan record, the Automation or its Automata', () => {
  const sb = load();
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });
    _plansBrowser = _plansBrowserFresh('acme'); _plansBrowser.status = 'ready';
    _plansBrowser.plans = [{ id: 'pl-1', automation_ids: ['au-1'] }, { id: 'pl-2', automation_ids: ['au-2'] }];
    _recordsTab = _recordsTabFresh('acme'); _recordsTab.status = 'ready';
    _recordsTab.rows = [
      { id: 'r1', title: 'ByPlan', kind: 'registry_row', plan_id: 'pl-1' },
      { id: 'r2', title: 'ByAutomaton', kind: 'finding', plan_id: 'pl-9', automaton_ids: ['p-2'] },
      { id: 'r3', title: 'Elsewhere', kind: 'registry_row', plan_id: 'pl-2' },
      { id: 'r4', title: 'NoLink', kind: 'finding' }];
    _recordsTab.total = 4;`);
  planReady(sb, ['p-1', 'p-2']);
  const list = run(sb, `recordsTabListHTML(_recordsTab)`);
  assert.match(list, /ByPlan/);
  assert.match(list, /ByAutomaton/);
  assert.ok(!/Elsewhere/.test(list));
  assert.ok(!/NoLink/.test(list));
  const body = page(sb, 'records');
  assert.ok(body.startsWith('<div class="scope-banner"'));
  run(sb, `setScope('acme', '', { silent: true, noHash: true });`);
  assert.match(run(sb, `recordsTabListHTML(_recordsTab)`), /Elsewhere/);
});

test('plan scope: History keeps the plan Automata and drops snapshots', () => {
  const sb = load();
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });
    _projectsState.hist = { name: 'acme', next: '', error: '', items: [
      { kind: 'automaton', id: 'p-1', title: 'Mine', status: 'completed', tasks: { done: 1, total: 1 } },
      { kind: 'automaton', id: 'p-3', title: 'Theirs', status: 'completed', tasks: { done: 1, total: 1 } },
      { kind: 'snapshot', taken_at: '2026-11-01T00:00:00Z', progress: { tasks: 4, tasks_done: 1 } }] };`);
  planReady(sb, ['p-1', 'p-2']);
  const body = page(sb, 'history');
  assert.ok(body.startsWith('<div class="scope-banner"'));
  assert.match(body, /Mine/);
  assert.ok(!/Theirs/.test(body));
  assert.ok(!/data-kind="snapshot"/.test(body));
  run(sb, `setScope('acme', '', { silent: true, noHash: true });`);
  const all = page(sb, 'history');
  assert.match(all, /Theirs/);
  assert.match(all, /data-kind="snapshot"/);
});

test('plan scope: History keeps reading pages while the plan shows too little, with a cap', async () => {
  let pages = 0;
  const sb = load({ fetches: { '/history': (u) => {
    pages++;
    const n = pages;
    return json({ items: [{ kind: 'automaton', id: n === 3 ? 'p-1' : 'x-' + n, title: 'row' + n, status: 'completed', tasks: {} }], next_cursor: 'c' + n });
  } } });
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true }); _projectsState.selected = 'acme';`);
  planReady(sb, ['p-1']);
  await run(sb, `_projectsLoadHistory(false)`);
  await flushAsync();
  assert.ok(pages >= 3, 'read until the plan row appeared, got ' + pages);
  assert.ok(pages <= 11, 'capped, got ' + pages);
  // an endless filtered-out list stops at the cap
  pages = 0;
  const sb2 = load({ fetches: { '/history': () => { pages++; return json({ items: [{ kind: 'automaton', id: 'z-' + pages, title: 't', status: 'completed', tasks: {} }], next_cursor: 'c' }); } } });
  run(sb2, `setScope('acme', 'au-1', { silent: true, noHash: true }); _projectsState.selected = 'acme';`);
  planReady(sb2, ['p-1']);
  await run(sb2, `_projectsLoadHistory(false)`);
  await flushAsync();
  assert.ok(pages <= 11, 'capped, got ' + pages);
  assert.ok(pages > 1);
});

test('plan scope: the timeline arcs level keeps the plan row and pages until it is found', async () => {
  let n = 0;
  const sb = load({ fetches: { '/timeline': (u) => {
    n++;
    if (u.includes('level=arcs') && !u.includes('cursor=')) return json({ rows: [{ id: 'au-2', kind: 'arc', title: 'Two', has_children: true }, { kind: 'unassigned', id: 'none' }], next_cursor: 'k1' });
    if (u.includes('level=arcs')) return json({ rows: [{ id: 'au-1', kind: 'arc', title: 'One', has_children: true }], next_cursor: 'k2' });
    return json({ rows: [] });
  } } });
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true }); _projectsState.selected = 'acme';`);
  planReady(sb, []);
  await run(sb, `_projectsLoadGantt()`);
  await flushAsync();
  const g = run(sb, `_projectsState.gantt`);
  assert.deepEqual(Array.from(g.rows).map(r => r.id), ['au-1']);
  assert.equal(g.next, '', 'one plan row: nothing more to find');
  assert.equal(n, 2);
  const body = page(sb, 'gantt');
  assert.ok(body.startsWith('<div class="scope-banner"'));
  assert.ok(!/>Two</.test(body));
});

test('a timeline reply for an older scope is dropped', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const sb = load({ fetches: { '/timeline': () => gate.then(() => json({ rows: [{ id: 'au-2', kind: 'arc', title: 'Two' }], next_cursor: '' })) } });
  run(sb, `setScope('acme', 'au-2', { silent: true, noHash: true }); _projectsState.selected = 'acme';`);
  planReady(sb, []);
  const p = run(sb, `_projectsLoadGantt()`);
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });`);
  release();
  await p;
  await flushAsync();
  const g = run(sb, `_projectsState.gantt`);
  assert.equal(g.rows.length, 0, 'the late reply is not applied');
});

test('plan scope: the project events keep rows of the plan Automation', () => {
  const sb = load();
  const ev = { events: [
    { id: 'e1', kind: 'task_done', automation_id: 'au-1' },
    { id: 'e2', kind: 'task_done', automation_id: 'au-2' },
    { id: 'e3', kind: 'note' }] };
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });`);
  sb.__ev = ev;
  const scoped = run(sb, `_projectEventStreamHTML(__ev, '')`);
  assert.match(scoped, /data-automation="au-1"/);
  assert.ok(!/data-automation="au-2"/.test(scoped));
  assert.ok(!/data-kind="note"/.test(scoped));
  run(sb, `setScope('acme', '', { silent: true, noHash: true });`);
  const all = run(sb, `_projectEventStreamHTML(__ev, '')`);
  assert.match(all, /data-automation="au-2"/);
  assert.match(all, /data-kind="note"/);
});

test('the project events request still names the project and a late reply for an older scope is dropped', async () => {
  let release;
  const gate = new Promise(r => { release = r; });
  const sb = load({ fetches: { '/api/autonomous/events': () => gate.then(() => json({ events: [{ id: 'e', automation_id: 'au-1' }] })) } });
  run(sb, `setScope('acme', 'au-2', { silent: true, noHash: true }); _projectsState.selected = 'acme';`);
  const p = run(sb, `_projectsLoadLinks()`);
  assert.ok(sb.__calls.some(u => /\/api\/autonomous\/events\?.*project=acme/.test(u)));
  run(sb, `setScope('acme', 'au-1', { silent: true, noHash: true });`);
  release();
  await p;
  await flushAsync();
  assert.equal(run(sb, `_projectsLinks.events`), null);
});

test('paint: a scoped project is the page, with the picker following the scope', () => {
  const sb = load();
  run(sb, `setScope('acme', '', { silent: true, noHash: true }); _projectsState.selected = '${'__overview__'}';`);
  sb.__dom = null;
  run(sb, `_projectsPaint()`);
  const html = sb.document.getElementById('automataPanel').innerHTML;
  assert.match(html, /project-detail-card/);
  assert.match(html, /data-project="acme"/);
});

test('a remembered scope whose plan is gone falls back to All and the banner goes', () => {
  const sb = load();
  run(sb, `setScope('acme', 'au-gone', { silent: true, noHash: true });`);
  assert.match(run(sb, `_scopeBannerHTML()`), /scope-banner/);
  run(sb, `_scopeValidate()`);
  assert.equal(run(sb, `getScope().plan`), '');
  assert.equal(run(sb, `getScope().project`), 'acme');
  run(sb, `_projectsState.list = []; _scopeValidate()`);
  assert.equal(run(sb, `_scopeBannerHTML()`), '');
});

test('the new empty-state string is in all five locale bundles and the style exists', () => {
  ['en', 'de', 'es', 'fr', 'ja'].forEach(l => {
    const j = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', l + '.json'), 'utf8'));
    assert.ok(j.projects_scope_unassigned_not_in_plan && !/\b(BL|GH|B)\d+\b/.test(j.projects_scope_unassigned_not_in_plan), l);
  });
  assert.match(fs.readFileSync(path.join(WEB, 'style.css'), 'utf8'), /\.scope-empty\s*\{/);
});
