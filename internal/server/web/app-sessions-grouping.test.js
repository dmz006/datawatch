'use strict';
// Session grouping + Automaton → story nesting + saved views (sessions page).
//
// There is no JS test framework in this PWA (no package.json, no bundler);
// following the established pattern in this directory (app-sessions-tiles.
// test.js, testutil_browser_stub.js) this is a plain Node script that loads
// the real, unmodified app.js into a Node vm stub browser and asserts on the
// grouping/nesting/saved-view behavior:
//
//   1. Grouping by state / node / project / tag / tag prefix — every session
//      lands in exactly one group (sorted first-match placement, same rule as
//      the server), unmatched sessions form the ungrouped bucket.
//   2. Automaton mode nests sub-task sessions (prd_id/story_id/task_id) under
//      their Automaton and then their story; Automaton headers show progress
//      (count + state mix).
//   3. Groups render in the active view mode (list / tiles / compact) with
//      per-group Show-more paging.
//   4. Collapsed groups persist on the device (localStorage) and survive a
//      re-render; storage failures degrade gracefully.
//   5. Saved views: save / apply / rename / delete round-trip, device-local,
//      capturing mode + group axis + session filter.
//   6. Tag filter (state.sessionFilter) narrows the pool before grouping is
//      applied.
//
// Run: node --test internal/server/web/app-sessions-grouping.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, vm, makeStubElement }
  = require('./testutil_browser_stub.js');

const WEB_DIR = __dirname;
const APP_JS = path.join(WEB_DIR, 'app.js');
const NOW = Date.parse('2026-09-02T12:00:00Z');

// ── fixture ──────────────────────────────────────────────────────────────
// 300 sessions: 5 states, 3 nodes, 2 projects, 5 tags (each session carries
// two adjacent tags), 20 Automaton sub-task sessions split over two
// Automata (prd-A, prd-B) with story nesting and a story-less root bucket.
function makeSessions() {
  const STATES = ['running', 'waiting_input', 'idle', 'error', 'stopped'];
  const HOSTS = ['node-a', 'node-b', 'node-c'];
  const DIRS = ['/p/alpha', '/p/beta'];
  const TAGS = ['ops:cron', 'ops:watch', 'data:etl', 'data:backfill', 'infra:ci'];
  const out = [];
  for (let i = 0; i < 300; i++) {
    const s = {
      id: 'sess-' + i,
      full_id: 'node-a/full-sess-' + i,
      name: 'work-' + i,
      state: STATES[i % 5],
      hostname: HOSTS[i % 3],
      project_dir: DIRS[i % 2],
      task: 'job-' + (i % 7) + '-etl',
      tags: [TAGS[i % 5], TAGS[(i + 1) % 5]],
      inherited_tags: (i % 13 === 0) ? ['zzz:inherit'] : [],
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-01T00:00:00Z',
    };
    if (i < 20) {
      s.prd_id = (i < 14) ? 'prd-A' : 'prd-B';
      s.story_id = (i % 3 === 2) ? '' : 'story-' + (i % 3);
      if (s.story_id === '') delete s.story_id; // some sub-tasks are top-level
      s.task_id = 'task-' + (i % 6);
    }
    out.push(s);
  }
  return out;
}

// ── sandbox ───────────────────────────────────────────────────────────────
function freshSandbox() {
  const byId = {
    'view-sessions': makeStubElement(),
    'sessionFilterInput': makeStubElement(),
  };
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // t() reads window._i18n.bundle and window._i18n.fallback; the async loader
  // can't run under the stub fetch, so install the English bundle in both.
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return sandbox;
}

// plain round-trips a value through the host realm so node:assert
// deep-equality (prototype-sensitive across realms) works on vm-created
// structures. Same pattern as app-sessions-tiles.test.js.
const plain = v => JSON.parse(JSON.stringify(v));

