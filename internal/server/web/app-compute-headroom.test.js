// The compute node detail modal (Settings → Compute → Compute Nodes) shows
// the node's last headroom measurement — free MB, per-request MB, resident
// model, its age, freshness under the configured max age, and the cap it
// supports — with a Measure now button on the REST measure endpoint, plus
// a Save-gated "Parallel verifiers" field on the shared node-update write
// gate. The modal body re-renders on every poll, so the input must survive
// redraws: while it is focused or differs from the saved setting, the
// redraw is skipped.
//
// The block builders and the redraw guard are pure — they decode an
// already-fetched body and return strings or booleans — so they are driven
// below without a server: the English bundle is handed to t() through
// window._i18n, exactly the way loadLocale() does in the browser. The
// measure and save handlers are driven against a stubbed apiFetch plus
// pre-registered element stubs, the same way the drain/undrain handlers
// are tested.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const LOCALES = path.join(__dirname, 'locales');
function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES, lang + '.json'), 'utf8'));
}
// The new keys this feature adds; the parity test below holds every bundle
// to exactly this set (plus whatever it already had).
const NEW_KEYS = [
  'compute_cap_error',
  'compute_cap_hint',
  'compute_cap_label',
  'compute_cap_refused',
  'compute_cap_save',
  'compute_cap_saved',
  'compute_cap_title',
  'compute_headroom_age',
  'compute_headroom_age_h',
  'compute_headroom_age_m',
  'compute_headroom_age_s',
  'compute_headroom_btn',
  'compute_headroom_btn_title',
  'compute_headroom_free_mb',
  'compute_headroom_fresh',
  'compute_headroom_max_age',
  'compute_headroom_max_age_value',
  'compute_headroom_measured',
  'compute_headroom_measured_at',
  'compute_headroom_measuring',
  'compute_headroom_none',
  'compute_headroom_per_request_mb',
  'compute_headroom_refused',
  'compute_headroom_resident_model',
  'compute_headroom_stale',
  'compute_headroom_supported_cap',
  'compute_headroom_title',
  'compute_headroom_unbounded',
  'compute_detail_unavailable',
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

// The handlers only read .value from the input and set disabled/textContent
// on the button and textContent/style on the message line, so plain objects
// stand in for the DOM.
function makeInput(value) { return { value }; }
function makeButton() { return { disabled: false, textContent: '' }; }
function makeMsg() { return { style: {}, textContent: '' }; }

const MEASUREMENT = {
  measured_at: '2026-11-05T12:00:00Z',
  age_seconds: 312,
  fresh: true,
  max_age_hours: 6,
  free_mb: 123456,
  per_request_mb: 8192,
  resident_model: 'qwen2.5-coder-32b',
  supported_cap: 8,
};

// ── Pure builders ──────────────────────────────────────────────────────────

test('the age renders as h/m/s, m/s or s only', () => {
  const sandbox = loadApp();
  assert.equal(vm.runInContext('window.formatComputeHeadroomAge(125)', sandbox), '2 m 5 s');
  assert.equal(vm.runInContext('window.formatComputeHeadroomAge(3725)', sandbox), '1 h 2 m 5 s');
  assert.equal(vm.runInContext('window.formatComputeHeadroomAge(45)', sandbox), '45 s');
  assert.equal(vm.runInContext('window.formatComputeHeadroomAge(0)', sandbox), '0 s');
  assert.equal(vm.runInContext('window.formatComputeHeadroomAge(-9)', sandbox), '0 s');
  assert.equal(vm.runInContext('window.formatComputeHeadroomAge("bad")', sandbox), '0 s');
});

test('the headroom body renders every reading of a stored measurement', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.buildComputeNodeHeadroomBody({measured_at: "2026-11-05T12:00:00Z", age_seconds: 312, fresh: true, max_age_hours: 6, free_mb: 123456, per_request_mb: 8192, resident_model: "qwen2.5-coder-32b", supported_cap: 8})', sandbox);
  assert.match(html, /Measured at/);
  assert.match(html, /2026-11-05T12:00:00Z/);
  assert.match(html, /5 m 12 s/);
  assert.match(html, /fresh/);
  assert.match(html, /Max age/);
  assert.match(html, /6 h/);
  assert.match(html, /Free memory/);
  assert.match(html, /123,456 MB/);
  assert.match(html, /Per verifier request/);
  assert.match(html, /8,192 MB/);
  assert.match(html, /Resident model/);
  assert.match(html, /qwen2\.5-coder-32b/);
  assert.match(html, /Supports cap/);
  assert.match(html, /8/);
});

test('the headroom body says stale when the measurement is older than the max age', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.buildComputeNodeHeadroomBody({age_seconds: 99999, fresh: false, supported_cap: 1})', sandbox);
  assert.match(html, /stale/);
  assert.doesNotMatch(html, /fresh/);
});

test('a supported cap of 0 renders as unbounded', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.buildComputeNodeHeadroomBody({supported_cap: 0})', sandbox);
  assert.match(html, /unbounded/);
});

