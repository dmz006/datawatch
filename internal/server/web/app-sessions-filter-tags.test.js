// Sessions Filters sheet tag section: system-scoped tags never get chips, the
// rest are grouped under collapsible axis headers with counts from the current
// session list, each axis previews six tags, one search narrows the chips, a
// separate picker filters by Automaton title, and the selection shows as a
// removable summary above the list.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`state.activeView = 'other'; _tagsState.list = [{ id: 'u1', name: 'Nightly batch' }]; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);
const count = (html, needle) => html.split(needle).length - 1;

const KINDS = ['code', 'review', 'verify', 'plan', 'docs', 'test', 'ops', 'chat'];

function inventory() {
  const e = [];
  const add = (axis, value, n) => e.push({ tag: axis + ':' + value, axis, value, count: n || 1 });
  for (let i = 0; i < 200; i++) add('automaton', 'a' + i);
  for (let i = 0; i < 60; i++) add('stage', 's' + i);
  for (let i = 0; i < 120; i++) e.push({ tag: 'task:t' + i, axis: '', value: '', count: 1 });
  KINDS.forEach(k => add('kind', k, 5));
  ['worker', 'judge'].forEach(r => add('role', r, 3));
  add('lane', 'gpu', 2);
  add('compute', 'node-a', 4);
  add('project', 'datawatch', 6);
  add('kind', 'ghost', 9); // no session in the list carries it
  e.push({ tag: 'u1', axis: '', value: '', count: 2 });
  return e;
}

function sessions() {
  const out = [];
  for (let i = 0; i < 10; i++) {
    const tags = ['kind:' + KINDS[i % KINDS.length], 'automaton:a' + (i % 3), 'stage:s1', 'task:t' + i];
    if (i < 3) tags.push('role:worker');
    if (i < 2) tags.push('u1');
    if (i < 4) tags.push('compute:node-a');
    out.push({ id: 'x' + i, full_id: 'n/x' + i, state: 'running', tags, prd_id: 'a' + (i % 3) });
  }
  return out;
}

function setup(byId) {
  const sb = loadApp(byId);
  sb.__inv = inventory();
  sb.__sess = sessions();
  run(sb, `state._sessionTagInventory = { loaded: true, entries: __inv }; state.sessions = __sess; _tagBrowse.session.sel = [];`);
  return sb;
}

test('system-scoped helper flags automaton, stage and task tags only', () => {
  const sb = setup();
  const f = e => run(sb, `_sessionTagIsSystemScoped(${JSON.stringify(e)})`);
  assert.equal(f({ tag: 'automaton:a1', axis: 'automaton', value: 'a1' }), true);
  assert.equal(f({ tag: 'stage:s1', axis: 'stage', value: 's1' }), true);
  assert.equal(f({ tag: 'task:t1', axis: '' }), true);
  assert.equal(f({ tag: 'automaton:a1' }), true);
  assert.equal(f({ tag: 'kind:code', axis: 'kind', value: 'code' }), false);
  assert.equal(f({ tag: 'u1', axis: '' }), false);
});

