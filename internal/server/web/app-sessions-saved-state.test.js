'use strict';
// The whole set of "remembered on this device" values the Sessions page keeps
// in localStorage is read through ONE shared, defensive helper: _safeStored.
// This file pins that contract down, because a malformed/stale saved value is
// exactly the failure the operator hit (the page threw while reading saved
// state and the rest of the tab never came up). The rule for every saved value:
//
//   * a value a prior release wrote and this release still understands is
//     honored — reading it back returns it, unchanged;
//   * a value that is not what this release writes (garbage, an old shape, an
//     unknown bucket, a runaway length) is REJECTED with a default used instead.
//     Rejection never throws, never toasts — it removes the key from storage
//     (so the bad value is not read again and again) and drops one line on
//     console.info;
//   * one bad key can never block the others: every read is independent, and
//     the page still renders with the defaults where a value was rejected.
//
// There is no JS test framework in this PWA (no package.json, no bundler);
// following the established pattern in this directory (the other
// app-sessions-*.test.js files, testutil_browser_stub.js) this is a plain Node
// script that loads the real, unmodified app.js into a stub browser and
// asserts on the saved-state reads.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { buildSandbox, loadScript, vm, makeStubElement }
  = require('./testutil_browser_stub.js');

const WEB_DIR = __dirname;
const APP_JS = path.join(WEB_DIR, 'app.js');

// The exact localStorage keys this file exercises (mirrors app.js so a silent
// rename on either side fails the test loudly).
const KEY_ORDER = 'cs_session_order';
const KEY_WATCHED = 'cs_sessions_watched';
const KEY_MUTED = 'cs_sessions_muted';
const KEY_MODE = 'cs_sessions_view_mode';
const KEY_SEARCH = 'cs_sessions_search';
const KEY_COLLAPSED = 'cs_sessions_collapsed_groups';
const KEY_VIEWS = 'cs_sessions_saved_views';
const KEY_CHIP = 'cs_session_state_chip';
const KEY_FILTERS_COLLAPSED = 'cs_filters_collapsed';
const KEY_GROUP_STORE = 'dw_sessions_group_by';
const KEY_GROUP_LEGACY = 'dw_sessions_group_axis';

const plain = v => JSON.parse(JSON.stringify(v));
const run = (sbx, code) => vm.runInContext(code, sbx, { filename: '<saved-state-test>' });

// The state chip's reader, mirroring the exact read app.js uses in
// renderSessionsView: a remembered value is only accepted if it is a known
// bucket/state (SESSIONS_STATE_CHIP_KEYS); anything else falls back to 'all'
// and is forgotten from storage.
function chipReaderCode() {
  return "_safeStored('cs_session_state_chip', (raw)=>(typeof raw==='string' && SESSIONS_STATE_CHIP_KEYS.indexOf(raw)>=0)?raw:null, 'all')";
}