test('absent readings drop their rows instead of rendering blanks', () => {
  const sandbox = loadApp();
  const html = vm.runInContext('window.buildComputeNodeHeadroomBody({age_seconds: 5, fresh: true})', sandbox);
  assert.match(html, /5 s/);
  assert.doesNotMatch(html, /Free memory/);
  assert.doesNotMatch(html, /Resident model/);
  assert.doesNotMatch(html, /Per verifier request/);
  assert.doesNotMatch(html, /Measured at/);
});

test('a missing measurement is the not-measured line, never an error', () => {
  const sandbox = loadApp();
  const nullBody = vm.runInContext('window.buildComputeNodeHeadroomBody(null)', sandbox);
  assert.match(nullBody, /No measurement yet/);
  const oops = vm.runInContext('window.buildComputeNodeHeadroomBody("oops")', sandbox);
  assert.match(oops, /No measurement yet/);
});

test('the headroom block carries the title, the Measure now button and the stable element ids', () => {
  const sandbox = loadApp();
  const html = vm.runInContext("window.buildComputeNodeHeadroomBlock('gpubox', null)", sandbox);
  assert.match(html, /Headroom/);
  assert.match(html, /id="compute_detail_headroom_btn"/);
  assert.match(html, /Measure now/);
  assert.match(html, /computeHeadroomMeasure\('gpubox'\)/);
  assert.match(html, /id="compute_detail_headroom_body"/);
  assert.match(html, /id="compute_detail_headroom_msg"/);
});

test('the cap control renders the saved verifier_slots, blank when unset', () => {
  const sandbox = loadApp();
  const set = vm.runInContext("window.buildComputeNodeCapControl('gpubox', {verifier_slots: 4})", sandbox);
  assert.match(set, /id="compute_detail_verifier_slots"[^>]*value="4"/);
  assert.match(set, /Parallel verifiers/);
  assert.match(set, /id="compute_detail_verifier_slots_save"/);
  assert.match(set, /computeSaveDetailVerifierSlots\('gpubox'\)/);
  assert.match(set, /id="compute_detail_verifier_slots_msg"/);
  const unset = vm.runInContext("window.buildComputeNodeCapControl('gpubox', {})", sandbox);
  assert.match(unset, /id="compute_detail_verifier_slots"[^>]*value=""/);
});

test('the redraw is skipped while the input is focused or differs from the saved setting', () => {
  const sandbox = loadApp();
  const run = (detail, value, focused) => vm.runInContext(
    `window.computeDetailSkipRedraw(${JSON.stringify(detail)}, {value: ${JSON.stringify(value)}}, ${focused})`,
    sandbox
  );
  assert.equal(run({verifier_slots: 4}, '4', false), false);   // matches saved → redraw ok
  assert.equal(run({verifier_slots: 4}, '4', true), true);     // focused → never redraw
  assert.equal(run({verifier_slots: 4}, '8', false), true);    // typed value → survive
  assert.equal(run({}, '2', false), true);                      // blank saved, typed → survive
  assert.equal(run({}, '', false), false);                      // both unset → redraw ok
  assert.equal(run({verifier_slots: 4}, '', false), true);      // blank typed, 4 saved → survive
  assert.equal(vm.runInContext("window.computeDetailSkipRedraw({verifier_slots: 4}, null, false)", sandbox), false);
});

// ── Measure now ────────────────────────────────────────────────────────────

test('Measure now fires the on-demand POST, re-renders the stored measurement, toasts success', async () => {
  const body = makeStubElement();
  const btn = makeButton();
  const msg = makeMsg();
  const calls = [];
  const toasts = [];
  const sandbox = loadApp({
    compute_detail_headroom_btn: btn,
    compute_detail_headroom_body: body,
    compute_detail_headroom_msg: msg,
  });
  sandbox.apiFetch = (p, opts) => {
    calls.push({ p, m: (opts && opts.method) || 'GET' });
    if (p.endsWith('/headroom') && (opts && opts.method) === 'POST') return Promise.resolve(null);
    return Promise.resolve(MEASUREMENT);
  };
  sandbox.showToast = (m, type) => { toasts.push([m, type]); };
  vm.runInContext("window.computeHeadroomMeasure('gpubox')", sandbox);
  await flushAsync(20);
  assert.deepEqual(calls, [
    { p: '/api/compute/nodes/gpubox/headroom', m: 'POST' },
    { p: '/api/compute/nodes/gpubox/headroom', m: 'GET' },
  ]);
  assert.match(body.innerHTML, /123,456 MB/);
  assert.match(body.innerHTML, /Supports cap/);
  assert.equal(msg.textContent, 'Headroom measured');
  assert.deepEqual(toasts, [['Headroom measured', 'success']]);
  assert.equal(btn.disabled, false);
});

