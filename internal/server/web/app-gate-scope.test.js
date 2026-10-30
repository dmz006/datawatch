// Quality-gate scoping in the PWA: the Autonomous settings card must expose
// a default test-scope select (full | touched-plus-smoke | touched) and a
// comma-separated smoke-package field, and the per-task view must render the
// recorded scope plan (escaped plain text, collapsed, with per-package
// reasons). An absent scope plan renders nothing — full-suite gate runs and
// older tasks have no scope_plan.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_FILES = ['en', 'de', 'es', 'fr', 'ja'];

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  // Point t() at the real EN bundle so rendered text uses the shipped
  // translations (the test sandbox never fetches locales itself).
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox._i18n.fallback = en;
  sandbox._i18n.bundle = en;
  sandbox._i18n.ready = true;
  return sandbox;
}

// ── Settings card fields ────────────────────────────────────────────────────

test('autonomous card exposes the gate test-scope select with exactly three options and a label key', () => {
  const sandbox = loadAppJS();
  const field = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.default_quality_gates.scope') || null)",
    sandbox));
  assert.ok(field, 'autonomous.default_quality_gates.scope missing from the autonomous card');
  assert.equal(field.type, 'select');
  assert.deepEqual(field.options, ['full', 'touched-plus-smoke', 'touched'],
    'scope select must expose exactly full / touched-plus-smoke / touched');
  assert.equal(field.labelKey, 'settings_gate_scope');
  assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  assert.ok(typeof en.settings_gate_scope === 'string' && en.settings_gate_scope.length > 0,
    'settings_gate_scope missing from en locale');
});

test('autonomous card exposes the smoke-packages field with a label key and the smoke-package save transformation', () => {
  const sandbox = loadAppJS();
  const field = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.default_quality_gates.smoke_packages') || null)",
    sandbox));
  assert.ok(field, 'autonomous.default_quality_gates.smoke_packages missing from the autonomous card');
  assert.equal(field.labelKey, 'settings_gate_smoke_packages');
  assert.ok(field.label && field.label.length > 0, 'hardcoded fallback label non-empty');
  assert.equal(typeof field.onchange, 'string', 'field must override the save handler');
  assert.match(field.onchange, /saveGateSmokePackagesSetting/);

  // Comma list in, array out: trim each entry and drop blanks.
  const out = vm.runInContext(
    "(() => { const calls = []; saveGeneralConfigPut = (k, v) => calls.push([k, v]); " +
    "saveGateSmokePackagesSetting({ value: 'internal/pipeline, internal/server ,  ' }); " +
    "saveGateSmokePackagesSetting({ value: '' }); " +
    "saveGateSmokePackagesSetting(null); " +
    "return JSON.stringify(calls); })()", sandbox);
  const calls = JSON.parse(out);
  assert.equal(calls[0][0], 'autonomous.default_quality_gates.smoke_packages');
  assert.deepEqual(calls[0][1], ['internal/pipeline', 'internal/server'], 'comma list converts to a trimmed array');
  assert.deepEqual(calls[1][1], [], 'empty box saves an empty list');
  assert.deepEqual(calls[2][1], [], 'missing element saves an empty list');
});

test('scope select saves its value as-is, with an unmade selection saved as empty', () => {
  const sandbox = loadAppJS();
  const out = vm.runInContext(
    "(() => { const calls = []; saveGeneralConfigPut = (k, v) => calls.push([k, v]); " +
    "saveGateScopeSetting({ value: 'touched-plus-smoke' }); " +
    "saveGateScopeSetting({ value: '' }); " +
    "saveGateScopeSetting(null); " +
    "return JSON.stringify(calls); })()", sandbox);
  const calls = JSON.parse(out);
  for (const [k] of calls) assert.equal(k, 'autonomous.default_quality_gates.scope');
  assert.equal(calls[0][1], 'touched-plus-smoke', 'chosen scope saves as-is');
  assert.equal(calls[1][1], '', 'empty select saves empty (inherit the built-in default)');
  assert.equal(calls[2][1], '', 'unmade selection saves empty');
});

