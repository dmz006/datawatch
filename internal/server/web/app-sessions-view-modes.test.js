// The Sessions topbar Tiles / Compact / List buttons change the rendering with
// no grouping, with a grouping, after a refresh (re-render with new data) and
// after a reload (mode remembered in localStorage cs_sessions_view_mode).
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const NOW = Date.parse('2026-10-25T12:00:00Z');

function makeSessions(n, tag) {
  const out = [];
  for (let i = 0; i < n; i++) {
    out.push({
      id: (tag || 'cs') + i, full_id: 'local/' + (tag || 'cs') + i, name: 'job ' + i, task: 'task ' + i,
      state: 'running', backend_family: 'claude-code', llm_ref: 'opus',
      hostname: 'node-' + (i % 2),
      updated_at: new Date(NOW - i * 60000).toISOString(),
      created_at: new Date(NOW - i * 60000 - 1000).toISOString(),
    });
  }
  return out;
}
function makeStore() {
  const m = {};
  return { map: m, ls: {
    getItem: k => (k in m) ? m[k] : null,
    setItem: (k, v) => { m[k] = String(v); },
    removeItem: k => { delete m[k]; },
  } };
}
const run = (sbx, code) => vm.runInContext(code, sbx, { filename: '<view-modes-test>' });

// Fresh page over a shared storage; the view mode is NOT preset, so the real
// default/stored path is exercised.
function page(store) {
  const cap = { html: '' };
  const el = { querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return cap.html; }, set(v) { cap.html = v; } });
  const sbx = buildSandbox({ byId: { view: el } });
  sbx.localStorage = store.ls;
  sbx.innerWidth = 1280;
  vm.createContext(sbx);
  loadScript(path.join(WEB, 'app.js'), sbx);
  const en = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  sbx._i18n.bundle = en; sbx._i18n.fallback = en;
  run(sbx, `state.sessions = ${JSON.stringify(makeSessions(6))};
    state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionFilter = ''; state._filtersCollapsed = true;`);
  return { sbx, cap };
}
// Only the session list region, so the topbar mode buttons don't pollute matches.
const listOf = html => {
  const i = html.indexOf('<div class="session-list">');
  assert.ok(i >= 0, 'session-list rendered');
  return html.slice(i);
};
const kinds = html => {
  const l = listOf(html);
  return {
    grid: /class="work-tile-grid session-tile-grid(?! session-tile-grid-compact)[ "]/.test(l) || /class="work-tile-grid session-tile-grid" /.test(l),
    compact: l.includes('session-tile-grid-compact'),
    cards: l.includes('session-card'),
    any: l.includes('session-tile-grid'),
  };
};

for (const grouped of [false, true]) {
  const label = grouped ? 'with a grouping' : 'with no grouping';
  test('Tiles, Compact and List render differently ' + label, () => {
    const { sbx, cap } = page(makeStore());
    if (grouped) run(sbx, "state.sessionGroupBy = { axis: 'node' };");
    else run(sbx, 'state.sessionGroupBy = null;');
    const out = {};
    for (const m of ['tiles', 'compact', 'list']) {
      sbx._sessionsSetViewMode(m);
      out[m] = listOf(cap.html);
    }
    const k = {}; for (const m of Object.keys(out)) { cap.html = '<div class="session-list">' + out[m]; k[m] = kinds(cap.html); }
    // (1) Tiles: tile grid, not compact, no list rows
    assert.ok(out.tiles.includes('session-tile-grid'), 'tiles draws the tile grid');
    assert.ok(!out.tiles.includes('session-tile-grid-compact'), 'tiles is not compact');
    assert.ok(!out.tiles.includes('session-card'), 'tiles has no list rows');
    // (2) Compact: compact grid whose data-tile-mode is a tile mode
    assert.ok(out.compact.includes('session-tile-grid-compact'), 'compact draws the compact grid');
    assert.match(out.compact, /session-tile-grid-compact" data-tile-mode="tile"/);
    assert.ok(!out.compact.includes('session-card'), 'compact has no list rows');
    // (3) List: rows, no tile grid
    assert.ok(out.list.includes('session-card'), 'list draws session-card rows');
    assert.ok(!out.list.includes('session-tile-grid'), 'list has no tile grid');
    assert.notEqual(out.tiles, out.compact);
    assert.notEqual(out.tiles, out.list);
    assert.notEqual(out.compact, out.list);
    void k;
  });
}

test('the mode persists across a refresh (re-render with new session data)', () => {
  const { sbx, cap } = page(makeStore());
  run(sbx, 'state.sessionGroupBy = null;');
  for (const m of ['compact', 'list', 'tiles']) {
    sbx._sessionsSetViewMode(m);
    run(sbx, `state.sessions = ${JSON.stringify(makeSessions(9, 'n' + m))};`);
    sbx.renderSessionsView();
    const l = listOf(cap.html);
    if (m === 'list') assert.ok(l.includes('session-card') && !l.includes('session-tile-grid'), 'list after refresh');
    if (m === 'compact') assert.ok(l.includes('session-tile-grid-compact'), 'compact after refresh');
    if (m === 'tiles') assert.ok(l.includes('session-tile-grid') && !l.includes('session-tile-grid-compact'), 'tiles after refresh');
  }
});

test('a reload restores the stored mode and applies it with no grouping', () => {
  const store = makeStore();
  for (const m of ['compact', 'list', 'tiles']) {
    const first = page(store);
    run(first.sbx, 'state.sessionGroupBy = null;');
    first.sbx._sessionsSetViewMode(m);
    assert.equal(store.map['cs_sessions_view_mode'], m);
    const { sbx, cap } = page(store); // simulated reload over the same storage
    run(sbx, 'state.sessionGroupBy = null;');
    sbx.renderSessionsView();
    assert.equal(sbx._sessionsViewMode(), m);
    const l = listOf(cap.html);
    if (m === 'list') assert.ok(l.includes('session-card') && !l.includes('session-tile-grid'));
    if (m === 'compact') assert.ok(l.includes('session-tile-grid-compact'));
    if (m === 'tiles') assert.ok(l.includes('session-tile-grid') && !l.includes('session-tile-grid-compact'));
  }
});

// Root cause: the lineage-tree branch ran before the mode choice, and a
// persisted tree flag (toggle hidden while filters are collapsed) made all
// three modes render the same tree.
test('a persisted tree-view flag does not override Tiles or Compact', () => {
  const store = makeStore();
  store.map['cs_session_tree_view'] = '1';
  const { sbx, cap } = page(store);
  run(sbx, 'state.sessionGroupBy = null;');
  assert.equal(run(sbx, 'state.sessionTreeView'), true);
  sbx._sessionsSetViewMode('tiles');
  assert.ok(listOf(cap.html).includes('session-tile-grid'));
  sbx._sessionsSetViewMode('compact');
  assert.ok(listOf(cap.html).includes('session-tile-grid-compact'));
  sbx._sessionsSetViewMode('list');
  assert.ok(listOf(cap.html).includes('session-card') && !listOf(cap.html).includes('session-tile-grid'));
  // reload with a stale tree flag and a stored grid mode still shows the grid
  store.map['cs_session_tree_view'] = '1';
  store.map['cs_sessions_view_mode'] = 'tiles';
  const re = page(store);
  run(re.sbx, 'state.sessionGroupBy = null;');
  re.sbx.renderSessionsView();
  assert.ok(listOf(re.cap.html).includes('session-tile-grid'));
});
