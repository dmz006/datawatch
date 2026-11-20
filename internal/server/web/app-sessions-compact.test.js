'use strict';
// Sessions page, compact layout: search, Filters sheet, status counts,
// group folding, phone tag collapse and the one-row phone toolbar, driven
// against a few-hundred-session fixture through the real app script.
//
// Run: node --test internal/server/web/app-sessions-compact.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const CSS = fs.readFileSync(path.join(WEB, 'style.css'), 'utf8');

function memStorage() {
  const m = new Map();
  return {
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
  };
}

function viewElement(slot) {
  const box = { html: '' };
  const el = {
    querySelector: () => null,
    querySelectorAll: () => [],
    insertBefore() {},
  };
  Object.defineProperty(el, 'innerHTML', { get() { return box.html; }, set(v) { box.html = v; } });
  return { box, el };
}

function loadApp({ storage, phone = false, view } = {}) {
  const v = view || viewElement();
  const sandbox = buildSandbox({ byId: { view: v.el } });
  if (storage) sandbox.localStorage = storage;
  sandbox.matchMedia = (q) => ({ matches: phone && /max-width:\s*640px/.test(q), addEventListener() {}, addListener() {} });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  sandbox.__box = v.box;
  return sandbox;
}

// 320 sessions: 4 states (80 each), 4 backends, tags t0..t3 by registry id.
const STATES = ['running', 'waiting_input', 'complete', 'failed'];
const BACKENDS = ['claude-code', 'opencode', 'pi', 'ollama'];
function fixture() {
  const out = [];
  for (let i = 0; i < 320; i++) {
    out.push({
      id: 'id' + String(i).padStart(3, '0'),
      full_id: 'local/id' + String(i).padStart(3, '0'),
      name: 'job-' + i + (i === 7 ? ' Zebra Special' : ''),
      state: STATES[i % 4],
      backend_family: BACKENDS[(i >> 2) % 4],
      tags: ['t' + (i % 4)],
      inherited_tags: [],
      updated_at: new Date().toISOString(),
      created_at: new Date().toISOString(),
    });
  }
  return out;
}

function seed(sb, extra = '') {
  sb.__fx = fixture();
  vm.runInContext(`state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionWatchFilter = false; state.sessionsViewMode = 'list'; state._filtersCollapsed = true;
    state.sessionFilter = ''; state.sessions = globalThis.__fx;
    _tagsState.byId = { t0: { id: 't0', name: 'Alpha Lane' }, t1: { id: 't1', name: 'Beta Lane' }, t2: { id: 't2', name: 'Gamma Lane' }, t3: { id: 't3', name: 'Delta Lane' } };
    _tagsState.loaded = true; ${extra}`, sb);
}

function render(sb) { sb.renderSessionsView(); return sb.__box.html; }
function countOf(html, re) { return (html.match(re) || []).length; }

// ── (1) search ──────────────────────────────────────────────────────────
test('search matches name, id, backend, tag text and state, case-insensitively, over 320 sessions', () => {
  const sb = loadApp();
  seed(sb);
  const n = (q) => sb.__fx.filter(s => sb._sessionsSearchMatch(s, q)).length;
  assert.equal(n('ZEBRA SPECIAL'), 1);
  assert.equal(n('id123'), 1);
  assert.equal(n('ID123'), 1);
  assert.equal(n('opencode'), 80);
  assert.equal(n('OpenCode'), 80);
  assert.equal(n('alpha lane'), 80);
  assert.equal(n('ALPHA'), 80);
  assert.equal(n('waiting_input'), 80);
  assert.equal(n('FAILED'), 80);
  assert.equal(n('no-such-thing'), 0);
});

test('search renders N of M, the clear button and only the matching cards', () => {
  const sb = loadApp();
  seed(sb);
  let html = render(sb);
  assert.ok(!html.includes('sessions-search-count'));
  assert.match(html, /id="sessionsSearchClear"[^>]* hidden/);
  vm.runInContext(`state.sessionFilter = 'beta lane';`, sb);
  html = render(sb);
  assert.match(html, /80 of 320 sessions/);
  assert.ok(!/id="sessionsSearchClear"[^>]* hidden/.test(html));
  vm.runInContext(`state.sessionFilter = 'zebra';`, sb);
  html = render(sb);
  assert.match(html, /1 of 320 sessions/);
  assert.ok(html.includes('Zebra Special'));
});