// run executes code in the sandbox's own context, where app.js top-level
// lexicals (state, _tagsState, ...) are reachable.
function run(sbx, code) {
  return vm.runInContext(code, sbx, { filename: '<grouping-test>' });
}

// makeStore wraps a plain object in a localStorage-shaped fake.
function makeStore() {
  const m = {};
  return {
    map: m,
    ls: {
      getItem: (k) => (k in m) ? m[k] : null,
      setItem: (k, v) => { m[k] = String(v); },
      removeItem: (k) => { delete m[k]; },
    },
  };
}

const COLLAPSED_KEY = 'cs_sessions_collapsed_groups';
const VIEWS_KEY = 'cs_sessions_saved_views';

// ── 1. placement rule: sorted first match, single placement ─────────────
test('tag placement: sorted first match wins and sessions land once', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();

  // Two-tag session: placement is the sorted first tag, not the first in
  // the array (array order is shuffled in the fixture by construction).
  assert.equal(
    sbx._sessionsTagPlacement({ tags: ['ops:watch', 'data:etl'] }, ''),
    'data:etl');
  assert.equal(
    sbx._sessionsTagPlacement({ tags: ['ops:watch', 'data:etl'] }, 'ops:'),
    'ops:watch');
  assert.equal(
    sbx._sessionsTagPlacement({ tags: ['ops:watch'] }, 'zz:'),
    'ungrouped');
  // Inherited tags participate, same union as the server.
  assert.equal(
    sbx._sessionsTagPlacement({ tags: [], inherited_tags: ['infra:ci'] }, 'infra:'),
    'infra:ci');
  // No tags at all → ungrouped.
  assert.equal(
    sbx._sessionsTagPlacement({ tags: [], inherited_tags: [] }, ''),
    'ungrouped');

  // Prefix 'ops:': i≡0,1 (mod 5) carry ops tags; i≡0,4 land ops:cron (120),
  // i≡1 land ops:watch (60); the other 120 are ungrouped. Total = 300 with
  // no duplicates.
  const groups = sbx._sessionsGroupSessions(sessions, { axis: 'tag', tagPrefix: 'ops:' });
  const byKey = {};
  groups.forEach(g => { byKey[g.key] = g.count; });
  assert.deepEqual(Object.keys(byKey).sort(), ['ops:cron', 'ops:watch', 'ungrouped']);
  assert.equal(byKey['ops:cron'], 120);
  assert.equal(byKey['ops:watch'], 60);
  assert.equal(byKey['ungrouped'], 120);
  const ids = new Set();
  groups.forEach(g => g.sessions.forEach(s => {
    assert.equal(ids.has(s.id), false, 'session appears in exactly one group');
    ids.add(s.id);
  }));
  assert.equal(ids.size, 300);

  // Every group carries a count and a state mix (the header's state mix).
  groups.forEach(g => {
    assert.equal(g.count, g.sessions.length);
    assert.ok(Array.isArray(g.mix) && g.mix.length > 0);
    assert.equal(g.mix.reduce((a, x) => a + x.count, 0), g.count);
  });
});

test('placement: sessions keep their other tags as chips, not moved', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();
  // Group by prefix 'ops:' — the same session carries data/infra tags too;
  // those remain as data (chips source), not as placement. Session i=4
  // carries [infra:ci, ops:cron] and lands under ops:cron.
  const g = sbx._sessionsGroupSessions(sessions, { axis: 'tag', tagPrefix: 'ops:' });
  const opsCron = g.find(x => x.key === 'ops:cron');
  const dual = opsCron.sessions.find(s => s.tags.some(tg => tg.indexOf('ops:') === 0)
    && s.tags.some(tg => tg.indexOf('ops:') !== 0));
  assert.ok(dual, 'a dual tagged session lands inside ops:cron');
  assert.ok(dual.tags.includes('infra:ci'), 'non-ops tag stays on the session');
});

