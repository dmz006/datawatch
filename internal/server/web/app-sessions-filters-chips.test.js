// Sessions page filter chips (Status / Backend / Tags) — the toggle-off and
// multi-select behavior, end to end on the rendered list:
//   (a) Status chips: one pick shows only that state; two picks show the
//       union; a second click on the first pick removes exactly it; the
//       "All" chip clears the whole group.
//   (b) Backend chips: the same four checks on `backend_family`, with the
//       free-text search field (`state.sessionFilter`) provably untouched.
//   (c) The compact summary row above the list shows one removable chip per
//       selected Status / Backend / tag value; "Clear all" empties all three
//       groups at once.
//   (d) `aria-pressed="true"` is set exactly on the selected chip and on
//       "All" when the group is empty — and every chip is a <button>.
//   (e) `_sessionsActiveFilterCount()` counts each non-empty group once.
//   (f) Legacy: the old single-select `setSessionStateChip('running')`
//       caller and a stored `cs_session_state_chip` value still work.
//
// Same sandbox pattern as app-sessions-filters-sheet.test.js: app.js loaded
// unmodified into a vm context with the stub browser, real en.json strings.
//
// Run: node --test internal/server/web/app-sessions-filters-chips.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

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
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.__box = v.box;
  return sandbox;
}

// 8 sessions: 4 states x 2 backends (claude-code, pi), one each — every
// filtered pool is small enough that card count IS the pool count.
// Two sessions also carry the placement tag 'lane:alpha' so the tag group
// has something to match (failed+claude-code and running+pi).
const STATES = ['running', 'waiting_input', 'complete', 'failed'];
const BACKENDS = ['claude-code', 'pi'];
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
      tags: (st === 'failed' && bt === 'claude-code') || (st === 'running' && bt === 'pi') ? ['lane:alpha'] : [],
      system_tags: (st === 'failed' && bt === 'claude-code') || (st === 'running' && bt === 'pi') ? ['lane:alpha'] : [],
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
// vm results come from another realm — round-trip once so the host's
// deepEqual sees plain host-realm arrays.
function stateChips(sb) { return JSON.parse(JSON.stringify(vm.runInContext('state.sessionStateChips || []', sb))); }
function backendSel(sb) { return JSON.parse(JSON.stringify(vm.runInContext('state.sessionBackendSel || []', sb))); }
function tagSel(sb) { return JSON.parse(JSON.stringify(vm.runInContext('(_tagBrowse && _tagBrowse.session && _tagBrowse.session.sel) || []', sb))); }
// Exactly which state chips carry aria-pressed="true".
function pressedStates(html) {
  return [...html.matchAll(/<button\b[^>]*onclick="setSessionStateChip\('(\w+)'\)"[^>]*aria-pressed="true"/g)]
    .map(m => m[1]);
}
function pressedBackends(html) {
  return (html.match(/<button\b[^>]*onclick="setBackendFilter\('([^']*)'\)"[^>]*aria-pressed="true"/g) || [])
    .map(m => m.match(/setBackendFilter\('([^']*)'\)/)[1]);
}
function allPressed(html, group) {
  // Single-quote char built indirectly so no JS string/regex escaping fights us.
  const q = String.fromCharCode(39);
  const pat = '<button\\b[^>]*onclick=\"sessionsFilterClearGroup\\(' + q + group + q + '\\)\"[^>]*aria-pressed=\"(true|false)\"';
  const m = new RegExp(pat).exec(html);
  return !!(m && m[1] === 'true');
}
function trueCount(html) { return (html.match(/aria-pressed="true"/g) || []).length; }
function assertButtons(h) {
  assert.equal((h.match(/<button\b[^>]*aria-pressed=/g) || []).length, (h.match(/aria-pressed=/g) || []).length, 'every pressed chip is a <button>');
}

// ── (a) Status chips ────────────────────────────────────────────────────────

test('a single status chip shows only that state', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('failed');
  assert.deepEqual(stateChips(sb), ['failed']);
  assert.equal(cards(render(sb)), 2, 'the two failed sessions only');
});

