'use strict';
// Header server button (#serverPickerBtn) — behaviour pin.
//
// The app header carries exactly ONE server control: a compact button that
// opens the server picker modal. This file pins what an operator (and a
// screen-reader or keyboard user) can rely on:
//   (a) one #serverPickerBtn with an accessible name; no server <select> in
//       the static shell;
//   (b) the button reflects this server, All and a named peer — including a
//       peer name full of quotes, angle brackets and backslashes, which is
//       rendered escaped and never breaks the markup;
//   (c) the modal's dialog semantics, focus handling, Escape / close button /
//       backdrop close, and that no document keydown listener leaks across
//       repeated open/close cycles;
//   (d) choosing a row calls selectServer exactly once and closes the modal;
//       the three-finger swipe-up opens the same modal, a two-finger one does
//       not;
//   (e) the Sessions page has no server selector of its own;
//   (f) phone width: the header stays on one non-wrapping line, the title
//       truncates with an ellipsis, the short name is hidden on phones and
//       shown above the breakpoint;
//   (g) unreachable peers carry the suffix and data-reachability="down" in the
//       modal and stay selectable.
//
// No daemon, no tmux: the real app.js runs in a vm sandbox, exactly like the
// other PWA tests. Helpers and the fake DOM are shared with
// app-header-server-dropdown.test.js.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-header-server-button.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const { installFakeDom } = require('./testutil_fake_dom');

const WEB = __dirname;
const APP_JS = path.join(WEB, 'app.js');
const INDEX_HTML = path.join(WEB, 'index.html');
const STYLE_CSS = path.join(WEB, 'style.css');
const PEER = 'pi-node';
const HOSTILE = 'say "hi" & <b>x</b> back\\slash \'q\' </button>';

function loadApp() {
  const sandbox = buildSandbox();
  const dom = installFakeDom(sandbox, { statics: ['serverPickerBtn', 'serverPickerDot', 'serverPickerName', 'view'] });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // Real English strings: the i18n stub returns raw keys without the bundle.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));

  assert.equal(typeof sandbox.selectServer, 'function', 'selectServer is not defined — renamed?');
  const realSelect = sandbox.selectServer;
  sandbox.__calls = [];
  sandbox.selectServer = function (name) {
    sandbox.__calls.push(name);
    return realSelect.apply(this, arguments);
  };

  // Neutralise the network/WS fan-out of a server switch; keep the real state
  // mutation and the button re-render (the subject under test).
  sandbox.showToast = () => {};
  sandbox.connect = () => {};
  sandbox.loadServers = () => {};
  sandbox.loadServersList = () => {};
  sandbox._ensureLocalHostname = () => {};
  sandbox._checkFederatedConnection = () => {};
  sandbox._loadAllServersSessions = () => {};
  sandbox._probePickerReachability = () => {};

  vm.runInContext(
    `state.servers = { servers: [{ name: ${JSON.stringify(PEER)}, label: ${JSON.stringify(PEER)}, enabled: true, url: 'https://x' }] };` +
    `state.activeServer = null; state.sessions = []; state.ws = null; state.activeView = 'sessions';`,
    sandbox,
  );
  sandbox.__dom = dom;
  return sandbox;
}

function setServers(sb, names) {
  const list = names.map(n => ({ name: n, label: n, enabled: true, url: 'https://x' }));
  vm.runInContext(`state.servers = { servers: ${JSON.stringify(list)} }`, sb);
}

function active(sb) { return vm.runInContext('state.activeServer', sb); }

// The button's observable state.
function btn(sb) {
  const b = sb.__dom.el('serverPickerBtn');
  return {
    el: b,
    label: b.getAttribute('aria-label'),
    title: b.getAttribute('title'),
    expanded: b.getAttribute('aria-expanded'),
    dot: sb.__dom.el('serverPickerDot').className,
    nameEl: sb.__dom.el('serverPickerName'),
    name: sb.__dom.el('serverPickerName').textContent,
  };
}

function openViaButton(sb) {
  // The shell's onclick is "openServerPickerModal()"; run it the way the
  // browser does (global handler resolution).
  const onclick = fs.readFileSync(INDEX_HTML, 'utf8').match(/id="serverPickerBtn"[^>]*onclick="([^"]*)"/);
  assert.ok(onclick, 'the shell button carries an onclick');
  vm.runInContext(onclick[1], sb);
  const modal = sb.__dom.byId('serverPickerModal');
  assert.ok(modal, 'serverPickerModal overlay is in the DOM');
  return modal;
}

