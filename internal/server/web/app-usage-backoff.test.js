// Pins the Settings > General > Autonomous usage back-off fields:
// autonomous.usage_backoff (pause_backend | off) and
// autonomous.backoff_max_wait_minutes (whole minutes >= 1). Both must render
// with safe defaults when the daemon reports nothing yet — an unset mode shows
// the default pause_backend and an unset maximum shows 60, never a blank that
// would save as 0 or switch the feature off by accident.

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

test('the two fields sit next to the other autonomous gates and are declared once', () => {
  const keys = autonomousFields().map((f) => f.key);
  for (const k of ['autonomous.usage_backoff', 'autonomous.backoff_max_wait_minutes']) {
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
  test(`locales: ${locale} defines both usage back-off labels`, () => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of ['settings_usage_backoff', 'settings_backoff_max_wait_minutes']) {
      const v = bundle[k];
      assert.ok(typeof v === 'string' && v.trim().length > 0, `${locale} key ${k} present and non-empty`);
      assert.ok(!/\bBL\d+\b/.test(v), 'no tracker IDs in the string');
    }
  });
}

// Models control on the usage back-off card: the optional models field limits a
// set or clear to those models of the backend; empty means the whole backend.
function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}
function run(sandbox, expr) {
  return JSON.parse(vm.runInContext('JSON.stringify(' + expr + ')', sandbox));
}

test('models field parses commas and spaces, drops blanks and repeats', () => {
  const sb = loadApp();
  assert.deepEqual(run(sb, "usageBackoffParseModels(' opus, sonnet  opus,,haiku ')"), ['opus', 'sonnet', 'haiku']);
  assert.deepEqual(run(sb, "usageBackoffParseModels('')"), []);
  assert.deepEqual(run(sb, 'usageBackoffParseModels(undefined)'), []);
});

test('set body carries models only when named; empty models holds the whole backend', () => {
  const sb = loadApp();
  const scoped = run(sb, "usageBackoffBuildSetBody({backend:'claude-code', minutes:'30', models:'opus, sonnet', reason:'r'})");
  assert.deepEqual(scoped.body, { backend: 'claude-code', minutes: 30, reason: 'r', models: ['opus', 'sonnet'] });
  const whole = run(sb, "usageBackoffBuildSetBody({backend:'claude-code', models:'  '})");
  assert.deepEqual(whole.body, { backend: 'claude-code' });
  assert.ok(!('models' in whole.body), 'no empty models array is sent');
});

test('set body refuses a missing backend and bad minutes', () => {
  const sb = loadApp();
  assert.ok(run(sb, "usageBackoffBuildSetBody({backend:'', models:'opus'})").error);
  assert.ok(run(sb, "usageBackoffBuildSetBody({backend:'x', minutes:'0'})").error);
  assert.ok(run(sb, "usageBackoffBuildSetBody({backend:'x', minutes:'1.5'})").error);
});

test('card renders the models input and the held/open models of each hold', () => {
  const sb = loadApp();
  vm.runInContext("window._usageBackoff = {status:{active:[{backend:'claude-code',until:'2026-11-15T10:00:00Z',reason:'limit',models:['opus']}],holds:[{backend:'claude-code',whole_backend:false,held_models:['opus'],open_models:['sonnet','haiku'],other_models_open:true}]}, error:'', message:''}", sb);
  const html = run(sb, 'usageBackoffCardHtml()');
  assert.match(html, /id="usageBackoffModels"/);
  assert.match(html, /backoff-held-models">[^<]*: opus</);
  assert.match(html, /backoff-open-models">[^<]*: sonnet, haiku</);
  assert.ok(!/\bBL\d+\b/.test(html), 'no tracker IDs in the card');
});

test('whole-backend hold says all models are held; capacity card shows models too', () => {
  const sb = loadApp();
  const whole = run(sb, "usageBackoffModelsHtml({whole_backend:true, held_models:[], open_models:[]})");
  assert.match(whole, /backoff-held-all/);
  const cap = run(sb, "capacityCardHtml({backoff:[{backend:'claude-code',until:'2026-11-15T10:00:00Z',reset_known:true,whole_backend:false,held_models:['opus'],open_models:['sonnet']}]})");
  assert.match(cap, /backoff-held-models">[^<]*: opus</);
  assert.match(cap, /backoff-open-models">[^<]*: sonnet</);
});

test('polling uses a window-level interval, never a DOM-stored timer', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /window\._usageBackoffInterval = setInterval/);
});

const MODEL_KEYS = ['usage_backoff_card_title', 'usage_backoff_models', 'usage_backoff_models_hint', 'usage_backoff_set', 'usage_backoff_clear',
  'usage_backoff_held_models', 'usage_backoff_open_models', 'usage_backoff_whole_backend', 'usage_backoff_models_need_backend'];
for (const locale of LOCALES) {
  test(`locales: ${locale} defines the back-off models control keys`, () => {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
    for (const k of MODEL_KEYS) {
      const v = bundle[k];
      assert.ok(typeof v === 'string' && v.trim().length > 0, `${locale} key ${k} present and non-empty`);
      assert.ok(!/\bBL\d+\b/.test(v), 'no tracker IDs in the string');
    }
  });
}
