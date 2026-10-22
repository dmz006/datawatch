// Plan tier + effective-inflight exposure — PWA side.
//
// The daemon now exposes plan_tier on every LLM (GET/PUT /api/llms) and
// the derived effective inflight cap + its source (effective_max_inflight
// / inflight_source). The PWA must let operators set both on the LLM
// forms, edit the autonomous.plan_tier_max_inflight table in Settings,
// and surface the effective cap in the capacity card. Locales: every new
// i18n key must exist in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

test('autonomous settings expose autonomous.plan_tier_max_inflight as a plan_tier_table field', () => {
  const sandbox = loadApp();
  const field = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === 'autonomous.plan_tier_max_inflight') || null)",
    sandbox);
  const parsed = JSON.parse(field);
  assert.ok(parsed, 'autonomous.plan_tier_max_inflight missing from autonomous card fields');
  assert.equal(parsed.type, 'plan_tier_table');
  assert.equal(parsed.labelKey, 'settings_plan_tier_max_inflight');
});

test('planTierOptions renders all four tiers + unset, with the selection marked', () => {
  const sandbox = loadApp();
  const out = vm.runInContext("planTierOptions('max_5x')", sandbox);
  for (const tier of ['pro', 'max_5x', 'max_20x', 'api']) {
    assert.ok(out.includes('value="' + tier + '"'), 'tier option missing: ' + tier + '\n' + out);
  }
  assert.ok(/value="max_5x" selected/.test(out), 'selected tier not marked: ' + out);
  const unset = vm.runInContext("planTierOptions('')", sandbox);
  assert.ok(/value="" selected/.test(unset), 'empty selection not default-marked: ' + unset);
});

test('llmEffectiveCapLine shows cap + source when present, nothing when absent', () => {
  const sandbox = loadApp();
  const withCap = vm.runInContext(
    "llmEffectiveCapLine({effective_max_inflight: 2, inflight_source: 'plan_tier'})", sandbox);
  assert.ok(withCap.includes(': 2'), 'cap value missing: ' + withCap);
  assert.ok(withCap.includes('plan_tier'), 'source missing: ' + withCap);
  const missing = vm.runInContext("llmEffectiveCapLine({})", sandbox);
  assert.equal(missing, '', 'expected empty string for missing cap, got: ' + missing);
  const nullRec = vm.runInContext('llmEffectiveCapLine(null)', sandbox);
  assert.equal(nullRec, '', 'expected empty string for null record');
});

test('savePlanTierTable is defined (collects per-tier inputs, PUTs the table)', () => {
  const sandbox = loadApp();
  assert.equal(typeof vm.runInContext('savePlanTierTable', sandbox), 'function');
});

test('app.js wires plan_tier into both LLM forms and the llm payload', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  // edit popup form + collect
  assert.ok(src.includes("id=\"fe_llm_plan_tier\""), 'edit popup form has no plan_tier select');
  assert.ok(src.includes("out.plan_tier"), 'edit popup collect omits plan_tier');
  // add/replace wizard + collect + payload
  assert.ok(src.includes("id=\"llmEditPlanTier\""), 'wizard has no plan_tier select');
  assert.ok(/plan_tier:\s*planTier,/.test(src), 'wizard payload omits plan_tier');
  // effective cap line on both forms
  assert.match(src, /llmEffectiveCapLine\(l\)/, 'edit form does not show effective cap');
  assert.match(src, /llmEffectiveCapLine\(existing\)/, 'wizard does not show effective cap');
  // capacity card: effective cap + source surfaced for llm pools
  assert.ok(src.includes('p.effective_max_inflight != null'), 'capacity pool title ignores effective cap');
  assert.ok(src.includes('p.inflight_source'), 'capacity pool title ignores cap source');
});

test('all new plan-tier i18n keys exist in all five locale bundles', () => {
  const keys = [
    'llm_field_plan_tier',
    'llm_plan_tier_hint',
    'llm_plan_tier_unset',
    'llm_effective_cap',
    'capacity_pool_llm_cap',
    'settings_plan_tier_max_inflight',
    'settings_plan_tier_hint',
    'settings_plan_tier_save',
  ];
  for (const locale of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0,
        k + ' missing or empty in ' + locale + ' locale');
    }
  }
});