function rows(sb) {
  const list = sb.__dom.byId('serverPickerModalList');
  return list ? list.querySelectorAll('button') : [];
}

// Rows by data-server-name — never a selector built from a name.
function rowFor(sb, name) {
  const hit = rows(sb).find(r => r.getAttribute('data-server-name') === name);
  assert.ok(hit, 'no row for ' + JSON.stringify(name));
  return hit;
}

function clickRow(sb, row) { vm.runInContext(row.getAttribute('onclick'), sb); }

function touches(ys) { return ys.map((y, i) => ({ clientX: 40 + i * 30, clientY: y })); }

// ── (a) one button, an accessible name, no server <select> ─────────────────

test('(a) the header holds exactly one #serverPickerBtn button with an accessible name', () => {
  const src = fs.readFileSync(INDEX_HTML, 'utf8');
  const header = src.slice(src.indexOf('<header class="header">'), src.indexOf('</header>'));
  assert.ok(header.length > 100, 'header block found');

  const ids = header.match(/id="serverPickerBtn"/g) || [];
  assert.equal(ids.length, 1, 'exactly one element with id serverPickerBtn in the header');
  assert.equal((src.match(/id="serverPickerBtn"/g) || []).length, 1, 'and exactly one in the whole shell');

  const tag = header.match(/<button\b[^>]*id="serverPickerBtn"[^>]*>/);
  assert.ok(tag, 'serverPickerBtn is a <button>');
  assert.match(tag[0], /type="button"/);
  const label = tag[0].match(/aria-label="([^"]*)"/);
  assert.ok(label && label[1].trim().length > 0, 'static aria-label gives it an accessible name before JS');

  // Only one control in the header is a server selector.
  const serverButtons = header.match(/<button\b[^>]*(?:server|Server)[^>]*>/g) || [];
  assert.equal(serverButtons.length, 1, 'one server button in the header: ' + serverButtons.join(' | '));
});

test('(a) the live aria-label comes from header_server_button_label, in every state', () => {
  const sb = loadApp();
  const en = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  assert.ok(en.header_server_button_label.includes('%1$s'), 'the key interpolates the server name');
  const expect = (name) => en.header_server_button_label.replace('%1$s', name);

  vm.runInContext('_localHostname = "dev-box"; renderServerDropdown()', sb);
  assert.equal(btn(sb).label, expect('dev-box'));
  vm.runInContext(`state.activeServer = 'all'; renderServerDropdown()`, sb);
  assert.equal(btn(sb).label, expect('All servers'));
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; renderServerDropdown()`, sb);
  assert.equal(btn(sb).label, expect(PEER));
  for (const s of [btn(sb)]) assert.ok(s.label && s.label.trim(), 'never empty');
});

test('(a) no server <select> anywhere in index.html', () => {
  const src = fs.readFileSync(INDEX_HTML, 'utf8');
  assert.ok(!/<select\b[^>]*server/i.test(src), 'no <select> mentioning "server" in the shell');
  assert.ok(!/id="serverDropdown"/.test(src), 'the old header select id is gone');
  const header = src.slice(src.indexOf('<header class="header">'), src.indexOf('</header>'));
  assert.ok(!/<select/i.test(header), 'no <select> at all in the header');
  // app.js must not inject one into the document either.
  const app = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(!/id="serverDropdown"/.test(app) && !/id="sessionsServerDropdown"/.test(app));
});

// ── (b) the button reflects this server, All and a named peer ──────────────

test('(b) the button reflects this server, All and a named peer', () => {
  const sb = loadApp();
  vm.runInContext('_localHostname = "dev-box"; renderServerDropdown()', sb);
  let s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-local');
  assert.equal(s.name, 'dev-box');
  assert.equal(s.title, 'dev-box');

  vm.runInContext(`state.activeServer = 'all'; renderServerDropdown()`, sb);
  s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-all');
  assert.equal(s.name, 'All');
  assert.equal(s.title, 'All servers');

  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}; renderServerDropdown()`, sb);
  s = btn(sb);
  assert.equal(s.dot, 'header-server-dot is-peer');
  assert.equal(s.name, PEER);
  assert.equal(s.title, PEER);
  assert.equal(s.label, 'Server: ' + PEER);
});

