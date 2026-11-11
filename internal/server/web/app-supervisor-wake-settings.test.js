// Supervisor wake group in Settings > Automata: the enable switch, the kind
// multi-select, the quiet window, the fallback interval and the status line.
// Every field carries a "?" help entry linked to the supervisor loop howto,
// saves through PUT /api/config, and every string exists in all five locale
// bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_DIR = path.join(__dirname, 'locales');
const KEY = 'autonomous.supervisor_loop.wake.';
const FIELD_HELP_KEYS = [
  'supervisor_loop_wake_enabled_hint',
  'supervisor_loop_wake_kinds_hint',
  'supervisor_loop_wake_coalesce_seconds_hint',
  'supervisor_loop_wake_fallback_interval_minutes_hint',
  'supervisor_wake_status_help',
];
const NEW_KEYS = [
  'supervisor_wake_group', 'supervisor_wake_group_hint', 'supervisor_wake_status_help',
  'supervisor_wake_status_loading', 'supervisor_wake_status_never', 'supervisor_wake_status_line',
  'supervisor_wake_status_silent', 'supervisor_wake_status_unavailable', 'supervisor_wake_number_invalid',
];

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, locale + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sandbox);
  return sandbox;
}

function run(sandbox, code) { return vm.runInContext(code, sandbox); }

test('the autonomous settings hold a wake group and the wake card that draws the wake keys', () => {
  const sandbox = loadApp();
  const fields = JSON.parse(run(sandbox,
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)"));
  const gi = fields.findIndex(f => f.type === 'group' && f.labelKey === 'supervisor_wake_group');
  assert.ok(gi >= 0, 'wake group heading present');
  assert.ok(fields.findIndex(f => f.type === 'supervisor_wake_card') > gi, 'wake card follows the group heading');
  const wake = fields.filter(f => String(f.key || '').startsWith(KEY));
  assert.equal(wake.length, 4);
  for (const f of wake) assert.equal(f.card, 'supervisor_wake', f.key + ' is drawn by the card');
});

test('the card has the switch, every kind, both numbers and a help entry per field', () => {
  const sandbox = loadApp();
  const html = run(sandbox, "supervisorWakeCardHtml({autonomous:{supervisor_loop:{wake:{enabled:true,kinds:['prd.blocked','task.failed'],coalesce_seconds:45,fallback_interval_minutes:15}}}})");
  assert.match(html, /id="supervisorWake_enabled" checked/);
  assert.ok(html.includes("saveGeneralField('" + KEY + "enabled', this.checked)"));
  const kinds = JSON.parse(run(sandbox, 'JSON.stringify(NOTIFY_EVENT_KINDS)'));
  for (const k of kinds) assert.ok(html.includes('value="' + k + '"'), 'kind ' + k);
  assert.match(html, /value="prd\.blocked" checked/);
  assert.doesNotMatch(html, /value="prd\.active" checked/);
  assert.match(html, /id="supervisorWake_coalesce_seconds"[^>]*value="45"[^>]*min="0"[^>]*max="3600"/);
  assert.match(html, /id="supervisorWake_fallback_interval_minutes"[^>]*value="15"[^>]*min="1"[^>]*max="1440"/);
  assert.equal((html.match(/class="docs-link"[^>]*>\?<\/a>/g) || []).length, 5, 'one ? per field and the status line');
  assert.ok(html.includes('howto/supervisor-loop.md#settings-and-defaults'));
});

test('a stored kind the list does not know stays visible and checked', () => {
  const sandbox = loadApp();
  const html = run(sandbox, "supervisorWakeCardHtml({autonomous:{supervisor_loop:{wake:{kinds:['plugin.custom']}}}})");
  assert.match(html, /value="plugin\.custom" checked/);
});

test('saving the kinds sends the checked kinds as a list', () => {
  const sandbox = loadApp();
  sandbox.document.querySelectorAll = () => [
    { checked: true, value: 'prd.blocked' }, { checked: false, value: 'prd.failed' }, { checked: true, value: 'task.failed' },
  ];
  run(sandbox, 'var _sent = null; saveGeneralField = (k, v) => { _sent = [k, v]; };');
  run(sandbox, 'saveSupervisorWakeKinds()');
  assert.deepEqual(JSON.parse(run(sandbox, 'JSON.stringify(_sent)')), [KEY + 'kinds', ['prd.blocked', 'task.failed']]);
});

test('a number outside its range is refused and nothing is sent', () => {
  const sandbox = loadApp();
  run(sandbox, 'var _sent = null; saveGeneralField = (k, v) => { _sent = [k, v]; }; showToast = () => {};');
  const el = { value: '4000', defaultValue: '30' };
  sandbox.__el = el;
  run(sandbox, "saveSupervisorWakeNumber('coalesce_seconds', __el, 0, 3600)");
  assert.equal(run(sandbox, '_sent'), null);
  assert.equal(el.value, '30');
  sandbox.__el = { value: '12', defaultValue: '20' };
  run(sandbox, "saveSupervisorWakeNumber('fallback_interval_minutes', __el, 1, 1440)");
  assert.deepEqual(JSON.parse(run(sandbox, 'JSON.stringify(_sent)')), [KEY + 'fallback_interval_minutes', 12]);
});

test('the status line shows the last event and the last scheduled check', () => {
  const sandbox = loadApp();
  sandbox.__v = {
    last_event: '2026-11-10T03:00:00Z', event_count: 7,
    last_scheduled_check: '0001-01-01T00:00:00Z', scheduled_count: 0,
  };
  const text = run(sandbox, 'supervisorWakeStatusText(__v)');
  assert.match(text, /Last event wake: .*\(7 total\)/);
  assert.match(text, /Last scheduled check: not yet \(0 total\)/);
  assert.doesNotMatch(text, /No event has woken/);
  sandbox.__v.events_silent_for = '1h5m0s';
  assert.match(run(sandbox, 'supervisorWakeStatusText(__v)'), /No event has woken the supervisor for over 1h5m0s/);
});

test('every wake string exists in all five locale bundles', () => {
  for (const lg of LOCALES) {
    const b = bundle(lg);
    for (const k of NEW_KEYS.concat(FIELD_HELP_KEYS, ['supervisor_loop_wake_enabled', 'supervisor_loop_wake_kinds',
      'supervisor_loop_wake_coalesce_seconds', 'supervisor_loop_wake_fallback_interval_minutes'])) {
      assert.ok(typeof b[k] === 'string' && b[k].trim(), lg + ' has ' + k);
    }
    assert.match(b.supervisor_wake_status_line, /%1\$s.*%2\$d.*%3\$s.*%4\$d/, lg + ' status line placeholders');
    for (const k of NEW_KEYS) assert.doesNotMatch(b[k], /\bPRD\b|\b(BL|B|GH)\d{2,}\b/, lg + ' ' + k + ' wording');
  }
});