// ── 2. axis grouping: state / node / project ─────────────────────────────
test('state / node / project axes: complete, sorted, correctly counted', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();

  const st = sbx._sessionsGroupSessions(sessions, { axis: 'state' });
  assert.deepEqual(plain(st.map(g => g.key).sort()),
    ['error', 'idle', 'running', 'stopped', 'waiting_input']);
  st.forEach(g => assert.equal(g.count, 60));

  const no = sbx._sessionsGroupSessions(sessions, { axis: 'node' });
  assert.deepEqual(plain(no.map(g => g.key)), ['node-a', 'node-b', 'node-c']); // sorted
  no.forEach(g => assert.equal(g.count, 100));

  const pr = sbx._sessionsGroupSessions(sessions, { axis: 'project' });
  assert.deepEqual(plain(pr.map(g => g.key)), ['/p/alpha', '/p/beta']);
  pr.forEach(g => assert.equal(g.count, 150));

  // Unknown session pieces never crash: missing fields bucket to ungrouped.
  const odd = sbx._sessionsGroupSessions(
    [{ state: null }, { hostname: null }, {}], { axis: 'state' });
  assert.equal(odd.length, 1);
  assert.equal(odd[0].key, 'unknown');
});

// ── tag axis label + registry name ───────────────────────────────────────
test('group labels: registry name when known, raw key otherwise, ungrouped', (t) => {
  const sbx = freshSandbox();
  run(sbx, "_tagsState.byId = { 'ops:cron': { name: 'Ops: Cron' } }");
  assert.equal(sbx._sessionsGroupLabel('ops:cron', { axis: 'tag' }), 'Ops: Cron');
  assert.equal(sbx._sessionsGroupLabel('ops:missing', { axis: 'tag' }), 'ops:missing');
  assert.equal(
    sbx._sessionsGroupLabel('ungrouped', { axis: 'tag' }),
    'Ungrouped',
    'ungrouped bucket uses its label, not a tag name');
  assert.equal(sbx._sessionsGroupLabel('node-a', { axis: 'node' }), 'node-a');
});

// ── 3. Automaton → story nesting + progress ──────────────────────────────
test('automaton mode nests prd → story, sub-tasks under their story', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();
  const html = sbx.renderSessionsAutomaton(sessions, NOW, 'tiles');

  // All three top blocks exist: two Automata + the ungrouped bucket.
  for (const k of ['automaton|::prd-A', 'automaton|::prd-B', 'automaton|::ungrouped']) {
    assert.ok(html.includes(`data-automaton-block="${k}"`), 'block ' + k);
  }
  // Story buckets inside prd-A (fixture: i<14, i%3: story-0 ×5, story-1 ×5,
  // story-less ×4 = 14 sub-task sessions, each exactly once).
  assert.ok(html.includes('data-story-block="automaton|::prd-A::story-0"'));
  assert.ok(html.includes('data-story-block="automaton|::prd-A::story-1"'));
  assert.ok(html.includes('data-story-block="automaton|::prd-A::root"'));

  const aIdx = html.indexOf('data-automaton-block="automaton|::prd-A"');
  const bIdx = html.indexOf('data-automaton-block="automaton|::prd-B"');
  const uIdx = html.indexOf('data-automaton-block="automaton|::ungrouped"');
  assert.ok(aIdx < bIdx && bIdx < uIdx, 'blocks sorted by Automaton key');
  const segA = html.slice(aIdx, bIdx);
  const segU = html.slice(uIdx);
  for (let i = 0; i < 14; i++) assert.ok(segA.includes('full-sess-' + i) || segA.includes('sess-' + i), 'sub-task i in prd-A');
  assert.ok(!segA.includes('full-sess-14'), 'prd-B sub-task not in prd-A');
  assert.ok(!segU.includes('full-sess-0'), 'prd sub-task not in ungrouped');
  assert.ok(segU.includes('full-sess-100'), 'regular session in ungrouped bucket');

  // Progress on the Automaton header: count + state mix.
  assert.ok(segA.includes('sgp-count') && segA.includes('>14<'), 'count 14 on prd-A header');
  assert.ok(segA.includes('sgp-mix'), 'state mix chips on prd-A header');
  assert.ok(segA.includes('stopped 2'), 'mix counts: 14 sub-tasks → 2 stopped');
});

