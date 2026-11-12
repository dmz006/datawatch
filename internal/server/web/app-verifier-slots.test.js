// PRD 35070001 — the PWA side of "verifier slots per compute node". The
// node form gains three verifier settings and the collector round-trips
// them (blank saves null = daemon default, a number saves that whole
// number); the node detail, the capacity card and the lane fill card learn
// to show executor/verifier slots, the free GPU memory, the headroom check
// with its reason, and the hardware-derived suggestion.
//
// The formatters are pure — they decode an already-fetched API body and
// return strings or HTML — so they are driven below without a server: the
// English bundle is handed to t() through window._i18n, exactly the way
// loadLocale() does in the browser.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const LOCALES = path.join(__dirname, 'locales');
function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES, lang + '.json'), 'utf8'));
}
// The new keys this feature adds; the parity test below holds every bundle
// to exactly this set (plus whatever it already had).
const NEW_KEYS = [
  'capacity_pool_held_of',
  'compute_field_verifier_context_mb',
  'compute_field_verifier_min_free_mem_mb',
  'compute_field_verifier_slots',
  'compute_free_gpu',
  'compute_headroom',
  'compute_headroom_no',
  'compute_headroom_no_reason',
  'compute_headroom_ok',
  'compute_headroom_ok_reason',
  'compute_slots_effective',
  'compute_slots_executor',
  'compute_slots_section',
  'compute_slots_verifier',
  'compute_slots_zero_mean',
  'compute_suggestion',
  'compute_suggestion_label',
  'compute_suggestion_no_data',
  'compute_suggestion_zero',
  'compute_verifier_ctx_default',
  'compute_verifier_min_free',
  'compute_verifier_op',
  'compute_verifier_slots_default',
  'lane_fill_node_slots_executor',
  'lane_fill_node_slots_title',
  'lane_fill_node_slots_verifier',
];

function loadApp(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // Hand the real English bundle to t(), like loadLocale('en') in the browser.
  const en = bundle('en');
  sandbox.window._i18n.bundle = en;
  sandbox.window._i18n.fallback = en;
  return sandbox;
}

// The collector only reads .value from an input, so a plain object stands in.
function inputWithValue(value) {
  return { value };
}

const FULL_DETAIL_BODY = {
  session_slots: 5,
  executor_slots: 5,
  verifier_slots: 4,
  verifier_slots_effective: 3,
  verifier_context_mb: 0,
  verifier_min_free_mem_mb_effective: 1024,
  free_gpu_mb: 1536,
  headroom_ok: false,
  headroom_reason: 'only 1536 MB free; needs ≥ 1024 MB headroom',
  suggested_verifier_slots: 3,
};

// ── Form fields ────────────────────────────────────────────────────────────

test('the node form renders the three verifier settings, blank when unset', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("buildComputeNodeForm({name: 'gpubox'})", sandbox);
  assert.match(html, /id="fe_verifier_slots"/);
  assert.match(html, /id="fe_verifier_min_free_mem_mb"/);
  assert.match(html, /id="fe_verifier_context_mb"/);
  assert.match(html, /id="fe_verifier_slots"[^>]*value=""/);
  assert.match(html, /id="fe_verifier_min_free_mem_mb"[^>]*value=""/);
  assert.match(html, /id="fe_verifier_context_mb"[^>]*value=""/);
});

test('the node form shows the saved verifier settings on edit', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    "buildComputeNodeForm({name: 'gpubox', verifier_slots: 4, verifier_min_free_mem_mb: 2048, verifier_context_mb: 120})",
    sandbox
  );
  assert.match(html, /id="fe_verifier_slots"[^>]*value="4"/);
  assert.match(html, /id="fe_verifier_min_free_mem_mb"[^>]*value="2048"/);
  assert.match(html, /id="fe_verifier_context_mb"[^>]*value="120"/);
});

test('collecting the form saves the typed verifier settings as whole numbers', () => {
  const sandbox = loadApp({
    fe_verifier_slots: inputWithValue('3'),
    fe_verifier_min_free_mem_mb: inputWithValue('2048'),
    fe_verifier_context_mb: inputWithValue('256'),
  });
  const out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox'}))", sandbox));
  assert.equal(out.verifier_slots, 3);
  assert.equal(out.verifier_min_free_mem_mb, 2048);
  assert.equal(out.verifier_context_mb, 256);
});

