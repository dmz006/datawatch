// Settings > General > Usage calculator — PWA side.
//
// The ten usage.calculator.* settings sit under one section header. This test
// pins their types and options, that every label and hint key exists in all
// five locale bundles, that the policy options show their locale labels, and
// that saving a field issues PUT /api/config with the dotted key.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync, makeStubElement } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

function loadApp(opts) {
  const sandbox = buildSandbox(opts);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function calculatorSection(sandbox) {
  return JSON.parse(vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'usage_calculator') || null)", sandbox));
}

function fieldsByKey(sandbox) {
  const sec = calculatorSection(sandbox);
  assert.ok(sec, 'usage_calculator section missing from GENERAL_CONFIG_FIELDS');
  const out = {};
  for (const f of sec.fields) out[f.key] = f;
  return out;
}

test('the section has one header and exactly the ten usage.calculator fields', () => {
  const sandbox = loadApp();
  const sec = calculatorSection(sandbox);
  assert.equal(sec.section, 'Usage calculator');
  assert.deepEqual(sec.fields.map(f => f.key), [
    'usage.calculator.variant',
    'usage.calculator.policy',
    'usage.calculator.safety_points',
    'usage.calculator.cost_per_task_points',
    'usage.calculator.baseline_per_24h',
    'usage.calculator.max_lanes',
    'usage.calculator.reset_weekday',
    'usage.calculator.reset_time',
    'usage.calculator.reset_zone',
    'usage.calculator.keeper',
  ]);
  const sectionsNamed = JSON.parse(vm.runInContext(
    "JSON.stringify(GENERAL_CONFIG_FIELDS.filter(s => s.section === 'Usage calculator').length)", sandbox));
  assert.equal(sectionsNamed, 1);
});

test('variant and policy are selects with the design options and the policy labels', () => {
  const f = fieldsByKey(loadApp());
  assert.equal(f['usage.calculator.variant'].type, 'select');
  assert.deepEqual(f['usage.calculator.variant'].options, ['pace-gauge', 'lane-planner', 'burn-ledger', 'all']);
  assert.equal(f['usage.calculator.policy'].type, 'select');
  assert.deepEqual(f['usage.calculator.policy'].options, ['use-all', 'pace', 'finish-early']);
  const en = bundle('en');
  const labels = f['usage.calculator.policy'].optionLabelKeys;
  assert.equal(en[labels['use-all']], 'Run until used up');
  assert.equal(en[labels['pace']], 'Pace to the week');
  assert.equal(en[labels['finish-early']], 'Run flat out');
});

test('the four numeric settings are number inputs and the rest are text or select', () => {
  const f = fieldsByKey(loadApp());
  for (const k of ['safety_points', 'cost_per_task_points', 'baseline_per_24h', 'max_lanes']) {
    assert.equal(f['usage.calculator.' + k].type, 'number', k);
  }
  assert.equal(f['usage.calculator.reset_weekday'].type, 'select');
  assert.deepEqual(f['usage.calculator.reset_weekday'].options,
    ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday']);
  for (const k of ['reset_time', 'reset_zone', 'keeper']) {
    assert.equal(f['usage.calculator.' + k].type, 'text', k);
  }
  assert.equal(f['usage.calculator.reset_time'].placeholder, '08:00');
  assert.equal(f['usage.calculator.reset_zone'].placeholder, 'America/New_York');
});

test('every label, option label and hint key exists in all five locale bundles', () => {
  const sec = calculatorSection(loadApp());
  const keys = [];
  for (const f of sec.fields) {
    assert.ok(f.labelKey && f.labelKey.startsWith('settings_usage_calculator_'), f.key + ' labelKey');
    assert.equal(f.labelKey, 'settings_usage_calculator_' + f.key.split('.').pop());
    keys.push(f.labelKey);
    if (f.hintKey) keys.push(f.hintKey);
    if (f.optionLabelKeys) keys.push(...Object.values(f.optionLabelKeys));
  }
  assert.ok(keys.includes('settings_usage_calculator_reset_zone_hint'));
  assert.ok(keys.includes('settings_usage_calculator_keeper_hint'));
  for (const locale of LOCALES) {
    const b = bundle(locale);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, k + ' missing or empty in ' + locale);
    }
    // The server-zone note is a rendered surface key of the reset_zone row,
    // not a field label, so it is checked here rather than from the section.
    const note = b.settings_usage_calculator_reset_zone_server_note;
    assert.ok(typeof note === 'string' && note.length > 0, 'settings_usage_calculator_reset_zone_server_note missing or empty in ' + locale);
  }
  const en = bundle('en');
  assert.match(en.settings_usage_calculator_reset_zone_hint, /America\/New_York/);
  assert.match(en.settings_usage_calculator_keeper_hint, /empty/i);
  assert.match(en.settings_usage_calculator_reset_zone_server_note, /server zone/i);
});

