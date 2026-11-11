// Every list-mode label key exists in all five locales.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');

const KEYS = [
  'list_mode_toggle', 'list_mode_list', 'list_mode_tiles', 'list_mode_compact',
  'list_group_by', 'list_group_by_none', 'list_group_untagged', 'list_group_none',
  'list_state_mix', 'list_show_more', 'list_overflow_menu',
];

for (const loc of ['de', 'en', 'es', 'fr', 'ja']) {
  test(`locale ${loc} has every list-mode key`, () => {
    const d = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.equal(typeof d[k], 'string', `${loc} missing ${k}`);
      assert.ok(d[k].trim().length > 0, `${loc} empty ${k}`);
    }
  });
}
