// The converted multi-select state filters — the shared chip set
// (createStateChipSet in app.js) used by the three converted state/status
// filters: the Records card on the project page (planRecordsState, server-
// fetched), the Plans browser (plansBrowserState, client-filtered) and the
// Records sub-tab (recordsTabState, server-fetched).
//
// Covered here:
//   - selecting none, one, or several states shows the right items on each
//     converted filter (any-of semantics),
//   - the chip row lists the right state chips per page (registry states vs
//     finding states vs the union),
//   - per-page storage keys do not leak between pages (and a corrupt stored
//     selection starts from all states),
//   - the clear control (All chip / summary clear / second toggle) resets the
//     group and drops the state query,
//   - the state set goes to the server as one comma-separated query,
//   - the chip locale keys exist in all five locale files with their
//     placeholders,
//   - the phone-width CSS guarantee: the chip row wraps instead of
//     overflowing.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-state-filter-chips.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const STYLE_CSS = path.join(__dirname, 'style.css');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

const REG_STATES = ['open', 'planned', 'in_progress', 'blocked', 'shipped', 'done', 'deferred', 'cancelled'];
const FIND_STATES = ['open', 'triaged', 'fixed', 'wontfix', 'duplicate'];
const UNION_STATES = REG_STATES.concat(FIND_STATES.filter(s => REG_STATES.indexOf(s) < 0));

// Registry rows and findings of the test project, in three different states.
const REGISTRY = [
  { title: 'Open row', id: 'r-open', uid: 'u1', kind: 'registry_row', project: 'acme', state: 'open', body: 'Still open.', legacy_number: 400, links: [], aliases: [], needs_review: false, created_at: '2026-11-01T10:00:00Z', updated_at: '2026-11-01T10:00:00Z' },
  { title: 'Blocked row', id: 'r-blocked', uid: 'u2', kind: 'registry_row', project: 'acme', state: 'blocked', body: 'Stuck on a dependency.', legacy_number: 401, links: [], aliases: [], needs_review: false, created_at: '2026-11-01T10:00:00Z', updated_at: '2026-11-02T10:00:00Z' },
  { title: 'Shipped row', id: 'r-shipped', uid: 'u3', kind: 'registry_row', project: 'acme', state: 'shipped', body: 'Shipped by auto-7.', legacy_number: 402, links: [], aliases: [], needs_review: false, created_at: '2026-11-01T10:00:00Z', updated_at: '2026-11-03T10:00:00Z' },
];
const FINDINGS = [
  { title: 'Open finding', id: 'f-open', uid: 'u4', kind: 'finding', project: 'acme', state: 'open', body: 'Raised by auto-3.', links: [], aliases: [], needs_review: false, created_at: '2026-11-02T10:00:00Z', updated_at: '2026-11-02T10:00:00Z' },
  { title: 'Triaged finding', id: 'f-triaged', uid: 'u5', kind: 'finding', project: 'acme', state: 'triaged', body: 'Triaged last week.', links: [], aliases: [], needs_review: true, created_at: '2026-11-02T10:00:00Z', updated_at: '2026-11-04T10:00:00Z' },
];
const BROWSE = REGISTRY.concat(FINDINGS);

function bundle(lang) { return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8')); }

function g(u, k) { return decodeURIComponent((u.match(new RegExp('[?&]' + k + '=([^&]*)')) || [])[1] || ''); }

// load builds the app with the Projects tab active and the project known; the
// mock server honours the comma-separated state query exactly like the real
// one (a row matches when its state is in the set; empty set === all).
function load() {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const body = { innerHTML: '' };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view, planRecordsBody: body, recordsTabList: { innerHTML: '' } } });
  sandbox.__calls = [];
  sandbox.fetch = (url, o) => {
    const u = String(url);
    const ok = b => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) });
    const states = g(u, 'state').split(',').map(s => s.trim()).filter(Boolean);
    const match = r => (!states.length || states.indexOf(r.state) >= 0);
    if (u.startsWith('/api/plan-records/registry-rows')) {
      sandbox.__calls.push(u);
      const rows = REGISTRY.filter(r => r.kind === 'registry_row' && match(r));
      return ok({ records: rows, total: rows.length, limit: 50, offset: 0 });
    }
    if (u.startsWith('/api/plan-records/findings')) {
      sandbox.__calls.push(u);
      const rows = FINDINGS.filter(match);
      return ok({ records: rows, total: rows.length, limit: 50, offset: 0 });
    }
    if (u.startsWith('/api/plan-records/browse')) {
      sandbox.__calls.push(u);
      const rows = BROWSE.filter(match);
      return ok({ records: rows, page: { total: rows.length } });
    }
    if (u.includes('/api/projects') && !u.includes('/policy/') && !u.includes('/rollup') && !u.includes('/history')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    if (u.startsWith('/api/projects/acme/plans')) {
      sandbox.__calls.push(u);
      return ok({ plans: [], page: { total: 0 }, backlog: { plans: [], page: { total: 0 } } });
    }
    if (u.includes('/history')) return ok({ items: [], gantt: [] });
    return ok({});
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return sandbox;
}

