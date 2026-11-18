'use strict';
// Header server dropdown (#serverDropdown) — PWA side.
//
// The app header carries ONE native <select id="serverDropdown">
// (successor to the per-panel .server-picker-bar chip row and the
// Sessions .sessions-topbar-servers slot, both removed). This test pins:
//   1. the control reflects the current server — 'All', this server,
//      or a named peer — and is the same on every page;
//   2. it lists All, this server, and every enabled peer, including with
//      many peers (12) and peer names containing quotes or backslashes
//      (the name travels in the escHtml-escaped option value);
//   3. choosing an entry goes through the single generic onchange
//      (onServerDropdownChange → selectServer) exactly once, the choice
//      is remembered across header re-renders, and it is saved to
//      localStorage under dw_server_choice ('all' / a peer name / '' for
//      this server) so the next boot restores it — a stale peer name
//      falls back to All, and blocked storage never throws;
//   4. no .server-picker-bar chip row or .sessions-topbar-servers element
//      is rendered anywhere, Sessions page included;
//   5. the control has an accessible name and native select keyboard
//      semantics (a <select> + value-driven onchange, not a custom
//      div listbox).
//
// No daemons are started and no tmux is touched: the real app.js runs in
// a vm sandbox, exactly like the other PWA tests here.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-header-server-dropdown.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const WEB = __dirname;
const APP_JS = path.join(WEB, 'app.js');
const INDEX_HTML = path.join(WEB, 'index.html');
const PEER = 'pi-node';

function unescapeAttr(s) {
  return s.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&#x27;/g, "'")
    .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
}

function elementCatcher() {
  let html = '';
  let val = '';
  return {
    style: {},
    classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } },
    dataset: {},
    setAttribute() {}, getAttribute() { return null; }, removeAttribute() {},
    querySelector() { return null; }, querySelectorAll() { return []; },
    addEventListener() {}, removeEventListener() {},
    appendChild() {}, remove() {},
    focus() {}, blur() {}, click() {},
    set innerHTML(v) { html = String(v); }, get innerHTML() { return html; },
    set textContent(v) { html = String(v); }, get textContent() { return html; },
    set value(v) { val = String(v); }, get value() { return val; },
  };
}

function loadApp() {
  const sel = elementCatcher();   // the header <select id="serverDropdown">
  const view = elementCatcher();  // #view — the page body the views render into
  const sandbox = buildSandbox({ byId: { serverDropdown: sel, view } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // Real English strings for the option labels ("All servers", "This
  // server", toasts) — the i18n stub returns raw keys without the bundle.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));

  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined — renamed?');
  const realSelect = sandbox.selectServer;
  sandbox.__calls = [];
  sandbox.selectServer = function (name) {
    sandbox.__calls.push(name);
    return realSelect.apply(this, arguments);
  };

  // Neutralise the side effects a server switch fans out to; keep the real
  // state mutation, toast text and the dropdown re-render (the subject
  // under test).
  sandbox.__toasts = [];
  sandbox.showToast = (msg) => { sandbox.__toasts.push(String(msg)); };
  sandbox.connect = () => {};
  sandbox.loadServers = () => {};
  sandbox.loadServersList = () => {};
  sandbox._ensureLocalHostname = () => {};
  sandbox._checkFederatedConnection = () => {};
  sandbox._loadAllServersSessions = () => {};
  sandbox._probePickerReachability = () => {};

  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, label: ${JSON.stringify(PEER)}, enabled: true, url: 'https://x' }] };` +
    `state.activeServer = null; state.sessions = []; state.ws = null;` +
    `state.activeView = 'sessions';`,
    sandbox,
  );
  sandbox.__sel = sel;
  sandbox.__view = view;
  return sandbox;
}

// The value attribute of the option currently carrying `selected`.
function selectedOption(html) {
  const m = html.match(/<option value="([^"]*)"[^>]*selected/);
  assert.ok(m, 'no option carries selected: ' + html.slice(0, 400));
  return unescapeAttr(m[1]);
}

// Every option's value, in rendered order.
function optionValues(html) {
  const out = [];
  const re = /<option value="([^"]*)"/g;
  let m;
  while ((m = re.exec(html))) out.push(unescapeAttr(m[1]));
  return out;
}

// The one generic change handler fires with the option's value, the way a
// keyboard or mouse choice in a native <select> does.
function choose(sandbox, value) {
  vm.runInContext(`onServerDropdownChange(${JSON.stringify(value)})`, sandbox);
}

function active(sandbox) { return vm.runInContext('state.activeServer', sandbox); }

// ── 1. The control shows the current server ────────────────────────────────

test('shows this server (local) as the selected entry', () => {
  const sb = loadApp();
  vm.runInContext('renderServerDropdown()', sb);
  const html = sb.__sel.innerHTML;
  assert.ok(html.startsWith('<option'), 'dropdown filled with options');
  assert.equal(selectedOption(html), '', 'the empty ("this server") option must be selected locally');
  assert.ok(html.includes('This server'), 'local option is labelled "This server"');
  assert.equal(sb.__sel.value, '', 'select value stays empty for local');
});

test('shows "All" as the selected entry in All mode', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = 'all'; renderServerDropdown()`, sb);
  const html = sb.__sel.innerHTML;
  assert.equal(selectedOption(html), 'all');
  assert.ok(html.includes('All servers'), 'all option is labelled "All servers"');
  assert.equal(sb.__sel.value, 'all');
});