test('(b) a peer name with quotes, angle brackets and backslashes never breaks the button markup', () => {
  const sb = loadApp();
  setServers(sb, [HOSTILE]);
  vm.runInContext(`state.activeServer = ${JSON.stringify(HOSTILE)}; renderServerDropdown()`, sb);
  const s = btn(sb);

  // The name goes in as text and attribute values — the full name is
  // preserved in the title and accessible name exactly as typed.
  assert.equal(s.title, HOSTILE);
  assert.equal(s.label, 'Server: ' + HOSTILE);
  // The visible short name is plain text (cut with an ellipsis), no element.
  assert.equal(s.name.length, 14, 'cut to the 14-character cap');
  assert.ok(s.name.endsWith('…'), 'with an ellipsis');
  assert.ok(s.name.startsWith('say "hi" & <b'), 'text carries the raw characters: ' + s.name);
  assert.equal(s.nameEl.children.length, 0, 'the name span holds text, never parsed markup');
  assert.equal(s.el.children.length, 0, 'the button gained no child elements from the name');
  assert.equal(s.expanded, 'false');
  assert.equal(s.dot, 'header-server-dot is-peer');
});

test('(b) the hostile peer name is escaped in the modal and selecting it round-trips exactly', () => {
  const sb = loadApp();
  setServers(sb, [PEER, HOSTILE, 'tail']);
  const modal = openViaButton(sb);
  const list = sb.__dom.byId('serverPickerModalList');

  assert.deepEqual(
    rows(sb).map(r => r.getAttribute('data-server-name')),
    ['all', '', PEER, HOSTILE, 'tail'],
    'every row present once, in order — the name did not split or swallow rows',
  );
  assert.equal(list.querySelectorAll('b').length, 0, 'no <b> element was injected');
  assert.ok(!list.innerHTML.includes('<b>x</b>'), 'angle brackets are escaped in the markup');
  assert.equal(modal.querySelectorAll('[role="dialog"]').length, 1, 'still exactly one dialog');
  rows(sb).forEach(r => assert.equal(r.tagName, 'BUTTON'));

  clickRow(sb, rowFor(sb, HOSTILE));
  assert.deepEqual(sb.__calls, [HOSTILE], 'the exact name reaches selectServer');
  assert.equal(active(sb), HOSTILE);
  assert.equal(btn(sb).title, HOSTILE);
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
});

// ── (c) opening, focus, closing, no leaked listeners ───────────────────────

test('(c) clicking the button opens #serverPickerModal as a labelled modal dialog with focus inside', () => {
  const sb = loadApp();
  const opener = sb.__dom.el('serverPickerBtn');
  vm.runInContext('renderServerDropdown()', sb); // the static shell already says "false"; sync like a navigation
  opener.focus();
  assert.equal(opener.getAttribute('aria-expanded'), 'false');

  const modal = openViaButton(sb);
  assert.equal(modal.id, 'serverPickerModal');
  const dlg = modal.querySelector('[role="dialog"]');
  assert.ok(dlg, 'role="dialog" present');
  assert.equal(dlg.getAttribute('aria-modal'), 'true');
  const labelId = dlg.getAttribute('aria-labelledby');
  assert.ok(labelId, 'aria-labelledby present');
  const title = sb.__dom.byId(labelId);
  assert.ok(title && title.textContent.trim().length > 0, 'aria-labelledby points at a titled element');
  assert.ok(dlg.contains(title), 'the title lives inside the dialog');

  assert.ok(dlg.contains(sb.__dom.active()), 'focus is inside the dialog');
  assert.notEqual(sb.__dom.active(), opener);
  assert.equal(opener.getAttribute('aria-expanded'), 'true', 'the opener reports expanded');
});

test('(c) Escape closes, focus returns to the button, and the keydown listener is gone — twice over', () => {
  const sb = loadApp();
  const opener = sb.__dom.el('serverPickerBtn');
  assert.equal(sb.__dom.listenerCount('keydown'), 0, 'baseline: no document keydown listener');

  for (let round = 1; round <= 2; round++) {
    opener.focus();
    openViaButton(sb);
    assert.equal(sb.__dom.listenerCount('keydown'), 1, `round ${round}: exactly one listener while open`);
    sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} });
    assert.equal(sb.__dom.byId('serverPickerModal'), null, `round ${round}: Escape closed the modal`);
    assert.equal(sb.__dom.active(), opener, `round ${round}: focus returned to the button`);
    assert.equal(opener.getAttribute('aria-expanded'), 'false', `round ${round}: aria-expanded reset`);
    assert.equal(sb.__dom.listenerCount('keydown'), 0, `round ${round}: the keydown listener is gone (no leak)`);
  }

  // A stray Escape with nothing open does nothing and nothing is left behind.
  assert.doesNotThrow(() => sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} }));
});