test('typing is debounced with one short one-shot timeout, and clear empties the box', () => {
  const sb = loadApp();
  seed(sb);
  const timers = []; const cleared = [];
  sb.setTimeout = (fn, ms) => { timers.push({ fn, ms }); return timers.length; };
  sb.clearTimeout = (h) => { cleared.push(h); };
  sb.setInterval = () => { throw new Error('search must not start an interval'); };
  sb.sessionsSearchInput({ value: 'z', selectionStart: 1, selectionEnd: 1 });
  sb.sessionsSearchInput({ value: 'ze', selectionStart: 2, selectionEnd: 2 });
  sb.sessionsSearchInput({ value: 'zeb', selectionStart: 3, selectionEnd: 3 });
  assert.equal(timers.length, 3);
  assert.equal(cleared.length, 2, 'each new keystroke cancels the pending timeout');
  assert.ok(timers.every(t => t.ms > 0 && t.ms <= 300));
  assert.equal(vm.runInContext('state.sessionFilter', sb), 'zeb');
  timers[2].fn();
  assert.equal(vm.runInContext('state._sessionsSearchTimer', sb), null);
  assert.match(sb.__box.html, /1 of 320 sessions/);
  // Re-entering the page cancels any pending timeout.
  sb.sessionsSearchInput({ value: 'zebr', selectionStart: 4, selectionEnd: 4 });
  const before = cleared.length;
  render(sb);
  assert.equal(cleared.length, before + 1);
  assert.equal(vm.runInContext('state._sessionsSearchTimer', sb), null);
  sb.sessionsSearchClear();
  assert.equal(vm.runInContext('state.sessionFilter', sb), '');
  assert.ok(!sb.__box.html.includes('sessions-search-count'));
  assert.match(sb.__box.html, /id="sessionsSearchClear"[^>]* hidden/);
});

test('search text survives a fresh load and degrades when storage throws', () => {
  const storage = memStorage();
  const a = loadApp({ storage });
  seed(a);
  a.sessionsSearchInput({ value: 'gamma', selectionStart: 5, selectionEnd: 5 });
  const b = loadApp({ storage });
  seed(b);
  const html = render(b);
  assert.equal(vm.runInContext('state.sessionFilter', b), 'gamma');
  assert.match(html, /value="gamma"/);
  assert.match(html, /80 of 320 sessions/);

  const bad = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); }, removeItem() { throw new Error('blocked'); } };
  const c = loadApp();
  assert.equal(c._sessionsSearchReadStored(bad), '');
  assert.doesNotThrow(() => c._sessionsSearchWriteStored('x', bad));
  assert.doesNotThrow(() => c._sessionsSearchWriteStored('', bad));
  // The page itself keeps working when the search key cannot be read or written.
  const mem = memStorage();
  c.localStorage = {
    getItem: (k) => { if (k === c.SESSIONS_SEARCH_KEY) throw new Error('blocked'); return mem.getItem(k); },
    setItem: (k, v) => { if (k === c.SESSIONS_SEARCH_KEY) throw new Error('blocked'); mem.setItem(k, v); },
    removeItem: (k) => { if (k === c.SESSIONS_SEARCH_KEY) throw new Error('blocked'); mem.removeItem(k); },
  };
  seed(c);
  assert.doesNotThrow(() => render(c));
  assert.doesNotThrow(() => c.sessionsSearchInput({ value: 'delta', selectionStart: 5, selectionEnd: 5 }));
  assert.equal(vm.runInContext('state.sessionFilter', c), 'delta');
  assert.doesNotThrow(() => render(c));
  assert.match(c.__box.html, /80 of 320 sessions/);
  assert.doesNotThrow(() => c.sessionsSearchClear());
});

// ── (2) Filters sheet ───────────────────────────────────────────────────
function keydownHooks(sb) {
  const live = new Set();
  sb.document.addEventListener = (n, fn) => { if (n === 'keydown') live.add(fn); };
  sb.document.removeEventListener = (n, fn) => { if (n === 'keydown') live.delete(fn); };
  return live;
}

