'use strict';
// Sessions page: Status and Backend filter chips are multi-select.
//   1. Status chip clicks add/remove states (second click removes); empty
//      set = 'all'; a historical pick still turns History on.
//   2. Backend chips live in their own set, never in the search field.
//   3. Each group has an "All" chip that clears the group (aria-pressed
//      tracks the group state).
//   4. The summary row above the list shows every selected chip with a
//      remove button plus "Clear all" (all three groups).
//   5. The filter badge counts each non-empty group once.
//   6. Selections persist under the new localStorage keys and the old
//      single-select key migrates; saved views migrate a backend stored
//      in the free-text filter into the backend set.
//
// Run: node --test internal/server/web/app-sessions-status-backend-multiselect.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;

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

function loadApp({ storage } = {}) {
  const v = viewElement();
  const sandbox = buildSandbox({ byId: { view: v.el } });
  if (storage) sandbox.localStorage = storage;
  sandbox.matchMedia = (q) => ({ matches: false, addEventListener() {}, addListener() {} });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  sandbox.__box = v.box;
  return sandbox;
}

// 16 sessions: 4 states x 4 backends, one each — every filtered pool fits
// in one rendered page and the card count is the pool count.
const STATES = ['running', 'waiting_input', 'complete', 'failed'];
const BACKENDS = ['claude-code', 'opencode', 'pi', 'ollama'];
function fixture() {
  const out = [];
  let i = 0;
  for (const st of STATES) for (const bt of BACKENDS) {
    out.push({
      id: 'id' + String(i).padStart(2, '0'),
      full_id: 'local/id' + String(i).padStart(2, '0'),
      name: 'job-' + i,
      state: st,
      backend_family: bt,
      tags: [],
      inherited_tags: [],
      updated_at: new Date(Date.now() - 1000).toISOString(),
      created_at: new Date(Date.now() - 1000).toISOString(),
    });
    i++;
  }
  return out;
}

function seed(sb, extra = '') {
  sb.__fx = fixture();
  vm.runInContext(`state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionWatchFilter = false; state.sessionsViewMode = 'list'; state._filtersCollapsed = true;
    state.sessionFilter = ''; state.sessions = globalThis.__fx;
    _tagsState.byId = {}; _tagsState.loaded = true; ${extra}`, sb);
}

function render(sb) { sb.renderSessionsView(); return sb.__box.html; }
function cards(html) { return (html.match(/class="session-card /g) || []).length; }
// vm results come from another realm: round-trip once so deepEqual sees
// arrays from the host realm.
function j(x) { return JSON.parse(JSON.stringify(x)); }

// ── 1. Status multi-select ───────────────────────────────────────────────

test('status chips multi-select: two states show the union, a third click removes one', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('running');
  assert.equal(cards(render(sb)), 4, 'only the four running sessions');

  sb.setSessionStateChip('failed');
  assert.deepEqual(j(vm.runInContext('state.sessionStateChips.slice().sort()', sb)), ['failed', 'running']);
  assert.equal(vm.runInContext('state.sessionStateChip', sb), 'all', 'two picks: the single-select reader is "all"');
  assert.equal(cards(render(sb)), 8, 'running + failed = 8 sessions');

  sb.setSessionStateChip('failed'); // second click removes it
  assert.deepEqual(j(vm.runInContext('state.sessionStateChips.slice()', sb)), ['running']);
  assert.equal(cards(render(sb)), 4);
});

test('empty status set shows all 16, and the chip buttons are keyboard-usable <button>s with aria-pressed', () => {
  const sb = loadApp();
  seed(sb);
  const html = render(sb);
  assert.equal(cards(html), 16);
  const sheet = sb._sessionsSheetStateChips().html;
  assert.match(sheet, /aria-pressed="true"/, 'the All chip is pressed while the group is empty');
  assert.match(sheet, /<button [^>]*type="button"/);
  assert.match(sheet, /onclick="sessionsFilterClearGroup\('status'\)"/);

  sb.setSessionStateChip('waiting_input');
  const sheet2 = sb._sessionsSheetStateChips().html;
  assert.match(sheet2, /aria-pressed="false"/, 'All is unpressed once a state is selected');
  assert.match(sheet2, /class="backend-filter-badge active"/, 'the selected state chip carries the active class');
});

test('the All chip clears the whole status group', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('running');
  sb.setSessionStateChip('failed');
  assert.equal(cards(render(sb)), 8);

  sb.sessionsFilterClearGroup('status');
  assert.deepEqual(j(vm.runInContext('state.sessionStateChips.slice()', sb)), []);
  assert.equal(vm.runInContext('state.sessionStateChip', sb), 'all');
  assert.equal(cards(render(sb)), 16);
});

test('picking a historical state still turns History on', () => {
  const sb = loadApp();
  seed(sb, 'state.showHistory = false;');
  sb.setSessionStateChip('failed');
  assert.equal(vm.runInContext('state.showHistory', sb), true, 'History auto-enabled for historical states');
});

// ── 2. Backend multi-select ──────────────────────────────────────────────

test('backend chips multi-select without touching the search field', () => {
  const sb = loadApp();
  seed(sb);
  sb.setBackendFilter('claude-code');
  assert.equal(vm.runInContext('state.sessionFilter', sb), '', 'the search box stays empty');
  assert.equal(cards(render(sb)), 4);

  sb.setBackendFilter('pi');
  assert.deepEqual(j(vm.runInContext('state.sessionBackendSel.slice().sort()', sb)), ['claude-code', 'pi']);
  assert.equal(cards(render(sb)), 8, 'two backends = 8 sessions');

  sb.setBackendFilter('pi'); // second click removes
  assert.deepEqual(j(vm.runInContext('state.sessionBackendSel.slice()', sb)), ['claude-code']);
  assert.equal(cards(render(sb)), 4);
});

test('the All chip clears the whole backend group', () => {
  const sb = loadApp();
  seed(sb);
  sb.setBackendFilter('opencode');
  sb.setBackendFilter('ollama');
  assert.equal(cards(render(sb)), 8);

  sb.sessionsFilterClearGroup('backend');
  assert.deepEqual(j(vm.runInContext('state.sessionBackendSel.slice()', sb)), []);
  assert.equal(cards(render(sb)), 16);
});

// ── 3. Summary row ───────────────────────────────────────────────────────

test('the summary row lists status and backend picks with remove buttons and Clear all', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('failed');
  sb.setBackendFilter('claude-code');
  const html = render(sb);
  assert.equal(cards(html), 1);
  assert.match(html, /class="sessions-tag-selected"/, 'summary row present');
  assert.match(html, /data-filter-chip="status:failed"/);
  assert.match(html, /data-filter-chip="backend:claude-code"/);
  assert.match(html, /onclick="setSessionStateChip\('failed'\)"/, 'status chip removes itself');
  assert.match(html, /onclick="setBackendFilter\('claude-code'\)"/, 'backend chip removes itself');
  assert.match(html, /onclick="sessionsSummaryClearAll\(\)"/, 'Clear all button');

  sb.sessionsSummaryClearAll();
  const html2 = render(sb);
  assert.ok(!html2.includes('sessions-tag-selected'), 'summary row empty again');
  assert.equal(cards(html2), 16);
});