test('(c) the close button closes the modal, returns focus and removes the listener', () => {
  const sb = loadApp();
  const opener = sb.__dom.el('serverPickerBtn');
  for (let round = 1; round <= 2; round++) {
    opener.focus();
    const modal = openViaButton(sb);
    const close = modal.querySelector('.header-modal-close');
    assert.ok(close, 'close button present');
    vm.runInContext(close.getAttribute('onclick'), sb);
    assert.equal(sb.__dom.byId('serverPickerModal'), null, `round ${round}: closed`);
    assert.equal(sb.__dom.active(), opener);
    assert.equal(opener.getAttribute('aria-expanded'), 'false');
    assert.equal(sb.__dom.listenerCount('keydown'), 0);
  }
});

test('(c) a backdrop click closes the modal; a click inside the dialog does not', () => {
  const sb = loadApp();
  const opener = sb.__dom.el('serverPickerBtn');
  opener.focus();
  const modal = openViaButton(sb);
  modal.dispatch('click', { target: modal.querySelector('[role="dialog"]') });
  assert.ok(sb.__dom.byId('serverPickerModal'), 'a click inside the dialog leaves it open');
  assert.equal(opener.getAttribute('aria-expanded'), 'true');

  modal.dispatch('click', { target: modal });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'the backdrop click closed it');
  assert.equal(sb.__dom.active(), opener);
  assert.equal(opener.getAttribute('aria-expanded'), 'false');
  assert.equal(sb.__dom.listenerCount('keydown'), 0);
});

// ── (d) choosing a row; the three-finger swipe ─────────────────────────────

test('(d) choosing a row calls selectServer exactly once with "all", null or the peer name, and closes the modal', () => {
  for (const [value, expected] of [['all', 'all'], ['', null], [PEER, PEER]]) {
    const sb = loadApp();
    // Start from a different server so the choice is a real switch.
    vm.runInContext(`state.activeServer = ${value === PEER ? "'all'" : JSON.stringify(PEER)}`, sb);
    openViaButton(sb);
    clickRow(sb, rowFor(sb, value));
    assert.equal(sb.__calls.length, 1, `one selectServer call for ${JSON.stringify(value)}`);
    assert.strictEqual(sb.__calls[0], expected, 'argument is ' + JSON.stringify(expected));
    assert.equal(active(sb), expected);
    assert.equal(sb.__dom.byId('serverPickerModal'), null, 'the modal closed');
    assert.equal(sb.__dom.listenerCount('keydown'), 0, 'and left no listener behind');
    assert.equal(sb.__dom.el('serverPickerBtn').getAttribute('aria-expanded'), 'false');
  }
});

test('(d) a three-finger swipe up opens the same modal as the button', () => {
  const sb = loadApp();
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
  sb.__dom.fire('touchstart', { touches: touches([400, 400, 400]) });
  sb.__dom.fire('touchmove', { touches: touches([400 - 70, 400 - 70, 400 - 70]) });
  const viaSwipe = sb.__dom.byId('serverPickerModal');
  assert.ok(viaSwipe, 'the swipe opened the picker modal');
  assert.equal(viaSwipe.id, 'serverPickerModal');
  const swipeValues = rows(sb).map(r => r.getAttribute('data-server-name'));
  assert.deepEqual(swipeValues, ['all', '', PEER]);
  assert.ok(viaSwipe.querySelector('[role="dialog"]'));

  // Same modal as the button path: same id, same rows, and closes the same way.
  sb.__dom.fire('keydown', { key: 'Escape', preventDefault() {} });
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
  const viaButton = openViaButton(sb);
  assert.equal(viaButton.id, viaSwipe.id);
  assert.deepEqual(rows(sb).map(r => r.getAttribute('data-server-name')), swipeValues);
});

