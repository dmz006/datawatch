// Sessions search box: matcher, count text, stored text, one-shot debounce,
// and the rendered single toolbar row with the count line.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;

function loadApp({ byId = {} } = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function viewElement() {
  const box = { capture: '' };
  const el = { querySelector: () => null, querySelectorAll: () => [] };
  Object.defineProperty(el, 'innerHTML', { get() { return box.capture; }, set(v) { box.capture = v; } });
  return { box, el };
}

const S = {
  id: 'ab12', full_id: 'local/ab12cdef', name: 'Nightly Build', state: 'waiting_input',
  backend_family: 'claude-code', llm_ref: 'Opus-Max', tags: ['own-tag'], inherited_tags: ['inh-tag'],
};

test('matcher is case-insensitive over name, ids, backend, llm ref, tags and state', () => {
  const sb = loadApp();
  const m = (q, s) => sb._sessionsSearchMatch(s || S, q);
  assert.equal(m(''), true);
  assert.equal(m('   '), true);
  assert.equal(m('NIGHTLY'), true);
  assert.equal(m('ab12'), true);
  assert.equal(m('AB12CDEF'), true);
  assert.equal(m('claude'), true);
  assert.equal(m('opus-max'), true);
  assert.equal(m('OWN-TAG'), true);
  assert.equal(m('inh-tag'), true);
  assert.equal(m('waiting'), true);
  assert.equal(m('zzz'), false);
  assert.equal(sb._sessionsSearchMatch(null, 'x'), false);
});

test('matcher resolves registry tag names', () => {
  const sb = loadApp();
  vm.runInContext(`_tagsState.byId = { t9: { id: 't9', name: 'Release Train' } };`, sb);
  assert.equal(sb._sessionsSearchMatch({ id: 'a', tags: ['t9'] }, 'release tr'), true);
  assert.equal(sb._sessionsSearchMatch({ id: 'a', inherited_tags: ['t9'] }, 'train'), true);
  assert.equal(sb._sessionsSearchMatch({ id: 'a', tags: [] }, 'train'), false);
});

test('count text has an English fallback and honors a bundle string', () => {
  const sb = loadApp();
  assert.equal(sb._sessionsSearchCountText(3, 40), '3 of 40 sessions');
  vm.runInContext(`_i18n.bundle.sessions_search_count = '%1$s / %2$s';`, sb);
  assert.equal(sb._sessionsSearchCountText(3, 40), '3 / 40');
});

test('stored text round-trips and tolerates missing or throwing storage', () => {
  const sb = loadApp();
  const mem = {}; const ls = { getItem: k => (k in mem ? mem[k] : null), setItem: (k, v) => { mem[k] = v; }, removeItem: k => { delete mem[k]; } };
  assert.equal(sb._sessionsSearchReadStored(ls), '');
  sb._sessionsSearchWriteStored('alpha', ls);
  assert.equal(mem[sb.SESSIONS_SEARCH_KEY], 'alpha');
  assert.equal(sb._sessionsSearchReadStored(ls), 'alpha');
  sb._sessionsSearchWriteStored('', ls);
  assert.equal(sb._sessionsSearchReadStored(ls), '');
  const bad = { getItem() { throw new Error('x'); }, setItem() { throw new Error('x'); }, removeItem() { throw new Error('x'); } };
  assert.equal(sb._sessionsSearchReadStored(bad), '');
  sb._sessionsSearchWriteStored('q', bad);
});

test('render shows one search row, a clear button only with text, and the count line only when filtering', () => {
  const { box, el } = viewElement();
  const sb = loadApp({ byId: { view: el } });
  vm.runInContext(`state.activeView = 'sessions'; state.showHistory = true; state.sessionStateChip = 'all';
    state.sessionWatchFilter = false; state.sessionsViewMode = 'list'; state._filtersCollapsed = true;
    state.sessions = [
      { id: 'a1', full_id: 'local/a1', name: 'alpha one', state: 'running', backend_family: 'claude-code' },
      { id: 'b1', full_id: 'local/b1', name: 'beta one', state: 'complete', backend_family: 'claude-code' },
      { id: 'b2', full_id: 'local/b2', name: 'beta two', state: 'complete', backend_family: 'claude-code' }];
    state.sessionFilter = '';`, sb);
  sb.renderSessionsView();
  assert.equal((box.capture.match(/class="sessions-search-row"/g) || []).length, 1);
  assert.match(box.capture, /sessionsFiltersSlot/);
  assert.match(box.capture, /id="sessionsSearchClear"[^>]* hidden/);
  assert.ok(!box.capture.includes('sessions-search-count'), 'no count line without search or filter');

  vm.runInContext(`state.sessionFilter = 'BETA';`, sb);
  sb.renderSessionsView();
  assert.match(box.capture, /2 of 3 sessions/);
  assert.ok(!/id="sessionsSearchClear"[^>]* hidden/.test(box.capture));
  assert.ok(!box.capture.includes('alpha one'));
});

test('typing debounces to one timer and clear cancels it', () => {
  const sb = loadApp();
  const timers = []; let cleared = 0;
  sb.setTimeout = (fn, ms) => { timers.push({ fn, ms }); return timers.length; };
  sb.clearTimeout = () => { cleared++; };
  vm.runInContext(`state.activeView = 'other';`, sb);
  sb.sessionsSearchInput({ value: 'a', selectionStart: 1, selectionEnd: 1 });
  sb.sessionsSearchInput({ value: 'al', selectionStart: 2, selectionEnd: 2 });
  assert.equal(timers.length, 2);
  assert.equal(cleared, 1, 'the earlier timeout was cancelled before the next was set');
  assert.equal(timers[1].ms, 150);
  assert.equal(vm.runInContext('state.sessionFilter', sb), 'al');
  sb.sessionsSearchClear();
  assert.equal(vm.runInContext('state.sessionFilter', sb), '');
  assert.equal(vm.runInContext('state._sessionsSearchTimer', sb), null);
});
