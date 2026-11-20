'use strict';
// Sessions Filters button: TOGGLE behaviour.
//
// The one Filters button in the Sessions toolbar opened the sheet before and
// had no way to close it from the button — closing worked only via the
// backdrop tap, the in-sheet close button and Escape. Now:
//   1. The button is a toggle: `onclick="sessionsFiltersToggle()"`, exposed
//      on window, and its `aria-expanded` follows state.
//   2. Toggle closes when the sheet is open, opens when it is closed.
//   3. Outside-click close: while the sheet is open a document-level
//      `click` listener closes it — for the phone sheet and the desktop
//      panel alike (both render the same `.sessions-sheet` node under the
//      same backdrop). A click on the Filters button itself is the toggle's
//      job (no close-then-reopen), and a click inside the sheet — including
//      the sheet kit's in-place re-patches — must not close it.
//   4. Escape still closes (keydown listener), and the backdrop tap still
//      closes.
//
// There is no JS test framework in this PWA (no package.json, no bundler);
// following the established pattern in this directory
// (app-sessions-filters-sheet.test.js, testutil_browser_stub.js) this is a
// plain Node script that loads the real, unmodified app.js into a Node vm
// stub browser.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function capture() {
  const cap = { html: '' };
  const el = makeStubElement((v) => { cap.html = v; });
  return { el, cap };
}

function loadApp() {
  const v = capture();
  const sandbox = buildSandbox({ byId: { view: v.el } });
  sandbox.__box = v.cap;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function seed(sb) {
  vm.runInContext(`
    state.sessions = [
      { id: 'a', state: 'running',       hostname: 'n1', project_dir: '/p', backend_family: 'claude-code', tags: [] },
      { id: 'b', state: 'waiting_input', hostname: 'n2', project_dir: '/p', backend_family: 'claude-code', tags: [] },
    ];
    state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionFilter = ''; state.sessionTreeView = false;
    state._filtersCollapsed = false; state.sessionGroupBy = null;
    state.sessionsViewMode = 'list'; state._sessionsSheetOpen = false;
  `, sb);
}

// Captures the document listeners the app registers, by type, so the test
// can fire the exact bound handler the way a browser dispatch would.
function docHooks(sb) {
  const live = { keydown: new Set(), click: new Set() };
  sb.document.addEventListener = (n, fn) => { if (live[n]) live[n].add(fn); };
  sb.document.removeEventListener = (n, fn) => { if (live[n]) live[n].delete(fn); };
  return live;
}

const open = (sb) => vm.runInContext('state._sessionsSheetOpen', sb);
const outsideClick = (sb, live) => [...live.click][0]({ target: { closest: () => null } });
const buttonClick = (sb, live) => [...live.click][0]({ target: { closest: (sel) => (sel === '#sessionsFiltersBtn' ? { btn: true } : null) } });
const sheetClick = (sb, live, sheetEl, inside) => [...live.click][0]({ target: inside });

test('the Filters button is a toggle: onclick, window exposure, aria-expanded', () => {
  const sb = loadApp();
  seed(sb);
  sb.renderSessionsView();
  const html = sb.__box.html;
  assert.ok(html.includes('onclick="sessionsFiltersToggle()"'), 'the button calls the toggle');
  assert.ok(!html.includes('onclick="sessionsFiltersOpen()"'), 'the button no longer only opens');
  assert.match(html, /id="sessionsFiltersBtn"[^>]*aria-haspopup="dialog"/);
  assert.match(html, /id="sessionsFiltersBtn"[^>]*aria-expanded="false"/, 'closed: aria-expanded=false');
  assert.equal(typeof sb.sessionsFiltersToggle, 'function', 'the toggle is exposed on window');
});

test('toggle opens when closed and closes when open', () => {
  const sb = loadApp();
  seed(sb);
  assert.equal(open(sb), false);
  sb.sessionsFiltersToggle();
  assert.equal(open(sb), true, 'toggle opened the closed sheet');
  assert.ok(sb.__box.html.includes('id="sessionsSheetBackdrop"'), 'the opened sheet is painted');
  sb.sessionsFiltersToggle();
  assert.equal(open(sb), false, 'toggle closed the open sheet');
  assert.ok(!sb.__box.html.includes('id="sessionsSheetBackdrop"'), 'the closed sheet is gone from the paint');
});

test('aria-expanded follows the toggled state across re-paints', () => {
  const sb = loadApp();
  seed(sb);
  sb.sessionsFiltersToggle();
  assert.match(sb.__box.html, /id="sessionsFiltersBtn"[^>]*aria-expanded="true"/, 'open: aria-expanded=true');
  sb.sessionsFiltersToggle();
  assert.match(sb.__box.html, /id="sessionsFiltersBtn"[^>]*aria-expanded="false"/, 'closed again: aria-expanded=false');
});

test('outside click closes the sheet and the listener is gone after', () => {
  const sb = loadApp();
  seed(sb);
  const live = docHooks(sb);
  sb.sessionsFiltersOpen();
  assert.equal(open(sb), true);
  assert.equal(live.click.size, 1, 'the outside-click listener is bound to document');
  assert.equal(live.keydown.size, 1, 'the Escape listener is still bound to document');
  outsideClick(sb, live);
  assert.equal(open(sb), false, 'an outside click closed the sheet');
  assert.equal(live.click.size, 0, 'the outside-click listener was removed on close');
  assert.equal(live.keydown.size, 0, 'the Escape listener was removed on close');
});

test('a click that lands on the Filters button does not close-then-reopen', () => {
  const sb = loadApp();
  seed(sb);
  const live = docHooks(sb);
  sb.sessionsFiltersOpen();
  // Simulate the click that the button's own onclick will handle: the
  // bubble to document must not close the sheet first (close-then-reopen
  // re-paints the whole page and loses in-flight state).
  buttonClick(sb, live);
  assert.equal(open(sb), true, 'the outside-click handler let the button click through');
  // Now the button's own toggle closes it, exactly once.
  sb.sessionsFiltersToggle();
  assert.equal(open(sb), false, 'the buttons toggle closed the sheet');
});

test('a click inside the sheet (incl. re-patched nodes) does not close it', () => {
  const sb = loadApp();
  seed(sb);
  const inside = { tag: 'select-in-sheet' };
  const sheetEl = { contains: (n) => n === sheetEl || n === inside };
  sb.document.querySelector = (sel) => (sel === '.sessions-sheet' ? sheetEl : null);
  const live = docHooks(sb);
  sb.sessionsFiltersOpen();
  sheetClick(sb, live, sheetEl, inside);
  assert.equal(open(sb), true, 'a click inside the live sheet node keeps it open');
  // The in-sheet close button is the sheet's own path.
  sb.sessionsFiltersClose();
  assert.equal(open(sb), false, 'the in-sheet close button still closes');
});

test('Escape and the backdrop tap still close it alongside the new paths', () => {
  const sb = loadApp();
  seed(sb);
  const live = docHooks(sb);
  sb.sessionsFiltersOpen();
  const [handler] = [...live.keydown];
  handler({ key: 'Escape' });
  assert.equal(open(sb), false, 'Escape still closes');

  sb.sessionsFiltersOpen();
  const backdrop = {};
  sb.sessionsFiltersBackdrop({ target: {}, currentTarget: backdrop });
  assert.equal(open(sb), true, 'a backdrop click with an inner target does not close');
  sb.sessionsFiltersBackdrop({ target: backdrop, currentTarget: backdrop });
  assert.equal(open(sb), false, 'a backdrop tap closes');
  assert.equal(live.keydown.size, 0, 'listeners are gone after the close');
});
