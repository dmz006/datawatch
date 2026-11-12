// Automation list: list/tiles/compact toggle, group-by and paging through the shared helpers.
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

const items = [
  { id: 'x1', name: 'One', status: 'active', members: [] },
  { id: 'x2', name: 'Two', status: 'active', members: [] },
  { id: 'x3', name: 'Three', status: 'draft', members: [] },
];
const render = "(a)=>buildAutomationCardHTML(a,{now:Date.now()})";

test('Automation list body goes through the shared tag browser and toolbar', () => {
  assert.match(src, /_tagBrowseMountHTML\('automation', _tagBrowseItems\('automation', base\)/);
  assert.match(src, /scope === 'automata' \|\| scope === 'automation'\) \? _listControlsHTML\(scope\)/);
});

test('toolbar offers list, tiles, compact and group-by for automation', () => {
  const sb = load();
  const html = run(sb, "_listControlsHTML('automation')");
  for (const m of ['list', 'tiles', 'compact']) assert.ok(html.includes(`listSetMode('automation','${m}')`), m);
  for (const by of ['tag', 'state']) assert.ok(html.includes(`value="${by}"`), by);
});

test('mode choice sets the tile grid mode for automation', () => {
  const sb = load();
  sb.__i = items;
  run(sb, "_listModeSet('automation','compact')");
  let html = run(sb, `_listBodyHTML('automation', __i, ${render})`);
  assert.match(html, /data-tile-mode="compact"/);
  assert.match(html, /data-automation-id="x1"/);
  run(sb, "_listModeSet('automation','tiles')");
  html = run(sb, `_listBodyHTML('automation', __i, ${render})`);
  assert.match(html, /data-tile-mode="tiles"|data-tile-mode="tile"/);
});

test('group by state makes a grid per state; collapse hides tiles', () => {
  const sb = load();
  sb.__i = items;
  run(sb, "_listGroupSet('automation','state')");
  let html = run(sb, `_listBodyHTML('automation', __i, ${render})`);
  assert.equal((html.match(/class="list-group-header"/g) || []).length, 2);
  run(sb, "_listToggleCollapsed('automation','active')");
  html = run(sb, `_listBodyHTML('automation', __i, ${render})`);
  assert.ok(!html.includes('data-automation-id="x1"'));
  assert.ok(html.includes('data-automation-id="x3"'));
});

test('long group pages with show-more', () => {
  const sb = load();
  sb.__i = Array.from({ length: 200 }, (_, i) => ({ id: 'b' + i, name: 'N' + i, status: 'active', members: [] }));
  const html = run(sb, `_listBodyHTML('automation', __i, ${render})`);
  assert.match(html, /listShowMore\('automation'/);
  assert.ok((html.match(/data-automation-id=/g) || []).length < 200);
});

test('tile shows name, state chip and open action', () => {
  const sb = load();
  const html = run(sb, "buildAutomationCardHTML({id:'z',name:'Zed',status:'active',members:[]},{now:Date.now()})");
  assert.match(html, /Zed/);
  assert.match(html, /automation-card-status|shipped/);
  assert.match(html, /work-tile-open/);
});