// ── 4. grouped rendering by active mode + per-group paging ───────────────
test('groups render in the active mode with per-group show-more', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();
  const by = { axis: 'node' }; // 100 sessions/group > 60 default page size

  const tiles = sbx.renderSessionsGrouped(sessions, by, NOW, 'tiles');
  assert.ok(tiles.includes('session-tile-grid') && !tiles.includes('-compact'));
  assert.ok(tiles.includes('60/100'), 'per-group paging 60/100 visible');
  assert.ok(tiles.includes('sgp-more'), 'Show more inside a group');

  const compact = sbx.renderSessionsGrouped(sessions, by, NOW, 'compact');
  assert.ok(compact.includes('session-tile-grid-compact'), 'compact mode body');

  const list = sbx.renderSessionsGrouped(sessions, by, NOW, 'list');
  assert.ok(list.includes('state-running'), 'list mode session cards');
  assert.ok(!list.includes('session-tile-grid'), 'list mode is not tiles');

  // Show more advances the group's own limit; that group closes out while
  // sibling groups keep their own paging.
  sbx.sessionsGroupShowMore('node::node-a');
  assert.equal(run(sbx, 'state._sessionsGroupPage["node::node-a"]'), 120);
  const more = sbx.renderSessionsGrouped(sessions, by, NOW, 'tiles');
  const aStart = more.lastIndexOf('<section', more.indexOf('data-sessions-group="node::node-a"'));
  const bStart = more.lastIndexOf('<section', more.indexOf('data-sessions-group="node::node-b"'));
  const segA = more.slice(aStart, bStart);
  assert.ok(!segA.includes('sgp-more'), 'node-a fully shown after two pages');
  assert.ok(segA.includes('full-sess-297'), 'node-a page-2 session now rendered');
  const segB = more.slice(bStart);
  assert.ok(segB.includes('sgp-more'), 'node-b still paginated at its own limit');
  // Other groups' paging state is untouched.
  assert.equal(run(sbx, 'state._sessionsGroupPage["node::node-b"]'), undefined);
});

test('group picker: axes + registry prefixes + current selection', (t) => {
  const sbx = freshSandbox();
  run(sbx, "_tagsState.list = [{ name: 'ops:cron' }, { name: 'ops:watch' }, { name: 'data:etl' }, { name: 'infra:ci' }]");
  const html = sbx.renderSessionsGroupPicker(null);
  for (const v of ['', 'state', 'node', 'project', 'tag', 'automaton', 'tag|ops:', 'tag|data:', 'tag|infra:']) {
    assert.ok(html.includes(`value="${v}"`) || v === '', 'option ' + v);
  }
  assert.ok(html.includes('Tag prefix'), 'prefix optgroup label');

  const sel = sbx.renderSessionsGroupPicker({ axis: 'tag', tagPrefix: 'ops:' });
  assert.ok(sel.includes('value="tag|ops:" selected'), 'current axis selected');

  run(sbx, 'state.activeView = "work"');
  sbx.setSessionGroupBy('tag|ops:');
  assert.equal(run(sbx, 'state.sessionGroupBy.axis'), 'tag');
  assert.equal(run(sbx, 'state.sessionGroupBy.tagPrefix'), 'ops:');
  sbx.setSessionGroupBy('state');
  assert.equal(run(sbx, 'state.sessionGroupBy.axis'), 'state');
  sbx.setSessionGroupBy('');
  assert.equal(run(sbx, 'state.sessionGroupBy'), null);
  sbx.setSessionGroupBy('bogus');
  assert.equal(run(sbx, 'state.sessionGroupBy'), null);
});

