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
//      (onServerDropdownChange → selectServer) exactly once, and the
//      choice is remembered across header re-renders in the same browser
//      page session (server choice is in-memory state, as before the
//      dropdown existed — no localStorage key);
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