test('a blank verifier field saves null (daemon decodes it as unset)', () => {
  const sandbox = loadApp({
    fe_verifier_slots: inputWithValue(''),
    fe_verifier_min_free_mem_mb: inputWithValue(''),
    fe_verifier_context_mb: inputWithValue(''),
  });
  const out = vm.runInContext("collectComputeNodeForm({name: 'gpubox'})", sandbox);
  assert.equal(out.verifier_slots, null);
  assert.equal(out.verifier_min_free_mem_mb, null);
  assert.equal(out.verifier_context_mb, null);
  // null must survive into the wire body so `unset` reaches the daemon.
  const wire = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox'}))", sandbox));
  assert.equal(wire.verifier_slots, null);
});


// ── Pure formatters ────────────────────────────────────────────────────────

test('the slot defaults decode the effective settings, falling back to 1024 MB', () => {
  const sandbox = loadApp();
  const d0 = vm.runInContext('window.computeVerifierSlotDefaults({})', sandbox);
  assert.equal(d0.memMb, '1024');
  assert.match(d0.slots, /1 slot/);
  const d1 = vm.runInContext('window.computeVerifierSlotDefaults({verifier_min_free_mem_mb_effective: 2048})', sandbox);
  assert.equal(d1.memMb, '2048');
});

test('free memory renders with separators and is blank when absent', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext('window.computeFormatFreeMem(1536)', sandbox), '1,536 MB');
  assert.equal(vm.runInContext('window.computeFormatFreeMem(0)', sandbox), '0 MB');
  assert.equal(vm.runInContext('window.computeFormatFreeMem(undefined)', sandbox), '');
});

test('the headroom line says pass or fail with the reason when one comes', () => {
  const sandbox = loadApp();
  const ok = vm.runInContext('window.computeHeadroomLine({headroom_ok: true, headroom_reason: "24576 MB free; 8192 MB needed"})', sandbox);
  assert.match(ok, /passed/);
  assert.match(ok, /24576 MB free/);
  const fail = vm.runInContext('window.computeHeadroomLine({headroom_ok: false, headroom_reason: "below 1 GB"})', sandbox);
  assert.match(fail, /not passed/);
  assert.match(fail, /below 1 GB/);
  assert.equal(vm.runInContext('window.computeHeadroomLine({})', sandbox), '');
});

test('the suggestion line names the count, zero, and a missing reading', () => {
  const sandbox = loadApp();
  const two = vm.runInContext('window.computeSuggestionLine({suggested_verifier_slots: 2})', sandbox);
  assert.match(two, /2/);
  const zero = vm.runInContext('window.computeSuggestionLine({suggested_verifier_slots: 0})', sandbox);
  assert.match(zero, /too small/);
  const none = vm.runInContext('window.computeSuggestionLine({})', sandbox);
  assert.match(none, /no usable data/);
});

test('the node detail slots section renders every reading when present', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    "window.computeNodeDetailSlotsHtml({session_slots: 5, executor_slots: 5, verifier_slots: 4, verifier_slots_effective: 3, verifier_context_mb: 0, verifier_min_free_mem_mb_effective: 1024, free_gpu_mb: 1536, headroom_ok: false, headroom_reason: 'only 1536 MB free; needs ≥ 1024 MB headroom', suggested_verifier_slots: 3})",
    sandbox
  );
  assert.match(html, /Slots and headroom/);
  assert.match(html, /Executor slots/);
  assert.match(html, /Verifier slots/);
  assert.match(html, /effective 5/);
  assert.match(html, /operator: 4/);
  assert.match(html, /effective 3/);
  assert.match(html, /min free memory: 1024 MB/);
  assert.match(html, /1,536 MB/);
  assert.match(html, /not passed: only 1536 MB free/);
  assert.match(html, /this box can usefully hold 3 verifier slot/);
});

