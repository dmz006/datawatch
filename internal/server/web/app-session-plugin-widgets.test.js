// Session-surface plugin widgets (BL403): refreshSessionPluginWidgets renders
// global session widgets into #sessionPluginWidgets in the session detail
// view. Covers the hidden/empty cases, one request per plugin, disabled
// plugin + non-session widget filtering, header escaping, keyed in-place
// updates, per-plugin failure → unavailable, list failure → hidden, the
// throttle/in-flight guard, and the module-level cache on session switch.
// Run: `node --test internal/server/web/*.test.js`.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

// Minimal keyed-DOM fake: attributes, ordered children, appendChild moves,
// and a stable body element per tile for querySelector.
function fakeEl() {
  const attrs = {};
  const el = {
    style: {}, children: [], className: '', writes: [],
    setAttribute(k, v) { attrs[k] = String(v); },
    getAttribute(k) { return Object.prototype.hasOwnProperty.call(attrs, k) ? attrs[k] : null; },
    appendChild(c) {
      const i = el.children.indexOf(c);
      if (i >= 0) el.children.splice(i, 1);
      el.children.push(c);
      return c;
    },
    removeChild(c) {
      const i = el.children.indexOf(c);
      if (i >= 0) el.children.splice(i, 1);
      return c;
    },
    querySelector(sel) {
      if (sel !== '.session-plugin-widget-body') return null;
      return el._body || (el._body = fakeEl());
    },
  };
  let html = '';
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; el.writes.push(v); if (v === '') el.children.length = 0; },
  });
  return el;
}

function setup() {
  const byId = { sessionPluginWidgets: fakeEl() };
  const sandbox = buildSandbox({ byId });
  sandbox.document.createElement = () => fakeEl();
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
  const settle = async () => {
    for (let i = 0; i < 5; i++) {
      while (pending.length) await pending.shift();
      await new Promise(r => setImmediate(r));
    }
  };
  // Rewind the throttle so the next refresh fetches again.
  const rewind = () => run(`for (const k of Object.keys(window._sessionPluginWidgetCache)) window._sessionPluginWidgetCache[k].fetchedAt = 0;`);
  return { sandbox, byId, calls, run, settle, rewind };
}

function view(id, over) {
  return Object.assign({ id, surface: 'session', kind: 'stat_tile', data_mode: 'push', interval_seconds: 0,
    value: { plugin: 'demo', widget_id: id, data: { value: 7, label: 'Queued' }, updated_at: '2026-10-12T00:00:00Z', source: 'push' } }, over || {});
}

const LIST = { plugins: [
  { name: 'demo', enabled: true, widgets: [
    { id: 'queue', surface: 'session', kind: 'stat_tile' },
    { id: 'dash', surface: 'dashboard', kind: 'stat_tile' },
  ] },
  { name: 'off', enabled: false, widgets: [{ id: 'x', surface: 'session', kind: 'list' }] },
  { name: 'nowidgets', enabled: true },
] };

test('no session widgets → container stays hidden, only the list is fetched', async () => {
  const { sandbox, byId, calls, run, settle } = setup();
  sandbox.__routes['/api/plugins'] = { plugins: [{ name: 'demo', enabled: true, widgets: [{ id: 'd', surface: 'dashboard', kind: 'stat_tile' }] }] };
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.deepEqual(calls, ['/api/plugins']);
  assert.equal(byId.sessionPluginWidgets.style.display, 'none');
  assert.equal(byId.sessionPluginWidgets.children.length, 0);
});

test('renders one tile per session widget, one request per enabled plugin', async () => {
  const { sandbox, byId, calls, run, settle } = setup();
  sandbox.__routes['/api/plugins'] = LIST;
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [view('queue'), view('dash', { surface: 'dashboard' })] };
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.deepEqual(calls, ['/api/plugins', '/api/plugins/demo/widgets']);
  const box = byId.sessionPluginWidgets;
  assert.equal(box.style.display, '');
  assert.equal(box.children.length, 1);
  const tile = box.children[0];
  assert.equal(tile.getAttribute('data-plugin-widget'), 'plugin:demo:queue');
  assert.match(tile.innerHTML, /🧩 queue/);
  assert.match(tile._body.innerHTML, />7</);
});