// ── 5. collapse persistence (per device) ─────────────────────────────────
test('collapsed groups persist on device and survive re-render', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();
  const store = makeStore();
  sbx.localStorage = store.ls;

  sbx._sessionsGroupToggleCollapse('state::running');
  const saved = plain(JSON.parse(store.map[COLLAPSED_KEY]));
  assert.ok(saved.includes('state::running'));

  // Collapsed groups render header-only (no body), expanded keep theirs.
  const html = sbx.renderSessionsGrouped(sessions, { axis: 'state' }, NOW, 'list');
  // Sorted order: error, idle, running, stopped, waiting_input.
  const runStart = () => html.lastIndexOf('<section', html.indexOf('data-sessions-group="state::running"'));
  const stopStart = html.lastIndexOf('<section', html.indexOf('data-sessions-group="state::stopped"'));
  const waitStart = html.lastIndexOf('<section', html.indexOf('data-sessions-group="state::waiting_input"'));
  assert.ok(runStart() > -1 && waitStart > stopStart, 'running and waiting sections present');
  const segRunning = html.slice(runStart(), stopStart);
  assert.ok(segRunning.includes('sessions-group collapsed'), 'running group class collapsed');
  assert.ok(segRunning.includes('aria-expanded="false"'), 'running header shows expanded=false');
  assert.ok(!segRunning.includes('sessions-group-body'), 'collapsed group has no body');
  const segWaiting = html.slice(waitStart);
  assert.ok(!segWaiting.includes('sessions-group collapsed'), 'other groups stay expanded');
  assert.ok(segWaiting.includes('sessions-group-body'));

  // Toggle back open → removed from persisted set.
  sbx._sessionsGroupToggleCollapse('state::running');
  assert.deepEqual(plain(JSON.parse(store.map[COLLAPSED_KEY])), []);

  // Corrupted storage degrades to "nothing collapsed", no throw.
  store.map[COLLAPSED_KEY] = 'not json';
  run(sbx, 'state._sessionsCollapsed = undefined');
  assert.deepEqual(plain(sbx._sessionsCollapsedRead(store.ls)), []);
  assert.doesNotThrow(() => sbx.renderSessionsGrouped(sessions, { axis: 'state' }, NOW, 'list'));
});

test('collapse storage: blocked storage degrades gracefully', (t) => {
  const sbx = freshSandbox();
  const boom = {
    getItem() { throw new Error('storage denied'); },
    setItem() { throw new Error('storage denied'); },
    removeItem() { throw new Error('storage denied'); },
  };
  sbx.localStorage = boom;
  assert.deepEqual(plain(sbx._sessionsCollapsedRead()), []);
  assert.doesNotThrow(() => sbx._sessionsCollapseWrite(['state::running']));
  // In-memory state still drives the render.
  run(sbx, 'state._sessionsCollapsed = ["state::running"]');
  const html = sbx.renderSessionsGrouped(makeSessions(), { axis: 'state' }, NOW, 'list');
  assert.ok(html.includes('sessions-group collapsed'));
});

