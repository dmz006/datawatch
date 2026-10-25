// Settings > General > Autonomous Automata planning: the fields for the
// outbound webhook sink and peer forwarding. Each key must be declared with
// the right control type, each label and help string must exist in all five
// locale bundles, and the csv fields must be the list-valued ones.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function loadFields() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)",
    sandbox);
  return JSON.parse(json);
}

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

// The expected controls for each key: type, and csv for a list field.
const EXPECTED = {
  'autonomous.notify.webhook.enabled': { type: 'toggle', labelKey: 'settings_notify_webhook_enabled', helpKey: 'settings_notify_webhook_enabled_help' },
  'autonomous.notify.webhook.allow_hosts': { type: 'text', csv: true, labelKey: 'settings_notify_webhook_allow_hosts', helpKey: 'settings_notify_webhook_allow_hosts_help' },
  'autonomous.notify.webhook.max_attempts': { type: 'number', labelKey: 'settings_notify_webhook_max_attempts' },
  'autonomous.notify.webhook.backoff_seconds': { type: 'text', csv: true, labelKey: 'settings_notify_webhook_backoff_seconds' },
  'autonomous.notify.webhook.jitter_percent': { type: 'number', labelKey: 'settings_notify_webhook_jitter_percent' },
  'autonomous.notify.webhook.attempt_timeout_seconds': { type: 'number', labelKey: 'settings_notify_webhook_attempt_timeout' },
  'autonomous.notify.webhook.max_body_bytes': { type: 'number', labelKey: 'settings_notify_webhook_max_body_bytes' },
  'autonomous.notify.webhook.signature_tolerance_seconds': { type: 'number', labelKey: 'settings_notify_webhook_signature_tolerance' },
  'autonomous.notify.forwarding.enabled': { type: 'toggle', labelKey: 'settings_notify_forwarding_enabled', helpKey: 'settings_notify_forwarding_enabled_help' },
  'autonomous.notify.forwarding.max_hops': { type: 'number', labelKey: 'settings_notify_forwarding_max_hops' },
  'autonomous.notify.forwarding.max_attempts': { type: 'number', labelKey: 'settings_notify_forwarding_max_attempts' },
  'autonomous.notify.forwarding.backoff_seconds': { type: 'text', csv: true, labelKey: 'settings_notify_forwarding_backoff_seconds' },
  'autonomous.notify.forwarding.jitter_percent': { type: 'number', labelKey: 'settings_notify_forwarding_jitter_percent' },
  'autonomous.notify.forwarding.attempt_timeout_seconds': { type: 'number', labelKey: 'settings_notify_forwarding_attempt_timeout' },
};

test('autonomous card declares every lifecycle webhook and forwarding key with its control type', () => {
  const fields = loadFields();
  for (const [key, want] of Object.entries(EXPECTED)) {
    const f = fields.find((x) => x.key === key);
    assert.ok(f, key + ' missing from the autonomous card');
    assert.equal(f.type, want.type, key + ' control type');
    assert.equal(f.labelKey, want.labelKey, key + ' labelKey');
    if (want.helpKey) assert.equal(f.helpKey, want.helpKey, key + ' helpKey');
    if (want.csv) assert.equal(f.csv, true, key + ' should be a csv list field');
    else assert.ok(!f.csv, key + ' should not be a csv field');
  }
});

test('lifecycle sink fields sit next to the default_events field they extend', () => {
  const keys = loadFields().map((f) => f.key);
  const at = keys.indexOf('autonomous.notify.default_events');
  assert.ok(at >= 0, 'default_events field present');
  assert.equal(keys[at + 1], 'autonomous.notify.webhook.enabled', 'webhook fields follow default_events');
});

test('every label and help string for the lifecycle sink fields is non-empty in all five locales', () => {
  const keys = new Set();
  for (const want of Object.values(EXPECTED)) {
    keys.add(want.labelKey);
    if (want.helpKey) keys.add(want.helpKey);
  }
  for (const locale of LOCALES) {
    const b = bundle(locale);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim().length > 0, k + ' missing or empty in ' + locale + ' locale');
      assert.ok(!/\bBL\d+\b/.test(b[k]), k + ' carries a tracker id in ' + locale);
      assert.ok(!/\bPRD\b/.test(b[k]), k + ' uses the internal term PRD in ' + locale);
    }
  }
});

test('the lifecycle sink help text in English names the empty allow-list and the no-restart effect', () => {
  const en = bundle('en');
  assert.match(en.settings_notify_webhook_allow_hosts_help, /Empty means nothing receives/);
  assert.match(en.settings_notify_webhook_enabled_help, /next event/);
});