test('the node detail slots section stays hidden without reader data', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext('window.computeNodeDetailSlotsHtml({})', sandbox), '');
  assert.equal(vm.runInContext('window.computeNodeDetailSlotsHtml("oops")', sandbox), '');
  assert.equal(vm.runInContext('window.computeNodeDetailSlotsHtml(null)', sandbox), '');
});

test('the capacity verifier row shows pool name, held and limit under the node', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    "window.capacityVerifierPoolRow({name: 'node:gpubox', held: 2, limit: 4, verifier_held: 1, verifier_limit: 2})",
    sandbox
  );
  assert.match(html, /node-verify:/);
  assert.match(html, /gpubox/);
  assert.match(html, /1 held \/ 2 limit/);
});

test('pools without a verifier side render no verifier row', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext("window.capacityVerifierPoolRow({name: 'lane:p3', held: 1, limit: 2})", sandbox), '');
  assert.equal(vm.runInContext("window.capacityVerifierPoolRow({name: 'queue:prds', held: 0})", sandbox), '');
  assert.equal(vm.runInContext('window.capacityVerifierPoolRow(null)', sandbox), '');
});

test('the lane fill node slots list shows executor and verifier counts per node', () => {
  const sandbox = loadApp();
  const html = vm.runInContext([
    'window._laneFillNodeSlotsHtml({pools: [',
    '  {name: "node:a", limit: 4, held: 2, verifier_limit: 2, verifier_held: 1},',
    '  {name: "node:b", limit: 2, held: 0, verifier_limit: 0, verifier_held: 0},',
    '  {name: "lane:p1", limit: 1, held: 1},',
    ']})',
  ].join(''), sandbox);
  assert.match(html, /Slots on each node/);
  assert.match(html, /executor slots a: 2\/4 held\/limit/);
  assert.match(html, /verifier slots a: 1\/2 held\/limit/);
  assert.match(html, /executor slots b: 0\/2 held\/limit/);
  assert.doesNotMatch(html, /lane:p1/);
});

test('the lane fill node slots list is empty without a capacity body', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext('window._laneFillNodeSlotsHtml(undefined)', sandbox), '');
  assert.equal(vm.runInContext('window._laneFillNodeSlotsHtml({})', sandbox), '');
  assert.equal(vm.runInContext('window._laneFillNodeSlotsHtml({pools: [{name: "lane:p0"}]})', sandbox), '');
});

test('the lane fill card carries the node slot counts next to its pools', () => {
  const sandbox = loadApp();
  const status = "{mode: 'auto', lanes: [{id: 'gpu-1', type: 'gpu', state: 'idle'}], lane_counts: {}}";
  const cap = "{pools: [{name: 'node:a', limit: 4, held: 2, verifier_limit: 2, verifier_held: 2}]}";
  const html = vm.runInContext(`window.laneFillCardHtml(${status}, ${cap})`, sandbox);
  assert.match(html, /Slots on each node/);
  assert.match(html, /verifier slots a: 2\/2 held\/limit/);
  // Without a capacity body the card still renders its lanes.
  const bare = vm.runInContext(`window.laneFillCardHtml(${status})`, sandbox);
  assert.match(bare, /Lane fill/);
  assert.doesNotMatch(bare, /Slots on each node/);
});

// ── Locale bundles ─────────────────────────────────────────────────────────

test('every locale bundle carries the new keys with a real translation', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of NEW_KEYS) {
      assert.ok(k in b, `${lang}.json is missing ${k}`);
      const v = b[k];
      assert.equal(typeof v, 'string', `${lang}.json ${k} is not a string`);
      assert.ok(v.trim().length > 0, `${lang}.json ${k} is empty`);
      assert.notEqual(v, k, `${lang}.json ${k} holds its own key`);
      // Placeholders stay Android-style %n$s so t() can substitute them;
      // a bare % that t() would leave behind is a bug.
      const bare = v.replace(/%\d+\$[sd]/g, '');
      assert.ok(!bare.includes('%'), `${lang}.json ${k} has a placeholder t() cannot substitute: ${v}`);
    }
  }
});

test('the five bundles keep the same total key set', () => {
  const sets = {};
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    sets[lang] = Object.keys(bundle(lang)).sort();
  }
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    assert.deepEqual(sets[lang], sets.en, `${lang}.json key set differs from en.json`);
  }
});
