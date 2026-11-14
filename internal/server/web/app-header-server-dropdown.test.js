'use strict';
// Header server button (#serverPickerBtn) and its picker modal — PWA side.
//
// The app header carries ONE compact button (icon + status dot + short name)
// that opens the server picker modal; it replaces the wide native
// <select id="serverDropdown"> (and the Sessions toolbar's second select, and
// before them the .server-picker-bar chip row). This test pins:
//   1. the button reflects the current server — 'All', this server or a named
//      peer — through its dot class, short name (truncated), title and
//      aria-label, and flags an unreachable selected peer with the warning dot;
//   2. the modal lists All, this server and every enabled peer as keyboard-
//      reachable buttons (aria-current on the active one), including many
//      peers and peer names with quotes/backslashes, never disabling a row;
//   3. choosing a row goes through selectServer exactly once and closes the
//      modal, the choice is remembered across header re-renders, and it is
//      saved under dw_server_choice so the next boot restores it — a stale
//      peer name falls back to All, and blocked storage never throws;
//   4. no .server-picker-bar chip row, .sessions-topbar-servers slot or server
//      <select> exists anywhere, Sessions page included;
//   5. the shared header-modal helpers: role/aria wiring, focus in, Tab trap,
//      Escape and backdrop close, listener removal, focus return and
//      aria-expanded reset, the '?' help link in the title row;
//   6. the static shell in index.html carries the button.
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
const { installFakeDom } = require('./testutil_fake_dom');

const WEB = __dirname;
const APP_JS = path.join(WEB, 'app.js');
const INDEX_HTML = path.join(WEB, 'index.html');
const PEER = 'pi-node';

function loadApp() {
  const sandbox = buildSandbox();
  const dom = installFakeDom(sandbox, { statics: ['serverPickerBtn', 'serverPickerDot', 'serverPickerName', 'view'] });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // Real English strings for labels and toasts — the i18n stub returns raw
  // keys without the bundle.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));

  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined — renamed?');
  const realSelect = sandbox.selectServer;
  sandbox.__calls = [];
  sandbox.selectServer = function (name) {
    sandbox.__calls.push(name);
    return realSelect.apply(this, arguments);
  };

  // Neutralise the side effects a server switch fans out to; keep the real
  // state mutation, toast text and the button re-render (the subject under
  // test).
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
  sandbox.__dom = dom;
  return sandbox;
}

function active(sandbox) { return vm.runInContext('state.activeServer', sandbox); }

// The button's observable state.
function btn(sb) {
  const b = sb.__dom.el('serverPickerBtn');
  return {
    label: b.getAttribute('aria-label'),
    title: b.getAttribute('title'),
    expanded: b.getAttribute('aria-expanded'),
    dot: sb.__dom.el('serverPickerDot').className,
    name: sb.__dom.el('serverPickerName').textContent,
  };
}

function openModal(sb) {
  vm.runInContext('openServerPickerModal()', sb);
  const modal = sb.__dom.byId('serverPickerModal');
  assert.ok(modal, 'serverPickerModal overlay is in the DOM');
  return modal;
}

function rows(sb) {
  return sb.__dom.byId('serverPickerModalList').querySelectorAll('button');
}

// Rows by data-server-name (never a selector built from the name).
function rowFor(sb, name) {
  const hit = rows(sb).find(r => r.getAttribute('data-server-name') === name);
  assert.ok(hit, 'no row for ' + JSON.stringify(name));
  return hit;
}

// Runs a row's onclick the way the browser does (global handler resolution).
function clickRow(sb, row) {
  vm.runInContext(row.getAttribute('onclick'), sb);
}

// ── 1. The button shows the current server ─────────────────────────────────

test('shows this server (local): local dot, short name, title and aria-label', () => {
  const sb = loadApp();
  vm.runInContext('_localHostname = "dev-box"; renderServerDropdown()', sb);
  const s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-local');
  assert.equal(s.name, 'dev-box');
  assert.equal(s.title, 'dev-box');
  assert.equal(s.label, 'Server: dev-box', 'aria-label comes from header_server_button_label');
  assert.equal(s.expanded, 'false');
});

