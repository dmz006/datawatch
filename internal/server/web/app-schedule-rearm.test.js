// Pins the schedule re-arm surfaces in the PWA: the Settings card with the four
// policy fields (each label in all five locales) and the schedule list rows
// that show the next fire, the last result with its reason and the re-arm state.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundle = (l) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  return sandbox;
}

const KEYS = [
  'settings_schedule_rearm_mode', 'settings_schedule_rearm_mode_help', 'settings_schedule_rearm_max_retries',
  'settings_schedule_rearm_backoff', 'settings_schedule_rearm_backoff_max', 'schedule_row_next', 'schedule_row_no_next',
  'schedule_row_last', 'schedule_result_spawned', 'schedule_result_deferred', 'schedule_result_failed',
  'schedule_result_skipped', 'schedule_rearm_waiting', 'schedule_rearm_retrying', 'schedule_rearm_gave_up',
];

test('every schedule re-arm string exists in all five locales', () => {
  for (const l of LOCALES) {
    const b = bundle(l);
    for (const k of KEYS) assert.ok(typeof b[k] === 'string' && b[k].length > 0, `${l} lacks ${k}`);
  }
  for (const l of LOCALES.filter(x => x !== 'en')) {
    for (const k of KEYS) {
      assert.notEqual(bundle(l)[k], bundle('en')[k], `${l} ${k} is an untranslated copy of English`);
    }
  }
});

test('the Settings card has the policy fields wired to the schedule.* keys', () => {
  const sb = loadApp();
  const card = JSON.parse(vm.runInContext("JSON.stringify(GENERAL_CONFIG_FIELDS.find(s => s.id === 'schedule') || null)", sb));
  assert.ok(card, 'schedule card missing');
  assert.deepEqual(card.fields.map(f => f.key), [
    'schedule.rearm_mode', 'schedule.rearm_max_retries', 'schedule.rearm_backoff_seconds', 'schedule.rearm_backoff_max_seconds',
    'schedule.one_time_max_wait_hours']);
  assert.deepEqual(card.fields[0].options, ['next_fire', 'backoff', 'stay_failed']);
  for (const f of card.fields) assert.ok(bundle('en')[f.labelKey], `${f.key} label key not in the bundle`);
});

test('a waiting schedule row shows next fire, last result with reason and state', () => {
  const sb = loadApp();
  const out = vm.runInContext(`_scheduleRearmSummary(${JSON.stringify({
    state: 'pending', run_at: '2030-01-02T03:04:05Z', last_fire_result: 'deferred',
    last_error: 'max sessions (20) reached', rearm_state: 'waiting_capacity', consecutive_waits: 3,
  })})`, sb);
  assert.match(out, /Next: /);
  assert.match(out, /Last: waiting for capacity \(max sessions \(20\) reached\)/);
  assert.match(out, /Waiting for capacity \(×3\)/);
});

test('a schedule the policy gave up on says so and has no next fire', () => {
  const sb = loadApp();
  const out = vm.runInContext(`_scheduleRearmSummary(${JSON.stringify({
    state: 'failed', last_fire_result: 'failed', last_error: 'tmux exploded', rearm_state: 'gave_up', consecutive_failures: 4,
  })})`, sb);
  assert.match(out, /No next fire/);
  assert.match(out, /Gave up after 4 failed starts/);
});

test('a schedule that never fired shows no re-arm line', () => {
  const sb = loadApp();
  assert.equal(vm.runInContext(`_scheduleRearmSummary(${JSON.stringify({ state: 'pending', run_at: '2030-01-02T03:04:05Z' })})`, sb), '');
});
