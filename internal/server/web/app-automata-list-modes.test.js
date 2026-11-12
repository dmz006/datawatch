// Automata list: list/tiles/compact toggle and group-by through the shared helpers.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
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
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);
const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

const prds = [
  { id: 'a1', status: 'running', node: 'n1', project_dir: 'p1' },
  { id: 'a2', status: 'running', node: 'n2', project_dir: 'p1' },
  { id: 'a3', status: 'done', node: 'n1', project_dir: 'p2' },
];
const tile = (p) => `<i data-id="${p.id}"></i>`;

test('Automata list draws its toolbar and body through the shared helpers', () => {
  assert.match(src, /_tagBrowseMountHTML\('automata', _automataFilteredList\(\)/);
  assert.match(src, /scope === 'automata' \|\| scope === 'automation'\) \? _listControlsHTML\(scope\)/);
});

test('toolbar has list, tiles, compact and a group-by select for automata', () => {
  const sb = load();
  const html = run(sb, "_listControlsHTML('automata')");
  assert.match(html, /listSetMode\('automata','list'\)/);
  assert.match(html, /listSetMode\('automata','tiles'\)/);
  assert.match(html, /listSetMode\('automata','compact'\)/);
  for (const by of ['tag', 'state', 'node', 'project']) assert.ok(html.includes(`value="${by}"`), by);
  assert.match(html, /value="prefix:stage"/);
});

test('mode choice sets the existing grid mode, no new grid', () => {
  const sb = load();
  sb.__p = prds;
  run(sb, "_listModeSet('automata','compact')");
  let html = run(sb, "_listBodyHTML('automata', __p, (p)=>'<i data-id=\"'+p.id+'\"></i>')");
  assert.match(html, /class="work-tile-grid" data-tile-mode="compact" data-tile-chosen="1"/);
  run(sb, "_listModeSet('automata','list')");
  html = run(sb, "_listBodyHTML('automata', __p, (p)=>'x')");
  assert.match(html, /data-tile-mode="line"/);
  assert.ok(!/automata-grid/.test(html));
});

test('group by state makes one grid per group with headers; collapse hides tiles', () => {
  const sb = load();
  sb.__p = prds;
  run(sb, "_listGroupSet('automata','state')");
  const r = "(p)=>'<i data-id=\"'+p.id+'\"></i>'";
  let html = run(sb, `_listBodyHTML('automata', __p, ${r})`);
  assert.equal((html.match(/class="list-group-header"/g) || []).length, 2);
  assert.equal((html.match(/work-tile-grid/g) || []).length, 2);
  assert.match(html, /data-list-group="running"/);
  run(sb, "_listToggleCollapsed('automata','running')");
  html = run(sb, `_listBodyHTML('automata', __p, ${r})`);
  assert.ok(!html.includes('data-id="a1"'));
  assert.ok(html.includes('data-id="a3"'));
  assert.match(html, /aria-expanded="false"/);
});

test('group by node and project use automaton fields', () => {
  const sb = load();
  sb.__p = prds;
  const keys = (by) => { sb.__by = by; return JSON.parse(JSON.stringify(run(sb, '_listGroupBy(__p, __by, _listCtx("automata")).map(g=>g.key)'))); };
  assert.deepEqual(keys('node'), ['n1', 'n2']);
  assert.deepEqual(keys('project'), ['p1', 'p2']);
  assert.deepEqual(keys('state'), ['done', 'running']);
});

test('search and tag filter feed the same list that grouping draws', () => {
  assert.match(src, /function _automataFilteredList\(\)/);
  const sb = load();
  sb.__p = [prds[2]];
  run(sb, "_listGroupSet('automata','state')");
  const html = run(sb, "_listBodyHTML('automata', __p, (p)=>'<i data-id=\"'+p.id+'\"></i>')");
  assert.equal((html.match(/class="list-group-header"/g) || []).length, 1);
  assert.ok(!html.includes('data-id="a1"'));
});