const pr = (sb, e) => vm.runInContext(e, sb);
const calls = (sb, prefix) => sb.__calls.filter(u => u.indexOf(prefix) >= 0);

// pressed finds the chip carrying onclick=...name,key) and reports whether it
// is pressed; null when the chip is not on the row at all.
function pressed(html, name, key) {
  const idx = html.indexOf(`stateChipsToggle('${name}','${key}')`);
  if (idx < 0) return null;
  const start = html.lastIndexOf('<button', idx);
  const end = html.indexOf('>', idx);
  return /aria-pressed="true"/.test(html.slice(start, end));
}
// allPressed reports the pressed state of the All chip (onclick stateChipsClear).
function allPressed(html, name) {
  const idx = html.indexOf(`stateChipsClear('${name}')`);
  if (idx < 0) return null;
  const start = html.lastIndexOf('<button', idx);
  const end = html.indexOf('>', idx);
  return /aria-pressed="true"/.test(html.slice(start, end));
}

async function open(sb) {
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  await sb._projectsLoadSelected();
  await flushAsync();
}

// ── the chip row lists the right states per converted filter ────────────────

test('the plan records card offers one chip per state of the open kind', async () => {
  const sb = load();
  await open(sb);
  let html = sb.planRecordsStateChipsHtml('registry_row', pr(sb, '_planRecords'));
  for (const s of REG_STATES) assert.equal(pressed(html, 'planRecordsState', s), false, s + ' chip present, not pressed');
  assert.equal(allPressed(html, 'planRecordsState'), true, 'nothing selected: the All chip is pressed');
  for (const s of ['triaged', 'fixed', 'wontfix', 'duplicate']) assert.equal(pressed(html, 'planRecordsState', s), null, s + ' is not a registry state');
  await sb.planRecordsSetKind('finding');
  html = sb.planRecordsStateChipsHtml('finding', pr(sb, '_planRecords'));
  for (const s of FIND_STATES) assert.equal(pressed(html, 'planRecordsState', s), false, s + ' chip present for the finding kind');
  for (const s of ['planned', 'in_progress', 'shipped', 'done', 'deferred', 'cancelled']) assert.equal(pressed(html, 'planRecordsState', s), null, s + ' is not a finding state');
});

test('the plans browser offers the registry states and the records tab the union', () => {
  const sb = load();
  const plans = sb.plansBrowserStateChipsHtml(pr(sb, '_plansBrowser'));
  for (const s of REG_STATES) assert.equal(pressed(plans, 'plansBrowserState', s), false, s + ' chip present on the plans browser');
  for (const s of ['triaged', 'fixed', 'wontfix', 'duplicate']) assert.equal(pressed(plans, 'plansBrowserState', s), null, s + ' is not a plan state');
  const tab = sb.recordsTabStateChipsHtml(pr(sb, '_recordsTab'));
  for (const s of UNION_STATES) assert.equal(pressed(tab, 'recordsTabState', s), false, s + ' chip present on the records tab');
  assert.equal(pressed(tab, 'recordsTabState', 'bogus'), null, 'no invented states');
});

// ── none, one, several states show the right items ───────────────────────────

test('plan records: none shows all, one shows that state, several show any of them', async () => {
  const sb = load();
  await open(sb);
  const shown = () => pr(sb, 'JSON.stringify(_planRecords.recs.map(r => r.id))');
  assert.equal(shown(), '["r-open","r-blocked","r-shipped"]', 'no selection: every state listed');
  assert.ok(!calls(sb, '/api/plan-records/registry-rows').pop().includes('state='), 'no selection sends no state query');
  await sb.planRecordsSetState('blocked');
  assert.equal(pr(sb, 'JSON.stringify(_planRecords.recs.map(r => r.id))'), '["r-blocked"]', 'one state: only that state listed');
  assert.equal(pressed(sb.planRecordsStateChipsHtml('registry_row', pr(sb, '_planRecords')), 'planRecordsState', 'blocked'), true, 'the picked chip is pressed');
  assert.equal(allPressed(sb.planRecordsStateChipsHtml('registry_row', pr(sb, '_planRecords')), 'planRecordsState'), false, 'the All chip is no longer pressed');
  await sb.planRecordsSetState('open, shipped');
  assert.equal(pr(sb, 'JSON.stringify(_planRecords.recs.map(r => r.id).sort())'), '["r-open","r-shipped"]', 'several states: any of them matches');
});

