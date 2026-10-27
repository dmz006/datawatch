// Header Tags menu wiring — the button count and label follow the watch filter, the tag
// selection, a watch toggle, a session's own tags, a tag_update frame and the open session.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const WEB = __dirname;

function makeEl() {
  const el = makeStubElement();
  const own = { innerHTML: '', textContent: '' };
  // The shared stub defines these as fixed accessors; plain ones let a test read back what was written.
  const plain = Object.create(el);
  Object.defineProperty(plain, 'innerHTML', { get() { return own.innerHTML; }, set(v) { own.innerHTML = v; }, configurable: true });
  Object.defineProperty(plain, 'textContent', { get() { return own.textContent; }, set(v) { own.textContent = v; }, configurable: true });
  plain.attrs = {};
  plain.setAttribute = (k, v) => { plain.attrs[k] = String(v); };
  plain.getAttribute = k => (k in plain.attrs ? plain.attrs[k] : null);
  return plain;
}

const TAGS = [{ id: 'a', name: 'Alpha' }, { id: 'b', name: 'Beta' }, { id: 'c', name: 'Gamma' }];

function loadApp(opts) {
  const o = opts || {};
  const btn = makeEl();
  const chip = makeEl();
  const byId = Object.assign({}, o.noButton ? {} : { headerTagsBtn: btn, headerTagsCount: chip }, o.byId || {});
  const sandbox = buildSandbox({ byId });
  const timers = { queue: [], seq: 0 };
  sandbox.setTimeout = (fn, ms) => { const id = ++timers.seq; timers.queue.push({ id, fn, ms }); return id; };
  sandbox.clearTimeout = id => {
    const k = timers.queue.findIndex(x => x.id === id);
    if (k >= 0) timers.queue.splice(k, 1);
  };
  sandbox.URLSearchParams = URLSearchParams;
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  if (o.noButton) {
    const stubbed = sandbox.document.getElementById;
    sandbox.document.getElementById = id => (id === 'headerTagsBtn' ? null : stubbed(id));
  }
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  vm.runInContext(`_tagsState.list = ${JSON.stringify(TAGS)}; _tagsState.byId = _tagsById(_tagsState.list); _tagsState.loaded = true;`, sandbox);
  timers.queue.length = 0;
  sandbox.__timers = timers;
  sandbox.renderSessionsView = () => {};
  return { sb: sandbox, btn, chip };
}

const run = (h, code) => vm.runInContext(code, h.sb);

// spyRefresh counts the calls to _tagsMenuRefresh, then runs the real one.
function spyRefresh(h) {
  const real = h.sb._tagsMenuRefresh;
  const spy = { calls: 0 };
  h.sb._tagsMenuRefresh = () => { spy.calls++; return real(); };
  return spy;
}

test('the count and aria-label follow the watch filter and each tag, and reset when cleared', () => {
  const h = loadApp();
  const label = h.sb.t('tags_menu_button');
  h.sb.toggleSessionWatchFilter();
  assert.equal(h.chip.textContent, '1');
  assert.equal(h.btn.getAttribute('aria-label'), label + ' (1)');
  h.sb.tagBrowseToggleTag('session', 'a', true);
  assert.equal(h.chip.textContent, '2');
  h.sb.tagBrowseToggleTag('session', 'b', true);
  assert.equal(h.chip.textContent, '3');
  assert.equal(h.btn.getAttribute('aria-label'), label + ' (3)');
  h.sb.tagBrowseToggleTag('session', 'a', false);
  assert.equal(h.chip.textContent, '2');
  h.sb.tagBrowseClear('session');
  assert.equal(h.chip.textContent, '1', 'the watch filter still counts after the tags are cleared');
  h.sb.toggleSessionWatchFilter();
  assert.equal(h.chip.textContent, '');
  assert.equal(h.btn.getAttribute('aria-label'), label);
});

test('toggling a tag or the watch filter does not close an open menu', () => {
  const h = loadApp();
  run(h, '_tagsMenuOpen = true;');
  h.sb.toggleSessionWatchFilter();
  h.sb.tagBrowseToggleTag('session', 'a', true);
  assert.equal(run(h, '_tagsMenuOpen'), true);
  assert.equal(h.btn.getAttribute('aria-expanded'), 'true');
});

test('on the sessions view with no open session the session section is absent', () => {
  const h = loadApp();
  run(h, `state.activeView = 'sessions'; state.activeSession = '';`);
  const html = run(h, '_tagsMenuHTML()');
  assert.equal(html.includes(h.sb.t('tags_menu_this_session')), false);
  assert.equal(html.includes('toggleSessionWatch('), false);
  assert.equal(html.includes('openSessionTagsModal('), false);
  run(h, `state.activeView = 'session-detail'; state.activeSession = 'host-aaaa';`);
  const open = run(h, '_tagsMenuHTML()');
  assert.ok(open.includes(h.sb.t('tags_menu_this_session')));
  assert.ok(open.includes("toggleSessionWatch('host-aaaa')"));
});