test('(d) a swipe shorter than 64px does not open the modal', () => {
  const sb = loadApp();
  sb.__dom.fire('touchstart', { touches: touches([400, 400, 400]) });
  sb.__dom.fire('touchmove', { touches: touches([400 - 63, 400 - 63, 400 - 63]) });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, '63px is below the threshold');
  sb.__dom.fire('touchmove', { touches: touches([400 - 64, 400 - 64, 400 - 64]) });
  assert.ok(sb.__dom.byId('serverPickerModal'), 'exactly 64px up triggers it');
});

test('(d) a two-finger swipe does not open the modal; neither does a downward three-finger one', () => {
  const sb = loadApp();
  sb.__dom.fire('touchstart', { touches: touches([400, 400]) });
  sb.__dom.fire('touchmove', { touches: touches([300, 300]) });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'two touches never trigger the gesture');

  // Three fingers start, then a finger lifts mid-swipe.
  sb.__dom.fire('touchstart', { touches: touches([400, 400, 400]) });
  sb.__dom.fire('touchmove', { touches: touches([300, 300]) });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'dropping to two touches cancels it');

  // Three fingers swiping down.
  sb.__dom.fire('touchstart', { touches: touches([400, 400, 400]) });
  sb.__dom.fire('touchmove', { touches: touches([500, 500, 500]) });
  assert.equal(sb.__dom.byId('serverPickerModal'), null, 'a downward swipe is not the gesture');
});

// ── (e) the Sessions page has no server selector of its own ────────────────

// Every <select> tag in a chunk of markup, as strings.
function selectTags(html) { return html.match(/<select\b[^>]*>/gi) || []; }

test('(e) the Sessions toolbar renderer emits no server <select> and no #sessionsServerDropdown', () => {
  const sb = loadApp();
  vm.runInContext(`state.activeServer = ${JSON.stringify(PEER)}`, sb);
  // The real renderer renderSessionsView() calls for the toolbar.
  const toolbar = vm.runInContext(`renderSessionsToolbar('', false, [], '', 'list', 0)`, sb);
  assert.ok(toolbar.includes('sessions-topbar'), 'the real toolbar markup was produced');
  assert.ok(!toolbar.includes('sessionsServerDropdown'), 'no #sessionsServerDropdown');
  assert.ok(!/server/i.test(selectTags(toolbar).join(' ')), 'no <select> in the toolbar is a server selector: ' + selectTags(toolbar).join(' '));
  assert.ok(!toolbar.includes('server-picker-bar'));
  assert.ok(!toolbar.includes('sessions-topbar-servers'));
  assert.ok(!toolbar.includes('selectServer('), 'nothing in the toolbar switches servers');
});

