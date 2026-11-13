// Shipped surface (autonomous.shipped): what happened to the work after it
// finished, orthogonal to the stored status of the PRD or Automation.
//
//   - chip: shippedCarriesState (shipped flag OR merged commit) decides;
//     the chip takes precedence over the status pill, so a
//     cancelled-but-merged record shows "Completed", not "Cancelled"
//   - filter: shippedFilterSelection / shippedRecordMatches — the
//     selection is carried as shipped=true|false (never as a status token)
//   - counts: shippedCountStates — count_separately pulls shipped records
//     out of their stored status and groups them under "completed"
//   - settings: loadShippedCounts seeds enabled/show_chip/filter_default/
//     count_separately and the shipped record counts
//   - locales: every shipped_* key app.js uses sits in en.json (parity
//     with the other locales is covered by the Go locale test)
//
//   node --test app-shipped.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadApp({ apiFetchImpl = null } = {}) {
  const sandbox = buildSandbox({ byId: {}, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.window._i18n.fallback = en;
  sandbox.window._i18n.bundle = en;
  if (apiFetchImpl) sandbox.apiFetch = apiFetchImpl;
  return sandbox;
}

// ---------- chip choice ------------------------------------------------

test('shipped chip — the carrier predicates', () => {
  const s = loadApp();
  // merged commit is the ground truth; a blank one is not a merge
  assert.equal(s.shippedCarriesState({ status: 'completed', merge_commit: 'abc123' }), true);
  assert.equal(s.shippedCarriesState({ status: 'cancelled', merge_commit: '  ' }), false);
  // an explicit shipped flag also counts (the Go side marks it)
  assert.equal(s.shippedCarriesState({ status: 'cancelled', shipped: true }), true);
  // a plain unmerged record — whatever its status — is not shipped
  assert.equal(s.shippedCarriesState({ status: 'completed' }), false);
  assert.equal(s.shippedCarriesState({}), false);
  assert.equal(s.shippedCarriesState(null), false);
});

test('shipped chip — label is the existing Completed status label', () => {
  const s = loadApp();
  assert.equal(s.shippedChipLabel({ merged_in_release: 'v8.9-21' }), 'Completed');
  assert.equal(s.shippedChipLabel({}), 'Completed');
  assert.equal(s.shippedChipLabel({}, 'automation_status_completed'), 'Completed');
});

test('shipped chip — cancelled-but-merged says Completed, genuinely cancelled renders nothing', () => {
  const s = loadApp();
  const cancelledMerged = { status: 'cancelled', merge_commit: 'abc123', merged_in_release: 'v8.9-21' };
  const chip = s.shippedChipHtml(cancelledMerged);
  assert.ok(chip, 'cancelled-but-merged must carry the chip');
  assert.ok(chip.includes('state-badge-shipped'), 'chip uses its own badge class');
  assert.ok(/>Completed/.test(chip), 'chip text is Completed');
  assert.ok(!/>Shipped/.test(chip), 'chip text is not Shipped');
  assert.equal(s.shippedChipHtml({ status: 'cancelled' }), '', 'genuinely cancelled has no chip');
});

test('shipped chip — hover detail carries release and commit only with a merge commit', () => {
  const s = loadApp();
  const withCommit = { status: 'cancelled', merge_commit: 'abc123', merged_in_release: 'v8.9-21', merged_at: '2026-01-02T10:00:00Z' };
  const tip = s.shippedChipTip(withCommit);
  assert.ok(tip.includes('Shipped in v8.9-21'), 'release in hover detail');
  assert.ok(tip.includes('abc123'), 'merge commit in hover detail');
  assert.ok(tip.includes('2026-01-02T10:00:00Z'), 'merged_at in hover detail');
  assert.ok(s.shippedChipHtml(withCommit).includes('title="Completed'), 'tooltip starts with Completed');
  // flag-only record: no commit, so no release or commit detail
  const flagOnly = { status: 'cancelled', shipped: true, merged_in_release: 'v8.9-21' };
  assert.equal(s.shippedChipTip(flagOnly), 'Completed');
  assert.ok(!s.shippedChipHtml(flagOnly).includes('v8.9-21'));
});

test('shipped chip — show_chip=false hides it even when the work shipped', () => {
  const s = loadApp();
  s._shippedState.show_chip = false;
  assert.equal(s.shippedChipHtml({ status: 'completed', merge_commit: 'abc123' }), '');
});

test('shipped chip — the detail line helper is gone (hover detail only)', () => {
  const s = loadApp();
  assert.equal(typeof s.shippedDetailLineHtml, 'undefined');
});

// ---------- filter selection -------------------------------------------

test('shipped filter — the selection parses shipped/unshipped tokens only', () => {
  const s = loadApp();
  assert.equal(s.shippedFilterSelection(['running', 'shipped']), 'shipped');
  assert.equal(s.shippedFilterSelection(['cancelled', 'unshipped']), 'unshipped');
  assert.equal(s.shippedFilterSelection(['running', 'blocked']), 'all');
  assert.equal(s.shippedFilterSelection(null), 'all');
});

test('shipped filter — matches ignore the stored status', () => {
  const s = loadApp();
  const merged = { status: 'cancelled', merge_commit: 'abc123' };
  const plain = { status: 'cancelled' };
  assert.equal(s.shippedRecordMatches(merged, 'shipped', true), true);
  assert.equal(s.shippedRecordMatches(plain, 'shipped', true), false);
  assert.equal(s.shippedRecordMatches(plain, 'unshipped', true), true);
  assert.equal(s.shippedRecordMatches(merged, 'unshipped', true), false);
  // 'all' never filters
  assert.equal(s.shippedRecordMatches(merged, 'all', true), true);
  assert.equal(s.shippedRecordMatches(plain, 'all', true), true);
});

test('shipped filter — disabled surface lets every record through', () => {
  const s = loadApp();
  const plain = { status: 'cancelled' };
  assert.equal(s.shippedRecordMatches(plain, 'shipped', false), true);
  assert.equal(s.shippedRecordMatches(plain, 'unshipped', false), true);
  assert.equal(s.shippedRecordMatches(plain, 'all', false), true);
});

// ---------- counts -------------------------------------------------------

test('shipped counts — count_separately folds shipped into completed', () => {
  const s = loadApp();
  const recs = [
    { status: 'cancelled', merge_commit: 'a1' }, // shipped
    { status: 'cancelled' },                     // genuinely cancelled
    { status: 'completed', merge_commit: 'b2' }, // shipped (a second bucket)
    { status: 'completed' },                     // genuinely completed
    { status: 'running' },
  ];
  const j = o => JSON.parse(JSON.stringify(o)); // vm realm: compare by value
  assert.deepEqual(j(s.shippedCountStates(recs, true, true)) , {
    completed: 3,
    cancelled: 1,
    running: 1,
  });
});

test('shipped counts — count_separately off, shipped records keep their status', () => {
  const s = loadApp();
  const recs = [
    { status: 'cancelled', merge_commit: 'a1' },
    { status: 'cancelled' },
  ];
  assert.deepEqual(JSON.parse(JSON.stringify(s.shippedCountStates(recs, true, false))), { cancelled: 2 });
});

test('shipped counts — disabled surface returns plain status counts', () => {
  const s = loadApp();
  const out = s.shippedCountStates([{ status: 'cancelled', merge_commit: 'a1' }], false, true);
  assert.deepEqual(JSON.parse(JSON.stringify(out)), { cancelled: 1 });
  assert.equal('shipped' in out, false);
  assert.equal('completed' in out, false);
});

// ---------- settings -----------------------------------------------------

test('shipped counts load — settings + counts from the endpoint', async () => {
  const s = loadApp({
    apiFetchImpl: () => Promise.resolve({
      settings: { enabled: true, show_chip: false, filter_default: 'shipped', count_separately: false },
      prds: { shipped: 2, cancelled: 1 },
      automations: { completed: 2 },
    }),
  });
  await s.loadShippedCounts();
  assert.equal(s._shippedState.show_chip, false);
  assert.equal(s._shippedState.filter_default, 'shipped');
  assert.equal(s._shippedState.count_separately, false);
  assert.equal(s._shippedState.counts.prds, 3);
  assert.equal(s._shippedState.counts.automations, 2);
  assert.equal(s._shippedState.counts.by_prd.shipped, 2);
  // a second call is a no-op (idempotent)
  s.apiFetch = () => Promise.resolve({ prds: { shipped: 99 } });
  await s.loadShippedCounts();
  assert.equal(s._shippedState.counts.prds, 3);
});

test('shipped counts load — server defaults win when the fields are absent', async () => {
  const s = loadApp({ apiFetchImpl: () => Promise.resolve({}) });
  await s.loadShippedCounts();
  assert.equal(s._shippedState.enabled, true);       // server default: on
  assert.equal(s._shippedState.show_chip, true);     // server default: on
  assert.equal(s._shippedState.filter_default, 'all');
  assert.equal(s._shippedState.count_separately, true);
});

test('shipped kick — an empty filter seeds with filter_default', async () => {
  const s = loadApp();
  s._shippedState.filter_default = 'shipped';
  s._shippedState.loaded = true; // skip the fetch; the kick runs its seed
  const automata = vm.runInContext('_automataState', s);
  const autos = vm.runInContext('_automationState', s);
  automata.statusFilter = new Set();
  autos.statusFilter = new Set();
  await s._shippedKick();
  assert.equal(automata.statusFilter.has('shipped'), true);
  assert.equal(autos.statusFilter.has('shipped'), true);
  // a non-empty selection is left untouched
  automata.statusFilter = new Set(['running']);
  await s._shippedKick();
  assert.deepEqual([...automata.statusFilter], ['running']);
});

// ---------- locales ------------------------------------------------------

test('shipped locales — every shipped_* key app.js uses is in the English bundle', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const k of ['shipped_chip', 'shipped_in_release', 'shipped_merged_at', 'shipped_merge_commit']) {
    assert.ok(typeof en[k] === 'string' && en[k].length > 0, `en.json must define ${k}`);
  }
  const de = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'de.json'), 'utf8'));
  for (const k of ['shipped_chip', 'shipped_in_release', 'shipped_merged_at', 'shipped_merge_commit']) {
    assert.ok(typeof de[k] === 'string' && de[k].length > 0, `de.json must define ${k}`);
  }
});