test('the Filters sheet opens, and closes by backdrop tap, close button and Escape', () => {
  const sb = loadApp();
  seed(sb);
  const live = keydownHooks(sb);
  let html = render(sb);
  assert.ok(!html.includes('id="sessionsSheetBackdrop"'));

  sb.sessionsFiltersOpen();
  html = sb.__box.html;
  assert.ok(html.includes('id="sessionsSheetBackdrop"'));
  assert.match(html, /role="dialog"/);
  assert.match(html, /onclick="sessionsFiltersClose\(\)"/);
  assert.equal(live.size, 1);

  // A tap inside the sheet does not close it; a tap on the backdrop does.
  const backdrop = {};
  sb.sessionsFiltersBackdrop({ target: {}, currentTarget: backdrop });
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), true);
  sb.sessionsFiltersBackdrop({ target: backdrop, currentTarget: backdrop });
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), false);
  assert.ok(!sb.__box.html.includes('id="sessionsSheetBackdrop"'));
  assert.equal(live.size, 0);

  sb.sessionsFiltersOpen();
  sb.sessionsFiltersClose();
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), false);
  assert.equal(live.size, 0);

  sb.sessionsFiltersOpen();
  const [handler] = [...live];
  handler({ key: 'a' });
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), true, 'other keys leave it open');
  handler({ key: 'Escape' });
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), false);
  assert.equal(live.size, 0, 'the Escape listener is gone after close');
});

test('the Filters sheet closes when leaving the Sessions page', () => {
  const sb = loadApp();
  seed(sb);
  const live = keydownHooks(sb);
  sb.sessionsFiltersOpen();
  assert.equal(live.size, 1);
  sb.sessionsFiltersClose(true);
  assert.equal(live.size, 0);
});

test('the Filters button shows a badge for grouping or a status chip and none with no preferences', () => {
  const sb = loadApp();
  seed(sb);
  let html = render(sb);
  assert.match(html, /id="sessionsFiltersBtn"/);
  assert.ok(!html.includes('sessionsFiltersBadge'));
  assert.equal(sb._sessionsActiveFilterCount(), 0);

  vm.runInContext(`state.sessionGroupBy = { axis: 'state' };`, sb);
  html = render(sb);
  assert.match(html, /id="sessionsFiltersBadge">1</);

  vm.runInContext(`state.sessionStateChip = 'running';`, sb);
  html = render(sb);
  assert.match(html, /id="sessionsFiltersBadge">2</);

  vm.runInContext(`state.sessionGroupBy = null; state.sessionStateChip = 'all';`, sb);
  html = render(sb);
  assert.ok(!html.includes('sessionsFiltersBadge'));
});

// ── (3) no state count line ─────────────────────────────────────────────
function assertNoCountLine(html, label) {
  assert.ok(!html.includes('sessions-summary'), label + ': no state count line');
  assert.ok(!/sessions 320</.test(html), label + ': no total in a count line');
  assert.ok(!/Running \d+ · /.test(html), label + ': no "Running N · ..." text');
}

test('the Sessions page draws no state count line in list, tiles and compact, grouped or not', () => {
  for (const mode of ['list', 'tiles', 'compact']) {
    for (const group of [null, 'state']) {
      const sb = loadApp();
      seed(sb, `state.sessionsViewMode = '${mode}'; state.sessionGroupBy = ${group ? `{ axis: '${group}' }` : 'null'};`);
      const html = render(sb);
      assert.ok(html.length > 0);
      assertNoCountLine(html, mode + (group ? ' grouped' : ' ungrouped'));
    }
  }
});

