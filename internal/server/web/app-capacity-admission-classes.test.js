// Pins the admission-class settings in Settings > General > Autonomous (the
// Claude account cap, the three supervisor buffers and the operator-overage
// alert) and the capacity view's notes: operator sessions over a limit and the
// buffer only supervisor sessions may use. Every string exists in all five
// locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const FIELDS = [
  ['autonomous.capacity_account_claude_limit', 'settings_capacity_account_claude_limit', 'number'],
  ['autonomous.capacity_supervisor_reserve_claude', 'settings_capacity_supervisor_reserve_claude', 'number'],
  ['autonomous.capacity_supervisor_reserve_pi', 'settings_capacity_supervisor_reserve_pi', 'number'],
  ['autonomous.capacity_supervisor_reserve_host', 'settings_capacity_supervisor_reserve_host', 'number'],
  ['autonomous.capacity_operator_reserve_host', 'settings_capacity_operator_reserve_host', 'number'],
  ['autonomous.capacity_supervisor_credential_enabled', 'settings_capacity_supervisor_credential_enabled', 'toggle'],
  ['autonomous.capacity_operator_overage_alert', 'settings_capacity_operator_overage_alert', 'toggle'],
];
const POOL_KEYS = ['capacity_pool_account', 'capacity_pool_over', 'capacity_pool_reserve'];

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  return sandbox;
}

test('admission class settings are in the autonomous settings with the right control', () => {
  const sandbox = loadApp();
  for (const [key, labelKey, type] of FIELDS) {
    const f = JSON.parse(vm.runInContext(
      "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
      sandbox));
    assert.ok(f, key + ' is missing from the autonomous settings');
    assert.equal(f.labelKey, labelKey);
    assert.equal(f.type, type);
    if (type === 'number') assert.equal(f.min, 0, key + ' must refuse negatives');
  }
});

test('all five locale bundles carry every admission class string', () => {
  for (const loc of LOCALES) {
    const b = bundle(loc);
    for (const k of [...FIELDS.map(f => f[1]), ...POOL_KEYS]) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, loc + ' lacks ' + k);
    }
    assert.ok(b.capacity_pool_over.includes('{0}') && b.capacity_pool_reserve.includes('{0}'), loc + ' pool notes need the {0} count');
  }
});

test('the capacity view labels the account pool and shows overage and the supervisor buffer', () => {
  const sandbox = loadApp();
  const label = (p) => vm.runInContext('capacityPoolLabel(' + JSON.stringify(p) + ')', sandbox);
  assert.equal(label({ name: 'account:claude' }), 'Account: claude');
  assert.equal(label({ name: 'llm:claude-code', over: 1, reserve: 1 }),
    'LLM: claude-code (1 over the limit: operator) (+1 reserved for supervisors)');
  assert.equal(label({ name: 'llm:pi' }), 'LLM: pi', 'a pool with neither note keeps its plain label');
});
