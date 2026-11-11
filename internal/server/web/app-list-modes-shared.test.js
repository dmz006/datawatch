// Shared list-mode helpers: grouping, state mix, collapse memory and paging.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function load() {
  const sandbox = buildSandbox({ byId: {} });
  const store = {};
  sandbox.localStorage = {
    getItem: (k) => (k in store ? store[k] : null),
    setItem: (k, v) => { store[k] = String(v); },
    removeItem: (k) => { delete store[k]; },
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);
const items = [
  { id: 1, st: 'running', tags: ['stage:build', 'lane:a'], node: 'n1', project: 'p1' },
  { id: 2, st: 'running', tags: ['stage:build'], node: 'n1', project: 'p2' },
  { id: 3, st: 'done', tags: ['stage:test'], node: 'n2', project: 'p1' },
  { id: 4, st: 'failed', tags: [], node: '', project: '' },
];
const ctx = { tags: (i) => i.tags, state: (i) => i.st, node: (i) => i.node, project: (i) => i.project };
const group = (sb, by) => {
  sb.__items = items; sb.__ctx = ctx; sb.__by = by;
  return JSON.parse(JSON.stringify(run(sb, '_listGroupBy(__items, __by, __ctx)').map(g => ({ key: g.key, ids: g.items.map(i => i.id), count: g.count, mix: g.mix }))));
};

test('mode: viewport default and remembered choice', () => {
  const sb = load();
  assert.equal(run(sb, '_listModeDefault(1000)'), 'tiles');
  assert.equal(run(sb, '_listModeDefault(500)'), 'compact');
  assert.equal(run(sb, '_listModeDefault(300)'), 'list');
  assert.equal(run(sb, "_listModeGet('s', 300)"), 'list');
  assert.equal(run(sb, "_listModeSet('s', 'tiles')"), true);
  assert.equal(run(sb, "_listModeGet('s', 300)"), 'tiles');
  assert.equal(run(sb, "_listModeSet('s', 'bogus')"), false);
  assert.equal(run(sb, "_listModeGet('other', 1000)"), 'tiles');
});

test('group by tag prefix, state, node, project', () => {
  const sb = load();
  const p = group(sb, 'prefix:stage');
  assert.deepEqual(p.map(g => [g.key, g.ids]), [['stage:build', [1, 2]], ['stage:test', [3]], ['', [4]]]);
  assert.deepEqual(group(sb, 'state').map(g => g.key), ['done', 'failed', 'running']);
  assert.deepEqual(group(sb, 'node').map(g => [g.key, g.ids]), [['n1', [1, 2]], ['n2', [3]], ['', [4]]]);
  assert.deepEqual(group(sb, 'project').map(g => g.key), ['p1', 'p2', '']);
  const t = group(sb, 'tag');
  assert.deepEqual(t.find(g => g.key === 'lane:a').ids, [1]);
  assert.equal(group(sb, 'none').length, 1);
});

test('state mix per group', () => {
  const sb = load();
  const g = group(sb, 'prefix:stage');
  assert.deepEqual(g[0].mix, { running: 2 });
  sb.__mix = { running: 3, done: 1, failed: 1 };
  assert.equal(run(sb, '_listStateMixText(__mix)'), 'running 3 · done 1 · failed 1');
});

test('collapse memory persists per scope', () => {
  const sb = load();
  assert.equal(run(sb, "_listIsCollapsed('a','x')"), false);
  assert.equal(run(sb, "_listToggleCollapsed('a','x')"), true);
  assert.equal(run(sb, "_listIsCollapsed('a','x')"), true);
  assert.equal(run(sb, "_listIsCollapsed('b','x')"), false);
  assert.equal(run(sb, "_listToggleCollapsed('a','x')"), false);
  assert.equal(run(sb, "_listIsCollapsed('a','x')"), false);
});

test('show-more paging', () => {
  const sb = load();
  sb.__many = Array.from({ length: 120 }, (_, i) => i);
  assert.equal(run(sb, "_listShownCount('s','g')"), 50);
  let pg = run(sb, "_listPage(__many, _listShownCount('s','g'))");
  assert.equal(pg.items.length, 50); assert.equal(pg.remaining, 70); assert.equal(pg.hasMore, true);
  run(sb, "_listShowMore('s','g')"); run(sb, "_listShowMore('s','g')");
  pg = run(sb, "_listPage(__many, _listShownCount('s','g'))");
  assert.equal(pg.items.length, 120); assert.equal(pg.hasMore, false);
  run(sb, "_listResetPaging('s')");
  assert.equal(run(sb, "_listShownCount('s','g')"), 50);
});

// ── UI wiring: the body, toolbar and sessions groups use the shared helpers ──
const tileOf = (it) => `<i${it.id}>`;
test('body: group-by prefix, collapse hides tiles, paging adds a show-more button', () => {
  const sb = load();
  sb.__items = items.map(i => ({ id: i.id, status: i.st, tags: [] }));
  sb.__tile = tileOf;
  // none: one grid, no header
  let html = run(sb, '_listBodyHTML("automata", __items, __tile)');
  assert.ok(!html.includes('list-group-header'));
  assert.equal((html.match(/<i\d>/g) || []).length, 4);
  // state grouping: header per state with counts and the mix
  run(sb, '_listGroupSet("automata", "state")');
  html = run(sb, '_listBodyHTML("automata", __items, __tile)');
  assert.equal((html.match(/list-group-header/g) || []).length, 3);
  // collapsed group draws header only
  run(sb, '_listToggleCollapsed("automata", "running")');
  html = run(sb, '_listBodyHTML("automata", __items, __tile)');
  assert.equal((html.match(/<i\d>/g) || []).length, 2);
  // invalid axis is refused and the old one is kept
  assert.equal(run(sb, '_listGroupSet("automata", "bogus")'), false);
  assert.equal(run(sb, '_listGroupGet("automata")'), 'state');
  assert.equal(run(sb, '_listGroupValid("prefix:lane")'), true);
  assert.equal(run(sb, '_listGroupValid("prefix:nope")'), false);
  // paging
  sb.__many = Array.from({ length: 120 }, (_, i) => ({ id: i, status: 'x', tags: [] }));
  run(sb, '_listGroupSet("automation", "none")');
  html = run(sb, '_listBodyHTML("automation", __many, __tile)');
  assert.equal((html.match(/<i\d+>/g) || []).length, 50);
  assert.match(html, /listShowMore\('automation',''\)/);
});

test('toolbar: remembered mode drives the grid and a resize leaves it alone', () => {
  const sb = load();
  sb.innerWidth = 1000;
  assert.match(run(sb, '_tileGridHTML("x", "automata")'), /data-tile-mode="tile"/);
  run(sb, '_listModeSet("automata", "list")');
  const g = run(sb, '_tileGridHTML("x", "automata")');
  assert.match(g, /data-tile-mode="line" data-tile-chosen="1"/);
  assert.match(run(sb, '_listControlsHTML("automata")'), /listSetMode\('automata','compact'\)/);
});

test('wiring: sessions groups and the tag browser call the shared helpers', () => {
  const src = require('fs').readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /_listGroupHeaderHTML\('session', g, 'tag'/);
  assert.match(src, /_listPage\(g\.items, _listShownCount\('session'/);
  assert.match(src, /_listControlsHTML\(scope\) : ''/);
  assert.ok(!src.includes('function _sessionGroupHeaderHTML'));
});