// ── 6. saved views round-trip ────────────────────────────────────────────
test('saved views: save / apply / rename / delete round-trip', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();
  const store = makeStore();
  sbx.localStorage = store.ls;
  run(sbx, 'state.activeView = "work"');

  // Capture the current mode + group axis + filter.
  run(sbx, "state.sessionsViewMode = 'tiles'");
  run(sbx, 'state.sessionGroupBy = { axis: "tag", tagPrefix: "ops:" }');
  run(sbx, "state.sessionFilter = 'etl'");
  const v = sbx.saveSessionsView('My <view>');
  assert.ok(v && v.id);
  assert.equal(v.name, 'My <view>');
  assert.equal(v.mode, 'tiles');
  assert.equal(JSON.stringify(v.groupBy), JSON.stringify({ axis: 'tag', tagPrefix: 'ops:' }));
  assert.equal(v.filter, 'etl');
  const onDisk = JSON.parse(store.map[VIEWS_KEY]);
  assert.equal(onDisk.length, 1);
  assert.equal(onDisk[0].name, 'My <view>');

  // Apply: restore all three parts onto the (reset) working state.
  run(sbx, "state.sessionsViewMode = 'list'");
  run(sbx, 'state.sessionGroupBy = null');
  run(sbx, 'state.sessionFilter = ""');
  run(sbx, 'state._savedViews = undefined');
  assert.equal(sbx.applySavedView(v.id), true);
  assert.equal(run(sbx, 'state.sessionsViewMode'), 'tiles');
  assert.equal(run(sbx, 'state.sessionGroupBy.axis'), 'tag');
  assert.equal(run(sbx, 'state.sessionGroupBy.tagPrefix'), 'ops:');
  assert.equal(run(sbx, 'state.sessionFilter'), 'etl');

  // Rename (name is the only identity that changes), reject blank.
  assert.equal(sbx.renameSavedView(v.id, 'Ops queue'), true);
  assert.equal(JSON.parse(store.map[VIEWS_KEY])[0].name, 'Ops queue');
  assert.equal(sbx.renameSavedView(v.id, '   '), false);
  assert.equal(JSON.parse(store.map[VIEWS_KEY])[0].name, 'Ops queue');
  assert.equal(sbx.renameSavedView('missing-id', 'X'), false);

  // Delete, then deleting again is a no-op false.
  assert.equal(sbx.deleteSavedView(v.id), true);
  assert.deepEqual(JSON.parse(store.map[VIEWS_KEY]), []);
  assert.equal(sbx.deleteSavedView(v.id), false);
  assert.equal(sbx.applySavedView(v.id), false);

  // Invalid names never persist.
  assert.equal(sbx.saveSessionsView('   '), null);
  assert.equal(sbx.saveSessionsView(''), null);
  assert.deepEqual(JSON.parse(store.map[VIEWS_KEY]), []);
});

test('saved views: blocked storage stays in-memory, chips render', (t) => {
  const sbx = freshSandbox();
  const boom = {
    getItem() { throw new Error('denied'); },
    setItem() { throw new Error('denied'); },
    removeItem() { throw new Error('denied'); },
  };
  sbx.localStorage = boom;
  run(sbx, 'state.activeView = "work"');
  const v = sbx.saveSessionsView('In mem');
  assert.ok(v, 'save returns the view even when storage is blocked');
  assert.equal(v.name, 'In mem');

  // The bar renders the chip with its apply/rename/delete hooks.
  const html = sbx.renderSessionsViewsBar();
  assert.ok(html.includes('In mem'));
  assert.ok(html.includes(`applySavedView('${v.id}')`));
  assert.ok(html.includes(`deleteSavedView('${v.id}')`));
  assert.ok(html.includes('submitSavedViewSave'), 'new-view input row');
});

// ── 7. filter interplay: session filter narrows before grouping ──────────
test('session filter + tag filter apply before grouping', (t) => {
  const sbx = freshSandbox();
  const sessions = makeSessions();

  // Same predicate renderSessionsView uses for state.sessionFilter.
  function filtered(text) {
    const q = (text || '').toLowerCase();
    if (!q) return sessions;
    return sessions.filter(s =>
      (s.name || '').toLowerCase().includes(q) ||
      (s.task || '').toLowerCase().includes(q) ||
      (s.id || '').toLowerCase().includes(q));
  }

  // 'job-3-ets' style filter: task 'job-n-etl' → 300/7 ≈ 42/43 by n.
  const pool = filtered('etl');           // all 300 (task suffix)
  const pool2 = filtered('job-3-');       // only task idx 3 → 43 sessions
  assert.equal(pool.length, 300);
  assert.equal(pool2.length, 43);

  // Grouping runs on the narrowed pool; totals are preserved.
  const g1 = sbx._sessionsGroupSessions(pool, { axis: 'node' });
  assert.equal(g1.reduce((a, g) => a + g.count, 0), 300);
  const g2 = sbx._sessionsGroupSessions(pool2, { axis: 'node' });
  assert.equal(g2.reduce((a, g) => a + g.count, 0), 43);
  g2.forEach(g => assert.equal(g.count, g.sessions.length));

  // Tag axis on the filtered pool: placement rule unchanged.
  const g3 = sbx._sessionsGroupSessions(pool2, { axis: 'tag', tagPrefix: 'ops:' });
  assert.equal(g3.reduce((a, g) => a + g.count, 0), 43);
});