test('the plans browser filters its list client-side with the same any-of rule', async () => {
  const sb = load();
  await open(sb);
  vm.runInContext(`_plansBrowser.plans = [
    { id: 'p-open', title: 'Open plan', state: 'open' },
    { id: 'p-blocked', title: 'Blocked plan', state: 'blocked' },
    { id: 'p-planned', title: 'Planned plan', state: 'planned' }
  ]`, sb);
  const shown = () => pr(sb, 'JSON.stringify(_plansBrowser.plans.filter(p => _plansMatch(_plansBrowser, p)).map(p => p.id))');
  assert.equal(shown(), '["p-open","p-blocked","p-planned"]', 'no selection: every state listed');
  await sb.plansBrowserSetState('blocked');
  assert.equal(shown(), '["p-blocked"]', 'one state: only that state listed');
  await sb.plansBrowserSetState('open, planned');
  assert.equal(shown(), '["p-open","p-planned"]', 'several states: any of them matches');
});

test('the records tab shows the rows whose state is in the set', async () => {
  const sb = load();
  await open(sb);
  const shown = () => pr(sb, 'JSON.stringify(_recordsTab.rows.map(r => r.id).sort())');
  assert.equal(shown(), '["f-open","f-triaged","r-blocked","r-open","r-shipped"]', 'no selection: every row listed');
  await sb.recordsTabSetState('triaged');
  assert.equal(shown(), '["f-triaged"]', 'one state: only that state listed');
  await sb.recordsTabSetState('open, shipped');
  assert.equal(shown(), '["f-open","r-open","r-shipped"]', 'several states: any of them matches, across kinds');
});

// ── per-page storage keys ────────────────────────────────────────────────────

test('each page keeps its selection under its own key, never reading the others', () => {
  const sb = load();
  vm.runInContext(`
    localStorage.setItem('cs_state_chips_plansBrowserState', '["open"]');
    localStorage.setItem('cs_state_chips_recordsTabState', '["triaged"]');
  `, sb);
  sb.planRecordsStateChipsHtml('registry_row', null);
  sb.plansBrowserStateChipsHtml(null);
  sb.recordsTabStateChipsHtml(null);
  const sel = name => pr(sb, `JSON.stringify(window._stateChipSets.${name}.get())`);
  const key = name => pr(sb, `window._stateChipSets.${name}.storageKey`);
  assert.equal(key('planRecordsState'), 'cs_state_chips_planRecordsState', 'each page stores under its own key');
  assert.equal(key('plansBrowserState'), 'cs_state_chips_plansBrowserState');
  assert.equal(key('recordsTabState'), 'cs_state_chips_recordsTabState');
  assert.equal(sel('planRecordsState'), '[]', 'the plans/records-tab selection does not leak into the records card');
  assert.equal(sel('plansBrowserState'), '["open"]', 'the plans browser keeps its own selection');
  assert.equal(sel('recordsTabState'), '["triaged"]', 'the records tab keeps its own selection');
  sb.stateChipsToggle('planRecordsState', 'blocked');
  assert.equal(sb.localStorage.getItem('cs_state_chips_plansBrowserState'), '["open"]', 'picking on the records card leaves the plans browser alone');
  assert.equal(sb.localStorage.getItem('cs_state_chips_recordsTabState'), '["triaged"]', 'picking on the records card leaves the records tab alone');
  assert.equal(sb.localStorage.getItem('cs_session_state_chips'), null, 'the Sessions status filter has its own key and is untouched');
  assert.equal(sel('plansBrowserState'), '["open"]', 'rebuilt from its own key, still its own selection');
  assert.equal(sel('planRecordsState'), '["blocked"]');
});

test('a corrupt or stale stored selection starts from all states', () => {
  const sb = load();
  vm.runInContext(`
    localStorage.setItem('cs_state_chips_planRecordsState', 'not json at all');
    localStorage.setItem('cs_state_chips_plansBrowserState', '["open", "state-that-does-not-exist"]');
  `, sb);
  sb.planRecordsStateChipsHtml('registry_row', null);
  sb.plansBrowserStateChipsHtml(null);
  assert.equal(pr(sb, 'JSON.stringify(window._stateChipSets.planRecordsState.get())'), '[]', 'unreadable storage: no selection, i.e. all states');
  assert.equal(pr(sb, 'JSON.stringify(window._stateChipSets.plansBrowserState.get())'), '["open"]', 'unknown states are dropped, known ones kept');
});

// ── the clear control ────────────────────────────────────────────────────────