test('before the hostname resolves the local button reads "This server", never "Local"', () => {
  const sb = loadApp();
  vm.runInContext('renderServerDropdown()', sb);
  const s = btn(sb);
  assert.equal(s.name, 'This server');
  assert.ok(!/local/i.test(s.name));
});

test('shows "All" in All mode with the All dot and the full name in the title', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = 'all'; renderServerDropdown()`, sb);
  const s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-all');
  assert.equal(s.name, 'All');
  assert.equal(s.title, 'All servers');
  assert.equal(s.label, 'Server: All servers');
});

test('shows the named peer with the peer dot', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; renderServerDropdown()`, sb);
  const s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-peer');
  assert.equal(s.name, PEER);
  assert.equal(s.label, 'Server: ' + PEER);
});

test('a long peer name truncates to about 14 characters with an ellipsis; the title keeps the full name', () => {
  const sb = loadApp();
  const longName = 'very-long-peer-hostname.example.internal';
  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(longName)}, label: ${JSON.stringify(longName)}, enabled: true }] };` +
    `state.activeServer = ${JSON.stringify(longName)}; renderServerDropdown()`, sb);
  const s = btn(sb);
  assert.equal(s.name.length, 14);
  assert.ok(s.name.endsWith('…'), 'ellipsis marks the cut');
  assert.equal(s.title, longName);
  assert.equal(s.label, 'Server: ' + longName);
});

test('an unreachable selected peer turns the dot to the warning state and says so in the title', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; _serverReachability[${JSON.stringify(PEER)}] = false; renderServerDropdown()`, sb);
  const s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-down');
  assert.ok(s.title.includes('(unreachable)'), s.title);
  assert.ok(s.label.includes('(unreachable)'), s.label);
  // A peer that is reachable (or not yet probed) keeps the peer dot.
  vm.runInContext(`_serverReachability[${JSON.stringify(PEER)}] = true; renderServerDropdown()`, sb);
  assert.equal(btn(sb).dot, 'header-server-dot is-peer');
});