test('plugin-supplied ids are escaped in the tile header', async () => {
  const { sandbox, byId, run, settle } = setup();
  const evil = '<img src=x onerror=alert(1)>';
  sandbox.__routes['/api/plugins'] = { plugins: [{ name: 'demo', enabled: true, widgets: [{ id: evil, surface: 'session', kind: 'stat_tile' }] }] };
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [view(evil)] };
  run('refreshSessionPluginWidgets()');
  await settle();
  const tile = byId.sessionPluginWidgets.children[0];
  assert.ok(!tile.innerHTML.includes('<img'), tile.innerHTML);
  assert.match(tile.innerHTML, /&lt;img/);
});

test('refresh updates the same tile in place; body re-renders only on change', async () => {
  const { sandbox, byId, run, settle, rewind } = setup();
  sandbox.__routes['/api/plugins'] = LIST;
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [view('queue')] };
  run('refreshSessionPluginWidgets()');
  await settle();
  const tile = byId.sessionPluginWidgets.children[0];
  const n = tile._body.writes.length;
  rewind();
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.equal(byId.sessionPluginWidgets.children[0], tile);
  assert.equal(tile._body.writes.length, n, 'unchanged value must not re-render');
  const v2 = view('queue');
  v2.value.data.value = 9;
  v2.value.updated_at = '2026-10-12T00:01:00Z';
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [v2] };
  rewind();
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.equal(byId.sessionPluginWidgets.children[0], tile);
  assert.match(tile._body.innerHTML, />9</);
});

test('per-plugin failure and missing widget → unavailable tiles', async () => {
  const { sandbox, byId, run, settle } = setup();
  sandbox.__routes['/api/plugins'] = { plugins: [
    { name: 'a', enabled: true, widgets: [{ id: 'w', surface: 'session', kind: 'stat_tile' }] },
    { name: 'b', enabled: true, widgets: [{ id: 'gone', surface: 'session', kind: 'stat_tile' }] },
  ] };
  // a → no route (404); b → widget absent from response.
  sandbox.__routes['/api/plugins/b/widgets'] = { plugin: 'b', widgets: [] };
  run('refreshSessionPluginWidgets()');
  await settle();
  const tiles = byId.sessionPluginWidgets.children;
  assert.equal(tiles.length, 2);
  for (const t of tiles) assert.match(t._body.innerHTML, /plugin_widget_unavailable|Widget unavailable/);
});

test('list failure hides the container', async () => {
  const { sandbox, byId, run, settle, rewind } = setup();
  sandbox.__routes['/api/plugins'] = LIST;
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [view('queue')] };
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.equal(byId.sessionPluginWidgets.children.length, 1);
  sandbox.__routes['/api/plugins'] = () => Promise.reject(new Error('503'));
  rewind();
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.equal(byId.sessionPluginWidgets.style.display, 'none');
  assert.equal(byId.sessionPluginWidgets.children.length, 0);
});

test('throttle and in-flight guard: repeated ticks do not refetch', async () => {
  const { sandbox, calls, run, settle } = setup();
  sandbox.__routes['/api/plugins'] = LIST;
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [view('queue')] };
  run('refreshSessionPluginWidgets(); refreshSessionPluginWidgets();');
  await settle();
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.equal(calls.filter(c => c === '/api/plugins').length, 1);
});

test('session switch paints instantly from the global cache', async () => {
  const { sandbox, byId, calls, run, settle } = setup();
  sandbox.__routes['/api/plugins'] = LIST;
  sandbox.__routes['/api/plugins/demo/widgets'] = { plugin: 'demo', widgets: [view('queue')] };
  run('refreshSessionPluginWidgets()');
  await settle();
  const before = calls.length;
  // A new session detail render produces a fresh, empty container.
  byId.sessionPluginWidgets = fakeEl();
  byId.sessionPluginWidgets.style.display = 'none';
  run('refreshSessionPluginWidgets()');
  assert.equal(calls.length, before, 'throttled — no fetch');
  assert.equal(byId.sessionPluginWidgets.children.length, 1);
  assert.match(byId.sessionPluginWidgets.children[0]._body.innerHTML, />7</);
});

test('no container (navigated away) → no fetch, no throw', async () => {
  const { sandbox, calls, run, settle } = setup();
  sandbox.document.getElementById = () => null;
  run('refreshSessionPluginWidgets()');
  await settle();
  assert.equal(calls.length, 0);
});

test('session detail template carries the container after statsPanel', () => {
  const src = require('fs').readFileSync(APP_JS, 'utf8');
  assert.match(src, /id="statsPanel"[^\n]*\n\s*<div id="sessionPluginWidgets" class="session-plugin-widgets" style="display:none;"><\/div>/);
});
