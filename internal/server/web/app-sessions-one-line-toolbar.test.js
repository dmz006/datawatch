'use strict';
// Sessions page one-line toolbar: the topbar is ONE non-splitting flex row
// (no separate fold row, no server-chip slot) at phone (≤640px) and wide
// widths. Contents in DOM order: server dropdown → search → Filters →
// Fold all (only while grouping is active) → the single view-mode <select>.
// The Fold all is a compact icon button carrying a tooltip, an aria-label
// and an aria-pressed that reflect collapsed vs expanded. The view control
// is a native <select> (List / Tiles / Compact) with an accessible label,
// showing the current mode, driving the shared setter, persisting through
// the stored-mode key cs_sessions_view_mode and restoring on a fresh paint.
// All the new locale keys must ship in en, de, es, fr and ja.
//
// This test never signals or kills a session: it only renders markup and
// inspects strings.
//
// Run: node --test internal/server/web/app-sessions-one-line-toolbar.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const CSS = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const TIPS = {
  fold: { label: 'Fold all', tip: 'Fold every group' },
  unfold: { label: 'Unfold all', tip: 'Unfold every group' },
};

// ── harness ─────────────────────────────────────────────────────────────────

function memStorage() {
  const m = new Map();
  return {
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
  };
}

function viewElement() {
  const box = { html: '' };
  const el = {
    querySelector: () => null,
    querySelectorAll: () => [],
    insertBefore() {},
  };
  Object.defineProperty(el, 'innerHTML', { get() { return box.html; }, set(v) { box.html = v; } });
  return { box, el };
}

function loadApp({ storage, phone = false } = {}) {
  const v = viewElement();
  const sandbox = buildSandbox({ byId: { view: v.el } });
  if (storage) sandbox.localStorage = storage;
  sandbox.matchMedia = (q) => ({ matches: phone && /max-width:\s*640px/.test(q), addEventListener() {}, addListener() {} });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  sandbox.__box = v.box;
  return sandbox;
}

// 8 sessions across two states + two tags, so a state/tag grouping has
// foldable groups and the search/filter surfaces have content.
function seed(sb, { grouped = true } = {}) {
  const sessions = [];
  for (let i = 0; i < 8; i++) {
    sessions.push({
      id: 'tb' + i, full_id: 'local/tb' + i, name: 'job ' + i,
      state: i % 2 === 0 ? 'running' : 'complete',
      backend_family: 'claude-code',
      tags: ['lane' + (i % 2)],
      updated_at: new Date().toISOString(),
      created_at: new Date().toISOString(),
    });
  }
  vm.runInContext(`
    state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionWatchFilter = false; state.sessionFilter = ''; state._filtersCollapsed = true;
    state.sessionsViewMode = 'list';
    state.sessionGroupBy = ${grouped ? "{ axis: 'state' }" : 'null'};
    state.sessions = ${JSON.stringify(sessions)};
    _tagsState.byId = { lane0: { id: 'lane0', name: 'Lane 0' }, lane1: { id: 'lane1', name: 'Lane 1' } };
    _tagsState.loaded = true;`, sb);
  return sessions;
}

function render(sb) { sb.renderSessionsView(); return sb.__box.html; }

function topbarSlice(html) {
  const start = html.indexOf('<div class="sessions-topbar">');
  assert.ok(start >= 0, 'the sessions topbar is painted');
  const end = html.indexOf('<div class="session-list">', start);
  assert.ok(end > start, 'topbar is followed by the session list');
  return html.slice(start, end);
}

// ── (e) locale coverage: every new control ships a translated string ───────

test('all one-line-toolbar locale keys exist with non-empty text in en, de, es, fr and ja', () => {
  const keys = [
    'sessions_fold_all', 'sessions_unfold_all',
    'sessions_fold_all_tip', 'sessions_unfold_all_tip',
    'sessions_view_dropdown', 'sessions_view_toggle',
    'sessions_view_list', 'sessions_view_tiles', 'sessions_view_compact',
  ];
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', `${loc}.json`), 'utf8'));
    for (const k of keys) {
      assert.equal(typeof bundle[k], 'string', `${loc}: ${k} is a string`);
      assert.ok(bundle[k].trim().length > 0, `${loc}: ${k} is not empty`);
      assert.equal(bundle[k].trim(), bundle[k], `${loc}: ${k} has no stray padding`);
    }
  }
});