// boot builds a fresh app.js sandbox whose device storage is pre-seeded with
// `seed` BEFORE the app's top-level runs — so the state initializers read
// exactly what a returning operator would have. It also swaps in a console
// spy so the discard log lines can be asserted (and so no real stdout noise).
// Returns { sbx, logs, storage } where `storage` is the app's localStorage.
function boot(seed, { withElId = true } = {}) {
  const byId = withElId
    ? { view: makeStubElement(), 'view-sessions': makeStubElement(), sessionFilterInput: makeStubElement() }
    : {};
  const sandbox = buildSandbox({ byId });
  for (const [k, v] of Object.entries(seed || {})) sandbox.localStorage.setItem(k, v);
  const logs = { out: [] };
  sandbox.console = {
    info: (...a) => { logs.out.push(a.join(' ')); },
    log() {}, warn() {}, error() {}, debug() {},
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;
  return { sbx: sandbox, logs, storage: sandbox.localStorage };
}

// aLs is a plain, host-side localStorage stand-in for exercising _safeStored's
// contract in isolation: it records every removeItem so a rejection can be
// proven to actually forget the key.
function aLs(init) {
  const m = {};
  for (const [k, v] of Object.entries(init || {})) m[k] = v;
  return {
    removed: [],
    getItem(k) { return (k in m) ? m[k] : null; },
    setItem(k, v) { m[k] = String(v); },
    removeItem(k) { delete m[k]; this.removed.push(k); },
    has(k) { return k in m; },
  };
}

// ── _safeStored contract, in isolation ─────────────────────────────────────
test('_safeStored: a valid value is honored; a missing key is a clean default', () => {
  const { sbx } = boot({});
  const ok = aLs({ k: '["a","b"]' });
  assert.deepEqual(plain(sbx._safeStored('k', sbx._storedStrArrayRead, ['z'], ok)), ['a', 'b']);
  assert.ok(ok.has('k'), 'an accepted key is left in place');

  const absent = aLs({});
  assert.deepEqual(plain(sbx._safeStored('nope', sbx._storedStrArrayRead, ['z'], absent)), ['z']);
  assert.deepEqual(absent.removed, [], 'a missing key is not removed');
});

test('_safeStored: a rejected value is forgotten, logged once, and the default used', () => {
  const { sbx, logs } = boot({});
  sbx.console = { ...sbx.console, info: (...a) => logs.out.push(a.join(' ')) };
  const ls = aLs({ k: '42' }); // a number: not the string-array this key holds
  const res = plain(sbx._safeStored('k', sbx._storedStrArrayRead, ['z'], ls));
  assert.deepEqual(res, ['z'], 'the fallback is used');
  assert.deepEqual(ls.removed, ['k'], 'the bad key is removed so it is not re-read');
  assert.equal(ls.has('k'), false, 'the key is really gone');
  assert.ok(logs.out.some(line => line.includes('k')), 'one console.info line names the key');
});

test('_safeStored: a reader that throws is a rejection, not a crash', () => {
  const { sbx } = boot({});
  const ls = aLs({ k: 'whatever' });
  const r = (x) => { throw new Error('reader exploded'); };
  assert.doesNotThrow(() => plain(sbx._safeStored('k', r, 'FB', ls)));
  assert.equal(plain(sbx._safeStored('k', r, 'FB', ls)), 'FB');
  assert.ok(ls.removed.includes('k'), 'a throwing reader still forgets the key');
});

test('_safeStored: a blocked or absent storage degrades to the default', () => {
  const { sbx } = boot({});
  const boom = {
    getItem() { throw new Error('denied'); },
    setItem() { throw new Error('denied'); },
    removeItem() { throw new Error('denied'); },
  };
  assert.equal(sbx._safeStored('k', sbx._storedStrArrayRead, 'DEF', boom), 'DEF');
  // A totally missing storage (ls === null) is the same story.
  assert.equal(sbx._safeStored('k', sbx._storedStrArrayRead, 'DEF', null), 'DEF');
  // A storage object with no getItem at all.
  assert.equal(sbx._safeStored('k', sbx._storedStrArrayRead, 'DEF', {}), 'DEF');
});

// ── (a) valid values a prior release wrote are honored ────────────────────
test('(a) order / watched / muted survive from a prior release, unchanged', () => {
  const { sbx } = boot({
    [KEY_ORDER]: JSON.stringify(['s-3', 's-1', 's-2']),
    [KEY_WATCHED]: JSON.stringify(['node-a/full-8', 'node-a/full-9']),
    [KEY_MUTED]: JSON.stringify(['node-b/muted-1']),
  });
  assert.deepEqual(plain(run(sbx, 'state.sessionOrder')), ['s-3', 's-1', 's-2']);
  assert.deepEqual(plain(run(sbx, '[...state.watchedSessions]')).sort(), ['node-a/full-8', 'node-a/full-9'].sort());
  assert.deepEqual(plain(run(sbx, '[...state.mutedSessions]')), ['node-b/muted-1']);
});

test('(a) a remembered view mode, chip and search are applied on load', () => {
  const { sbx } = boot({
    [KEY_MODE]: 'tiles',
    [KEY_CHIP]: 'waiting_input',
    [KEY_SEARCH]: 'etl',
  });
  assert.equal(run(sbx, chipReaderCode()), 'waiting_input');
  assert.equal(sbx._sessionsSearchReadStored(), 'etl');
  assert.equal(sbx._sessionsViewModeNormalize(sbx._sessionsReadStoredMode()), 'tiles');
});

test('(a) remembered collapsed groups + saved views are read back', () => {
  const { sbx } = boot({
    [KEY_COLLAPSED]: JSON.stringify(['state::running', 'node::node-b']),
    [KEY_VIEWS]: JSON.stringify([
      { id: 'v-1', name: 'Ops queue', mode: 'list', groupBy: { axis: 'tag', tagPrefix: 'ops:' }, filter: 'etl' },
      { id: 'v-2', name: 'Busy', mode: 'tiles', groupBy: null, filter: '' },
    ]),
    [KEY_FILTERS_COLLAPSED]: '0',
  });
  assert.deepEqual(plain(sbx._sessionsCollapsedRead(sbx.localStorage)).sort(), ['node::node-b', 'state::running']);
  assert.equal(plain(sbx._savedViewsRead(sbx.localStorage)).length, 2);
  assert.equal(plain(sbx._savedViewsRead(sbx.localStorage)).every(v => v.id && v.name), true);
  // filters expanded is remembered ('0' = expanded).
  assert.equal(run(sbx, "_safeStored('cs_filters_collapsed', (raw)=>(raw==='0'||raw==='1')?raw:null, '1')"), '0');
});

test('(a) a remembered group axis is honored, and the legacy bare axis migrates', () => {
  // Current shape.
  {
    const { sbx } = boot({ [KEY_GROUP_STORE]: JSON.stringify({ axis: 'node' }) });
    assert.equal(plain(sbx._sessionsGroupCurrent()).axis, 'node');
  }
  // The older shape (a bare axis string under the legacy key).
  {
    const { sbx } = boot({ [KEY_GROUP_LEGACY]: 'tag' });
    const by = plain(sbx._sessionsGroupCurrent());
    assert.equal(by.axis, 'tag', 'legacy bare axis becomes a tag-prefix group');
  }
});

// ── (b) every malformed / old value falls back to the default, no throw ───
test('(b) a non-array / non-string-array under cs_session_order is rejected', () => {
  for (const bad of ['not json', '42', '{"a":1}', '[1,2]', '["ok", 5]']) {
    const { sbx, storage } = boot({ [KEY_ORDER]: bad });
    assert.deepEqual(plain(run(sbx, 'state.sessionOrder')), [], 'value ' + bad);
    assert.equal(storage.getItem(KEY_ORDER), null, 'the bad key is forgotten (' + bad + ')');
  }
});

test('(b) a bad watched/muted value empties the set and forgets the key', () => {
  for (const key of [KEY_WATCHED, KEY_MUTED]) {
    for (const bad of ['garbage', '"just a string"', '[1]', 'true']) {
      const { sbx, storage } = boot({ [key]: bad });
      const set = key === KEY_WATCHED ? 'watchedSessions' : 'mutedSessions';
      assert.equal(run(sbx, 'state.' + set + '.size'), 0, key + ' <- ' + bad);
      assert.equal(storage.getItem(key), null, 'forgotten: ' + key + ' <- ' + bad);
    }
  }
});

test('(b) an unknown view mode falls back and is forgotten', () => {
  for (const bad of ['bogus-mode', 'TILES', '', '42']) {
    const { sbx, storage } = boot({ [KEY_MODE]: bad });
    assert.equal(sbx._sessionsReadStoredMode(), null, 'bad mode read as null: ' + bad);
    assert.equal(storage.getItem(KEY_MODE), null, 'forgotten: ' + bad);
    assert.doesNotThrow(() => sbx._sessionsViewMode(), 'the viewport default still works: ' + bad);
  }
});

test('(b) a runaway search (past the cap) is rejected and forgotten', () => {
  // A stored search is a plain string. "42" or "etl" are legitimate searches and
  // stay; the only thing that disqualifies one is going past the length cap
  // (a runaway value, not a human typing).
  for (const bad of ['x'.repeat(501), 'x'.repeat(9000)]) {
    const { sbx, storage } = boot({ [KEY_SEARCH]: bad });
    assert.equal(sbx._sessionsSearchReadStored(), '', 'bad search rejected');
    assert.equal(storage.getItem(KEY_SEARCH), null, 'forgotten');
  }
  // A non-string raw value (only reachable through a non-string storage) is
  // rejected by the typeof guard — never thrown.
  {
    const { sbx } = boot({});
    const strange = { getItem: () => 42, setItem() {}, removeItem() {} };
    assert.equal(sbx._sessionsSearchReadStored(strange), '', 'a non-string search is rejected');
  }
  // And the exact cap boundary is still accepted.
  const boundary = 'y'.repeat(500);
  {
    const { sbx } = boot({ [KEY_SEARCH]: boundary });
    assert.equal(sbx._sessionsSearchReadStored(), boundary, 'a value of exactly the cap is a real search');
  }
});

test('(b) a state chip that is not a known bucket/state falls back to the legacy default', () => {
  for (const bad of ['bogus', 'TILES', '42', '']) {
    const { sbx, storage } = boot({ [KEY_CHIP]: bad });
    // Render's exact read: a bad bucket falls back to 'all'...
    assert.equal(run(sbx, chipReaderCode()), 'all', 'bad chip -> all: ' + bad);
    // ...and that read forgets the key so it is not read again.
    assert.equal(storage.getItem(KEY_CHIP), null, 'forgotten: ' + bad);
  }
});

test('(b) a collapsed value that is not a list at all is rejected, but a list trims its stray entries', () => {
  for (const bad of ['garbage', '42', '{"a":1}', 'just a string']) {
    const { sbx, storage } = boot({ [KEY_COLLAPSED]: bad });
    assert.deepEqual(plain(sbx._sessionsCollapsedRead(sbx.localStorage)), [], 'value ' + bad);
    assert.equal(storage.getItem(KEY_COLLAPSED), null, 'forgotten: ' + bad);
  }
  // A real array keeps its string entries and drops the stray ones — it is
  // still a recognized shape, so the key is not forgotten (just trimmed).
  {
    const { sbx, storage } = boot({ [KEY_COLLAPSED]: JSON.stringify(['state::running', 9, '', 'node::node-b']) });
    assert.deepEqual(plain(sbx._sessionsCollapsedRead(sbx.localStorage)).sort(), ['node::node-b', 'state::running']);
    assert.ok(storage.getItem(KEY_COLLAPSED) !== null, 'a valid array keeps the key');
  }
});

test('(b) a saved-views value that is not an array of views is rejected', () => {
  for (const bad of ['garbage', '42', '{"views":[]}', 'solo']) {
    const { sbx, storage } = boot({ [KEY_VIEWS]: bad });
    assert.deepEqual(plain(sbx._savedViewsRead(sbx.localStorage)), [], 'value ' + bad);
    assert.equal(storage.getItem(KEY_VIEWS), null, 'forgotten: ' + bad);
  }
  // A recognized array keeps only the well-formed entries (id + name) and
  // drops the rest — so it may trim to empty while still keeping the key.
  {
    const { sbx, storage } = boot({ [KEY_VIEWS]: JSON.stringify([{}, null, { id: '', name: 'x' }]) });
    assert.deepEqual(plain(sbx._savedViewsRead(sbx.localStorage)), []);
    assert.ok(storage.getItem(KEY_VIEWS) !== null, 'a recognized array keeps the key');
  }
  // A list with one good and one bad entry keeps the good one only.
  {
    const { sbx } = boot({
      [KEY_VIEWS]: JSON.stringify([
        { id: 'v-1', name: 'Keep', mode: 'list', groupBy: null, filter: '' },
        { id: '', name: '' },
      ]),
    });
    const views = plain(sbx._savedViewsRead(sbx.localStorage));
    assert.equal(views.length, 1);
    assert.equal(views[0].name, 'Keep');
  }
});

test('(b) a state chip / filters value that is not a known token falls back', () => {
  for (const bad of ['2', 'nope', '{', '01', 'true']) {
    const { sbx, storage } = boot({ [KEY_FILTERS_COLLAPSED]: String(bad) });
    // The default is '1' (collapsed). A bad value -> collapsed, key forgotten.
    const v = run(sbx, "_safeStored('cs_filters_collapsed', (raw)=>(raw==='0'||raw==='1')?raw:null, '1')");
    assert.equal(v, '1', 'bad collapse token -> default ' + String(bad));
    assert.equal(storage.getItem(KEY_FILTERS_COLLAPSED), null, 'forgotten: ' + String(bad));
  }
});

test('(b) a group store of an unknown axis is rejected; the legacy path too', () => {
  for (const bad of ['not json', '{"axis":"bogus"}', '["node"]']) {
    const { sbx, storage } = boot({ [KEY_GROUP_STORE]: bad });
    assert.equal(sbx._sessionsGroupCurrent(), null, 'bad group -> no grouping: ' + bad);
    assert.equal(storage.getItem(KEY_GROUP_STORE), null, 'forgotten: ' + bad);
  }
  // A valid axis survives even if its tagPrefix is the wrong type — it is
  // coerced to '' rather than the whole grouping being discarded.
  {
    const { sbx } = boot({ [KEY_GROUP_STORE]: JSON.stringify({ axis: 'tag', tagPrefix: 42 }) });
    const by = plain(sbx._sessionsGroupCurrent());
    assert.equal(by.axis, 'tag');
    assert.equal(by.tagPrefix, '');
  }
});

// ── (c) one bad key does not block reading the others ─────────────────────
test('(c) one bad key does not block the others', () => {
  const seed = {
    // the bad ones:
    [KEY_MUTED]: '{"unexpected":1}',      // object, not a string array
    [KEY_SEARCH]: 'x'.repeat(900),        // runaway length
    [KEY_GROUP_STORE]: '{"axis":"nonsense"}',
    [KEY_CHIP]: 'not-a-bucket',
    // the good ones that must still be honored:
    [KEY_ORDER]: JSON.stringify(['s-3', 's-1', 's-2']),
    [KEY_WATCHED]: JSON.stringify(['full/8', 'full/9']),
    [KEY_MODE]: 'tiles',
    [KEY_COLLAPSED]: JSON.stringify(['state::running']),
  };
  const { sbx } = boot(seed);
  assert.deepEqual(plain(run(sbx, 'state.sessionOrder')), ['s-3', 's-1', 's-2'], 'good order survives');
  assert.equal(run(sbx, 'state.watchedSessions.size'), 2, 'good watched survives');
  assert.equal(sbx._sessionsSearchReadStored(), '', 'bad search -> empty');
  assert.deepEqual(plain(run(sbx, '[...state.mutedSessions]')), [], 'bad muted -> empty');
  assert.equal(sbx._sessionsGroupCurrent(), null, 'bad group -> none');
  assert.equal(run(sbx, chipReaderCode()), 'all', 'bad chip -> all');
  assert.deepEqual(plain(sbx._sessionsCollapsedRead(sbx.localStorage)), ['state::running'], 'good collapsed survives');
  assert.equal(sbx._sessionsReadStoredMode(), 'tiles', 'good mode survives');
});

test('(c) a corrupted device yields defaults across the board, and the page still renders', () => {
  const seed = {};
  // Poke a lot of recognized keys in ways a prior/other release might have.
  seed[KEY_ORDER] = 'corrupt';
  seed[KEY_WATCHED] = '[]';
  seed[KEY_MUTED] = '["a", 3]';
  seed[KEY_MODE] = 'weird';
  seed[KEY_SEARCH] = 'x'.repeat(3000);
  seed[KEY_COLLAPSED] = '99';
  seed[KEY_VIEWS] = '"solo"';
  seed[KEY_CHIP] = 'mystery';
  seed[KEY_FILTERS_COLLAPSED] = '7';
  seed[KEY_GROUP_STORE] = 'notjson';
  const cap = { html: '' };
  const el = { querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return cap.html; }, set(v) { cap.html = v; } });
  const sandbox = buildSandbox({ byId: { view: el } });
  for (const [k, v] of Object.entries(seed)) sandbox.localStorage.setItem(k, v);
  sandbox.console = { info() {}, log() {}, warn() {}, error() {}, debug() {} };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(WEB_DIR, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.bundle = en;
  sandbox._i18n.fallback = en;

  // Defaults everywhere — nothing threw during load even with a corrupted device.
  assert.deepEqual(plain(run(sandbox, 'state.sessionOrder')), []);
  assert.equal(run(sandbox, 'state.watchedSessions.size'), 0);
  assert.equal(run(sandbox, 'state.mutedSessions.size'), 0);
  assert.equal(sandbox._sessionsReadStoredMode(), null);
  assert.equal(sandbox._sessionsSearchReadStored(), '');
  assert.deepEqual(plain(sandbox._sessionsCollapsedRead(sandbox.localStorage)), []);
  assert.deepEqual(plain(sandbox._savedViewsRead(sandbox.localStorage)), []);
  assert.equal(run(sandbox, chipReaderCode()), 'all');
  assert.equal(sandbox._sessionsGroupCurrent(), null);

  // And the whole Sessions view still paints with that corrupted device.
  run(sandbox, "state.sessions = [{id:'s-1', name:'work-1', state:'running', updated_at:'2026-09-01T00:00:00Z'}]; state.activeView='sessions'; state.showHistory=true; state.sessionFilter=''; state.sessionTreeView=false; state.sessionsViewMode='list'; state._sessionsSheetOpen=false; state._filtersCollapsed=true;");
  assert.doesNotThrow(() => sandbox.renderSessionsView(), 'corrupted device must not take the page down');
  assert.ok(cap.html && cap.html.length > 0, 'the page rendered real HTML despite a corrupted device');
});

// ── the render guard: one bad optional block cannot sink the page ─────────
test('_sessionsRenderGuard: a block that throws is skipped, not fatal', () => {
  const { sbx } = boot({});
  assert.equal(sbx._sessionsRenderGuard('demo', () => 'ok-html'), 'ok-html');
  assert.doesNotThrow(() => {
    const out = sbx._sessionsRenderGuard('demo', () => { throw new Error('block failed'); });
    assert.equal(out, '', 'a failed block renders as absent');
  });
  // A non-function block is also tolerated.
  assert.equal(sbx._sessionsRenderGuard('demo', null), '');
});
