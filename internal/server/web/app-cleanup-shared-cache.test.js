'use strict';
// The shared-cache sweep switch is a toggle in the Automata settings, off by
// default, with its label in every locale.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const KEY = 'settings_automation_cleanup_shared_cache_sweep';
const app = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));

test('the shared-cache sweep is a toggle with its English label as the fallback', () => {
  const line = app.split('\n').find((l) => l.includes("key: 'autonomous.automation.cleanup_shared_cache_sweep'"));
  assert.ok(line, 'field is declared');
  assert.ok(line.includes("type: 'toggle'"), 'field is a toggle');
  assert.ok(line.includes("labelKey: '" + KEY + "'"), 'field has its locale key');
  const en = bundle('en')[KEY];
  assert.ok(en, 'English label exists');
  assert.ok(line.includes("label: '" + en + "'"), 'fallback label equals the English text');
});

test('every locale has the label', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const text = bundle(lang)[KEY];
    assert.ok(text && text.length > 0, lang + ' lacks ' + KEY);
  }
});
