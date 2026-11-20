'use strict';
// selectServer() — every control that calls it, PWA side.
//
// Controls that invoke selectServer (found by reading app.js):
//   1. header server button  <button id="serverPickerBtn"> opens the picker
//                        modal (openServerPickerModal); it never calls
//                        selectServer itself -- every choice is a modal row
//   2. header chip       .server-indicator  onclick="selectServer(null)"   (renderSessionsView)
//   3. picker modal      _loadServerPickerModalList rows (real <button>s)
//                        onclick="selectServer(<json name>);closeServerPickerModal();"
//   4. Settings servers  loadServers        onclick="selectServer('<name>')" (one button per row)
//   5. "back to local"   the federated-connection error panel, onclick="selectServer(null)"
//
// Double invocation, from reading the handlers: none of the five controls
// binds a second handler. The only extra work next to a selectServer call
// is the picker-modal row, which calls closeServerPickerModal() in the same
// onclick; the overlay's own click listener (added by _headerModalShow)
// only acts when e.target === overlay, so a row click (target = the button)
// never closes twice or re-enters selectServer. selectServer itself calls loadServers() and showToast(),
// neither of which calls selectServer back. The test "no control invokes
// selectServer twice per click" pins that.
//
// What IS wrong is the toggle inside selectServer: it does
//   state.activeServer = (state.activeServer === name) ? null : name;
// so a second selection of the ACTIVE peer (a re-click on its chip, the
// "Connected" button in Settings, or any repeated call) flips back to local
// instead of staying put. The tests that encode the fixed behaviour
// (idempotent selection) fail against the current code by design.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-select-server-controls.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const PEER = 'pi-node';

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined -- renamed?');

  // Count real invocations: handlers resolve `selectServer` through the global
  // object, so wrapping the property intercepts every control.
  const realSelect = sandbox.selectServer;
  sandbox.__calls = [];
  sandbox.selectServer = function (name) {
    sandbox.__calls.push(name);
    return realSelect.apply(this, arguments);
  };

  // Neutralise the side effects selectServer fans out to; keep the real
  // state mutation and toast text.
  sandbox.__realLoadServers = sandbox.loadServers;
  // The i18n stub returns bare keys; interpolate the args so the toast text
  // carries the server name the way the translated string does.
  const realT = sandbox.t;
  sandbox.t = function (key, args) { return Array.isArray(args) && args.length ? key + ': ' + args.join(' ') : realT.apply(this, arguments); };
  sandbox.__toasts = [];
  sandbox.showToast = (msg, type) => { sandbox.__toasts.push(String(msg)); };
  sandbox.connect = () => {};
  sandbox.loadServers = () => {};
  sandbox.loadServersList = () => {};
  sandbox._ensureLocalHostname = () => {};
  sandbox._checkFederatedConnection = () => {};
  sandbox._loadAllServersSessions = () => {};

  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, label: ${JSON.stringify(PEER)}, enabled: true, url: 'https://x' }] };` +
    `state.activeServer = null; state.sessions = []; state.ws = null;`,
    sandbox,
  );
  return sandbox;
}

function unescapeAttr(s) {
  return s.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&#x27;/g, "'")
    .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
}

// Returns the decoded onclick code of every <button> whose onclick mentions selectServer.
function onclicks(html) {
  const out = [];
  const re = /onclick="([^"]*)"/g;
  let m;
  while ((m = re.exec(html))) {
    const code = unescapeAttr(m[1]);
    if (code.includes('selectServer(')) out.push(code);
  }
  return out;
}

// Pick the onclick that selects `name` (or null).
function clickFor(codes, name) {
  const want = name === null ? 'selectServer(null)' : `selectServer(${JSON.stringify(name)})`;
  const alt = name === null ? null : `selectServer('${name}')`;
  const hit = codes.find(c => c.includes(want) || (alt && c.includes(alt)));
  assert.ok(hit, `no control for ${name}: ${JSON.stringify(codes)}`);
  return hit;
}

// The header server button: it only opens the picker modal; the choice is
// then a modal row. Returns the row's onclick code for `name` ('' = this
// server, 'all', or a peer name) after asserting the button opens the modal.
function headerButtonChoiceFor(sandbox, name) {
  const src = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  const m = src.match(/<button type="button" id="serverPickerBtn"[^>]*onclick="([^"]*)"/);
  assert.ok(m, 'header server button not found in index.html');
  assert.equal(m[1], 'openServerPickerModal()', 'the header button only opens the picker modal');
  const codes = pickerModalCodes(sandbox);
  return clickFor(codes, name === '' ? null : name);
}

function click(sandbox, code) {
  // The modal rows call closeServerPickerModal().
  vm.runInContext(code, sandbox);
}

function active(sandbox) { return vm.runInContext('state.activeServer', sandbox); }

// ── Control renderers ──────────────────────────────────────────────────────

function pickerModalCodes(sandbox) {
  let captured = '';
  const listEl = {
    set innerHTML(v) { captured = v; }, get innerHTML() { return captured; },
  };
  const realGet = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => id === 'serverPickerModalList' ? listEl : realGet(id);
  vm.runInContext('_loadServerPickerModalList()', sandbox);
  sandbox.document.getElementById = realGet;
  return onclicks(captured);
}

async function settingsCodes(sandbox) {
  // Render the real Settings servers list through the real loadServers().
  sandbox.loadServers = sandbox.__realLoadServers;
  let captured = '';
  const el = { set innerHTML(v) { captured = v; }, get innerHTML() { return captured; }, set textContent(v) { captured = String(v); } };
  const realGet = sandbox.document.getElementById;
  sandbox.document.getElementById = (id) => id === 'serverStatus' ? el : realGet(id);
  sandbox.fetch = (url) => {
    const body = String(url).includes('/api/servers/health') ? []
      : String(url).includes('/api/config') ? {}
      : [
        { name: 'local', hostname: 'this-host', enabled: true, url: 'http://l' },
        { name: PEER, enabled: true, url: 'https://x' },
      ];
    return Promise.resolve({ ok: true, json: () => Promise.resolve(body) });
  };
  sandbox._mintAuthNonce = () => Promise.resolve('');
  vm.runInContext('loadServers()', sandbox);
  await flushAsync(30);
  sandbox.document.getElementById = realGet;
  sandbox.loadServers = () => {};
  return onclicks(captured);
}

function headerChipCode() {
  // The header chip sits inside renderSessionsView's big template; read its
  // handler from the source rather than rendering the whole sessions view.
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/class="server-indicator"[^>]*onclick="([^"]*)"/);
  assert.ok(m, 'header .server-indicator chip not found in app.js');
  return unescapeAttr(m[1]);
}

function backToLocalCode() {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/<button class="btn-primary" onclick="(selectServer\(null\))"/);
  assert.ok(m, 'federated "back to local" button not found in app.js');
  return m[1];
}

// Each control: how to get the code that selects PEER.
const CONTROLS = [
  { name: 'header server button (picker modal row)', peerCode: async (s) => headerButtonChoiceFor(s, PEER) },
  { name: 'picker modal row', peerCode: async (s) => clickFor(await pickerModalCodes(s), PEER) },
  { name: 'Settings servers list button', peerCode: async (s) => clickFor(await settingsCodes(s), PEER) },
];

for (const c of CONTROLS) {
  test(`${c.name}: one click selects the peer once`, async () => {
    const sandbox = loadApp();
    const code = await c.peerCode(sandbox);
    click(sandbox, code);
    assert.equal(active(sandbox), PEER);
    assert.ok(sandbox.__toasts.some(t => t.includes(PEER)), `toast should name the peer: ${JSON.stringify(sandbox.__toasts)}`);
    assert.deepEqual(sandbox.__calls, [PEER], 'selectServer must be invoked exactly once per click');
    assert.equal(sandbox.__toasts.length, 1, 'exactly one toast per click');
  });

  test(`${c.name}: no second invocation after the click settles`, async () => {
    const sandbox = loadApp();
    const code = await c.peerCode(sandbox);
    click(sandbox, code);
    await flushAsync(30);
    assert.equal(sandbox.__calls.length, 1);
  });
}

test('header server button: choosing this server returns to local with a single call', () => {
  const sandbox = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sandbox);
  click(sandbox, headerButtonChoiceFor(sandbox, ''));
  assert.equal(active(sandbox), null);
  assert.deepEqual(sandbox.__calls, [null]);
  assert.ok(sandbox.__toasts.length === 1);
});

test('header chip: onclick returns to local with a single call', () => {
  const sandbox = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sandbox);
  click(sandbox, headerChipCode());
  assert.equal(active(sandbox), null);
  assert.deepEqual(sandbox.__calls, [null]);
  assert.ok(sandbox.__toasts.length === 1);
});

test('federated "back to local" button: single call, returns to local', () => {
  const sandbox = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sandbox);
  click(sandbox, backToLocalCode());
  assert.equal(active(sandbox), null);
  assert.deepEqual(sandbox.__calls, [null]);
});

test('no control invokes selectServer twice per click (modal close does not re-trigger)', async () => {
  const sandbox = loadApp();
  // Count closes: handlers resolve closeServerPickerModal through the global
  // object, so wrapping the property intercepts the row's inline call.
  const realClose = sandbox.closeServerPickerModal;
  let closed = 0;
  sandbox.closeServerPickerModal = function () { closed++; return realClose.apply(this, arguments); };
  const codes = pickerModalCodes(sandbox);
  click(sandbox, clickFor(codes, PEER));
  assert.equal(closed, 1, 'modal closed once');
  assert.deepEqual(sandbox.__calls, [PEER]);
});

// ── Behaviour of selectServer itself ───────────────────────────────────────

// Encodes the FIXED behaviour: selecting the active peer again is idempotent.
// Fails against current code, which toggles back to null.
test('selecting the same peer twice leaves activeServer unchanged', () => {
  const sandbox = loadApp();
  vm.runInContext(`selectServer(${JSON.stringify(PEER)})`, sandbox);
  assert.equal(active(sandbox), PEER);
  vm.runInContext(`selectServer(${JSON.stringify(PEER)})`, sandbox);
  assert.equal(active(sandbox), PEER, 'second selection of the active peer must not toggle to local');
});

test('selecting null returns to local', () => {
  const sandbox = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; selectServer(null)`, sandbox);
  assert.ok(!active(sandbox), 'activeServer should be null/local');
  assert.ok(sandbox.__toasts.length === 1);
});

test("selecting 'local' returns to local", () => {
  const sandbox = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; selectServer('local')`, sandbox);
  const a = active(sandbox);
  assert.ok(a === null || a === 'local', `expected local, got ${a}`);
  // Must not be a peer, and must not read as a remote server.
  assert.notEqual(a, PEER);
});

test('a re-render does not re-invoke selectServer with the active name', async () => {
  const sandbox = loadApp();
  vm.runInContext(`selectServer(${JSON.stringify(PEER)})`, sandbox);
  const before = sandbox.__calls.length;
  // Re-render every surface that shows the active peer.
  vm.runInContext('renderServerDropdown()', sandbox);
  pickerModalCodes(sandbox);
  await settingsCodes(sandbox);
  await flushAsync(30);
  assert.equal(sandbox.__calls.length, before, 'rendering must not call selectServer');
  assert.equal(active(sandbox), PEER);
});