// ── 4. Filter badge count ────────────────────────────────────────────────

test('_sessionsActiveFilterCount counts each non-empty group once', () => {
  const sb = loadApp();
  seed(sb);
  assert.equal(sb._sessionsActiveFilterCount(), 0);
  sb.setSessionStateChip('running');
  assert.equal(sb._sessionsActiveFilterCount(), 1);
  sb.setSessionStateChip('failed');
  assert.equal(sb._sessionsActiveFilterCount(), 1, 'two statuses are still one group');
  sb.setBackendFilter('claude-code');
  sb.setBackendFilter('pi');
  assert.equal(sb._sessionsActiveFilterCount(), 2, 'two backends are still one group');
});

// ── 5. Persistence + migration ──────────────────────────────────────────

test('status and backend selections persist under the new keys and migrate from the legacy key', () => {
  const storage = memStorage();
  storage.setItem('cs_session_state_chip', 'waiting_input'); // legacy single pick
  const sb = loadApp({ storage });
  seed(sb, 'delete state.sessionStateChip;');
  render(sb);
  assert.deepEqual(j(vm.runInContext('state.sessionStateChips.slice()', sb)), ['waiting_input'], 'legacy key migrated');

  sb.setSessionStateChip('running');
  sb.setBackendFilter('ollama');
  assert.deepEqual(JSON.parse(storage.getItem('cs_session_state_chips')), ['waiting_input', 'running']);
  assert.deepEqual(JSON.parse(storage.getItem('cs_session_backend_sel')), ['ollama']);

  // A refresh-like new sandbox reads the stored sets back.
  const sb2 = loadApp({ storage });
  seed(sb2, 'delete state.sessionStateChip;');
  render(sb2);
  assert.deepEqual(j(vm.runInContext('state.sessionStateChips.slice()', sb2)).sort(), ['running', 'waiting_input']);
  assert.deepEqual(j(vm.runInContext('state.sessionBackendSel.slice()', sb2)), ['ollama']);
});

// ── 6. Saved views ──────────────────────────────────────────────────────

test('a saved view carries the backend set, and a legacy view migrates its backend out of the search text', () => {
  const storage = memStorage();
  const sb = loadApp({ storage });
  seed(sb);
  sb.setBackendFilter('pi');
  const v = sb.saveSessionsView('my pick');
  assert.ok(v && v.id);
  assert.deepEqual(j(v.backends), ['pi']);

  sb.sessionsFilterClearGroup('backend');
  assert.equal(vm.runInContext('state.sessionBackendSel.length', sb), 0);

  assert.equal(sb.applySavedView(v.id), true);
  assert.deepEqual(j(vm.runInContext('state.sessionBackendSel.slice()', sb)), ['pi'], 'the set comes back');
  assert.equal(vm.runInContext('state.sessionFilter', sb), '');

  // Legacy view shape: backend family stored in `filter`, no `backends`.
  const views = JSON.parse(storage.getItem('cs_sessions_saved_views') || '[]');
  const legacy = { id: 'sv-legacy', name: 'old', mode: 'list', groupBy: null, filter: 'claude-code' };
  views.push(legacy);
  storage.setItem('cs_sessions_saved_views', JSON.stringify(views));
  vm.runInContext('state._savedViews = null;', sb);

  assert.equal(sb.applySavedView('sv-legacy'), true);
  assert.deepEqual(j(vm.runInContext('state.sessionBackendSel.slice()', sb)), ['claude-code'], 'backend moved into the set');
  assert.equal(vm.runInContext('state.sessionFilter', sb), '', 'search box left clean');
});