test('two status chips show the union, and toggling the first off removes exactly it', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('running');
  sb.setSessionStateChip('failed');
  assert.deepEqual(stateChips(sb).sort(), ['failed', 'running']);
  assert.equal(cards(render(sb)), 4, 'running + failed = 4 sessions');

  sb.setSessionStateChip('running'); // second click on the first pick turns it off
  assert.deepEqual(stateChips(sb), ['failed'], "toggling 'running' off leaves only 'failed'");
  assert.equal(cards(render(sb)), 2);
});

test('the All status chip clears the whole group', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('running');
  sb.setSessionStateChip('failed');
  assert.equal(cards(render(sb)), 4);

  sb.sessionsFilterClearGroup('status');
  assert.deepEqual(stateChips(sb), []);
  assert.equal(vm.runInContext('state.sessionStateChip', sb), 'all');
  assert.equal(cards(render(sb)), 8, 'back to the unfiltered list');
});

// ── (b) Backend chips ───────────────────────────────────────────────────────

test('a single backend chip shows only that backend family', () => {
  const sb = loadApp();
  seed(sb);
  sb.setBackendFilter('pi');
  assert.deepEqual(backendSel(sb), ['pi']);
  assert.equal(vm.runInContext('state.sessionFilter', sb), '', 'backend picks never touch the search text');
  assert.equal(cards(render(sb)), 4, 'the four pi sessions only');
});

test('two backend chips show the union; toggling one off leaves the other', () => {
  const sb = loadApp();
  seed(sb);
  sb.setBackendFilter('claude-code');
  sb.setBackendFilter('pi');
  assert.deepEqual(backendSel(sb).sort(), ['claude-code', 'pi']);
  assert.equal(cards(render(sb)), 8, 'claude-code + pi = all 8 sessions');

  sb.setBackendFilter('claude-code'); // second click removes it
  assert.deepEqual(backendSel(sb), ['pi'], "toggling 'claude-code' off leaves only 'pi'");
  assert.equal(cards(render(sb)), 4);
});

test('the All backend chip clears the group without touching the search field', () => {
  const sb = loadApp();
  seed(sb);
  sb.setBackendFilter('claude-code');
  sb.setBackendFilter('pi');
  assert.equal(cards(render(sb)), 8);

  sb.sessionsFilterClearGroup('backend');
  assert.deepEqual(backendSel(sb), []);
  assert.equal(vm.runInContext('state.sessionFilter', sb), '', 'still no text in the search field');
  assert.equal(cards(render(sb)), 8, 'same pool — nothing was being filtered to begin with');

  // A real narrowing pick proves the clear actually resets the filter, not
  // just the array: pi alone (4) back to all 8.
  sb.setBackendFilter('pi');
  assert.equal(cards(render(sb)), 4);
  sb.sessionsFilterClearGroup('backend');
  assert.deepEqual(backendSel(sb), []);
  assert.equal(cards(render(sb)), 8);

  // The rendered search input carries an empty value, not a leftover pick.
  assert.match(render(sb), /id="sessionFilterInput"[\s\S]{0,200}?value=""/, 'search input renders empty');
});

// ── (c) Compact summary row ─────────────────────────────────────────────────

test('the summary row has one removable chip per selected status, backend and tag', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('failed');
  sb.setBackendFilter('pi');
  sb.sessionTagChipToggle('lane:alpha');
  const html = render(sb);
  assert.equal(cards(html), 0, 'valid pick set: nothing is failed AND pi AND lane:alpha');

  assert.ok(html.includes('class="sessions-tag-selected"'), 'summary row rendered');
  assert.match(html, /data-filter-chip="status:failed"/, 'status chip in the row');
  assert.match(html, /data-filter-chip="backend:pi"/, 'backend chip in the row');
  assert.match(html, /data-tag="lane:alpha"/, 'tag chip in the row');
  assert.match(html, /onclick="setSessionStateChip\('failed'\)"/, 'status chip carries its remove button');
  assert.match(html, /onclick="setBackendFilter\('pi'\)"/, 'backend chip carries its remove button');
  assert.match(html, /onclick="sessionTagChipToggle\('lane:alpha'\)"/, 'tag chip carries its remove button');
  assert.match(html, /onclick="sessionsSummaryClearAll\(\)"/, 'Clear all button present');
});