test('a late reachability probe updates the dot via _updatePickerOptionReachability', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; renderServerDropdown()`, sb);
  assert.equal(btn(sb).dot, 'header-server-dot is-peer');
  vm.runInContext(`_serverReachability[${JSON.stringify(PEER)}] = false; _updatePickerOptionReachability(${JSON.stringify(PEER)})`, sb);
  assert.equal(btn(sb).dot, 'header-server-dot is-down');
});

test('rendering without the header markup in the DOM is a harmless no-op', () => {
  const sb = buildSandbox();
  vm.createContext(sb);
  loadScript(APP_JS, sb);
  assert.doesNotThrow(() => vm.runInContext('renderServerDropdown()', sb));
});

// ── 2. The modal lists All, this server, and every enabled peer ────────────

test('lists All + this server + every peer as buttons, in order, with a hostile peer name', () => {
  const sb = loadApp();
  const hostile = 'say "hi" & back\\slash <b>';
  const names = [];
  for (let i = 1; i <= 12; i++) names.push('relay-' + String(i).padStart(2, '0'));
  names[2] = hostile;
  vm.runInContext(
    `state.servers = { servers: ${JSON.stringify(names.map(n => ({ name: n, label: n, enabled: true, url: 'https://x' })))}` +
    ` .concat([{ name: 'retired-07', label: 'retired-07', enabled: false, url: 'https://old' }]) };`,
    sb,
  );
  openModal(sb);
  const values = rows(sb).map(r => r.getAttribute('data-server-name'));
  assert.equal(values[0], 'all', 'first entry is All');
  assert.equal(values[1], '', 'second entry is this server');
  assert.deepEqual(values.slice(2), names, 'all 12 enabled peers listed in order');
  assert.ok(!values.includes('retired-07'), 'a disabled peer is not listed');
  assert.ok(values.includes(hostile), 'hostile name round-trips through the data attribute');
  assert.equal(rows(sb).length, 2 + names.length, 'no extra/missing rows from the hostile name');
  rows(sb).forEach(r => {
    assert.equal(r.tagName, 'BUTTON', 'rows are real buttons (keyboard reachable)');
    assert.equal(r.getAttribute('type'), 'button');
    assert.ok(!r.hasAttribute('disabled'), 'a row is never disabled');
  });
  // The hostile name never made it into raw markup.
  assert.ok(!sb.__dom.byId('serverPickerModalList').innerHTML.includes('<b>'), 'peer name is escaped in the markup');
});

test('the active row carries aria-current and only the active row does', () => {
  const sb = loadApp();
  for (const [name, expected] of [[null, ''], ['all', 'all'], [PEER, PEER]]) {
    vm.runInContext(`state.activeServer = ${JSON.stringify(name)}`, sb);
    openModal(sb);
    const current = rows(sb).filter(r => r.getAttribute('aria-current') === 'true');
    assert.equal(current.length, 1, 'exactly one aria-current row for ' + String(name));
    assert.equal(current[0].getAttribute('data-server-name'), expected);
    vm.runInContext('closeServerPickerModal()', sb);
  }
});

test('an unreachable peer row is marked data-reachability="down" with the suffix and stays selectable', () => {
  const sb = loadApp();
  vm.runInContext(`_serverReachability[${JSON.stringify(PEER)}] = false`, sb);
  openModal(sb);
  const row = rowFor(sb, PEER);
  assert.equal(row.getAttribute('data-reachability'), 'down');
  assert.ok(row.textContent.includes('(unreachable)'), row.textContent);
  assert.ok(!row.hasAttribute('disabled'), 'unreachable is never disabled');
  // A peer with no known state renders unmarked.
  vm.runInContext(`delete _serverReachability[${JSON.stringify(PEER)}]; closeServerPickerModal()`, sb);
  openModal(sb);
  assert.equal(rowFor(sb, PEER).getAttribute('data-reachability'), 'unknown');
  assert.ok(!rowFor(sb, PEER).textContent.includes('(unreachable)'));
});

test('a late probe patches the open modal row in place (no repaint), by name match not selector', () => {
  const sb = loadApp();
  // A name that would break any selector built from it.
  const nasty = 'a"]; x[b="';
  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(nasty)}, label: ${JSON.stringify(nasty)}, enabled: true }, { name: 'other', label: 'other', enabled: true }] }`, sb);
  openModal(sb);
  const before = rows(sb);
  vm.runInContext(`_serverReachability[${JSON.stringify(nasty)}] = false; _updatePickerOptionReachability(${JSON.stringify(nasty)})`, sb);
  const after = rows(sb);
  assert.deepEqual(after, before, 'rows are the same elements: nothing was repainted');
  assert.equal(rowFor(sb, nasty).getAttribute('data-reachability'), 'down');
  assert.ok(rowFor(sb, nasty).textContent.includes('(unreachable)'));
  assert.equal(rowFor(sb, 'other').getAttribute('data-reachability'), 'unknown', 'only the matching row changed');
  assert.ok(!rowFor(sb, nasty).hasAttribute('disabled'));
  // Recovery clears the mark.
  vm.runInContext(`_serverReachability[${JSON.stringify(nasty)}] = true; _updatePickerOptionReachability(${JSON.stringify(nasty)})`, sb);
  assert.equal(rowFor(sb, nasty).getAttribute('data-reachability'), 'unknown');
  assert.ok(!rowFor(sb, nasty).textContent.includes('(unreachable)'));
});

test('the real hostname labels the this-server row once known', () => {
  const sb = loadApp();
  vm.runInContext('_localHostname = "dev-box"', sb);
  openModal(sb);
  assert.ok(rowFor(sb, '').textContent.includes('dev-box'));
  assert.ok(!/local/i.test(rowFor(sb, '').textContent));
});

