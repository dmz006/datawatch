'use strict';
//
// The persisted server choice (localStorage key 'dw_server_choice', value
// 'all' | a peer name | '' for this server) — app.js's
// _serverChoiceRead / _serverChoiceSave / _serverChoiceSettleBoot plus the
// selectServer() save. These tests cover the PWA-side persistence
// behavior, modeled on app-header-server-dropdown.test.js (the header server
// button and its picker modal):
//   * saved  — selecting a peer / All / this server each saves exactly one
//              key with the expected value; re-selecting the active saved
//              choice writes nothing;
//   * reload — a fresh sandbox (a page reload) sharing the same storage map
//              restores state.activeServer, and the first-paint header
//              button shows the saved peer name or 'all';
//   * falls back — an unknown saved peer name falls back to All at boot
//              and that fallback is saved, so a stale choice never
//              survives another reload;
//   * blocked — localStorage accessors that throw still leave boot and
//              selectServer working, with the choice kept in memory;
//   * refresh — re-rendering the dropdown and a simulated page refresh
//              keep the chosen value; it never snaps back to All;
//   * one round — applying the restored choice at boot costs exactly one
//              fetch round (settle joins the in-flight /api/servers fetch
//              instead of firing a second one), never touches a session
//              kill or signal endpoint, and re-rendering adds no round.
//
// Real app.js runs in a Node vm sandbox — no daemon, no tmux, no browser.
// Run: env -u TMUX -u TMUX_TMPDIR node --test app-server-choice-persist.test.js

const test = require('node:test');
const assert = require('node:assert');
const path = require('node:path');
const fs = require('node:fs');

const WEB = __dirname;
const APP_JS = path.join(WEB, 'app.js');
const PEER = 'pi-node';
const STORE_KEY = 'dw_server_choice';

const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

// The fake DOM (testutil_fake_dom.js) parses what app.js writes, so the tests
// can read the header button and click picker-modal rows.
const { installFakeDom } = require('./testutil_fake_dom');

function active(sb) { return vm.runInContext('state.activeServer', sb); }

