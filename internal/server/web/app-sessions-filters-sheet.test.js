// Sessions Filters sheet: badge count, open/close state, Escape listener
// lifetime, the one-line status counts, and the full open/close case set
// (button open, button close, Escape, outside click, inside click) at
// phone (≤640px) and desktop widths.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

test('badge counts group, state chip, watch filter and tags but not search text', () => {
  const sb = loadApp();
  vm.runInContext(`state.sessions = [{ id: 'a', state: 'running', backend_family: 'claude-code' }]; state.sessionFilter = 'zzz';`, sb);
  assert.equal(sb._sessionsActiveFilterCount(), 0);
  vm.runInContext(`state.sessionGroupBy = { axis: 'state' }; state.sessionStateChip = 'running'; state.sessionWatchFilter = true;`, sb);
  assert.equal(sb._sessionsActiveFilterCount(), 3);
});

test('open and close flip the state and the Escape listener only lives while open', () => {
  const sb = loadApp();
  let added = 0, removed = 0;
  sb.document.addEventListener = (n) => { if (n === 'keydown') added++; };
  sb.document.removeEventListener = (n) => { if (n === 'keydown') removed++; };
  vm.runInContext(`state.activeView = 'other';`, sb);
  sb.sessionsFiltersOpen();
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), true);
  assert.equal(added, 1);
  sb.sessionsFiltersClose();
  assert.equal(vm.runInContext('state._sessionsSheetOpen', sb), false);
  assert.equal(removed, 1);
  sb.sessionsFiltersClose();
  assert.equal(removed, 1, 'no listener left to remove');
});

test('the sheet carries exactly one Group by control, offering discovered placement axes', () => {
  const sb = loadApp();
  vm.runInContext(`
    state.activeView = 'sessions';
    state.sessions = [{ id: 'a', state: 'running', system_tags: ['compute:gpu-1'] }];
    state._sessionsSheetOpen = true;
  `, sb);
  const html = sb._sessionsSheetHTML({ groupBy: null, stateBadges: '', llmBadges: '', tagPanel: '', tagHelp: '' });
  assert.match(html, /<select id="sessions-group-select"/);
  assert.equal((html.match(/<select/g) || []).length, 1, 'exactly one <select> in the sheet (the Group by control)');
  assert.ok(!html.includes('group-axis-control'), 'the removed second placement control is gone');
  const sel = html.match(/<select id="sessions-group-select"[\s\S]*?<\/select>/)[0];
  assert.ok(sel.includes('value="tag|compute:"'), 'placement axis the sessions carry is offered in the one picker');
  // The one picker is the shared control: it reads state.sessionGroupBy.
  vm.runInContext(`state.sessionGroupBy = { axis: 'tag', tagPrefix: 'compute:' };`, sb);
  const cur = sb.renderSessionsGroupPicker(vm.runInContext('state.sessionGroupBy', sb));
  assert.ok(cur.includes('value="tag|compute:" selected'), 'current placement grouping selected inside the same dropdown');
});

// ────────────────────────────────────────────────────────────────────────
// Open/close case set. The phone sheet and the desktop panel paint the same
// .sessions-sheet node under the same backdrop and share one set of
// document listeners, so the identical case set is exercised at a phone
// width (≤640px, matchMedia max-width: 640px true) and at a desktop width.

const WIDTHS = [
  { label: 'phone (≤640px)', phone: true },
  { label: 'desktop (>640px)', phone: false },
];

