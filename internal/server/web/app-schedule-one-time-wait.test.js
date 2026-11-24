// Pins the one-time schedule wait limit in the PWA: the Scheduled Spawns card
// has a hours field wired to schedule.one_time_max_wait_hours, with its label
// and help in all five locales (real translations, not English copies).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = (l) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
const KEYS = ['settings_schedule_one_time_max_wait', 'settings_schedule_one_time_max_wait_help'];

test('the one-time wait limit strings exist in all five locales', () => {
  for (const l of LOCALES) {
    for (const k of KEYS) assert.ok(typeof bundle(l)[k] === 'string' && bundle(l)[k].length > 0, `${l} lacks ${k}`);
  }
  for (const l of LOCALES.filter(x => x !== 'en')) {
    for (const k of KEYS) assert.notEqual(bundle(l)[k], bundle('en')[k], `${l} ${k} is an untranslated copy of English`);
  }
  assert.match(bundle('en').settings_schedule_one_time_max_wait_help, /12/);
});

test('the Scheduled Spawns card has the wait limit field wired to its key', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const card = JSON.parse(vm.runInContext("JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'schedule') || null)", sandbox));
  const f = card.fields.find(x => x.key === 'schedule.one_time_max_wait_hours');
  assert.ok(f, 'field missing');
  assert.equal(f.type, 'number');
  assert.equal(f.min, 1);
  assert.equal(f.placeholder, '12');
  assert.ok(bundle('en')[f.labelKey] && bundle('en')[f.helpKey], 'label and help keys must be in the bundle');
});

test('a schedule that gave up waiting for capacity says so, not "0 failed starts"', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  const out = vm.runInContext(`_scheduleRearmSummary(${JSON.stringify({
    state: 'failed', last_fire_result: 'failed', rearm_state: 'gave_up', consecutive_failures: 0, consecutive_waits: 49,
    last_error: 'gave up waiting for capacity after 12 hours: max sessions (20) reached',
  })})`, sandbox);
  assert.match(out, /Gave up waiting for capacity \(49 tries\)/);
  assert.doesNotMatch(out, /0 failed starts/);
  assert.ok(bundle('en').schedule_rearm_gave_up_waiting);
  for (const l of LOCALES.filter(x => x !== 'en')) {
    assert.notEqual(bundle(l).schedule_rearm_gave_up_waiting, bundle('en').schedule_rearm_gave_up_waiting, `${l} is an untranslated copy`);
  }
});