test('shows the named peer as the selected entry when connected to it', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; renderServerDropdown()`, sb);
  const html = sb.__sel.innerHTML;
  assert.equal(selectedOption(html), PEER);
  assert.equal(sb.__sel.value, PEER);
});

// ── 2. It lists All, this server, and every enabled peer ───────────────────

test('lists All + this server + every peer, in order, with a hostile peer name', () => {
  const sb = loadApp();
  const hostile = 'say "hi" & back\\slash';
  const names = [
    'relay-01', 'relay-02', 'relay-03', 'relay-04', 'relay-05', 'relay-06',
  ];
  for (let i = 0; i < 6; i++) names.push('relay-' + String(10 + i + 1)); // total 12
  names[2] = hostile;
  vm.runInContext(
    `state.servers = { servers: ${JSON.stringify(names.map(n => ({ name: n, label: n, enabled: true, url: 'https://x' })))}` +
    ` .concat([{ name: 'retired-07', label: 'retired-07', enabled: false, url: 'https://old' }]) };`,
    sb,
  );
  vm.runInContext('renderServerDropdown()', sb);
  const html = sb.__sel.innerHTML;
  const values = optionValues(html);
  assert.equal(values[0], 'all', 'first entry is All');
  assert.equal(values[1], '', 'second entry is this server');
  assert.deepEqual(values.slice(2), names, 'all 12 enabled peers listed in order');
  assert.ok(!values.includes('retired-07'), 'a disabled peer is not listed');
  // The hostile name survived escaping: it is one intact option value, and
  // it did not break out of its value attribute into markup.
  assert.ok(optionValues(html).includes(hostile), 'hostile name round-trips through the option value');
  assert.equal(html.match(/<option /g).length, 2 + names.length, 'no extra/missing options from the hostile name');
});

// ── 3. Choosing an entry calls selectServer; the choice is remembered ──────

test('choosing a peer calls selectServer exactly once and selects the entry', () => {
  const sb = loadApp();
  choose(sb, PEER);
  assert.deepEqual(sb.__calls, [PEER], 'one selectServer call per choice');
  assert.equal(active(sb), PEER);
  const html = sb.__sel.innerHTML;
  assert.equal(selectedOption(html), PEER, 'dropdown re-renders with the peer highlighted');
  assert.equal(sb.__sel.value, PEER);
  assert.ok(sb.__toasts.some(t => t.includes(PEER)), 'toast names the peer');
  assert.equal(sb.__toasts.length, 1);
});

test('choosing All routes to selectServer("all")', () => {
  const sb = loadApp();
  choose(sb, 'all');
  assert.deepEqual(sb.__calls, ['all']);
  assert.equal(active(sb), 'all');
  assert.equal(selectedOption(sb.__sel.innerHTML), 'all');
});

test('choosing this server returns to local with a single selectServer(null)', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  choose(sb, '');
  assert.deepEqual(sb.__calls, [null], '"" is "this server" → local');
  assert.equal(active(sb), null);
  assert.equal(selectedOption(sb.__sel.innerHTML), '');
});

test('the choice is remembered and restored on the next header render in the same browser', () => {
  const sb = loadApp();
  choose(sb, PEER);
  const before = sb.__calls.length;

  // Next navigation: the header is re-initialised exactly how navigate()
  // does it. Same page, same browser — the operator's server choice must
  // still be there, and re-initialising must not switch servers again.
  vm.runInContext('_initServerDropdown()', sb);
  assert.equal(active(sb), PEER, 'activeServer survives the next header render');
  assert.equal(selectedOption(sb.__sel.innerHTML), PEER, 'dropdown re-renders on the remembered peer');
  assert.equal(sb.__sel.value, PEER);
  assert.deepEqual(sb.__calls.slice(before), [], 're-render must not call selectServer again');

  // Re-render straight (the path selectServer itself uses) must agree.
  vm.runInContext('renderServerDropdown()', sb);
  assert.equal(selectedOption(sb.__sel.innerHTML), PEER);
  assert.equal(sb.__calls.length, before, 'a plain re-render stays call-free');
});

// ── 4. The old chip row and topbar slot are gone ───────────────────────────

test('no .server-picker-bar or .sessions-topbar-servers element anywhere', () => {
  const appSrc = fs.readFileSync(APP_JS, 'utf8');
  const shellSrc = fs.readFileSync(INDEX_HTML, 'utf8');
  for (const src of [appSrc, shellSrc]) {
    assert.ok(!src.includes('class="server-picker-bar"'), 'markup class="server-picker-bar" must not exist');
    assert.ok(!src.includes('class="sessions-topbar-servers"'), 'markup class="sessions-topbar-servers" must not exist');
  }

  const sb = loadApp();
  // The header control itself renders exactly one native select, no chips.
  const bar = vm.runInContext('_serverPickerBar()', sb);
  assert.ok(!bar.includes('server-picker-bar'));
  assert.ok(!bar.includes('sessions-topbar-servers'));

  // And the Sessions page body — rendered with a peer active, the state
  // where the old slot used to show its own chip row.
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  vm.runInContext('renderSessionsView()', sb);
  const page = sb.__view.innerHTML;
  assert.ok(page !== '', 'sessions view rendered into #view');
  assert.ok(!page.includes('server-picker-bar'), 'sessions page has no chip row');
  assert.ok(!page.includes('sessions-topbar-servers'), 'sessions page has no topbar server slot');
});

// ── 5. Accessible name + native select semantics ───────────────────────────

test('the control has an accessible name and keyboard-usable select markup', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  const bar = vm.runInContext('_serverPickerBar()', sb);
  // one element, a native select — native controls are keyboard-operable
  assert.match(bar, /^<select[\s\S]*<\/select>$/);
  assert.ok(!bar.includes('<div'), 'no custom div listbox');
  assert.ok(bar.includes('aria-label="Server"'), 'accessible name is present');
  assert.ok(bar.includes('<option'), 'options are real <option> children');
});

test('the static shell in index.html is the same control, visible before JS', () => {
  const src = fs.readFileSync(INDEX_HTML, 'utf8');
  const m = src.match(/<select id="serverDropdown"[^>]*>[\s\S]*?<\/select>/);
  assert.ok(m, 'header <select id="serverDropdown"> present in the static shell');
  assert.ok(m[0].includes('aria-label="Server"'), 'static shell carries the accessible name');
  assert.ok(m[0].includes('onchange="onServerDropdownChange(this.value)"'), 'one generic value-driven onchange');
  assert.ok(m[0].includes('<option value="" selected>This server</option>'), 'placeholder option selected before the first JS render');
});

test('the change path is keyboard-equivalent: value in → selectServer out', () => {
  const sb = loadApp();
  // A native <select>'s keyboard choice (arrow keys / typeahead) is just:
  // the .value property changes, then the element's onchange expression
  // `onServerDropdownChange(this.value)` runs. Reproduce exactly that.
  sb.__sel.value = PEER;
  vm.runInContext('onServerDropdownChange(__sel.value)', sb);
  assert.deepEqual(sb.__calls, [PEER]);
  assert.equal(active(sb), PEER);

  // Same gesture, All and back-to-local.
  sb.__sel.value = 'all';
  vm.runInContext('onServerDropdownChange(__sel.value)', sb);
  assert.deepEqual(sb.__calls, [PEER, 'all']);
  sb.__sel.value = '';
  vm.runInContext('onServerDropdownChange(__sel.value)', sb);
  assert.deepEqual(sb.__calls, [PEER, 'all', null]);
  assert.equal(active(sb), null);
});

test('rendering settles without touching tmux-adjacent state or re-firing handlers', async () => {
  const sb = loadApp();
  choose(sb, PEER);
  await flushAsync(30);
  assert.equal(sb.__calls.length, 1, 'settled: still exactly one selectServer call');
  assert.equal(active(sb), PEER);
});

// ── 6. The choice is persisted (dw_server_choice) and restored at boot ────

// Counts writes to the choice key while the real storage map still records
// them, so a test can assert both "nothing extra was written" and "the map
// holds the right value".
function spyWrites(sb) {
  const writes = [];
  const realSet = sb.localStorage.setItem.bind(sb.localStorage);
  sb.localStorage.setItem = (k, v) => { writes.push([k, v]); realSet(k, v); };
  return writes;
}

test('the choice key and shape: dw_server_choice = "all" | peer name | ""', () => {
  const sb = loadApp();
  assert.equal(vm.runInContext('SERVER_CHOICE_STORE_KEY', sb), 'dw_server_choice', 'one constant names the key');
  vm.runInContext("_serverChoiceSave('all')", sb);
  assert.equal(sb.localStorage.getItem('dw_server_choice'), 'all');
  assert.equal(vm.runInContext('_serverChoiceRead()', sb), 'all');
  vm.runInContext(`_serverChoiceSave(${JSON.stringify(PEER)})`, sb);
  assert.equal(sb.localStorage.getItem('dw_server_choice'), PEER);
  assert.equal(vm.runInContext('_serverChoiceRead()', sb), PEER);
  vm.runInContext("_serverChoiceSave('')", sb);
  assert.equal(sb.localStorage.getItem('dw_server_choice'), '', 'this server is the empty string');
  assert.equal(vm.runInContext("_serverChoiceRead()", sb), '');
});

test('selectServer saves the choice: peer, All, and back to local', () => {
  const sb = loadApp();
  const writes = spyWrites(sb);
  choose(sb, PEER);
  assert.deepEqual(writes, [['dw_server_choice', PEER]], 'choosing a peer writes the peer name');
  choose(sb, 'all');
  assert.deepEqual(writes[1], ['dw_server_choice', 'all'], 'choosing All writes "all"');
  choose(sb, '');
  assert.deepEqual(writes[2], ['dw_server_choice', ''], 'returning to local writes ""');
});

test('a no-op re-select writes nothing new and re-fetches nothing', () => {
  const sb = loadApp();
  const writes = spyWrites(sb);
  choose(sb, PEER);
  choose(sb, PEER); // same entry again: the handler fires, selectServer is a no-op
  assert.deepEqual(sb.__calls, [PEER, PEER], 'one handler pass per gesture');
  assert.deepEqual(writes, [['dw_server_choice', PEER]], 'the no-op re-select writes nothing');
  assert.equal(active(sb), PEER);
});

test('blocked localStorage never throws: read, save, and a full switch', () => {
  const sb = loadApp();
  sb.localStorage = {
    getItem() { throw new Error('blocked'); },
    setItem() { throw new Error('blocked'); },
    removeItem() { throw new Error('blocked'); },
  };
  assert.equal(vm.runInContext('_serverChoiceRead()', sb), null, 'read degrades to "nothing saved"');
  vm.runInContext("_serverChoiceSave('all')", sb); // must not throw
  const before = sb.__calls.length;
  choose(sb, PEER); // the real selectServer runs its save against blocked storage
  assert.equal(active(sb), PEER, 'the switch still lands (in-memory, as before persistence existed)');
  assert.equal(sb.__calls.length, before + 1);
});

// Boot commit — _serverChoiceSettleBoot. The DOMContentLoaded handler is a
// no-op in the sandbox, so these call the settle step directly, the way the
// boot sequence does (before the first connect()/session-fetch round).
test('boot: nothing saved → this server (the default)', async () => {
  const sb = loadApp();
  assert.equal(sb.localStorage.getItem('dw_server_choice'), null, 'no saved choice');
  vm.runInContext('_serverChoiceSettleBoot()', sb);
  await flushAsync(20);
  assert.equal(active(sb), null, 'local is the default');
});

test('boot: a saved "" restores this server, a saved "all" restores All', async () => {
  const sb1 = loadApp();
  sb1.localStorage.setItem('dw_server_choice', '');
  vm.runInContext('_serverChoiceSettleBoot()', sb1);
  await flushAsync(20);
  assert.equal(active(sb1), null);

  const sb2 = loadApp();
  sb2.localStorage.setItem('dw_server_choice', 'all');
  vm.runInContext('_serverChoiceSettleBoot()', sb2);
  await flushAsync(20);
  assert.equal(active(sb2), 'all');
});

// The one-round rule, peer path: the saved peer is validated against the
// server list, and the list lookup must join the one in-flight
// /api/servers fetch instead of firing a second one.
test('boot: a saved peer in the list commits, using exactly one /api/servers round', async () => {
  const sb = loadApp();
  // Force the real first-load path: the list is not fetched yet (the
  // load-bearing state.servers === undefined signal).
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  const urls = [];
  sb.fetch = (url) => {
    urls.push(String(url));
    return Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ servers: [{ name: PEER, label: PEER, enabled: true, url: 'https://x' }] }),
    });
  };
  sb.localStorage.setItem('dw_server_choice', PEER);
  vm.runInContext('_serverChoiceSettleBoot()', sb);
  await flushAsync(30);
  assert.equal(active(sb), PEER, 'the saved peer is committed before the boot round');
  assert.equal(urls.filter(u => u.includes('/api/servers')).length, 1, 'exactly one /api/servers round');
});

test('boot: a stale saved peer falls back to All and saves that fallback', async () => {
  const sb = loadApp();
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  sb.fetch = () => Promise.resolve({
    ok: true,
    // The saved peer is gone from the list (removed or disabled).
    json: () => Promise.resolve({ servers: [{ name: PEER, label: PEER, enabled: true, url: 'https://x' }] }),
  });
  sb.localStorage.setItem('dw_server_choice', 'ghost-peer');
  vm.runInContext('_serverChoiceSettleBoot()', sb);
  await flushAsync(30);
  assert.equal(active(sb), 'all', 'unvalidatable choice → All');
  assert.equal(sb.localStorage.getItem('dw_server_choice'), 'all', 'the fallback is saved, so it does not survive the next reload');
});

test('boot: a peer choice that cannot be validated in time falls back to All and does not hold boot open', async () => {
  const sb = loadApp();
  vm.runInContext('state.servers = undefined; state._serversLoading = false;', sb);
  // The list never arrives (the fetch hangs): the settle bound must fire
  // instead of leaving the Promise pending. Fire the bound synchronously so
  // the test does not race a real timer against the harness's ticks.
  sb.fetch = () => new Promise(() => {}); // never settles
  sb.setTimeout = (fn) => { fn(); return 0; }; // the bound, fired inline
  sb.localStorage.setItem('dw_server_choice', PEER);
  vm.runInContext('_serverChoiceSettleBoot()', sb);
  await flushAsync(30);
  assert.equal(active(sb), 'all', 'timeout → All, boot proceeds');
  assert.equal(sb.localStorage.getItem('dw_server_choice'), 'all');
});

test('a header render after boot commit shows the restored entry selected', () => {
  const sb = loadApp();
  sb.localStorage.setItem('dw_server_choice', PEER);
  vm.runInContext("state.activeServer = 'all'", sb); // pre-commit state
  // _onServerListReady runs its callback via setTimeout when the list is
  // already loaded — the sandbox timer is a no-op, so fire it inline.
  sb.setTimeout = (fn) => { fn(); return 0; };
  vm.runInContext('_serverChoiceSettleBoot()', sb);
  assert.equal(active(sb), PEER, 'the commit is synchronous once the list is known');
  const html = vm.runInContext('renderServerDropdown(); __sel.innerHTML', sb);
  assert.equal(selectedOption(html), PEER, 'first paint is already on the restored server');
  assert.equal(sb.__sel.value, PEER);
});
