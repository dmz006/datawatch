// Pins the Settings > General > Autonomous usage back-off fields:
// autonomous.usage_backoff (pause_backend | off) and
// autonomous.backoff_max_wait_minutes (whole minutes >= 1). Both must render
// with safe defaults when the daemon reports nothing yet — an unset mode
// shows the default pause_backend and an unset maximum shows 60, never a
// blank that would save as 0 or switch the feature off by accident.
// The review-reserve pair sits beside them: autonomous.backoff_reserve_
// for_review (whole slots >= 0, 0 = off, default 1) and
// autonomous.backoff_reserve_tokens_per_hour (whole tokens >= 0, 0 = no
// cap). The "back-off, review reserve in use" wait reason must earn its
// own chip label and capacity-card text, not the bare back-off label.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function autonomousFields() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox));
}

function field(key) {
  const f = autonomousFields().find((x) => x.key === key);
  assert.ok(f, key + ' missing from the autonomous card fields');
  return f;
}

test('autonomous.usage_backoff is a pause_backend/off select defaulting to pause_backend', () => {
  const f = field('autonomous.usage_backoff');
  assert.equal(f.type, 'select');
  assert.deepEqual(f.options, ['pause_backend', 'off']);
  assert.equal(f.default, 'pause_backend');
  assert.ok(f.options.includes(f.default), 'default is one of the options');
  assert.equal(f.labelKey, 'settings_usage_backoff');
  assert.ok(f.label && f.label.length > 0, 'fallback label non-empty');
});

test('autonomous.backoff_max_wait_minutes is a whole-minute number >= 1 defaulting to 60', () => {
  const f = field('autonomous.backoff_max_wait_minutes');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 1, 'a 0 or negative maximum must not be enterable');
  assert.equal(f.step, 1);
  assert.equal(f.default, '60');
  assert.equal(f.placeholder, '60');
  assert.equal(f.labelKey, 'settings_backoff_max_wait_minutes');
  assert.ok(f.label && f.label.length > 0, 'fallback label non-empty');
});

test('autonomous.backoff_reserve_for_review is a whole-slot number >= 0 defaulting to 1', () => {
  const f = field('autonomous.backoff_reserve_for_review');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 0, '0 is a legal value: it switches the reserve off');
  assert.equal(f.step, 1);
  assert.equal(f.default, '1');
  assert.equal(f.placeholder, '1');
  assert.equal(f.labelKey, 'settings_backoff_reserve_for_review');
  assert.ok(f.label && f.label.length > 0, 'fallback label non-empty');
});

test('autonomous.backoff_reserve_tokens_per_hour is a whole-token number >= 0 defaulting to 0 (no cap)', () => {
  const f = field('autonomous.backoff_reserve_tokens_per_hour');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 0, '0 is a legal value: it means no token cap');
  assert.equal(f.step, 1);
  assert.equal(f.default, '0');
  assert.equal(f.placeholder, '0');
  assert.equal(f.labelKey, 'settings_backoff_reserve_tokens_per_hour');
  assert.ok(f.label && f.label.length > 0, 'fallback label non-empty');
});

test('the two back-off fields and the review-reserve pair are declared once each', () => {
  const keys = autonomousFields().map((f) => f.key);
  for (const k of ['autonomous.usage_backoff', 'autonomous.backoff_max_wait_minutes',
                   'autonomous.backoff_reserve_for_review', 'autonomous.backoff_reserve_tokens_per_hour']) {
    assert.equal(keys.filter((x) => x === k).length, 1, k + ' declared exactly once');
  }
});

test('an unset daemon value renders the select on its default, never blank', () => {
  const f = field('autonomous.usage_backoff');
  // Same selection rule the settings renderer applies to select fields:
  // String(val) === option. With no value reported, falling back to the
  // field default must land on a real option.
  const reported = undefined;
  const shown = reported === undefined || reported === '' ? f.default : String(reported);
  assert.ok(f.options.includes(shown));
  assert.equal(shown, 'pause_backend');
  // An explicit off survives (off is the one value that is falsy-looking
  // to naive "value || default" fallbacks only if it were empty; it is not).
  assert.ok(f.options.includes('off'));
});

for (const locale of LOCALES) {
  test(`locales: ${locale} defines the usage back-off and reserve labels`, () => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of ['settings_usage_backoff', 'settings_backoff_max_wait_minutes',
                     'settings_backoff_reserve_for_review', 'settings_backoff_reserve_tokens_per_hour',
                     'task_wait_cause_backoff_reserve', 'capacity_backoff_reason_review_reserve']) {
      const v = bundle[k];
      assert.ok(typeof v === 'string' && v.trim().length > 0, `${locale} key ${k} present and non-empty`);
      assert.ok(!/\bBL\d+\b/.test(v), 'no tracker IDs in the string');
    }
  });
}

// The stub has no locale bundle, so t() returns the key itself. The
// assertions below therefore check which i18n key the render path picked —
// which is exactly the translation wiring the labels above pin.
test('the review-reserve wait reason takes its own chip label, other back-off reasons keep the bare one', () => {
  function labelFor(reason) {
    const sandbox = buildSandbox();
    vm.createContext(sandbox);
    loadScript(path.join(__dirname, 'app.js'), sandbox);
    return vm.runInContext(
      "_taskWaitCauseLabel('backoff', " + JSON.stringify(reason) + ')', sandbox);
  }
  assert.equal(labelFor('back-off, review reserve in use'), 'task_wait_cause_backoff_reserve');
  assert.equal(labelFor('pool llm:cc backpressure: back-off, review reserve in use'),
    'task_wait_cause_backoff_reserve', 'the pool-prefixed form is recognized too');
  assert.equal(labelFor('pool llm:cc backpressure: rate limited, resets 12:00'),
    'task_wait_cause_backoff', 'a non-reserve back-off reason keeps the bare label');
});

test('the capacity card translates the reserve back-off reason and leaves other reasons alone', () => {
  function capacityBackoff(reason) {
    const sandbox = buildSandbox();
    vm.createContext(sandbox);
    loadScript(path.join(__dirname, 'app.js'), sandbox);
    return vm.runInContext(
      "capacityBackoffHtml([{ backend: 'llm:cc', until: '2026-01-01T12:00:00Z', reset_known: true, reason: " +
      JSON.stringify(reason) + '}])', sandbox);
  }
  const reserveCard = capacityBackoff('back-off, review reserve in use');
  assert.ok(reserveCard.includes('capacity_backoff_reason_review_reserve'),
    'the reserve reason renders via its translated key', reserveCard);
  const otherCard = capacityBackoff('rate limited, resets 12:00');
  assert.ok(!otherCard.includes('capacity_backoff_reason_review_reserve'),
    'non-reserve reasons are untouched', otherCard);
});