// ── rendering invariants ─────────────────────────────────────────────────
test('grouped renders never throw on empty pool or odd input', (t) => {
  const sbx = freshSandbox();
  assert.doesNotThrow(() => sbx.renderSessionsGrouped([], { axis: 'state' }, NOW, 'tiles'));
  assert.doesNotThrow(() => sbx.renderSessionsGrouped(null, { axis: 'node' }, NOW, 'compact'));
  assert.doesNotThrow(() => sbx.renderSessionsAutomaton([], NOW, 'list'));
  assert.doesNotThrow(() => sbx.renderSessionsAutomaton([{ prd_id: 'p', story_id: 42 }], NOW, 'list'));
  assert.doesNotThrow(() => sbx.renderSessionsGrouped(
    makeSessions(), { axis: 'tag', tagPrefix: 'nope:' }, NOW, 'list'));
});

test('all i18n keys used by the grouping UI exist in en.json', (t) => {
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  const used = ('sessions_group_by sessions_group_none sessions_group_state sessions_group_node sessions_group_project '
    + 'sessions_group_tag sessions_group_tag_prefixes sessions_group_automaton sessions_group_ungrouped '
    + 'sessions_group_empty sessions_group_none_view sessions_group_show_more sessions_auto_ungrouped '
    + 'sessions_auto_story_root sessions_view_title sessions_view_apply sessions_view_rename '
    + 'sessions_view_delete sessions_view_new sessions_view_ok sessions_view_name_ph').split(' ');
  used.forEach(k => assert.equal(typeof en[k], 'string', k + ' present'));
});

// ── project axis: placement tag, then working folder, then ungrouped ─────
test('project axis: project: system tag, else project_dir, else ungrouped', () => {
  const sbx = freshSandbox();
  const by = { axis: 'project' };
  // Automaton task session in a worktree, tagged with its project.
  assert.equal(sbx._sessionsGroupKeyFor({
    prd_id: 'prd-A', project_dir: '/home/x/.datawatch/prd-worktrees/wt',
    system_tags: ['project:datawatch'],
  }, by), 'datawatch');
  // Object-shaped system tag entries work too.
  assert.equal(sbx._sessionsGroupKeyFor({
    project_dir: '/p/alpha', system_tags: [{ name: 'project:datawatch' }],
  }, by), 'datawatch');
  // Non-project placement tags are ignored; folder is used.
  assert.equal(sbx._sessionsGroupKeyFor({
    project_dir: '/p/alpha', system_tags: ['compute:gpu-1'],
  }, by), '/p/alpha');
  // Plain session with only a working folder.
  assert.equal(sbx._sessionsGroupKeyFor({ project_dir: '/p/beta' }, by), '/p/beta');
  // Neither: ungrouped, and the header reads Ungrouped.
  assert.equal(sbx._sessionsGroupKeyFor({}, by), 'ungrouped');
  assert.equal(sbx._sessionsGroupKeyFor({ system_tags: [] }, by), 'ungrouped');
  assert.equal(sbx._sessionsGroupLabel('datawatch', by), 'datawatch');
});