test('clearing the group resets every chip, drops the stored set and the state query', async () => {
  const sb = load();
  await open(sb);
  await sb.planRecordsSetState('open, blocked');
  const summary = pr(sb, 'window._stateChipSets.planRecordsState.summaryHTML()').replace(/<[^>]+>/g, ' ');
  assert.ok(summary.includes('Clear all'), 'the summary row offers the clear control');
  assert.ok(summary.includes('Open') && summary.includes('Blocked'), 'the picked chips are summarised by name');
  assert.equal(sb.stateChipsClear('planRecordsState'), undefined, 'the control dispatches');
  assert.deepEqual(pr(sb, 'JSON.stringify(window._stateChipSets.planRecordsState.get())'), '[]', 'the whole group is back to none');
  assert.equal(sb.localStorage.getItem('cs_state_chips_planRecordsState'), null, 'the stored set is dropped, so a reload reads all states');
  assert.equal(allPressed(sb.planRecordsStateChipsHtml('registry_row', pr(sb, '_planRecords')), 'planRecordsState'), true, 'the All chip is pressed again');
  await sb._planRecordsLoad(false);
  const last = calls(sb, '/api/plan-records/registry-rows').pop();
  assert.ok(!last.includes('state='), 'the cleared filter sends no state query');
  assert.equal(pr(sb, '_planRecords.recs.length'), 3, 'every state is listed again');
});

test('toggling a picked chip again removes it from the group', async () => {
  const sb = load();
  await open(sb);
  sb.stateChipsToggle('planRecordsState', 'shipped');
  assert.deepEqual(pr(sb, 'JSON.stringify(window._stateChipSets.planRecordsState.get())'), '["shipped"]');
  sb.stateChipsToggle('planRecordsState', 'shipped');
  assert.deepEqual(pr(sb, 'JSON.stringify(window._stateChipSets.planRecordsState.get())'), '[]', 'second click clears the one selection');
});

// ── the comma-separated fetch query ──────────────────────────────────────────

test('the state set goes to the server as one comma-separated query on both fetched filters', async () => {
  const sb = load();
  await open(sb);
  await sb.planRecordsSetState('blocked, shipped');
  assert.equal(pr(sb, 'JSON.stringify(_planRecords.states)'), '["blocked","shipped"]', 'the mirror is a set, not a string');
  const rows = calls(sb, '/api/plan-records/registry-rows').pop();
  assert.ok(rows.includes('state=blocked%2Cshipped'), 'plan records: one comma-separated state query');
  await sb.recordsTabSetState('open, triaged');
  const browse = calls(sb, '/api/plan-records/browse').pop();
  assert.ok(browse.includes('state=open%2Ctriaged'), 'records tab: one comma-separated state query');
});

// ── locale keys ──────────────────────────────────────────────────────────────

test('the chip set locale keys exist in all five locale files, with placeholders', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = ['state_chips_all', 'state_chips_selected', 'state_chips_clear_all', 'state_chips_remove']
    .concat(UNION_STATES.map(s => 'plan_records_state_' + s));
  for (const k of ['state_chips_all', 'state_chips_selected', 'state_chips_clear_all', 'state_chips_remove']) {
    assert.ok(src.includes("'" + k + "'"), k + ' is used by app.js');
  }
  for (const l of LANGS) {
    const b = bundle(l);
    for (const k of keys) assert.ok(typeof b[k] === 'string' && b[k].length > 0, `${l} has ${k}`);
    assert.ok(b.state_chips_remove.includes('%1$s'), l + ' keeps the remove placeholder');
  }
});

// ── the phone-width guarantee ────────────────────────────────────────────────

test('the chip row wraps at phone width instead of overflowing (style.css)', () => {
  const css = fs.readFileSync(STYLE_CSS, 'utf8');
  const setRule = css.match(/\.state-chip-set\s*\{([^}]*)\}/);
  assert.ok(setRule, 'the shared chip row has a style rule');
  assert.ok(/display:\s*flex/.test(setRule[1]), 'the row is a flex row');
  assert.ok(/flex-wrap:\s*wrap/.test(setRule[1]), 'the row wraps onto more lines instead of overflowing');
  const media = [...css.matchAll(/@media \(max-width:\s*599px\)\s*\{([\s\S]*?)\n\}/g)].map(m => m[1]).find(b => b.includes('.state-chip-set'));
  assert.ok(media, 'the phone breakpoint (max-width 599px) sizes the chip row');
  assert.ok(media.includes('.state-chip-set .backend-filter-badge') && media.includes('max-width: calc(100% - 12px)') && media.includes('overflow: hidden'), 'an overlong chip label is bounded to the container');
});
