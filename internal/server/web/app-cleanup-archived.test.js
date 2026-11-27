// The cleanup_archived settings field: a toggle with label and help keys in
// every locale bundle.
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const app = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

test('cleanup_archived is a toggle with label and help keys', () => {
  const line = app.split('\n').find((l) => l.includes("key: 'autonomous.automation.cleanup_archived'"));
  assert.ok(line, 'field is declared');
  assert.match(line, /type: 'toggle'/);
  assert.match(line, /labelKey: 'settings_automation_cleanup_archived'/);
  assert.match(line, /helpKey: 'settings_automation_cleanup_archived_help'/);
});

test('every locale bundle has both keys, and English matches the fallback label', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const key of ['settings_automation_cleanup_archived', 'settings_automation_cleanup_archived_help']) {
      assert.ok(b[key] && b[key].trim(), `${lang} is missing ${key}`);
    }
    if (lang === 'en') {
      assert.ok(app.includes(`label: '${b.settings_automation_cleanup_archived}'`), 'fallback label differs from English');
    }
  }
});

test('the needed_by_record skip reason has a label in every locale bundle', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    assert.ok(b.cleanup_skip_reason_needed_by_record, lang + ' is missing the label');
  }
});
