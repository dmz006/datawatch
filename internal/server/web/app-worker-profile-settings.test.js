// Settings > General > Autonomous: the Worker session profiles block
// (autonomous.worker_profile, autonomous.worker_profile_defaults and
// autonomous.worker_profiles). Pins the field list, the group header, the
// ? help link target, the select's option list (built-ins plus user
// profiles), and the locale keys in all five bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');
const HELP_DOCS = 'howto/worker-session-profiles.md';

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function autonomousFields() {
  const sb = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)", sb));
}

function fieldByKey(key) {
  return autonomousFields().find(f => f.key === key) || null;
}

test('GENERAL_CONFIG_FIELDS lists worker_profile, worker_profile_defaults and worker_profiles under autonomous', () => {
  const keys = autonomousFields().map(f => f.key).filter(Boolean);
  for (const k of ['autonomous.worker_profile', 'autonomous.worker_profile_defaults', 'autonomous.worker_profiles']) {
    assert.ok(keys.includes(k), k + ' missing from the autonomous card: ' + keys.join(','));
  }
});

test('the Worker session profiles group header sits before its three fields', () => {
  const fields = autonomousFields();
  const head = fields.findIndex(f => f.labelKey === 'settings_worker_profiles_group' && f.type === 'group');
  assert.ok(head >= 0, 'group header missing');
  const first = fields.findIndex(f => f.key === 'autonomous.worker_profile');
  const last = fields.findIndex(f => f.key === 'autonomous.worker_profiles');
  assert.ok(head < first && first < last, 'header must precede the three fields in order');
});

test('worker_profile is a select whose options come from the profile names', () => {
  const f = fieldByKey('autonomous.worker_profile');
  assert.ok(f, 'field missing');
  assert.equal(f.type, 'select');
  assert.equal(f.optionsFrom, 'worker_profiles');
  assert.equal(f.helpDocs, HELP_DOCS);
});

test('worker_profile_defaults renders role and kind selects and links to the howto', () => {
  const f = fieldByKey('autonomous.worker_profile_defaults');
  assert.ok(f, 'field missing');
  assert.equal(f.type, 'worker_profile_defaults');
  assert.equal(f.helpDocs, HELP_DOCS);
});

test('worker_profiles is the JSON editor and links to the howto', () => {
  const f = fieldByKey('autonomous.worker_profiles');
  assert.ok(f, 'field missing');
  assert.equal(f.type, 'worker_profiles_json');
  assert.equal(f.helpDocs, HELP_DOCS);
});

test('workerProfileNames lists the built-ins then user profiles, each once, with an optional blank first', () => {
  const sb = loadApp();
  const names = vm.runInContext(
    "JSON.stringify(workerProfileNames({autonomous: {worker_profiles: {mine: {}, full: {}}}}))", sb);
  assert.deepEqual(JSON.parse(names), ['minimal', 'digest', 'datawatch-tools', 'full', 'mine']);
  const blank = vm.runInContext("JSON.stringify(workerProfileNames({}, true))", sb);
  assert.deepEqual(JSON.parse(blank).slice(0, 2), ['', 'minimal']);
});

test('every worker profile field has its label and help keys in all five locales', () => {
  const keys = [
    'settings_worker_profiles_group',
    'settings_worker_profile_label',
    'settings_worker_profile_help',
    'settings_worker_profile_defaults_label',
    'settings_worker_profile_defaults_help',
    'settings_worker_profiles_label',
    'settings_worker_profiles_help',
  ];
  for (const lang of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, lang + ' missing ' + k);
    }
  }
});

test('the autonomous card lists a read-only profile-in-force field after the three worker profile fields', () => {
  const fields = autonomousFields();
  const idx = fields.findIndex(f => f.type === 'worker_profile_in_force');
  const last = fields.findIndex(f => f.key === 'autonomous.worker_profiles');
  assert.ok(idx >= 0, 'profile-in-force field missing');
  assert.ok(idx > last, 'profile-in-force must sit below the worker profile selects');
  const f = fields[idx];
  assert.equal(f.labelKey, 'settings_worker_profile_in_force_label');
  assert.equal(f.helpKey, 'settings_worker_profile_in_force_help');
  assert.equal(f.key, undefined, 'a read-only list stores no config key');
});

test('workerProfileInForceHTML renders one line per in_force entry with its profile and tier', () => {
  const sb = loadApp();
  const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  sb.window._i18n = { bundle, fallback: bundle };
  // Same shape GET /api/config serves: in_force sits inside the autonomous block.
  const cfg = { autonomous: { in_force: [
    { scope: 'role', key: 'worker', profile: 'full', tier: 'daemon', reason: '' },
    { scope: 'kind', key: 'docs', profile: 'minimal', tier: 'built-in', reason: '' },
    { scope: 'kind', key: 'note', profile: '', tier: 'none', reason: '' },
  ] } };
  sb.__cfg = cfg;
  const html = vm.runInContext(
    "workerProfileInForceHTML(__cfg, {labelKey: 'settings_worker_profile_in_force_label', helpKey: 'settings_worker_profile_in_force_help'}, 'Profile in force')", sb);
  const items = html.match(/<li>[^<]*<\/li>/g) || [];
  assert.deepEqual(items, [
    '<li>worker: full (daemon-wide)</li>',
    '<li>docs: minimal (built-in)</li>',
    '<li>note: none (no profile)</li>',
  ]);
  assert.ok(!/<input|<select|<textarea/.test(html), 'the list must be read-only');
});

test('workerProfileInForceHTML renders nothing when the daemon reports no in_force block', () => {
  const sb = loadApp();
  sb.__cfg = {};
  const html = vm.runInContext("workerProfileInForceHTML(__cfg, {}, 'x')", sb);
  assert.equal(html, '');
});

test('workerProfileInForceHTML ignores a top-level in_force key the daemon never serves', () => {
  const sb = loadApp();
  const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));
  sb.window._i18n = { bundle, fallback: bundle };
  sb.__cfg = { in_force: [{ scope: 'role', key: 'worker', profile: 'full', tier: 'daemon', reason: '' }] };
  const html = vm.runInContext("workerProfileInForceHTML(__cfg, {}, 'x')", sb);
  assert.equal(html, '');
});

test('every profile-in-force locale key is present in all five bundles', () => {
  const keys = [
    'settings_worker_profile_in_force_label',
    'settings_worker_profile_in_force_help',
    'settings_worker_profile_in_force_row',
    'settings_worker_profile_in_force_none',
    'settings_worker_profile_in_force_tier_built_in',
    'settings_worker_profile_in_force_tier_daemon',
    'settings_worker_profile_in_force_tier_role',
    'settings_worker_profile_in_force_tier_kind',
    'settings_worker_profile_in_force_tier_none',
  ];
  for (const lang of LOCALE_LIST) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundle[k] === 'string' && bundle[k].length > 0, lang + ' missing ' + k);
    }
  }
});