// ── 8. the group select keeps the choice (render, refresh, reload) ───────
// Operator report (2026-11-12): the dropdown snapped back to "No grouping".
// The tag-prefix option comes from the tag registry, which can be empty
// during a refresh, so the chosen option vanished; the choice was also not
// kept across a reload.
function paintedSandbox(store) {
  const cap = { html: '' };
  const el = { querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return cap.html; }, set(v) { cap.html = v; } });
  const sbx = buildSandbox({ byId: { view: el } });
  if (store) sbx.localStorage = store.ls;
  vm.createContext(sbx);
  loadScript(APP_JS, sbx);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sbx._i18n.bundle = en;
  sbx._i18n.fallback = en;
  run(sbx, `state.sessions = ${JSON.stringify(makeSessions())};
    state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionFilter = ''; state.sessionTreeView = false; state._filtersCollapsed = true;
    state.sessionsViewMode = 'list';`);
  return { sbx, cap };
}

function selectedGroupOption(html) {
  const m = html.match(/<select id="sessions-group-select"[\s\S]*?<\/select>/);
  assert.ok(m, 'group select is rendered');
  const sel = m[0].match(/<option value="([^"]*)" selected>/g) || [];
  assert.equal(sel.length, 1, 'exactly one option is selected');
  return sel[0].match(/value="([^"]*)"/)[1];
}

test('group select keeps the choice after re-render and list refresh', () => {
  const store = makeStore();
  const { sbx, cap } = paintedSandbox(store);
  run(sbx, "_tagsState.list = [{ name: 'ops:cron' }, { name: 'data:etl' }]; _tagsState.loaded = true;");
  sbx.setSessionGroupBy('tag|ops:');
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'right after selection');
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'after a re-render');

  // A list refresh: new session objects, and the tag registry momentarily empty.
  run(sbx, `state.sessions = ${JSON.stringify(makeSessions())}; _tagsState.list = []; _tagsState.loaded = false;`);
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'after a refresh with no registry');
  run(sbx, "_tagsState.list = [{ name: 'data:etl' }]; _tagsState.loaded = true;");
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'after the registry no longer lists the prefix');

  sbx.setSessionGroupBy('state');
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'state');
});

test('group select choice is restored after a reload and blocked storage is tolerated', () => {
  const store = makeStore();
  let { sbx, cap } = paintedSandbox(store);
  sbx.setSessionGroupBy('tag|ops:');
  assert.deepEqual(plain(JSON.parse(store.map['dw_sessions_group_by'])), { axis: 'tag', tagPrefix: 'ops:' });

  // Reload: a new sandbox over the same storage.
  ({ sbx, cap } = paintedSandbox(store));
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'restored after reload');
  assert.equal(run(sbx, 'state.sessionGroupBy.tagPrefix'), 'ops:');

  // Clearing the choice is remembered too.
  sbx.setSessionGroupBy('');
  ({ sbx, cap } = paintedSandbox(store));
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), '');

  // A corrupt saved value falls back to no grouping.
  store.map['dw_sessions_group_by'] = '{"axis":"bogus"}';
  ({ sbx, cap } = paintedSandbox(store));
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), '');

  // Blocked storage: choice still holds in memory, nothing throws.
  // Only the group keys are blocked; the rest of the app reads other keys.
  const blocked = {
    getItem(k) { if (/^dw_sessions_group/.test(k)) throw new Error('blocked'); return null; },
    setItem(k) { if (/^dw_sessions_group/.test(k)) throw new Error('blocked'); },
  };
  ({ sbx, cap } = paintedSandbox(makeStore()));
  sbx.localStorage = blocked;
  sbx.setSessionGroupBy('node');
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'node');
});

test('toolbar placement control and the group select share one source of truth', () => {
  const store = makeStore();
  store.map['dw_sessions_group_axis'] = 'ops';   // legacy saved axis
  const { sbx, cap } = paintedSandbox(store);
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'legacy axis migrates into the shared choice');
  sbx.setSessionsGroupAxis('data');
  assert.equal(run(sbx, 'state.sessionGroupBy.tagPrefix'), 'data:');
  assert.equal(selectedGroupOption(cap.html), 'tag|data:');
  sbx.setSessionsGroupAxis('');
  assert.equal(run(sbx, 'state.sessionGroupBy'), null);
});