// ── 3. Choosing a row calls selectServer; the choice is remembered ─────────

test('picking a peer row calls selectServer exactly once, selects it and closes the modal', () => {
  const sb = loadApp();
  openModal(sb);
  clickRow(sb, rowFor(sb, PEER));
  assert.deepEqual(sb.__calls, [PEER], 'one selectServer call per choice');
  assert.equal(active(sb), PEER);
  assert.equal(btn(sb).name, PEER, 'the header button follows the choice');
  assert.ok(sb.__toasts.some(t => t.includes(PEER)), 'toast names the peer');
  assert.equal(sb.__toasts.length, 1);
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'the modal closed');
});

test('picking All routes to selectServer("all")', () => {
  const sb = loadApp();
  openModal(sb);
  clickRow(sb, rowFor(sb, 'all'));
  assert.deepEqual(sb.__calls, ['all']);
  assert.equal(active(sb), 'all');
  assert.equal(btn(sb).dot, 'header-server-dot is-all');
});

test('picking this server returns to local with a single selectServer(null)', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  openModal(sb);
  clickRow(sb, rowFor(sb, ''));
  assert.deepEqual(sb.__calls, [null]);
  assert.equal(active(sb), null);
});

test('the back-to-local row (shown while a peer is active) returns to local and closes the modal', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  openModal(sb);
  const back = rows(sb).find(r => r.hasAttribute('data-back-to-local'));
  assert.ok(back, 'back-to-local row present while a peer is active');
  clickRow(sb, back);
  assert.deepEqual(sb.__calls, [null]);
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
  // Not offered when already local or in All mode.
  openModal(sb);
  assert.ok(!rows(sb).some(r => r.hasAttribute('data-back-to-local')));
});

test('the choice is remembered and restored on the next header render in the same browser', () => {
  const sb = loadApp();
  openModal(sb);
  clickRow(sb, rowFor(sb, PEER));
  const before = sb.__calls.length;

  // Next navigation: the header is re-initialised exactly how navigate()
  // does it. Same page, same browser — the operator's server choice must
  // still be there, and re-initialising must not switch servers again.
  vm.runInContext('_initServerDropdown()', sb);
  assert.equal(active(sb), PEER, 'activeServer survives the next header render');
  assert.equal(btn(sb).name, PEER, 'the button re-renders on the remembered peer');
  assert.deepEqual(sb.__calls.slice(before), [], 're-render must not call selectServer again');

  vm.runInContext('renderServerDropdown()', sb);
  assert.equal(btn(sb).name, PEER);
  assert.equal(sb.__calls.length, before, 'a plain re-render stays call-free');
});

// ── 4. The old chip row, topbar slot and every server <select> are gone ────

test('no .server-picker-bar, .sessions-topbar-servers or server <select> anywhere', () => {
  const appSrc = fs.readFileSync(APP_JS, 'utf8');
  const shellSrc = fs.readFileSync(INDEX_HTML, 'utf8');
  for (const src of [appSrc, shellSrc]) {
    assert.ok(!src.includes('class="server-picker-bar"'), 'markup class="server-picker-bar" must not exist');
    assert.ok(!src.includes('class="sessions-topbar-servers"'), 'markup class="sessions-topbar-servers" must not exist');
    assert.ok(!/id="serverDropdown"/.test(src), 'the header <select id="serverDropdown"> is gone');
    assert.ok(!/id="sessionsServerDropdown"/.test(src), 'the Sessions <select id="sessionsServerDropdown"> is gone');
  }
  const sb = loadApp();
  for (const gone of ['_serverPickerBar', '_sessionsServerSelectHTML', 'onServerDropdownChange', '_pickerDropdownEls']) {
    assert.equal(vm.runInContext(`typeof ${gone}`, sb), 'undefined', gone + ' is removed');
  }

  // The Sessions page body — rendered with a peer active, the state where
  // the old slot used to show its own chip row — has no server select.
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  vm.runInContext('renderSessionsView()', sb);
  const page = sb.__dom.el('view').innerHTML;
  assert.ok(page !== '', 'sessions view rendered into #view');
  assert.ok(!page.includes('server-picker-bar'), 'sessions page has no chip row');
  assert.ok(!page.includes('sessions-topbar-servers'), 'sessions page has no topbar server slot');
  assert.ok(!/ServerDropdown/.test(page), 'sessions page has no server select');
  assert.ok(!page.includes('sessions-server-select'), 'sessions page has no server select class');
});

