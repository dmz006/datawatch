// Sessions view modes: list / tiles / compact. Covers the pure helpers
// (mode normalize, viewport defaults, paging math, summary counts, the
// try/catch localStorage persistence), the tile markup for one session
// (state chip, backend · model, node, tags with the +N overflow, activity,
// and the per-state actions moved into the ⋯ overflow menu), the Show-more
// paging with a 300-session fixture, and an end-to-end renderSessionsView
// proof that list / tiles / compact all draw the same filtered page.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const NOW = Date.parse('2026-10-25T12:00:00Z');
const plain = v => JSON.parse(JSON.stringify(v));
const MODE_KEY = 'cs_sessions_view_mode';

const TAGS = {
  t1: { id: 't1', name: 'Alpha' },
  t2: { id: 't2', name: 'Beta' },
  t3: { id: 't3', name: 'Gamma' },
  t4: { id: 't4', name: 'Delta' },
  t5: { id: 't5', name: 'Epsilon' },
};

function loadApp({ byId = {} } = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  // t() returns the raw key until a bundle loads; install the English one so
  // the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

// The 300-session fixture: five states round-robin, every sixth session named
// "alpha job N" so a text search has a known, non-trivial subset to keep, and
// every seventh session carrying five registry tags so the +N overflow chip
// fires. updated_at is spread over the past hours so the recent/history split
// is exercised too.
function makeSessions(n) {
  const states = ['running', 'waiting_input', 'complete', 'failed', 'killed'];
  const list = [];
  for (let i = 0; i < n; i++) {
    const s = {
      id: 'ss' + i,
      full_id: 'local/ss' + i,
      name: (i % 6 === 0) ? 'alpha job ' + i : 'beta job ' + (i % 5),
      task: 'task ' + i,
      state: states[i % states.length],
      backend_family: 'claude-code',
      llm_ref: 'opus',
      hostname: 'node-' + (i % 3),
      updated_at: new Date(NOW - (i % 20) * 3600 * 1000).toISOString(),
      created_at: new Date(NOW - (i % 20) * 3600 * 1000 - 1000).toISOString(),
    };
    if (i % 7 === 0) s.tags = ['t1', 't2', 't3', 't4', 't5'];
    list.push(s);
  }
  return list;
}

function setSessions(sb, sessions) {
  vm.runInContext(`state.sessions = ${JSON.stringify(sessions)};`, sb);
}

function setRegistry(sb) {
  const list = Object.values(TAGS);
  const byId = {};
  list.forEach(tg => { byId[tg.id] = tg; });
  vm.runInContext(`_tagsState.list = ${JSON.stringify(list)}; _tagsState.byId = ${JSON.stringify(byId)}; _tagsState.loaded = true;`, sb);
}

function countTiles(html) {
  // data-sess-id exists only on session tiles (list cards expose data-full-id),
  // so counting it over the whole paint is the mode-discriminating signal.
  return (html.match(/data-sess-id="/g) || []).length;
}

// ── mode normalize + viewport defaults ──────────────────────────────────────

test('only the three named modes are accepted', () => {
  const sb = loadApp();
  assert.equal(sb._sessionsViewModeNormalize('list'), 'list');
  assert.equal(sb._sessionsViewModeNormalize('tiles'), 'tiles');
  assert.equal(sb._sessionsViewModeNormalize('compact'), 'compact');
  assert.equal(sb._sessionsViewModeNormalize('bogus'), null);
  assert.equal(sb._sessionsViewModeNormalize(undefined), null);
  assert.equal(sb._sessionsViewModeNormalize(7), null);
});

test('phone widths default to the list, wider screens default to tiles', () => {
  const sb = loadApp();
  assert.equal(sb._sessionsTileModeDefault(360), 'list');
  assert.equal(sb._sessionsTileModeDefault(599), 'list');
  assert.equal(sb._sessionsTileModeDefault(600), 'tiles', 'the boundary sits on the tile side');
  assert.equal(sb._sessionsTileModeDefault(1024), 'tiles');
  assert.equal(sb._sessionsTileModeDefault(undefined), 'list', 'an unknown width keeps the classic list');
  assert.equal(sb._sessionsTileModeDefault(-1), 'list', 'a non-positive width is unknown, so the list');
});

test('a first visit opens by viewport, and later visits honor the stored choice', () => {
  const phone = loadApp();
  phone.innerWidth = 360;
  assert.equal(phone._sessionsViewMode(), 'list');
  const wide = loadApp();
  wide.innerWidth = 1280;
  assert.equal(wide._sessionsViewMode(), 'tiles');
});

test('the stored mode wins over the viewport, and a garbage stored value falls back', () => {
  const sb = loadApp();
  sb.innerWidth = 360;
  sb.localStorage.setItem(MODE_KEY, 'compact');
  assert.equal(sb._sessionsViewMode(), 'compact');
  const bad = loadApp();
  bad.innerWidth = 360;
  bad.localStorage.setItem(MODE_KEY, 'bogus-mode');
  assert.equal(bad._sessionsViewMode(), 'list', 'a non-mode value is ignored in favor of the viewport default');
  assert.equal(bad._sessionsReadStoredMode(), 'bogus-mode', 'the raw read is left as-is...');
  assert.equal(bad._sessionsViewModeNormalize(bad._sessionsReadStoredMode()), null, '... and normalization turns it into a fallback');
});

// ── storage safety ──────────────────────────────────────────────────────────

test('a blocked storage never throws and degrades to the in-memory choice', () => {
  const sb = loadApp();
  const throwing = {};
  Object.defineProperty(throwing, 'getItem', { get() { throw new Error('blocked'); } });
  Object.defineProperty(throwing, 'setItem', { get() { throw new Error('blocked'); } });
  assert.equal(sb._sessionsReadStoredMode(throwing), null);
  assert.doesNotThrow(() => sb._sessionsStoreMode('compact', throwing));
  assert.equal(sb._sessionsReadStoredMode(throwing), null);
  // The same writes through the real (working) storage still land.
  sb._sessionsStoreMode('tiles');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'tiles');
});

test('a mode choice stores, sticks in memory and re-renders the sessions view', () => {
  const sb = loadApp();
  setSessions(sb, makeSessions(10));
  let paints = 0;
  sb.renderSessionsView = () => { paints++; }; // spy: skip the real DOM paint
  vm.runInContext('state.activeView = "sessions"; state.sessionsViewMode = null; state._sessionsPageLimit = 0;', sb);
  sb._sessionsSetViewMode('compact');
  assert.equal(paints, 1, 'switching modes repaints the view');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'compact');
  assert.equal(sb._sessionsViewMode(), 'compact', 'the memory copy agrees with storage');
  vm.runInContext('state.activeView = "settings";', sb);
  sb._sessionsSetViewMode('list');
  assert.equal(paints, 1, 'no repaint while on another view');
  vm.runInContext('state.activeView = "sessions";', sb);
  sb._sessionsSetViewMode('nonsense');
  assert.equal(paints, 1, 'an invalid mode is refused');
  assert.equal(sb.localStorage.getItem(MODE_KEY), 'list', 'the last valid choice remains stored; the invalid one is not written');
});

// ── paging ──────────────────────────────────────────────────────────────────

test('paging exposes the first 60 of 300 with a hasMore flag, and Show-more grows by pages', () => {
  const sb = loadApp();
  const pool = makeSessions(300);
  assert.equal(sb._sessionsPageSize(), 60, 'the first page is 60 by default');
  let p = plain(sb._sessionsPagingState(pool));
  assert.equal(p.shownCount, 60);
  assert.equal(p.total, 300);
  assert.equal(p.hasMore, true);
  assert.ok(p.shown[59].full_id === 'local/ss59');
  p = plain(sb._sessionsPagingState(pool, 120));
  assert.equal(p.shownCount, 120);
  assert.equal(p.hasMore, true);
  p = plain(sb._sessionsPagingState(pool, 300));
  assert.equal(p.shownCount, 300);
  assert.equal(p.hasMore, false);
  p = plain(sb._sessionsPagingState(pool, 400));
  assert.equal(p.shownCount, 300, 'a limit beyond the pool clamps to the pool');
  assert.equal(p.hasMore, false);
  assert.deepEqual(plain(sb._sessionsPagingState([])), { shown: [], shownCount: 0, total: 0, hasMore: false });
  assert.deepEqual(plain(sb._sessionsPagingState(null)), { shown: [], shownCount: 0, total: 0, hasMore: false });
});

test('the page size is operator-overridable, with sane floors', () => {
  const sb = loadApp();
  assert.equal(sb._sessionsPageSize(), 60);
  vm.runInContext('state._sessionsPageOverride = 25;', sb);
  assert.equal(sb._sessionsPageSize(), 25);
  vm.runInContext('state._sessionsPageOverride = 0;', sb);
  assert.equal(sb._sessionsPageSize(), 60, 'a non-positive override falls back to the default');
  vm.runInContext('delete state._sessionsPageOverride;', sb);
  assert.equal(sb._sessionsPageSize(), 60);
});

test('the Show-more control only appears while a page is still to come', () => {
  const sb = loadApp();
  const withMore = plain(sb._sessionsPagingState(makeSessions(300)));
  const html = sb._sessionsShowMoreHTML(withMore);
  assert.match(html, /<button[^>]*onclick="_sessionsShowMore\(\)"/);
  assert.match(html, />Show more \(60\/300\)<\/button>/);
  const allShown = plain(sb._sessionsPagingState(makeSessions(300), 300));
  assert.equal(sb._sessionsShowMoreHTML(allShown), '');
});

// ── summary bar ─────────────────────────────────────────────────────────────

test('the summary counts group by state in a fixed order and keep unknown states last', () => {
  const sb = loadApp();
  const rows = plain(sb._sessionsSummaryCounts(makeSessions(300)));
  assert.deepEqual(rows.map(r => r.state), ['running', 'waiting_input', 'complete', 'failed', 'killed']);
  assert.equal(rows.reduce((n, r) => n + r.count, 0), 300);
  assert.deepEqual(plain(sb._sessionsSummaryCounts([
    { state: 'complete' }, { state: 'complete' }, { state: 'paused' }, {}, { state: null },
  ])), [
    { state: 'complete', count: 2 },
    { state: 'paused', count: 1 },
    { state: 'unknown', count: 2 },
  ], 'unknown states keep their own names, sorted after the fixed order');
  assert.deepEqual(plain(sb._sessionsSummaryCounts([])), []);
});

// ── mode toggle ─────────────────────────────────────────────────────────────

test('the view select offers all three modes with the current one selected', () => {
  const sb = loadApp();
  const html = sb._sessionsViewSelectHTML('tiles');
  assert.match(html, /<select id="sessionsViewSelect"/);
  for (const m of ['list', 'tiles', 'compact']) {
    assert.match(html, new RegExp('<option value="' + m + '"'), `offers ${m}`);
  }
  assert.match(html, /<option value="tiles" selected>/);
  assert.match(html, /onchange="_sessionsSetViewMode\(this\.value\)"/);
  for (const k of ['sessions_view_toggle', 'sessions_view_list', 'sessions_view_tiles', 'sessions_view_compact']) {
    assert.equal(html.includes(k), false, `the rendered labels never expose the raw i18n key ${k}`);
  }
  assert.match(html, /List/);
  assert.match(html, /Tiles/);
  assert.match(html, /Compact/);
});


test('missing locale keys fall back to the English label, never the raw key', () => {
  const sb = loadApp();
  assert.equal(sb._stx('sessions_no_such_key_x', 'Fallback'), 'Fallback');
  assert.match(sb._sessionsViewSelectHTML('list'), /Compact/);
});

test('session tile progress is lifecycle based', () => {
  const sb = loadApp();
  assert.deepEqual(plain(sb._sessionTileProgress({ state: 'complete' })), { done: 1, total: 1 });
  assert.deepEqual(plain(sb._sessionTileProgress({ state: 'running' })), { done: 1, total: 2 });
  assert.deepEqual(plain(sb._sessionTileProgress({ state: 'failed' })), { done: 0, total: 1 });
});

// ── one session as a tile ───────────────────────────────────────────────────

test('a tile carries name, state chip, backend · model, node, tags and activity', () => {
  const sb = loadApp();
  setRegistry(sb);
  const s = makeSessions(1)[0]; // i=0: waiting? states[0]='running', tags t1..t5, node-0
  assert.equal(s.state, 'running');
  const html = sb.renderSessionTile(s, NOW);
  assert.match(html, /^<div class="prd-row prd-card prd-card-status-running work-tile" id="sess-tile-local\/ss0"\s+data-sess-id="local\/ss0"/);
  assert.match(html, /alpha job 0/);
  assert.match(html, /state-badge-running/);
  assert.match(html, /claude-code · opus/);
  // Two labelled placement facts, not one guessed node: where it runs (node-0)
  // and where its model is served (Anthropic cloud for a claude-code session).
  assert.match(html, /class="session-tile-node session-tile-facts"/);
  assert.match(html, /session-tile-fact-label">Runs on<\/span> node-0/);
  assert.match(html, /session-tile-fact-label">Model on<\/span> Anthropic cloud/);
  // Each fact carries a hover title explaining it.
  assert.match(html, /class="session-tile-fact" title="[^"]*peer name when the session is federated"/);
  assert.match(html, /class="session-tile-fact" title="[^"]*compute node serving the model of this session[^"]*"/, 'the Model fact is hover-titled');
  assert.match(html, /class="tag-chip"/);
  assert.match(html, /work-tile-tags-more/, 'five tags fold past the three visible into the +N chip');
  assert.match(html, />just now<\/?span>/, 'the last-activity line is the locale age string');
});

test('a session with no hostname and no model still draws a complete tile', () => {
  const sb = loadApp();
  const s = { id: 'x1', full_id: 'local/x1', name: 'Plain', state: 'failed', backend_family: 'shell', updated_at: new Date(NOW - 3600e3).toISOString() };
  const html = sb.renderSessionTile(s, NOW);
  assert.match(html, /^<div class="prd-row prd-card prd-card-status-failed work-tile"/);
  assert.ok(!html.includes('session-tile-node'));
  assert.match(html, /1h ago/);
  assert.match(html, /shell/);
});

// ── the two placement facts: runs-on and model-on, on tile AND card ───────
//
// The tile and card must both show two labelled facts, each hover-titled:
//   Runs on  — the host's real name (never the word "local"), or the peer's
//              name for a federated session.
//   Model on — the compute node for pi/Ollama, "Anthropic cloud" for Claude,
//              and nothing when unknown.
// Each scenario below is checked in the tile and in the compact/list card.

test('a pi session on a compute node shows the node under Runs on and Model on', () => {
  const sb = loadApp();
  const pi = { id: 'pi1', full_id: 'local/pi1', name: 'Pi job', state: 'running',
    hostname: 'devbox', backend_family: 'pi-prompt', compute_node_ref: 'pi-node-a',
    llm_ref: 'qwen3', updated_at: new Date(NOW - 60e3).toISOString() };
  const tile = sb.renderSessionTile(pi, NOW);
  assert.match(tile, /session-tile-fact-label">Runs on<\/span> devbox/, 'tile runs on the real host name');
  assert.match(tile, /session-tile-fact-label">Model on<\/span> pi-node-a/, 'tile names the compute node, not Anthropic cloud');
  const card = sb.sessionCard(pi, 0, 1);
  assert.match(card, /card-fact-label">Runs on<\/span> devbox/, 'card runs on the real host name');
  assert.match(card, /card-fact-label">Model on<\/span> pi-node-a/, 'card names the compute node, not Anthropic cloud');
});

test('a Claude session points Model on at Anthropic cloud', () => {
  const sb = loadApp();
  const cc = { id: 'cc1', full_id: 'local/cc1', name: 'Claude job', state: 'running',
    hostname: 'devbox', backend_family: 'claude-code',
    llm_ref: 'claude-opus-4', updated_at: new Date(NOW - 60e3).toISOString() };
  const tile = sb.renderSessionTile(cc, NOW);
  assert.match(tile, /session-tile-fact-label">Runs on<\/span> devbox/);
  assert.match(tile, /session-tile-fact-label">Model on<\/span> Anthropic cloud/, 'tile: Claude model is served by Anthropic cloud');
  const card = sb.sessionCard(cc, 0, 1);
  assert.match(card, /card-fact-label">Runs on<\/span> devbox/);
  assert.match(card, /card-fact-label">Model on<\/span> Anthropic cloud/, 'card: Claude model is served by Anthropic cloud');
});

test('a local session with no model shows Runs on the host and no Model fact', () => {
  const sb = loadApp();
  const loc = { id: 'loc1', full_id: 'local/loc1', name: 'Shell job', state: 'running',
    hostname: 'my-laptop', backend_family: 'shell', updated_at: new Date(NOW - 60e3).toISOString() };
  const tile = sb.renderSessionTile(loc, NOW);
  assert.match(tile, /session-tile-fact-label">Runs on<\/span> my-laptop/, 'runs on the host real name');
  assert.doesNotMatch(tile, /Model on/, 'unknown model: no second fact on the tile');
  assert.doesNotMatch(tile, /Anthropic cloud/);
  const card = sb.sessionCard(loc, 0, 1);
  assert.match(card, /card-fact-label">Runs on<\/span> my-laptop/, 'card shows Runs on');
  assert.doesNotMatch(card, /Model on/, 'unknown model: no second fact on the card');
  assert.doesNotMatch(card, /Anthropic cloud/);
});

test('a federated peer session shows the peer name under Runs on, never local', () => {
  const sb = loadApp();
  const peer = { id: 'fe1', full_id: 'peer-x/fe1', name: 'Remote job', state: 'running',
    server: 'peer-x', hostname: 'peer-x-machine', backend_family: 'claude-code',
    llm_ref: 'claude-opus-4', updated_at: new Date(NOW - 60e3).toISOString() };
  const tile = sb.renderSessionTile(peer, NOW);
  assert.match(tile, /session-tile-fact-label">Runs on<\/span> peer-x/, 'tile runs on the peer name');
  assert.doesNotMatch(tile, /">local/);
  assert.match(tile, /session-tile-fact-label">Model on<\/span> Anthropic cloud/, 'peer Claude session names Anthropic cloud');
  const card = sb.sessionCard(peer, 0, 1);
  assert.match(card, /card-fact-label">Runs on<\/span> peer-x/, 'card runs on the peer name');
  assert.doesNotMatch(card, /card-fact-label">Runs on<\/span> local/);
  assert.match(card, /card-fact-label">Model on<\/span> Anthropic cloud/);
});

test('the Runs-on and Model-on facts each carry a hover title on the tile and the card', () => {
  const sb = loadApp();
  const cc = { id: 'cc2', full_id: 'local/cc2', name: 'Titled', state: 'running',
    hostname: 'devbox', backend_family: 'claude-code', updated_at: new Date(NOW - 60e3).toISOString() };
  const tile = sb.renderSessionTile(cc, NOW);
  assert.match(tile, /class="session-tile-fact" title="[^"]*peer name when the session is federated"/);
  assert.match(tile, /class="session-tile-fact" title="[^"]*compute node serving the model of this session[^"]*"/);
  const card = sb.sessionCard(cc, 0, 1);
  assert.match(card, /class="card-fact" title="[^"]*peer name when the session is federated"/, 'card Runs-on fact is hover-titled');
  assert.match(card, /class="card-fact" title="[^"]*compute node serving the model of this session[^"]*"/, 'card Model-on fact is hover-titled');
});

test('tile actions follow the session state, and all of them live in the overflow menu', () => {
  const sb = loadApp();
  const running = { id: 'r', full_id: 'local/r', name: 'R', state: 'running' };
  const html = sb.renderSessionTile(running, NOW);
  assert.match(html, /killSession\('local\/r'\)/, 'running sessions can be stopped');
  assert.equal(html.includes('restartSession'), false, '... but not restarted');
  assert.equal(html.includes('deleteSession'), false, '... and not deleted');
  assert.equal(html.includes('showCardCmds'), false, 'no quick commands outside the waiting state');
  assert.match(html, /toggleSessionWatch\('local\/r'\)/);
  assert.match(html, /toggleSessionMute\('local\/r'\)/);
  assert.match(html, /<details class="work-tile-more"/, 'a tile with menu items always keeps its ⋯ menu');
  assert.equal(html.includes('work-tile-more is-narrow-only'), false, 'the menu is never width-gated for sessions');

  const waiting = { ...running, state: 'waiting_input' };
  const whtml = sb.renderSessionTile(waiting, NOW);
  assert.match(whtml, /showCardCmds\('local\/r'\)/, 'waiting_input adds the quick commands');
  assert.match(whtml, /killSession\('local\/r'\)/);

  const done = { ...running, state: 'complete' };
  const dhtml = sb.renderSessionTile(done, NOW);
  assert.match(dhtml, /restartSession\('local\/r'\)/, 'done sessions restart');
  assert.match(dhtml, /deleteSession\('local\/r'\)/, 'done sessions delete');
  assert.equal(dhtml.includes('killSession'), false, '... but never stop');

  vm.runInContext('state._summarizerEnabled = true;', sb);
  const swhtml = sb.renderSessionTile(done, NOW);
  assert.match(swhtml, /manualSummarize\('local\/r'\)/, 'the manual summary appears when the summarizer is on');
});

test('a hostile session name and node are escaped on the tile', () => {
  const sb = loadApp();
  const s = {
    id: 'bad', full_id: 'local/bad",onclick=x', name: '<img src=x onerror=alert(1)>',
    state: 'running', hostname: '<script>alert(2)</script>', llm_ref: 'model"><i>x</i>',
  };
  const html = sb.renderSessionTile(s, NOW);
  assert.ok(!html.includes('<img'), 'title markup is escaped');
  assert.ok(!html.includes('<script>'));
  assert.ok(!html.includes('<i>'));
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'));
});

// ── the two grid variants ───────────────────────────────────────────────────

test('compact is the same tile grid at a smaller size, tiles follows the viewport width mode', () => {
  const sb = loadApp();
  setRegistry(sb);
  const s = makeSessions(1)[0];
  const compact = sb.renderSessionTileGrid(sb.renderSessionTile(s, NOW), 'compact');
  assert.match(compact, /class="work-tile-grid session-tile-grid session-tile-grid-compact"/);
  // compact is now a smaller tile grid, so it is pinned to data-tile-mode="tile" (it used to pin the row mode "compact").
  assert.match(compact, /data-tile-mode="tile"/);
  sb.innerWidth = 1280;
  const wide = sb.renderSessionTileGrid(sb.renderSessionTile(s, NOW), 'tiles');
  assert.equal(wide.includes('session-tile-grid-compact'), false);
  assert.match(wide, /data-tile-mode="tile"/, 'wide screens follow the shared width-based tile mode');
  sb.innerWidth = 400;
  assert.match(sb.renderSessionTileGrid('<i>x</i>', 'tiles'), /data-tile-mode="line"/, 'narrow screens follow it too');
  // compact keeps its density on resize because _tileSyncModes skips it
  const grids = [
    { dataset: {}, classList: { contains: () => true } },
    { dataset: { tileMode: 'tile' }, classList: { contains: () => false } },
  ];
  sb.document.querySelectorAll = sel => (sel === '.work-tile-grid' ? grids : []);
  sb.innerWidth = 500;
  sb._tileSyncModes();
  assert.equal(grids[0].dataset.tileMode, undefined, 'the compact-size tile grid is left untouched');
  assert.equal(grids[1].dataset.tileMode, 'compact', '... while the tiles grid follows the width');
});

// ── parity: filters and search apply before render, in every mode ─────────

function viewElement() {
  const viewEl = { capture: '' };
  const el = { innerHTML: '', querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return viewEl.capture; }, set(v) { viewEl.capture = v; } });
  return { viewEl, el };
}
test('list, tiles and compact all draw the same filtered page of the 300-session pool', () => {
  const { viewEl, el } = viewElement();
  const sb = loadApp({ byId: { view: el } });
  sb.innerWidth = 1280;
  setSessions(sb, makeSessions(300));
  setRegistry(sb);
  vm.runInContext(`state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionFilter = ''; state.sessionWatchFilter = false; state.selectedSessions.clear();
    state.sessionTreeView = false; state.sessionsViewMode = 'tiles'; state._filtersCollapsed = true;`, sb);

  const paint = () => { viewEl.capture = ''; sb.renderSessionsView(); return viewEl.capture; };

  // First paint: tiles mode, first page of 60, out of the 300-session pool.
  let html = paint();
  assert.match(html, /session-tile-grid/, 'tiles mode paints the work-tile grid');
  assert.ok(!html.includes('session-tile-grid-compact'), 'default tiles grid is not the compact variant');
  assert.equal(countTiles(html), 60, 'page one renders 60 tiles');
  assert.match(html, />Show more \(60\/300\)/);
  assert.match(html, /sessionsViewSelect/, 'the one-line toolbar view select is painted');
  assert.ok(!html.includes('sessions-summary'), 'no state count line above the tiles');

  // Show-more grows the page until the whole pool fits; paging is per-filter-state.
  vm.runInContext('state._sessionsPageLimit = 120;', sb);
  html = paint();
  assert.equal(countTiles(html), 120);
  vm.runInContext('state._sessionsPageLimit = 300;', sb);
  html = paint();
  assert.equal(countTiles(html), 300, 'all 300 fit once the page outgrows the pool');
  assert.ok(!html.includes('_sessionsShowMore'), 'no Show-more once every session is on screen');

  // Compact mode: same pool, now as the dense fixed grid.
  vm.runInContext('state.sessionsViewMode = "compact"; state._sessionsPageKey = null; state._sessionsPageLimit = 60;', sb);
  html = paint();
  assert.match(html, /session-tile-grid-compact/);
  assert.equal(countTiles(html), 60, 'a mode switch starts the paging back at page one');
  vm.runInContext('state._sessionsPageLimit = 300;', sb);

  // Search applies to the pool before paging, in every mode.
  vm.runInContext(`state.sessionFilter = 'alpha job'; state.sessionsViewMode = "compact"; state._sessionsPageKey = null; state._sessionsPageLimit = 60;`, sb);
  html = paint();
  assert.equal(countTiles(html), 50, 'the search keeps only the 50 alpha sessions');
  assert.ok(!html.includes('_sessionsShowMore'), '50 < one page, so no Show-more');
  for (const i of [1, 2, 3, 4, 5, 7, 8]) {
    assert.ok(!html.includes(`data-sess-id="local/ss${i}"`), `ss${i} (a beta row) is filtered out in compact mode`);
  }

  vm.runInContext(`state.sessionFilter = "alpha job"; state.sessionsViewMode = "list";`, sb);
  html = paint();
  assert.ok(!html.includes('session-tile-grid'), 'list mode draws cards, not tiles');
  assert.equal(countTiles(html), 0, '... and none of the tile roots remain');
  assert.equal((html.match(/class="session-card /g) || []).length, 50, '... with the exact same 50 sessions the tile modes drew');

  // Back to tiles with the same filter: identical membership.
  vm.runInContext(`state.sessionsViewMode = "tiles";`, sb);
  html = paint();
  assert.equal(countTiles(html), 50, 'tiles mode shows the same 50');
  // Clearing the filter is a NEW filter state — paging restarts at page one.
  vm.runInContext(`state.sessionFilter = ""; state.sessionsViewMode = "tiles";`, sb);
  html = paint();
  assert.equal(countTiles(html), 60, 'back to the full pool, page one of 60 again');
  vm.runInContext('state._sessionsPageLimit = 300;', sb);
  html = paint();
  assert.equal(countTiles(html), 300, 'grown to the whole pool the unfiltered view still shows all 300');
});

test('the tag filter applies before render the same way in the tile and the compact modes', () => {
  const sb = loadApp();
  const pool = makeSessions(300);
  setRegistry(sb);
  // Exactly the filter the renderer applies: tag selection, then sort, then page.
  const selCount = pool.filter(s => Array.isArray(s.tags) && s.tags.length).length; // i % 7 === 0
  assert.equal(selCount, 43, '43 of the 300 fixture sessions carry the five tags');
  vm.runInContext(`_tagBrowse.session.sel = ['t1'];`, sb);
  let filtered = sb._sessionTagFilter(pool);
  assert.equal(filtered.length, 43, 'the AND/ANY tag selection keeps exactly its sessions');
  const p1 = plain(sb._sessionsPagingState(filtered));
  assert.equal(p1.shownCount, 43);
  assert.equal(p1.hasMore, false);
  vm.runInContext(`_tagBrowse.session.sel = [];`, sb);
  assert.equal(sb._sessionTagFilter(pool).length, 300, 'clearing the selection restores the pool');
});

// ── end-to-end render in all three modes ────────────────────────────────────

test('a 300-session view stays one paint per page in every mode', () => {
  const { viewEl, el } = viewElement();
  const sb = loadApp({ byId: { view: el } });
  sb.innerWidth = 1440;
  setSessions(sb, makeSessions(300));
  setRegistry(sb);
  vm.runInContext(`state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionFilter = ''; state.sessionTreeView = false; state._filtersCollapsed = true;`, sb);
  let html = '';
  vm.runInContext('state.sessionsViewMode = "list"; state._sessionsPageKey = null; state._sessionsPageLimit = 60;', sb);
  sb.renderSessionsView();
  html = viewEl.capture;
  assert.equal((html.match(/class="session-card /g) || []).length, 60);
  assert.equal(countTiles(html), 0, 'list mode keeps zero tiles');
  assert.match(html, />Show more \(60\/300\)/);
  assert.ok(!html.includes('sessions-summary'), 'no state count line above the list');
  vm.runInContext('state.sessionsViewMode = "compact"; state._sessionsPageKey = null; state._sessionsPageLimit = 60;', sb);
  sb.renderSessionsView();
  html = viewEl.capture;
  assert.equal(countTiles(html), 60, 'compact mode draws page one as tiles');
});