test('Measure now shows the server refusal inline and as an error toast', async () => {
  const btn = makeButton();
  const msg = makeMsg();
  const errors = [];
  const sandbox = loadApp({
    compute_detail_headroom_btn: btn,
    compute_detail_headroom_msg: msg,
  });
  sandbox.apiFetch = (p, opts) => {
    if ((opts && opts.method) === 'POST') {
      return Promise.reject(new Error('node gpubox busy: serving 2 verifier runs'));
    }
    return Promise.reject(new Error('no measurement'));
  };
  sandbox.showError = (m) => { errors.push(m); };
  vm.runInContext("window.computeHeadroomMeasure('gpubox')", sandbox);
  await flushAsync(20);
  assert.equal(msg.textContent, 'Measurement refused — node gpubox busy: serving 2 verifier runs');
  assert.deepEqual(errors, ['Measurement refused — node gpubox busy: serving 2 verifier runs']);
  assert.equal(btn.disabled, false);
  assert.equal(btn.textContent, 'Measure now');
});

// ── Verifier cap save ──────────────────────────────────────────────────────

test('Save PUTs the node record with the typed verifier_slots and toasts success', async () => {
  const input = makeInput('4');
  const btn = makeButton();
  const msg = makeMsg();
  const calls = [];
  const toasts = [];
  const sandbox = loadApp({
    compute_detail_verifier_slots: input,
    compute_detail_verifier_slots_save: btn,
    compute_detail_verifier_slots_msg: msg,
  });
  sandbox.apiFetch = (p, opts) => {
    calls.push({ p, m: (opts && opts.method) || 'GET', body: opts && opts.body });
    if ((opts && opts.method) === 'PUT') return Promise.resolve({ ok: true });
    return Promise.resolve({ name: 'gpubox', kind: 'node' });
  };
  sandbox.showToast = (m, type) => { toasts.push([m, type]); };
  vm.runInContext("window.computeSaveDetailVerifierSlots('gpubox')", sandbox);
  await flushAsync(20);
  const put = calls.find(c => c.m === 'PUT');
  assert.ok(put, 'no PUT call was made');
  assert.equal(put.p, '/api/compute/nodes/gpubox');
  const wire = JSON.parse(put.body);
  assert.equal(wire.verifier_slots, 4);
  assert.equal(wire.name, 'gpubox');
  assert.equal(msg.textContent, 'Verifier cap saved');
  assert.deepEqual(toasts, [['Verifier cap saved', 'success']]);
  assert.equal(btn.disabled, false);
});

test('a blank cap field saves null (server decodes it as unset)', async () => {
  const input = makeInput('');
  const sandbox = loadApp({ compute_detail_verifier_slots: input });
  const calls = [];
  sandbox.apiFetch = (p, opts) => {
    calls.push({ m: (opts && opts.method) || 'GET', body: opts && opts.body });
    return Promise.resolve({ name: 'gpubox' });
  };
  vm.runInContext("window.computeSaveDetailVerifierSlots('gpubox')", sandbox);
  await flushAsync(20);
  const put = calls.find(c => c.m === 'PUT');
  assert.ok(put, 'no PUT call was made');
  const wire = JSON.parse(put.body);
  assert.equal(wire.verifier_slots, null);
});

test('a write-gate refusal (409 text) is shown inline and as an error, value untouched', async () => {
  const input = makeInput('8');
  const btn = makeButton();
  const msg = makeMsg();
  const errors = [];
  const sandbox = loadApp({
    compute_detail_verifier_slots: input,
    compute_detail_verifier_slots_save: btn,
    compute_detail_verifier_slots_msg: msg,
  });
  sandbox.apiFetch = (p, opts) => {
    if ((opts && opts.method) === 'PUT') {
      return Promise.reject(new Error('cap on node gpubox refused: measured free 3000 MB, shortfall 500 MB'));
    }
    return Promise.resolve({ name: 'gpubox' });
  };
  sandbox.showError = (m) => { errors.push(m); };
  vm.runInContext("window.computeSaveDetailVerifierSlots('gpubox')", sandbox);
  await flushAsync(20);
  assert.equal(msg.textContent, 'Save refused — cap on node gpubox refused: measured free 3000 MB, shortfall 500 MB');
  assert.deepEqual(errors, ['Save refused — cap on node gpubox refused: measured free 3000 MB, shortfall 500 MB']);
  assert.equal(input.value, '8', 'the operator\'s typed value must survive the refusal');
  assert.equal(btn.disabled, false);
});

test('a negative cap value is rejected client-side with no fetch at all', async () => {
  const input = makeInput('-1');
  const msg = makeMsg();
  const errors = [];
  let fetched = 0;
  const sandbox = loadApp({
    compute_detail_verifier_slots: input,
    compute_detail_verifier_slots_msg: msg,
  });
  sandbox.apiFetch = () => { fetched++; return Promise.resolve({}); };
  sandbox.showError = (m) => { errors.push(m); };
  vm.runInContext("window.computeSaveDetailVerifierSlots('gpubox')", sandbox);
  await flushAsync(20);
  assert.equal(fetched, 0);
  assert.equal(msg.textContent, 'Save failed');
  assert.deepEqual(errors, ['Save failed']);
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
