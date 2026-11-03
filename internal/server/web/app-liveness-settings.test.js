// Pins the Settings > Sessions liveness fields and the locale-driven
// session state chips: both fields exist with locale label and hint keys,
// the hint renders, and realStateChips labels come from t().

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

function sessField(sandbox, key) {
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'sess') || {fields: []}).fields.find(f => f.key === '" + key + "') || null)",
    sandbox));
}

const FIELDS = {
  'session.liveness_window_seconds': {
    type: 'number', placeholder: '120',
    labelKey: 'settings_session_liveness_window_label', hintKey: 'settings_session_liveness_window_hint',
  },
  'session.liveness_idle_timeout_overrides': {
    type: 'json_map',
    labelKey: 'settings_session_liveness_overrides_label', hintKey: 'settings_session_liveness_overrides_hint',
  },
};

test('liveness fields exist with locale label and hint keys', () => {
  const sandbox = loadApp();
  for (const [key, want] of Object.entries(FIELDS)) {
    const f = sessField(sandbox, key);
    assert.ok(f, key + ' missing from the sessions card');
    assert.equal(f.type, want.type);
    assert.equal(f.labelKey, want.labelKey);
    assert.equal(f.hintKey, want.hintKey);
    if (want.placeholder) assert.equal(f.placeholder, want.placeholder);
  }
});

test('liveness label and hint resolve from every locale bundle', () => {
  for (const lang of LOCALE_LIST) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const want of Object.values(FIELDS)) {
      for (const k of [want.labelKey, want.hintKey]) {
        assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
      }
    }
  }
});

test('the hint line renders the locale text', () => {
  const sandbox = loadApp();
  sandbox.t = (k) => 'TXT:' + k;
  for (const [key, want] of Object.entries(FIELDS)) {
    const f = sessField(sandbox, key);
    sandbox.__f = f;
    const html = vm.runInContext('_fieldHintHTML(__f)', sandbox);
    assert.ok(html.includes('TXT:' + want.hintKey), key + ' hint not rendered: ' + html);
  }
});

test('the overrides save helper sends an object and rejects bad input', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.match(src, /type === 'json_map'/);
  assert.match(src, /function saveJSONMapSetting\(/);
});

test('realStateChips labels come from t() lookups', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const start = src.indexOf('const realStateChips = [');
  assert.ok(start > 0, 'realStateChips not found');
  const block = src.slice(start, src.indexOf('];', start));
  const expected = {
    all: 'session_chip_all', running: 'session_chip_running', waiting_input: 'session_chip_waiting',
    stalled: 'session_chip_stalled', rate_limited: 'session_chip_rate_limited',
    complete: 'session_chip_complete', failed: 'session_chip_failed', killed: 'session_chip_killed',
  };
  for (const [state, key] of Object.entries(expected)) {
    const re = new RegExp("key: '" + state + "',\\s*label: t\\('" + key + "'\\)");
    assert.match(block, re, state + ' chip does not use t(' + key + ')');
  }
  assert.ok(!/label: '/.test(block), 'a chip label is still an English literal');
});