test('the state filter still works and no count line appears while it is set', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('waiting_input');
  const html = sb.__box.html;
  assert.equal(countOf(html, /class="session-card /g), 60, 'first page of the 80 waiting sessions');
  assertNoCountLine(html, 'filtered');
  assert.equal(vm.runInContext('state.sessionStateChip', sb), 'waiting_input', 'the state filter is applied');
  sb.setSessionStateChip('all');
  assert.equal(vm.runInContext('state.sessionStateChip', sb), 'all');
});

// ── (4) group headers and folding ───────────────────────────────────────
test('a group header folds and unfolds, shows name and count, and remembers it', () => {
  const storage = memStorage();
  const sb = loadApp({ storage });
  seed(sb, `state.sessionGroupBy = { axis: 'state' };`);
  let html = render(sb);
  assert.equal(countOf(html, /class="sessions-group-header"/g), 4);
  assert.match(html, /<span class="sgp-label">running<\/span>\s*<span class="sgp-count">80<\/span>/);
  assert.match(html, /aria-expanded="true"/);
  const cards0 = countOf(html, /class="session-card /g);
  assert.ok(cards0 > 0);

  const ck = encodeURIComponent('state::running');
  assert.equal(sb._sessionsGroupToggleCollapse(ck), true);
  html = sb.__box.html;
  assert.match(html, /<section class="sessions-group collapsed" data-sessions-group="state::running">/);
  assert.ok(countOf(html, /class="session-card /g) < cards0);

  // Survives another render and a fresh load on the same device.
  html = render(sb);
  assert.match(html, /sessions-group collapsed" data-sessions-group="state::running"/);
  const fresh = loadApp({ storage });
  seed(fresh, `state.sessionGroupBy = { axis: 'state' };`);
  html = render(fresh);
  assert.match(html, /sessions-group collapsed" data-sessions-group="state::running"/);
  assert.equal(countOf(html, /sessions-group collapsed"/g), 1);

  fresh._sessionsGroupToggleCollapse(ck);
  html = fresh.__box.html;
  assert.equal(countOf(html, /sessions-group collapsed"/g), 0);
});

test('fold-all folds every group and unfold-all opens them again', () => {
  const storage = memStorage();
  const sb = loadApp({ storage });
  seed(sb, `state.sessionGroupBy = { axis: 'state' };`);
  let html = render(sb);
  assert.match(html, /class="sessions-fold-all"[^>]*title="Fold every group"[^>]*aria-pressed="false"/);

  sb._sessionsFoldAllToggle();
  html = sb.__box.html;
  assert.equal(countOf(html, /sessions-group collapsed"/g), 4);
  assert.equal(countOf(html, /class="session-card /g), 0);
  assert.match(html, /class="sessions-fold-all"[^>]*title="Unfold every group"[^>]*aria-pressed="true"/);

  const fresh = loadApp({ storage });
  seed(fresh, `state.sessionGroupBy = { axis: 'state' };`);
  assert.equal(countOf(render(fresh), /sessions-group collapsed"/g), 4);

  fresh._sessionsFoldAllToggle();
  html = fresh.__box.html;
  assert.equal(countOf(html, /sessions-group collapsed"/g), 0);
  assert.match(html, /class="sessions-fold-all"[^>]*title="Fold every group"[^>]*aria-pressed="false"/);
});

// ── (5) phone width cards ───────────────────────────────────────────────
test('at phone width the +N chip expands the tags and the card keeps its parts', () => {
  const sb = loadApp({ phone: true });
  seed(sb, `state._summarizerEnabled = true;`);
  vm.runInContext(`state.sessionFilter = 'id000'; state.sessions[0].tags = ['t0', 't1', 't2', 't3'];
    state.sessions[0].last_response = 'done'; state.sessions[0].last_summary = 'sum';`, sb);
  const card = () => {
    const h = render(sb);
    const i = h.indexOf('data-full-id="local/id000"');
    assert.ok(i > 0);
    const j = h.indexOf('data-full-id=', i + 10);
    return h.slice(i, j > 0 ? j : undefined);
  };
  let c = card();
  assert.match(c, /session-tags-more-chip[^>]*aria-expanded="false"[^>]*>\+2</);
  assert.equal(countOf(c, /class="tag-chip/g), 2);

  sb._sessionTagsToggle(encodeURIComponent('local/id000'));
  c = card();
  assert.equal(countOf(c, /class="tag-chip/g), 4);
  assert.match(c, /session-tags-more-chip[^>]*aria-expanded="true"[^>]*>Less</);

  assert.match(c, /task" [^>]*>job-0</);
  assert.match(c, /data-state="running"/);
  assert.match(c, /class="backend-badge"[^>]*>claude-code</);
  assert.match(c, /class="id"[^>]*>id000</);
  assert.match(c, /&#9632; Stop</);
  assert.match(c, /&#129302; Summary</);
  assert.match(c, /class="sess-maximize-btn"/);
  assert.match(c, /showResponseViewer/);
  assert.match(c, /class="time">/);

  sb._sessionTagsToggle(encodeURIComponent('local/id000'));
  assert.equal(countOf(card(), /class="tag-chip/g), 2);
});

test('at wide width every tag chip stays and there is no +N chip', () => {
  const sb = loadApp({ phone: false });
  seed(sb);
  vm.runInContext(`state.sessions[0].tags = ['t0', 't1', 't2', 't3'];`, sb);
  const h = render(sb);
  assert.ok(!h.includes('session-tags-more-chip'));
});

// ── (6) one-line toolbar ────────────────────────────────────────────────────
test('the one-line toolbar holds server, search, Filters, fold-all and view mode in one row', () => {
  const sb = loadApp({ phone: true });
  seed(sb);
  vm.runInContext("state.sessionGroupBy = { axis: 'state' };", sb); // grouping active → the fold control is in the row
  const html = render(sb);
  const start = html.indexOf('<div class="sessions-topbar">');
  assert.ok(start >= 0);
  const top = html.slice(start, html.indexOf('<div class="session-list">'));
  const order = ['id="sessionsServerDropdown"', 'sessions-search-row', 'id="sessionFilterInput"',
    'class="sessions-filters-slot"', 'id="sessionsFiltersBtn"', 'sessions-fold-all', 'class="sessions-view-buttons"']
    .map(k => top.indexOf(k));
  assert.ok(order.every(i => i >= 0), 'all toolbar parts are inside the top row');
  assert.deepEqual(order, [...order].sort((a, b) => a - b));
  // The server control is the toolbar's own instance of the shared picker —
  // same options body, same onchange → selectServer() mutation path.
  assert.match(top, /onchange="onServerDropdownChange\(this\.value\)"/);
  // The view control is one role=group of three native buttons (list, tiles, compact).
  assert.match(top, /<div class="sessions-view-buttons" role="group"/);
  assert.match(top, /onclick="_sessionsSetViewMode\('tiles'\)"/);
  for (const m of ['list', 'tiles', 'compact']) {
    assert.match(top, new RegExp('data-mode="' + m + '"'), `view control offers ${m}`);
  }
  // fold-all is the compact icon button with tooltip + aria text.
  assert.match(top, /class="sessions-fold-all"[\s\S]*title="Fold every group"[\s\S]*aria-pressed/);
  assert.equal(countOf(top, /class="sessions-search-row"/g), 1);
  // The old server-chip slot is gone: server switching lives in the header
  // dropdown (#serverDropdown) plus this toolbar's own instance.
  assert.ok(!top.includes('sessions-topbar-servers'), 'no server-chip slot in the topbar');
});

test('the toolbar is laid out as a single non-wrapping row at every width', () => {
  const rule = (sel) => {
    const r = new RegExp('(?:^|\\n)\\s*' + sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}');
    const x = CSS.match(r);
    assert.ok(x, 'base rule for ' + sel);
    return x[1];
  };
  const top = rule('.sessions-topbar');
  assert.match(top, /display:\s*flex/);
  assert.match(top, /flex-wrap:\s*nowrap/); // non-wrapping in the base rule, not just a media query
  const search = rule('.sessions-topbar .sessions-search-row');
  assert.match(search, /flex:\s*1 1 auto/);
  assert.match(search, /min-width:\s*0/); // the only flexible member, so it absorbs the slack
  assert.match(rule('.sessions-topbar .sessions-fold-all'), /flex:\s*0 0 auto/);
  assert.match(CSS, /\.sessions-topbar \.sessions-fold-all\[aria-pressed="true"\]/); // pressed style
  assert.match(CSS, /\.sessions-topbar \.sessions-view-buttons \{[^}]*flex:\s*0 0 auto/);
  assert.match(CSS, /\.sessions-topbar \.sessions-view-btn \{[^}]*flex:\s*0 0 auto/);
  assert.match(CSS, /\.sessions-topbar \.sessions-view-btn\.active/);
  assert.match(CSS, /var\(--accent\)/);
  // The phone media block still exists for the tighter cap, not the layout.
  const blocks = [];
  const re = /@media \(max-width: 640px\) \{([\s\S]*?)\n\}/g;
  let m;
  while ((m = re.exec(CSS))) blocks.push(m[1]);
  const phone = blocks.join('\n');
  assert.match(phone, /\.sessions-topbar \.sessions-server-select \{[^}]*max-width/);
  assert.match(phone, /\.sessions-topbar \.sessions-view-buttons \{[^}]*max-width:\s*30%/);
  assert.match(phone, /\.sessions-filters-label \{ display: none; \}/);
});