test('hundreds of automaton, stage and task tags render no chips', () => {
  const sb = setup();
  const html = run(sb, '_sessionTagFilterPanelHTML()');
  assert.doesNotMatch(html, /sessionTagChipToggle\('automaton:[^']*'\)"[^>]*>[^<]*<span class="badge-count"/);
  const chips = run(sb, '_sessionTagResultsHTML()');
  assert.equal(count(chips, "sessionTagChipToggle('automaton:"), 0);
  assert.equal(count(chips, "sessionTagChipToggle('stage:"), 0);
  assert.equal(count(chips, "sessionTagChipToggle('task:"), 0);
  assert.ok(count(chips, 'sessions-tag-chip') > 0);
});

test('remaining tags sit under one collapsible header per axis with counts from state.sessions', () => {
  const sb = setup();
  const html = run(sb, '_sessionTagResultsHTML()');
  const heads = html.match(/<button type="button" class="sessions-tag-axis-head" aria-expanded="true"/g) || [];
  assert.equal(heads.length, 4, 'kind, role, compute and user tags; lane and project have no session in the list');
  for (const axis of ['kind', 'role', 'compute', 'user']) assert.match(html, new RegExp('data-axis="' + axis + '"'));
  assert.doesNotMatch(html, /data-axis="lane"/, 'lane has no session in the list');
  assert.doesNotMatch(html, /data-axis="project"/, 'project has no session in the list');
  // counts come from state.sessions, not the inventory (inventory said 3 for worker / 4 for node-a)
  assert.match(html, /sessionTagChipToggle\('role:worker'\)">worker<span class="badge-count">3<\/span>/);
  assert.match(html, /sessionTagChipToggle\('compute:node-a'\)">node-a<span class="badge-count">4<\/span>/);
  assert.match(html, /sessionTagChipToggle\('u1'\)">Nightly batch<span class="badge-count">2<\/span>/);
  // a zero-count tag is hidden unless selected
  assert.doesNotMatch(html, /kind:ghost/);
  run(sb, `_tagBrowse.session.sel = ['kind:ghost'];`);
  assert.match(run(sb, '_sessionTagResultsHTML()'), /sessionTagChipToggle\('kind:ghost'\)"/);
  // collapsing an axis remembers it and drops its chips
  run(sb, `_tagBrowse.session.sel = []; sessionTagAxisToggle('kind');`);
  const collapsed = run(sb, '_sessionTagResultsHTML()');
  assert.match(collapsed, /aria-expanded="false"/);
  assert.equal(count(collapsed, "sessionTagChipToggle('kind:"), 0);
  assert.equal(run(sb, '_sessionTagUI().collapsed.kind'), true);
});

test('more than six tags in an axis show six plus Show all, which reveals the rest', () => {
  const sb = setup();
  const kindChips = html => (html.match(/sessionTagChipToggle\('kind:[^']*'\)/g) || []).length;
  const html = run(sb, '_sessionTagResultsHTML()');
  assert.equal(kindChips(html), 6);
  assert.match(html, /class="sessions-tag-more"[^>]*>Show all \(8\)</);
  run(sb, `sessionTagAxisShowAll('kind')`);
  const all = run(sb, '_sessionTagResultsHTML()');
  assert.equal(kindChips(all), 8);
  assert.match(all, />Show fewer</);
  run(sb, `sessionTagAxisShowAll('kind')`);
  assert.equal(kindChips(run(sb, '_sessionTagResultsHTML()')), 6);
});

test('tag search narrows chips by substring and an unmatched query shows an empty line', () => {
  const sb = setup({ sessionTagResults: { innerHTML: '' }, sessionTagAutoResults: { innerHTML: '' } });
  const res = sb.document.getElementById('sessionTagResults');
  sb.sessionTagSearchInput('REV');
  assert.match(res.innerHTML, /sessionTagChipToggle\('kind:review'\)/);
  assert.equal(count(res.innerHTML, 'sessions-tag-chip '), 1);
  // axis name matches too, and every match shows past the six-tag preview
  sb.sessionTagSearchInput('kind');
  assert.equal((res.innerHTML.match(/sessionTagChipToggle\('kind:/g) || []).length, 8);
  assert.doesNotMatch(res.innerHTML, /sessions-tag-more/);
  // registry name
  sb.sessionTagSearchInput('nightly');
  assert.match(res.innerHTML, /sessionTagChipToggle\('u1'\)/);
  sb.sessionTagSearchInput('zzzz');
  assert.match(res.innerHTML, /class="sessions-tag-empty">No tags match your search</);
  assert.equal(count(res.innerHTML, 'sessions-tag-chip '), 0);
  // the search box itself is outside the rewritten container (focus survives)
  const panel = run(sb, '_sessionTagFilterPanelHTML()');
  assert.ok(panel.indexOf('id="sessionTagSearch"') < panel.indexOf('id="sessionTagResults"'));
  assert.match(panel, /id="sessionTagSearch"[^>]*value="zzzz"[^>]*oninput="sessionTagSearchInput\(this\.value\)"/);
});

test('automaton picker lists titles with ids, filters by title and toggles without chips', () => {
  const sb = setup();
  run(sb, `state._sessionsGroupTitles = { automaton: { a0: 'Ship the parser', a1: 'Refactor the queue', a2: 'Tidy docs' }, project: {}, node: {}, loaded: { automaton: true } };`);
  const panel = run(sb, '_sessionTagFilterPanelHTML()');
  assert.match(panel, /id="sessionTagAutoSearch"/);
  const list = run(sb, '_sessionTagAutomatonResultsHTML()');
  assert.match(list, /<span class="sessions-tag-pick-title">Ship the parser<\/span><span class="sessions-tag-pick-id">a0<\/span>/);
  assert.equal(count(list, 'class="sessions-tag-pick '), 8, 'capped at the picker limit');
  assert.equal(count(list, 'backend-filter-badge'), 0, 'rows are not chips');
  run(sb, `_sessionTagUI().autoQ = 'queue'`);
  const hit = run(sb, '_sessionTagAutomatonResultsHTML()');
  assert.equal(count(hit, 'class="sessions-tag-pick '), 1);
  assert.match(hit, /Refactor the queue/);
  run(sb, `_sessionTagUI().autoQ = 'nothing like it'`);
  assert.match(run(sb, '_sessionTagAutomatonResultsHTML()'), /No Automaton matches/);
  run(sb, `_sessionTagUI().autoQ = ''`);
  sb.sessionTagChipToggle('automaton:a1');
  assert.deepEqual(Array.from(run(sb, '_tagBrowse.session.sel')), ['automaton:a1']);
  assert.equal(count(run(sb, '_sessionTagResultsHTML()'), "sessionTagChipToggle('automaton:"), 0);
  sb.sessionTagChipToggle('automaton:a1');
  assert.deepEqual(Array.from(run(sb, '_tagBrowse.session.sel')), []);
});

test('selected-filter summary shows removable chips and removing one updates the selection', () => {
  const sb = setup();
  run(sb, `state._sessionsGroupTitles = { automaton: { a1: 'Refactor the queue' }, project: {}, node: {}, loaded: { automaton: true } };`);
  assert.equal(run(sb, '_sessionTagSelectedSummaryHTML()'), '');
  run(sb, `_tagBrowse.session.sel = ['automaton:a1', 'kind:code', 'u1'];`);
  const html = run(sb, '_sessionTagSelectedSummaryHTML()');
  assert.equal(count(html, 'class="sessions-tag-sel-chip"'), 3);
  assert.match(html, />Refactor the queue</);
  assert.match(html, />code</);
  assert.match(html, />Nightly batch</);
  assert.match(html, /onclick="sessionTagChipToggle\('kind:code'\)"/);
  assert.match(html, /onclick="tagBrowseClear\('session'\)">Clear all</);
  sb.sessionTagChipToggle('kind:code');
  assert.deepEqual(Array.from(run(sb, '_tagBrowse.session.sel')), ['automaton:a1', 'u1']);
  assert.equal(count(run(sb, '_sessionTagSelectedSummaryHTML()'), 'class="sessions-tag-sel-chip"'), 2);
});