test('Edit tags closes the menu and the watch toggle does not', () => {
  const h = loadApp();
  run(h, `state.activeView = 'session-detail'; state.activeSession = 'host-aaaa';`);
  const html = run(h, '_tagsMenuHTML()');
  assert.ok(html.includes("onclick=\"closeTagsMenu();openSessionTagsModal('host-aaaa')\""));
  assert.ok(html.includes("onclick=\"toggleSessionWatch('host-aaaa')\""));
  run(h, '_tagsMenuOpen = true;');
  h.sb.renderSessionDetail = () => {};
  h.sb.toggleSessionWatch('host-aaaa');
  assert.equal(run(h, '_tagsMenuOpen'), true);
});

test('_tagsMenuRefresh runs after a tag_update frame', async () => {
  const h = loadApp();
  run(h, `state.activeView = 'sessions';`);
  h.sb.apiFetch = () => Promise.resolve({ tags: TAGS });
  h.sb.loadSessionsList = () => Promise.resolve([]);
  const spy = spyRefresh(h);
  h.sb.handleMessage({ type: 'tag_update', data: {} });
  h.sb.__timers.queue.shift().fn();
  await flushAsync(20);
  assert.ok(spy.calls >= 1, '_tagsMenuRefresh ran');
});

test('_tagsMenuRefresh runs after a tag_update frame on the session detail', async () => {
  const h = loadApp({ byId: { sessionDetailTagRow: makeStubElement() } });
  run(h, `state.activeView = 'session-detail'; state.activeSession = 'host-aaaa'; state.sessions = [{ full_id: 'host-aaaa', id: 'aaaa', tags: [] }];`);
  h.sb.apiFetch = u => Promise.resolve(u.startsWith('/api/sessions/') ? { full_id: 'host-aaaa', id: 'aaaa', tags: ['a'] } : { tags: TAGS });
  const spy = spyRefresh(h);
  h.sb.handleMessage({ type: 'tag_update', data: {} });
  h.sb.__timers.queue.shift().fn();
  await flushAsync(20);
  assert.ok(spy.calls >= 1, '_tagsMenuRefresh ran');
});

test('_tagsMenuRefresh runs after toggleSessionWatch, and only when the menu exists', () => {
  const h = loadApp();
  h.sb.renderSessionDetail = () => {};
  const spy = spyRefresh(h);
  h.sb.toggleSessionWatch('host-aaaa');
  assert.equal(spy.calls, 1);
  assert.equal(run(h, "state.watchedSessions.has('host-aaaa')"), true);

  const bare = loadApp({ noButton: true });
  bare.sb.renderSessionDetail = () => {};
  const bareSpy = spyRefresh(bare);
  bare.sb.toggleSessionWatch('host-aaaa');
  assert.equal(bareSpy.calls, 0);
});

test('_tagsMenuRefresh runs after a session\'s own tags are saved', async () => {
  const h = loadApp();
  const s = { full_id: 'host-aaaa', id: 'aaaa', tags: [] };
  run(h, `state.activeView = 'sessions'; state.sessions = [${JSON.stringify(s)}];`);
  let modal = null;
  h.sb.showModal = m => { modal = m; };
  h.sb.showToast = () => {};
  h.sb.apiFetch = () => Promise.resolve(Object.assign({}, s, { tags: ['a'] }));
  h.sb.openSessionTagsModal('host-aaaa');
  await flushAsync(10);
  assert.ok(modal, 'the tags modal opened');
  const spy = spyRefresh(h);
  modal.onConfirm();
  await flushAsync(10);
  assert.ok(spy.calls >= 1, '_tagsMenuRefresh ran');
});

test('_tagsMenuRefresh runs when the open session changes', () => {
  const h = loadApp();
  h.sb.fetch = () => new Promise(() => {});
  const spy = spyRefresh(h);
  try { h.sb.renderSessionDetail('host-aaaa'); } catch (_) { /* the rest of the render needs a full page */ }
  assert.ok(spy.calls >= 1, '_tagsMenuRefresh ran');
});

test('the menu adds no storage key beyond the two existing ones', () => {
  const src = fs.readFileSync(path.join(WEB, 'app.js'), 'utf8');
  const start = src.indexOf('// The header Tags menu:');
  const end = src.indexOf('const _tagsState =');
  assert.ok(start > 0 && end > start);
  const menu = src.slice(start, end);
  assert.equal(/localStorage|sessionStorage|indexedDB/.test(menu), false);
  assert.ok(src.includes("'cs_tag_browse_' + scope"));
  assert.ok(src.includes("'cs_sessions_watched'"));
});
