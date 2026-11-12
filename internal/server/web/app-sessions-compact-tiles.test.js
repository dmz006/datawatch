// Sessions Compact view mode renders the same tile grid as Tiles mode at a
// smaller size: one work-tile per session inside a session-tile-grid-compact
// grid whose data-tile-mode is a tile mode (never compact or line), also inside
// groups and after a resize sync. List mode keeps its own rows.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const NOW = Date.parse('2026-10-25T12:00:00Z');

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}
function makeSessions(n) {
  const out = [];
  for (let i = 0; i < n; i++) {
    out.push({
      id: 'cs' + i, full_id: 'local/cs' + i, name: 'job ' + i, task: 'task ' + i,
      state: 'running', backend_family: 'claude-code', llm_ref: 'opus',
      hostname: 'node-' + (i % 2),
      updated_at: new Date(NOW - i * 60000).toISOString(),
      created_at: new Date(NOW - i * 60000 - 1000).toISOString(),
    });
  }
  return out;
}
const count = (html, needle) => html.split(needle).length - 1;
const gridMode = html => {
  const m = html.match(/class="work-tile-grid session-tile-grid session-tile-grid-compact" data-tile-mode="([^"]*)"/);
  return m && m[1];
};

test('compact mode draws one work-tile per session in a non-row tile grid', () => {
  const sb = loadApp();
  const list = makeSessions(5);
  for (const w of [1280, 500, 400]) {
    sb.innerWidth = w;
    const html = sb.renderSessionTileGrid(list.map(s => sb.renderSessionTile(s, NOW)).join(''), 'compact');
    assert.equal(count(html, 'data-sess-id="'), 5, 'one tile per session at width ' + w);
    const mode = gridMode(html);
    assert.ok(mode, 'compact grid class present');
    assert.notEqual(mode, 'compact');
    assert.notEqual(mode, 'line');
  }
});

test('grouped compact render keeps the tile grid mode', () => {
  const sb = loadApp();
  sb.innerWidth = 400;
  const html = sb.renderSessionsGrouped(makeSessions(6), { axis: 'node' }, NOW, 'compact');
  const grids = html.match(/<div class="work-tile-grid[^"]*session-tile-grid-compact"[^>]*>/g) || [];
  assert.equal(grids.length, 2, 'one compact grid per node group');
  for (const g of grids) assert.doesNotMatch(g, /data-tile-mode="(compact|line)"/);
  assert.equal(count(html, 'data-sess-id="'), 6);
});

test('compact grid is distinct from the tiles grid and list mode keeps its rows', () => {
  const sb = loadApp();
  sb.innerWidth = 1280;
  const s = makeSessions(2);
  const inner = s.map(x => sb.renderSessionTile(x, NOW)).join('');
  const tiles = sb.renderSessionTileGrid(inner, 'tiles');
  const compact = sb.renderSessionTileGrid(inner, 'compact');
  assert.ok(!tiles.includes('session-tile-grid-compact'));
  assert.ok(compact.includes('session-tile-grid-compact'));
  const list = sb.renderSessionsGrouped(s, { axis: 'node' }, NOW, 'list');
  assert.ok(!list.includes('session-tile-grid'), 'list mode is not a tile grid');
  assert.ok(list.includes('state-running'), 'list mode keeps its own rows');
});

test('resize sync never rewrites a compact tile grid to compact or line', () => {
  const sb = loadApp();
  const grids = [{ dataset: { tileMode: 'tile' }, classList: { contains: c => c === 'session-tile-grid-compact' } }];
  sb.document.querySelectorAll = sel => (sel === '.work-tile-grid' ? grids : []);
  for (const w of [500, 400, 1280]) {
    sb.innerWidth = w;
    sb._tileSyncModes();
    assert.equal(grids[0].dataset.tileMode, 'tile');
  }
});
