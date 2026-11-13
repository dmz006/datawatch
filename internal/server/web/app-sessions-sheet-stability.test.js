// PWA — the filters sheet must not re-render while it is open.
//
// Operator-reported bug: while the Sessions Filters sheet was open, every
// timer-based or live (WS) update re-drew it — destroying the open
// Group-by dropdown, the focused search box, scroll positions and any
// open pickers.
//
// Required behaviour, asserted here:
//   1. A refresh updates only the session list behind the sheet — the
//      sheet is never rebuilt with fresh nodes while it is open.
//   2. Where a change lands IN the sheet (filter counts, select
//      options, chip rows), it is a patch IN PLACE: every select/input
//      stays the exact same node, with its value, its focus and the
//      block's scroll position kept.
//   3. While a select in the sheet is open (focused) or an input in the
//      sheet has focus, the patch is DEFERRED — it runs exactly once
//      when the control is released (focusout/blur/change/click).
//   4. The same rule covers the search box: same node, value restored
//      in place.
//
// The harness stub's `document.getElementById(id)` returns exactly the
// object pre-registered under `byId`, which is how a real browser
// returns the operator's live, in-use nodes to the page. Tests below
// plant the LIVE sheet/select/search nodes that way, seed the "fresh"
// page markup the paint would produce, and then assert the app kept
// the operator's nodes (identity, value, focus, scroll) and only
// patched what actually changed — deferring while engaged.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadApp(byId) {
  const sandbox = buildSandbox({ byId: byId || {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

// A live control node: what an in-use <select>/<input>/<section> looks
// like to the stability kit — identity, value, innerHTML, scrollTop,
// event listeners. Plain data properties so the test observes every
// write the app makes.
function liveEl(id, opt) {
  opt = opt || {};
  const el = {
    id: String(id || ''),
    value: typeof opt.value === 'string' ? opt.value : '',
    innerHTML: typeof opt.innerHTML === 'string' ? opt.innerHTML : '',
    scrollTop: typeof opt.scrollTop === 'number' ? opt.scrollTop : 0,
    children: Array.isArray(opt.children) ? opt.children : [],
    focused: false,
    removed: false,
    listeners: {},
  };
  el.focus = function () { el.focused = true; };
  el.remove = function () { el.removed = true; };
  el.contains = function (n) {
    if (n === el) return true;
    return el.children.indexOf(n) !== -1;
  };
  el.addEventListener = function (type, fn) {
    (el.listeners[type] = el.listeners[type] || []).push(fn);
  };
  el.fire = function (type, e) {
    for (const fn of (el.listeners[type] || [])) fn(e || {});
  };
  return el;
}

test('refresh with the grouping select open: same select node, value + focus + scroll kept; counts update, deferred until release', () => {
  // The operator's live sheet, mid-interaction.
  const liveSelect = liveEl('sessions-group-select', { value: 'project', innerHTML: '<option value="none">stale</option>' });
  const liveChips = liveEl('sessionsSheetStateChips', { innerHTML: 'stale-counts' });
  const liveSheet = liveEl('sessionsSheetBackdrop', { children: [liveSelect, liveChips], scrollTop: 140 });
  const liveSearch = liveEl('sessionFilterInput', { value: '' });
  const viewEl = makeStubElement(); // the page element the paint writes into

  const sb = loadApp({
    'view': viewEl,
    'sessionFilterInput': liveSearch,
    'sessionsSheetBackdrop': liveSheet,
  });

  vm.runInContext(`
    state.activeView = 'sessions';
    state.showHistory = false;
    state.sessionFilter = '';                   // no text filter: all sessions visible
    state.sessionGroupBy = { axis: 'project', tagPrefix: '' };
    state._sessionsSheetOpen = true;               // sheet is OPEN
    state.sessions = [
      { id: 's1', full_id: 'srv/s1', state: 'running',       project: 'p', backend_family: 'claude-code' },
      { id: 's2', full_id: 'srv/s2', state: 'waiting_input', project: 'p', backend_family: 'claude-code' },
      { id: 's3', full_id: 'srv/s3', state: 'waiting_input', project: 'q', backend_family: 'claude-code' },
    ];
  `, sb);

  // The Group-by select is OPEN and focused; that is the engaged control.
  sb.document.activeElement = liveSelect;

  // A refresh (timer/WS) fires a full re-render of the same view element.
  sb.renderSessionsView();

  // 1. The list behind the sheet was updated by this paint…
  assert.match(viewEl.innerHTML, /view-content/);
  assert.match(viewEl.innerHTML, /srv\/s1/, 'the refreshed list still contains the sessions'); ;

  // …the sheet's live controls survived as the SAME nodes…
  assert.equal(liveSelect.value, 'project', 'focused select value kept');
  assert.equal(liveSelect.focused, true, 'focus re-landed on the live select node');
  assert.equal(liveSheet.scrollTop, 140, 'sheet scroll position kept');
  assert.equal(liveSelect.removed, false, 'the live select was not removed/replaced');
  assert.equal(liveChips.removed, false, 'the live chips row was not removed/replaced');

  // 2. While the select is engaged (open/focused), the in-place count
  //    patch is DEFERRED — the stale markup has not been touched yet…
  assert.equal(liveSelect.innerHTML, '<option value="none">stale</option>', 'options patch deferred while the select is open');
  assert.equal(liveChips.innerHTML, 'stale-counts', 'chip-count patch deferred while the select is open');
  assert.ok(liveSheet.__dwPending && liveSheet.__dwPending.length >= 1, 'patch queued on the live sheet root');

  // …and it lands exactly once when the control is released.
  liveSheet.fire('dw-settle'); // (focusout / change / click fire the same path)
  assert.equal(liveSheet.__dwPending.length, 0, 'queue flushed exactly once');
  assert.match(liveSelect.innerHTML, /<option/, 'options synced in place on the same node');
  assert.equal(liveSelect.value, 'project', 'selection still "project" after the sync');
  assert.match(liveChips.innerHTML, /badge-count/, 'fresh chip row present');
  assert.match(liveChips.innerHTML, /\>1</, 'updated per-state counts (waiting_input=2, running=1 → counts in the row)');

  // 3. The search box was part of the same paint and stayed the same
  //    node, un-replaced (value-patch behaviour is covered by test 2,
  //    which runs the engaged/deferred path with a non-empty value).
  assert.equal(liveSearch.value, '', 'search input value untouched');
  assert.equal(liveSearch.removed, false, 'search input node was not replaced');
});

test('focused search input defers its own patch until blur, then flushes once', () => {
  const liveSheet = liveEl('sessionsSheetBackdrop', { children: [] });
  const liveSearch = liveEl('sessionFilterInput', { value: '' });
  const viewEl = makeStubElement();

  const sb = loadApp({
    'view': viewEl,
    'sessionFilterInput': liveSearch,
    'sessionsSheetBackdrop': liveSheet,
  });
  vm.runInContext(`
    state.activeView = 'sessions';
    state.showHistory = false;
    state.sessionFilter = 'alpha';
    state.sessionGroupBy = { axis: 'project', tagPrefix: '' };
    state._sessionsSheetOpen = true;
    state.sessions = [
      { id: 's1', full_id: 'srv/s1', state: 'running', project: 'p', backend_family: 'claude-code' },
    ];
  `, sb);

  // The SEARCH input has focus right now: the patch must defer.
  sb.document.activeElement = liveSearch;
  sb.renderSessionsView();

  assert.equal(liveSearch.value, '', 'value patch deferred while the input is focused');
  assert.ok(liveSearch.__dwPending && liveSearch.__dwPending.length >= 1, 'queued on the live input');

  // Blur → the deferred patch runs exactly once.
  liveSearch.fire('focusout', { relatedTarget: null });
  assert.equal(liveSearch.__dwPending.length, 0, 'flushed on blur');
  assert.equal(liveSearch.value, 'alpha', 'value landed in place after blur, same node');
});