// ── (a) one flex line: CSS says flex + nowrap, and nothing wraps it ─────────

function cssRules(css) {
  // Comments first: a CSS block comment before the first rule (and inside
  // media blocks) must not leak into selector text.
  const cleaned = css.replace(/\/\*[\s\S]*?\*\//g, '');
  return [...cleaned.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(m => ({ sel: m[1].trim(), body: m[2] }));
}

function ruleForDesc(rules, name) {
  return rules.filter(r => r.sel.split(',').map(s => s.trim()).some(s => s.endsWith('.sessions-topbar .' + name)));
}

test('the toolbar is a single non-splittable flex row at every width in the CSS', () => {
  const rules = cssRules(CSS);
  // Every rule that targets the topbar itself — base AND any width-specific
  // override — must keep the row non-wrapping.
  const selfRules = rules.filter(r => r.sel.split(',').map(s => s.trim()).includes('.sessions-topbar'));
  assert.ok(selfRules.length >= 2, 'the topbar has a base rule and a width-specific one (saw ' + selfRules.length + ')');
  assert.ok(selfRules.some(r => /display:\s*flex/.test(r.body)), 'one rule makes it a flex row');
  assert.ok(selfRules.some(r => /flex-wrap:\s*nowrap/.test(r.body)), 'one rule forbids wrapping');
  for (const r of selfRules) {
    assert.doesNotMatch(r.body, /flex-wrap:\s*wrap(?![\w-])/); // no width re-enables wrapping
  }

  // The search row is the only flexible member: it absorbs the slack, the
  // other controls keep their size (fixed flex + bounded max-widths).
  const search = ruleForDesc(rules, 'sessions-search-row');
  assert.ok(search.length, 'a .sessions-topbar .sessions-search-row rule exists');
  assert.match(search[0].body, /flex:\s*1 1 auto/, 'the search row flexes');
  assert.match(search[0].body, /min-width:\s*0/, '... and may shrink below its content size');
  for (const name of ['sessions-server-select', 'sessions-fold-all', 'sessions-view-select']) {
    const r = ruleForDesc(rules, name);
    assert.ok(r.length, 'a .sessions-topbar .' + name + ' rule exists');
    assert.match(r[0].body, /flex:\s*0 0 auto/, '.sessions-topbar .' + name + ' stays fixed-size');
  }
});


test('there is no separate fold row: not in the CSS, not in the rendered markup', () => {
  assert.doesNotMatch(CSS, /\.sessions-fold-row/, 'no .sessions-fold-row selector in style.css');
  const sbP = loadApp({ phone: true }); seed(sbP);
  const sbW = loadApp({ phone: false }); seed(sbW);
  for (const [label, sb] of [['phone', sbP], ['wide', sbW]]) {
    const h = render(sb);
    assert.doesNotMatch(h, /sessions-fold-row/, label + ': no fold-row element rendered');
  }
});

// ── (a)+(b) rendered row: one row, correct parts, correct order ─────────────

test('at phone AND wide width the toolbar is one row: server, search, Filters, Fold all, view select — in that order', () => {
  for (const [label, phone] of [['phone', true], ['wide', false]]) {
    const sb = loadApp({ phone });
    seed(sb);
    const html = render(sb);
    assert.equal((html.match(/<div class="sessions-topbar">/g) || []).length, 1, label + ': exactly one toolbar row');
    assert.equal((html.match(/sessions-fold-row/g) || []).length, 0, label + ': no separate fold row');
    assert.ok(!html.includes('sessions-topbar-servers'), label + ': no leftover server-chip slot');

    const top = topbarSlice(html);
    const order = [
      'id="sessionsServerDropdown"',           // (2) shared server dropdown instance
      'class="sessions-search-row"',           // (3) search
      'id="sessionFilterInput"',
      'class="sessions-filters-slot"',         // (4) Filters
      'id="sessionsFiltersBtn"',
      'class="sessions-fold-all"',             // (5) Fold all (grouping is active)
      'id="sessionsViewSelect"',               // (6) view control
    ].map(k => top.indexOf(k));
    assert.ok(order.every(i => i >= 0), label + ': every toolbar part is in the one row: ' + order.join(','));
    assert.deepEqual(order, [...order].sort((a, b) => a - b), label + ': parts appear in the stated DOM order');
    assert.equal((top.match(/class="sessions-search-row"/g) || []).length, 1, label + ': exactly one search row');
    // The server control is the shared picker instance — its mutation path is
    // the header dropdown's selectServer() path.
    assert.match(top, /onchange="onServerDropdownChange\(this\.value\)"/, label + ': server dropdown drives selectServer()');
  }
});

// ── (c) Fold all: icon button with tooltip, aria text, pressed state ────────

test('the fold-all icon button is absent without grouping and present while grouping is active', () => {
  const none = loadApp({ phone: true });
  const pool = seed(none, { grouped: false });
  const ungrouped = render(none);
  assert.doesNotMatch(ungrouped, /sessions-fold-all/, 'no fold control with no grouping');
  assert.equal(none._sessionsFoldControlHTML(pool, null), '', 'a pool with no group axis never renders a fold control');
  assert.equal(none._sessionsFoldControlHTML([], { axis: 'state' }), '', 'an empty pool renders no fold control either');

  const grouped = loadApp({ phone: false });
  seed(grouped);
  const top = topbarSlice(render(grouped));
  assert.match(top, /class="sessions-fold-all"/, 'grouping puts the fold control into the one row');
});

test('the fold-all button carries a tooltip, an aria-label and a glyph, never a raw icon', () => {
  const sb = loadApp({ phone: true });
  const pool = seed(sb);
  const by = vm.runInContext('state.sessionGroupBy', sb);
  const html = sb._sessionsFoldControlHTML(pool, by);
  // Not an icon-only control: it has visible-on-focus text AND a tooltip.
  assert.match(html, /<button type="button" class="sessions-fold-all" onclick="_sessionsFoldAllToggle\(\)"/);
  assert.match(html, new RegExp('title="' + TIPS.fold.tip + '"'));
  assert.match(html, new RegExp('aria-label="' + TIPS.fold.label + '"'));
  assert.match(html, /aria-pressed="false"/, 'starts with the groups expanded');
  assert.match(html, /&#8857;</, 'the fold glyph');
  assert.equal(html.includes('sessions_fold_all'), false, 'no raw i18n key in the markup');
});

test('the fold-all button reflects the collapsed state: Unfold wording and aria-pressed=true', () => {
  const sb = loadApp({ phone: false });
  const pool = seed(sb);
  const keys = vm.runInContext(`JSON.stringify(_sessionsFoldKeys(${JSON.stringify(pool)}, state.sessionGroupBy))`, sb, { filename: '<fold-keys>' });
  const keyList = JSON.parse(keys);
  assert.ok(keyList.length >= 2, 'the state grouping of the fixture is foldable');
  vm.runInContext(`state._sessionsCollapsed = ${JSON.stringify(keyList)};`, sb);
  const folded = sb._sessionsFoldControlHTML(pool, vm.runInContext('state.sessionGroupBy', sb));
  assert.match(folded, new RegExp('title="' + TIPS.unfold.tip + '"'), 'tooltip offers the inverse action');
  assert.match(folded, new RegExp('aria-label="' + TIPS.unfold.label + '"'));
  assert.match(folded, /aria-pressed="true"/, 'pressed reads as "currently folded"');
  assert.match(folded, /&#8862;</, 'the unfold glyph');
});

test('toggling folds every group, flips the button, and un-folds again — all through the painted control', () => {
  const sb = loadApp({ phone: true });
  seed(sb);
  let html = render(sb);
  assert.equal((html.match(/sessions-group collapsed"/g) || []).length, 0, 'starts expanded');
  assert.match(html, /class="sessions-fold-all"[\s\S]*?aria-pressed="false"/);

  sb._sessionsFoldAllToggle();
  html = sb.__box.html;
  assert.equal((html.match(/sessions-group collapsed"/g) || []).length, 2, 'both fixture groups fold');
  assert.equal((html.match(/class="session-card /g) || []).length, 0, 'no cards while every group is folded');
  assert.match(html, /class="sessions-fold-all"[\s\S]*?title="Unfold every group"[\s\S]*?aria-pressed="true"/, 'button now offers the inverse');

  sb._sessionsFoldAllToggle();
  html = sb.__box.html;
  assert.equal((html.match(/sessions-group collapsed"/g) || []).length, 0, 'toggle again expands everything');
  assert.match(html, /class="sessions-fold-all"[\s\S]*?aria-pressed="false"/);
});

// ── (d) view control: one <select>, accessible, drives the shared path ──────

test('the view control is a native select with List/Tiles/Compact, an accessible label, the current mode and the shared setter', () => {
  const sb = loadApp();
  const html = sb._sessionsViewSelectHTML('tiles');
  assert.match(html, /<select id="sessionsViewSelect" class="sessions-view-select"/);
  assert.match(html, /aria-label="Sessions view mode"/, 'the select has its own accessible label');
  assert.match(html, /onchange="_sessionsSetViewMode\(this\.value\)"/, 'changing it calls the shared mode setter');
  for (const [m, label] of [['list', 'List'], ['tiles', 'Tiles'], ['compact', 'Compact']]) {
    if (m === 'tiles') assert.match(html, /<option value="tiles" selected>/, 'the active mode is marked selected');
    else assert.match(html, new RegExp('<option value="' + m + '">'), `option ${m} present`);
    assert.match(html, new RegExp('>' + label + '</option>'), `option label for ${m}`);
  }
  assert.equal((html.match(/<option/g) || []).length, 3, 'exactly the three modes, nothing more');
  for (const k of ['sessions_view_toggle', 'sessions_view_dropdown', 'sessions_view_list', 'sessions_view_tiles', 'sessions_view_compact']) {
    assert.equal(html.includes(k), false, 'raw i18n key leaks out: ' + k);
  }
});

test('the select shows the current mode as selected in a real paint', () => {
  const sb = loadApp({ phone: true });
  seed(sb);
  sb._sessionsSetViewMode('compact');
  const top = topbarSlice(sb.__box.html);
  assert.match(top, /<option value="compact" selected>/, 'the painted select marks the active mode');
  assert.match(top, /<option value="list">/);
  assert.match(top, /<option value="tiles">/);
});

test('choosing a mode stores it under the stored-mode key and the invalid choice is refused', () => {
  const sb = loadApp();
  sb._sessionsSetViewMode('tiles');
  assert.equal(sb.localStorage.getItem('cs_sessions_view_mode'), 'tiles', 'choice persists through the stored-mode key');
  assert.equal(sb._sessionsViewMode(), 'tiles', 'the in-memory mode agrees');
  sb._sessionsSetViewMode('bogus');
  assert.equal(sb.localStorage.getItem('cs_sessions_view_mode'), 'tiles', 'a non-mode value is not written');
  assert.equal(sb._sessionsViewMode(), 'tiles', '... and does not overwrite the choice');
  assert.equal(sb._sessionsViewModeNormalize('bogus'), null, 'normalization refuses it');
});

test('a fresh paint restores the stored mode into the select, and a blocked storage degrades to the viewport default', () => {
  const shared = memStorage();
  const first = loadApp({ storage: shared, phone: true });
  seed(first);
  first._sessionsSetViewMode('compact');
  assert.equal(shared.getItem('cs_sessions_view_mode'), 'compact');

  const fresh = loadApp({ storage: shared, phone: true }); // simulated reload, same device
  seed(fresh);
  vm.runInContext('state.sessionsViewMode = null;', fresh); // nothing in memory yet: the stored choice must surface
  const h = render(fresh);
  assert.equal(fresh._sessionsViewMode(), 'compact', 'the stored choice wins over the viewport');
  assert.match(topbarSlice(h), /<option value="compact" selected>/, 'the fresh paint marks it as the current mode');

  const blocked = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); }, removeItem() { throw new Error('blocked'); } };
  const b = loadApp({ phone: true });
  assert.doesNotThrow(() => { b._sessionsSetViewMode('tiles', blocked); }, 'a blocked storage never throws');
  assert.equal(b._sessionsViewMode(), 'tiles', 'the in-memory choice still applies');
});

// ── guardrail: the ungrouped non-empty page keeps the full one-line toolbar ────

test('an ungrouped Sessions page keeps the one-line toolbar without the fold control', () => {
  const sb = loadApp({ phone: true });
  seed(sb, { grouped: false });
  const html = render(sb);
  const top = topbarSlice(html);
  for (const k of ['sessionsServerDropdown', 'sessions-search-row', 'sessionsFiltersBtn', 'sessionsViewSelect']) {
    assert.ok(top.includes(k), 'toolbar keeps its parts on an ungrouped page: ' + k);
  }
  assert.ok(!top.includes('sessions-fold-all'), 'no grouping means nothing is foldable');
});
