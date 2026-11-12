'use strict';
// Sessions Filters sheet layout: the single Group by control and the FAB
// clearance rules around it.
//
// There is no JS test framework in this PWA (no package.json, no bundler);
// following the established pattern in this directory
// (app-sessions-filters-sheet.test.js, testutil_browser_stub.js) this is a
// plain Node script that loads the real, unmodified app.js into a Node vm
// stub browser (and reads the real style.css as text) and asserts:
//
//   1. The rendered Filters sheet contains exactly one grouping <select>,
//      inside the "Group by" section, and no legacy `group-axis-control`.
//   2. The sessions page toolbar markup carries no grouping control — the
//      sheet is the one place the control lives.
//   3. Changing the Group by dropdown and reading state back through
//      _sessionsGroupCurrent agree, and the same value (the one saved
//      setting) is selected after a re-render.
//   4. style.css hides .new-session-fab while the sheet is open, and
//      .sessions-sheet-body's bottom padding references the 56px FAB size
//      and --nav-h.
//   5. Every overlay audited in the CSS "FAB sheet/modal audit" comment
//      carries the same hide rule (or an equivalent FAB-height padding).
//
// Run: env -u TMUX -u TMUX_TMPDIR node --test \
//   internal/server/web/app-sessions-filters-sheet-layout.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, vm }
  = require('./testutil_browser_stub.js');

const WEB_DIR = __dirname;
const APP_JS = path.join(WEB_DIR, 'app.js');
const STYLE_CSS = path.join(WEB_DIR, 'style.css');

function makeStore() {
  const m = {};
  return {
    map: m,
    ls: {
      getItem: (k) => (k in m) ? m[k] : null,
      setItem: (k, v) => { m[k] = String(v); },
      removeItem: (k) => { delete m[k]; },
    },
  };
}

const run = (sbx, code) => vm.runInContext(code, sbx);

// paintedSandbox mirrors the paint harness of
// app-sessions-grouping.test.js: one tagged session, the Filters sheet
// open, and a `view` element whose innerHTML capture holds the painted page.
function paintedSandbox(store) {
  const cap = { html: '' };
  const el = { querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return cap.html; }, set(v) { cap.html = v; } });
  const sbx = buildSandbox({ byId: { view: el } });
  if (store) sbx.localStorage = store.ls;
  vm.createContext(sbx);
  loadScript(APP_JS, sbx);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sbx._i18n.bundle = en;
  sbx._i18n.fallback = en;
  run(sbx, `
    state.sessions = [
      { id: 'a', state: 'running', hostname: 'node-a', project_dir: '/p/alpha', tags: ['ops:cron'] },
      { id: 'b', state: 'waiting_input', hostname: 'node-b', project_dir: '/p/beta', tags: ['ops:watch'] },
    ];
    state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionFilter = ''; state.sessionTreeView = false;
    state._filtersCollapsed = false;      // toolbar expanded: its markup is audited
    state.sessionsViewMode = 'list'; state._sessionsSheetOpen = true;
    _tagsState.list = [{ name: 'ops:cron' }, { name: 'ops:watch' }]; _tagsState.loaded = true;
  `);
  return { sbx, cap, store };
}

function sheetFragment(html) {
  const i = html.indexOf('<div class="sessions-sheet-backdrop"');
  assert.ok(i >= 0, 'the Filters sheet is painted');
  return html.slice(i);
}

function selectedGroupOption(html) {
  const m = html.match(/<select id="sessions-group-select"[\s\S]*?<\/select>/);
  assert.ok(m, 'the Group by select is rendered');
  const sel = m[0].match(/<option value="([^"]*)" selected>/g) || [];
  assert.equal(sel.length, 1, 'exactly one option is selected');
  return sel[0].match(/value="([^"]*)"/)[1];
}

// ── 1. the sheet: one grouping control, in the Group by section ─────────
test('sheet carries exactly one grouping select inside the Group by section, no legacy axis control', () => {
  const box = paintedSandbox(makeStore());
  box.sbx.renderSessionsView();
  const html = box.cap.html;

  const sheet = sheetFragment(html);
  assert.equal((sheet.match(/<select/g) || []).length, 1,
    'exactly one <select> in the sheet (the Group by control)');
  const selIdx = sheet.indexOf('<select id="sessions-group-select"');
  assert.ok(selIdx >= 0, 'that one select is the Group by picker');

  // The select sits inside the "Group by" section, before the next section.
  const h4 = '<section class="sessions-sheet-sec"><h4>Group by</h4>';
  const secIdx = sheet.indexOf(h4);
  assert.ok(secIdx >= 0, 'the sheet has a Group by section');
  assert.ok(selIdx > secIdx, 'the select comes after the Group by heading');
  const nextSec = sheet.indexOf('<section class="sessions-sheet-sec">', secIdx + h4.length);
  assert.ok(nextSec === -1 || selIdx < nextSec,
    'the Group by select stays inside its own section');

  assert.ok(!html.includes('group-axis-control'),
    'the removed second placement control is gone from the page');
});