test('saving a field issues PUT /api/config with the dotted key', async () => {
  const sandbox = loadApp();
  const calls = [];
  sandbox.fetch = (url, init) => {
    calls.push({ url, init });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
  };
  vm.runInContext("saveGeneralField('usage.calculator.policy', 'finish-early')", sandbox);
  vm.runInContext("saveGeneralField('usage.calculator.max_lanes', 2)", sandbox);
  await flushAsync();
  assert.equal(calls.length, 2);
  assert.equal(calls[0].url, '/api/config');
  assert.equal(calls[0].init.method, 'PUT');
  assert.deepEqual(JSON.parse(calls[0].init.body), { 'usage.calculator.policy': 'finish-early' });
  assert.deepEqual(JSON.parse(calls[1].init.body), { 'usage.calculator.max_lanes': 2 });
});

test('the section renders its fields: policy labels, saved value selected, hints, dotted-key save handlers', async () => {
  const holder = makeStubElement();
  const sandbox = loadApp({ byId: { gcfg_usage_calculator: holder } });
  sandbox.fetch = (url) => Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve(url === '/api/config'
      ? { usage: { calculator: { policy: 'pace', max_lanes: 2, reset_zone: 'America/New_York' } } }
      : {}),
  });
  vm.runInContext('loadGeneralConfig()', sandbox);
  await flushAsync(30);
  const html = String(holder.innerHTML || '');
  assert.ok(html.includes('Run until used up'), 'policy label missing: ' + html);
  assert.ok(html.includes('Pace to the week'));
  assert.ok(html.includes('Run flat out'));
  assert.match(html, /<option value="pace" selected>Pace to the week<\/option>/);
  assert.ok(html.includes("saveGeneralField('usage.calculator.max_lanes', Number(this.value))"));
  assert.ok(html.includes("saveGeneralField('usage.calculator.reset_zone', this.value)"));
  assert.ok(html.includes('America/New_York'), 'zone value or hint missing');
  assert.ok(html.includes('Leave empty to keep the readings on this daemon.'), 'keeper hint missing');
  assert.ok(!html.includes('settings_usage_calculator_'), 'a raw locale key leaked into the markup');
});

test('an unset zone renders empty with the effective zone as placeholder and a server-zone note', async () => {
  const holder = makeStubElement();
  const sandbox = loadApp({ byId: { gcfg_usage_calculator: holder } });
  sandbox.fetch = (url) => Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve(url === '/api/config'
      ? { usage: { calculator: { policy: 'pace', reset_zone: 'America/New_York', reset_zone_source: 'server' } } }
      : {}),
  });
  vm.runInContext('loadGeneralConfig()', sandbox);
  await flushAsync(30);
  const html = String(holder.innerHTML || '');
  // The zone input carries the save handler and the effective-zone
  // placeholder, and the note says the reset follows this daemon's zone.
  const zoneParts = html.split("saveGeneralField('usage.calculator.reset_zone', this.value)");
  assert.ok(zoneParts.length > 1, 'the reset_zone field did not render');
  const handlerAt = html.indexOf("saveGeneralField('usage.calculator.reset_zone', this.value)");
  const fieldHtml = html.substring(Math.max(0, handlerAt - 600), handlerAt);
  assert.match(fieldHtml, /placeholder="America\/New_York"/, 'effective zone placeholder: ' + fieldHtml);
  assert.ok(html.includes('server zone'), 'server-zone note missing: ' + html);
  // Saving with the input empty puts back empty, never a rewritten default.
  const calls = [];
  sandbox.fetch = (url, init) => {
    calls.push({ url, init });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) });
  };
  vm.runInContext("saveGeneralField('usage.calculator.reset_zone', '')", sandbox);
  await flushAsync();
  assert.equal(calls.length, 1);
  assert.deepEqual(JSON.parse(calls[0].init.body), { 'usage.calculator.reset_zone': '' });
});

test('a configured zone renders its saved value without the server-zone note', async () => {
  const holder = makeStubElement();
  const sandbox = loadApp({ byId: { gcfg_usage_calculator: holder } });
  sandbox.fetch = (url) => Promise.resolve({
    ok: true,
    status: 200,
    json: () => Promise.resolve(url === '/api/config'
      ? { usage: { calculator: { policy: 'pace', reset_zone: 'UTC', reset_zone_source: 'configured' } } }
      : {}),
  });
  vm.runInContext('loadGeneralConfig()', sandbox);
  await flushAsync(30);
  const html = String(holder.innerHTML || '');
  assert.match(html, /value="UTC"/ , 'the pinned zone should be the input value: ' + html);
  assert.ok(!html.includes('server zone'), 'configured zone wrongly shows the server-zone note');
});
