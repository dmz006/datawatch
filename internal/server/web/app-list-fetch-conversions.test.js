// List fetches that load on a tab open, a reconnect or a timer ask for the
// small form: the federated sessions probe pages the summary list, the Alerts
// tab pages by a limit window, and the PRD active-session card and the boot
// alert count ask only for the rows they use.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(fetchImpl) {
  const sandbox = buildSandbox();
  sandbox.fetch = fetchImpl;
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

const json = (body) => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });

test('federated probe asks the named server for the first summary page and keeps its cursor', async () => {
  const urls = [];
  const sandbox = loadAppJS((url) => {
    urls.push(String(url));
    return json({ sessions: [{ id: 's1', full_id: 'h-s1', state: 'running' }], next_cursor: 'c1', total: 120 });
  });
  vm.runInContext(`state.activeServer = 'host-a'; onSessionsUpdated = function(){};`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  await flushAsync();
  assert.deepEqual(urls.filter(u => u.includes('/api/sessions')), ['/api/proxy/host-a/api/sessions?view=summary&limit=50']);
  assert.equal(vm.runInContext('state.sessions.length', sandbox), 1);
  assert.equal(vm.runInContext('_sl().next', sandbox), 'c1');
  assert.equal(vm.runInContext('_sl().total', sandbox), 120);
});

test('Load more on a named server follows the cursor through the same proxy prefix', async () => {
  const urls = [];
  const sandbox = loadAppJS((url) => {
    urls.push(String(url));
    const more = String(url).includes('cursor=c1');
    return json({ sessions: [{ id: more ? 's2' : 's1', full_id: more ? 'h-s2' : 'h-s1', state: 'running' }], next_cursor: more ? '' : 'c1', total: 2 });
  });
  vm.runInContext(`state.activeServer = 'host-a'; onSessionsUpdated = function(){}; renderSessionsView = function(){};`, sandbox);
  vm.runInContext(`_checkFederatedConnection('host-a')`, sandbox);
  await flushAsync();
  urls.length = 0;
  vm.runInContext('sessionsLoadMore()', sandbox);
  await flushAsync();
  assert.deepEqual(urls.filter(u => u.includes('/api/sessions')), ['/api/proxy/host-a/api/sessions?view=summary&limit=50&cursor=c1']);
  assert.equal(vm.runInContext('state.sessions.length', sandbox), 2);
  assert.equal(vm.runInContext('_sl().next', sandbox), '');
});

test('the all-servers sessions list has no Load more', () => {
  const sandbox = loadAppJS(() => json({}));
  vm.runInContext(`state.activeServer = 'all'; _sl().next = 'stale'; _sl().total = 99;`, sandbox);
  assert.equal(vm.runInContext('_sessionsLoadMoreHtml()', sandbox), '');
});

const ALERT = { id: 'a1', level: 'info', title: 'Hello', body: 'b', created_at: '2026-10-23T01:00:00Z', source: 'system' };

test('Alerts tab requests a limit window and shows Load more only while more exist', async () => {
  const urls = [];
  const sandbox = loadAppJS((url, opts) => {
    urls.push(String(url));
    if (String(url).startsWith('/api/alerts?limit=')) return json({ alerts: [ALERT], unread_count: 1, total: 250, has_more: true, next_before: 'a1' });
    return json([]);
  });
  const result = await vm.runInContext('_alertsFetch()', sandbox);
  assert.ok(urls.includes('/api/alerts?limit=100'), urls.join());
  assert.equal(result.hasMore, true);
  assert.equal(result.total, 250);
  const html = vm.runInContext('(r) => _alertsBuildHtml(r)', sandbox)(result);
  assert.match(html, /id="alertsLoadMore"/);
  assert.match(html, /1\/250/);
  const done = vm.runInContext('(r) => _alertsBuildHtml(r)', sandbox)(Object.assign({}, result, { hasMore: false }));
  assert.doesNotMatch(done, /alertsLoadMore/);
});

test('Alerts Load more widens the window by one page and a server switch resets it', async () => {
  const urls = [];
  const sandbox = loadAppJS((url) => {
    urls.push(String(url));
    if (String(url).includes('/api/alerts')) return json({ alerts: [ALERT], unread_count: 0, total: 1, has_more: false });
    return json([]);
  });
  vm.runInContext('alertsLoadMore()', sandbox);
  urls.length = 0;
  await vm.runInContext('_alertsFetch()', sandbox);
  assert.ok(urls.includes('/api/alerts?limit=200'), urls.join());
  vm.runInContext(`state.activeServer = 'host-a'`, sandbox);
  urls.length = 0;
  await vm.runInContext('_alertsFetch()', sandbox);
  assert.ok(urls.includes('/api/proxy/host-a/api/alerts?limit=100'), urls.join());
});

test('the all-servers alerts list stays on the aggregated endpoint, unpaged', async () => {
  const urls = [];
  const sandbox = loadAppJS((url) => { urls.push(String(url)); return json([]); });
  vm.runInContext(`state.activeServer = 'all'`, sandbox);
  await vm.runInContext('_alertsFetch()', sandbox);
  assert.ok(urls.includes('/api/alerts/aggregated'), urls.join());
});

test('the PRD active-session card polls only live sessions and the boot count asks only for unread alerts', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("apiFetch('/api/sessions?states=active')"), 'active-session card must filter to live sessions');
  assert.ok(!src.includes("apiFetch('/api/sessions')"), 'no bare full sessions list left in app.js');
  assert.ok(src.includes("fetch('/api/alerts?unread=1'"), 'boot unread count must request unread alerts only');
});