test('(e) the whole Sessions page — in every server mode — has exactly one server selector: the header button', () => {
  const sb = loadApp();
  // Past the first-load skeleton and the empty state, so the real page
  // (toolbar included) renders.
  vm.runInContext(
    `state._sessionsLoad = { loading: false, loaded: true, at: 1, error: '', next: '', total: 1, loadingMore: false, moreError: '', timer: null };` +
    `state.sessions = [{ id: 'a1', full_id: 'h-a1', name: 'one', state: 'running', backend: 'claude-code', updated_at: new Date().toISOString(), hostname: 'h' }];`,
    sb);
  for (const mode of [null, 'all', PEER]) {
    vm.runInContext(`state.activeServer = ${JSON.stringify(mode)}`, sb);
    vm.runInContext('renderSessionsView()', sb);
    const page = sb.__dom.el('view').innerHTML;
    assert.ok(page.includes('sessions-topbar'), 'sessions page rendered for ' + String(mode));
    assert.ok(!page.includes('sessionsServerDropdown'), 'no #sessionsServerDropdown');
    assert.ok(!page.includes('serverPickerBtn'), 'the page body holds no second server button');
    assert.ok(!/server/i.test(selectTags(page).join(' ')), 'no server <select> in the page body: ' + selectTags(page).join(' '));
    assert.ok(!/selectServer\(/.test(page), 'no inline server switch in the page body');
  }
  // The one selector is the header button, in the static shell.
  const shell = fs.readFileSync(INDEX_HTML, 'utf8');
  assert.equal((shell.match(/id="serverPickerBtn"/g) || []).length, 1);
  assert.equal(selectTags(shell).filter(t => /server/i.test(t)).length, 0);
});

// ── (f) phone width, from style.css ────────────────────────────────────────

// A small CSS reader: strips comments and returns every rule with the chain of
// @media conditions it sits in. Good enough for this stylesheet's plain
// nested-at-rule structure.
function parseCss(src) {
  const css = src.replace(/\/\*[\s\S]*?\*\//g, '');
  const rules = [];
  function walk(text, media) {
    let i = 0;
    while (i < text.length) {
      const open = text.indexOf('{', i);
      if (open < 0) break;
      const head = text.slice(i, open).trim();
      let depth = 1;
      let j = open + 1;
      while (j < text.length && depth > 0) {
        if (text[j] === '{') depth++;
        else if (text[j] === '}') depth--;
        j++;
      }
      const body = text.slice(open + 1, j - 1);
      if (head.startsWith('@media')) walk(body, media.concat(head.slice(6).trim()));
      else if (head.startsWith('@')) { /* @keyframes / @font-face: not selector rules */ }
      else rules.push({ selectors: head.split(',').map(s => s.trim()), media, body });
      i = j;
    }
  }
  walk(css, []);
  return rules;
}

const RULES = parseCss(fs.readFileSync(STYLE_CSS, 'utf8'));

function rulesFor(selector, wantMedia) {
  return RULES.filter(r => r.selectors.includes(selector) && (wantMedia === undefined || (r.media.length > 0) === wantMedia));
}

function decl(rule, prop) {
  const m = rule.body.match(new RegExp('(?:^|[;\\s])' + prop + '\\s*:\\s*([^;]+)'));
  return m ? m[1].trim().replace(/\s*!important$/, '') : null;
}

test('(f) the header stays on one non-wrapping line: a flex row that never wraps at any width', () => {
  const base = rulesFor('.header', false);
  assert.equal(base.length, 1, 'one base .header rule');
  assert.equal(decl(base[0], 'display'), 'flex');
  // No rule that styles the header row or the button — in any media query —
  // allows wrapping.
  for (const sel of ['.header', '.header-title', '.header > .header-server-btn', '.header-server-btn', '.header-server-name']) {
    for (const r of rulesFor(sel)) {
      assert.notEqual(decl(r, 'flex-wrap'), 'wrap', `${sel} must not wrap (media: ${r.media.join(' / ') || 'none'})`);
      assert.notEqual(decl(r, 'flex-wrap'), 'wrap-reverse');
    }
  }
  const btnRule = rulesFor('.header > .header-server-btn', false)[0];
  assert.ok(btnRule, 'the button has its own header rule');
  assert.equal(decl(btnRule, 'flex'), '0 0 auto', 'the button neither grows nor shrinks');
  assert.equal(decl(btnRule, 'white-space'), 'nowrap');
});

test('(f) #headerTitle truncates: min-width 0 and an ellipsis on one line', () => {
  const shell = fs.readFileSync(INDEX_HTML, 'utf8');
  const tag = shell.match(/<[a-z]+\b[^>]*id="headerTitle"[^>]*>/);
  assert.ok(tag, '#headerTitle exists in the header');
  const cls = (tag[0].match(/class="([^"]*)"/) || [, ''])[1].split(/\s+/);
  // Rules that reach the element through its id or its classes.
  const selectors = ['#headerTitle'].concat(cls.filter(Boolean).map(c => '.' + c));
  const hits = selectors.flatMap(s => rulesFor(s, false));
  assert.ok(hits.length > 0, 'a style rule targets #headerTitle: ' + selectors.join(', '));
  const get = (p) => hits.map(r => decl(r, p)).filter(v => v !== null).pop();
  assert.equal(get('min-width'), '0', 'min-width: 0 lets the flex item shrink below its text width');
  assert.equal(get('text-overflow'), 'ellipsis');
  assert.equal(get('white-space'), 'nowrap');
  assert.ok(['hidden', 'clip'].includes(get('overflow')), 'overflow clips the text');
  assert.match(get('flex') || '', /^1 1 auto$/, 'the title takes the spare space');
});

test('(f) the short-name span is hidden under the phone breakpoint and shown above it', () => {
  const sel = '.header-server-name';
  const all = rulesFor(sel);
  assert.ok(all.length >= 2, 'a base rule plus a phone override');

  // Above the breakpoint (no media query): visible, capped, ellipsised.
  const base = rulesFor(sel, false);
  assert.equal(base.length, 1);
  assert.notEqual(decl(base[0], 'display'), 'none', 'shown by default / on wide screens');
  assert.match(decl(base[0], 'max-width') || '', /^\d+ch$/, 'width capped in ch');
  assert.equal(decl(base[0], 'text-overflow'), 'ellipsis');

  // Hidden only inside a max-width phone query.
  const hiding = all.filter(r => decl(r, 'display') === 'none');
  assert.ok(hiding.length >= 1, 'something hides it');
  for (const r of hiding) {
    assert.equal(r.media.length, 1, 'hidden only inside a media query');
    const m = r.media[0].match(/^\(max-width:\s*(\d+)px\)$/);
    assert.ok(m, 'a plain max-width query: ' + r.media[0]);
    const px = Number(m[1]);
    assert.ok(px >= 480 && px <= 700, 'the phone breakpoint, got ' + px + 'px');
  }
  // Nothing re-hides it for wide screens, and nothing in a min-width query touches it.
  assert.ok(!all.some(r => r.media.some(q => /min-width/.test(q))), 'no min-width override of the name');

  // The icon and the dot are not hidden at phone width — they are what remain.
  for (const s of ['.header-server-icon', '.header-server-dot', '.header > .header-server-btn']) {
    for (const r of rulesFor(s)) assert.notEqual(decl(r, 'display'), 'none', s + ' stays visible at every width');
  }
});

test('(f) the old Sessions server select styling is gone: no .sessions-server-select rule anywhere', () => {
  const raw = fs.readFileSync(STYLE_CSS, 'utf8');
  assert.ok(!/sessions-server-select/.test(raw), 'no .sessions-server-select text in style.css');
  assert.ok(!RULES.some(r => r.selectors.some(s => /sessions-server-select|sessionsServerDropdown|#serverDropdown|\.server-dropdown\b/.test(s))));
  const app = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(!/sessions-server-select/.test(app), 'and app.js emits no such class');
});

// ── (g) unreachable peers ──────────────────────────────────────────────────

test('(g) unreachable peers show the suffix and data-reachability="down" in the modal and stay selectable', () => {
  const sb = loadApp();
  setServers(sb, [PEER, 'ok-node', 'quiet-node']);
  vm.runInContext(`_serverReachability[${JSON.stringify(PEER)}] = false; _serverReachability['ok-node'] = true;`, sb);
  openViaButton(sb);

  const down = rowFor(sb, PEER);
  assert.equal(down.getAttribute('data-reachability'), 'down');
  assert.ok(down.textContent.includes('(unreachable)'), down.textContent);
  assert.ok(!down.hasAttribute('disabled'), 'an unreachable row is never disabled');
  assert.equal(down.getAttribute('aria-disabled'), null);

  const ok = rowFor(sb, 'ok-node');
  assert.notEqual(ok.getAttribute('data-reachability'), 'down');
  assert.ok(!ok.textContent.includes('(unreachable)'));
  const unknown = rowFor(sb, 'quiet-node');
  assert.notEqual(unknown.getAttribute('data-reachability'), 'down', 'an unprobed peer is not flagged');
  assert.ok(!unknown.textContent.includes('(unreachable)'));

  // Still selectable: the click goes through selectServer once and closes.
  clickRow(sb, down);
  assert.deepEqual(sb.__calls, [PEER]);
  assert.equal(active(sb), PEER);
  assert.equal(sb.__dom.byId('serverPickerModal'), null);
  // The header button then carries the warning dot for the unreachable peer.
  assert.equal(btn(sb).dot, 'header-server-dot is-down');
});

test('(g) an unreachable peer with a hostile name keeps its suffix, mark and selectability', () => {
  const sb = loadApp();
  setServers(sb, [HOSTILE]);
  vm.runInContext(`_serverReachability[${JSON.stringify(HOSTILE)}] = false`, sb);
  openViaButton(sb);
  const row = rowFor(sb, HOSTILE);
  assert.equal(row.getAttribute('data-reachability'), 'down');
  assert.ok(row.textContent.includes('(unreachable)'));
  assert.ok(!row.hasAttribute('disabled'));
  assert.equal(sb.__dom.byId('serverPickerModalList').querySelectorAll('b').length, 0);
  clickRow(sb, row);
  assert.deepEqual(sb.__calls, [HOSTILE]);
});
