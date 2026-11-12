// Pins the Settings > Classifier section: every classifier tunable the daemon
// accepts on PUT /api/config has a field, and each label exists in all five locales.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'timeout_seconds', 'top_logprobs', 'abstain_margin', 'min_probability', 'min_option_mass',
  'rotate_options', 'thinking_off_models', 'probe_ttl_seconds', 'probe_timeout_seconds',
  'calibration.test_fraction', 'calibration.split_seed', 'calibration.min_fit_samples',
  'calibration.false_pass_weight', 'calibration.rotation_bias_threshold',
  'calibration.rotation_flip_threshold',
];

function section() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return JSON.parse(vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'classifier') || null)", sandbox));
}

test('classifier section declares every settable classifier key', () => {
  const sec = section();
  assert.ok(sec, 'classifier section missing');
  assert.deepEqual(sec.fields.map((f) => f.key), KEYS.map((k) => 'classifier.' + k));
  assert.ok(!sec.fields.some((f) => f.key === 'classifier.publisher_allowlist'),
    'the allowlist is confirmation-only and must not be an editable field');
});

test('classifier fields have sensible types', () => {
  const byKey = Object.fromEntries(section().fields.map((f) => [f.key, f]));
  assert.equal(byKey['classifier.rotate_options'].type, 'toggle');
  assert.equal(byKey['classifier.thinking_off_models'].csv, true);
  assert.equal(byKey['classifier.timeout_seconds'].type, 'number');
  assert.equal(byKey['classifier.abstain_margin'].max, 1);
});

for (const lang of LOCALE_LIST) {
  test(`locales: ${lang} defines every classifier settings label`, () => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const f of section().fields) {
      const v = bundle[f.labelKey];
      assert.ok(typeof v === 'string' && v.trim().length > 0, `${lang} ${f.labelKey} present`);
    }
  });
}