// One "page": the real app.js in a fresh sandbox over a caller-owned
// storage object (a Map-backed localStorage stand-in). Simulating a page
// reload = calling loadPage again with the SAME storage object — that is
// what a browser keeps across reloads.
function loadPage(storage) {
  const sandbox = buildSandbox();
  const dom = installFakeDom(sandbox, { statics: ['serverPickerBtn', 'serverPickerDot', 'serverPickerName', 'view'] });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);

  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined — renamed?');
  const realSelect = sandbox.selectServer;
  sandbox.__calls = [];
  sandbox.selectServer = function (name) {
    sandbox.__calls.push(name);
    return realSelect.apply(this, arguments);
  };

  // Real English strings for the option labels and toasts.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));

  // The caller owns the storage map; the sandbox keeps a reference to it.
  sandbox.localStorage = storage;

  // Fetch URL recorder: every fetch app.js fires is visible to assertions
  // (the one-round and no-kill/no-signal checks). Default response is a
  // harmless empty server list.
  sandbox.__fetchUrls = [];
  sandbox.__fetchImpl = (url) => {
    const u = String(url);
    const body = /\/api\/sessions\/aggregated/.test(u) ? [] : { servers: [] };
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
  sandbox.fetch = (url, opts) => {
    sandbox.__fetchUrls.push(String(url));
    return sandbox.__fetchImpl(url, opts);
  };

  // Neutralise the side effects a server switch / boot fans out to; keep
  // the real state mutation, the save, and the dropdown re-render (the
  // subject under test). connect() is a counter: the one-round assertions
  // count every connect() call anywhere.
  sandbox.__connects = 0;
  sandbox.connect = () => { sandbox.__connects++; };
  sandbox.loadServers = () => {};
  sandbox.loadServersList = () => {};
  sandbox._ensureLocalHostname = () => {};
  sandbox._checkFederatedConnection = () => {};
  sandbox._probePickerReachability = () => {};
  sandbox.showToast = () => {};

  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, label: ${JSON.stringify(PEER)}, enabled: true, url: 'https://x' }] };` +
    `state.activeServer = null; state.sessions = []; state.ws = null;` +
    `state.activeView = 'sessions';`,
    sandbox,
  );
  sandbox.__dom = dom;
  return sandbox;
}

// The choice the header button currently shows, in the shape the choice key
// uses: 'all', a peer name, or '' for this server.
function shownChoice(sb) {
  const dot = sb.__dom.el('serverPickerDot');
  const kind = dot.dataset.kind;
  if (kind === 'all') return 'all';
  if (kind === 'local') return '';
  return sb.__dom.el('serverPickerBtn').getAttribute('title').replace(/ \(unreachable\)$/, '');
}

function renderSelected(sb) {
  vm.runInContext('renderServerDropdown();', sb);
  return shownChoice(sb);
}

function assertNoKillOrSignal(sb) {
  for (const u of sb.__fetchUrls) {
    assert.ok(!u.includes('kill'), `a session kill endpoint was called: ${u}`);
    assert.ok(!/signal/.test(u), `a session signal endpoint was called: ${u}`);
  }
}

// Picks an entry the way an operator does: open the picker modal from the
// header button and click the row ('' = this server, 'all', or a peer name).
// The row's own inline handler runs, so selectServer + close are the real path.
function choose(sandbox, value) {
  vm.runInContext('openServerPickerModal()', sandbox);
  const list = sandbox.__dom.byId('serverPickerModalList');
  const row = list.querySelectorAll('button').find(r => r.getAttribute('data-server-name') === value);
  assert.ok(row, 'no picker row for ' + JSON.stringify(value));
  vm.runInContext(row.getAttribute('onclick'), sandbox);
}

// ── (a) saved ──────────────────────────────────────────────────────────────

test('saved: selecting a peer, All, and this server each saves exactly one key with the expected value', () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  const sb = loadPage(storage);
  const writes = [];
  const realSet = storage.setItem.bind(storage);
  storage.setItem = (k, v) => { writes.push([k, v]); realSet(k, v); };

  choose(sb, PEER);
  assert.deepEqual(storage.keys(), [STORE_KEY], 'exactly one key after the peer choice');
  assert.equal(storage.getItem(STORE_KEY), PEER);
  assert.equal(shownChoice(sb), PEER, 'the header button tracks the choice');
  assert.equal(active(sb), PEER);

  choose(sb, 'all');
  assert.deepEqual(storage.keys(), [STORE_KEY], 'still exactly one key after the All choice');
  assert.equal(storage.getItem(STORE_KEY), 'all');
  assert.equal(active(sb), 'all');

  choose(sb, ''); // this server
  assert.deepEqual(storage.keys(), [STORE_KEY], 'still exactly one key after returning to this server');
  assert.equal(storage.getItem(STORE_KEY), '', 'this server is the empty string');
  assert.equal(active(sb), null);

  assert.deepEqual(writes, [
    [STORE_KEY, PEER],
    [STORE_KEY, 'all'],
    [STORE_KEY, ''],
  ], 'one save per choice, one key, expected values');
  assertNoKillOrSignal(sb);
});

test('saved: re-selecting the already-active saved choice writes nothing new', () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  const sb = loadPage(storage);
  const writes = [];
  const realSet = storage.setItem.bind(storage);
  storage.setItem = (k, v) => { writes.push([k, v]); realSet(k, v); };

  choose(sb, PEER);
  choose(sb, PEER); // same entry again: the handler fires, selectServer is a no-op
  assert.deepEqual(sb.__calls, [PEER, PEER], 'one handler pass per gesture');
  assert.deepEqual(writes, [[STORE_KEY, PEER]], 'the no-op re-select wrote nothing');
  assert.equal(active(sb), PEER);

  choose(sb, 'all');
  choose(sb, 'all');
  assert.deepEqual(writes.slice(-1), [[STORE_KEY, 'all']], 're-selecting the active saved All wrote nothing');

  choose(sb, '');
  choose(sb, '');
  assert.deepEqual(writes.slice(-1), [[STORE_KEY, '']], 're-selecting the active this-server wrote nothing');
  assert.deepEqual(storage.keys(), [STORE_KEY]);
});

// ── (b) reload ─────────────────────────────────────────────────────────────

test('reload: a saved peer name that is still in the server list is restored by a fresh sandbox, and the first-paint dropdown shows it', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  storage.setItem(STORE_KEY, PEER);

  // The "reload": a fresh page over the same storage map.
  const sb = loadPage(storage);
  // Force the real first-load path: the list is not fetched yet (the
  // load-bearing state.servers === undefined signal).
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  sb.__fetchImpl = (url) => Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve({ servers: [{ name: PEER, label: PEER, enabled: true, url: 'https://x' }] }),
  });

  vm.runInContext('_serverChoiceSettleBoot();', sb);
  await flushAsync(30);
  assert.equal(active(sb), PEER, 'the reload restores the saved peer');
  assert.equal(renderSelected(sb), PEER, 'first paint is already on the restored server');
  assert.equal(shownChoice(sb), PEER);
  assertNoKillOrSignal(sb);
});

test('reload: a saved "all" is restored by a fresh sandbox the same way, and the first-paint dropdown is on All', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  storage.setItem(STORE_KEY, 'all');

  const sb = loadPage(storage);
  assert.equal(active(sb), null, 'before the settle step, boot starts on this server');

  vm.runInContext('_serverChoiceSettleBoot();', sb);
  await flushAsync(30);
  assert.equal(active(sb), 'all', 'the reload restores the saved All');
  assert.equal(renderSelected(sb), 'all', 'first paint is already on All');
  assert.equal(shownChoice(sb), 'all');
  assertNoKillOrSignal(sb);
});

// ── (c) falls back ─────────────────────────────────────────────────────────

test('falls back: an unknown saved peer name lands on All at reload, and that fallback is saved', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  storage.setItem(STORE_KEY, 'ghost-peer');

  const sb = loadPage(storage);
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  sb.__fetchImpl = () => Promise.resolve({
    ok: true,
    status: 200,
    // ghost-peer is gone from the list (removed or disabled).
    json: () => Promise.resolve({ servers: [{ name: PEER, label: PEER, enabled: true, url: 'https://x' }] }),
  });

  vm.runInContext('_serverChoiceSettleBoot();', sb);
  await flushAsync(30);
  assert.equal(active(sb), 'all', 'the unknown saved name falls back to All');
  assert.equal(storage.getItem(STORE_KEY), 'all', 'the fallback is saved, so a stale choice does not survive the next reload');
  assert.deepEqual(storage.keys(), [STORE_KEY], 'no second key');
  assert.equal(renderSelected(sb), 'all', 'first paint is on All');
  assertNoKillOrSignal(sb);
});

test('falls back: a peer choice that cannot be validated in time lands on All and does not hold boot open', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  storage.setItem(STORE_KEY, PEER);

  const sb = loadPage(storage);
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  sb.__fetchImpl = () => new Promise(() => {}); // the list never arrives
  sb.setTimeout = (fn) => { fn(); return 0; };   // the settle bound, fired inline

  vm.runInContext('_serverChoiceSettleBoot();', sb);
  await flushAsync(30);
  assert.equal(active(sb), 'all', 'unvalidatable choice → All, boot proceeds');
  assert.equal(storage.getItem(STORE_KEY), 'all', 'the fallback is saved');
  assertNoKillOrSignal(sb);
});

// ── (d) blocked ────────────────────────────────────────────────────────────

test('blocked: localStorage accessors that throw still leave boot and selectServer working, with the choice kept in memory', () => {
  // 1) the localStorage global is unavailable (sandbox artifact of a browser
  //    forbidding the global; real browsers keep it defined and throw at the
  //    accessors instead — cases 2 and 3). The persistence helpers must
  //    degrade cleanly. activeView is a view with no re-render route, so
  //    selectServer's session-list fan-out (which expects the global, as in
  //    any real browser) stays out of this persistence-focused block.
  {
    const sb = loadPage({ m: new Map(), getItem() { return null; }, setItem() {}, removeItem() {} });
    Object.defineProperty(sb, 'localStorage', {
      get() { throw new Error('SecurityError: localStorage is not available'); },
      configurable: true,
    });
    vm.runInContext("state.activeView = 'overview';", sb);
    assert.equal(vm.runInContext('_serverChoiceRead();', sb), null, 'read degrades to "nothing saved", never throws');
    vm.runInContext("_serverChoiceSave('all')", sb); // save swallows the block
    vm.runInContext('_serverChoiceSettleBoot();', sb);
    assert.equal(active(sb), null, 'boot proceeds with the default: this server');
    choose(sb, PEER);
    assert.equal(active(sb), PEER, 'selectServer still works — the choice lives in memory');
    choose(sb, '');
    assert.equal(active(sb), null, 'and back to this server');
  }
  // 2) getItem AND setItem both throw (e.g. a private-mode browser)
  {
    const sb = loadPage({ m: new Map(), getItem() { return null; }, setItem() {}, removeItem() {} });
    sb.localStorage = {
      getItem() { throw new Error('SecurityError: access denied'); },
      setItem() { throw new Error('SecurityError: access denied'); },
      removeItem() {},
    };
    vm.runInContext("state.activeView = 'overview';", sb);
    assert.equal(vm.runInContext('_serverChoiceRead();', sb), null);
    vm.runInContext("_serverChoiceSave('all')", sb);
    vm.runInContext('_serverChoiceSettleBoot();', sb);
    assert.equal(active(sb), null, 'boot proceeds');
    choose(sb, PEER);
    assert.equal(active(sb), PEER, 'the switch still lands (in memory, as before persistence existed)');
    choose(sb, '');
    assert.equal(active(sb), null, 'and back to this server');
  }
  // 3) only the write side is blocked (quota): reads still work, and a
  //    new choice still applies in memory
  {
    const readStore = { m: new Map([['dw_server_choice', PEER]]), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem() {}, removeItem() {} };
    const sb = loadPage({ m: new Map(), getItem() { return null; }, setItem() {}, removeItem() {} });
    sb.localStorage = {
      getItem(k) { return readStore.getItem(k); },
      setItem() { throw new Error('QuotaExceededError'); },
      removeItem() {},
    };
    assert.equal(vm.runInContext('_serverChoiceRead();', sb), PEER, 'reads still work when only writes are blocked');
    choose(sb, 'all');
    assert.equal(active(sb), 'all', 'selectServer still works; the new choice is in memory');
  }
});

// ── (e) refresh ────────────────────────────────────────────────────────────

test('refresh: the chosen value survives renderServerDropdown re-runs and a simulated page refresh, never snapping back to All', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };

  const sb = loadPage(storage);
  choose(sb, PEER);
  assert.equal(storage.getItem(STORE_KEY), PEER);
  assert.equal(renderSelected(sb), PEER);
  assert.equal(shownChoice(sb), PEER);

  // Re-rendering (view switch, data update, ...) keeps the chosen value.
  vm.runInContext('renderServerDropdown(); renderServerDropdown();', sb);
  assert.equal(renderSelected(sb), PEER, 're-renders never snap the dropdown back to All');
  assert.equal(storage.getItem(STORE_KEY), PEER);
  assert.deepEqual(storage.keys(), [STORE_KEY]);
  assertNoKillOrSignal(sb);

  // A simulated page refresh: a fresh sandbox over the same storage map.
  const sb2 = loadPage(storage);
  sb2.setTimeout = (fn) => { fn(); return 0; };
  vm.runInContext('_serverChoiceSettleBoot();', sb2);
  await flushAsync(20);
  assert.equal(active(sb2), PEER, 'the refresh restores the peer, not All');
  assert.equal(renderSelected(sb2), PEER, 'and first paint after the refresh is on it');
  assert.equal(shownChoice(sb2), PEER);
  assert.deepEqual(storage.keys(), [STORE_KEY], 'still exactly one key after the refresh');
  assertNoKillOrSignal(sb2);
});

// ── one fetch round for the restored choice ───────────────────────────────

test('the restored peer commits at boot using exactly one /api/servers fetch round, no second round, and no session kill or signal call', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  storage.setItem(STORE_KEY, PEER);

  const sb = loadPage(storage);
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  sb.__fetchImpl = (url) => Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve({ servers: [{ name: PEER, label: PEER, enabled: true, url: 'https://x' }] }),
  });

  // The settle step commits the choice BEFORE the boot round — it must not
  // itself connect() or add any fetch round.
  vm.runInContext('_serverChoiceSettleBoot();', sb);
  await flushAsync(30);
  assert.equal(active(sb), PEER);
  assert.equal(sb.__connects, 0, 'the settle step does not connect()');
  assert.equal(sb.__fetchUrls.filter((u) => u.includes('/api/servers')).length, 1, 'exactly one /api/servers round');
  assert.ok(!sb.__fetchUrls.some((u) => u.includes('/api/sessions')), 'no sessions round before the boot round');
  assertNoKillOrSignal(sb);

  // The single boot round the app runs after settle: one connect().
  vm.runInContext('connect();', sb);
  assert.equal(sb.__connects, 1, 'exactly one connect() for the boot round');

  // Re-rendering the header after boot adds no round.
  vm.runInContext('renderServerDropdown(); _initServerDropdown();', sb);
  await flushAsync(30);
  assert.equal(sb.__connects, 1, 'no second connect after the re-render');
  assert.equal(sb.__fetchUrls.filter((u) => u.includes('/api/servers')).length, 1, 'no second /api/servers round after the re-render');
  assertNoKillOrSignal(sb);
});

test('the restored "all" commits at boot with no extra fetch round and no session kill or signal call', async () => {
  const storage = { m: new Map(), getItem(k) { return this.m.has(k) ? this.m.get(k) : null; }, setItem(k, v) { this.m.set(k, String(v)); }, removeItem(k) { this.m.delete(k); }, keys() { return Array.from(this.m.keys()); } };
  storage.setItem(STORE_KEY, 'all');

  const sb = loadPage(storage);

  // 'all' needs no list lookup: it commits immediately.
  vm.runInContext('_serverChoiceSettleBoot();', sb);
  await flushAsync(30);
  assert.equal(active(sb), 'all');
  assert.equal(sb.__connects, 0, 'the settle step does not connect()');
  assert.equal(sb.__fetchUrls.length, 0, 'no fetch round from the settle step');
  assertNoKillOrSignal(sb);

  // The single boot round for All: one connect(), then the one aggregated
  // sessions round (the app's real boot order after settle). Active view is
  // not sessions, so the real loader never renders the sessions view.
  vm.runInContext("state.activeView = 'overview';", sb);
  vm.runInContext('connect();', sb);
  vm.runInContext('_loadAllServersSessions();', sb);
  await flushAsync(30);
  assert.equal(sb.__connects, 1, 'exactly one connect() for the boot round');
  const sessionsRounds = sb.__fetchUrls.filter((u) => /\/api\/sessions(\?|\/|$)/.test(u)).length;
  assert.equal(sessionsRounds, 1, 'exactly one sessions round');
  assert.ok(sb.__fetchUrls.includes('/api/sessions/aggregated'), 'the one sessions round is the aggregated fetch for All');

  // Re-rendering after boot adds no round.
  vm.runInContext('renderServerDropdown(); _initServerDropdown();', sb);
  await flushAsync(30);
  assert.equal(sb.__connects, 1, 'no second connect after the re-render');
  assert.equal(sb.__fetchUrls.filter((u) => /\/api\/sessions(\?|\/|$)/.test(u)).length, 1, 'no second sessions round after the re-render');
  assertNoKillOrSignal(sb);
});