// ── 5. Shared header modal helpers ─────────────────────────────────────────

test('the modal is a labelled dialog on the existing confirm-modal classes', () => {
  const sb = loadApp();
  const modal = openModal(sb);
  assert.equal(modal.className, 'confirm-modal-overlay');
  const dlg = modal.querySelector('[role="dialog"]');
  assert.ok(dlg, 'role="dialog" element present');
  assert.ok(dlg.className.split(/\s+/).includes('confirm-modal'));
  assert.equal(dlg.getAttribute('aria-modal'), 'true');
  const labelId = dlg.getAttribute('aria-labelledby');
  assert.ok(labelId, 'aria-labelledby present');
  const title = sb.__dom.byId(labelId);
  assert.ok(title, 'aria-labelledby points at a real element');
  assert.equal(title.textContent, 'Servers');
});

test('opening sets aria-expanded on the opener; closing resets it and returns focus to it', () => {
  const sb = loadApp();
  const opener = sb.__dom.el('serverPickerBtn');
  opener.focus();
  openModal(sb);
  assert.equal(opener.getAttribute('aria-expanded'), 'true');
  vm.runInContext('renderServerDropdown()', sb);
  assert.equal(opener.getAttribute('aria-expanded'), 'true', 'a re-render keeps it in step with the open modal');
  vm.runInContext('closeServerPickerModal()', sb);
  assert.equal(opener.getAttribute('aria-expanded'), 'false');
  assert.equal(sb.__dom.active(), opener, 'focus returned to the opener button');
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
});

test('focus moves into the dialog: the active row first', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  openModal(sb);
  assert.equal(sb.__dom.active(), rowFor(sb, PEER), 'the aria-current row holds focus');
  vm.runInContext('closeServerPickerModal()', sb);
  vm.runInContext(`state.activeServer = 'all'`, sb);
  openModal(sb);
  assert.equal(sb.__dom.active(), rowFor(sb, 'all'));
});

test('while the server list is still loading focus lands on the first button, then moves to the active row', () => {
  const sb = loadApp();
  vm.runInContext('state.servers = undefined; state._serversLoading = true', sb);
  openModal(sb);
  const first = sb.__dom.byId('serverPickerModal').querySelector('button');
  assert.equal(sb.__dom.active(), first, 'first button (no rows yet) holds focus');
  assert.equal(rows(sb).length, 0, 'list not painted yet');
  // The list arrives.
  vm.runInContext(`state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, enabled: true }] }; state.activeServer = ${JSON.stringify(PEER)}`, sb);
  vm.runInContext('_loadServerPickerModalList()', sb);
  assert.equal(sb.__dom.active(), rowFor(sb, PEER), 'focus follows to the active row');
});

test('a repaint keeps focus on the row the operator is on', () => {
  const sb = loadApp();
  openModal(sb);
  rowFor(sb, PEER).focus();
  vm.runInContext('_loadServerPickerModalList()', sb);
  assert.equal(sb.__dom.active(), rowFor(sb, PEER), 'focus restored to the same row after the repaint');
});

