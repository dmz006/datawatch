// The output filter list marks the filters that type into sessions, and
// creating or saving one asks first.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function fn(name) {
  const start = src.indexOf(`function ${name}(`);
  assert.ok(start >= 0, `${name} missing`);
  const end = src.indexOf('\n}\n', start);
  return src.slice(start, end + 3);
}

function load(confirmAnswer) {
  const body = ['filterTypesInput', 'filterBadgesHTML', 'confirmInputFilter'].map(fn).join('\n');
  return new Function('t', 'escHtml', 'confirm',
    `${body}\nreturn { filterBadgesHTML, confirmInputFilter };`)(
    k => k, s => String(s).replace(/</g, '&lt;'), () => confirmAnswer);
}

test('typing filters are badged and show why they were turned off', () => {
  const { filterBadgesHTML } = load(true);
  for (const action of ['send_input', 'schedule']) {
    assert.match(filterBadgesHTML({ action }), /filter_types_input_badge/);
  }
  assert.strictEqual(filterBadgesHTML({ action: 'alert' }), '');
  assert.match(filterBadgesHTML({ action: 'schedule', disabled_reason: '<x>' }), /&lt;x>/);
});

test('consent is asked only for typing filters', () => {
  assert.deepStrictEqual(load(false).confirmInputFilter('alert'), {});
  assert.strictEqual(load(false).confirmInputFilter('send_input'), null);
  assert.deepStrictEqual(load(true).confirmInputFilter('schedule'), { allow_input: true });
});

test('create and edit send the consent', () => {
  for (const name of ['createFilter', 'saveFilterEdit']) {
    assert.match(fn(name), /confirmInputFilter\(action\)/);
    assert.match(fn(name), /\.\.\.consent/);
  }
});

test('the switch is a Sessions setting with every locale string', () => {
  assert.match(src, /key: 'session\.input_filters_enabled'.*type: 'toggle'/);
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const l = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', `${lang}.json`), 'utf8'));
    for (const k of ['settings_input_filters_enabled', 'settings_input_filters_enabled_help', 'filter_types_input_badge',
      'filter_types_input_note', 'filter_types_input_confirm', 'filter_saved_off', 'filter_value_placeholder']) {
      assert.ok(l[k], `${lang}: ${k}`);
    }
  }
});