test("the summary row's remove buttons clear exactly their own chip", () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('failed');
  sb.setBackendFilter('pi');
  sb.sessionTagChipToggle('lane:alpha');
  render(sb);

  // Press the status chip's X in the row: 'failed' comes off, the other two stay.
  sb.setSessionStateChip('failed');
  assert.deepEqual(stateChips(sb), [], 'status pick removed');
  assert.deepEqual(backendSel(sb), ['pi'], 'backend pick still there');
  assert.deepEqual(tagSel(sb), ['lane:alpha'], 'tag pick still there');
  const html = render(sb);
  assert.ok(!/data-filter-chip="status:failed"/.test(html), 'status chip gone from the row');
  assert.match(html, /data-filter-chip="backend:pi"/);
  assert.match(html, /data-tag="lane:alpha"/);
  assert.equal(cards(html), 1, 'only running+pi: the lane:alpha session that is also a pi backend session');

  // Then press the backend chip's X: 'pi' comes off too.
  sb.setBackendFilter('pi');
  assert.deepEqual(backendSel(sb), []);
  const html2 = render(sb);
  assert.ok(!/data-filter-chip="backend:pi"/.test(html2));
  assert.match(html2, /data-tag="lane:alpha"/, 'the tag chip is the last one standing');
  assert.equal(cards(html2), 2, 'both lane:alpha sessions are back (failed+claude-code, running+pi)');
});

test("'Clear all' on the summary row empties status, backend and tag picks at once", () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('failed');
  sb.setBackendFilter('pi');
  sb.sessionTagChipToggle('lane:alpha');
  sb.sessionsSummaryClearAll();
  assert.deepEqual(stateChips(sb), []);
  assert.deepEqual(backendSel(sb), []);
  assert.deepEqual(tagSel(sb), []);
  const html = render(sb);
  assert.equal(cards(html), 8, 'unfiltered list again');
  assert.ok(!/data-filter-chip="(status|backend):/.test(html), 'no status/backend chips left');
  assert.ok(!/data-tag="lane:alpha"/.test(html), 'no tag chip left');
});

// ── (d) aria-pressed + <button> chips ───────────────────────────────────────

test('status chips: aria-pressed=true sits exactly on the selected chips, and on All when the group is empty', () => {
  const sb = loadApp();
  seed(sb);

  // Empty group: exactly one pressed chip, and it is the All button.
  let html = sb._sessionsSheetStateChips().html;
  assertButtons(html);
  assert.match(html, /<button\b[^>]*type="button"[^>]*aria-pressed="(true|false)"/, 'chips are <button type="button"> elements');
  assert.equal(allPressed(html, 'status'), true, 'All is pressed while the group is empty');
  assert.deepEqual(pressedStates(html), [], 'no state chip is pressed');
  assert.equal(trueCount(html), 1, 'exactly one pressed chip in an empty group');

  // Pick two states: exactly those two are pressed, All is released.
  sb.setSessionStateChip('running');
  sb.setSessionStateChip('failed');
  html = sb._sessionsSheetStateChips().html;
  assertButtons(html);
  assert.equal(allPressed(html, 'status'), false, 'All unpressed once there are picks');
  assert.deepEqual(pressedStates(html).sort(), ['failed', 'running'], 'pressed=true exactly on the two selected chips');
  assert.equal(trueCount(html), 2, 'exactly two pressed chips in total');

  // Toggle one off: the pressed set shrinks to exactly the survivor.
  sb.setSessionStateChip('running');
  html = sb._sessionsSheetStateChips().html;
  assert.deepEqual(pressedStates(html), ['failed'], 'only the surviving chip is pressed');
  assert.equal(trueCount(html), 1);
});

