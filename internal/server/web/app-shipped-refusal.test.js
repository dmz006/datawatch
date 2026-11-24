// Shipped-work rule — PWA side: the Settings row for autonomous.shipped_refusal
// and the lane fill reason label for already_shipped, in all five bundles.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-shipped-refusal.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const APP = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

test('the Settings page has a select row for autonomous.shipped_refusal with on, shadow and off', () => {
  const m = APP.match(/\{ key: 'autonomous\.shipped_refusal'[^\n]*\}/);
  assert.ok(m, 'autonomous.shipped_refusal row missing from the settings fields');
  assert.match(m[0], /labelKey: 'settings_shipped_refusal'/);
  assert.match(m[0], /type: 'select'/);
  assert.match(m[0], /options: \['on', 'shadow', 'off'\]/);
  assert.match(m[0], /default: 'on'/);
});

test('the settings label and the already_shipped reason label exist in every bundle', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of ['settings_shipped_refusal', 'lane_fill_eligibility_already_shipped']) {
      assert.ok(typeof b[k] === 'string' && b[k].trim() !== '', k + ' missing or empty in ' + lang);
    }
    assert.doesNotMatch(b.lane_fill_eligibility_already_shipped, /\b(PRD|BL\d+)\b/, lang + ' leaks an internal name');
    if (lang !== 'en') {
      assert.notEqual(b.lane_fill_eligibility_already_shipped, en.lane_fill_eligibility_already_shipped,
        'the reason label in ' + lang + ' must be a real translation, not the English text');
    }
  }
});
