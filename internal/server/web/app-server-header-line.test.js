// Stable server header line: #server-header-line reads "Showing: <name>" on
// every page. Peer selected -> peer name; null/'local' -> local hostname;
// 'all' -> all-servers label. navigate() (the shared shell) must render it
// for every page route so no page can omit it.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadApp() {
  const sandbox = buildSandbox();
  // Page renders fire fetches we do not care about; never resolve them.
  sandbox.fetch = () => new Promise(() => {});
  // Minimal parent/#view pair so the header line can be inserted and found.
  const made = {};
  const parent = {
    insertBefore(el) { made.line = el; },
  };
  const viewEl = { parentNode: parent, classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } }, style: {}, innerHTML: '' };
  const realGet = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => {
    if (id === 'view') return viewEl;
    if (id === 'server-header-line') return made.line || null;
    return realGet(id);
  };
  sandbox.document.createElement = () => {
    const attrs = {};
    return { style: {}, textContent: '', id: '', className: '',
      setAttribute(k, v) { attrs[k] = v; }, getAttribute(k) { return attrs[k]; } };
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.renderServerHeaderLine, 'function');
  sandbox.window._i18n.fallback = JSON.parse(require('fs').readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.window._i18n.bundle = {};
  sandbox.__made = made;
  return sandbox;
}

function setServer(sandbox, name, host) {
  vm.runInContext(`state.activeServer = ${JSON.stringify(name)}; _localHostname = ${JSON.stringify(host)};`, sandbox);
}

test('line shows the peer name for a selected peer', () => {
  const sb = loadApp();
  setServer(sb, 'pi-node', 'myhost');
  const el = sb.renderServerHeaderLine();
  assert.equal(el.id, 'server-header-line');
  assert.ok(el.textContent.includes('pi-node'), el.textContent);
  assert.ok(el.textContent.startsWith('Showing:'), el.textContent);
  assert.equal(el.getAttribute('data-server'), 'pi-node');
});

test('line shows the local hostname for null and local', () => {
  for (const v of [null, 'local']) {
    const sb = loadApp();
    setServer(sb, v, 'myhost');
    const el = sb.renderServerHeaderLine();
    assert.ok(el.textContent.includes('myhost'), el.textContent);
    assert.equal(el.getAttribute('data-server'), 'myhost');
  }
});

test('all mode shows the all-servers label', () => {
  const sb = loadApp();
  setServer(sb, 'all', 'myhost');
  const el = sb.renderServerHeaderLine();
  assert.ok(el.textContent.includes('All'), el.textContent);
  assert.ok(!el.textContent.includes('myhost'));
});

test('navigate renders the line for every page route', () => {
  const routes = ['sessions', 'new', 'settings', 'alerts', 'autonomous', 'dashboard', 'observer', 'plugins'];
  for (const route of routes) {
    const sb = loadApp();
    setServer(sb, 'pi-node', 'myhost');
    try { sb.navigate(route, null, true); } catch (_) { /* page body may need a fuller DOM; the shell ran first */ }
    const el = sb.__made.line;
    assert.ok(el, `no server header line for route ${route}`);
    assert.ok(el.textContent.includes('pi-node'), `route ${route}: ${el.textContent}`);
  }
});

test('settings tab Showing hook also refreshes the line', () => {
  const sb = loadApp();
  setServer(sb, 'peer-b', 'myhost');
  sb.settingsRefreshShowingLines();
  assert.ok(sb.__made.line.textContent.includes('peer-b'));
});

test('rendering with a cached local hostname does not self-reschedule', () => {
  const sb = loadApp();
  const queue = [];
  sb.setTimeout = (fn) => { queue.push(fn); return queue.length; };
  setServer(sb, null, 'myhost');
  sb.renderServerHeaderLine();
  let rounds = 0;
  while (queue.length && rounds < 50) { queue.splice(0).forEach(f => f()); rounds++; }
  assert.equal(queue.length, 0, 'timer loop must terminate');
  assert.ok(rounds <= 1);
});

test('all mode sets data-server to all', () => {
  const sb = loadApp();
  setServer(sb, 'all', 'myhost');
  assert.equal(sb.renderServerHeaderLine().getAttribute('data-server'), 'all');
});