test('Tab is trapped inside the dialog, both directions', () => {
  const sb = loadApp();
  const modal = openModal(sb);
  const items = modal.querySelectorAll('button, a[href]');
  assert.ok(items.length >= 4, 'help link, rows, add and close are focusable');
  const first = items[0];
  const last = items[items.length - 1];

  last.focus();
  let prevented = 0;
  sb.__dom.fire('keydown', { key: 'Tab', shiftKey: false, preventDefault() { prevented++; } });
  assert.equal(sb.__dom.active(), first, 'Tab on the last control wraps to the first');
  assert.equal(prevented, 1);

  first.focus();
  sb.__dom.fire('keydown', { key: 'Tab', shiftKey: true, preventDefault() { prevented++; } });
  assert.equal(sb.__dom.active(), last, 'Shift+Tab on the first control wraps to the last');

  // Tab from somewhere in the middle is left to the browser.
  items[1].focus();
  const midBefore = prevented;
  sb.__dom.fire('keydown', { key: 'Tab', shiftKey: false, preventDefault() { prevented++; } });
  assert.equal(prevented, midBefore, 'a mid-dialog Tab is not intercepted');

  // Focus that escaped the dialog is pulled back in.
  sb.__dom.body.focus();
  sb.__dom.fire('keydown', { key: 'Tab', shiftKey: false, preventDefault() {} });
  assert.equal(sb.__dom.active(), first);
});

test('Escape closes the modal, resets the opener and removes the document keydown listener', () => {
  const sb = loadApp();
  const opener = sb.__dom.el('serverPickerBtn');
  assert.equal(sb.__dom.listenerCount('keydown'), 0, 'baseline: no keydown listeners');
  openModal(sb);
  assert.equal(sb.__dom.listenerCount('keydown'), 1, 'one listener while open');
  sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'Escape closed it');
  assert.equal(opener.getAttribute('aria-expanded'), 'false');
  assert.equal(sb.__dom.active(), opener);
  assert.equal(sb.__dom.listenerCount('keydown'), 0, 'listener removed on close');
});

test('a backdrop click closes; a click inside the dialog does not', () => {
  const sb = loadApp();
  const modal = openModal(sb);
  modal.dispatch('click', { target: modal.querySelector('[role="dialog"]') });
  assert.ok(sb.__dom.byId('serverPickerModal'), 'click inside the dialog leaves it open');
  modal.dispatch('click', { target: modal });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'backdrop click closed it');
  assert.equal(sb.__dom.listenerCount('keydown'), 0);
});

test('every close path ends in closeServerPickerModal: the Close button, add-server and a row', () => {
  const sb = loadApp();
  const real = sb.closeServerPickerModal;
  let closes = 0;
  sb.closeServerPickerModal = function () { closes++; return real.apply(this, arguments); };

  openModal(sb);
  const close = sb.__dom.byId('serverPickerModal').querySelector('.header-modal-close');
  assert.ok(close, 'Close button present');
  vm.runInContext(close.getAttribute('onclick'), sb);
  assert.equal(closes, 1);
  assert.equal(sb.__dom.byId('serverPickerModal'), null);

  openModal(sb);
  clickRow(sb, rowFor(sb, PEER));
  assert.equal(closes, 2, 'a row click closes through the one function');

  // add-server: navigates to Settings, closing the modal first.
  sb.navigate = () => {};
  sb.showServerForm = () => {};
  openModal(sb);
  vm.runInContext('_openAddServerFromPicker()', sb);
  assert.equal(closes, 3, 'add-server closes through the one function');
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
});

test('reopening replaces the modal: one overlay, one keydown listener', () => {
  const sb = loadApp();
  openModal(sb);
  openModal(sb);
  assert.equal(sb.__dom.body.children.filter(c => c.id === 'serverPickerModal').length, 1);
  assert.equal(sb.__dom.listenerCount('keydown'), 1);
  vm.runInContext('closeServerPickerModal()', sb);
  assert.equal(sb.__dom.listenerCount('keydown'), 0);
  // Closing twice is harmless.
  assert.doesNotThrow(() => vm.runInContext('closeServerPickerModal()', sb));
});

