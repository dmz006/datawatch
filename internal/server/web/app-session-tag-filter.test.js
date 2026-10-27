// Session list tag filter: the session scope of the shared tag browser. Covers no
// selection, ANY and ALL over own and inherited tags, an unknown tag ID, the
// selection saved under cs_tag_browse_session and restored on load, clearing, an
// empty registry, and that renderSessionsView applies it to the pool it sorts.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;

// Work > Mobile, Work > Shared, and Home on its own.
const TAGS = [
  { id: 'w', name: 'Work' },
  { id: 'm', name: 'Mobile', parent_id: 'w' },
  { id: 's', name: 'Shared', parent_id: 'w' },
  { id: 'h', name: 'Home' },
];

const SESSIONS = [
  { full_id: 'n-a', id: 'a', state: 'running', tags: ['m'] },
  { full_id: 'n-b', id: 'b', state: 'running', tags: [], inherited_tags: ['h'] },
  { full_id: 'n-c', id: 'c', state: 'running', tags: ['s'], inherited_tags: ['h'] },
  { full_id: 'n-d', id: 'd', state: 'running', tags: [] },
  { full_id: 'n-e', id: 'e', state: 'running' },
];

function loadApp(opts) {
  const o = opts || {};
  const sandbox = buildSandbox(o);
  if (o.stored) sandbox.localStorage.setItem('cs_tag_browse_session', JSON.stringify(o.stored));
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  const list = o.tags === undefined ? TAGS : o.tags;
  vm.runInContext(`_tagsState.list = ${JSON.stringify(list)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  return sandbox;
}

function run(sb, code) { return vm.runInContext(code, sb); }

function select(sb, sel, mode) {
  run(sb, `_tagBrowse.session.sel = ${JSON.stringify(sel)}; _tagBrowse.session.mode = ${JSON.stringify(mode || '')};`);
}

function keptIds(sb) {
  return Array.from(run(sb, `_sessionTagFilter(${JSON.stringify(SESSIONS)})`)).map(s => s.id);
}

test('no selection keeps every session', () => {
  const sb = loadApp();
  assert.deepEqual(keptIds(sb), ['a', 'b', 'c', 'd', 'e']);
});

test('ANY matches own tags, inherited tags and descendants of a selected tag', () => {
  const sb = loadApp();
  select(sb, ['w'], 'any');
  assert.deepEqual(keptIds(sb), ['a', 'c']);
  select(sb, ['h'], 'any');
  assert.deepEqual(keptIds(sb), ['b', 'c']);
  select(sb, ['m', 'h'], 'any');
  assert.deepEqual(keptIds(sb), ['a', 'b', 'c']);
});

test('ALL needs every selected tag across own and inherited tags', () => {
  const sb = loadApp();
  select(sb, ['s', 'h'], 'all');
  assert.deepEqual(keptIds(sb), ['c']);
  select(sb, ['m', 'h'], 'all');
  assert.deepEqual(keptIds(sb), []);
});

test('the filter keeps the original session objects', () => {
  const sb = loadApp();
  select(sb, ['h'], 'any');
  const input = JSON.parse(JSON.stringify(SESSIONS));
  run(sb, 'globalThis.__in = null;');
  sb.__in = input;
  const out = Array.from(run(sb, '_sessionTagFilter(__in)'));
  assert.equal(out[0], input[1]);
  assert.equal(out[1], input[2]);
});

test('an unknown tag ID is ignored', () => {
  const sb = loadApp();
  select(sb, ['gone'], 'any');
  assert.deepEqual(keptIds(sb), ['a', 'b', 'c', 'd', 'e']);
  select(sb, ['gone', 'h'], 'all');
  assert.deepEqual(keptIds(sb), ['b', 'c']);
});

test('an empty registry keeps every session', () => {
  const sb = loadApp({ tags: [] });
  select(sb, ['h', 'm'], 'any');
  assert.deepEqual(keptIds(sb), ['a', 'b', 'c', 'd', 'e']);
});

test('the selection is saved under cs_tag_browse_session', () => {
  const sb = loadApp();
  select(sb, ['m', 'h'], 'all');
  run(sb, "_tagBrowseSave('session')");
  const saved = JSON.parse(sb.localStorage.getItem('cs_tag_browse_session'));
  assert.deepEqual(saved.sel, ['m', 'h']);
  assert.equal(saved.mode, 'all');
});

test('a saved selection is restored on load and filters the list', () => {
  const sb = loadApp({ stored: { view: 'list', sel: ['h'], mode: 'any' } });
  assert.deepEqual(Array.from(run(sb, '_tagBrowse.session.sel')), ['h']);
  assert.equal(run(sb, "_tagBrowseMode('session')"), 'any');
  assert.deepEqual(keptIds(sb), ['b', 'c']);
});

test('clearing removes all tag filtering and the saved selection', () => {
  const sb = loadApp();
  select(sb, ['h'], 'any');
  run(sb, "_tagBrowseSave('session')");
  run(sb, "tagBrowseClear('session')");
  assert.deepEqual(Array.from(run(sb, '_tagBrowse.session.sel')), []);
  assert.deepEqual(keptIds(sb), ['a', 'b', 'c', 'd', 'e']);
  assert.deepEqual(JSON.parse(sb.localStorage.getItem('cs_tag_browse_session')).sel, []);
});

test('renderSessionsView sorts only the sessions that pass the tag filter', () => {
  const sb = loadApp();
  let pooled = null;
  sb.sortSessionsByOrder = pool => { pooled = pool; return pool; };
  run(sb, `state.sessions = ${JSON.stringify(SESSIONS)}; state.activeView = 'sessions';`);
  select(sb, ['h'], 'any');
  run(sb, 'renderSessionsView()');
  assert.deepEqual(Array.from(pooled).map(s => s.id), ['b', 'c']);
});
