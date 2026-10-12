// Plugin widget dashboard cards (BL403 Phase 4): each widget is its own
// layout entry 'plugin:<plugin>:<widget>'. Covers the card shell markup
// (escaping of plugin-author ids, built-in cards unchanged), the
// fetch/render cache (in-flight guard, refresh interval, disabled/missing
// plugin, fetch failure, non-dashboard surface, render-on-change), the
// Add Card panel's plugin section, and _dashAddCard's plugin branch.
// Run: `node --test internal/server/web/*.test.js`.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function fakeCardEl(id) {
  const writes = [];
  let html = '';
  return {
    writes,
    getAttribute(n) { return n === 'data-plugin-card' ? id : null; },
    get innerHTML() { return html; },
    set innerHTML(v) { html = v; writes.push(v); },
  };
}

function setup(cardIds) {
  const els = (cardIds || []).map(fakeCardEl);
  const grid = { querySelectorAll() { return els; }, innerHTML: '' };
  const sandbox = buildSandbox({ byId: { dashCardGrid: grid } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const calls = [];
  const pending = [];
  sandbox.__routes = {};
  sandbox.__apiFetch = (p) => {
    calls.push(p);
    const r = sandbox.__routes[p];
    let pr;
    if (typeof r === 'function') pr = r();
    else if (r === undefined) pr = Promise.reject(new Error('404'));
    else pr = Promise.resolve(JSON.parse(JSON.stringify(r)));
    pending.push(pr.catch(() => {}));
    return pr;
  };
  vm.runInContext('apiFetch = (...a) => __apiFetch(...a);', sandbox);
  const run = (code) => vm.runInContext(code, sandbox);
  const settle = async () => { while (pending.length) await pending.shift(); await new Promise(r => setImmediate(r)); };
  return { sandbox, els, calls, run, settle };
}

const CARD = 'plugin:demo:queue';
const WURL = '/api/plugins/demo/widgets/queue';
const ENABLED = { plugins: [{ name: 'demo', enabled: true, widgets: [{ id: 'queue', surface: 'dashboard', kind: 'stat_tile' }] }] };
function view(over) {
  return Object.assign({ id: 'queue', surface: 'dashboard', kind: 'stat_tile', data_mode: 'push', interval_seconds: 0,
    value: { plugin: 'demo', widget_id: 'queue', data: { value: 7, label: 'Queued' }, updated_at: '2026-10-12T00:00:00Z', source: 'push' } }, over || {});
}

// ── card shell ──────────────────────────────────────────────────────────────

test('_dashCardHTML: plugin card shell with escaped ids and loading body', () => {
  const { run, sandbox } = setup();
  sandbox.__e = { id: "plugin:ev'il<b>:w\\'x\"y", cs: 4 };
  const html = run('_dashCardHTML(__e)');
  assert.ok(html.includes('class="dboard-card"'));
  assert.ok(html.includes('dboard-card-hdr') && html.includes('dboard-card-body'));
  assert.ok(html.includes('data-cs="4"') && html.includes('data-rs="1"'));
  assert.ok(html.includes('draggable="true"') && html.includes('window._dashDragStart(event)'));
  assert.ok(html.includes('dboard-plugin-widget'));
  assert.ok(html.includes('plugin_widget_loading'), 'loading empty state in body');
  assert.ok(!html.includes('<b>'), 'no raw markup from plugin name');
  // data-card-id round-trips through escHtml.
  assert.ok(html.includes('data-card-id="plugin:ev&#39;il&lt;b&gt;:w\\&#39;x&quot;y"'));
  // onclick args use escJsAttr: backslash doubled, quote backslash-escaped.
  const jsId = run('escJsAttr(__e.id)');
  assert.ok(html.includes(`window._dashRemoveCard('${jsId}')`));
  assert.ok(html.includes(`window._dashCycleSpan('${jsId}')`));
  assert.ok(html.includes(`window._dashCycleRows('${jsId}')`));
});

test('_dashCardHTML: plugin card defaults to 3 wide; built-in cards unchanged', () => {
  const { run } = setup();
  assert.ok(run(`_dashCardHTML({ id: '${CARD}' })`).includes('data-cs="3"'));
  const tree = run(`_dashCardHTML({ id: 'tree', cs: 2 })`);
  assert.ok(tree.includes(`data-card-id="tree"`) && tree.includes(`window._dashRemoveCard('tree')`));
  assert.ok(!tree.includes('dboard-plugin-widget'));
  assert.equal(run(`_dashCardHTML({ id: 'nope' })`), '');
});

// ── fetch + render ──────────────────────────────────────────────────────────

test('renders widget once data arrives, never stacks requests, repaints only on change', async () => {
  const { run, els, calls, settle, sandbox } = setup([CARD]);
  sandbox.__routes['/api/plugins'] = ENABLED;
  sandbox.__routes[WURL] = view();
  run('_dashRenderPluginWidgets()');
  run('_dashRenderPluginWidgets()');
  run('_dashRenderPluginWidgets()');
  assert.equal(calls.filter(c => c === WURL).length, 1, 'in-flight guard');
  assert.equal(calls.filter(c => c === '/api/plugins').length, 1);
  assert.ok(els[0].innerHTML.includes('plugin_widget_loading'));
  await settle();
  run('_dashRenderPluginWidgets()');
  assert.ok(els[0].innerHTML.includes('Queued'), els[0].innerHTML);
  const n = els[0].writes.length;
  run('_dashRenderPluginWidgets(); _dashRenderPluginWidgets();');
  assert.equal(els[0].writes.length, n, 'no churn when updated_at unchanged');
  assert.equal(calls.filter(c => c === WURL).length, 1, 'cached within refresh window');
  // Age the cache past the 30s default: refetch, repaint on new updated_at.
  run(`for (const k in _dash._pluginWidgets) _dash._pluginWidgets[k].fetchedAt -= 31000;`);
  sandbox.__routes[WURL] = view({ value: { data: { value: 9, label: 'Later' }, updated_at: '2026-10-12T00:01:00Z' } });
  run('_dashRenderPluginWidgets()');
  await settle();
  run('_dashRenderPluginWidgets()');
  assert.equal(calls.filter(c => c === WURL).length, 2);
  assert.ok(els[0].innerHTML.includes('Later'));
});

test('refresh interval honours interval_seconds with a 10s floor', () => {
  const { run } = setup();
  const stale = (iv, age) => run(`_dashPluginWidgetStale({ view: { interval_seconds: ${iv} }, fetchedAt: Date.now() - ${age} })`);
  assert.equal(stale(1, 5000), false);
  assert.equal(stale(1, 10001), true);
  assert.equal(stale(120, 60000), false);
  assert.equal(stale(120, 121000), true);
  assert.equal(stale(0, 29000), false);
  assert.equal(run('_dashPluginWidgetStale(undefined)'), true);
});

for (const [label, list] of [
  ['disabled plugin', { plugins: [{ name: 'demo', enabled: false }] }],
  ['missing plugin', { plugins: [{ name: 'other', enabled: true }] }],
  ['plugin list failure', undefined],
]) {
  test(`unavailable for ${label}`, async () => {
    const { run, els, settle, sandbox } = setup([CARD]);
    if (list) sandbox.__routes['/api/plugins'] = list;
    sandbox.__routes[WURL] = view();
    run('_dashRenderPluginWidgets()');
    await settle();
    run('_dashRenderPluginWidgets()');
    assert.ok(els[0].innerHTML.includes('plugin_widget_unavailable'), els[0].innerHTML);
  });
}

test('unavailable on widget fetch rejection (404/503) and on a throwing apiFetch', async () => {
  const { run, els, settle, sandbox } = setup([CARD, 'plugin:demo:other']);
  sandbox.__routes['/api/plugins'] = ENABLED;
  sandbox.__routes[WURL] = () => Promise.reject(new Error('503'));
  run('_dashRenderPluginWidgets()');
  await settle();
  run('_dashRenderPluginWidgets()');
  assert.ok(els[0].innerHTML.includes('plugin_widget_unavailable'));
  assert.ok(els[1].innerHTML.includes('plugin_widget_unavailable'));
  // A synchronous throw must not escape the render tick.
  run(`_dash._pluginWidgets = {}; apiFetch = () => { throw new Error('boom'); };`);
  assert.doesNotThrow(() => run('_dashRenderPluginWidgets()'));
});

test('unavailable when the widget surface is not dashboard', async () => {
  const { run, els, settle, sandbox } = setup([CARD]);
  sandbox.__routes['/api/plugins'] = ENABLED;
  sandbox.__routes[WURL] = view({ surface: 'session' });
  run('_dashRenderPluginWidgets()');
  await settle();
  run('_dashRenderPluginWidgets()');
  assert.ok(els[0].innerHTML.includes('plugin_widget_unavailable'));
});

test('null value renders the no-data state', async () => {
  const { run, els, settle, sandbox } = setup([CARD]);
  sandbox.__routes['/api/plugins'] = ENABLED;
  sandbox.__routes[WURL] = view({ value: null });
  run('_dashRenderPluginWidgets()');
  await settle();
  run('_dashRenderPluginWidgets()');
  assert.ok(els[0].innerHTML.includes('plugin_widget_no_data'));
});

// ── Add Card panel + _dashAddCard ───────────────────────────────────────────

test('_dashAddPanelPluginHTML lists enabled dashboard widgets, escaped, marks used', () => {
  const { run, sandbox } = setup();
  sandbox.__pl = [
    { name: "x'<i>", enabled: true, widgets: [{ id: 'w"1', surface: 'dashboard' }, { id: 'sess', surface: 'session' }] },
    { name: 'off', enabled: false, widgets: [{ id: 'a', surface: 'dashboard' }] },
    { name: 'demo', enabled: true, widgets: [{ id: 'queue', surface: 'dashboard' }] },
    { name: 'none', enabled: true },
  ];
  const html = run(`_dashAddPanelPluginHTML(__pl, ['${CARD}'])`);
  assert.ok(html.includes('dash_plugin_widgets_heading'));
  assert.ok(!html.includes('<i>'));
  assert.ok(html.includes('x&#39;&lt;i&gt; · w&quot;1'));
  assert.ok(!html.includes('sess') && !html.includes('off ·'));
  const jsId = run(`escJsAttr(_pwCardId("x'<i>", 'w"1'))`);
  assert.ok(html.includes(`window._dashAddCard('${jsId}')`));
  // Already-placed card is marked used and has no onclick.
  assert.equal((html.match(/dboard-add-used/g) || []).length, 1);
  assert.ok(!html.includes(`_dashAddCard('${CARD}')`));
  assert.equal(run(`_dashAddPanelPluginHTML([{ name: 'off', enabled: false, widgets: [{ id: 'a', surface: 'dashboard' }] }], [])`), '');
  assert.equal(run('_dashAddPanelPluginHTML(undefined, [])'), '');
});

test('_dashAddCard pushes a plugin card at width 3 (once)', () => {
  const { run } = setup();
  run(`_dash._editing = true; _dash._layout = [{ id: 'tree', cs: 2 }];`);
  run(`window._dashAddCard('${CARD}'); window._dashAddCard('${CARD}');`);
  assert.equal(run('JSON.stringify(_dash._layout)'), JSON.stringify([{ id: 'tree', cs: 2 }, { id: CARD, cs: 3 }]));
  run(`window._dashAddCard('plugin:bad')`);
  assert.equal(run('_dash._layout.length'), 2);
});