test('the title row carries a "?" help link to the multi-servers how-to', () => {
  const sb = loadApp();
  const modal = openModal(sb);
  const help = modal.querySelector('a[href]');
  assert.ok(help, 'help link present');
  assert.equal(help.textContent, '?');
  assert.ok(help.getAttribute('href').includes('docs/howto/multi-servers.md'), help.getAttribute('href'));
  assert.equal(help.getAttribute('target'), '_blank');
  assert.equal(help.getAttribute('rel'), 'noopener');
  assert.equal(help.getAttribute('aria-label'), 'Help: switching servers', 'label from header_server_help_label');
  const titleRow = modal.querySelector('.header-modal-title-row');
  assert.ok(titleRow && titleRow.contains(help), 'the link sits in the title row');
});

test('the shared helpers work for another modal id: Escape closes only the topmost', () => {
  const sb = loadApp();
  vm.runInContext(`_headerModalShow({ id: 'scopeModal', titleId: 'scopeModalTitle', titleText: 'Scope', bodyHtml: '<button type="button">ok</button>', openerId: 'serverPickerBtn' })`, sb);
  openModal(sb);
  assert.equal(sb.__dom.listenerCount('keydown'), 2);
  sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'the topmost (picker) closed');
  assert.ok(sb.__dom.byId('scopeModal'), 'the one underneath stays open');
  sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} });
  assert.equal(sb.__dom.byId('scopeModal'), null);
  assert.equal(sb.__dom.listenerCount('keydown'), 0);
});

// ── 6. The static shell ────────────────────────────────────────────────────

test('the static shell in index.html carries the header server button, visible before JS', () => {
  const src = fs.readFileSync(INDEX_HTML, 'utf8');
  const m = src.match(/<button type="button" id="serverPickerBtn"[\s\S]*?<\/button>/);
  assert.ok(m, 'header <button id="serverPickerBtn"> present in the static shell');
  const b = m[0];
  assert.ok(b.includes('class="header-server-btn"'));
  assert.ok(b.includes('aria-haspopup="dialog"'));
  assert.ok(b.includes('aria-expanded="false"'));
  assert.ok(b.includes('onclick="openServerPickerModal()"'), 'opens the shared picker modal');
  assert.match(b, /<svg[^>]*aria-hidden="true"[^>]*focusable="false"/, 'decorative icon is hidden from assistive tech');
  assert.ok(b.includes('id="serverPickerDot"'), 'status dot present');
  assert.ok(b.includes('id="serverPickerName"'), 'short-name span present');
  assert.ok(b.includes('aria-label="Server: This server"'), 'accessible name before the first JS render');
});

test('the static shell has no <select> in the header', () => {
  const src = fs.readFileSync(INDEX_HTML, 'utf8');
  const header = src.slice(src.indexOf('<header class="header">'), src.indexOf('</header>'));
  assert.ok(header.length > 100, 'header block found');
  assert.ok(!/<select/.test(header), 'no server <select> in the header');
});

test('the three-finger swipe gesture still opens the same modal', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const m = src.match(/\(function initThreeFingerSwipeGesture\(\) \{[\s\S]*?\}\)\(\);/);
  assert.ok(m, 'gesture initialiser present');
  assert.ok(m[0].includes('openServerPickerModal();'), 'the gesture calls openServerPickerModal()');
  assert.ok(m[0].includes('THRESHOLD_PX = 64') && m[0].includes('DEBOUNCE_MS = 500'), 'gesture constants unchanged');
});

// Picks an entry through the modal the way an operator does ('' = this server).
function choose(sb, value) {
  openModal(sb);
  clickRow(sb, rowFor(sb, value));
}

// ── 7. The choice is persisted (dw_server_choice) and restored at boot ────

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