function loadAppAt(phone) {
  const sandbox = buildSandbox({ byId: {} });
  sandbox.matchMedia = (q) => ({ matches: phone && /max-width:\s*640px/.test(q), addEventListener() {}, addListener() {} });
  sandbox.innerWidth = phone ? 375 : 1280;
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

// spyDoc stands in for the real document: it records the listeners app.js
// attaches/removes so the test can dispatch them and assert on removal, and
// answers querySelector('.sessions-sheet') with a node whose contains() says
// whether a given click target is inside the live sheet.
function spyDoc({ insideTarget } = {}) {
  const listeners = { keydown: [], click: [] };
  const sheet = { contains: (t) => !!insideTarget && t === insideTarget };
  return {
    listeners,
    addEventListener(n, fn) { (listeners[n] = listeners[n] || []).push(fn); },
    removeEventListener(n, fn) { listeners[n] = (listeners[n] || []).filter((f) => f !== fn); },
    querySelector(sel) { return sel === '.sessions-sheet' ? sheet : null; },
    getElementById() { return makeStubElement(); },
    querySelectorAll() { return []; },
  };
}

// freshSheet loads app.js at one width, swaps in the spy document, seeds the
// sessions view, and returns the three click targets the sheet cares about:
// the Filters button itself, a node inside the .sessions-sheet dialog, and a
// node outside it.
function freshSheet(phone) {
  const sb = loadAppAt(phone);
  const btnTarget = { closest: (sel) => (sel === '#sessionsFiltersBtn' ? btnTarget : null) };
  const insideTarget = { closest: () => null };
  const outsideTarget = { closest: () => null };
  const doc = spyDoc({ insideTarget });
  sb.document = doc;
  vm.runInContext(`
    state.activeView = 'sessions';
    state.sessions = [];
    state.showHistory = true;
    state.sessionFilter = '';
    state.sessionStateChip = 'all';
    state.sessionWatchFilter = false;
    state.sessionGroupBy = null;
    state._sessionsSheetOpen = false;
  `, sb);
  return { sb, doc, btnTarget, insideTarget, outsideTarget };
}

function dispatch(doc, type, ev) {
  (doc.listeners[type] || []).slice().forEach((fn) => fn(ev));
}

// aria-expanded as painted on the Filters button by the HTML that renders it.
function buttonExpanded(sb) {
  const html = vm.runInContext('_sessionsFiltersButtonHTML()', sb);
  const m = html.match(/aria-expanded="(\w+)"/);
  assert.ok(m, 'the Filters button is painted with aria-expanded');
  return m[1];
}

function assertOpen(ctx) {
  assert.equal(vm.runInContext('state._sessionsSheetOpen', ctx.sb), true);
  assert.equal(buttonExpanded(ctx.sb), 'true');
  assert.equal(vm.runInContext('!!state._sessionsSheetListening', ctx.sb), true);
  assert.equal(ctx.doc.listeners.keydown.length, 1);
  assert.equal(ctx.doc.listeners.click.length, 1);
}

function assertClosed(ctx) {
  assert.equal(vm.runInContext('state._sessionsSheetOpen', ctx.sb), false);
  assert.equal(buttonExpanded(ctx.sb), 'false');
  assert.notEqual(vm.runInContext('!!state._sessionsSheetListening', ctx.sb), true);
  assert.deepEqual(ctx.doc.listeners.keydown, [], 'keydown listener removed');
  assert.deepEqual(ctx.doc.listeners.click, [], 'click listener removed');
}

for (const w of WIDTHS) {
  test(`${w.label}: opens with the Filters button and closes with it again`, () => {
    const ctx = freshSheet(w.phone);
    assertClosed(ctx);
    ctx.sb.sessionsFiltersToggle();
    assertOpen(ctx);
    // The button's own click also bubbles to document: the outside-click
    // handler must ignore it (it is the toggle's job), so the sheet is still
    // open when the toggle runs.
    dispatch(ctx.doc, 'click', { target: ctx.btnTarget });
    assert.equal(vm.runInContext('state._sessionsSheetOpen', ctx.sb), true);
    ctx.sb.sessionsFiltersToggle();
    assertClosed(ctx);
  });

  test(`${w.label}: Escape closes it and its key is accepted either spelling`, () => {
    const ctx = freshSheet(w.phone);
    ctx.sb.sessionsFiltersToggle();
    assertOpen(ctx);
    dispatch(ctx.doc, 'keydown', { key: 'Escape' });
    assertClosed(ctx);
    ctx.sb.sessionsFiltersToggle();
    dispatch(ctx.doc, 'keydown', { key: 'Esc' });
    assertClosed(ctx);
  });

  test(`${w.label}: a click outside the sheet closes it`, () => {
    const ctx = freshSheet(w.phone);
    ctx.sb.sessionsFiltersToggle();
    assertOpen(ctx);
    dispatch(ctx.doc, 'click', { target: ctx.outsideTarget });
    assertClosed(ctx);
  });

  test(`${w.label}: a click inside the sheet does not close it`, () => {
    const ctx = freshSheet(w.phone);
    ctx.sb.sessionsFiltersToggle();
    dispatch(ctx.doc, 'click', { target: ctx.insideTarget });
    assertOpen(ctx);
    // And the Escape listener survived the inside click: it still closes.
    dispatch(ctx.doc, 'keydown', { key: 'Escape' });
    assertClosed(ctx);
  });

  test(`${w.label}: after close the document listeners are gone and close is a no-op`, () => {
    const ctx = freshSheet(w.phone);
    ctx.sb.sessionsFiltersToggle();
    assertOpen(ctx);
    dispatch(ctx.doc, 'keydown', { key: 'Escape' });
    assertClosed(ctx);
    // No listeners left to fire: synthetic events are inert now.
    dispatch(ctx.doc, 'keydown', { key: 'Escape' });
    dispatch(ctx.doc, 'click', { target: ctx.outsideTarget });
    assertClosed(ctx);
    ctx.sb.sessionsFiltersClose();
    ctx.sb.sessionsFiltersClose();
    assertClosed(ctx);
  });
}