// ---------- badges and throughput --------------------------------------

test('shipped badges — no separate Shipped status badge, Completed counts shipped work', () => {
  const s = loadApp();
  const recs = [
    { id: 'a', status: 'cancelled', merge_commit: 'a1', tags: [] },
    { id: 'b', status: 'cancelled', tags: [] },
    { id: 'c', status: 'completed', tags: [] },
  ];
  const st = vm.runInContext('_automataState', s);
  st.allPrds = recs;
  st.statusFilter = new Set();
  const html = s._tagFacetsHTML('automata', false);
  assert.ok(!html.includes('status-shipped'), 'no Shipped badge');
  assert.ok(!/>Shipped </.test(html), 'no Shipped label');
  const completed = html.match(/status-completed[^>]*>[^<]*<span[^>]*>(\d+)</);
  const cancelled = html.match(/status-cancelled[^>]*>[^<]*<span[^>]*>(\d+)</);
  assert.ok(completed, 'Completed badge present');
  assert.ok(cancelled, 'Cancelled badge present');
  assert.equal(completed[1], '2', 'Completed = genuine + shipped-carrying');
  assert.equal(cancelled[1], '1', 'Cancelled excludes the shipped-carrying record');
});

test('shipped throughput — the Projects line prints Completed, not Shipped', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(src.includes("${shippedTotal} ${escHtml(t('prd_status_completed'))}"));
  assert.ok(!src.includes("t('shipped_chip')"), 'no status label uses shipped_chip any more');
  assert.ok(!src.includes("badge('shipped'"), 'no Shipped status badge');
});