test('a header render after boot commit shows the restored entry', () => {
  const sb = loadApp();
  sb.localStorage.setItem('dw_server_choice', PEER);
  vm.runInContext("state.activeServer = 'all'", sb); // pre-commit state
  // _onServerListReady runs its callback via setTimeout when the list is
  // already loaded — the sandbox timer is a no-op, so fire it inline.
  sb.setTimeout = (fn) => { fn(); return 0; };
  vm.runInContext('_serverChoiceSettleBoot()', sb);
  assert.equal(active(sb), PEER, 'the commit is synchronous once the list is known');
  vm.runInContext('renderServerDropdown()', sb);
  assert.equal(btn(sb).name, PEER, 'first paint is already on the restored server');
  assert.equal(btn(sb).dot, 'header-server-dot is-peer');
});

// ── 8. Styles and locale keys ──────────────────────────────────────────────

const CSS = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');

function cssRule(selector) {
  const esc = selector.replace(/[.*+?^${}()|[\]\\>]/g, '\\$&');
  const m = CSS.match(new RegExp('(?:^|\\n)' + esc + '\\s*\\{([^}]*)\\}'));
  assert.ok(m, 'rule for ' + selector + ' present in style.css');
  return m[1];
}

test('style.css: .header-server-btn replaces .server-dropdown and the Sessions select rules', () => {
  assert.ok(!/\.server-dropdown/.test(CSS), 'no .server-dropdown rule remains');
  assert.ok(!/sessions-server-select/.test(CSS), 'no .sessions-server-select rule remains');
  const b = cssRule('.header > .header-server-btn');
  assert.match(b, /flex:\s*0 0 auto/, 'the button never grows or shrinks away');
  assert.match(b, /min-width:\s*44px/, 'hit target matches the other header buttons');
  assert.match(b, /min-height:\s*44px/);
  assert.match(b, /white-space:\s*nowrap/);
});

test('style.css: the header stays one non-wrapping line and the title takes the spare space', () => {
  const header = cssRule('.header');
  assert.match(header, /display:\s*flex/);
  assert.doesNotMatch(header, /flex-wrap:\s*wrap/, 'the header never wraps');
  const title = cssRule('.header-title');
  assert.match(title, /flex:\s*1 1 auto/);
  assert.match(title, /min-width:\s*0/);
  assert.match(title, /text-overflow:\s*ellipsis/);
  assert.match(title, /white-space:\s*nowrap/);
});

test('style.css: only the icon and dot show on phones; the short name (max about 14ch) shows on wide screens', () => {
  const name = cssRule('.header-server-name');
  assert.match(name, /max-width:\s*14ch/);
  assert.match(name, /text-overflow:\s*ellipsis/);
  assert.match(name, /display:\s*inline-block/, 'visible by default (wide screens)');
  assert.match(CSS, /@media \(max-width: 600px\) \{\s*\.header-server-name \{ display: none; \}\s*\}/, 'hidden at the phone breakpoint');
  // One dot colour per state, all from theme tokens.
  for (const k of ['is-local', 'is-peer', 'is-all', 'is-down']) {
    assert.match(cssRule('.header-server-dot.' + k), /background:\s*var\(--[a-z0-9]+\)/, k + ' uses a theme token');
  }
});

test('style.css: the expand toggle is hidden at phone width, where it has no effect', () => {
  assert.match(CSS, /@media \(max-width: 600px\) \{\s*#headerFullscreenBtn \{ display: none !important; \}/);
});

test('every locale carries the header button, help and modal-close keys, without tracker ids', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', lang + '.json'), 'utf8'));
    for (const k of ['header_server_button_label', 'header_server_help_label', 'header_modal_close']) {
      assert.ok(bundle[k] && bundle[k].length > 0, lang + ' has ' + k);
      assert.doesNotMatch(bundle[k], /\b(BL|GH|SEC|HLLM)-?\d+\b|\b[BG]\d{2,}\b/, lang + ' ' + k + ' has no tracker id');
    }
    assert.match(bundle.header_server_button_label, /%1\$s/, lang + ' label interpolates the server name');
  }
  const en = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  assert.equal(en.header_server_button_label, 'Server: %1$s');
});
