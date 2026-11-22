// The compute node form carries the operator's session_slots override. The
// same builder renders the add and the edit form, and the collector reads
// the field back into the node record: a blank field saves 0 (derive from
// observer data), and a number saves as that whole number.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function loadApp(byId = {}) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

// The collector only reads .value from the input, so a plain object stands in.
function inputWithValue(value) {
  return { value };
}

test('the node form renders a session_slots input showing the saved override', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("buildComputeNodeForm({name: 'gpubox', kind: 'ollama', session_slots: 3})", sandbox);
  assert.match(html, /id="fe_compute_session_slots"/);
  assert.match(html, /id="fe_compute_session_slots"[^>]*value="3"/);
});

test('the node form shows an empty session_slots input when no override is set', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("buildComputeNodeForm({name: 'gpubox', kind: 'ollama'})", sandbox);
  assert.match(html, /id="fe_compute_session_slots"[^>]*value=""/);
});

test('collecting the form saves a typed session_slots as a whole number', () => {
  const sandbox = loadApp({ fe_compute_session_slots: inputWithValue('4') });
  const out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox'}))", sandbox));
  assert.equal(out.session_slots, 4);
});

test('collecting the form with a blank session_slots saves 0 (derive)', () => {
  const sandbox = loadApp({ fe_compute_session_slots: inputWithValue('') });
  const out = JSON.parse(vm.runInContext("JSON.stringify(collectComputeNodeForm({name: 'gpubox', session_slots: 7}))", sandbox));
  assert.equal(out.session_slots, 0);
});

// ── headroom block (compute node detail) ──────────────────────────────────────
// buildComputeNodeHeadroomBlock renders the headroom section of the
// compute-node detail modal from the last stored measurement (GET
// /api/compute/nodes/{name}/headroom): last measurement time, its age,
// free MB, per-request MB, and the supported cap. null = the node has
// never been measured (GET 404) → "no measurement".
//
// The test sandbox loads app.js with no locale bundle, so t(key) returns
// the key itself; assert on keys (the shipped en.json values are the
// human-facing strings) and on the exact rendered value spans.

function renderHeadroom(measurement) {
  const sandbox = loadApp();
  return vm.runInContext(
    `buildComputeNodeHeadroomBlock('gpubox', ${JSON.stringify(measurement)})`,
    sandbox,
  );
}

test('headroom block: no measurement → says "no measurement", still offers Measure now', () => {
  const html = renderHeadroom(null);
  assert.ok(html.includes('id="compute_headroom_block"'), 'block container is present');
  assert.ok(html.includes('compute_headroom_none'), 'shows the no-measurement label (en.json: "no measurement")');
  assert.ok(html.includes("computeHeadroomMeasure('gpubox')"), 'Measure now button still offered with the node name');
  assert.ok(!html.includes(' MB</span>'), 'no MB values rendered when never measured');
  assert.ok(!html.includes('compute_headroom_fresh') && !html.includes('compute_headroom_stale'), 'no freshness badge without a measurement');
});

test('headroom block: fresh measurement → time, age, free MB, per-request MB, cap', () => {
  const html = renderHeadroom({
    measured_at: '2026-10-31T09:00:00Z',
    age_seconds: 60,
    fresh: true,
    free_mb: 8192,
    per_request_mb: 512,
    supported_cap: 16,
  });
  assert.ok(html.includes('>2026-10-31T09:00:00Z</span>'), 'last measurement time rendered');
  assert.ok(html.includes('>1m 00s</span>'), 'age rendered from age_seconds (60s → 1m 00s)');
  assert.ok(html.includes('>8192 MB</span>'), 'free MB rendered');
  assert.ok(html.includes('>512 MB</span>'), 'per-request MB rendered');
  assert.ok(html.includes('>16</span>'), 'supported cap rendered');
  assert.ok(html.includes('compute_headroom_fresh'), 'fresh badge shown');
  assert.ok(!html.includes('compute_headroom_stale'), 'stale badge not shown');
});

test('headroom block: stale measurement → badged stale, age still shown', () => {
  const html = renderHeadroom({
    measured_at: '2026-10-29T09:00:00Z',
    age_seconds: 7200,
    fresh: false,
    free_mb: 4096,
    per_request_mb: 2048,
    supported_cap: 2,
  });
  assert.ok(html.includes('compute_headroom_stale'), 'stale badge shown');
  assert.ok(!html.includes('compute_headroom_fresh'), 'fresh badge not shown');
  assert.ok(html.includes('>2h 00m</span>'), 'age rendered from age_seconds (7200s → 2h 00m)');
  assert.ok(html.includes('>4096 MB</span>'), 'free MB rendered');
  assert.ok(html.includes('>2048 MB</span>'), 'per-request MB rendered');
});