// ── Locales ─────────────────────────────────────────────────────────────────

const GATE_SCOPE_KEYS = [
  'settings_gate_scope',
  'settings_gate_scope_hint',
  'settings_gate_smoke_packages',
  'settings_gate_smoke_packages_hint',
  'prd_task_gate_scope',
  'prd_task_gate_scope_full',
];

for (const locale of LOCALE_FILES) {
  test(`gate-scope keys are non-empty real strings in the ${locale} bundle`, () => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of GATE_SCOPE_KEYS) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].trim().length > 0,
        k + ' missing or empty in ' + locale + ' locale');
      assert.ok(!/\bBL\d+\b/.test(bundle[k]), 'no tracker IDs in ' + locale + ' text');
      assert.ok(!bundle[k].includes('{') && !bundle[k].includes('}'), 'no template braces in ' + locale + ' text');
    }
    assert.ok(bundle.settings_gate_scope !== (bundle.settings_gate_scope_hint),
      locale + ': scope label and hint must differ');
  });
}

// ── Per-task scope-plan rendering ───────────────────────────────────────────

function taskJson(res) {
  return JSON.stringify(res === null ? {} : { quality_gate_result: res });
}

test('scope plan renders scope, package list and per-package reasons as escaped plain text', () => {
  const sandbox = loadAppJS();
  const html = vm.runInContext('renderTaskGateScopePlan(' + taskJson({
    regression: false,
    scope_plan: {
      scope: 'touched-plus-smoke',
      scope_source: 'global',
      packages: ['internal/pipeline', 'internal/server <web>'],
      reasons: [
        { package: 'internal/pipeline', reason: 'touched by changed files' },
        { package: 'internal/server <web>', reason: 'smoke: & configured' },
      ],
    },
  }) + ')', sandbox);
  assert.ok(html.includes('<details'), 'plan renders as a collapsed <details>');
  assert.ok(html.includes('Quality gate scope: touched-plus-smoke'), 'summary carries the scope value');
  assert.ok(html.includes('· global'), 'scope source shown');
  assert.ok(html.includes('internal/pipeline, internal/server &lt;web&gt;'), 'package list escaped');
  assert.ok(!html.includes('<web>'), 'raw angle brackets must not leak through');
  assert.ok(html.includes('smoke: &amp; configured'), 'reason text escaped');
  assert.ok(html.includes('touched by changed files'), 'touch reason shown');
});

test('full-suite plans render the full-suite label with the reason', () => {
  const sandbox = loadAppJS();
  const html = vm.runInContext('renderTaskGateScopePlan(' + taskJson({
    regression: false,
    scope_plan: { scope: 'full', scope_source: 'default', full: true, full_reason: 'final gate task always runs the full suite' },
  }) + ')', sandbox);
  assert.ok(html.includes('Quality gate scope: full'), 'scope summary says full');
  assert.ok(html.includes('full suite'), 'full-suite label rendered');
  assert.ok(html.includes('final gate task always runs the full suite'), 'full reason rendered');
});

test('no scope plan renders nothing: absent result, present result without a plan, null task', () => {
  const sandbox = loadAppJS();
  assert.equal(vm.runInContext('renderTaskGateScopePlan({})', sandbox), '', 'no quality_gate_result');
  assert.equal(vm.runInContext('renderTaskGateScopePlan(' + taskJson({ regression: true, summary: 'x' }) + ')', sandbox),
    '', 'gate result without scope_plan');
  assert.equal(vm.runInContext('renderTaskGateScopePlan(null)', sandbox), '', 'null task');
  assert.equal(vm.runInContext('renderTaskGateScopePlan(' + taskJson({ scope_plan: null }) + ')', sandbox),
    '', 'explicit null scope_plan');
});