test('backend chips: aria-pressed=true sits exactly on the selected chips, and on All when the group is empty', () => {
  const sb = loadApp();
  seed(sb);

  let html = sb._sessionsSheetLlmChips().html;
  assertButtons(html);
  assert.match(html, /<button\b[^>]*type="button"[^>]*aria-pressed="(true|false)"/, 'chips are <button type="button"> elements');
  assert.equal(allPressed(html, 'backend'), true, 'All is pressed while the group is empty');
  assert.deepEqual(pressedBackends(html), [], 'no backend chip is pressed');
  assert.equal(trueCount(html), 1, 'exactly one pressed chip in an empty group');

  sb.setBackendFilter('claude-code');
  sb.setBackendFilter('pi');
  html = sb._sessionsSheetLlmChips().html;
  assertButtons(html);
  assert.equal(allPressed(html, 'backend'), false, 'All unpressed once there are picks');
  assert.deepEqual(pressedBackends(html).sort(), ['claude-code', 'pi'], 'pressed=true exactly on the two backend chips');
  assert.equal(trueCount(html), 2, 'exactly two pressed chips in total');

  sb.setBackendFilter('claude-code');
  html = sb._sessionsSheetLlmChips().html;
  assert.deepEqual(pressedBackends(html), ['pi'], 'only the surviving backend chip is pressed');
  assert.equal(trueCount(html), 1);
});

// ── (e) Filter badge count ──────────────────────────────────────────────────

test('_sessionsActiveFilterCount counts each non-empty group once', () => {
  const sb = loadApp();
  seed(sb);
  assert.equal(sb._sessionsActiveFilterCount(), 0, 'no groups selected');

  sb.setSessionStateChip('running');
  assert.equal(sb._sessionsActiveFilterCount(), 1, 'status group on');

  sb.setSessionStateChip('failed');
  assert.equal(sb._sessionsActiveFilterCount(), 1, 'two status picks are still one group');

  sb.setBackendFilter('claude-code');
  sb.setBackendFilter('pi');
  assert.equal(sb._sessionsActiveFilterCount(), 2, 'two backend picks are still one group');

  sb.sessionTagChipToggle('lane:alpha');
  assert.equal(sb._sessionsActiveFilterCount(), 3, 'tag group on');

  vm.runInContext('state.sessionWatchFilter = true;', sb);
  assert.equal(sb._sessionsActiveFilterCount(), 4, 'watch filter adds one group');

  sb.sessionsSummaryClearAll();
  assert.equal(sb._sessionsActiveFilterCount(), 1, 'only the watch group survived Clear all');
});

// ── (f) Legacy single-select path ────────────────────────────────────────────

test('the legacy setSessionStateChip("running") caller still selects exactly one state', () => {
  const sb = loadApp();
  seed(sb);
  sb.setSessionStateChip('running');
  assert.deepEqual(stateChips(sb), ['running']);
  assert.equal(vm.runInContext('state.sessionStateChip', sb), 'running', "single pick keeps the legacy reader at 'running'");
  assert.equal(cards(render(sb)), 2, 'the two running sessions only');
  assert.equal(vm.runInContext('state.sessionBackendSel.length', sb), 0, 'the legacy caller never touches the backend set');
});

test('a stored legacy cs_session_state_chip value still filters the list', () => {
  const storage = memStorage();
  storage.setItem('cs_session_state_chip', 'complete');
  const sb = loadApp({ storage });
  seed(sb, 'delete state.sessionStateChip;'); // fresh page: only the stored legacy pick exists
  const html = render(sb);
  assert.deepEqual(stateChips(sb), ['complete'], 'legacy key migrated into the chip set');
  assert.equal(cards(html), 2, 'only the two complete sessions');

  storage.setItem('cs_session_state_chip', 'running');
  const sb2 = loadApp({ storage });
  seed(sb2, 'delete state.sessionStateChip;');
  const html2 = render(sb2);
  assert.deepEqual(stateChips(sb2), ['running'], "legacy 'running' stored value honored");
  assert.equal(cards(html2), 2, 'only the two running sessions');
});