// ── 2. the page toolbar: no grouping control ─────────────────────────────
test('the sessions page toolbar (top bar + toolbar rows) has no grouping control', () => {
  const box = paintedSandbox(makeStore());
  box.sbx.renderSessionsView();
  const html = box.cap.html;

  const start = html.indexOf('class="sessions-topbar"');
  assert.ok(start >= 0, 'the sessions top bar is painted');
  const end = html.indexOf('<div class="sessions-sheet-backdrop"');
  assert.ok(end > start, 'the sheet paints after the toolbar');
  const toolbar = html.slice(start, end);

  assert.ok(!toolbar.includes('<select'),
    'no <select> anywhere in the top bar and toolbar region');
  assert.ok(!toolbar.includes('sessions-group-select'), 'no Group by picker in the toolbar');
  assert.ok(!toolbar.includes('group-axis-control'), 'no legacy placement control in the toolbar');
  assert.ok(!toolbar.includes('setSessionGroupBy('), 'nothing in the toolbar writes the grouping choice');
  // The toolbar still carries its real controls.
  assert.ok(toolbar.includes('class="sessions-toolbar"'), 'the toolbar itself is painted (audit is against a non-empty region)');
});

// ── 3. one setting: dropdown, _sessionsGroupCurrent, re-render agree ─────
test('Group by dropdown, _sessionsGroupCurrent and the re-render read the same single setting', () => {
  const box = paintedSandbox(makeStore());
  const { sbx, cap, store } = box;

  // Simulate the operator changing the sheet dropdown.
  sbx.setSessionGroupBy('tag|ops:');

  // Read it back through the validated accessor (JSON round-trip: values
  // created inside the vm sandbox carry a different prototype realm, so
  // host-side deep-equal needs a plain copy).
  const fromSandbox = (code) => JSON.parse(vm.runInContext(code, sbx));
  const cur = fromSandbox('JSON.stringify(_sessionsGroupCurrent())');
  assert.deepEqual(cur, { axis: 'tag', tagPrefix: 'ops:' }, '_sessionsGroupCurrent matches the dropdown change');
  assert.deepEqual(fromSandbox('JSON.stringify(state.sessionGroupBy)'), cur,
    'state.sessionGroupBy and _sessionsGroupCurrent are the one setting');

  // The freshly re-rendered picker selects the same option.
  const picker = sbx.renderSessionsGroupPicker(cur);
  assert.ok(picker.includes('value="tag|ops:" selected'), 'the picker shows the changed value selected');

  // Full re-render: the same value is still the selected option.
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), 'tag|ops:', 'same value selected after re-render');

  // The single setting is the one saved on the device.
  assert.deepEqual(JSON.parse(store.map['dw_sessions_group_by']), { axis: 'tag', tagPrefix: 'ops:' });

  // And flipping it off works through the same one control/setting.
  sbx.setSessionGroupBy('');
  assert.equal(run(sbx, 'JSON.stringify(_sessionsGroupCurrent())'), 'null', 'cleared via the same dropdown');
  sbx.renderSessionsView();
  assert.equal(selectedGroupOption(cap.html), '', 're-render shows "No grouping"');
});

// ── 4./5. style.css: FAB clearance for this sheet and every audited sheet ─
const css = fs.readFileSync(STYLE_CSS, 'utf8');

const FAB_HIDE = /body:has\(([^)]+)\) \.new-session-fab/g;

test('style.css hides the new-session FAB while the Filters sheet is open', () => {
  assert.match(css, /body:has\(\.sessions-sheet-backdrop\) \.new-session-fab/);
  const m = css.match(/body:has\(\.sessions-sheet-backdrop\) \.new-session-fab,\s*[\s\S]*?\{\s*display:\s*none;\s*\}/);
  assert.ok(m, 'the FAB hide rule block is real (display: none), not just a selector');
});

test('style.css pads .sessions-sheet-body bottom by the 56px FAB size over the nav', () => {
  const m = css.match(/\.sessions-sheet-body\s*\{[\s\S]*?\}/);
  assert.ok(m, 'the .sessions-sheet-body rule exists');
  const b = m[0];
  assert.ok(/padding(?::|s)/.test(b), 'the rule sets padding');
  assert.ok(b.includes('56px'), 'bottom padding references the 56px FAB button size');
  assert.ok(b.includes('var(--nav-h)'), 'bottom padding references --nav-h');
  assert.match(b, /padding:\s*[^;]*calc\(56px\s*\+\s*var\(--nav-h\)/,
    'padding composes 56px + --nav-h so the last sheet rows clear the FAB and bottom nav');
});

test('every overlay audited in the CSS comment carries the same hide or padding rule', () => {
  // Audited overlays are listed in the "FAB sheet/modal audit" comment,
  // one `  .class — description` line each.
  const c0 = css.indexOf('FAB sheet/modal audit');
  assert.ok(c0 >= 0, 'the audit comment is present');
  const comment = css.slice(c0, css.indexOf('*/', c0));
  const audited = (comment.match(/^\s*\.[A-Za-z][\w.-]+/gm) || []).map((s) => s.trim());
  assert.ok(audited.length >= 9, 'the audit lists several overlays, got: ' + audited.join(', '));
  assert.ok(audited.includes('.sessions-sheet-backdrop'), 'the Filters sheet itself is audited');

  // The concrete hide selectors in the CSS (the audited set that got a rule).
  const hidden = new Set();
  for (const m of css.matchAll(FAB_HIDE)) hidden.add(m[1]);
  // A sheet keeps clearance by padding its content by the FAB zone instead.
  const padded = (sel) => {
    const m = css.match(new RegExp(sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{[^}]*\\}'));
    return !!(m && m[0].includes('56px') && m[0].includes('var(--nav-h)'));
  };

  for (const sel of audited) {
    // `.tag-filter-sheet.is-open` may be listed with or without the state;
    // the CSS may hide the base class or the .is-open state.
    const base = sel.replace(/\.is-open$/, '');
    const ok = hidden.has(sel) || hidden.has(base)
      || padded(sel) || (base !== sel && padded(base));
    assert.ok(ok, `audited overlay ${sel} has a FAB hide rule or a 56px/--nav-h padding rule`);
  }
});
